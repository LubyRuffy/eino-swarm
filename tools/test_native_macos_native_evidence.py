"""Keep every real native helper result before parsing or later case cleanup."""
import base64
from contextlib import redirect_stderr
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("native_evidence_harness", Path(__file__).with_name("native_macos_resilience.py"))
harness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = harness
SPEC.loader.exec_module(harness)


class NativeEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.helper = self.root / "helper"
        self.helper.write_text("#!" + sys.executable + "\nimport os,sys\nos.write(1,b'{\"active\":true}\\n')\nos.write(2,b'diagnostic'+b'x'*2000+b' TAIL_REASON\\n')\nsys.exit(2 if sys.argv[1]=='refuse' else 0)\n")
        self.helper.chmod(0o700)
        self.context = dict(app_pid=41, case_id="owned-case", app=self.root / "zwai.app",
                            helper=str(self.helper), evidence=self.root)
        owner = mock.patch.object(harness.runner, "process_identity", return_value=(
            [str(self.context["app"] / "Contents/MacOS/zwai")], "owned-case"))
        owner.start()
        self.addCleanup(owner.stop)

    def receipts(self):
        return [json.loads(path.read_text()) for path in sorted((self.root / "native-actions").glob("*/receipt.json"))]

    def stream(self, row, key, expected, kind="bytes"):
        self.assertEqual(row[key]["type"], kind)
        self.assertEqual(base64.b64decode(row[key]["base64"]), expected)

    def test_actual_successes_keep_distinct_full_receipts_before_json_parsing(self):
        for _ in range(2):
            self.assertEqual(harness.native(self.context, "activate"), dict(active=True))
        rows = self.receipts()
        self.assertEqual(len(rows), 2)
        for row in rows:
            self.assertEqual(row["requested_argv"], [str(self.helper), "activate", "41", ""])
            self.assertEqual((row["action"], row["case_id"], row["pid"]), ("activate", "owned-case", 41))
            self.assertEqual((row["returncode"], row["timeout_seconds"], row["exception_type"]), (0, 8, None))
            self.stream(row, "stdout", b'{"active":true}\n')
            self.stream(row, "stderr", b"diagnostic" + b"x" * 2000 + b" TAIL_REASON\n")
        self.assertFalse((self.root / "native-result.json").exists())

    def test_actual_nonzero_keeps_reason_beyond_the_old_exception_truncation(self):
        with self.assertRaises(OSError) as caught:
            harness.native(self.context, "refuse")
        row, = self.receipts()
        self.assertEqual(row["returncode"], 2)
        self.stream(row, "stderr", b"diagnostic" + b"x" * 2000 + b" TAIL_REASON\n")
        self.assertTrue(str(caught.exception).startswith("native refuse: diagnostic"))

    def test_bad_json_keeps_original_completed_bytes_before_parser_refusal(self):
        argv = [str(self.helper), "snapshot", "41", ""]
        result = subprocess.CompletedProcess(argv, 0, b"not-json\x00\xff", b"parser diagnostic\xff")
        original = []
        def parse(raw):
            self.assertEqual(len(self.receipts()), 1)
            try:
                return json.loads(raw)
            except (json.JSONDecodeError, UnicodeDecodeError) as error:
                original.append(error)
                raise
        with mock.patch.object(harness, "_run_native_helper", return_value=result) as run, \
                mock.patch.object(harness, "json", SimpleNamespace(loads=parse, dump=json.dump)):
            with self.assertRaises((json.JSONDecodeError, UnicodeDecodeError)) as caught:
                harness.native(self.context)
        self.assertIs(caught.exception, original[0])
        self.assertEqual(run.call_count, 1)
        row, = self.receipts()
        self.stream(row, "stdout", result.stdout)
        self.stream(row, "stderr", result.stderr)

    def test_timeout_keeps_binary_and_none_streams_and_the_same_exception(self):
        argv = [str(self.helper), "activate", "41", ""]
        for stdout, stderr in [(b"\x00\xffout", b"err\x00\xfe"), (None, b"")]:
            error = subprocess.TimeoutExpired(argv, 8, output=stdout, stderr=stderr)
            cause = RuntimeError("original timeout cause")
            error.__cause__ = cause
            with mock.patch.object(harness, "_run_native_helper", side_effect=error) as run:
                with self.assertRaises(subprocess.TimeoutExpired) as caught:
                    harness.native(self.context, "activate")
            self.assertIs(caught.exception, error)
            self.assertIs(caught.exception.__cause__, cause)
            self.assertEqual(run.call_count, 1)
            self.assertEqual(run.call_args.kwargs, dict(capture_output=True, timeout=8))
        rows = self.receipts()
        self.assertEqual(len(rows), 2)
        by_kind = {row["stdout"]["type"]: row for row in rows}
        self.stream(by_kind["bytes"], "stdout", b"\x00\xffout")
        self.stream(by_kind["bytes"], "stderr", b"err\x00\xfe")
        self.stream(by_kind["none"], "stdout", b"", "none")
        self.stream(by_kind["none"], "stderr", b"")
        for row in rows:
            self.assertEqual(row["exception_type"], "TimeoutExpired")
            self.assertEqual(row["exception_argv"], argv)
            self.assertIsNone(row["returncode"])

    def test_evidence_io_failure_rejects_success_without_masking_nonzero_or_timeout(self):
        failure = OSError("evidence unavailable")
        argv = [str(self.helper), "activate", "41", ""]
        timeout = subprocess.TimeoutExpired(argv, 8, output=b"raw", stderr=None)
        for result in [subprocess.CompletedProcess(argv, 0, b'{"active":true}', b""),
                       subprocess.CompletedProcess(argv, 2, b"", b"original helper failure"), timeout]:
            with self.subTest(kind=type(result).__name__, code=getattr(result, "returncode", None)), \
                    mock.patch.object(harness, "atomic_json", side_effect=failure), \
                    mock.patch.object(harness, "_run_native_helper", **(
                        {"side_effect": result} if isinstance(result, Exception) else {"return_value": result})) as run:
                if result is timeout:
                    with self.assertRaises(subprocess.TimeoutExpired) as caught:
                        harness.native(self.context, "activate")
                    self.assertIs(caught.exception, timeout)
                elif result.returncode:
                    with self.assertRaisesRegex(OSError, "^native activate: original helper failure$"):
                        harness.native(self.context, "activate")
                else:
                    with self.assertRaises(OSError) as caught:
                        harness.native(self.context, "activate")
                    self.assertIs(caught.exception, failure)
                self.assertEqual(run.call_count, 1)

    def test_spawn_failure_keeps_the_same_exception_and_an_attempt_receipt(self):
        error = FileNotFoundError("identified helper absent")
        with mock.patch.object(harness, "_run_native_helper", side_effect=error):
            with self.assertRaises(FileNotFoundError) as caught:
                harness.native(self.context)
        self.assertIs(caught.exception, error)
        row, = self.receipts()
        self.assertEqual(row["exception_type"], "FileNotFoundError")
        self.stream(row, "stdout", b"", "none")

    def test_owner_refusal_never_executes_or_creates_an_action_receipt(self):
        with mock.patch.object(harness.runner, "process_identity", return_value=([str(self.helper)], "other-case")), \
                mock.patch.object(harness, "_run_native_helper") as run:
            with self.assertRaisesRegex(RuntimeError, "lost its private case ownership"):
                harness.native(self.context, "activate")
        run.assert_not_called()
        self.assertEqual(self.receipts(), [])

    def test_helper_elapsed_time_ends_before_evidence_and_parsing(self):
        clock = SimpleNamespace(time=mock.Mock(return_value=1234.5),
                                monotonic=mock.Mock(side_effect=[20.0, 20.75]))
        result = subprocess.CompletedProcess([], 0, b'{"active":true}', b"")
        original_writer = harness.atomic_json
        def write(path, value):
            self.assertEqual(clock.monotonic.call_count, 2)
            original_writer(path, value)
        with mock.patch.object(harness, "time", clock), \
                mock.patch.object(harness, "_run_native_helper", return_value=result), \
                mock.patch.object(harness, "atomic_json", side_effect=write):
            self.assertEqual(harness.native(self.context, "activate"), dict(active=True))
        row, = self.receipts()
        self.assertEqual((row["started_at"], row["elapsed_seconds"]), (1234.5, .75))
        self.assertIsNone(row["exception_timeout_seconds"])
        clock.time.assert_called_once_with()

    def test_actual_timeout_retains_its_original_budget_and_real_elapsed_time(self):
        actual = []
        def boundary(argv, **options):
            self.assertEqual(options, dict(capture_output=True, timeout=8))
            try:
                subprocess.run([sys.executable, "-c", "import os,time;os.write(1,b'partial');time.sleep(1)"],
                               capture_output=True, timeout=.05)
            except subprocess.TimeoutExpired as error:
                actual.append(error)
                raise
        before = time.time()
        with mock.patch.object(harness, "_run_native_helper", side_effect=boundary):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:
                harness.native(self.context, "activate")
        after = time.time()
        self.assertIs(caught.exception, actual[0])
        row, = self.receipts()
        self.assertEqual((row["timeout_seconds"], row["exception_timeout_seconds"]), (8, .05))
        self.assertEqual(row["exception_argv"], actual[0].cmd)
        self.assertLessEqual(before, row["started_at"])
        self.assertLessEqual(row["started_at"], after)
        self.assertGreaterEqual(row["elapsed_seconds"], .05)
        self.assertLessEqual(row["elapsed_seconds"], after - before)

    def test_keyboard_cancellation_keeps_same_exception_and_known_or_absent_streams(self):
        for known in (False, True):
            error = KeyboardInterrupt("original cancellation")
            cause = RuntimeError("original cause")
            error.__cause__ = cause
            if known:
                error.stdout, error.stderr = b"known partial\xff", b""
            with mock.patch.object(harness, "_run_native_helper", side_effect=error):
                with self.assertRaises(KeyboardInterrupt) as caught:
                    harness.native(self.context, "activate")
            self.assertIs(caught.exception, error)
            self.assertIs(caught.exception.__cause__, cause)
        rows = self.receipts()
        self.assertEqual(len(rows), 2)
        by_kind = {row["stdout"]["type"]: row for row in rows}
        self.stream(by_kind["bytes"], "stdout", b"known partial\xff")
        self.stream(by_kind["bytes"], "stderr", b"")
        self.stream(by_kind["none"], "stdout", b"", "none")
        self.stream(by_kind["none"], "stderr", b"", "none")
        self.assertTrue(all(row["exception_type"] == "KeyboardInterrupt" for row in rows))

    def test_failed_helper_receipt_io_warns_without_private_details_or_overriding_failure(self):
        for outcome in (subprocess.CompletedProcess([], 2, b"", b"helper refused"),
                        subprocess.TimeoutExpired([str(self.helper)], 8), KeyboardInterrupt("cancelled")):
            warning = io.StringIO()
            options = dict(side_effect=outcome) if isinstance(outcome, BaseException) else dict(return_value=outcome)
            with mock.patch.object(harness, "_run_native_helper", **options), \
                    mock.patch.object(harness, "atomic_json", side_effect=OSError("private IO body")), \
                    redirect_stderr(warning):
                with self.assertRaises(type(outcome) if isinstance(outcome, BaseException) else OSError) as caught:
                    harness.native(self.context, "activate")
            if isinstance(outcome, BaseException):
                self.assertIs(caught.exception, outcome)
            else:
                self.assertEqual(str(caught.exception), "native activate: helper refused")
            prefix = "WARN native_evidence_write_failed "
            self.assertTrue(warning.getvalue().startswith(prefix))
            self.assertEqual(json.loads(warning.getvalue()[len(prefix):]), dict(action="activate", error_type="OSError"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
