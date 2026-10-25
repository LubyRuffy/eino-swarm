"""Explicit urgent delivery changes timing policy, never publication identity."""
import copy
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release
import release_ios


class UrgentReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'mobile').mkdir()
        (self.root / 'go.mod').write_text('module github.com/example/app\n')
        release.write_json(self.root / 'mobile/package.json', {'version': '1.2.3'})
        self.ledger = self.root / 'delivery.json'
        self.delivery = {'active_issue': None,
            'issues': {'1': {'macos': 'pending', 'android': 'pending', 'ios': 'pending', 'server': 'not_required'}},
            'pending_new_batch': {
            'version': '1.2.3', 'source_sha': 'sha', 'issues': [1],
            'qualified_for_new_release': False, 'version_locked': False,
            'finalized_workset': {'source_sha': 'sha', 'issues': [1],
                'platforms_to_release': ['android', 'ios']}}}
        self.report = {'resume_allocated_version': False, 'main_sha': 'sha',
            'batch_version': '1.2.3', 'version_locked': False,
            'unallocated_issues': [1], 'platforms_to_release': ['android', 'ios']}
        self.dirty, self.main, self.tag, self.remote_version = '', 'sha', '', 'v1.2.2'
        environment = patch.dict(os.environ, {'DELIVERY_STATE': str(self.ledger),
                                              'ZWAI_HOME': str(self.root)})
        environment.start()
        self.addCleanup(environment.stop)
        runner = patch.object(release, 'run', side_effect=self.command)
        self.run = runner.start()
        self.addCleanup(runner.stop)
        self.save()

    def save(self):
        release.write_json(self.ledger, self.delivery)

    def command(self, args, **kwargs):
        if args[0] == sys.executable:
            return json.dumps(self.report)
        if args[:2] == ['gh', 'repo']:
            return 'example/app'
        if args[0] == 'gh':
            return json.dumps([[{'tag_name': self.remote_version}]])
        if args[1:3] == ['status', '--porcelain']:
            return self.dirty
        if args[1] == 'rev-parse':
            return self.main if args[-1] == 'origin/main' else 'sha'
        if args[1] == 'ls-remote':
            return self.tag
        return ''

    def preflight(self, reason=None):
        return release.source_preflight(self.root, '1.2.3', ('android', 'ios'),
                                        urgent_reason=reason)

    def test_explicit_reason_accepts_an_allocated_batch_below_aggregation_threshold(self):
        with self.assertRaisesRegex(ValueError, 'allocated batch'):
            self.preflight()
        self.assertEqual(self.preflight('urgent user-requested delivery'), 'sha')
        self.assertEqual(release.read_json(self.ledger), self.delivery)

    def test_blank_reason_is_rejected_before_external_checks(self):
        for reason in ('', ' \t\n'):
            with self.subTest(reason=reason), self.assertRaisesRegex(ValueError, 'reason'):
                self.preflight(reason)
        self.run.assert_not_called()

    def test_urgent_delivery_keeps_source_identity_and_complete_workset_gates(self):
        base_delivery, base_report = copy.deepcopy(self.delivery), copy.deepcopy(self.report)
        mutations = [
            lambda: setattr(self, 'dirty', ' M tracked'),
            lambda: setattr(self, 'main', 'other'),
            lambda: setattr(self, 'tag', 'other\trefs/tags/v1.2.3'),
            lambda: self.delivery['pending_new_batch'].update(source_sha='other'),
            lambda: self.delivery['pending_new_batch'].update(issues=[]),
            lambda: self.report.update(main_sha='other'),
            lambda: self.report.update(unallocated_issues=[1, 2]),
            lambda: self.report.update(platforms_to_release=['android']),
            lambda: self.report.update(batch_version='1.2.2'),
            lambda: self.report.update(version_locked=True),
            lambda: self.delivery.update(active_issue=67),
            lambda: self.delivery['pending_new_batch']['finalized_workset'].update(source_sha='other'),
            lambda: self.delivery['pending_new_batch']['finalized_workset'].update(issues=[2]),
            lambda: self.delivery['pending_new_batch']['finalized_workset'].update(platforms_to_release=['android']),
            lambda: self.delivery['pending_new_batch']['finalized_workset'].update(active_issue=67),
            lambda: setattr(self, 'remote_version', 'v1.2.3'),
        ]
        for number, mutate in enumerate(mutations):
            self.delivery, self.report = copy.deepcopy(base_delivery), copy.deepcopy(base_report)
            self.dirty, self.main, self.tag, self.remote_version = '', 'sha', '', 'v1.2.2'
            mutate()
            self.save()
            with self.subTest(gate=number), self.assertRaises(ValueError):
                self.preflight('urgent user-requested delivery')

    def test_driver_records_and_passes_the_current_reason_to_each_publisher(self):
        path = self.root / 'state.json'
        with patch.object(release, 'source_preflight', return_value='sha') as source, \
             patch.object(release, 'github_platform', return_value={'status': 'published'}) as github, \
             patch.object(release, 'release_notes'), patch.object(release_ios, 'IOSRelease') as ios:
            ios.return_value.publish.return_value = {'status': 'published'}
            self.assertEqual(release.execute(self.root, '1.2.3', 'all', path,
                                            urgent_reason='  urgent user request  '), 0)
            self.assertEqual(source.call_args.kwargs['urgent_reason'], 'urgent user request')
            self.assertTrue(all(call.kwargs['urgent_reason'] == 'urgent user request'
                                for call in github.call_args_list))
            self.assertEqual(ios.call_args.kwargs['urgent_reason'], 'urgent user request')
        self.assertEqual(release.read_json(path)['urgent_reason'], 'urgent user request')

    def test_installer_build_rechecks_source_with_the_current_reason(self):
        artifact = self.root / 'bin/zwai-1.2.3-android.apk'
        artifact.parent.mkdir()
        artifact.write_bytes(b'accepted installer')
        with patch.object(release, 'source_preflight', return_value='sha') as source:
            release.github_platform(self.root, '1.2.3', 'sha', 'android', {},
                                    urgent_reason='urgent user request')
        source.assert_called_once_with(self.root, '1.2.3', ('android',), urgent_reason='urgent user request')

    def test_retained_reason_does_not_authorize_another_urgent_attempt(self):
        path = self.root / 'state.json'
        release.write_json(path, {'version': '1.2.3', 'source_sha': 'sha', 'urgent_reason': 'earlier request'})
        with self.assertRaisesRegex(ValueError, 'allocated batch'):
            release.execute(self.root, '1.2.3', 'android', path)

    def test_urgent_check_does_not_write_and_native_wait_still_blocks_upload(self):
        path = self.root / 'state.json'
        self.delivery['issues'] = {'1': {'android': 'pending_native_device_acceptance'}}
        self.save()
        with patch.object(release, 'source_preflight', return_value='sha'), \
             patch.object(release, 'release_notes'), patch.object(release, 'github_platform') as publish:
            self.assertEqual(release.execute(self.root, '1.2.3', 'android', path,
                                            urgent_reason='urgent user request'), 1)
            publish.assert_not_called()
        self.assertEqual(release.read_json(path)['urgent_reason'], 'urgent user request')
        path.unlink()
        with patch.object(release, 'source_preflight', return_value='sha'):
            self.assertEqual(release.execute(self.root, '1.2.3', 'android', path, True,
                                            urgent_reason='urgent user request'), 0)
        self.assertFalse(path.exists())

    def test_cli_forwards_explicit_reason_and_refuses_blank(self):
        argv = ['release', '--version', '1.2.3', '--urgent-reason', '  urgent user request  ', '--check']
        with patch.object(sys, 'argv', argv), patch.object(release, 'execute', return_value=0) as execute:
            self.assertEqual(release.main(), 0)
            self.assertEqual(execute.call_args.kwargs['urgent_reason'], 'urgent user request')
        with patch.object(sys, 'argv', argv[:-3] + ['--urgent-reason', ' ']), \
             patch.object(release, 'execute') as execute, self.assertRaises(SystemExit):
            release.main()
        execute.assert_not_called()


if __name__ == '__main__':
    unittest.main()
