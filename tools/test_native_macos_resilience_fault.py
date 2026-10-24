"""Only pure cohort fixtures and this test's own headless children receive signals."""
from contextlib import contextmanager
import copy
import importlib.util
import json
import os
from pathlib import Path
import select
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("fault", Path(__file__).with_name("native_macos_resilience_fault.py"))
fault = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fault)


class SelectorTests(unittest.TestCase):
    def setUp(self):
        self.case = "a" * 32
        self.app = Path("/private/var/folders/qa/T/zwai-native-" + self.case + "-owned/zwai.app")
        self.now = 2000000.0
        def identity(pid, born, path):
            return dict(pid=pid, birth=[born, 123], executable=str(path))
        old_shell = identity(100, 1999980, self.app / "Contents/MacOS/zwai")
        new_shell = identity(200, 1999995, self.app / "Contents/MacOS/zwai")
        self.paths = {kind: "/System/Volumes/Preboot/Cryptexes/OS/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/" + name + ".xpc/Contents/MacOS/" + name
                      for kind, name in (("gpu", "com.apple.WebKit.GPU"), ("renderer", "com.apple.WebKit.WebContent"))}
        def snapshot(at, shell, base=0):
            row = dict(case_id=self.case, at=at, shell=shell, prior_observed_shell=shell or old_shell,
                       process_rows=[], observations=[], guest_os="macOS QA identified build")
            if shell:
                identities = [shell, identity(base + 1, shell["birth"][0] + 1, self.paths["gpu"]),
                              identity(base + 2, shell["birth"][0] + 1, self.paths["renderer"])]
                for ident in identities:
                    name = Path(ident["executable"]).name
                    row["process_rows"].append(dict(pid=ident["pid"], executable_name=name,
                        ownership="case" if name == "zwai" else "observed_unattributed"))
                    version = {} if name == "zwai" else dict(CFBundleIdentifier="com.apple.WebKit", CFBundleVersion="identified")
                    row["observations"].append(dict(identity=ident, procinfo_exit=0,
                        procinfo_fields=["program path = " + ident["executable"], "pid = " + str(ident["pid"])],
                        signature_exit=0, signature=["Identifier=" + ("app.zwai.desktop" if name == "zwai" else name), "TeamIdentifier=not set"],
                        framework_version=version))
            return row
        self.boundaries = {"before-quit": snapshot(1999985, old_shell, 100),
                           "after-quit": snapshot(1999990, None),
                           "before-reopen": snapshot(1999994, None),
                           "after-reopen": snapshot(1999998, new_shell, 200)}
        self.current = snapshot(1999999.8, new_shell, 200)

    def select(self, kind="gpu"):
        return fault.select_target(self.boundaries, self.current, kind, case_id=self.case,
                                   private_app=self.app, now=self.now)

    def test_only_the_new_unique_system_service_is_selected(self):
        for kind, pid in (("gpu", 201), ("renderer", 202)):
            target = self.select(kind)
            self.assertEqual(target["target"]["pid"], pid)
            self.assertEqual(target["shell"]["pid"], 200)
            self.assertEqual(target["private_app"], str(self.app))

    def test_incomplete_mixed_stale_and_ambiguous_proofs_never_select(self):
        original, current = copy.deepcopy(self.boundaries), copy.deepcopy(self.current)
        mutations = [
            lambda: self.boundaries.pop("after-quit"),
            lambda: self.boundaries["before-reopen"].update(case_id="b" * 32),
            lambda: self.current.update(at=self.now - 20),
            lambda: self.boundaries["after-quit"].update(at=self.now),
            lambda: self.boundaries["after-quit"].update(shell=original["before-quit"]["shell"]),
            lambda: self.boundaries["before-reopen"]["process_rows"].append(original["before-quit"]["process_rows"][1]),
            lambda: self.current["process_rows"].append(dict(pid=999, executable_name="com.apple.WebKit.GPU")),
            lambda: self.current["process_rows"].append(dict(pid=999, executable_name="com.apple.WebKit.WebContent.CaptivePortal")),
            lambda: self.current["observations"][1]["identity"].update(birth=[1999990, 1]),
            lambda: self.current["observations"][1]["identity"].update(executable="/tmp/com.apple.WebKit.GPU"),
            lambda: self.current["shell"].update(executable="/other/zwai.app/Contents/MacOS/zwai"),
            lambda: self.current["observations"][1].update(signature_exit=1),
            lambda: self.current["observations"][1].update(signature=["Identifier=other", "TeamIdentifier=not set"]),
            lambda: self.current["observations"][1].update(framework_version={}),
            lambda: self.current["observations"].pop(),
            lambda: self.boundaries["before-quit"]["observations"][1]["identity"].update(pid=201),
        ]
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                self.boundaries, self.current = copy.deepcopy(original), copy.deepcopy(current)
                mutate()
                with self.assertRaises((ValueError, RuntimeError)):
                    self.select()

    def test_receipts_bind_the_case_and_app_before_any_resume(self):
        target = self.select()
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary)
            fault._append_target(evidence, target)
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_resume_identity") as resume:
                with self.assertRaises(RuntimeError):
                    fault.cleanup_case(evidence, "b" * 32, self.app)
                resume.assert_not_called()
                resume.return_value = dict(outcome="exited", identity=target["target"])
                result = fault.cleanup_case(evidence, self.case, self.app)
                self.assertTrue(result["complete"])
                self.assertEqual(result["targets"][0]["outcome"], "exited")
            with self.assertRaises(RuntimeError):
                fault._append_target(evidence, target)

    def test_physical_host_gate_precedes_any_fault_signal(self):
        with mock.patch.object(fault, "require_guest", side_effect=RuntimeError("host refused")), \
                mock.patch.object(fault.os, "kill") as send:
            with self.assertRaisesRegex(RuntimeError, "host refused"):
                with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                 evidence=Path("/unused"), snapshot=lambda: self.current, seconds=1):
                    self.fail("host entered fault context")
            send.assert_not_called()

    def test_public_signature_verification_and_durable_receipt_precede_stop(self):
        @contextmanager
        def inspect_receipt(expected, _identity, _seconds):
            rows = fault._read_rows(evidence / fault.TARGETS)
            self.assertEqual(rows[0]["target"], expected)
            yield dict(stopped=True)
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary)
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                    mock.patch.object(fault.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b"", b"")) as verify, \
                    mock.patch.object(fault, "process_identity", side_effect=lambda pid: self.current["shell"] if pid == 200 else None), \
                    mock.patch.object(fault, "_pause", side_effect=inspect_receipt):
                with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                 evidence=evidence, snapshot=lambda: self.current, seconds=1) as receipt:
                    self.assertTrue(receipt["state"]["stopped"])
                self.assertEqual(verify.call_args.args[0], ["/usr/bin/codesign", "--verify", "--strict", "--ignore-resources",
                    "--all-architectures", "-R", '=anchor apple and identifier "com.apple.WebKit.GPU"', self.paths["gpu"]])
            (evidence / fault.TARGETS).unlink()
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault.time, "time", return_value=self.now), \
                    mock.patch.object(fault.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "codesign")), \
                    mock.patch.object(fault.os, "kill") as send:
                with self.assertRaises(subprocess.CalledProcessError):
                    with fault.stall(self.boundaries, "gpu", case_id=self.case, private_app=self.app,
                                     evidence=evidence, snapshot=lambda: self.current, seconds=1):
                        self.fail("unverified system executable was admitted")
                self.assertFalse((evidence / fault.TARGETS).exists())
                send.assert_not_called()

    def test_cleanup_of_an_exited_identity_never_resumes_its_reused_pid(self):
        target = self.select()
        replacement = dict(target["target"], birth=[2000001, 1])
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary)
            fault._append_target(evidence, target)
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "process_identity", return_value=replacement), \
                    mock.patch.object(fault.os, "kill") as send:
                result = fault.cleanup_case(evidence, self.case, self.app, require_exit=True)
                self.assertTrue(result["complete"])
                self.assertEqual(result["targets"][0]["outcome"], "exited")
                send.assert_not_called()

    def test_valid_target_cannot_hide_a_dangling_permission_ledger(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary)
            fault._append_target(evidence, self.select())
            (evidence / fault.COPIES).symlink_to(evidence / "missing")
            with mock.patch.object(fault, "require_guest"), \
                    mock.patch.object(fault, "_resume_identity", return_value=dict(outcome="exited")) as resume:
                with self.assertRaises(RuntimeError):
                    fault.cleanup_case(evidence, self.case, self.app)
                resume.assert_not_called()


class OwnedChildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.tick = Path(self.temp.name) / "ticks"
        self.child = subprocess.Popen([sys.executable, "-c",
            "import pathlib,sys,time\np=pathlib.Path(sys.argv[1])\ni=0\nwhile True:\n i+=1;p.write_text(str(i));time.sleep(.01)\n", str(self.tick)])
        self.addCleanup(self.stop_child)
        deadline = time.monotonic() + 3
        while not self.tick.exists() and time.monotonic() < deadline:
            time.sleep(.01)
        self.assertTrue(self.tick.exists())
        self.identity = fault.process_identity(self.child.pid)
        self.assertIsNotNone(self.identity)

    def stop_child(self):
        if self.child.poll() is None:
            if fault.process_identity(self.child.pid) == self.identity:
                os.kill(self.child.pid, signal.SIGCONT)
                self.child.terminate()
            self.child.wait(timeout=3)

    def moving(self, previous):
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline:
            if self.tick.stat().st_mtime_ns != previous:
                return True
            time.sleep(.01)
        return False

    def test_timeout_resumes_the_exact_private_child_even_inside_the_context(self):
        with fault._pause(self.identity, fault.process_identity, .15) as receipt:
            self.assertFalse(receipt["before_stat"].startswith("T"))
            self.assertTrue(receipt["stopped_stat"].startswith("T"))
            previous = self.tick.stat().st_mtime_ns
            time.sleep(.04)
            self.assertEqual(self.tick.stat().st_mtime_ns, previous)
            self.assertTrue(self.moving(previous))
            self.assertTrue(receipt["expired"])
        self.assertEqual(receipt["resume"]["outcome"], "resumed")
        self.assertEqual(fault._resume_identity(self.identity, fault.process_identity)["outcome"], "resumed")

    def test_cancellation_and_body_failure_resume_the_exact_private_child(self):
        for error in (RuntimeError("cancelled operation"), KeyboardInterrupt()):
            with self.subTest(error=type(error).__name__), self.assertRaises(type(error)):
                with fault._pause(self.identity, fault.process_identity, .8):
                    raise error
            self.assertTrue(self.moving(self.tick.stat().st_mtime_ns))

    def test_changed_birth_or_path_refuses_resume_instead_of_signalling_a_reused_pid(self):
        for field, value in (("birth", [1, 2]), ("executable", "/different/process")):
            changed = False
            def inspect(pid):
                return dict(self.identity, **({field: value} if changed else {}))
            with self.subTest(field=field), mock.patch.object(fault.os, "kill", wraps=os.kill) as send:
                with self.assertRaises(RuntimeError):
                    with fault._pause(self.identity, inspect, .8):
                        changed = True
                self.assertFalse(any(call.args[1] == signal.SIGCONT for call in send.call_args_list))
            os.kill(self.child.pid, signal.SIGCONT)
            self.assertTrue(self.moving(self.tick.stat().st_mtime_ns))


class PrivateCopyTests(unittest.TestCase):
    def setUp(self):
        self.case = "c" * 32
        self.temp = tempfile.TemporaryDirectory(prefix="zwai-native-" + self.case + "-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.app = self.root / "zwai.app"
        self.original = self.app / "Contents/MacOS/zwai"
        self.original.parent.mkdir(parents=True)
        self.original.write_bytes(b"#!/bin/sh\nexit 0\n")
        self.original.chmod(0o755)
        self.executable = self.root / "home/Library/Caches/zwai/desktop-recovery-owned/zwai.app/Contents/MacOS/zwai"
        self.executable.parent.mkdir(parents=True)
        self.executable.write_bytes(self.original.read_bytes())
        self.executable.chmod(0o755)
        (self.executable.parents[3] / "session").write_text("d" * 16)
        self.evidence = self.root / "evidence"
        self.evidence.mkdir()

    def deny(self):
        return fault.deny_successor_launch(case_id=self.case, private_app=self.app,
                                          evidence=self.evidence, session_id="d" * 16)

    def test_real_eacces_is_limited_to_the_unused_copy_and_cancellation_restores_it(self):
        before = self.original.read_bytes(), self.original.stat().st_mode
        with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[]):
            with self.assertRaisesRegex(RuntimeError, "cancelled"):
                with self.deny():
                    self.assertTrue((self.evidence / fault.COPIES).is_file())
                    with self.assertRaises(PermissionError):
                        subprocess.run([str(self.executable)], check=True, capture_output=True)
                    self.assertEqual((self.original.read_bytes(), self.original.stat().st_mode), before)
                    raise RuntimeError("cancelled")
        self.assertEqual(stat.S_IMODE(self.executable.stat().st_mode), 0o755)
        self.assertEqual(self.executable.read_bytes(), before[0])
        self.assertEqual(subprocess.run([str(self.executable)], capture_output=True).returncode, 0)

    def test_a_hardlink_to_the_installed_app_never_loses_execute_permission(self):
        self.executable.unlink()
        os.link(self.original, self.executable)
        with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[]):
            with self.assertRaises(RuntimeError):
                with self.deny():
                    pass
        self.assertEqual(stat.S_IMODE(self.original.stat().st_mode), 0o755)

    def test_only_the_current_ready_session_copy_can_be_denied(self):
        historical = self.executable.parents[4] / "desktop-recovery-historical/zwai.app/Contents/MacOS/zwai"
        historical.parent.mkdir(parents=True)
        historical.write_bytes(self.original.read_bytes())
        historical.chmod(0o755)
        (historical.parents[3] / "session").write_text("e" * 16)
        with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[]):
            with self.deny() as row:
                self.assertEqual(row["path"], str(self.executable))
                self.assertEqual(stat.S_IMODE(historical.stat().st_mode), 0o755)
            (self.executable.parents[3] / "session").write_text("f" * 16)
            with self.assertRaises(RuntimeError):
                with self.deny():
                    self.fail("another session was selected")

    def test_symlink_or_nonprivate_case_never_admits_a_permission_fault(self):
        for mutation in ("symlink", "public-case"):
            with self.subTest(mutation=mutation), mock.patch.object(fault, "require_guest"), \
                    mock.patch.object(fault, "_live_executables", return_value=[]):
                if mutation == "symlink":
                    self.executable.unlink()
                    self.executable.symlink_to(self.original)
                else:
                    self.executable.unlink()
                    self.executable.write_bytes(self.original.read_bytes())
                    self.executable.chmod(0o755)
                    self.root.chmod(0o755)
                with self.assertRaises(RuntimeError):
                    with self.deny():
                        self.fail("unowned copy was modified")
                self.assertEqual(stat.S_IMODE(self.original.stat().st_mode), 0o755)
        self.root.chmod(0o700)

    def test_a_live_private_macho_executable_is_never_modified(self):
        subprocess.run(["/usr/bin/cc", "-x", "c", "-o", str(self.executable), "-"],
                       input="#include <unistd.h>\nint main(void){write(1,\"ready\\n\",6);for(;;)pause();}\n",
                       text=True, capture_output=True, check=True, timeout=30)
        child = subprocess.Popen([str(self.executable)], stdout=subprocess.PIPE)
        try:
            # proc_pidpath can precede dyld startup. The receipt proves this is a
            # live executable before the permission guard and fixture teardown.
            self.assertTrue(select.select([child.stdout], [], [], 3)[0])
            self.assertEqual(child.stdout.readline(), b"ready\n")
            live = fault.process_identity(child.pid)
            self.assertEqual(live["executable"], str(self.executable))
            with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[live]):
                with self.assertRaisesRegex(RuntimeError, "live executable"):
                    with self.deny():
                        self.fail("live executable was admitted")
            self.assertEqual(stat.S_IMODE(self.executable.stat().st_mode), 0o755)
        finally:
            child.terminate()
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=3)
            child.stdout.close()

    def test_permission_only_cleanup_restores_the_same_copy_or_accepts_its_removal(self):
        with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[]):
            with self.deny():
                result = fault.cleanup_case(self.evidence, self.case, self.app)
                self.assertTrue(result["complete"])
                self.assertEqual(result["targets"], [])
                self.assertEqual(result["copies"][0]["outcome"], "restored")
                self.assertEqual(stat.S_IMODE(self.executable.stat().st_mode), 0o755)
            self.executable.unlink()
            result = fault.cleanup_case(self.evidence, self.case, self.app, require_exit=True)
            self.assertEqual(result["copies"][0]["outcome"], "removed")

    def test_valid_permission_ledger_cannot_hide_a_dangling_target_ledger(self):
        with mock.patch.object(fault, "require_guest"), mock.patch.object(fault, "_live_executables", return_value=[]):
            with self.deny():
                (self.evidence / fault.TARGETS).symlink_to(self.evidence / "missing")
                with self.assertRaises(RuntimeError):
                    fault.cleanup_case(self.evidence, self.case, self.app, require_exit=True)
                self.assertFalse(os.access(self.executable, os.X_OK))
            self.assertTrue(os.access(self.executable, os.X_OK))


class InventoryTests(unittest.TestCase):
    def test_only_current_uid_pids_are_inspected_and_unknown_same_uid_is_not_skipped(self):
        uid = os.getuid()
        output = "100 " + str(uid) + "\n101 " + str(uid + 1) + "\n102 " + str(uid) + "\n"
        with mock.patch.object(fault.subprocess, "check_output", return_value=output) as ps, \
                mock.patch.object(fault, "process_identity", side_effect=[dict(pid=100), RuntimeError("unavailable")]) as identity:
            with self.assertRaisesRegex(RuntimeError, "unavailable"):
                list(fault._live_executables())
            self.assertEqual([call.args[0] for call in identity.call_args_list], [100, 102])
            self.assertIn("pid=,uid=", ps.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
