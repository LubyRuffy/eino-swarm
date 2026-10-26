"""Restore evidence uses the real harness/HTTP adapter; AX and time are OS seams."""
import ast
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("restore_observation_driver", ROOT / "native_macos_resilience.py")
h = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = h
SPEC.loader.exec_module(h)


class RestoreObservationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                test = cls.active
                test.calls.append(("api", self.path))
                if self.path == "/api/meta":
                    body = json.dumps(test.meta).encode()
                elif self.path == "/api/composer/drafts/thread%3Ath_saved":
                    body = json.dumps(dict(draft=test.draft)).encode()
                elif self.path == "/api/composer/drafts/thread%3Ath_saved/files/file-id":
                    body = test.file_bytes
                else:
                    self.send_error(404)
                    return
                self.send_response(200)
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def setUp(self):
        type(self).active = self
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        (root / "data").mkdir()
        (root / "evidence").mkdir()
        self.context = dict(case_id="restore-case", app=root / "zwai.app", app_pid=41,
                            activated_pid=41, engine_pid=42, helper="/private/own-helper",
                            data=root / "data", evidence=root / "evidence", thread_id="th_saved",
                            origin="http://127.0.0.1:" + str(self.server.server_port))
        self.meta = dict(data_dir=str(root / "data"), mock=True,
                         clients=[dict(surface="desktop", pid=41)])
        self.file_bytes = b"private attachment\x00\xff"
        self.draft = dict(key="thread:th_saved", revision=7, text="private body 中文  ",
                          quotes=[dict(id="quote-id", text="private quote")], state="editing",
                          files=[dict(id="file-id", name="input.bin", kind="file",
                                      mime="application/octet-stream", size=len(self.file_bytes))])
        self.expected = {key: copy.deepcopy(self.draft[key]) for key in ("text", "quotes", "files", "state")}
        self.expected["files"][0]["sha256"] = hashlib.sha256(self.file_bytes).hexdigest()
        self.snapshot = dict(textareas=1, values=[self.expected["text"]], text="input.bin",
                             has_web_area=True, pid=41, case_id="restore-case")
        self.calls, self.effects = [], []
        self.owned = True
        self.clock = 1000.0
        (root / "data/engine.json").write_text(json.dumps(dict(pid=42, url=self.context["origin"])))
        for active in (
            patch.object(h.runner, "process_identity", side_effect=self.identity),
            patch.object(h, "_run_native_helper", side_effect=self.native_process),
            patch.object(h.time, "monotonic", side_effect=lambda: self.clock),
            patch.object(h.time, "sleep", side_effect=self.sleep),
        ):
            active.start()
            self.addCleanup(active.stop)

    def identity(self, pid):
        self.calls.append(("identity", pid))
        return [str(self.context["app"] / "Contents/MacOS/zwai")], "restore-case" if self.owned else "foreign"

    def native_process(self, argv, **kwargs):
        self.assertEqual(kwargs, dict(capture_output=True, timeout=8))
        self.calls.append(("native", argv[1], argv[2], argv[3]))
        effect = self.effects.pop(0) if self.effects else copy.deepcopy(self.snapshot)
        if isinstance(effect, BaseException):
            raise effect
        if isinstance(effect, subprocess.CompletedProcess):
            return effect
        return subprocess.CompletedProcess(argv, 0, json.dumps(effect).encode("utf-8"), b"binary diagnostic")

    def sleep(self, seconds):
        self.clock += seconds

    def receipts(self):
        return [json.loads(path.read_text()) for path in sorted(self.context["evidence"].glob("draft-restore-*.json"))]

    def verify(self, timeout=.4):
        return h.verify_draft(self.context, self.expected, timeout)

    def fail_observation_write(self, error):
        original = h.atomic_json
        def write(path, value):
            if Path(path).name.startswith("draft-restore-"):
                raise error
            return original(path, value)
        return write

    def assert_binary_summary(self, summary, raw):
        self.assertEqual(summary, dict(type="bytes", length=len(raw), byte_length=len(raw),
                                      sha256=hashlib.sha256(raw).hexdigest()))

    def test_success_preserves_actual_http_native_call_order_and_marks_not_false(self):
        self.assertEqual(self.verify(), self.expected)
        rows = self.receipts()
        self.assertEqual(len(rows), 1)
        row = rows[0]
        self.assertEqual(row["outcome"], "verified")
        self.assertIsNone(row["first_false"])
        self.assertTrue(row["last"]["predicates"]["saved_snapshot"])
        self.assertEqual(self.calls, [
            ("api", "/api/meta"), ("identity", 41), ("identity", 41),
            ("native", "snapshot", "41", ""), ("identity", 41),
            ("native", "snapshot", "41", ""),
            ("api", "/api/composer/drafts/thread%3Ath_saved"),
            ("api", "/api/composer/drafts/thread%3Ath_saved/files/file-id"),
        ])
        self.assertEqual(row["last"]["api"]["key"], "thread:th_saved")
        self.assertEqual(row["last"]["api"]["revision"], 7)

    def test_false_textarea_retains_existing_current_shell_row_without_new_draft_api(self):
        self.snapshot["textareas"] = 0
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]
        self.assertFalse(row["first_false"]["predicates"]["native_textareas"])
        self.assertEqual(row["last"]["predicates"]["native_values"], "not_read")
        self.assertEqual(row["last"]["predicates"]["saved_snapshot"], "not_read")
        self.assertTrue(all(call[1] == "/api/meta" for call in self.calls if call[0] == "api"))

    def test_false_values_keeps_api_not_read_and_summary_never_contains_body(self):
        self.effects = [self.snapshot, dict(self.snapshot, values=["different body"])] * 3
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]
        self.assertFalse(row["first_false"]["predicates"]["native_values"])
        self.assertEqual(row["last"]["predicates"]["saved_snapshot"], "not_read")
        self.assertNotIn("private body", json.dumps(row))
        self.assertNotIn("different body", json.dumps(row))
        stream = row["last"]["native_calls"][-1]["stdout"]
        self.assert_binary_summary(stream, json.dumps(dict(self.snapshot, values=["different body"])).encode("utf-8"))

    def test_first_false_and_last_are_separate_and_later_invocation_does_not_overwrite(self):
        self.effects = [dict(self.snapshot, textareas=0), self.snapshot, self.snapshot]
        self.verify()
        first_paths = {path: path.read_bytes() for path in self.context["evidence"].glob("draft-restore-*.json")}
        first = self.receipts()[0]
        self.assertFalse(first["first_false"]["predicates"]["native_textareas"])
        self.assertTrue(first["last"]["predicates"]["verified"])
        self.verify()
        self.assertEqual(len(self.receipts()), 2)
        for path, data in first_paths.items():
            self.assertEqual(path.read_bytes(), data)

    def test_snapshot_difference_is_original_business_failure_with_exact_false_predicate(self):
        self.draft["state"] = "uncertain"
        with self.assertRaisesRegex(RuntimeError, "changed unsent"):
            self.verify()
        row = self.receipts()[0]
        self.assertFalse(row["last"]["predicates"]["saved_snapshot"])
        self.assertEqual(row["last"]["predicates"]["attachment_names"], "not_read")
        self.assertEqual(row["last"]["error"]["origin"], "draft_snapshot")

    def test_missing_visible_filename_does_not_make_a_saved_snapshot_match_into_success(self):
        self.snapshot["text"] = "no attachment shown"
        with self.assertRaises(TimeoutError):
            self.verify()
        self.assertTrue(self.receipts()[0]["last"]["predicates"]["saved_snapshot"])
        self.assertFalse(self.receipts()[0]["last"]["predicates"]["attachment_names"])

    def test_missing_candidate_is_false_not_an_empty_native_snapshot(self):
        self.meta["clients"] = []
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertFalse(row["predicates"]["current_shell"])
        self.assertEqual(row["predicates"]["native_textareas"], "not_read")
        self.assertEqual(row["native_calls"], [])

    def test_foreign_candidate_is_not_a_native_read_or_navigation(self):
        self.owned = False
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertFalse(row["predicates"]["candidate_owned"])
        self.assertFalse(any(call[0] == "native" for call in self.calls))

    def test_metadata_and_lease_failures_keep_their_original_categories(self):
        for field in ("metadata", "lease"):
            with self.subTest(field=field):
                self.meta["data_dir"] = "/foreign" if field == "metadata" else str(self.context["data"])
                self.context["data"].joinpath("engine.json").write_text(json.dumps(
                    dict(pid=42 if field == "metadata" else 999, url=self.context["origin"])))
                with self.assertRaisesRegex(RuntimeError, "identity|engine"):
                    self.verify()
                row = self.receipts()[-1]["last"]
                self.assertEqual(row["error"]["origin"], field)

    def test_native_nonzero_is_retried_as_before_with_safe_binary_stream_summary(self):
        self.effects = [subprocess.CompletedProcess(["ignored"], 2, b"private stdout", b"private stderr")] * 3
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertEqual(row["error"]["origin"], "current_shell_native")
        self.assertEqual(row["error"]["type"], "OSError")
        self.assertEqual(row["native_calls"][-1]["returncode"], 2)
        self.assert_binary_summary(row["native_calls"][-1]["stdout"], b"private stdout")
        self.assert_binary_summary(row["native_calls"][-1]["stderr"], b"private stderr")
        self.assertNotIn("private stdout", json.dumps(row))
        self.assertNotIn("private stderr", json.dumps(row))

    def test_keyboardinterrupt_keeps_original_instance_cause_and_last_successful_read(self):
        error = KeyboardInterrupt()
        cause = RuntimeError("original cause")
        error.__cause__ = cause
        self.effects = [self.snapshot, error]
        with self.assertRaises(KeyboardInterrupt) as caught:
            self.verify()
        self.assertIs(caught.exception, error)
        self.assertIs(caught.exception.__cause__, cause)
        row = self.receipts()[0]["last"]
        self.assertTrue(row["predicates"]["native_textareas"])
        self.assertEqual(row["predicates"]["native_values"], "error")
        self.assertEqual(row["error"]["origin"], "verify_native")

    def test_timeout_streams_are_bytes_summary_and_original_wait_cause_is_preserved(self):
        error = subprocess.TimeoutExpired(["private helper"], 8, output=b"\x00\xffprivate", stderr=None)
        # current_shell deliberately swallows its native error; the verifier's
        # second read is instead caught by the original wait_for.
        self.effects = [self.snapshot, error] * 3
        with self.assertRaises(TimeoutError) as caught:
            self.verify()
        self.assertIs(caught.exception.__cause__, error)
        row = self.receipts()[0]["last"]
        self.assertEqual(row["native_calls"][-1]["stdout"]["type"], "bytes")
        self.assertEqual(row["native_calls"][-1]["stderr"]["type"], "none")
        self.assertEqual(row["error"]["type"], "TimeoutExpired")

    def test_unknown_values_type_keeps_original_false_predicate_instead_of_observer_error(self):
        self.effects = [self.snapshot, dict(self.snapshot, values=None)] * 3
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertFalse(row["predicates"]["native_values"])
        self.assertEqual(row["native_values"]["type"], "NoneType")
        self.assertEqual(row["predicates"]["saved_snapshot"], "not_read")

    def test_failed_evidence_serialization_never_replaces_primary_exception(self):
        error = KeyboardInterrupt()
        self.effects = [error]
        with patch.object(h, "atomic_json", side_effect=self.fail_observation_write(TypeError("observation serialization"))) as output:
            with self.assertRaises(KeyboardInterrupt) as caught:
                self.verify()
        self.assertIs(caught.exception, error)
        self.assertEqual(sum(Path(call.args[0]).name.startswith("draft-restore-") for call in output.call_args_list), 1)

    def test_transient_current_shell_error_is_first_false_and_success_has_no_stale_error(self):
        error = OSError(5, "native read unavailable")
        self.effects = [error, self.snapshot, self.snapshot]
        self.verify()
        row = self.receipts()[0]
        self.assertEqual(row["first_false"]["error"]["errno"], 5)
        self.assertEqual(row["first_false"]["error"]["origin"], "current_shell_native")
        self.assertTrue(row["last"]["predicates"]["verified"])
        self.assertNotIn("error", row["last"])

    def test_original_activation_remains_single_and_uses_the_actual_adapter(self):
        del self.context["activated_pid"]
        self.verify()
        self.assertEqual([call[1] for call in self.calls if call[0] == "native"],
                         ["activate", "snapshot", "snapshot"])
        self.assertEqual(self.context["activated_pid"], 41)
        self.assertEqual([call["action"] for call in self.receipts()[0]["last"]["native_calls"]],
                         ["activate", "snapshot", "snapshot"])

    def test_outer_interrupt_during_original_sleep_keeps_last_false_poll(self):
        self.snapshot["textareas"] = 0
        error = KeyboardInterrupt()
        with patch.object(h.time, "sleep", side_effect=error):
            with self.assertRaises(KeyboardInterrupt) as caught:
                self.verify()
        self.assertIs(caught.exception, error)
        row = self.receipts()[0]
        self.assertFalse(row["last"]["predicates"]["native_textareas"])
        self.assertEqual(row["last"], row["first_false"])
        self.assertEqual(row["error"]["type"], "KeyboardInterrupt")

    def test_native_json_failure_preserves_decode_error_origin_and_binary_summary(self):
        self.effects = [subprocess.CompletedProcess(["ignored"], 0, b"private malformed JSON", b"")] * 3
        with self.assertRaises(TimeoutError):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertEqual(row["error"]["type"], "JSONDecodeError")
        self.assertEqual(row["predicates"]["native_textareas"], "error")
        self.assert_binary_summary(row["native_calls"][0]["stdout"], b"private malformed JSON")
        self.assertNotIn("private malformed", json.dumps(row))

    def test_missing_lease_is_error_not_false_or_an_additional_api_read(self):
        self.context["data"].joinpath("engine.json").unlink()
        with self.assertRaises(TimeoutError) as caught:
            self.verify()
        self.assertIsInstance(caught.exception.__cause__, FileNotFoundError)
        row = self.receipts()[0]["last"]
        self.assertEqual(row["predicates"]["engine_lease"], "error")
        self.assertEqual(row["error"]["errno"], 2)
        self.assertEqual(row["predicates"]["native_textareas"], "not_read")
        self.assertTrue(all(call[0] == "api" and call[1] == "/api/meta" for call in self.calls))

    def test_malformed_api_quotes_keep_original_file_reads_and_snapshot_failure(self):
        for value in (None, False, 3.25, "private malformed quote", [None, "private quote"],
                      {"private": "quote"}):
            with self.subTest(kind=type(value).__name__):
                self.calls.clear()
                self.draft["quotes"] = value
                with self.assertRaisesRegex(RuntimeError, "recovery changed unsent"):
                    self.verify()
                self.assertEqual([call[1] for call in self.calls if call[0] == "api"],
                    ["/api/meta", "/api/composer/drafts/thread%3Ath_saved",
                     "/api/composer/drafts/thread%3Ath_saved/files/file-id"])
                row = self.receipts()[-1]["last"]
                self.assertFalse(row["predicates"]["saved_snapshot"])
                self.assertEqual(row["api"]["quotes_type"], type(value).__name__)
                for body in ("private malformed quote", "private quote"):
                    self.assertNotIn(body, json.dumps(row))

    def test_malformed_file_item_summary_does_not_preempt_original_download_error(self):
        self.draft["files"] = [None]
        with self.assertRaisesRegex(TypeError, "not subscriptable"):
            self.verify()
        row = self.receipts()[0]["last"]
        self.assertEqual(row["error"]["type"], "TypeError")
        self.assertEqual(row["api"]["files"][0]["type"], "none")

    def test_unknown_float_metadata_does_not_make_observation_serialization_a_new_failure(self):
        self.draft["revision"] = float("nan")
        self.draft["files"][0]["size"] = float(len(self.file_bytes))
        # The original comparison ignores revision and treats numerically equal
        # int/float sizes equally. The observer must not introduce validation.
        self.assertEqual(self.verify(), self.expected)
        row = self.receipts()[0]["last"]["api"]
        self.assertEqual(row["revision"]["type"], "float")
        self.assertEqual(row["files"][0]["size"]["type"], "float")

    def test_structured_metadata_is_typed_without_new_business_validation_or_body_copy(self):
        private = {"body": "private arbitrary metadata"}
        self.draft.update(key=private, revision=private)
        self.assertEqual(self.verify(), self.expected)
        row = self.receipts()[0]["last"]["api"]
        self.assertEqual(row["key"]["type"], "dict")
        self.assertEqual(row["revision"]["type"], "dict")
        self.assertNotIn("private", json.dumps(row))
        for field in ("mime", "kind"):
            with self.subTest(field=field):
                self.draft["files"][0][field] = private
                with self.assertRaisesRegex(RuntimeError, "changed unsent"):
                    self.verify()
                row = self.receipts()[-1]["last"]["api"]
                self.assertEqual(row["files"][0][field]["type"], "dict")
                self.assertNotIn("private", json.dumps(row))
                self.draft["files"][0][field] = self.expected["files"][0][field]

    def test_primary_exception_survives_observation_write_and_stderr_write_failures(self):
        error = KeyboardInterrupt()
        cause = RuntimeError("same cause")
        error.__cause__ = cause
        self.effects = [error]
        with patch.object(h, "atomic_json", side_effect=self.fail_observation_write(OSError("disk"))) as output, \
                patch("builtins.print", side_effect=OSError("stderr")):
            with self.assertRaises(KeyboardInterrupt) as caught:
                self.verify()
        self.assertIs(caught.exception, error)
        self.assertIs(caught.exception.__cause__, cause)
        self.assertNotIn("_restore_observation", self.context)
        self.assertEqual(sum(Path(call.args[0]).name.startswith("draft-restore-") for call in output.call_args_list), 1)

    def test_success_cannot_pass_if_mandatory_observation_cannot_be_written(self):
        with patch.object(h, "atomic_json", side_effect=self.fail_observation_write(OSError("disk"))) as output:
            with self.assertRaisesRegex(OSError, "disk"):
                self.verify()
        self.assertNotIn("_restore_observation", self.context)
        self.assertEqual(sum(Path(call.args[0]).name.startswith("draft-restore-") for call in output.call_args_list), 1)

    def test_observation_is_only_written_after_original_wait_exits(self):
        original, original_wait = h.atomic_json, h.wait_for
        wait_exited, observer_writes, native_writes = False, [], []
        def wait(check, deadline):
            nonlocal wait_exited
            try:
                return original_wait(check, deadline)
            finally:
                wait_exited = True
        def write(path, value):
            if Path(path).name.startswith("draft-restore-"):
                self.assertTrue(wait_exited)
                self.assertEqual(self.calls[-1], ("api", "/api/composer/drafts/thread%3Ath_saved/files/file-id"))
                observer_writes.append(path)
            else:
                self.assertFalse(wait_exited)
                self.assertEqual(Path(path).parent.parent.name, "native-actions")
                self.assertEqual(Path(path).name, "receipt.json")
                self.assertEqual(self.calls[-1], ("native", value["action"], "41", ""))
                native_writes.append(path)
            return original(path, value)
        with patch.object(h, "atomic_json", side_effect=write) as output, patch.object(h, "wait_for", side_effect=wait):
            self.verify()
        self.assertEqual(len(observer_writes), 1)
        self.assertEqual(len(native_writes), 2)
        self.assertEqual(output.call_count, 3)

    def test_standalone_current_shell_keeps_original_call_contract_without_receipt(self):
        self.assertEqual(h.current_shell(self.context), 41)
        self.assertEqual(self.receipts(), [])

    def test_runtime_sources_remain_python39_parseable(self):
        for name in ("native_macos_resilience.py", "native_macos_resilience_phases.py"):
            ast.parse((ROOT / name).read_text(), filename=name, feature_version=(3, 9))


if __name__ == "__main__":
    unittest.main(verbosity=2)
