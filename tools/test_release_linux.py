import io
import json
import os
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release
import release_linux


def elf(machine=183):
    body = bytearray(64)
    body[:7] = b'\x7fELF\x02\x01\x01'
    body[16:18] = (2).to_bytes(2, 'little')
    body[18:20] = machine.to_bytes(2, 'little')
    return bytes(body)


def archive(path, body=None, name='zwai', mode=0o755, link=False):
    with tarfile.open(path, 'w:gz') as out:
        entry = tarfile.TarInfo(name)
        entry.mode = mode
        if link:
            entry.type = tarfile.SYMTYPE
            entry.linkname = '/etc/passwd'
            out.addfile(entry)
        else:
            body = elf() if body is None else body
            entry.size = len(body)
            out.addfile(entry, io.BytesIO(body))


class LinuxReleaseTests(unittest.TestCase):
    def test_archive_requires_only_executable_arm64_elf(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'zwai-1.2.3-linux-arm64.tar.gz'
            archive(path)
            release_linux.validate_archive(path)
            for kwargs in ({'body': elf(62)}, {'body': b'bad'}, {'mode': 0o644},
                           {'name': '../zwai'}, {'link': True}):
                archive(path, **kwargs)
                with self.subTest(kwargs=kwargs), self.assertRaises(ValueError):
                    release_linux.validate_archive(path)
            path.write_bytes(b'bad')
            with self.assertRaises(ValueError):
                release_linux.validate_archive(path)

    def test_build_stamps_server_arch_and_keeps_previous_artifact_on_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / 'out'
            calls = []
            def compile(args, **kwargs):
                calls.append((args, kwargs))
                Path(args[args.index('-o') + 1]).write_bytes(elf())
            with patch.object(release_linux.subprocess, 'run', side_effect=compile):
                path = release_linux.build(root, '1.2.3', output)
            release_linux.validate_archive(path)
            args, kwargs = calls[0]
            self.assertEqual(kwargs['env']['GOOS'], 'linux')
            self.assertEqual(kwargs['env']['GOARCH'], 'arm64')
            self.assertEqual(kwargs['env']['CGO_ENABLED'], '0')
            self.assertIn('server', args)
            self.assertIn('-X main.version=1.2.3', args)
            previous = path.read_bytes()
            with patch.object(release_linux.subprocess, 'run', side_effect=RuntimeError('compile')):
                with self.assertRaises(RuntimeError):
                    release_linux.build(root, '1.2.3', output)
            self.assertEqual(path.read_bytes(), previous)
            with self.assertRaises(ValueError):
                release_linux.build(root, 'dirty', output)

    def test_linux_publisher_builds_validates_and_does_not_rebuild_remote_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'bin').mkdir()
            path = root / 'bin/zwai-1.2.3-linux-arm64.tar.gz'
            calls = []
            def command(args, **kwargs):
                calls.append(args)
                if args[:2] == ['gh', 'api']:
                    return '[]'
                if args[:2] == ['make', 'linux-release']:
                    archive(path)
                return ''
            with patch.object(release, 'run', side_effect=command), \
                 patch.object(release, 'source_preflight', return_value='sha'), \
                 patch.dict(os.environ, {'DELIVERY_STATE': str(root / 'missing')}):
                result = release.github_platform(root, '1.2.3', 'sha', 'linux', {})
            self.assertEqual(result['status'], 'published')
            self.assertTrue(any(c[:2] == ['make', 'linux-release'] for c in calls))
            self.assertFalse(any(c[0] == 'uname' for c in calls))
            remote = json.dumps([[{'tag_name': 'v1.2.3', 'assets': [
                {'name': path.name, 'digest': 'sha256:' + release.sha256(path)}]}]])
            calls.clear()
            with patch.object(release, 'run', side_effect=lambda args, **kw: calls.append(args) or remote), \
                 patch.object(release, 'source_preflight', return_value='sha'):
                release.github_platform(root, '1.2.3', 'sha', 'linux', {})
            self.assertFalse(any(c[0] == 'make' for c in calls))
            path.write_bytes(b'changed')
            with patch.object(release, 'run', return_value=remote), self.assertRaises(ValueError):
                release.github_platform(root, '1.2.3', 'sha', 'linux', {})

    def test_direct_linux_batch_is_selected_and_recorded_without_issue_rows(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'delivery.json'
            release.write_json(path, {'issues': {}, 'pending_new_batch': {
                'version': '1.2.3', 'source_sha': 'sha', 'issues': [],
                'additional_platforms': {'linux': {'status': 'pending_release'}}}})
            with patch.dict(os.environ, {'DELIVERY_STATE': str(path)}):
                self.assertEqual(release.batch_platforms('1.2.3', True), ['linux'])
                release.record_installer('1.2.3', 'sha', 'linux', {'status': 'published'})
            batch = release.read_json(path)['pending_new_batch']
            self.assertEqual(batch['publication'], 'platform_delivery_verified')
            self.assertEqual(batch['additional_platforms']['linux']['status'], 'published')

    def test_linux_runtime_accepted_bytes_are_reused_or_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'bin').mkdir()
            path = root / 'bin/zwai-1.2.3-linux-arm64.tar.gz'
            archive(path)
            ledger = root / 'delivery.json'
            accepted = {'status': 'passed', 'source_sha': 'sha', 'version': '1.2.3',
                        'asset_name': path.name, 'asset_sha256': release.sha256(path)}
            release.write_json(ledger, {'pending_new_batch': {
                'version': '1.2.3', 'source_sha': 'sha', 'release_linux_validation': accepted}})
            calls = []
            def command(args, **kwargs):
                calls.append(args)
                return '[]' if args[:2] == ['gh', 'api'] else ''
            with patch.dict(os.environ, {'DELIVERY_STATE': str(ledger)}), \
                 patch.object(release, 'run', side_effect=command), \
                 patch.object(release, 'source_preflight', return_value='sha'):
                release.github_platform(root, '1.2.3', 'sha', 'linux', {})
                self.assertFalse(any(c[0] == 'make' for c in calls))
                path.write_bytes(b'changed')
                with self.assertRaisesRegex(ValueError, 'runtime evidence'):
                    release.github_platform(root, '1.2.3', 'sha', 'linux', {})
                path.unlink()
                with self.assertRaisesRegex(ValueError, 'runtime evidence'):
                    release.github_platform(root, '1.2.3', 'sha', 'linux', {})

    def test_old_rows_do_not_gain_linux_requirement(self):
        row = dict(android='published', macos='published', ios='published', server='not_required')
        self.assertTrue(release.delivery_complete(row))
        self.assertFalse(release.issue_needs_platform(row, 'linux', set()))
        row['linux'] = 'pending'
        self.assertFalse(release.delivery_complete(row))


if __name__ == '__main__':
    unittest.main()
