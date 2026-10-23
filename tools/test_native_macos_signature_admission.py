"""System code admission never signals an unverified or unrecorded target."""
import base64
from contextlib import contextmanager
import copy
import json
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import tempfile
import unittest
from unittest import mock

import test_native_macos_resilience_fault as fixtures

fault = fixtures.fault


class SignatureAdmissionTests(unittest.TestCase):
    def setUp(self):
        fixtures.SelectorTests.setUp(self)

    def selected(self, kind="gpu"):
        return fault.select_target(self.boundaries, self.current, kind, case_id=self.case,
            private_app=self.app, now=self.now)

    def command(self, kind):
        return ["/usr/bin/codesign", "--verify", "--strict", "--ignore-resources", "--all-architectures",
            "-R", '=anchor apple and identifier "' + fault.NAMES[kind] + '"', self.paths[kind]]

    def test_signature_scope_and_target_receipts_are_durable_before_pause(self):
        for kind in fault.NAMES:
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                evidence = Path(directory)
                @contextmanager
                def pause(expected, _identity, _seconds):
                    row = fault._read_rows(evidence / fault.SIGNATURES)[0]
                    self.assertEqual(row["status"], "passed")
                    self.assertEqual(row["command"], self.command(kind))
                    self.assertEqual(row["target"], expected)
                    self.assertEqual(row["case_id"], self.case)
                    self.assertFalse(row["resources_validated"])
                    self.assertFalse(row["whole_bundle_validated"])
                    self.assertEqual(row["scope"], "system_executable_code_and_apple_identity")
                    self.assertEqual(fault._read_rows(evidence / fault.TARGETS)[0]["target"], expected)
                    yield dict(stopped=True)
                with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                        mock.patch.object(fault.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b"verified", b"")) as run, \
                        mock.patch.object(fault, "process_identity", side_effect=lambda pid: self.current["shell"] if pid == 200 else None), \
                        mock.patch.object(fault, "_pause", side_effect=pause), mock.patch.object(fault.os, "kill") as send:
                    with fault.stall(self.boundaries, kind, case_id=self.case, private_app=self.app,
                            evidence=evidence, snapshot=lambda: self.current, seconds=1) as result:
                        self.assertTrue(result["state"]["stopped"])
                    run.assert_called_once_with(self.command(kind), check=True, capture_output=True, timeout=2)
                    send.assert_not_called()

    def test_each_command_failure_retains_raw_streams_without_a_target_or_signal(self):
        failures = [subprocess.CalledProcessError(1, "codesign", output=b"partial\xff", stderr=b"resource failure"),
            subprocess.CalledProcessError(3, "codesign", output="", stderr="requirement failure"),
            subprocess.TimeoutExpired("codesign", 2, output=b"partial", stderr=None),
            FileNotFoundError(2, "codesign unavailable")]
        for error in failures:
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as directory:
                evidence = Path(directory)
                with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                        mock.patch.object(fault.subprocess, "run", side_effect=error), \
                        mock.patch.object(fault, "_pause") as pause, mock.patch.object(fault, "_append_target") as append, \
                        mock.patch.object(fault.os, "kill") as send:
                    with self.assertRaises(type(error)) as caught:
                        with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                evidence=evidence, snapshot=lambda: self.current, seconds=1):
                            self.fail("unverified executable reached the fault")
                    self.assertIs(caught.exception, error)
                    pause.assert_not_called(); append.assert_not_called(); send.assert_not_called()
                self.assertFalse((evidence / fault.TARGETS).exists())
                row = fault._read_rows(evidence / fault.SIGNATURES)[0]
                self.assertEqual(row["command"], self.command("gpu"))
                self.assertEqual(row["target"], self.selected()["target"])
                self.assertEqual(row["shell"], self.current["shell"])
                self.assertNotEqual(row["status"], "passed")
                self.assertEqual(row["returncode"], getattr(error, "returncode", None))
                for key, value in (("stdout_base64", getattr(error, "output", None)),
                                   ("stderr_base64", getattr(error, "stderr", None))):
                    self.assertEqual(None if row[key] is None else base64.b64decode(row[key]),
                        value.encode() if isinstance(value, str) else value)

    def test_evidence_write_failure_refuses_success_and_preserves_the_original_failure(self):
        original = subprocess.CalledProcessError(3, "codesign", stderr=b"original refusal")
        for command_error in (None, original):
            with self.subTest(command_error=command_error), tempfile.TemporaryDirectory() as directory:
                with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                        mock.patch.object(fault.subprocess, "run", side_effect=command_error,
                            return_value=subprocess.CompletedProcess([], 0, b"", b"")), \
                        mock.patch.object(fault, "_append", side_effect=OSError("receipt unavailable")), \
                        mock.patch.object(fault, "_append_target") as append, mock.patch.object(fault, "_pause") as pause, \
                        mock.patch.object(fault.os, "kill") as send:
                    with self.assertRaises(type(command_error) if command_error else OSError) as caught:
                        with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                evidence=Path(directory), snapshot=lambda: self.current, seconds=1):
                            self.fail("unrecorded signature reached the fault")
                    if command_error:
                        self.assertIs(caught.exception, original)
                        self.assertIsInstance(original.__cause__, OSError)
                        self.assertEqual(str(original.__cause__), "receipt unavailable")
                    append.assert_not_called(); pause.assert_not_called(); send.assert_not_called()

    def test_kernel_and_complete_cohort_recheck_still_precede_admission(self):
        with tempfile.TemporaryDirectory() as directory:
            evidence = Path(directory)
            def verify(*_args, **_kwargs):
                self.current["observations"][1]["identity"]["birth"] = [1999997, 123]
                return subprocess.CompletedProcess([], 0, b"", b"")
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                    mock.patch.object(fault.subprocess, "run", side_effect=verify), \
                    mock.patch.object(fault, "_append_target") as append, mock.patch.object(fault, "_pause") as pause, \
                    mock.patch.object(fault.os, "kill") as send:
                with self.assertRaises(RuntimeError):
                    with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                            evidence=evidence, snapshot=lambda: self.current, seconds=1):
                        self.fail("changed cohort reached the fault")
                append.assert_not_called(); pause.assert_not_called(); send.assert_not_called()
            self.assertEqual(fault._read_rows(evidence / fault.SIGNATURES)[0]["status"], "passed")

    def test_unknown_system_path_or_kind_never_invokes_codesign(self):
        original = self.selected()
        for mutate in (lambda row: row.update(kind="other"),
                lambda row: row["target"].update(executable="/tmp/untrusted")):
            row = copy.deepcopy(original); mutate(row)
            with tempfile.TemporaryDirectory() as directory, mock.patch.object(fault.subprocess, "run") as run:
                with self.assertRaises(RuntimeError):
                    fault._verify_system_executable(row, Path(directory))
                run.assert_not_called()

    def test_signature_success_does_not_bypass_live_identity_or_selection_changes(self):
        selected = self.selected()
        for mode in ("old-process-live", "shell-changed", "target-changed"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                def identity(pid):
                    if mode == "old-process-live" and pid == 100:
                        return self.boundaries["before-quit"]["shell"]
                    return None if mode == "shell-changed" else self.current["shell"] if pid == 200 else None
                changed = copy.deepcopy(selected); changed["target"]["birth"][1] += 1
                selection = mock.patch.object(fault, "select_target", side_effect=[selected, changed]) if mode == "target-changed" else mock.patch.object(fault, "select_target", wraps=fault.select_target)
                with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                        mock.patch.object(fault.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b"", b"")), \
                        mock.patch.object(fault, "process_identity", side_effect=identity), selection, \
                        mock.patch.object(fault, "_append_target") as append, mock.patch.object(fault, "_pause") as pause, \
                        mock.patch.object(fault.os, "kill") as send:
                    with self.assertRaises(RuntimeError):
                        with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                evidence=Path(directory), snapshot=lambda: self.current, seconds=1):
                            self.fail("changed ownership reached the fault")
                    append.assert_not_called(); pause.assert_not_called(); send.assert_not_called()


@unittest.skipUnless(platform.system() == "Darwin", "real codesign CLI is macOS-only")
class RealCodeSignatureTests(unittest.TestCase):
    def run_verify(self, path, requirement):
        return subprocess.run(["/usr/bin/codesign", "--verify", "--strict", "--ignore-resources",
            "--all-architectures", "-R", requirement, str(path)], capture_output=True, timeout=2)

    def test_requirement_source_is_inline_and_wrong_identifier_is_refused(self):
        old = self.run_verify("/usr/bin/true", "anchor apple")
        self.assertEqual(old.returncode, 1)
        self.assertIn(b"invalid requirement specification", old.stderr)
        self.assertEqual(self.run_verify("/usr/bin/true", '=anchor apple and identifier "com.apple.true"').returncode, 0)
        wrong = self.run_verify("/usr/bin/true", '=anchor apple and identifier "invalid.zwai.fixture"')
        self.assertEqual(wrong.returncode, 3)

    def test_changed_executable_code_page_is_refused_without_resigning(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "true-copy"
            shutil.copyfile("/usr/bin/true", path)
            requirement = '=anchor apple and identifier "com.apple.true"'
            self.assertEqual(self.run_verify(path, requirement).returncode, 0)
            data = bytearray(path.read_bytes())
            # Change executable __TEXT,__text, never its signature or a resource.
            base = struct.unpack_from(">IIIII", data, 8)[2] if data[:4] == b"\xca\xfe\xba\xbe" else 0
            self.assertEqual(struct.unpack_from("<I", data, base)[0], 0xfeedfacf)
            commands = struct.unpack_from("<I", data, base + 16)[0]
            cursor, code = base + 32, None
            for _ in range(commands):
                command, size = struct.unpack_from("<II", data, cursor)
                if command == 0x19:
                    count = struct.unpack_from("<I", data, cursor + 64)[0]
                    for index in range(count):
                        section = cursor + 72 + index * 80
                        if bytes(data[section:section+16]).rstrip(b"\0") == b"__text":
                            code = base + struct.unpack_from("<I", data, section + 48)[0]
                            break
                cursor += size
                if code is not None: break
            self.assertIsNotNone(code)
            data[code] ^= 1
            path.write_bytes(data)
            self.assertNotEqual(self.run_verify(path, requirement).returncode, 0)

    def test_own_linker_signed_code_cannot_satisfy_an_apple_anchor(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "own-headless-code"
            subprocess.run(["/usr/bin/cc", "-x", "c", "-o", str(path), "-"],
                input=b"int main(void){return 0;}\n", capture_output=True, check=True, timeout=30)
            display = subprocess.run(["/usr/bin/codesign", "-dv", str(path)], capture_output=True, check=True, timeout=2)
            identifier = next(line.split(b"=", 1)[1] for line in display.stderr.splitlines() if line.startswith(b"Identifier=")).decode()
            result = self.run_verify(path, '=anchor apple and identifier "' + identifier + '"')
            self.assertEqual(result.returncode, 3)


if __name__ == "__main__":
    unittest.main()
