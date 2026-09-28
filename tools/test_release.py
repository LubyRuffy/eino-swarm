import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


class MakeEntryTests(unittest.TestCase):
    def test_release_runs_all_platforms_and_exposes_ios_recovery(self):
        root = Path(__file__).resolve().parents[1]
        makefile = (root / 'Makefile').read_text()
        self.assertIn('python3 tools/release.py', makefile)
        self.assertIn('mobile-ios-release:', makefile)


import os
import sys
import plistlib
import subprocess
import zipfile
from datetime import datetime, timedelta
from types import SimpleNamespace
sys.path.insert(0, str(Path(__file__).parent))
import release
import release_ios

class DriverTests(unittest.TestCase):
    def test_python_import_cache_does_not_dirty_the_release_source(self):
        result = subprocess.run(['git', 'check-ignore', '--no-index', '-q',
            'tools/__pycache__/release.cpython-314.pyc'], cwd=release.ROOT)
        self.assertEqual(result.returncode, 0)

    def test_independent_failures_are_saved_and_ios_still_runs(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'state.json'
            with patch.object(release, 'release_notes'), patch.object(release, 'source_preflight', return_value='sha'), \
                 patch.object(release, 'github_platform', side_effect=[RuntimeError('mac failed'), {'status': 'published'}]), \
                 patch.object(release_ios, 'IOSRelease') as ios:
                ios.return_value.publish.return_value = {'status': 'waiting_beta_review'}
                self.assertEqual(release.execute(Path(directory), '1.2.3', 'all', path), 1)
                data = json.loads(path.read_text())
                self.assertEqual(data['macos']['last_error'], 'mac failed')
                self.assertEqual(data['android']['status'], 'published')
                self.assertEqual(data['ios']['status'], 'waiting_beta_review')
                ios.return_value.publish.assert_called_once_with(check=False)

    def test_check_does_not_save_platform_state(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'state.json'
            with patch.object(release, 'source_preflight', return_value='sha'), patch.object(release, 'run', return_value=''):
                self.assertEqual(release.execute(Path(directory), '1.2.3', 'android', path, True), 0)
                self.assertFalse(path.exists())
            release.write_json(path, {'version': '1.2.3', 'source_sha': 'other'})
            with patch.object(release, 'source_preflight', return_value='sha'), self.assertRaisesRegex(ValueError, 'another'):
                release.execute(Path(directory), '1.2.3', 'android', path)

    def test_version_mapping_rejects_collisions_and_non_release_versions(self):
        self.assertEqual(release.version_code('0.1.20'), '120')
        self.assertEqual(release.version_code('0.2.0'), '200')
        for value in ['1.2.3-dirty', '1.2', '0.1.100', '0.100.0', '0.0.0', '9999999.0.0']:
            with self.assertRaises(ValueError):
                release.version_code(value)

    def test_source_gate_checks_clean_pushed_main_package_repo_and_tag(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'mobile').mkdir()
            release.write_json(root / 'mobile/package.json', {'version': '1.2.3'})
            (root / 'go.mod').write_text('module github.com/example/app\n')
            def command(args, **kwargs):
                if args[1:3] == ['status', '--porcelain']: return ''
                if args[1] == 'rev-parse': return 'sha'
                if args[0] == 'gh' and args[1] == 'repo': return 'example/app'
                if args[1] == 'ls-remote': return 'sha\trefs/tags/v1.2.3'
                return ''
            with patch.object(release, 'run', side_effect=command):
                self.assertEqual(release.source_preflight(root, '1.2.3'), 'sha')
                with self.assertRaisesRegex(ValueError, 'package'):
                    release.source_preflight(root, '1.2.4')
            def dirty(args, **kwargs):
                return ' M tracked' if args[1:3] == ['status', '--porcelain'] else command(args, **kwargs)
            with patch.object(release, 'run', side_effect=dirty), self.assertRaisesRegex(ValueError, 'clean'):
                release.source_preflight(root, '1.2.3')
            def wrong_tag(args, **kwargs):
                return 'other\tref' if args[1] == 'ls-remote' else command(args, **kwargs)
            with patch.object(release, 'run', side_effect=wrong_tag), self.assertRaisesRegex(ValueError, 'frozen'):
                release.source_preflight(root, '1.2.3')

    def test_subprocess_failure_does_not_echo_credentials(self):
        with self.assertRaisesRegex(RuntimeError, 'exit 1') as error:
            release.run([sys.executable, '-c', 'import sys;print("secret");sys.exit(1)'])
        self.assertNotIn('secret', str(error.exception))
        self.assertEqual(release.run([sys.executable, '-c', 'print("ok")']), 'ok')


class Apple:
    def __init__(self):
        self.builds = []
        self.uploads = []
        self.groups = [{'id': 'internal', 'attributes': {'isInternalGroup': True}},
                       {'id': 'external', 'attributes': {'isInternalGroup': False}}]
        self.members = {'internal': [], 'external': []}
        self.ready = False
        self.review_state = 'WAITING_FOR_REVIEW'
    def all(self, path):
        if '/buildUploads' in path: return self.uploads
        if '/betaGroups' in path: return self.groups
        if path.startswith('betaGroups/'): return self.members[path.split('/')[1]]
        return self.builds
    def request(self, method, path, body=None):
        if path.startswith('apps/'):
            return {'data': {'attributes': {'bundleId': 'com.example.app'}}}
        if path.endswith('/preReleaseVersion'):
            return {'data': {'attributes': {'version': '0.1.20'}}}
        if path.endswith('/buildBetaDetail'):
            return {'data': {'attributes': {'internalBuildState': 'READY_FOR_BETA_TESTING',
                'externalBuildState': 'IN_BETA_TESTING' if self.ready else 'READY_FOR_BETA_SUBMISSION'}}}
        return {'data': {'attributes': {'betaReviewState': 'APPROVED' if self.ready else self.review_state}}}


class IosTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = patch.dict(os.environ, {'ZWAI_HOME': str(self.root), 'IOS_RELEASE_CONFIG': str(self.root / 'config.json')})
        self.env.start(); self.addCleanup(self.env.stop)
        self.apple = Apple()
        self.actions = []
        self.module = SimpleNamespace(APP_ID='app', Client=lambda: self.apple,
            set_compliance=lambda *args: self.actions.append('compliance'),
            add_group=lambda *args: self.actions.append('group'),
            submit_review=lambda *args: self.actions.append('review'))
        self.config = {'signing_dir': str(self.root), 'testflight_cli': 'private.py',
            'group_ids': ['internal', 'external'], 'processing_timeout_seconds': 0,
            'compliance': {'source_sha': 'sha', 'build_number': '120', 'confirmed_by': 'authorized reviewer', 'uses_non_exempt': False}}
        self.save_config()
        self.loader = patch.object(release_ios, 'load_client', return_value=self.module)
        self.loader.start(); self.addCleanup(self.loader.stop)
        source = patch.object(release_ios, 'source_preflight', return_value='sha')
        source.start(); self.addCleanup(source.stop)
        self.runner = patch.object(release_ios, 'run', return_value='')
        self.run = self.runner.start(); self.addCleanup(self.runner.stop)
        release.write_json(self.root / 'exportOptions.plist.json', {})
        (self.root / 'exportOptions.plist').write_bytes(plistlib.dumps({'method': 'app-store-connect', 'teamID': 'TEAM',
            'provisioningProfiles': {'com.example.app': 'profile'}, 'signingCertificate': 'Apple Distribution'}))
        (self.root / 'AppStoreConnectAPI.credentials').write_text('APP_STORE_CONNECT_KEY_ID=key\nAPP_STORE_CONNECT_ISSUER_ID=issuer\n')
        self.state = {'version': '0.1.20', 'source_sha': 'sha'}

    def save_config(self):
        release.write_json(self.root / 'config.json', self.config)
    def ios(self):
        return release_ios.IOSRelease(self.root, '0.1.20', 'sha', self.state)
    def build(self, number='120', state='VALID'):
        return {'id': 'build' + number, 'attributes': {'version': number, 'processingState': state}}
    def ipa(self, version='0.1.20', flag=None):
        path = self.root / 'App.ipa'
        info = {'CFBundleShortVersionString': version, 'CFBundleVersion': '120', 'CFBundleIdentifier': 'com.example.app'}
        if flag is not None: info['ITSAppUsesNonExemptEncryption'] = flag
        with zipfile.ZipFile(path, 'w') as archive:
            archive.writestr('Payload/App.app/Info.plist', plistlib.dumps(info))
            archive.writestr('Payload/App.app/embedded.mobileprovision', 'fixture')
        return path
    def receipt(self):
        self.state['ios'] = {'source_sha': 'sha'}
        self.apple.builds = [self.build()]

    def test_missing_classification_refuses_archive_and_upload(self):
        self.config.pop('compliance'); self.save_config()
        ios = self.ios()
        with patch.object(ios, 'build_ipa') as build, self.assertRaisesRegex(ValueError, 'declaration'):
            ios.publish_once()
        build.assert_not_called()
        self.assertFalse(any('altool' in args[0] for args in self.run.call_args_list))
        self.assertFalse(self.actions)

    def test_old_false_or_wrong_source_cannot_classify_this_build(self):
        self.config['compliance']['source_sha'] = 'old'; self.save_config()
        with self.assertRaisesRegex(ValueError, 'declaration'):
            self.ios().declaration()
        self.assertTrue(self.ios().declaration({'ITSAppUsesNonExemptEncryption': True}))

    def test_accepted_upload_never_reuploads(self):
        self.apple.uploads = [{'id': 'accepted', 'attributes': {'cfBundleVersion': '120', 'state': {'state': 'COMPLETE'}}}]
        ios = self.ios()
        with patch.object(ios, 'build_ipa') as build:
            self.assertEqual(ios.publish()['status'], 'waiting_processing')
        build.assert_not_called()
        self.assertFalse(any('altool' in args[0] for args in self.run.call_args_list))

    def test_ambiguous_upload_attempt_never_reuploads(self):
        self.state['ios'] = {'status': 'upload_attempted', 'source_sha': 'sha'}
        self.assertEqual(self.ios().publish()['status'], 'waiting_upload_confirmation')

    def test_daily_limit_uses_availability_date_not_upload_date(self):
        ios = self.ios()
        release.write_json(ios.ledger_path, {'releases': [{'app_id': 'app', 'build_number': '115',
            'successfully_published': True, 'success_beijing_date': ios.day}]})
        self.assertEqual(self.ios().publish()['status'], 'waiting_daily_limit')
        data = release.read_json(ios.ledger_path); data['releases'][0]['success_beijing_date'] = '2000-01-01'
        release.write_json(ios.ledger_path, data)
        with patch.object(release_ios.IOSRelease, 'build_ipa', return_value=self.ipa()), \
             patch.object(release_ios.IOSRelease, 'verify_ipa', return_value=False):
            self.assertEqual(self.ios().publish()['status'], 'waiting_upload_confirmation')

    def test_daily_wait_retains_signed_candidate_for_next_day(self):
        ipa = self.ipa(flag=False)
        self.state['ios'] = {'status': 'signed', 'source_sha': 'sha',
            'ipa': str(ipa), 'ipa_sha256': release.sha256(ipa)}
        ios = self.ios()
        release.write_json(ios.ledger_path, {'releases': [{'app_id': 'app', 'build_number': '115',
            'successfully_published': True, 'success_beijing_date': ios.day}]})
        result = self.ios().publish()
        self.assertEqual(result['status'], 'waiting_daily_limit')
        self.assertEqual(result['ipa'], str(ipa))
        self.assertEqual(result['ipa_sha256'], release.sha256(ipa))

    def test_unknown_remote_review_prevents_two_candidates_becoming_available(self):
        self.apple.builds = [self.build('119')]
        ios = self.ios()
        with patch.object(ios, 'build_ipa') as archive, self.assertRaisesRegex(ValueError, 'pending'):
            ios.publish_once()
        archive.assert_not_called()
        self.assertFalse(self.actions)

    def test_current_build_becomes_available_and_alias_delivery_is_retained(self):
        self.receipt(); self.apple.ready = True
        for group in self.apple.members: self.apple.members[group] = self.apple.builds
        ios = self.ios()
        release.write_json(ios.ledger_path, {'releases': [], 'pending_changes': [{'issue': 1, 'batch_source_sha': 'sha', 'batch_version': '0.1.20'}],
            'delivery_aliases': {'2': {'source_sha': 'sha', 'status': 'pending'}}})
        delivery = {'pending_new_batch': {'version': '0.1.20', 'source_sha': 'sha', 'issues': [1]},
            'issues': {'1': {'android': 'published', 'macos': 'published', 'server': 'not_required'},
                       '2': {'android': 'published_previous_release', 'macos': 'not_required', 'server': 'not_required'}},
            'delivery_aliases': {'2': {'source_sha': 'sha'}}}
        path = self.root / 'issue-automation/delivery-state.json'; release.write_json(path, delivery)
        result = self.ios().publish()
        self.assertEqual(result['status'], 'published')
        self.assertFalse(self.actions)
        self.assertTrue(release.read_json(path)['issues']['2']['delivery_complete'])
        self.assertEqual(release.read_json(ios.ledger_path)['pending_changes'][0]['status'], 'published')

    def test_ios_completion_keeps_mac_only_issue_out_of_ios_delivery(self):
        ios = self.ios()
        release.write_json(ios.ledger_path, {'pending_changes': [
            {'issue': 44, 'batch_source_sha': 'sha', 'batch_version': '0.1.20'}]})
        delivery = {'pending_new_batch': {'version': '0.1.20', 'source_sha': 'sha', 'issues': [43, 44]},
            'issues': {
                '43': {'macos': 'published', 'android': 'not_required_by_behavior_change',
                       'ios': 'not_required_by_behavior_change', 'server': 'not_required'},
                '44': {'macos': 'not_required_by_behavior_change', 'android': 'published',
                       'ios': 'pending_next_qualified_batch', 'server': 'not_required'}}}
        path = self.root / 'issue-automation/delivery-state.json'
        release.write_json(path, delivery)
        ios.complete_delivery()
        saved = release.read_json(path)
        rows = saved['issues']
        self.assertTrue(saved['pending_new_batch']['version_locked'])
        self.assertTrue(all(row['batch_version'] == '0.1.20' for row in rows.values()))
        self.assertEqual(rows['43']['ios'], 'not_required_by_behavior_change')
        self.assertEqual(rows['44']['ios'], 'published')
        self.assertTrue(rows['44']['delivery_complete'])

    def test_review_wait_is_not_publication(self):
        self.receipt()
        result = self.ios().publish()
        self.assertEqual(result['status'], 'waiting_beta_review')
        self.assertEqual(self.actions, ['compliance', 'group', 'group', 'review'])
        self.assertFalse(release.read_json(self.root / 'ios-signing/testflight-releases.json', {}).get('releases'))

    def test_check_reads_remote_but_never_changes_ledger(self):
        ios = self.ios()
        self.assertEqual(ios.publish(check=True)['status'], 'preflight_passed')
        self.assertFalse(ios.ledger_path.exists()); self.assertFalse(self.actions)

    def test_group_processing_upload_and_receipt_failures(self):
        self.config['group_ids'] = ['missing']; self.save_config()
        with self.assertRaisesRegex(ValueError, 'groups'): self.ios().publish_once()
        self.config['group_ids'] = ['internal']; self.save_config()
        with self.assertRaisesRegex(ValueError, 'internal and external'): self.ios().publish_once()
        self.config['group_ids'] = ['internal', 'external']; self.save_config()
        self.apple.builds = [self.build()]
        with self.assertRaisesRegex(ValueError, 'source receipt'): self.ios().publish_once()
        self.receipt(); self.apple.builds[0]['attributes']['expired'] = True
        with self.assertRaisesRegex(ValueError, 'expired'): self.ios().publish_once()
        self.apple.builds[0]['attributes']['expired'] = False
        self.apple.builds[0]['attributes']['processingState'] = 'FAILED'
        with self.assertRaisesRegex(ValueError, 'processing failed'): self.ios().publish_once()
        self.apple.builds[0]['attributes']['processingState'] = 'PROCESSING'
        self.assertEqual(self.ios().publish_once()['status'], 'waiting_processing')
        self.apple.builds = []; self.apple.uploads = [{'id': 'u', 'attributes': {'cfBundleVersion': '120', 'state': {'state': 'FAILED'}}}]
        with self.assertRaisesRegex(ValueError, 'upload processing failed'): self.ios().publish_once()
        self.apple.uploads = []; self.apple.builds = [self.build('121')]
        self.apple.review_state = 'REJECTED'
        with self.assertRaisesRegex(ValueError, 'increase'): self.ios().publish_once()

    def test_ipa_validates_version_distribution_identity_profile_and_declaration(self):
        path = self.ipa(flag=False)
        profile = {'Entitlements': {'application-identifier': 'TEAM.com.example.app', 'get-task-allow': False},
            'ExpirationDate': datetime.now() + timedelta(days=1)}
        def command(args, **kwargs):
            if args[:2] == ['security', 'cms']: return plistlib.dumps(profile).decode()
            if args[:2] == ['codesign', '-dvv']: return 'Authority=Apple Distribution: Owner\nTeamIdentifier=TEAM'
            return ''
        self.run.side_effect = command
        self.assertFalse(self.ios().verify_ipa(path))
        profile['Entitlements']['get-task-allow'] = True
        with self.assertRaisesRegex(ValueError, 'App Store'): self.ios().verify_ipa(path)
        profile['Entitlements']['get-task-allow'] = False
        profile['ExpirationDate'] = datetime.now() - timedelta(days=1)
        with self.assertRaisesRegex(ValueError, 'expired'): self.ios().verify_ipa(path)
        self.ipa(version='other')
        with self.assertRaisesRegex(ValueError, 'version/build'): self.ios().verify_ipa(path)

    def test_archive_uses_shared_version_export_settings_and_no_source_version_rewrite(self):
        ios = self.ios()
        path = ios.build_ipa()
        self.assertTrue(str(path).endswith('-export/App.ipa'))
        archive = next(c for c in self.run.call_args_list if 'archive' in c.args[0])
        self.assertIn('MARKETING_VERSION=0.1.20', archive.args[0])
        self.assertIn('CURRENT_PROJECT_VERSION=120', archive.args[0])
        export = next(c for c in self.run.call_args_list if '-exportArchive' in c.args[0])
        self.assertTrue(export.kwargs['env']['PATH'].startswith('/usr/bin:'))
        Path(str(path).removesuffix('-export/App.ipa') + '.xcarchive').mkdir(parents=True)
        with self.assertRaisesRegex(ValueError, 'refusing rebuild'): ios.build_ipa()

    def test_upload_checkpoints_signature_and_rechecks_remote_before_mutation(self):
        ios = self.ios(); ipa = self.ipa(flag=False)
        with patch.object(ios, 'build_ipa', return_value=ipa), patch.object(ios, 'verify_ipa', return_value=False):
            result = ios.publish()
        self.assertEqual(result['status'], 'waiting_upload_confirmation')
        upload = [c for c in self.run.call_args_list if 'altool' in c.args[0]]
        self.assertEqual(len(upload), 1)
        self.assertEqual(ios.state['ios']['ipa_sha256'], release.sha256(ipa))
        self.assertEqual(len(release.read_json(ios.ledger_path)['releases']), 1)
        self.assertEqual(ios.publish()['status'], 'waiting_upload_confirmation')
        self.assertEqual(len([c for c in self.run.call_args_list if 'altool' in c.args[0]]), 1)

class MoreGateTests(unittest.TestCase):
    def test_mixed_platform_batch_preserves_per_issue_delivery_scope(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state = root / 'delivery.json'
            release.write_json(state, {'pending_new_batch': {'version': '0.1.21', 'source_sha': 'sha',
                'issues': [43, 44, 45]}, 'issues': {
                '43': {'macos': 'pending_next_qualified_batch', 'android': 'not_required_by_behavior_change',
                       'ios': 'not_required_by_behavior_change', 'server': 'not_required'},
                '44': {'macos': 'not_required_by_behavior_change', 'android': 'pending_next_qualified_batch',
                       'ios': 'pending_next_qualified_batch', 'server': 'not_required'},
                '45': {'macos': 'not_required_by_behavior_change', 'android': 'pending_next_qualified_batch',
                       'ios': 'pending_next_qualified_batch', 'server': 'not_required'}}})
            with patch.dict(os.environ, {'DELIVERY_STATE': str(state)}):
                release.record_installer('0.1.21', 'sha', 'macos', {'status': 'published'})
                release.record_installer('0.1.21', 'sha', 'android', {'status': 'published'})
            saved = release.read_json(state)
            rows = saved['issues']
            self.assertTrue(saved['pending_new_batch']['version_locked'])
            self.assertTrue(all(row['batch_version'] == '0.1.21' and row['release_source_sha'] == 'sha'
                                for row in rows.values()))
            self.assertEqual(rows['43']['macos'], 'published')
            self.assertEqual(rows['43']['android'], 'not_required_by_behavior_change')
            self.assertTrue(rows['43']['delivery_complete'])
            for number in ('44', '45'):
                self.assertEqual(rows[number]['macos'], 'not_required_by_behavior_change')
                self.assertEqual(rows[number]['android'], 'published')
                self.assertEqual(rows[number]['ios'], 'pending_next_qualified_batch')
                self.assertFalse(rows[number]['delivery_complete'])

    def test_qualified_batch_requires_live_audit_and_new_numeric_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root/'mobile').mkdir()
            release.write_json(root/'mobile/package.json', {'version': '1.2.3'})
            (root/'go.mod').write_text('module github.com/example/app\n')
            statepath = root/'delivery.json'
            release.write_json(statepath, {'pending_new_batch': {'version': '1.2.3', 'source_sha': 'sha',
                'issues': [1,2,3], 'qualified_for_new_release': True}})
            remote = [[{'tag_name': 'v1.2.2'}]]
            report = {'allocate_new_version': True, 'main_sha': 'sha', 'issues': [1,2,3]}
            def command(args, **kwargs):
                if args[0] == sys.executable: return json.dumps(report)
                if args[0] == 'gh':
                    return 'example/app' if args[1] == 'repo' else json.dumps(remote)
                return 'sha' if args[1] == 'rev-parse' else ''
            with patch.dict(os.environ, {'DELIVERY_STATE':str(statepath)}), patch.object(release,'run',side_effect=command):
                self.assertEqual(release.source_preflight(root,'1.2.3'),'sha')
                remote[0][0]['tag_name'] = 'v1.2.3'
                with self.assertRaisesRegex(ValueError,'strictly greater'): release.source_preflight(root,'1.2.3')
                report['allocate_new_version'] = False
                with self.assertRaisesRegex(ValueError,'Live batch audit'): release.source_preflight(root,'1.2.3')
                release.write_json(statepath, {})
                with self.assertRaisesRegex(ValueError,'three distinct'): release.source_preflight(root,'1.2.3')

    def test_installer_remote_retry_never_builds_and_rejects_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'bin').mkdir()
            apk=root/'bin/zwai-1.2.3-android.apk';apk.write_bytes(b'original')
            digest='sha256:'+release.sha256(apk)
            remote=[[{'tag_name':'v1.2.3','assets':[{'name':apk.name,'digest':digest}]}]]
            calls=[]
            def command(args,**kwargs):
                calls.append(args)
                if args[0]=='gh': return json.dumps(remote)
                return ''
            with patch.object(release,'run',side_effect=command), patch.object(release,'source_preflight',return_value='sha'):
                result=release.github_platform(root,'1.2.3','sha','android',{})
                self.assertEqual(result['status'],'published')
                self.assertFalse(any(c[0]=='make' for c in calls))
                apk.write_bytes(b'changed')
                with self.assertRaisesRegex(ValueError,'differs'): release.github_platform(root,'1.2.3','sha','android',{})
                remote[0][0]['assets'][0]['digest']=None
                with self.assertRaisesRegex(ValueError,'digest'): release.github_platform(root,'1.2.3','sha','android',{})

    def test_new_installer_build_failures_and_receipts(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'bin').mkdir()
            artifact=root/'bin/zwai-1.2.3-darwin-arm64.zip'
            dirty=False
            calls=[]
            def command(args,**kwargs):
                calls.append(args)
                if args[0]=='uname': return 'arm64'
                if args[:2]==['gh','api']: return '[]'
                if args[0]=='make': artifact.write_bytes(b'zip')
                if args[0]=='git': return 'dirty' if dirty else ''
                return ''
            delivery=root/'delivery.json'
            release.write_json(delivery, {'pending_new_batch':{'version':'1.2.3','source_sha':'sha','issues':[1]},'issues':{'1':{}}})
            with patch.object(release,'run',side_effect=command),patch.object(release,'source_preflight',return_value='sha'),patch.dict(os.environ,{'DELIVERY_STATE':str(delivery)}):
                result=release.github_platform(root,'1.2.3','sha','macos',{})
                self.assertTrue(any(c[:2]==['make','desktop-release'] for c in calls))
                release.record_installer('1.2.3','sha','macos',result)
                self.assertEqual(release.read_json(delivery)['issues']['1']['macos'],'published')
                dirty=True
                with self.assertRaisesRegex(ValueError,'changed tracked'): release.github_platform(root,'1.2.3','sha','macos',{})

    def test_cli_preflight_and_lock_refuse_overlap(self):
        with tempfile.TemporaryDirectory() as directory,patch.dict(os.environ,{'ZWAI_HOME':directory}),\
             patch.object(release,'execute',return_value=0) as action,patch.object(sys,'argv',['release','--version','1.2.3','--check']):
            self.assertEqual(release.main(),0)
            self.assertEqual(action.call_args.args[1:3],('1.2.3','all'))
            path=Path(directory)/'releases/release.lock'
            with path.open('a') as lock:
                release.fcntl.flock(lock,release.fcntl.LOCK_EX|release.fcntl.LOCK_NB)
                with self.assertRaisesRegex(ValueError,'Another release'): release.main()


class ReleaseNotesTests(unittest.TestCase):
    def test_partial_delivery_notes_preserve_existing_description(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            body='Original release information\n<!-- zwai-platform-delivery -->old<!-- /zwai-platform-delivery -->'
            with patch.object(release,'run',side_effect=[json.dumps({'body':body}),'']) as commands:
                release.release_notes(root,'1.2.3',{'android':{'status':'published'},'macos':{'status':'failed'},'ios':{'status':'waiting_beta_review'}})
            text=(root/'bin/release-notes.md').read_text()
            self.assertIn('Original release information',text)
            self.assertIn('macos: failed',text)
            self.assertIn('ios: waiting_beta_review',text)
            self.assertNotIn('>old<',text)
            self.assertIn('--notes-file',commands.call_args.args[0])

if __name__ == '__main__':
    unittest.main()
