import importlib.util
import json
from pathlib import Path
import unittest
import errno
import os
import subprocess
import time
import tempfile
import sys
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("android_account_native", Path(__file__).with_name("android-account-native.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class NativeLogMetadataTest(unittest.TestCase):
    def test_native_case_requires_success_and_one_owned_evidence_directory(self):
        good = "INSTRUMENTATION_STATUS: evidence_directory=/sdcard/Android/data/com.lubyruffy.zwai/files/native-account\nOK (1 test)\n"
        self.assertEqual(runner.native_result_directory(good), "/sdcard/Android/data/com.lubyruffy.zwai/files/native-account")
        for bad in ("OK (1 test)", good + good, good + "FAILURES", good.replace("OK (1 test)", "INSTRUMENTATION_ABORTED")):
            with self.subTest(output=bad), self.assertRaises(AssertionError):
                runner.native_result_directory(bad)

    def test_sdk_options_results_and_exception_payloads_are_not_retained(self):
        raw = b'''Capacitor: To native (Capacitor plugin): callbackId: 12, pluginId: Browser, methodName: open
Capacitor: callbackId: 13, pluginId: StreamBody, methodName: open, methodData: {"headers":{"Authorization":"Bearer private-token"},"url":"http://127.0.0.1/?ticket=secret","body":"private-payload"}
Capacitor/Console: native App.getInfo (#14)
Capacitor/Console: result App.getInfo (#14) {"data":"private-result"}
Capacitor: To native (Capacitor plugin): callbackId: 15, pluginId: AppLauncher, methodName: openUrl, methodData: {"url":"http://127.0.0.1/?cookie=secret"}
ActivityTaskManager: START com.capacitorjs.plugins.browser.BrowserControllerActivity https://private/?cookie=secret
java.lang.NullPointerException: private-payload
arbitrary private-token cookie=secret header=secret
'''
        retained = runner.native_log_metadata(raw)
        rows = json.loads(retained)
        self.assertEqual(rows[0], {"event": "native_call", "callback_id": "12", "plugin": "Browser", "method": "open"})
        self.assertIn({"event": "browser_controller_activity"}, rows)
        self.assertIn({"event": "exception", "category": "null_pointer"}, rows)
        self.assertTrue(any(row.get("event") == "result" and row.get("method") == "getInfo" for row in rows))
        self.assertIn({"event": "native_call", "callback_id": "15", "plugin": "AppLauncher", "method": "openUrl"}, rows)
        for sensitive in (b"private", b"Bearer", b"ticket", b"cookie", b"headers", b"http", b"payload", b"data"):
            self.assertNotIn(sensitive, retained)


class OwnedCleanupTest(unittest.TestCase):
    def test_missing_runtime_never_accepts_an_unknown_group_or_unjoined_child(self):
        # Deleting private data is not evidence that the owned processes exited.
        cleanup = {"owned_runtime_removed": True, "ports_closed": True,
                   "artifact_bytes_unchanged": True, "owned_process_groups_gone": True,
                   "owned_processes": [{"pid": 123, "returncode": 0}]}
        self.assertTrue(runner.owned_cleanup_verified(cleanup))
        cleanup["owned_process_groups_gone"] = False
        self.assertFalse(runner.owned_cleanup_verified(cleanup))
        cleanup["owned_process_groups_gone"] = True
        cleanup["owned_processes"][0]["returncode"] = None
        self.assertFalse(runner.owned_cleanup_verified(cleanup))

    def test_transient_unknown_group_is_observed_again_before_acceptance(self):
        # An EPERM sample is unknown, never proof the owned group disappeared.
        child = subprocess.Popen(["/usr/bin/true"], start_new_session=True)
        child.wait(timeout=2)
        with patch.object(runner.os, "killpg", side_effect=[PermissionError(errno.EPERM, "unknown"), ProcessLookupError(errno.ESRCH, "gone")]) as probe:
            closed_ports, groups_gone, observations = runner.wait_owned_cleanup([child], [], time.monotonic() + .5)
        self.assertTrue(closed_ports)
        self.assertTrue(groups_gone)
        self.assertEqual(probe.call_count, 2)
        self.assertEqual(observations[0]["errno"], errno.EPERM)
        self.assertEqual(observations[0]["pid"], child.pid)

    def test_persistent_unknown_group_refuses_cleanup_at_original_deadline(self):
        child = subprocess.Popen(["/usr/bin/true"], start_new_session=True)
        child.wait(timeout=2)
        with patch.object(runner.os, "killpg", side_effect=PermissionError(errno.EPERM, "unknown")):
            closed_ports, groups_gone, observations = runner.wait_owned_cleanup([child], [], time.monotonic() + .03)
        self.assertTrue(closed_ports)
        self.assertFalse(groups_gone)
        self.assertGreaterEqual(len(observations), 1)
        self.assertTrue(all(row["errno"] == errno.EPERM for row in observations))

    def test_owned_listening_port_refuses_even_after_process_groups_are_gone(self):
        with runner.socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            number = listener.getsockname()[1]
            self.assertEqual(runner.wait_owned_cleanup([], [number], time.monotonic())[:2], (False, True))
        self.assertEqual(runner.wait_owned_cleanup([], [number], time.monotonic())[:2], (True, True))

    def test_actual_live_group_refuses_and_reaped_group_is_gone(self):
        child = subprocess.Popen(["/bin/sleep", "10"], start_new_session=True)
        try:
            closed_ports, groups_gone, observations = runner.wait_owned_cleanup([child], [], time.monotonic())
            self.assertTrue(closed_ports)
            self.assertFalse(groups_gone)
            self.assertEqual(observations, [])
        finally:
            os.killpg(child.pid, 15)
            child.wait(timeout=2)
        self.assertEqual(runner.wait_owned_cleanup([child], [], time.monotonic())[:2], (True, True))


class FinishOwnedCaseTest(unittest.TestCase):
    def case_inputs(self, base):
        out = base / "evidence"
        out.mkdir(mode=0o700)
        runtime = out / "runtime"
        runtime.mkdir(mode=0o700)
        artifact = base / "artifact"
        artifact.write_bytes(b"immutable fixture")
        root = base / "lease-root"
        root.mkdir()
        (root / "go.mod").write_text("module example.com/owned-case-test\n")
        lock_root = base / "mutex"
        lock_root.mkdir()
        lease_spec = importlib.util.spec_from_file_location(
            "owned_case_host_lease", Path(__file__).resolve().parents[2] / "tools/release_host_lease.py")
        lease_module = importlib.util.module_from_spec(lease_spec)
        with patch.object(sys, "path", [str(Path(__file__).resolve().parents[2] / "tools"), *sys.path]):
            lease_spec.loader.exec_module(lease_module)
        lease = lease_module.HostValidationLease(root, out, timeout=1, lock_root=lock_root,
                                                inventory=lambda *_: {"status": "clear", "processes": []})
        lease.__enter__()
        return dict(processes=[], streams=[], commands=[], artifacts={"apk": artifact},
                    hashes={"apk": runner.sha(artifact)}, ports=[], runtime=runtime, out=out,
                    result={"native_case_passed": True, "formal_acceptance": False}, lease=lease,
                    primary=None, development=False, dirty=False, started=time.monotonic())

    def test_stop_error_preserves_business_failure_and_still_joins_other_owned_child(self):
        with tempfile.TemporaryDirectory() as directory:
            inputs = self.case_inputs(Path(directory))
            later = subprocess.Popen(["/bin/sleep", "10"], start_new_session=True)
            first = subprocess.Popen(["/bin/sleep", "10"], start_new_session=True)
            inputs["processes"] = [later, first]
            inputs["commands"] = [{"label": "later", "pid": later.pid}, {"label": "first", "pid": first.pid}]
            original = RuntimeError("original business failure")
            inputs["primary"] = original
            real_killpg = os.killpg
            commands_seen = []

            def denied_after_exit(pid, sig):
                if pid == first.pid and sig == 15:
                    commands_seen.append((inputs["out"] / "commands.json").is_file())
                    real_killpg(pid, sig)
                    first.wait(timeout=2)
                    raise PermissionError(errno.EPERM, "injected stop failure")
                return real_killpg(pid, sig)

            try:
                with patch.object(runner.os, "killpg", side_effect=denied_after_exit):
                    try:
                        error = runner.finish_owned_case(**inputs)
                    except BaseException as raised:
                        error = raised
                self.assertIs(error, original)
                self.assertEqual(commands_seen, [True])
                self.assertIsNotNone(later.returncode)
                self.assertIsNone(inputs["lease"].stream)
                result = json.loads((inputs["out"] / "result.json").read_text())
                self.assertFalse(result["formal_acceptance"])
                self.assertEqual(result["failure"]["message"], str(original))
                self.assertEqual(result["cleanup_errors"][0]["phase"], "stop_child")
                self.assertTrue((inputs["out"] / "owned-process-exits.json").is_file())
            finally:
                for child in [first, later]:
                    if child.poll() is None:
                        real_killpg(child.pid, 9)
                        child.wait(timeout=2)
                if inputs["lease"].stream is not None:
                    inputs["lease"].__exit__(None, None, None)

    def test_success_requires_joined_child_removed_runtime_and_released_real_lease(self):
        with tempfile.TemporaryDirectory() as directory:
            inputs = self.case_inputs(Path(directory))
            child = subprocess.Popen(["/bin/sleep", "10"], start_new_session=True)
            inputs["processes"] = [child]
            try:
                self.assertIsNone(runner.finish_owned_case(**inputs))
                self.assertIsNotNone(child.returncode)
                self.assertIsNone(inputs["lease"].stream)
                result = json.loads((inputs["out"] / "result.json").read_text())
                self.assertTrue(result["formal_acceptance"])
                self.assertTrue(result["lease_exit_completed"])
                self.assertTrue(result["cleanup_complete"])
                self.assertFalse(inputs["runtime"].exists())
                self.assertEqual(json.loads(inputs["lease"].receipt.read_text())["status"], "released")
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, 9)
                    child.wait(timeout=2)
                if inputs["lease"].stream is not None:
                    inputs["lease"].__exit__(None, None, None)

    def test_real_lease_receipt_failure_cannot_leave_positive_formal_result(self):
        with tempfile.TemporaryDirectory() as directory:
            inputs = self.case_inputs(Path(directory))
            child = subprocess.Popen(["/bin/sleep", "10"], start_new_session=True)
            inputs["processes"] = [child]
            inputs["commands"] = [{"label": "owned", "pid": child.pid}]
            failure = OSError(errno.EIO, "injected lease receipt failure")
            try:
                with patch.object(inputs["lease"], "_save", side_effect=failure):
                    try:
                        error = runner.finish_owned_case(**inputs)
                    except BaseException as raised:
                        error = raised
                self.assertIs(error, failure)
                self.assertIsNotNone(child.returncode)
                self.assertIsNone(inputs["lease"].stream)
                result = json.loads((inputs["out"] / "result.json").read_text())
                self.assertFalse(result["formal_acceptance"])
                self.assertFalse(result["lease_exit_completed"])
                self.assertEqual(result["cleanup_errors"][-1]["phase"], "lease_exit")
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, 9)
                    child.wait(timeout=2)
                if inputs["lease"].stream is not None:
                    inputs["lease"].__exit__(None, None, None)


if __name__ == "__main__":
    unittest.main()
