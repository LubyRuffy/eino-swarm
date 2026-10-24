"""The first public APK must be the exact installer accepted on Android."""
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release


class AcceptedAndroidInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'bin').mkdir()
        self.asset = self.root / 'bin/zwai-1.2.3-android.apk'
        self.asset.write_bytes(b'accepted installer fixture')
        self.digest = hashlib.sha256(self.asset.read_bytes()).hexdigest()
        self.source = 'd' * 40
        self.ledger = self.root / 'delivery.json'
        self.batch = {
            'version': '1.2.3', 'source_sha': self.source,
            'android': 'ready_for_publication',
            'release_android_validation': {
                'status': 'passed', 'version': '1.2.3', 'source_sha': self.source,
                'asset_name': self.asset.name, 'asset_sha256': self.digest,
            },
        }
        self.commands = []
        self.remote = []
        self.allow_build = False
        self.save_batch(self.batch)
        self.env = patch.dict(os.environ, {'DELIVERY_STATE': str(self.ledger)})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.command = patch.object(release, 'run', side_effect=self.run_command)
        self.command.start()
        self.addCleanup(self.command.stop)
        self.preflight = patch.object(release, 'source_preflight', return_value=self.source)
        self.preflight.start()
        self.addCleanup(self.preflight.stop)

    def save_batch(self, batch):
        release.write_json(self.ledger, {'pending_new_batch': batch})

    def run_command(self, args, **kwargs):
        self.commands.append(list(args))
        if args[:3] == ['gh', 'api', '--paginate']:
            return json.dumps(self.remote)
        if args[:2] == ['make', 'mobile-android-release']:
            self.assertTrue(self.allow_build, 'The accepted Android installer was rebuilt')
            self.assertEqual(kwargs['env']['ANDROID_ARTIFACT'], 'apk')
            self.asset.write_bytes(b'new unaccepted build')
            return ''
        if args == ['git', 'status', '--porcelain']:
            return ''
        if args[:3] == ['go', 'run', './internal/release/cmd']:
            self.assertEqual(args, ['go', 'run', './internal/release/cmd', '-version',
                '1.2.3', '-dir', str(self.root / 'bin'), '-platform', 'android',
                '-target', self.source])
            return ''
        self.fail(f'Unexpected external command: {args}')

    def publish(self):
        return release.github_platform(self.root, '1.2.3', self.source, 'android', {})

    def assert_no_build_or_upload(self):
        self.assertFalse(any(args[0] in ('make', 'go') for args in self.commands))

    def test_passed_exact_installer_is_reused_without_build(self):
        original = self.asset.read_bytes()
        result = self.publish()
        self.assertEqual(result, {'status': 'published',
            'digests': {self.asset.name: self.digest}})
        self.assertEqual(self.asset.read_bytes(), original)
        self.assertEqual(sum(args[0] == 'go' for args in self.commands), 1)
        self.assertFalse(any(args[0] == 'make' for args in self.commands))

    def test_each_receipt_identity_and_status_must_match_before_upload(self):
        for field, value in [
            ('status', 'pending'), ('version', '1.2.4'), ('source_sha', 'e' * 40),
            ('asset_name', 'other.apk'), ('asset_sha256', '0' * 64),
        ]:
            with self.subTest(field=field):
                batch = copy.deepcopy(self.batch)
                batch['release_android_validation'][field] = value
                self.save_batch(batch)
                self.commands.clear()
                with self.assertRaisesRegex(ValueError, 'accepted Android package'):
                    self.publish()
                self.assert_no_build_or_upload()
        for field in self.batch['release_android_validation']:
            with self.subTest(missing=field):
                batch = copy.deepcopy(self.batch)
                del batch['release_android_validation'][field]
                self.save_batch(batch)
                self.commands.clear()
                with self.assertRaisesRegex(ValueError, 'accepted Android package'):
                    self.publish()
                self.assert_no_build_or_upload()

    def test_missing_changed_or_nonfile_installer_never_rebuilds(self):
        self.asset.write_bytes(b'changed after device acceptance')
        with self.assertRaisesRegex(ValueError, 'accepted Android package'):
            self.publish()
        self.assert_no_build_or_upload()
        self.asset.unlink()
        for directory in (False, True):
            with self.subTest(directory=directory):
                if directory:
                    self.asset.mkdir()
                self.commands.clear()
                with self.assertRaisesRegex(ValueError, 'accepted Android package'):
                    self.publish()
                self.assert_no_build_or_upload()

    def test_ready_without_receipt_and_present_malformed_receipt_fail_closed(self):
        batch = copy.deepcopy(self.batch)
        del batch['release_android_validation']
        self.save_batch(batch)
        with self.assertRaisesRegex(ValueError, 'accepted Android package'):
            self.publish()
        self.assert_no_build_or_upload()
        for value in (None, [], 'passed', {}, False):
            with self.subTest(receipt=value):
                batch = copy.deepcopy(self.batch)
                batch['android'] = 'pending'
                batch['release_android_validation'] = value
                self.save_batch(batch)
                self.commands.clear()
                with self.assertRaisesRegex(ValueError, 'accepted Android package'):
                    self.publish()
                self.assert_no_build_or_upload()

    def test_completed_batch_recovery_reuses_its_accepted_installer(self):
        release.write_json(self.ledger, {'pending_new_batch': {'version': '1.2.4'},
            'completed_batches': {'1.2.3': {'batch': self.batch}}})
        self.assertEqual(self.publish()['digests'][self.asset.name], self.digest)
        self.assertFalse(any(args[0] == 'make' for args in self.commands))

    def test_existing_remote_asset_keeps_digest_retry_rules(self):
        # Public asset identity is already frozen; no new receipt/build is needed.
        batch = copy.deepcopy(self.batch)
        del batch['release_android_validation']
        batch['source_sha'] = 'e' * 40
        self.save_batch(batch)
        self.remote = [[{'tag_name': 'v1.2.3', 'assets': [{
            'name': self.asset.name, 'digest': 'sha256:' + self.digest}]}]]
        self.assertEqual(self.publish()['digests'][self.asset.name], self.digest)
        self.assertFalse(any(args[0] == 'make' for args in self.commands))
        for digest in (None, 'sha256:' + '0' * 64):
            with self.subTest(digest=digest):
                self.remote[0][0]['assets'][0]['digest'] = digest
                self.commands.clear()
                with self.assertRaises(ValueError):
                    self.publish()
                self.assert_no_build_or_upload()

    def test_unaccepted_pending_build_keeps_original_build_entry(self):
        batch = copy.deepcopy(self.batch)
        del batch['release_android_validation']
        batch['android'] = 'pending'
        self.save_batch(batch)
        self.allow_build = True
        self.assertEqual(self.publish()['status'], 'published')
        self.assertEqual(sum(args[0] == 'make' for args in self.commands), 1)
        self.assertEqual(sum(args[0] == 'go' for args in self.commands), 1)

    def test_source_drift_after_artifact_validation_still_refuses_upload(self):
        with patch.object(release, 'source_preflight', return_value='other'):
            with self.assertRaisesRegex(ValueError, 'Source changed'):
                self.publish()
        self.assert_no_build_or_upload()


if __name__ == '__main__':
    unittest.main()
