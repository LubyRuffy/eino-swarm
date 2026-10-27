"""Pure fixtures exercise ordinary readiness through the real smoke entry."""
import base64
from contextlib import ExitStack
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import stat
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import urllib.error
import zlib


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("readiness_smoke", HERE / "native_macos_smoke.py")
smoke = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(smoke)


def image_bytes(blank=False, color=0, depth=8, pixel=0):
    def chunk(kind, data):
        return struct.pack(">I", len(data))+kind+data+struct.pack(">I", zlib.crc32(kind+data))
    channels = {0: 1, 2: 3, 4: 2, 6: 4}[color]
    line = bytearray()
    for x in range(100):
        value = pixel if blank else (x % 16) * 16
        for channel in range(channels):
            line.extend(bytes([value]) if depth == 8 else bytes([value, 0]))
    scanlines = (b"\0"+line)*100
    return (b"\x89PNG\r\n\x1a\n"+chunk(b"IHDR", struct.pack(">IIBBBBB", 100, 100, depth, color, 0, 0, 0))
            +chunk(b"IDAT", zlib.compress(scanlines))+chunk(b"IEND", b""))


class ReadinessTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        case = self.root / "case"
        case.mkdir()
        for name in ("data", "home", "workspace", "candidate.app"):
            (case / name).mkdir()
        (self.root / "evidence").mkdir()
        self.context = dict(case_id="case-fixture", app_pid=123, app=case / "candidate.app",
                            data=case / "data", home=case / "home", workspace=case / "workspace",
                            evidence=self.root / "evidence", ax=self.root / "prepared-helper", timeout=2)
        self.context["ax"].write_bytes(b"prepared fixture")
        self.executable = self.context["app"] / "Contents/MacOS/zwai"
        self.executable.parent.mkdir(parents=True)
        self.executable.write_bytes(b"byte-identified fixture")
        (self.context["app"] / "Contents/Info.plist").write_bytes(plistlib.dumps({}))
        self.origin = "http://127.0.0.1:4567"
        self.clock = mock.Mock(return_value=10)
        self.deadline = smoke.Deadline(2, clock=self.clock)
        self.environment = {"ZWAI_NATIVE_"+name: str(self.context[key]) for name, key in (
            ("APP", "app"), ("DATA", "data"), ("WORKSPACE", "workspace"), ("EVIDENCE", "evidence"))}
        self.environment.update(ZWAI_NATIVE_CASE_ID=self.context["case_id"], ZWAI_NATIVE_APP_PID="123",
                                ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS="2", ZWAI_HOME=str(self.context["data"]),
                                HOME=str(self.context["home"]))
        (self.context["data"] / "engine.json").write_text(json.dumps(dict(url=self.origin, mock=True, pid=456)))
        self.metadata = dict(mock=True, data_dir=str(self.context["data"]), version="0.1.45",
                             clients=[dict(surface="desktop", pid=123)])

    def row(self, **changes):
        return dict(dict(pid=123, ready=True, origin=self.origin, bundle=str(self.context["app"]),
                         case_verified=True, title="fixture", frontmost=True, has_web_area=True,
                         rect=[10, 20, 100, 100], textareas=1, area_values=[""],
                         send_count=1, send_enabled=False, window_count=1,
                         window_id=17, window_owner_pid=123, windows=[dict(id=17, owner_pid=123)],
                         screen_capture_allowed=True, expected_visible=False,
                         web_urls=[self.origin+"/?shell=desktop&pid=123"]), **changes)

    def patches(self):
        stack = ExitStack()
        self.addCleanup(stack.close)
        stack.enter_context(mock.patch.object(smoke, "assert_process", return_value=self.executable))
        stack.enter_context(mock.patch.object(smoke, "validate_helper", return_value=dict(binary_sha256="prepared")))
        stack.enter_context(mock.patch.object(smoke.runner, "require_guest"))
        stack.enter_context(mock.patch.object(smoke, "Deadline", return_value=self.deadline))
        stack.enter_context(mock.patch.object(smoke.time, "sleep", side_effect=self.advance))
        return stack

    def advance(self, seconds):
        self.clock.return_value += seconds

    def test_recovery_journal_gate_is_reproduced_but_actual_smoke_reaches_business_without_it(self):
        # Reproduce the mistaken adapter, including its actual ready callback.
        # This does not weaken automatic recovery's independent journal gate.
        with mock.patch.object(sys, "path", [str(HERE), *sys.path]):
            spec = importlib.util.spec_from_file_location("readiness_resilience", HERE / "native_macos_resilience.py")
            harness = importlib.util.module_from_spec(spec)
            with mock.patch.dict(sys.modules, {spec.name: harness}):
                spec.loader.exec_module(harness)
                def bounded(probe, deadline):
                    self.assertIsNone(probe())
                    raise TimeoutError("recovery journal missing")
                with mock.patch.dict(os.environ, self.environment), \
                        mock.patch.object(harness, "api", return_value=self.metadata), \
                        mock.patch.object(harness, "native", return_value={}) as old_native, \
                        mock.patch.object(harness, "wait_for", side_effect=bounded), \
                        mock.patch.object(harness.background, "retain"), \
                        self.assertRaisesRegex(TimeoutError, "journal missing"):
                    harness.context_for(dict(helper="fixture", package=dict(version="0.1.45")), 2)
                self.assertEqual([call.args[1] for call in old_native.call_args_list], ["activate"])
        self.assertFalse((self.context["data"] / "diagnostics").exists())
        stack, actions, message = self.patches(), [], []
        def request(origin, path, deadline):
            self.assertIs(deadline, self.deadline)
            return self.metadata if path == "/api/meta" else dict(threads=[])
        def native(command, **kwargs):
            self.advance(.01)
            if command[0] == "/usr/sbin/screencapture":
                Path(command[-1]).write_bytes(image_bytes())
                return subprocess.CompletedProcess(command, 0, "", "")
            action = command[2]
            actions.append(action)
            row = self.row()
            if action == "fill":
                message.append(command[-1])
                row["filled"] = True
            elif action == "send":
                row["submitted"] = True
            if message:
                row.update(area_values=[message[0]], send_enabled=True)
            return subprocess.CompletedProcess(command, 0, json.dumps(row), "")
        stack.enter_context(mock.patch.object(smoke, "request_json", side_effect=request))
        stack.enter_context(mock.patch.object(smoke.subprocess, "run", side_effect=native))
        stack.enter_context(mock.patch.object(smoke, "wait_trace", side_effect=RuntimeError("business assertion sentinel")))
        with mock.patch.dict(os.environ, self.environment):
            code = smoke.main(["--ax", str(self.context["ax"])])
        self.assertEqual(code, 2)
        receipt = json.loads((self.context["evidence"] / "smoke.json").read_text())
        self.assertEqual(receipt["failure_kind"], "native_assertion_refused")
        self.assertEqual(receipt["failure"], "business assertion sentinel")
        self.assertFalse(receipt["retryable"])
        self.assertEqual(actions, ["snapshot", "fill", "snapshot", "send"])
        initial = json.loads((self.context["evidence"] / "smoke-initial.json").read_text())
        self.assertTrue(initial["pixels"]["content_visible"])
        self.assertFalse(initial["recovery_accepted"])
        self.assertEqual(initial["shared_deadline_monotonic"], self.deadline.ends)
        self.assertEqual(stat.S_IMODE((self.context["evidence"] / "smoke.json").stat().st_mode), 0o600)

    def test_unknown_snapshot_timeout_retains_exact_streams_and_is_never_replayed(self):
        self.patches()
        context = dict(self.context, origin=self.origin)
        original = subprocess.TimeoutExpired(["actual-helper"], 1, output=b"\xff"+b"x"*1000, stderr=b"first failure")
        with mock.patch.object(smoke.subprocess, "run", side_effect=original) as run, \
                self.assertRaises(subprocess.TimeoutExpired) as caught:
            smoke.wait_window(context, self.deadline)
        self.assertIs(caught.exception, original)
        run.assert_called_once()
        record = json.loads((context["evidence"] / "smoke-native-call-0001.json").read_text())
        self.assertEqual(base64.b64decode(record["streams"]["stdout"]["base64"]), original.stdout)
        self.assertEqual(base64.b64decode(record["streams"]["stderr"]["base64"]), original.stderr)
        self.assertEqual(record["failure_kind"], "native_observation_timeout")
        self.assertEqual(record["shared_deadline_monotonic"], self.deadline.ends)
        self.assertIn("argv", record)
        self.assertIn("elapsed_seconds", record)

    def test_evidence_errors_keep_the_original_failure_and_attempt_both_receipts(self):
        self.patches()
        original = subprocess.TimeoutExpired(["helper"], 1, output=b"original")
        with mock.patch.object(smoke.subprocess, "run", side_effect=original) as run, \
                mock.patch.object(smoke.readiness, "save_json", side_effect=OSError("storage failed")) as save, \
                self.assertRaises(subprocess.TimeoutExpired) as caught:
            smoke.native_snapshot(dict(self.context, origin=self.origin), self.deadline)
        self.assertIs(caught.exception, original)
        self.assertEqual(save.call_count, 2)
        run.assert_called_once()

    def test_missing_success_evidence_refuses_instead_of_repeating_the_input(self):
        self.patches()
        with mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "{}", "")) as run, \
                mock.patch.object(smoke.readiness, "save_json", side_effect=OSError("storage failed")), \
                self.assertRaises(smoke.readiness.Failure) as caught:
            smoke.native_snapshot(dict(self.context, origin=self.origin), self.deadline, action="send", message="fixture")
        self.assertEqual(caught.exception.failure_kind, "native_evidence_failed")
        run.assert_called_once()

    def test_nonready_rows_cannot_hide_wrong_identity_permissions_or_ambiguous_windows(self):
        self.patches()
        context = dict(self.context, origin=self.origin)
        for change, kind in ((dict(pid=124), "native_identity_refused"),
                             (dict(case_id="other-case"), "native_identity_refused"),
                             (dict(case_verified=False), "native_identity_refused"),
                             (dict(web_urls=[self.origin+"/?shell=desktop&pid=124"]), "native_identity_refused"),
                             (dict(screen_capture_allowed=False), "native_permission_refused"),
                             (dict(window_owner_pid=124), "native_window_refused"),
                             (dict(windows=[dict(id=17, owner_pid=124)]), "native_window_refused"),
                             (dict(window_count=2), "native_window_refused")):
            with self.subTest(change=change), \
                    mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(self.row(ready=False, **change)), "")) as run, \
                    self.assertRaises(smoke.readiness.Failure) as caught:
                smoke.wait_window(context, self.deadline)
            self.assertEqual(caught.exception.failure_kind, kind)
            run.assert_called_once()

    def test_blank_pixels_poll_only_readonly_observations_under_the_original_deadline(self):
        stack = self.patches()
        actions, timeouts = [], []
        self.deadline.ends = self.clock.return_value + .4
        original_end = self.deadline.ends
        def native(command, **kwargs):
            timeouts.append(kwargs["timeout"])
            self.advance(.05)
            if command[0] == "/usr/sbin/screencapture":
                Path(command[-1]).write_bytes(image_bytes(blank=True))
                return subprocess.CompletedProcess(command, 0, "", "")
            actions.append(command[2])
            return subprocess.CompletedProcess(command, 0, json.dumps(self.row()), "")
        stack.enter_context(mock.patch.object(smoke, "request_json", return_value=self.metadata))
        stack.enter_context(mock.patch.object(smoke.subprocess, "run", side_effect=native))
        with self.assertRaises(TimeoutError):
            smoke.wait_initial_ready(self.context, self.deadline)
        self.assertTrue(actions)
        self.assertEqual(set(actions), {"snapshot"})
        self.assertEqual(original_end, self.deadline.ends)
        self.assertTrue(all(a > b for a, b in zip(timeouts, timeouts[1:])))
        rows = json.loads((self.context["evidence"] / "smoke-readiness.json").read_text())["observations"]
        self.assertEqual({row["reason"] for row in rows}, {"ordinary_window_pixels_pending"})

    def test_http_permission_and_malformed_lease_are_not_transient(self):
        self.patches()
        denied = urllib.error.HTTPError(self.origin, 403, "refused", {}, None)
        self.addCleanup(denied.close)
        with mock.patch.object(smoke, "request_json", side_effect=denied) as request, \
                self.assertRaises(smoke.readiness.Failure) as caught:
            smoke.wait_engine(self.context, self.deadline)
        self.assertEqual(caught.exception.failure_kind, "native_http_refused")
        request.assert_called_once()
        (self.context["data"] / "engine.json").write_text("{partial")
        with self.assertRaises(json.JSONDecodeError):
            smoke.wait_engine(self.context, self.deadline)

    def test_initial_pixel_fixture_checks_light_dark_blank_and_decoded_bytes(self):
        path = self.root / "pixels.png"
        for color, depth in ((0, 8), (2, 8), (4, 8), (6, 8), (6, 16)):
            with self.subTest(color=color, depth=depth):
                path.write_bytes(image_bytes(color=color, depth=depth))
                self.assertEqual(smoke.png_dimensions(path), [100, 100])
                self.assertTrue(smoke.readiness.pixel_observation(path)["content_visible"])
        for value in (0, 255):
            raw = image_bytes(blank=True, pixel=value)
            path.write_bytes(raw)
            self.assertFalse(smoke.readiness.pixel_observation(path)["content_visible"])
        path.write_bytes(raw[:-12])
        with self.assertRaises(RuntimeError):
            smoke.png_dimensions(path)

    def test_filtered_scanlines_have_the_same_nonblank_content_as_unfiltered_pixels(self):
        # Fixed grayscale rows use each PNG filter. Up and Paeth encode a
        # repeated prior row entirely as zeros; decoding must recover its ink.
        path = self.root / "filtered.png"
        raw = image_bytes()
        line = bytes((x % 16)*16 for x in range(100))
        left = bytes([0])+line[:-1]
        rows = [b"\0"+line, b"\1"+bytes((a-b) & 255 for a, b in zip(line, left)),
                b"\2"+bytes(100), b"\3"+bytes((a-(a+b)//2) & 255 for a, b in zip(line, left)),
                b"\4"+bytes(100)]
        payload = b"".join(rows)*20
        def chunk(kind, data):
            return struct.pack(">I", len(data))+kind+data+struct.pack(">I", zlib.crc32(kind+data))
        path.write_bytes(raw[:33]+chunk(b"IDAT", zlib.compress(payload))+chunk(b"IEND", b""))
        self.assertEqual(smoke.png_dimensions(path), [100, 100])
        observed = smoke.readiness.pixel_observation(path)
        self.assertTrue(observed["content_visible"])
        self.assertEqual(observed["buckets"], 16)
        self.assertAlmostEqual(observed["dominant_fraction"], .07)


if __name__ == "__main__":
    unittest.main()
