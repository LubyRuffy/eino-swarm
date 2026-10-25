"""Actual loopback downloads must survive private case cleanup as evidence."""
import copy
import hashlib
import http.server
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import threading
import struct
import unittest
from unittest import mock
import urllib.error

SPEC = importlib.util.spec_from_file_location("input_evidence_harness", Path(__file__).with_name("native_macos_resilience.py"))
harness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = harness
SPEC.loader.exec_module(harness)


class InputEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.binary = bytes(range(256)) + b"\0native attachment\xff"
        self.image = harness.fixture_png()
        self.files = [dict(id="file-a", name="input.bin", mime="application/macbinary", kind="file", size=len(self.binary)),
                      dict(id="image-a", name="image.png", mime="image/png", kind="image", size=len(self.image))]
        self.draft = dict(key="thread:thread-a", revision=6, text="unsent 中文  ", quotes=[],
                          files=self.files, state="editing")
        self.draft_path = "/api/composer/drafts/thread%3Athread-a"
        self.responses = {
            self.draft_path: (json.dumps(dict(draft=self.draft)).encode(), "application/json"),
            self.draft_path + "/files/file-a": (self.binary, "application/octet-stream"),
            self.draft_path + "/files/image-a": (self.image, "image/png"),
            "/api/threads/thread-a/files": (json.dumps(dict(files=[dict(path="uploads/input.bin", name="input.bin",
                size=len(self.binary), uploaded=True, dir=False)])).encode(), "application/json"),
            "/api/threads/thread-a/download/uploads/input.bin": (self.binary, "application/octet-stream"),
            "/api/threads/thread-a/input-images/img-a": (self.image, "image/png"),
        }
        responses = self.responses
        self.headers = dict(no_sniff="nosniff")
        headers = self.headers
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path not in responses:
                    self.send_error(404)
                    return
                data, mime = responses[self.path]
                self.send_response(200)
                if mime is not None:
                    self.send_header("Content-Type", mime)
                self.send_header("Content-Length", str(len(data)))
                self.send_header("X-Content-Type-Options", headers["no_sniff"])
                self.end_headers()
                self.wfile.write(data)
            def log_message(self, *args):
                pass
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.context = dict(origin="http://127.0.0.1:" + str(self.server.server_port), evidence=self.root,
                            case_id="a" * 32, thread_id="thread-a")

    def capture(self):
        before = harness.draft_snapshot(self.context, evidence_label="before")
        after = harness.draft_snapshot(self.context, evidence_label="after")
        self.assertEqual(before, after)
        harness.atomic_json(self.root / "draft-before.json", before)
        harness.atomic_json(self.root / "draft-after.json", after)
        self.send(before)
        return before

    def send(self, before):
        wire = before["text"].strip() + "\n\nAttached files:\n- uploads/input.bin"
        row = dict(id="turn-b", thread_id="thread-a", user_text=wire)
        trace = dict(events=[dict(kind="user_message", text=wire, images=[dict(id="img-a", mime="image/png")])])
        harness.validate_sent_input(self.context, before["text"], before, row, trace)

    def stage(self):
        result = dict(case_id="a" * 32, passed=True, phase="baseline", engine_pid_before=9, engine_pid_after=9,
                      snapshots=["before.png", "after.png"], turn_ids=["turn-a", "turn-b"],
                      draft_verified=True, input_fingerprint="frozen")
        cleanup = dict(case_id="a" * 32, cleanup_complete=True, data_removed=True, remaining_owned_pids=[],
            scenario_exit=0, exit_code=0, budget_exceeded=False, budget_seconds=240, scenario_limit_seconds=180,
            timings_seconds=dict(guest_gate=.1, prepare=.1, launch=.1, scenario=1, cleanup=.1, total=1.4))
        for name in result["snapshots"]:
            (self.root / name).write_bytes(b"other domain gate owns native pixels")
        harness.atomic_json(self.root / "resilience.json", result)
        harness.atomic_json(self.root / "cleanup.json", cleanup)
        return dict(budgets=dict(baseline=dict(scenario_seconds=180, cleanup_seconds=60, whole_case_seconds=240)),
                    input_fingerprint="frozen")

    def accept(self):
        with mock.patch.object(harness.background, "accept_evidence"):
            return harness.accepted_stage(self.root, "baseline", self.stage())

    def test_actual_http_bodies_and_distinct_mime_survive_in_evidence(self):
        before = self.capture()
        for label in ("before", "after", "sent-turn-b"):
            receipt = harness.read_json(self.root / "http-inputs" / label / "receipt.json")
            self.assertEqual(receipt["case_id"], self.context["case_id"])
            self.assertEqual(receipt["status"], "complete")
            for index, (record, expected) in enumerate(zip(receipt["files"], (self.binary, self.image))):
                self.assertEqual((self.root / record["body"]).read_bytes(), expected)
                self.assertEqual(record["sha256"], hashlib.sha256(expected).hexdigest())
                self.assertEqual(record["metadata"]["mime"], before["files"][index]["mime"])
            self.assertEqual(receipt["files"][0]["content_type"], "application/octet-stream")
            self.assertEqual(receipt["files"][0]["metadata"]["mime"], "application/macbinary")
            self.assertEqual(receipt["files"][1]["image"], dict(format="png", width=3, height=2))
        self.assertTrue(self.accept())

    def test_accepted_stage_refuses_old_summary_without_original_http_bodies(self):
        before = harness.draft_snapshot(self.context)
        harness.atomic_json(self.root / "draft-before.json", before)
        harness.atomic_json(self.root / "draft-after.json", before)
        with self.assertRaisesRegex(RuntimeError, "input|body|evidence"):
            self.accept()

    def test_accepted_stage_rereads_body_case_headers_and_decoded_image(self):
        self.capture()
        self.assertTrue(self.accept())
        receipt_path = self.root / "http-inputs/after/receipt.json"
        original = receipt_path.read_bytes()
        receipt = json.loads(original)
        body = self.root / receipt["files"][1]["body"]
        body_bytes = body.read_bytes()
        for mutation in ("body", "case", "mime", "dimensions", "escape", "symlink", "missing"):
            changed = copy.deepcopy(receipt)
            if mutation == "body": body.write_bytes(body_bytes[:-1] + b"x")
            if mutation == "case": changed["case_id"] = "b" * 32
            if mutation == "mime": changed["files"][1]["content_type"] = "text/html"
            if mutation == "dimensions": changed["files"][1]["image"]["width"] = 4
            if mutation == "escape": changed["files"][1]["body"] = "../outside.body"
            if mutation in ("symlink", "missing"):
                body.unlink()
                if mutation == "symlink": body.symlink_to(self.root / "draft-before.json")
            receipt_path.write_text(json.dumps(changed))
            with self.subTest(mutation=mutation), self.assertRaisesRegex(RuntimeError, "input|body|evidence|MIME|image"):
                self.accept()
            receipt_path.write_bytes(original)
            if body.is_symlink(): body.unlink()
            body.write_bytes(body_bytes)

    def test_capture_never_overwrites_an_old_artifact_or_uses_a_path_label(self):
        harness.draft_snapshot(self.context, evidence_label="before")
        saved = (self.root / "http-inputs/before/receipt.json").read_bytes()
        with self.assertRaises(FileExistsError):
            harness.draft_snapshot(self.context, evidence_label="before")
        self.assertEqual((self.root / "http-inputs/before/receipt.json").read_bytes(), saved)
        for label in ("../outside", "/absolute", "a/b"):
            with self.subTest(label=label), self.assertRaisesRegex(RuntimeError, "label|path"):
                harness.draft_snapshot(self.context, evidence_label=label)
        self.assertFalse((self.root.parent / "outside").exists())

    def test_missing_image_http_type_cannot_borrow_metadata_mime(self):
        self.responses[self.draft_path + "/files/image-a"] = (self.image, None)
        with self.assertRaisesRegex(RuntimeError, "MIME|Content-Type"):
            harness.draft_snapshot(self.context, evidence_label="before")

    def test_primary_download_failure_survives_an_evidence_write_failure(self):
        self.responses[self.draft_path + "/files/file-a"] = (b"truncated", "application/octet-stream")
        with mock.patch.object(harness.input_evidence, "retain", side_effect=OSError("disk full")):
            with self.assertRaisesRegex(RuntimeError, "truncated"):
                harness.draft_snapshot(self.context, evidence_label="before")

    def test_failed_actual_image_download_retains_its_real_body_and_http_mime(self):
        for label, data, mime in (("wrong-mime", self.image, "text/html"),
                ("broken-image", self.image[:-1] + b"x", "image/png")):
            self.responses[self.draft_path + "/files/image-a"] = (data, mime)
            with self.subTest(label=label), self.assertRaisesRegex(RuntimeError, "MIME|image"):
                harness.draft_snapshot(self.context, evidence_label=label)
            receipt = harness.read_json(self.root / "http-inputs" / label / "receipt.json")
            record = receipt["files"][1]
            self.assertEqual(receipt["status"], "failed")
            self.assertEqual(record["content_type"], mime)
            self.assertEqual((self.root / record["body"]).read_bytes(), data)

    def test_repeated_final_ui_poll_reuses_identical_sent_bytes_without_overwriting(self):
        before = self.capture()
        receipt_path = self.root / "http-inputs/sent-turn-b/receipt.json"
        original = receipt_path.read_bytes()
        self.send(before)
        self.assertEqual(receipt_path.read_bytes(), original)
        sent = self.root / "sent-input-turn-b.json"
        sent.write_text('{"original":"must survive"}')
        with self.assertRaisesRegex(RuntimeError, "overwrite"):
            self.send(before)
        self.assertEqual(json.loads(sent.read_text()), dict(original="must survive"))
        changed = json.loads(original)
        changed["thread_id"] = "another-thread"
        receipt_path.write_text(json.dumps(changed))
        with self.assertRaisesRegex(RuntimeError, "overwrite"):
            self.send(before)
        self.assertEqual(json.loads(receipt_path.read_text()), changed)

    def test_actual_http_404_and_missing_nosniff_remain_primary_failures(self):
        self.responses.pop(self.draft_path + "/files/file-a")
        with self.assertRaises(urllib.error.HTTPError) as caught:
            harness.draft_snapshot(self.context, evidence_label="not-found")
        self.assertEqual(caught.exception.code, 404)
        self.assertEqual(harness.read_json(self.root / "http-inputs/not-found/receipt.json")["status"], "failed")
        self.responses[self.draft_path + "/files/file-a"] = (self.binary, "application/octet-stream")
        self.headers["no_sniff"] = ""
        with self.assertRaisesRegex(RuntimeError, "identity"):
            harness.draft_snapshot(self.context, evidence_label="no-nosniff")

    def test_capture_write_failure_cannot_become_passed_or_write_through_a_link(self):
        with mock.patch.object(harness.input_evidence, "_exclusive", side_effect=OSError("disk full")):
            with self.assertRaisesRegex(OSError, "disk full"):
                harness.draft_snapshot(self.context, evidence_label="disk-failure")
        self.assertFalse((self.root / "http-inputs/disk-failure/receipt.json").exists())
        alias = self.root / "alias"
        alias.symlink_to(self.root, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "symbolic"):
            harness.draft_snapshot(dict(self.context, evidence=alias), evidence_label="through-link")
        self.assertFalse((self.root / "http-inputs/through-link").exists())

    def test_stage_rejects_changed_draft_scope_and_sent_notice_even_when_summaries_agree(self):
        self.capture()
        for label in ("after", "sent-turn-b"):
            path = self.root / "http-inputs" / label / "receipt.json"
            original = path.read_bytes()
            changed = json.loads(original)
            if label == "after": changed["draft_revision"] += 1
            else:
                sent_path = self.root / "sent-input-turn-b.json"
                sent = json.loads(sent_path.read_text())
                sent["text"] = "different notice"
                changed["snapshot"] = sent
                sent_path.write_text(json.dumps(sent))
            path.write_text(json.dumps(changed))
            with self.subTest(label=label), self.assertRaisesRegex(RuntimeError, "input evidence"):
                self.accept()
            path.write_bytes(original)

    def test_actual_image_decode_cannot_be_replaced_by_a_header_or_digest_assertion(self):
        damaged = bytearray(self.image)
        damaged[16:20] = struct.pack(">I", 4)
        damaged = bytes(damaged)
        self.responses[self.draft_path + "/files/image-a"] = (damaged, "image/png")
        with mock.patch.object(harness, "fixture_png", return_value=damaged):
            with self.assertRaisesRegex(RuntimeError, "scanlines|dimensions"):
                harness.draft_snapshot(self.context, evidence_label="invalid-decode")
        self.assertEqual((self.root / "http-inputs/invalid-decode/1.body").read_bytes(), damaged)

    def test_primary_failure_survives_both_retention_and_warning_output_failure(self):
        self.responses[self.draft_path + "/files/file-a"] = (b"short", "application/octet-stream")
        with mock.patch.object(harness.input_evidence, "retain", side_effect=OSError("disk full")), \
                mock.patch.object(harness.input_evidence.sys.stderr, "write", side_effect=OSError("closed")):
            with self.assertRaisesRegex(RuntimeError, "truncated") as caught:
                harness.draft_snapshot(self.context, evidence_label="broken-warning")
        self.assertIsNone(caught.exception.__cause__)

    def test_stage_needs_one_actual_send_and_unchanged_unsent_summary(self):
        self.capture()
        after_path = self.root / "draft-after.json"
        original = after_path.read_bytes()
        after = json.loads(original)
        after["text"] += "changed"
        after_path.write_text(json.dumps(after))
        with self.assertRaisesRegex(RuntimeError, "unsent snapshot"):
            self.accept()
        after_path.write_bytes(original)
        (self.root / "sent-input-turn-b.json").unlink()
        with self.assertRaisesRegex(RuntimeError, "unique actual sent"):
            self.accept()
        alias = self.root / "alias"
        alias.symlink_to(self.root, target_is_directory=True)
        with mock.patch.object(harness.background, "accept_evidence"):
            with self.assertRaisesRegex(RuntimeError, "symbolic"):
                harness.accepted_stage(alias, "baseline", self.stage())
        for path in ("../outside", "/absolute", "."):
            with self.subTest(path=path), self.assertRaisesRegex(RuntimeError, "escapes"):
                harness.input_evidence._path(self.root, path)

    def test_default_snapshot_keeps_original_http_contract_without_fixture_or_evidence_work(self):
        self.responses[self.draft_path + "/files/image-a"] = (self.image, None)
        with mock.patch.object(harness, "fixture_png", side_effect=AssertionError("default snapshot evaluated fixture")) as fixture:
            snapshot = harness.draft_snapshot(self.context)
        fixture.assert_not_called()
        self.assertEqual(snapshot["text"], self.draft["text"])
        self.assertEqual(snapshot["files"][1]["sha256"], hashlib.sha256(self.image).hexdigest())
        self.assertFalse((self.root / "http-inputs").exists())
        self.headers["no_sniff"] = ""
        with mock.patch.object(harness, "fixture_png", side_effect=AssertionError("must preserve primary download error")):
            with self.assertRaisesRegex(RuntimeError, "draft byte download identity differs"):
                harness.draft_snapshot(self.context)

    def test_actual_acceptance_rejects_boolean_and_float_post_revision_equal_to_integer_one(self):
        self.draft["revision"] = 1
        self.responses[self.draft_path] = (json.dumps(dict(draft=self.draft)).encode(), "application/json")
        self.capture()
        self.assertTrue(self.accept())
        path = self.root / "http-inputs/after/receipt.json"
        original = path.read_bytes()
        for revision in (True, 1.0):
            receipt = json.loads(original)
            receipt["draft_revision"] = revision
            path.write_text(json.dumps(receipt))
            with self.subTest(type=type(revision).__name__), self.assertRaisesRegex(RuntimeError, "revision"):
                self.accept()
        path.write_bytes(original)


if __name__ == "__main__":
    unittest.main()
