import json
import os
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release


def pe(machine):
    buf = bytearray(0x80)
    buf[0:2] = b"MZ"
    buf[0x3C:0x40] = (0x40).to_bytes(4, "little")
    buf[0x40:0x44] = b"PE\0\0"
    buf[0x44:0x46] = machine.to_bytes(2, "little")
    return bytes(buf)


def write_windows_zip(path, machine):
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("zwai.exe", pe(machine))


class WindowsInstallerTests(unittest.TestCase):
    def test_both_architectures_are_required_and_typed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            amd64 = root / "zwai-1.2.3-windows-amd64.zip"
            arm64 = root / "zwai-1.2.3-windows-arm64.zip"
            write_windows_zip(amd64, 0x8664)
            write_windows_zip(arm64, 0xAA64)
            release.validate_windows_zip(amd64)
            release.validate_windows_zip(arm64)
            write_windows_zip(amd64, 0xAA64)
            with self.assertRaisesRegex(ValueError, "amd64"):
                release.validate_windows_zip(amd64)
            bad = root / "zwai-1.2.3-windows-amd64.zip"
            with zipfile.ZipFile(bad, "w") as archive:
                archive.writestr("readme.txt", b"no")
            with self.assertRaisesRegex(ValueError, "only zwai.exe"):
                release.validate_windows_zip(bad)
            (root / "notes.zip").write_bytes(b"nope")
            with self.assertRaisesRegex(ValueError, "name"):
                release.validate_windows_zip(root / "notes.zip")

    def test_absent_windows_rows_do_not_become_a_required_platform(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "delivery.json"
            release.write_json(ledger, {"pending_new_batch": {
                "version": "1.2.3", "source_sha": "sha", "issues": [4]},
                "issues": {"4": {"macos": "pending_next_qualified_batch",
                                 "android": "not_required_by_behavior_change",
                                 "ios": "not_required_by_behavior_change",
                                 "server": "not_required"}}})
            with patch.dict(os.environ, {"DELIVERY_STATE": str(ledger)}):
                self.assertEqual(release.batch_platforms("1.2.3", pending_only=True), ["macos"])
                release.record_installer("1.2.3", "sha", "windows", {"status": "published"})
            row = release.read_json(ledger)["issues"]["4"]
            self.assertNotIn("windows", row)
            done = {"macos": "published", "android": "not_required", "ios": "not_required",
                    "server": "not_required"}
            self.assertTrue(release.delivery_complete(done))
            done["windows"] = "pending_release"
            self.assertFalse(release.delivery_complete(done))

    def test_windows_build_publishes_both_zips_and_rejects_a_bad_executable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            files = [root / "bin" / name for name in (
                "zwai-1.2.3-windows-amd64.zip", "zwai-1.2.3-windows-arm64.zip")]
            calls = []

            def command(args, **kwargs):
                calls.append(args)
                if args[:2] == ["gh", "api"]:
                    return "[]"
                if args[:2] == ["make", "windows-release"]:
                    write_windows_zip(files[0], 0x8664)
                    write_windows_zip(files[1], 0xAA64)
                if args[0] == "git":
                    return ""
                return ""

            with patch.object(release, "run", side_effect=command), \
                 patch.object(release, "source_preflight", return_value="sha"), \
                 patch.dict(os.environ, {"DELIVERY_STATE": str(root / "missing.json")}):
                result = release.github_platform(root, "1.2.3", "sha", "windows", {})
            self.assertEqual(result["status"], "published")
            self.assertEqual(set(result["digests"]), {path.name for path in files})
            self.assertTrue(any(call[:2] == ["make", "windows-release"] for call in calls))
            self.assertFalse(any(call[0] == "uname" for call in calls))
            files[0].write_bytes(b"not a zip")
            calls.clear()

            def rebuild(args, **kwargs):
                calls.append(args)
                if args[:2] == ["gh", "api"]:
                    return "[]"
                if args[0] == "make":
                    files[0].write_text("still bad")
                    write_windows_zip(files[1], 0xAA64)
                return ""

            with patch.object(release, "run", side_effect=rebuild), \
                 patch.object(release, "source_preflight", return_value="sha"), \
                 patch.dict(os.environ, {"DELIVERY_STATE": str(root / "missing.json")}):
                with self.assertRaisesRegex(ValueError, "zip"):
                    release.github_platform(root, "1.2.3", "sha", "windows", {})

    def test_published_windows_pair_is_not_rebuilt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            files = []
            for name, machine in (("zwai-1.2.3-windows-amd64.zip", 0x8664),
                                  ("zwai-1.2.3-windows-arm64.zip", 0xAA64)):
                path = root / "bin" / name
                write_windows_zip(path, machine)
                files.append(path)
            remote = [[{"tag_name": "v1.2.3", "assets": [
                {"name": path.name, "digest": "sha256:" + release.sha256(path)} for path in files]}]]
            calls = []

            def command(args, **kwargs):
                calls.append(args)
                if args[0] == "gh":
                    return json.dumps(remote)
                return ""

            with patch.object(release, "run", side_effect=command), \
                 patch.object(release, "source_preflight", return_value="sha"):
                result = release.github_platform(root, "1.2.3", "sha", "windows", {})
            self.assertEqual(result["status"], "published")
            self.assertFalse(any(call[0] == "make" for call in calls))


if __name__ == "__main__":
    unittest.main()
