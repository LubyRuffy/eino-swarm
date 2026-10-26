"""Readiness refuses only this caller before allocating native case state."""
import base64
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("native_macos_smoke.py")
spec = importlib.util.spec_from_file_location("smoke_readiness", SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)
runner = smoke.runner


class ReadinessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.helper = self.root / "prepared-helper"
        self.app = self.root / "candidate.app"
        executable = self.app / "Contents/MacOS/fixture"
        executable.parent.mkdir(parents=True)
        executable.write_text("#!" + sys.executable + "\nraise SystemExit(0)\n")
        executable.chmod(0o755)
        (self.app / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleExecutable": "fixture"}))
        self.lock = self.root / "guest-case.lock"
        self.previous = {"cleanup_complete": True, "remaining_owned_pids": [], "case_id": "previous"}
        self.lock.write_text(json.dumps(self.previous))
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(runner, "LOCK_PATH", self.lock).start()
        mock.patch.object(runner, "require_guest").start()
        self.identity = mock.patch.object(smoke, "validate_helper", return_value={
            "binary": str(self.helper), "binary_sha256": "prepared"}).start()

    def observation(self, case_id="current", **changes):
        return dict(schema_version=1, case_id=case_id, uid=os.getuid(), session_uid=os.getuid(),
                    on_console=True, login_done=True, lock_key_present=True, locked=False,
                    ax_trusted=True, screen_capture_allowed=True, **changes)

    def invoke(self, *, omit=(), **changes):
        evidence = self.root / ("evidence-" + str(time.monotonic_ns()))
        def probe(command, **kwargs):
            self.assertEqual(command[:2], [str(self.helper), "--preflight"])
            self.assertGreater(kwargs["timeout"], 0)
            self.assertLessEqual(kwargs["timeout"], 5)
            with self.lock.open() as peer:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(peer, fcntl.LOCK_EX | fcntl.LOCK_NB)
            row = self.observation(command[2])
            row.update(changes)
            for key in omit:
                row.pop(key)
            return subprocess.CompletedProcess(command, 0, json.dumps(row), "")
        with mock.patch.object(smoke.subprocess, "run", side_effect=probe) as native, \
             mock.patch.object(runner, "copy_app", side_effect=RuntimeError("copy boundary reached")) as copy, \
             mock.patch.object(runner, "stop_owned", return_value=[]), \
             mock.patch.object(runner.subprocess, "Popen") as launch:
            code = smoke.main(["--case", "--ax", str(self.helper), "--app", str(self.app),
                               "--evidence", str(evidence), "--budget", "8"])
        return code, evidence, json.loads((evidence / "cleanup.json").read_text()), native, copy, launch

    def test_actual_entry_refuses_unready_route_before_copy_or_launch_under_lock(self):
        for changes, reason in (({"ax_trusted": False}, "current_invocation_accessibility_unavailable"),
                                ({"screen_capture_allowed": False}, "current_invocation_screen_capture_unavailable"),
                                ({"locked": True}, "current_guest_locked"),
                                ({"session_uid": -1}, "current_console_login_unready"),
                                ({"on_console": False}, "current_console_login_unready"),
                                ({"login_done": None}, "current_console_login_unready")):
            with self.subTest(changes=changes):
                before = self.lock.read_bytes()
                code, evidence, receipt, native, copy, launch = self.invoke(**changes)
                self.assertEqual(code, 2)
                native.assert_called_once()
                copy.assert_not_called()
                launch.assert_not_called()
                self.assertFalse(receipt["launched"])
                self.assertNotIn("case_root", receipt)
                self.assertTrue(receipt["cleanup_complete"])
                self.assertEqual(receipt["failure_kind"], "native_preflight")
                self.assertEqual(receipt["preflight"]["status"], "blocked")
                self.assertIn(reason, receipt["preflight"]["blockers"])
                self.assertEqual(json.loads((evidence / "native-preflight.json").read_text()), receipt["preflight"])
                self.assertEqual(stat.S_IMODE((evidence / "native-preflight.json").stat().st_mode), 0o600)
                self.assertGreater(receipt["timings_seconds"]["preflight"], 0)
                self.assertEqual(self.lock.read_bytes(), before)

    def test_unknown_lock_stays_unknown_and_never_becomes_native_acceptance(self):
        code, _, receipt, _, copy, launch = self.invoke(lock_key_present=False, locked=None)
        copy.assert_called_once()
        launch.assert_not_called()
        self.assertEqual(code, 2)  # The deliberately inert copy boundary fails.
        self.assertEqual(receipt["preflight"]["status"], "ready_for_attempt")
        self.assertEqual(receipt["preflight"]["login_lock_state"], "unknown")
        self.assertFalse(receipt["preflight"]["native_behavior_accepted"])

    def test_new_invocation_observes_changed_terminal_route_instead_of_reusing_ssh_denial(self):
        first = self.invoke(ax_trusted=False)[2]
        second = self.invoke()[2]
        self.assertEqual(first["preflight"]["status"], "blocked")
        self.assertEqual(second["preflight"]["status"], "ready_for_attempt")
        self.assertNotEqual(first["case_id"], second["case_id"])
        self.assertEqual(self.identity.call_count, 2)

    def test_malformed_or_foreign_observations_refuse(self):
        for changes in ({"schema_version": True}, {"case_id": "previous"}, {"uid": -1},
                        {"ax_trusted": 1}, {"lock_key_present": False, "locked": False},
                        {"lock_key_present": True, "locked": None}):
            row = self.observation()
            row.update(changes)
            with self.subTest(changes=changes), self.assertRaisesRegex(ValueError, "malformed"):
                smoke.readiness_blockers(row, "current")

    def test_invalid_prepared_helper_refuses_without_probe_copy_or_launch(self):
        with mock.patch.object(smoke, "validate_helper", side_effect=RuntimeError("helper bytes changed")):
            code, _, receipt, native, copy, launch = self.invoke()
        self.assertEqual(code, 2)
        native.assert_not_called()
        copy.assert_not_called()
        launch.assert_not_called()
        self.assertIn("helper bytes changed", receipt["failure"])
        self.assertEqual(receipt["failure_kind"], "native_preflight")

    def test_actual_case_missing_nullable_lock_field_is_classified_and_never_copies(self):
        code, evidence, receipt, native, copy, launch = self.invoke(lock_key_present=False, omit=("locked",))
        self.assertEqual(code, 2)
        native.assert_called_once()
        copy.assert_not_called()
        launch.assert_not_called()
        self.assertEqual(receipt["failure_kind"], "native_preflight")
        self.assertEqual(receipt["preflight"]["status"], "failed")
        self.assertIn("malformed", receipt["failure"])
        self.assertIn("malformed", json.loads((evidence / "native-preflight.json").read_text())["failure"])

    def test_preflight_evidence_is_private_and_never_replaces_existing_or_linked_evidence(self):
        path = self.root / "private-readiness.json"
        smoke.save_preflight(path, {"scope": "current_invocation"})
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        original = path.read_bytes()
        alias = self.root / "linked-readiness.json"
        alias.symlink_to(path)
        for target in (path, alias):
            with self.subTest(target=target), self.assertRaises(OSError):
                smoke.save_preflight(target, {"scope": "replacement"})
            self.assertEqual(path.read_bytes(), original)

    def test_failed_probe_evidence_keeps_the_original_failure_and_blocks_a_ready_route(self):
        with mock.patch.object(smoke, "save_preflight", side_effect=OSError("preflight evidence disk failed")):
            code, _, receipt, _, copy, launch = self.invoke(ax_trusted=False)
        self.assertEqual(code, 2)
        self.assertIn("current_invocation_accessibility_unavailable", receipt["failure"])
        self.assertIn("disk failed", receipt["preflight_evidence_error"])
        copy.assert_not_called()
        launch.assert_not_called()
        with mock.patch.object(smoke, "save_preflight", side_effect=OSError("preflight evidence disk failed")):
            code, _, receipt, _, copy, launch = self.invoke()
        self.assertEqual(code, 2)
        self.assertEqual(receipt["preflight"]["status"], "failed")
        self.assertIn("evidence could not be saved", receipt["failure"])
        copy.assert_not_called()
        launch.assert_not_called()

    def test_evidence_failure_does_not_replace_original_timeout_or_budget_error(self):
        evidence = self.root / "direct-errors"
        evidence.mkdir()
        error = subprocess.TimeoutExpired([str(self.helper)], 1, output=b"original")
        for cause, deadline in ((error, time.monotonic() + 5),
                                (runner.CaseBudgetExpired("original budget"), 0)):
            receipt = {"case_id": "current"}
            with self.subTest(cause=type(cause).__name__), \
                 mock.patch.object(smoke.subprocess, "run", side_effect=cause), \
                 mock.patch.object(smoke, "save_preflight", side_effect=OSError("disk failed")), \
                 self.assertRaises(type(cause)) as raised:
                smoke.case_preflight(self.helper, receipt, evidence, deadline)
            self.assertIn("disk failed", receipt["preflight_evidence_error"])
            if isinstance(cause, subprocess.TimeoutExpired):
                self.assertIs(raised.exception, cause)

    def test_probe_timeout_keeps_command_deadline_and_original_streams(self):
        evidence = self.root / "timeout-evidence"
        error = subprocess.TimeoutExpired([str(self.helper), "--preflight", "case"], .01,
                                          output=b"partial stdout", stderr=b"partial stderr")
        with mock.patch.object(smoke.subprocess, "run", side_effect=error), \
             mock.patch.object(runner, "copy_app") as copy, \
             mock.patch.object(runner.subprocess, "Popen") as launch:
            code = smoke.main(["--case", "--ax", str(self.helper), "--app", str(self.app),
                               "--evidence", str(evidence)])
        self.assertEqual(code, 2)
        copy.assert_not_called()
        launch.assert_not_called()
        receipt = json.loads((evidence / "cleanup.json").read_text())
        timeout = receipt["timeouts"][0]
        self.assertEqual(timeout["command"], error.cmd)
        self.assertEqual(timeout["timeout_seconds"], error.timeout)
        self.assertEqual(base64.b64decode(timeout["stdout"]["base64"]), b"partial stdout")
        self.assertEqual(base64.b64decode(timeout["stderr"]["base64"]), b"partial stderr")
        self.assertGreaterEqual(receipt["preflight"]["elapsed_seconds"], 0)

    def test_guest_gate_and_previous_cleanup_latch_run_before_probe(self):
        for guest_error, previous in ((RuntimeError("physical host refused"), self.previous),
                                      (None, {"cleanup_complete": False})):
            with self.subTest(previous=previous):
                self.lock.write_text(json.dumps(previous))
                with mock.patch.object(runner, "require_guest", side_effect=guest_error), \
                     mock.patch.object(smoke.subprocess, "run") as probe, \
                     mock.patch.object(runner, "copy_app") as copy:
                    code = smoke.main(["--case", "--ax", str(self.helper), "--app", str(self.app),
                                       "--evidence", str(self.root / str(time.monotonic_ns()))])
                self.assertEqual(code, 2)
                probe.assert_not_called()
                copy.assert_not_called()

    def test_preflight_consumes_original_budget_without_allocating_a_case(self):
        evidence = self.root / "budget-evidence"
        now = time.monotonic()
        def expire(*args, **kwargs):
            time.sleep(.06)
            return subprocess.CompletedProcess(args[0], 0, json.dumps(self.observation(args[0][2])), "")
        with mock.patch.object(smoke.subprocess, "run", side_effect=expire), \
             mock.patch.object(runner, "copy_app") as copy:
            code = smoke.main(["--case", "--ax", str(self.helper), "--app", str(self.app),
                               "--evidence", str(evidence), "--budget", ".05"])
        receipt = json.loads((evidence / "cleanup.json").read_text())
        self.assertEqual(code, 124)
        self.assertTrue(receipt["budget_exceeded"])
        self.assertGreater(receipt["timings_seconds"]["preflight"], .05)
        self.assertNotIn("case_root", receipt)
        copy.assert_not_called()
        self.assertLess(time.monotonic() - now, 1)


@unittest.skipUnless(sys.platform == "darwin" and Path("/usr/bin/xcrun").exists(), "macOS SDK required")
class NativeLeafTests(unittest.TestCase):
    def test_actual_helper_preflight_branch_emits_nullable_current_observations_without_ui(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "preflight-fixture.m"
            source.write_text('''#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#include <sys/sysctl.h>
#include <unistd.h>
static NSString *state(void) { return NSProcessInfo.processInfo.environment[@"READINESS_FIXTURE"] ?: @"ready"; }
static int guestSysctl(const char *name, void *value, size_t *size, void *newValue, size_t newSize) {
    *(int *)value=1; *size=sizeof(int); return 0;
}
static Boolean trusted(void) { return ![state() isEqual:@"accessibility-denied"]; }
static bool capture(void) { return ![state() isEqual:@"capture-denied"]; }
static CFDictionaryRef fixtureCurrentSession(void) {
    NSMutableDictionary *value=[@{@"kCGSSessionUserIDKey":@(getuid()),
        @"kCGSSessionOnConsoleKey":@YES, @"kCGSessionLoginDoneKey":@YES,
        @"CGSSessionScreenIsLocked":@NO} mutableCopy];
    if ([state() isEqual:@"locked"]) value[@"CGSSessionScreenIsLocked"]=@YES;
    if ([state() isEqual:@"unknown"]) [value removeObjectForKey:@"CGSSessionScreenIsLocked"];
    return (__bridge_retained CFDictionaryRef)value;
}
static AXUIElementRef forbidUI(pid_t pid) { abort(); }
#define sysctlbyname guestSysctl
#define AXIsProcessTrusted trusted
#define CGPreflightScreenCaptureAccess capture
#define CGSessionCopyCurrentDictionary fixtureCurrentSession
#define AXUIElementCreateApplication forbidUI
#define main helper_main
#include ''' + json.dumps(str(smoke.AX_SOURCE.resolve())) + '''
#undef main
int main(int argc, const char **argv) { return helper_main(argc,argv); }
''')
            binary = root / "preflight-fixture"
            compiled = subprocess.run(["/usr/bin/xcrun", "clang", *smoke.AX_FLAGS, str(source), "-o", str(binary)],
                                      capture_output=True, text=True, timeout=30)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            for state, changes in (("ready", {}), ("accessibility-denied", {"ax_trusted": False}),
                                   ("capture-denied", {"screen_capture_allowed": False}),
                                   ("locked", {"locked": True}),
                                   ("unknown", {"lock_key_present": False, "locked": None})):
                with self.subTest(state=state):
                    result = subprocess.run([str(binary), "--preflight", "fresh-case"],
                                            env=dict(os.environ, READINESS_FIXTURE=state),
                                            capture_output=True, text=True, check=True, timeout=5)
                    row = json.loads(result.stdout)
                    self.assertEqual(row["case_id"], "fresh-case")
                    self.assertEqual(row["uid"], os.getuid())
                    for key, value in changes.items():
                        self.assertIs(row[key], value)
                    blockers = smoke.readiness_blockers(row, "fresh-case")
                    self.assertEqual(bool(blockers), state not in ("ready", "unknown"))


if __name__ == "__main__":
    unittest.main()
