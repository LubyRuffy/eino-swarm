"""A first public TestFlight build freezes a batch even without a GitHub tag."""
import copy
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release
import release_ios


class PublicRecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'source'
        self.root.mkdir()
        self.remote = self.base / 'remote.git'
        self.git('init', '--bare', str(self.remote))
        self.git('init', '-b', 'main')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'Offline release fixture')
        self.git('remote', 'add', 'origin', str(self.remote))
        (self.root / 'mobile').mkdir()
        release.write_json(self.root / 'mobile/package.json', {'version': '1.2.3'})
        (self.root / 'go.mod').write_text('module github.com/example/app\n')
        self.git('add', 'mobile/package.json', 'go.mod')
        self.git('commit', '-m', 'Complete verified batch')
        self.git('push', 'origin', 'main')
        self.sha = self.git('rev-parse', 'HEAD')
        self.state_home = self.base / 'private'
        self.delivery_path = self.state_home / 'issue-automation/delivery-state.json'
        self.state_path = self.state_home / 'releases/1.2.3.json'
        self.ledger_path = self.state_home / 'ios-signing/testflight-releases.json'
        self.config_path = self.state_home / 'ios-signing/release-config.json'
        self.groups = ['internal-a', 'internal-b', 'external-a', 'external-b']
        self.batch = {'version': '1.2.3', 'source_sha': self.sha, 'issues': [1],
            'qualified_for_new_release': True, 'version_locked': True, 'ios': 'published',
            'platforms_to_release': ['macos', 'android', 'ios'],
            'finalized_workset': {'source_sha': self.sha, 'issues': [1],
                'platforms_to_release': ['macos', 'android', 'ios'], 'active_issue': None}}
        self.delivery = {'active_issue': None, 'pending_new_batch': self.batch}
        self.state = {'version': '1.2.3', 'source_sha': self.sha, 'ios': {
            'status': 'published', 'source_sha': self.sha, 'build_number': '10203',
            'marketing_version': '9.0.0', 'build_id': 'build-one',
            'ipa_sha256': 'a' * 64, 'group_ids': self.groups}}
        self.ledger = {'releases': [{'app_id': 'app', 'version': '1.2.3',
            'source_sha': self.sha, 'build_number': '10203', 'marketing_version': '9.0.0',
            'successfully_published': True, 'ipa_sha256': 'a' * 64}]}
        self.build = {'id': 'build-one', 'attributes': {'version': '10203',
            'processingState': 'VALID', 'expired': False}}
        self.detail = {'attributes': {'internalBuildState': 'READY_FOR_BETA_TESTING',
                                     'externalBuildState': 'IN_BETA_TESTING'}}
        self.review = {'attributes': {'betaReviewState': 'APPROVED'}}
        self.marketing = '9.0.0'
        self.members = set(self.groups)
        self.reverse_members = set(self.groups)
        self.calls = []
        release.write_json(self.config_path, {'signing_dir': str(self.base / 'signing'),
            'testflight_cli': str(self.base / 'offline-client.py'), 'group_ids': self.groups})
        self.persist()
        self.env = patch.dict(os.environ, {'ZWAI_HOME': str(self.state_home),
            'DELIVERY_STATE': str(self.delivery_path), 'IOS_RELEASE_CONFIG': str(self.config_path)})
        self.env.start()
        self.addCleanup(self.env.stop)
        original = release.run

        def command(args, **kwargs):
            self.calls.append(args)
            if args[0] == 'gh':
                if args[1] == 'repo':
                    return 'example/app'
                return json.dumps([[{'tag_name': 'v1.2.2'}]])
            if args[0] == sys.executable:
                # The real reconciliation excludes this already public batch.
                return json.dumps({'resume_allocated_version': False, 'main_sha': self.sha,
                    'unallocated_issues': [], 'platforms_to_release': []})
            return original(args, **kwargs)

        self.command_patch = patch.object(release, 'run', side_effect=command)
        self.command_patch.start()
        self.addCleanup(self.command_patch.stop)
        module = SimpleNamespace(APP_ID='app', Client=lambda: self)
        client_patch = patch.object(release_ios, 'load_client', return_value=module)
        client_patch.start()
        self.addCleanup(client_patch.stop)

    def git(self, *args):
        result = subprocess.run(['git', *args], cwd=self.root, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip()

    def persist(self):
        release.write_json(self.delivery_path, self.delivery)
        release.write_json(self.state_path, self.state)
        release.write_json(self.ledger_path, self.ledger)

    def all(self, route):
        self.calls.append(['GET', route])
        if '/buildUploads?' in route:
            return []
        if route.startswith('apps/') and '/betaGroups?' in route:
            return [{'id': group, 'attributes': {'isInternalGroup': group.startswith('internal')}}
                    for group in self.groups]
        if route.startswith('betaGroups/'):
            return [self.build] if route.split('/')[1] in self.members else []
        if route == f'betaGroups?filter%5Bbuilds%5D={self.build["id"]}&limit=200':
            return [{'id': group} for group in self.reverse_members]
        if '/builds?' in route:
            return [self.build]
        raise AssertionError(route)

    def request(self, method, route):
        self.calls.append([method, route])
        self.assertEqual(method, 'GET', 'Recovery proof must never mutate Apple')
        if route.endswith('/buildBetaDetail'):
            return {'data': self.detail}
        if route.endswith('/betaAppReviewSubmission'):
            return {'data': self.review}
        if route.endswith('/preReleaseVersion'):
            return {'data': {'attributes': {'version': self.marketing}}}
        raise AssertionError(route)

    def allocated_unpublished(self):
        self.batch.update(version_locked=False, ios='pending',
            allocated_at='2026-01-01T00:00:00+00:00',
            required_scope_source_shas=[self.sha],
            additional_platforms={'macos': {'source_shas': [self.sha],
                'status': 'pending_native_complete_main_scope_acceptance'},
                'android': {'source_shas': [self.sha], 'inherited_source_shas': [self.sha],
                    'status': 'pending_native_device_acceptance'}})
        self.delivery['issues'] = {'1': {'code': 'verified_integrated_and_pushed',
            'fix_sha': self.sha, 'macos': 'pending_native_window_acceptance',
            'android': 'pending_native_device_acceptance', 'ios': 'pending'}}
        self.state = {'version': '1.2.3', 'source_sha': self.sha}
        self.ledger = {'releases': []}
        self.persist()
        (self.root / 'later.txt').write_text('Later main work stays outside this allocation.\n')
        self.git('add', 'later.txt')
        self.git('commit', '-m', 'Later independent main work')
        self.git('push', 'origin', 'main')
        self.later_sha = self.git('rev-parse', 'HEAD')
        self.git('checkout', '--detach', self.sha)

    def test_allocated_unpublished_source_resumes_after_main_advances(self):
        self.allocated_unpublished()
        originals = {p: p.read_bytes() for p in (self.delivery_path, self.state_path, self.ledger_path)}
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('macos',)), self.sha)
        self.assertEqual(self.git('rev-parse', 'HEAD'), self.sha)
        self.assertEqual(self.git('rev-parse', 'origin/main'), self.later_sha)
        self.assertEqual(self.git('ls-remote', 'origin', 'refs/tags/v1.2.3'), '')
        self.assertEqual(originals, {p: p.read_bytes() for p in originals})
        self.assertFalse(any(call[0] in (sys.executable, 'make', 'go', 'GET') for call in self.calls))
        self.batch['finalized_workset']['required_scope_source_shas'] = [self.sha]
        self.persist()
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('macos',)), self.sha)

    def test_unpublished_recovery_keeps_remote_version_and_tag_conflicts(self):
        self.allocated_unpublished()
        command = release.run
        def occupied(args, **kwargs):
            if args[:4] == ['gh', 'api', '--paginate', '--slurp']:
                return json.dumps([[{'tag_name': 'v1.2.3'}]])
            return command(args, **kwargs)
        with patch.object(release, 'run', side_effect=occupied), \
                self.assertRaisesRegex(ValueError, 'strictly greater'):
            release.source_preflight(self.root, '1.2.3', ('macos',))
        self.git('tag', 'v1.2.3', self.later_sha)
        self.git('push', 'origin', 'refs/tags/v1.2.3')
        with self.assertRaisesRegex(ValueError, 'frozen at another source'):
            release.source_preflight(self.root, '1.2.3', ('macos',))

    def test_unpublished_recovery_refuses_incomplete_or_changed_allocation(self):
        self.allocated_unpublished()
        original = copy.deepcopy(self.delivery)
        changes = [
            lambda d: d['pending_new_batch'].update(qualified_for_new_release=False),
            lambda d: d['pending_new_batch'].update(version_locked=None),
            lambda d: d['pending_new_batch'].update(source_sha=self.later_sha),
            lambda d: d['pending_new_batch'].pop('allocated_at'),
            lambda d: d['pending_new_batch'].update(allocated_at='not-a-date'),
            lambda d: d['pending_new_batch'].update(allocated_at='2026-01-01'),
            lambda d: d.update(active_issue=2),
            lambda d: d.pop('active_issue'),
            lambda d: d['pending_new_batch']['finalized_workset'].update(active_issue=2),
            lambda d: d['pending_new_batch']['finalized_workset'].pop('active_issue'),
            lambda d: d['pending_new_batch']['finalized_workset'].update(source_sha=self.later_sha),
            lambda d: d['pending_new_batch']['finalized_workset'].update(issues=[2]),
            lambda d: d['pending_new_batch']['finalized_workset'].update(platforms_to_release=['macos']),
            lambda d: d['pending_new_batch'].update(platforms_to_release=['ios']),
            lambda d: d['pending_new_batch'].update(issues=[1, 1]),
            lambda d: d['pending_new_batch'].update(required_scope_source_shas=[]),
            lambda d: d['pending_new_batch'].pop('required_scope_source_shas'),
            lambda d: d['pending_new_batch'].update(required_scope_source_shas=[self.sha, self.sha]),
            lambda d: d['pending_new_batch'].update(required_scope_source_shas=['unknown']),
            lambda d: d['pending_new_batch'].update(required_scope_source_shas=[self.later_sha]),
            lambda d: d['pending_new_batch']['finalized_workset'].update(required_scope_source_shas=[self.later_sha]),
            lambda d: d['pending_new_batch'].update(additional_platforms=[]),
            lambda d: d['pending_new_batch']['additional_platforms'].update(server={'source_shas': [self.sha]}),
            lambda d: d['pending_new_batch']['additional_platforms']['macos'].update(source_shas=[]),
            lambda d: d['pending_new_batch']['additional_platforms']['macos'].update(source_shas=[self.later_sha]),
            lambda d: d['pending_new_batch']['additional_platforms']['android'].update(inherited_source_shas=[self.later_sha]),
            lambda d: d['pending_new_batch']['additional_platforms']['android'].update(inherited_source_shas='unknown'),
            lambda d: d['issues']['1'].update(code='pending'),
            lambda d: d['issues']['1'].update(fix_sha=self.later_sha),
            lambda d: d['issues']['1'].update(fix_sha={}),
        ]
        for index, change in enumerate(changes):
            self.delivery = copy.deepcopy(original)
            change(self.delivery)
            self.persist()
            with self.subTest(change=index), self.assertRaises((ValueError, RuntimeError)):
                release.source_preflight(self.root, '1.2.3', ('macos',))
        self.assertFalse(any(call[0] in ('make', 'go', 'GET') for call in self.calls))

    def test_unpublished_recovery_refuses_coordinated_platform_shrink(self):
        self.allocated_unpublished()
        original = copy.deepcopy(self.delivery)
        for delivered in (False, True):
            self.delivery = copy.deepcopy(original)
            batch = self.delivery['pending_new_batch']
            if delivered:
                self.delivery['issues']['1'].update(android='published', ios='delivered')
            batch['platforms_to_release'] = ['macos']
            batch['finalized_workset']['platforms_to_release'] = ['macos']
            del batch['additional_platforms']['android']
            self.persist()
            with self.subTest(delivered=delivered), self.assertRaisesRegex(ValueError, 'platform scope'):
                release.source_preflight(self.root, '1.2.3', ('macos',))

    def test_unpublished_recovery_refuses_orphaned_extra_scope(self):
        (self.root / 'extra.txt').write_text('Independent additional desktop scope.\n')
        self.git('add', 'extra.txt')
        self.git('commit', '-m', 'Additional behavior')
        extra_sha = self.git('rev-parse', 'HEAD')
        (self.root / 'fix.txt').write_text('Accepted Issue fix.\n')
        self.git('add', 'fix.txt')
        self.git('commit', '-m', 'Issue behavior')
        self.sha = self.git('rev-parse', 'HEAD')
        self.batch['source_sha'] = self.sha
        self.batch['finalized_workset']['source_sha'] = self.sha
        self.allocated_unpublished()
        self.batch['required_scope_source_shas'] = [self.sha, extra_sha]
        self.batch['additional_platforms']['macos']['source_shas'] = [extra_sha]
        self.persist()
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('macos',)), self.sha)
        del self.batch['additional_platforms']['macos']
        self.persist()
        with self.assertRaisesRegex(ValueError, 'scope identities'):
            release.source_preflight(self.root, '1.2.3', ('macos',))

    def test_unpublished_recovery_keeps_state_and_native_gates(self):
        self.allocated_unpublished()
        with self.assertRaisesRegex(ValueError, 'native acceptance'):
            release.platform_acceptance_gate('1.2.3', 'macos')
        self.state['source_sha'] = self.later_sha
        self.persist()
        with self.assertRaisesRegex(ValueError, 'another version/source'):
            release.execute(self.root, '1.2.3', 'macos', self.state_path)
        self.assertFalse(any(call[0] in ('make', 'go', 'GET') for call in self.calls))

    def test_actual_cli_uses_source_dir_for_unpublished_recovery_without_writes(self):
        self.allocated_unpublished()
        originals = {p: p.read_bytes() for p in (self.delivery_path, self.state_path, self.ledger_path)}
        command = release.run
        def no_build(args, **kwargs):
            if args[0] == 'go':
                self.calls.append(args)
                self.assertEqual(args, ['go', 'run', './internal/release/cmd', '-version', '1.2.3', '-check'])
                return ''
            return command(args, **kwargs)
        with patch.object(sys, 'argv', ['release.py', '--version', '1.2.3', '--platform', 'macos',
                '--source-dir', str(self.root), '--check']), patch.object(release, 'run', side_effect=no_build):
            self.assertEqual(release.main(), 0)
        self.assertEqual(originals, {p: p.read_bytes() for p in originals})
        self.assertEqual(self.git('rev-parse', 'HEAD'), self.sha)
        self.assertEqual(self.git('ls-remote', 'origin', 'refs/tags/v1.2.3'), '')

    def test_ios_first_without_tag_resumes_both_installers_after_main_advances(self):
        (self.root / 'later.txt').write_text('A later batch must not enter the frozen version.\n')
        self.git('add', 'later.txt')
        self.git('commit', '-m', 'Later independent main work')
        self.git('push', 'origin', 'main')
        self.git('checkout', '--detach', self.sha)
        originals = {p: p.read_bytes() for p in (self.delivery_path, self.state_path, self.ledger_path)}
        for platform in ('android', 'macos'):
            self.assertEqual(release.source_preflight(self.root, '1.2.3', (platform,)), self.sha)
        self.assertEqual(originals, {p: p.read_bytes() for p in originals})
        self.assertFalse(any(call[0] == sys.executable for call in self.calls))
        self.assertFalse(any(call[0] in ('make', 'xcodebuild', 'xcrun') for call in self.calls))

    def test_old_public_batch_recovery_does_not_rewind_current_batch(self):
        self.delivery['completed_batches'] = {'1.2.3': {'batch': copy.deepcopy(self.batch)}}
        self.delivery['pending_new_batch'] = {'version': '1.2.4', 'source_sha': 'later'}
        self.persist()
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('android',)), self.sha)

    def test_public_flags_without_matching_source_and_remote_availability_are_refused(self):
        changes = [
            lambda: self.state.update(source_sha='wrong'),
            lambda: self.state['ios'].update(source_sha='wrong'),
            lambda: self.state['ios'].update(build_number='10204'),
            lambda: self.state['ios'].update(build_id='other-build'),
            lambda: self.state['ios'].update(ipa_sha256='b' * 64),
            lambda: self.state['ios'].update(status='waiting_beta_review'),
            lambda: self.ledger['releases'][0].update(successfully_published=False),
            lambda: self.ledger['releases'][0].update(source_sha='wrong'),
            lambda: self.ledger['releases'][0].update(marketing_version='8.0.0'),
            lambda: self.ledger['releases'].clear(),
            lambda: self.ledger['releases'].append(copy.deepcopy(self.ledger['releases'][0])),
            lambda: self.build.update(id='changed-build'),
            lambda: self.build['attributes'].update(version='10204'),
            lambda: self.build['attributes'].update(expired=True),
            lambda: self.build['attributes'].update(processingState='PROCESSING'),
            lambda: self.detail['attributes'].update(externalBuildState='READY_FOR_BETA_SUBMISSION'),
            lambda: self.detail['attributes'].update(internalBuildState='PROCESSING'),
            lambda: self.review['attributes'].update(betaReviewState='IN_REVIEW'),
            lambda: self.members.remove('external-b'),
            lambda: self.reverse_members.remove('external-b'),
            lambda: setattr(self, 'marketing', '8.0.0'),
        ]
        baseline = copy.deepcopy((self.state, self.ledger, self.build, self.detail, self.review,
                                  self.members, self.reverse_members, self.marketing))
        for index, change in enumerate(changes):
            with self.subTest(case=index):
                (self.state, self.ledger, self.build, self.detail, self.review,
                 self.members, self.reverse_members, self.marketing) = copy.deepcopy(baseline)
                change()
                self.persist()
                with self.assertRaises(ValueError):
                    release.source_preflight(self.root, '1.2.3', ('android',))

    def test_no_review_record_is_accepted_only_with_actual_external_availability(self):
        self.review = None
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('android',)), self.sha)
        self.detail['attributes']['externalBuildState'] = 'IN_REVIEW'
        with self.assertRaises(ValueError):
            release.source_preflight(self.root, '1.2.3', ('android',))

    def test_frozen_workset_and_required_platforms_cannot_be_changed(self):
        original = copy.deepcopy(self.batch)
        changes = [
            lambda: self.batch['finalized_workset'].update(source_sha='wrong'),
            lambda: self.batch['finalized_workset'].update(issues=[2]),
            lambda: self.batch['finalized_workset'].update(platforms_to_release=['android']),
            lambda: self.batch['finalized_workset'].update(active_issue=2),
            lambda: self.batch.update(ios='waiting_beta_review'),
            lambda: self.batch.update(issues=[]),
        ]
        for index, change in enumerate(changes):
            with self.subTest(case=index):
                self.batch = copy.deepcopy(original)
                self.delivery['pending_new_batch'] = self.batch
                change()
                self.persist()
                with self.assertRaises(ValueError):
                    release.source_preflight(self.root, '1.2.3', ('android',))
        self.delivery['pending_new_batch'] = original
        self.persist()
        with self.assertRaises(ValueError):
            release.source_preflight(self.root, '1.2.3', ('server',))

    def test_unpublished_batch_cannot_use_an_ios_public_flag_to_skip_new_batch_audit(self):
        self.batch['version_locked'] = False
        self.persist()
        with self.assertRaisesRegex(ValueError, 'Live batch audit'):
            release.source_preflight(self.root, '1.2.3', ('android',))

    def test_github_tag_conflict_wins_over_ios_public_receipt(self):
        (self.root / 'other.txt').write_text('other source')
        self.git('add', 'other.txt')
        self.git('commit', '-m', 'Conflicting source')
        self.git('tag', 'v1.2.3')
        self.git('push', 'origin', 'refs/tags/v1.2.3')
        self.git('checkout', '--detach', self.sha)
        with self.assertRaisesRegex(ValueError, 'frozen at another source'):
            release.source_preflight(self.root, '1.2.3', ('android',))

    def test_github_first_tag_allows_ios_without_ios_first_proof(self):
        self.git('tag', 'v1.2.3')
        self.git('push', 'origin', 'refs/tags/v1.2.3')
        with patch.object(release_ios, 'IOSRelease', side_effect=AssertionError('not iOS first')):
            self.assertEqual(release.source_preflight(self.root, '1.2.3', ('ios',)), self.sha)

    def test_existing_github_tag_does_not_bootstrap_apple_proof_during_installer_execution(self):
        self.git('tag', 'v1.2.3')
        self.git('push', 'origin', 'refs/tags/v1.2.3')
        with patch.object(release_ios, 'load_client', side_effect=AssertionError('No Apple tools needed')), \
             patch.object(release, 'github_platform', side_effect=RuntimeError('offline upload failed')), \
             patch.object(release, 'release_notes'):
            self.assertEqual(release.execute(self.root, '1.2.3', 'android', self.state_path), 1)
        self.assertNotIn('first_publication', release.read_json(self.state_path))

    def test_public_tag_cannot_hide_a_conflicting_accepted_android_batch(self):
        # A real public tag bypasses new-batch allocation checks. The Android
        # receipt must still be checked rather than disappearing in SHA lookup.
        self.git('tag', 'v1.2.3')
        self.git('push', 'origin', 'refs/tags/v1.2.3')
        with (self.root / '.git/info/exclude').open('a') as stream:
            stream.write('\nbin/\n')
        asset = self.root / 'bin/zwai-1.2.3-android.apk'
        asset.parent.mkdir()
        asset.write_bytes(b'accepted Android installer fixture')
        digest = release.sha256(asset)
        self.command_patch.stop()
        original_run = release.run

        def command(args, **kwargs):
            self.calls.append(args)
            if args[0] == 'gh':
                if args[1] == 'repo':
                    return 'example/app'
                if args[1] == 'api':
                    return json.dumps([[{'tag_name': 'v1.2.3', 'assets': []}]])
                if args[1:3] == ['release', 'view']:
                    return json.dumps({'body': ''})
                if args[1:3] == ['release', 'edit']:
                    return ''
                self.fail(f'Unexpected external command: {args}')
            if args[0] == 'make':
                asset.write_bytes(b'unaccepted rebuild')
                return ''
            if args[0] == 'go':
                return ''
            return original_run(args, **kwargs)

        baseline = copy.deepcopy(self.state)
        for receipt_source in ('e' * 40, self.sha, None):
            with self.subTest(receipt_source=receipt_source):
                self.state = copy.deepcopy(baseline)
                self.batch.update(source_sha='e' * 40, android='ready_for_publication')
                self.batch['release_android_validation'] = {
                    'status': 'passed', 'version': '1.2.3',
                    'source_sha': receipt_source, 'asset_name': asset.name,
                    'asset_sha256': digest,
                }
                if receipt_source is None:
                    del self.batch['release_android_validation']
                self.delivery['issues'] = {'1': {'android': 'pending'}}
                self.persist()
                self.calls.clear()
                with patch.object(release, 'run', side_effect=command):
                    self.assertEqual(release.source_preflight(self.root, '1.2.3',
                        ('android',)), self.sha)
                    self.assertEqual(release.execute(self.root, '1.2.3', 'android',
                        self.state_path), 1)
                self.assertFalse(any(args[0] in ('make', 'go') for args in self.calls))
                self.assertIn('accepted Android package',
                    release.read_json(self.state_path)['android']['last_error'])

    def test_pending_native_mac_remains_blocked_after_source_recovery(self):
        self.delivery['issues'] = {'1': {'macos': 'pending_native_window_acceptance'}}
        self.persist()
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('macos',)), self.sha)
        with self.assertRaisesRegex(ValueError, 'native acceptance'):
            release.platform_acceptance_gate('1.2.3', 'macos')

    def retain_first_public_proof(self):
        proof = release_ios.IOSRelease(self.root, '1.2.3', self.sha,
            copy.deepcopy(self.state)).verify_publication()
        self.state['first_publication'] = proof
        self.persist()
        self.calls.clear()
        return proof

    def test_retained_first_public_proof_survives_expiry_and_unavailable_apple_tools(self):
        self.retain_first_public_proof()
        self.build['attributes']['expired'] = True
        self.members.clear()
        with patch.object(release_ios, 'load_client', side_effect=AssertionError('No Apple tools needed')):
            for platform in ('macos', 'android'):
                self.assertEqual(release.source_preflight(self.root, '1.2.3', (platform,)), self.sha)
        self.assertFalse(any(call[0] == 'GET' for call in self.calls))

    def test_incomplete_or_conflicting_first_public_proofs_are_never_replaced_by_live_queries(self):
        proof = self.retain_first_public_proof()
        invalid = [None, {}, {**proof, 'source_sha': 'wrong'}, {**proof, 'build_id': 'other'},
                   {**proof, 'available_at': 'not-a-date'}, {**proof, 'available_at': '2026-01-01'},
                   {**proof, 'availability': {}},
                   {**proof, 'availability': {**proof['availability'], 'expired': True}},
                   {**proof, 'availability': {**proof['availability'], 'groups_reverse': []}},
                   {**proof, 'availability': {**proof['availability'], 'review': {
                       'attributes': {'betaReviewState': 'IN_REVIEW'}}}}]
        for index, value in enumerate(invalid):
            with self.subTest(case=index):
                self.state['first_publication'] = value
                self.persist()
                with patch.object(release_ios, 'load_client', side_effect=AssertionError('No fallback')), \
                     self.assertRaises(ValueError):
                    release.source_preflight(self.root, '1.2.3', ('android',))

    def test_legacy_check_is_read_only_and_actual_recovery_persists_proof_before_failed_upload(self):
        before = self.state_path.read_bytes()
        self.assertEqual(release.source_preflight(self.root, '1.2.3', ('android',)), self.sha)
        self.assertEqual(self.state_path.read_bytes(), before)
        with patch.object(release, 'github_platform', side_effect=RuntimeError('offline upload failed')), \
             patch.object(release, 'release_notes'):
            self.assertEqual(release.execute(self.root, '1.2.3', 'android', self.state_path), 1)
        saved = release.read_json(self.state_path)
        proof = saved['first_publication']
        self.assertEqual(proof['source_sha'], self.sha)
        self.assertEqual(proof['build_number'], '10203')
        self.assertEqual(saved['android']['status'], 'failed')
        self.assertEqual(saved['ios']['status'], 'published')
        self.assertEqual(release_ios.verify_public_receipt('1.2.3', self.sha, saved, self.ledger), proof)

    def test_ios_publisher_saves_first_public_proof_but_still_checks_current_installability(self):
        ios = release_ios.IOSRelease(self.root, '1.2.3', self.sha, self.state)
        with patch.object(release_ios, 'run', return_value=''), patch.object(ios, 'complete_delivery'):
            self.assertEqual(ios.publish()['status'], 'published')
        saved = release.read_json(self.state_path)
        self.assertIn('first_publication', saved)
        # Historical proof authorizes the source; it cannot make iOS expired builds installable.
        self.build['attributes']['expired'] = True
        with patch.object(release_ios, 'run', return_value=''), patch.object(ios, 'quota', return_value=False), \
             self.assertRaisesRegex(ValueError, 'expired'):
            ios.publish_once()

    def test_first_available_observation_freezes_delivery_without_another_network_round(self):
        self.batch.update(version_locked=False, ios='pending')
        self.delivery['issues'] = {'1': {'android': 'pending', 'macos': 'pending',
                                      'ios': 'pending', 'server': 'not_required'}}
        self.state['ios']['status'] = 'signed'
        self.ledger['releases'][0]['successfully_published'] = False
        self.persist()
        ios = release_ios.IOSRelease(self.root, '1.2.3', self.sha, self.state)
        observed = False
        original_observe, original_all, original_request = ios.observe, self.all, self.request

        def observe(*args, **kwargs):
            nonlocal observed
            result = original_observe(*args, **kwargs)
            observed = bool(result)
            return result

        def all_rows(*args):
            self.assertFalse(observed, 'Publication must not add a network round after strict success')
            return original_all(*args)

        def request(*args):
            self.assertFalse(observed, 'Publication must not add a network round after strict success')
            return original_request(*args)

        with patch.object(release_ios, 'run', return_value=''), patch.object(ios, 'observe', side_effect=observe), \
             patch.object(self, 'all', side_effect=all_rows), patch.object(self, 'request', side_effect=request):
            self.assertEqual(ios.publish()['status'], 'published')
        self.assertTrue(release.read_json(self.delivery_path)['pending_new_batch']['version_locked'])
        saved = release.read_json(self.state_path)
        self.assertIn('first_publication', saved)
        self.assertEqual(release_ios.verify_public_receipt('1.2.3', self.sha, saved,
            release.read_json(self.ledger_path)), saved['first_publication'])

    def test_second_publisher_is_rejected_before_any_source_or_upload_command(self):
        self.state_path.parent.mkdir(exist_ok=True)
        with (self.state_path.parent / 'release.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = subprocess.run([sys.executable, str(release.ROOT / 'tools/release.py'),
                '--version', '1.2.3', '--platform', 'android', '--source-dir', str(self.root)],
                text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 1)
        self.assertIn('Another release is active', result.stderr)
        self.assertEqual(self.calls, [])


if __name__ == '__main__':
    unittest.main()
