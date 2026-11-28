"""Exercise the real publisher's receipt and partial-publication boundaries."""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release
from test_release_linux import archive, elf


class LinuxPublicationTests(unittest.TestCase):
    def fixture(self, root):
        (root / 'bin').mkdir()
        files, receipts = {}, {}
        for arch, machine in (('amd64', 62), ('arm64', 183)):
            path = root / 'bin' / f'zwai-1.2.3-linux-{arch}.tar.gz'
            archive(path, body=elf(machine))
            files[arch] = path
            receipts[arch] = {'status': 'passed', 'arch': arch, 'asset_name': path.name,
                              'asset_sha256': release.sha256(path),
                              'host_recovery': dict.fromkeys((
                                  'preparation_power_loss', 'trial_power_loss', 'committed_power_loss', 'supervisor_failure',
                                  'pairing_refusal', 'listener_ownership'), 'passed')}
        accepted = {'status': 'passed', 'version': '1.2.3', 'source_sha': 'sha', 'assets': receipts}
        batch = {'version': '1.2.3', 'source_sha': 'sha', 'linux': 'ready_for_publication',
                 'release_linux_validation': accepted}
        return files, batch

    def publish(self, root, batch, remote=(), download=None):
        ledger = root / 'delivery.json'
        release.write_json(ledger, {'pending_new_batch': batch})
        calls = []

        def command(args, **kwargs):
            calls.append(args)
            if args[:2] == ['gh', 'api']:
                return json.dumps([[{'tag_name': 'v1.2.3', 'assets': list(remote)}]])
            if args[:3] == ['gh', 'release', 'download'] and download:
                download(args)
            return ''

        with patch.dict(os.environ, {'DELIVERY_STATE': str(ledger)}), \
                patch.object(release, 'run', side_effect=command), \
                patch.object(release, 'source_preflight', return_value='sha'):
            try:
                result = release.github_platform(root, '1.2.3', 'sha', 'linux', {})
            except ValueError:
                self.assertFalse(any(c[0] in ('make', 'go') for c in calls), calls)
                raise
        self.assertFalse(any(c[0] == 'make' for c in calls), calls)
        self.assertEqual(result['status'], 'published')
        self.assertEqual(len(result['digests']), 2)
        self.assertEqual(sum(c[:3] == ['go', 'run', './internal/release/cmd'] for c in calls), 1)
        return calls

    def test_both_runtime_accepted_archives_are_published_without_rebuild(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files, batch = self.fixture(root)
            before = {a: p.read_bytes() for a, p in files.items()}
            self.publish(root, batch)
            self.assertEqual(before, {a: p.read_bytes() for a, p in files.items()})

    def test_new_upload_refuses_missing_or_mismatched_evidence_before_publisher(self):
        for fault in ('missing-receipt', 'legacy-single-receipt', 'missing-arch', 'arch', 'name',
                      'source', 'version', 'status', 'asset-status', 'hash', 'file', 'machine',
                      'missing-host-recovery', 'failed-recovery-case', 'missing-recovery-case'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                files, batch = self.fixture(root)
                accepted = batch['release_linux_validation']
                item = accepted['assets']['amd64']
                if fault == 'missing-receipt':
                    del batch['release_linux_validation']
                elif fault == 'legacy-single-receipt':
                    batch['release_linux_validation'] = dict(item, version='1.2.3', source_sha='sha')
                elif fault == 'missing-arch':
                    del accepted['assets']['amd64']
                elif fault in ('source', 'version', 'status'):
                    accepted[{'source': 'source_sha'}.get(fault, fault)] = 'wrong'
                elif fault in ('arch', 'name', 'hash', 'asset-status'):
                    item[{'name': 'asset_name', 'hash': 'asset_sha256', 'asset-status': 'status'}.get(fault, fault)] = 'wrong'
                elif fault == 'file':
                    files['amd64'].unlink()
                elif fault == 'machine':
                    archive(files['amd64'], body=elf(183))
                    item['asset_sha256'] = release.sha256(files['amd64'])
                elif fault == 'missing-host-recovery':
                    del item['host_recovery']
                elif fault == 'failed-recovery-case':
                    item['host_recovery']['supervisor_failure'] = 'failed'
                elif fault == 'missing-recovery-case':
                    del item['host_recovery']['preparation_power_loss']
                with self.assertRaises(ValueError):
                    self.publish(root, batch)

    def test_partial_publication_keeps_existing_bytes_and_requires_other_arch_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files, batch = self.fixture(root)
            arm = files['arm64']
            original = arm.read_bytes()
            remote = [{'name': arm.name, 'digest': 'sha256:' + release.sha256(arm)}]
            # Published bytes use their remote digest, not a new runtime receipt.
            del batch['release_linux_validation']['assets']['arm64']
            self.publish(root, batch, remote)
            self.assertEqual(arm.read_bytes(), original)
            arm.unlink()
            calls = self.publish(root, batch, remote, download=lambda args: arm.write_bytes(original))
            self.assertTrue(any(c[:3] == ['gh', 'release', 'download'] for c in calls))
            self.assertEqual(arm.read_bytes(), original)
            arm.write_bytes(b'changed')
            with self.assertRaises(ValueError):
                self.publish(root, batch, remote)

    def test_fully_public_retry_needs_digests_and_never_rebuilds(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files, _ = self.fixture(root)
            remote = [{'name': p.name, 'digest': 'sha256:' + release.sha256(p)} for p in files.values()]
            self.publish(root, {}, remote)
            del remote[0]['digest']
            with self.assertRaises(ValueError):
                self.publish(root, {}, remote)


if __name__ == '__main__':
    unittest.main()
