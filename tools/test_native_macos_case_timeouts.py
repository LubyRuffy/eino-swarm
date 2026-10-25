"""Run the actual private-case lifecycle across inventory failures and cancellation."""
import base64
import ctypes
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import platform
import signal
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("case_timeout_runner", Path(__file__).with_name("native_macos_case.py"))
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)
PS = ["/bin/ps", "-axo", "pid=,ppid=,pgid=,uid=,stat=,lstart="]


class NativeCaseTimeoutTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="native-timeout-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "Original.app"
        (self.app / "Contents/MacOS").mkdir(parents=True)
        (self.app / "Contents/Info.plist").write_bytes(plistlib.dumps(dict(CFBundleExecutable="fixture")))
        self.executable = self.app / "Contents/MacOS/fixture"
        self.executable.write_text("#!" + sys.executable + "\nimport time\ntime.sleep(120)\n")
        self.executable.chmod(0o700)
        self.evidence = self.root / "evidence"
        self.lock = self.root / "case.lock"
        self.processes, self.case_roots = [], []
        self.scope = None
        original = runner.ProcessScope.__init__
        def scope_init(scope, bundle, data, processes, case_id=None):
            self.processes = processes
            self.scope = scope
            self.bundle, self.data = bundle, data
            self.case_roots.append(data.parent)
            original(scope, bundle, data, processes, case_id)
        # The inventory is the explicit OS seam: enumerate this fixture's real
        # Popen PIDs, not unrelated user applications. Keep the actual scope
        # parser/ownership logic and real process signals/waits below it.
        process_api = SimpleNamespace(**vars(subprocess))
        process_api.check_output = self.fixture_inventory
        for patch in (mock.patch.object(runner, "LOCK_PATH", self.lock),
                      mock.patch.object(runner, "require_guest"),
                      mock.patch.object(runner, "subprocess", process_api),
                      mock.patch.object(runner.ProcessScope, "__init__", scope_init)):
            patch.start()
            self.addCleanup(patch.stop)
        self.addCleanup(self.reap_fixture)

    def reap_fixture(self):
        # Only Popen objects created by this test's actual run_case are eligible.
        if self.scope is not None:
            runner.stop_owned(self.bundle, self.data, self.processes, scope=self.scope, grace=.1)
        for process in self.processes:
            if process.poll() is None:
                process.kill()
            subprocess.Popen.wait(process, timeout=2)
        for root in self.case_roots:
            if root.exists():
                runner.remove_case_tree(root)

    def fixture_inventory(self, argv, **kwargs):
        self.assertEqual(argv, PS)
        self.assertEqual(kwargs, dict(timeout=5, text=True))
        pids = {process.pid for process in self.processes}
        child = self.evidence / "late-child"
        if child.exists():
            pids.add(int(child.read_text()))
        rows = []
        for pid in pids:
            if platform.system() == "Darwin":
                class BSD(ctypes.Structure):
                    _fields_ = [("ids", ctypes.c_uint32 * 12), ("comm", ctypes.c_char * 16),
                                ("name", ctypes.c_char * 32), ("group", ctypes.c_uint32 * 5),
                                ("nice", ctypes.c_int32), ("seconds", ctypes.c_uint64), ("micros", ctypes.c_uint64)]
                info = BSD()
                lib = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
                if lib.proc_pidinfo(pid, 3, 0, ctypes.byref(info), ctypes.sizeof(info)) != ctypes.sizeof(info) or info.ids[1] == 5:
                    continue
                parent, group, uid, born = info.ids[4], info.group[1], info.ids[5], "%d.%d" % (info.seconds, info.micros)
            else:
                try:
                    fields = Path("/proc", str(pid), "stat").read_text().rsplit(") ", 1)[1].split()
                except FileNotFoundError:
                    continue
                if fields[0] == "Z":
                    continue
                parent, group, uid, born = fields[1], fields[2], os.getuid(), fields[19]
            rows.append("%s %s %s %s S %s" % (pid, parent, group, uid, born))
        return "\n".join(rows)

    def run_case(self, script="import time;time.sleep(120)", timeout=180, budget=240):
        code = runner.run_case(self.app, self.evidence, timeout,
                               [sys.executable, "-c", script], budget=budget)
        return code, json.loads((self.evidence / "cleanup.json").read_text())

    def inventory_failure(self, first, cleanup=None, started=False):
        original = runner.ProcessScope.sample
        issued = False
        def sample(scope):
            nonlocal issued
            if len(scope.processes) == 2 and not issued:
                if started:
                    until = time.monotonic() + 3
                    while not (self.evidence / "actor-started").exists():
                        if time.monotonic() >= until:
                            self.fail("actual scenario did not publish its startup marker")
                        time.sleep(.005)
                issued = True
                raise first
            if issued and cleanup is not None:
                raise cleanup
            return original(scope)
        return mock.patch.object(runner.ProcessScope, "sample", sample)

    def timeout_rows(self):
        return [json.loads(path.read_text()) for path in sorted(self.evidence.glob("runner-timeout-*.json"))]

    def assert_stream(self, row, name, value):
        expected_type = "none" if value is None else "bytes" if isinstance(value, bytes) else "text"
        self.assertEqual(row[name]["type"], expected_type)
        raw = value.encode("utf-8", "surrogatepass") if isinstance(value, str) else value
        self.assertEqual(base64.b64decode(row[name]["base64"]), raw if raw is not None else b"")

    def assert_held_reaped(self):
        self.assertEqual(len(self.processes), 2)
        self.assertTrue(all(process.returncode is not None for process in self.processes))
        self.assertTrue(all(process.poll() is not None for process in self.processes))

    def actor(self, ignore_term=False):
        return ("import os,signal,subprocess,sys,time\nfrom pathlib import Path\n"
                + ("signal.signal(signal.SIGTERM,signal.SIG_IGN)\n" if ignore_term else "")
                + "e=Path(os.environ['ZWAI_NATIVE_EVIDENCE'])\n"
                "(e/'actor-started').write_text(str(os.getpid()))\n"
                "time.sleep(.35)\n(e/'late-business-action').write_text('continued')\n"
                "p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(120)'])\n"
                "(e/'late-child').write_text(str(p.pid))\ntime.sleep(120)\n")

    def test_wait_inventory_timeout_keeps_original_bytes_before_cleanup_and_reaps_held_children(self):
        error = subprocess.TimeoutExpired(PS, 5, output=b"\x00\xffpartial", stderr=None)
        cause = RuntimeError("first observation cause")
        error.__cause__ = cause
        seen = []
        original = runner.recover_renderer_faults
        def fault_cleanup(*args, **kwargs):
            seen.append(self.timeout_rows())
            return original(*args, **kwargs)
        with self.inventory_failure(error), mock.patch.object(runner, "recover_renderer_faults", side_effect=fault_cleanup):
            code, receipt = self.run_case()
        self.assertEqual(code, 2, receipt)
        self.assertEqual(receipt["failure_kind"], "subprocess_timeout")
        self.assertNotEqual(receipt["failure"], "scenario timeout")
        self.assertFalse(receipt["budget_exceeded"])
        self.assertEqual((receipt["scenario_limit_seconds"], receipt["budget_seconds"]), (180, 240))
        self.assertEqual(len(seen[0]), 1, "first timeout evidence must be durable before cleanup")
        row, = self.timeout_rows()
        self.assertEqual((row["command"], row["timeout_seconds"], row["error_type"], row["phase"]),
                         (PS, 5, "TimeoutExpired", "scenario"))
        self.assert_stream(row, "stdout", error.stdout)
        self.assert_stream(row, "stderr", error.stderr)
        self.assertIs(error.__cause__, cause)
        self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
        self.assert_held_reaped()

    def test_second_inventory_timeout_keeps_first_failure_and_cancels_actor_before_unknown_scan(self):
        first = subprocess.TimeoutExpired(PS, 5, output=b"first", stderr=b"")
        second = subprocess.TimeoutExpired(PS, 5, output=None, stderr=b"cleanup")
        with self.inventory_failure(first, cleanup=second, started=True):
            code, receipt = self.run_case(self.actor())
        self.assertEqual(code, 3)
        self.assertEqual(receipt["failure_kind"], "subprocess_timeout")
        self.assertFalse(receipt["cleanup_complete"] or receipt["data_removed"])
        self.assertFalse(json.loads(self.lock.read_text())["cleanup_complete"])
        self.assertFalse(receipt["budget_exceeded"])
        self.assert_held_reaped()
        time.sleep(.45)
        self.assertFalse((self.evidence / "late-business-action").exists())
        self.assertFalse((self.evidence / "late-child").exists())
        rows = {row["phase"]: row for row in self.timeout_rows()}
        self.assertEqual(set(rows), {"scenario", "cleanup"})
        self.assert_stream(rows["scenario"], "stdout", b"first")
        self.assert_stream(rows["cleanup"], "stderr", b"cleanup")
        self.assertIn("cleanup_error", receipt)
        # Unknown inventory remains a refusal even when both directly held children exited.
        blocked = self.root / "blocked"
        with mock.patch.object(runner.subprocess, "Popen") as launch:
            self.assertNotEqual(runner.run_case(self.app, blocked, 10, [sys.executable, "-c", ""]), 0)
        launch.assert_not_called()

    def test_inventory_failure_cannot_leave_direct_scenario_executing_late_business(self):
        with self.inventory_failure(subprocess.TimeoutExpired(PS, 5),
                                    cleanup=subprocess.TimeoutExpired(PS, 5), started=True):
            code, receipt = self.run_case(self.actor())
        self.assertEqual(code, 3)
        time.sleep(.45)
        self.assertFalse((self.evidence / "late-business-action").exists(),
                         "held scenario kept executing after the failed runner returned")
        self.assertFalse((self.evidence / "late-child").exists())
        self.assert_held_reaped()
        self.assertFalse(receipt["cleanup_complete"] or receipt["data_removed"])

    def test_true_scenario_deadline_remains_124_and_has_distinct_timeout_type(self):
        code, receipt = self.run_case(timeout=.1)
        self.assertEqual(code, 124, receipt)
        self.assertEqual(receipt["failure"], "scenario timeout")
        self.assertEqual(receipt["failure_kind"], "scenario_deadline")
        row, = self.timeout_rows()
        self.assertEqual(row["error_type"], "ScenarioDeadlineExpired")
        self.assertEqual(row["phase"], "scenario")
        self.assert_stream(row, "stdout", None)
        self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
        self.assert_held_reaped()

    def test_wait_scenario_does_not_wrap_or_mutate_a_leaf_timeout(self):
        error = subprocess.TimeoutExpired(PS, 5, output="text 中文", stderr=b"\xff")
        process = SimpleNamespace(native_scope=SimpleNamespace(sample=mock.Mock(side_effect=error)))
        with self.assertRaises(subprocess.TimeoutExpired) as caught:
            runner.wait_scenario(process, 180)
        self.assertIs(caught.exception, error)
        self.assertEqual((error.cmd, error.timeout, error.stdout, error.stderr), (PS, 5, "text 中文", b"\xff"))

    def test_timeout_evidence_write_failure_does_not_change_first_error_or_cleanup(self):
        error = subprocess.TimeoutExpired(PS, 5, output="text 中文", stderr=b"\x00\xff")
        with self.inventory_failure(error), \
                mock.patch.object(runner, "write_timeout_receipt", create=True, side_effect=OSError(5, "private disk body")):
            code, receipt = self.run_case()
        self.assertEqual(code, 2)
        self.assertEqual(receipt["failure_kind"], "subprocess_timeout")
        self.assertEqual(receipt["timeout_evidence_errors"][0]["error_type"], "OSError")
        self.assertNotIn("private disk body", json.dumps(receipt))
        self.assertFalse(receipt["timeouts"][0]["saved"])
        self.assert_stream(receipt["timeouts"][0], "stdout", "text 中文")
        self.assert_stream(receipt["timeouts"][0], "stderr", b"\x00\xff")
        self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
        self.assert_held_reaped()

    def test_timeout_argv_accepts_pathlike_and_bytes_without_losing_first_failure(self):
        for index, command in enumerate((Path("/bin/ps"), b"/bin/\xff", [Path("/bin/ps"), b"-axo", b"\xff"],
                                         (Path("/bin/ps"), b"-axo"))):
            with self.subTest(command_type=type(command).__name__):
                self.evidence = self.root / ("argv-evidence-" + str(index))
                error = subprocess.TimeoutExpired(command, 5, output=b"original", stderr=None)
                with self.inventory_failure(error):
                    code, receipt = self.run_case()
                self.assertEqual(code, 2, receipt)
                self.assertEqual(receipt["failure_kind"], "subprocess_timeout")
                row, = self.timeout_rows()
                expected = [os.fsdecode(arg) for arg in command] if isinstance(command, (list, tuple)) else os.fsdecode(command)
                self.assertEqual(row["command"], expected)
                self.assertEqual(row["command_type"], type(command).__name__)
                self.assertEqual(receipt["timeouts"][0]["command"], expected)
                self.assertIs(error.cmd, command)
                self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
                self.assert_held_reaped()

    def test_term_ignoring_held_actor_is_killed_and_reaped_with_original_grace(self):
        error = subprocess.TimeoutExpired(PS, 5)
        with self.inventory_failure(error, started=True):
            code, receipt = self.run_case(self.actor(ignore_term=True))
        self.assertEqual(code, 2, receipt)
        actor = next(row for row in receipt["direct_child_cleanup"] if row["pid"] == self.processes[-1].pid)
        self.assertEqual(actor["signals"], ["TERM", "KILL"])
        self.assertEqual(actor["outcome"], "killed")
        self.assertEqual(actor["returncode"], -signal.SIGKILL)
        self.assertEqual(actor["wait_errors"][0]["error_type"], "TimeoutExpired")
        self.assert_held_reaped()

    def test_already_exited_handles_are_waited_without_signals(self):
        self.executable.write_text("#!" + sys.executable + "\n")
        original = runner.recover_renderer_faults
        def both_exited(*args, **kwargs):
            for process in self.processes:
                process.wait(timeout=2)
                process.terminate = mock.Mock(side_effect=AssertionError("already-exited handle signalled"))
                process.kill = mock.Mock(side_effect=AssertionError("already-exited handle signalled"))
            return original(*args, **kwargs)
        with mock.patch.object(runner, "recover_renderer_faults", side_effect=both_exited):
            code, receipt = self.run_case("")
        self.assertEqual(code, 0, receipt)
        self.assertEqual(len(receipt["direct_child_cleanup"]), 2)
        for row in receipt["direct_child_cleanup"]:
            self.assertEqual((row["outcome"], row["signals"]), ("already_exited", []))
        self.assert_held_reaped()

    def test_term_and_kill_waits_share_one_original_grace_per_phase_across_both_children(self):
        self.executable.write_text("#!" + sys.executable + "\nimport os,signal,time\nfrom pathlib import Path\n"
                                   "signal.signal(signal.SIGTERM,signal.SIG_IGN)\n"
                                   "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'app-started').write_text('ready')\ntime.sleep(120)\n")
        original = runner.recover_renderer_faults
        waits, installed = [], False
        def record_waits(*args, **kwargs):
            nonlocal installed
            if not installed:
                installed = True
                for process in self.processes:
                    wait = process.wait
                    def recorded(*args, _process=process, _wait=wait, **kwargs):
                        waits.append((_process.pid, kwargs["timeout"]))
                        return _wait(*args, **kwargs)
                    process.wait = recorded
            return original(*args, **kwargs)
        with self.inventory_failure(subprocess.TimeoutExpired(PS, 5), started=True), \
                mock.patch.object(runner, "recover_renderer_faults", side_effect=record_waits):
            script = self.actor(ignore_term=True).replace("(e/'actor-started').write_text",
                "\nwhile not (e/'app-started').exists(): time.sleep(.005)\n(e/'actor-started').write_text")
            code, receipt = self.run_case(script)
        self.assertEqual(code, 2, receipt)
        self.assertEqual(len(waits), 4)
        self.assertGreater(waits[0][1], 1.8)
        self.assertLessEqual(waits[1][1], .05, "second child was given a fresh TERM grace")
        self.assertGreater(waits[2][1], 1.8)
        self.assertLessEqual(waits[3][1], waits[2][1], "second child was given a fresh KILL grace")
        self.assertTrue(all(row["signals"] == ["TERM", "KILL"] for row in receipt["direct_child_cleanup"]))
        self.assert_held_reaped()

    def test_original_scope_still_reaps_markerless_group_descendant_after_known_parents_exit(self):
        script = ("import os,subprocess,sys\nfrom pathlib import Path\n"
                  "env={k:v for k,v in os.environ.items() if k!='ZWAI_NATIVE_CASE_ID'}\n"
                  "p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(120)'],env=env)\n"
                  "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'late-child').write_text(str(p.pid))\n")
        code, receipt = self.run_case(script)
        self.assertEqual(code, 0, receipt)
        pid = int((self.evidence / "late-child").read_text())
        self.assertIsNone(runner.process_argv(pid))
        self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
        self.assert_held_reaped()

    def test_direct_wait_error_keeps_false_latch_and_still_reaps_other_held_child(self):
        error = subprocess.TimeoutExpired(PS, 5)
        original = runner.recover_renderer_faults
        installed = False
        def failed_wait(*args, **kwargs):
            nonlocal installed
            if not installed:
                installed = True
                process = self.processes[-1]
                wait = process.wait
                calls = [0]
                def once(*args, **kwargs):
                    calls[0] += 1
                    if calls[0] == 1:
                        raise OSError(5, "private wait body")
                    return wait(*args, **kwargs)
                process.wait = once
            return original(*args, **kwargs)
        with self.inventory_failure(error), mock.patch.object(runner, "recover_renderer_faults", side_effect=failed_wait):
            code, receipt = self.run_case()
        self.assertEqual(code, 3, receipt)
        self.assertFalse(receipt["cleanup_complete"] or receipt["data_removed"])
        self.assertFalse(json.loads(self.lock.read_text())["cleanup_complete"])
        errors = [item for row in receipt["direct_child_cleanup"] for item in row["wait_errors"]]
        self.assertIn("OSError", [row["error_type"] for row in errors])
        self.assertNotIn("private wait body", json.dumps(receipt))
        self.assert_held_reaped()

    def test_application_cancellation_still_cancels_and_reaps_direct_actor(self):
        with self.inventory_failure(KeyboardInterrupt(), started=True):
            code, receipt = self.run_case(self.actor())
        self.assertEqual((code, receipt["failure"]), (130, "interrupted"))
        self.assertTrue(receipt["cleanup_complete"] and receipt["data_removed"])
        self.assert_held_reaped()
        time.sleep(.45)
        self.assertFalse((self.evidence / "late-business-action").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
