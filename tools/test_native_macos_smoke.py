#!/usr/bin/env python3
"""Offline boundaries for the guest-only native smoke scenario."""
import importlib.util
import json
import os
from pathlib import Path
import struct
import tempfile
import threading
import unittest
from unittest import mock
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


SCRIPT = Path(__file__).with_name("native_macos_smoke.py")
spec = importlib.util.spec_from_file_location("native_macos_smoke", SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class SmokeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve() / "case"
        self.root.mkdir()
        (Path(self.temporary.name) / "evidence").mkdir()
        for name in ("data", "workspace", "home", "candidate.app"):
            (self.root / name).mkdir()
        self.environment = {
            "ZWAI_NATIVE_CASE_ID": "private-case", "ZWAI_NATIVE_APP_PID": "123",
            "ZWAI_NATIVE_APP": str(self.root / "candidate.app"),
            "ZWAI_NATIVE_DATA": str(self.root / "data"),
            "ZWAI_HOME": str(self.root / "data"), "HOME": str(self.root / "home"),
            "ZWAI_NATIVE_WORKSPACE": str(self.root / "workspace"),
            "ZWAI_NATIVE_EVIDENCE": str(Path(self.temporary.name) / "evidence"),
            "ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS": "40",
        }
        self.context = smoke.case_context(self.environment)

    def trace(self):
        events = []
        for kind, agent, call in (("user_message", "manager", None),
                                  ("spawned", "worker", None),
                                  ("tool_call", "worker", "write-1"),
                                  ("tool_result", "worker", "write-1"),
                                  ("finished", "worker", None),
                                  ("done", "manager", None)):
            events.append(dict(kind=kind, agent_id=agent, tool_call_id=call,
                               turn_id="turn", thread_id="thread", seq=len(events)+1,
                               text="input" if kind == "user_message" else "result"))
        events[2]["text"] = 'write({"file_path":"notes/worker.md","content":"verified"})'
        return {"turn": {"id": "turn", "thread_id": "thread", "status": "done",
                         "user_text": "input", "final": "A completed result with verified workspace findings."},
                "events": events, "llm_calls": [{"agent_id": "manager", "turn_id": "turn"},
                                                {"agent_id": "worker", "turn_id": "turn"}]}

    def test_one_deadline_is_shared_and_waits_only_until_next_poll(self):
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(2, clock=clock)
        clock.return_value = 11.95
        with mock.patch.object(smoke.time, "sleep") as sleep:
            deadline.pause("window")
        self.assertAlmostEqual(sleep.call_args.args[0], .05)
        clock.return_value = 12
        with self.assertRaisesRegex(TimeoutError, "window"):
            deadline.remaining("window")
        for invalid in (0, -1, float("nan"), float("inf")):
            with self.assertRaises(ValueError):
                smoke.Deadline(invalid)

    def test_runner_budget_caps_scenario_and_missing_private_identity_refuses(self):
        self.assertEqual(self.context["timeout"], 40)
        self.environment["ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS"] = "90"
        self.assertEqual(smoke.case_context(self.environment)["timeout"], 45)
        for key in self.environment:
            environment = dict(self.environment)
            environment.pop(key)
            with self.subTest(key=key), self.assertRaises((ValueError, KeyError)):
                smoke.case_context(environment)
        for value in ("0", "-2", "nan", "inf"):
            with self.subTest(timeout=value), self.assertRaises(ValueError):
                smoke.case_context(dict(self.environment, ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS=value))

    def test_private_data_and_case_paths_cannot_point_at_a_sibling(self):
        for key, value in (("ZWAI_HOME", str(self.root / "workspace")),
                           ("ZWAI_NATIVE_DATA", str(self.root)),
                           ("ZWAI_NATIVE_APP_PID", "-1"),
                           ("ZWAI_NATIVE_EVIDENCE", str(self.root / "data"))):
            with self.subTest(key=key), self.assertRaises(ValueError):
                smoke.case_context(dict(self.environment, **{key: value}))

    def test_identity_uses_case_marker_and_exact_bundle_or_private_cache(self):
        own = [str(self.root / "candidate.app/Contents/MacOS/zwai")]
        with mock.patch.object(smoke.runner, "process_identity", return_value=(own, "private-case")):
            smoke.assert_process(self.context, 123, "desktop")
        for argv, marker in ((own, "another-case"),
                             ([str(self.root / "candidate.app-other/Contents/MacOS/zwai")], "private-case"),
                             (None, None)):
            with self.subTest(argv=argv, marker=marker), \
                    mock.patch.object(smoke.runner, "process_identity", return_value=(argv, marker)), \
                    self.assertRaisesRegex(RuntimeError, "identity"):
                smoke.assert_process(self.context, 123, "desktop")

    def test_engine_url_is_exact_loopback_and_metadata_cannot_select_other_data(self):
        for url in ("https://127.0.0.1:80", "http://localhost:80", "http://127.0.0.1:80/evil",
                    "http://user@127.0.0.1:80", "http://127.0.0.1:80?x=1",
                    "http://192.0.2.1:80", "http://127.0.0.1"):
            with self.subTest(url=url), self.assertRaises(ValueError):
                smoke.engine_origin(url)
        self.assertEqual(smoke.engine_origin("http://127.0.0.1:1234"), "http://127.0.0.1:1234")
        record = dict(pid=456, mock=True, url="http://127.0.0.1:1234")
        metadata = dict(data_dir=self.environment["ZWAI_NATIVE_DATA"], mock=True,
                        clients=[dict(surface="desktop", pid=123)])
        (self.root / "data/engine.json").write_text(json.dumps(record))
        with mock.patch.object(smoke, "assert_process"), \
                mock.patch.object(smoke, "request_json", return_value=metadata):
            origin, _ = smoke.wait_engine(self.context, smoke.Deadline(1))
        self.assertEqual(origin, record["url"])
        for field, value in (("mock", False), ("data_dir", str(self.root / "workspace"))):
            with self.subTest(field=field), mock.patch.object(smoke, "assert_process"), \
                    mock.patch.object(smoke, "request_json", return_value=dict(metadata, **{field: value})), \
                    self.assertRaisesRegex(RuntimeError, "identity"):
                smoke.wait_engine(self.context, smoke.Deadline(1))

    def test_trace_requires_same_input_terminal_worker_tools_and_call_receipts(self):
        trace = self.trace()
        fragment = smoke.assert_trace(trace, "thread", "turn", "input")
        self.assertIn("completed result", fragment)
        for change in ("wrong_input", "wrong_turn", "failed", "no_worker", "unfinished_worker",
                       "unpaired_tool", "missing_result", "no_calls", "error_event", "missing_done", "manager_tools_only"):
            row = self.trace()
            if change == "wrong_input": row["turn"]["user_text"] = "another input"
            if change == "wrong_turn": row["events"][0]["turn_id"] = "other"
            if change == "failed": row["turn"]["status"] = "error"
            if change == "no_worker": row["events"] = [e for e in row["events"] if e["kind"] != "spawned"]
            if change == "unfinished_worker": row["events"] = [e for e in row["events"] if e["kind"] != "finished"]
            if change == "unpaired_tool": row["events"][3]["tool_call_id"] = "other"
            if change == "missing_result": row["turn"]["final"] = "input"
            if change == "no_calls": row["llm_calls"] = []
            if change == "error_event": row["events"][1]["err"] = "failed"
            if change == "missing_done": row["events"] = [e for e in row["events"] if e["kind"] != "done"]
            if change == "manager_tools_only":
                row["events"][2]["agent_id"] = row["events"][3]["agent_id"] = "manager"
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                smoke.assert_trace(row, "thread", "turn", "input")

    def test_worker_write_receipts_require_actual_unchanged_files_inside_case_data(self):
        workspace = self.context["data"] / "workspaces/thread"
        (workspace / "notes").mkdir(parents=True)
        target = workspace / "notes/worker.md"
        target.write_text("verified")
        with mock.patch.object(smoke, "request_json", return_value={"workspace": str(workspace)}):
            outputs = smoke.assert_workspace_outputs("http://127.0.0.1:123", self.context, self.trace(), smoke.Deadline(1))
            self.assertEqual(outputs[0]["bytes"], 8)
            target.write_text("write failed or changed")
            with self.assertRaisesRegex(RuntimeError, "bytes"):
                smoke.assert_workspace_outputs("http://127.0.0.1:123", self.context, self.trace(), smoke.Deadline(1))
            target.unlink()
            with self.assertRaisesRegex(RuntimeError, "missing"):
                smoke.assert_workspace_outputs("http://127.0.0.1:123", self.context, self.trace(), smoke.Deadline(1))
        with mock.patch.object(smoke, "request_json", return_value={"workspace": str(self.root / "workspace")}):
            with self.assertRaisesRegex(RuntimeError, "private"):
                smoke.assert_workspace_outputs("http://127.0.0.1:123", self.context, self.trace(), smoke.Deadline(1))

    def test_real_loopback_http_json_and_redirect_boundary(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/redirect":
                    self.send_response(302)
                    self.send_header("Location", "http://192.0.2.1/")
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"mock":true}' if self.path == "/api/meta" else b'[]')
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        origin = "http://127.0.0.1:" + str(server.server_address[1])
        self.assertEqual(smoke.request_json(origin, "/api/meta", smoke.Deadline(1)), {"mock": True})
        with self.assertRaisesRegex(RuntimeError, "redirect"):
            smoke.request_json(origin, "/redirect", smoke.Deadline(1))
        with self.assertRaisesRegex(RuntimeError, "non-object"):
            smoke.request_json(origin, "/array", smoke.Deadline(1))

    def test_window_requires_rendered_page_and_result_not_just_pid_or_title(self):
        row = dict(pid=123, title="zwai", frontmost=True, has_web_area=True,
                   textareas=1, rect=[10, 20, 800, 600], expected_visible=True)
        smoke.assert_window(row, 123, require_result=True)
        for field, value in (("pid", 124), ("title", ""), ("frontmost", False),
                             ("has_web_area", False), ("textareas", 0),
                             ("expected_visible", False), ("rect", [0, 0, 0, 1])):
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                smoke.assert_window(dict(row, **{field: value}), 123, require_result=True)

    def test_screenshot_must_decode_dimensions_and_not_accept_magic_bytes(self):
        path = self.root / "image.png"
        path.write_bytes(b"\x89PNG\r\n\x1a\n")
        with self.assertRaises(RuntimeError):
            smoke.png_dimensions(path)
        # A complete image with compressed scanlines is needed, not just IHDR.
        import zlib
        def chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind+data))
        raw = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 1, 8, 2, 0, 0, 0))
        raw += chunk(b"IDAT", zlib.compress(b"\0\xff\0\0\0\xff\0")) + chunk(b"IEND", b"")
        path.write_bytes(raw)
        self.assertEqual(smoke.png_dimensions(path), [2, 1])
        path.write_bytes(raw[:-12])
        with self.assertRaises(RuntimeError):
            smoke.png_dimensions(path)
        header = chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 1, 8, 2, 0, 0, 0))
        for scanlines in (b"\0", b"\x05\xff\0\0\0\xff\0"):
            path.write_bytes(b"\x89PNG\r\n\x1a\n" + header + chunk(b"IDAT", zlib.compress(scanlines)) + chunk(b"IEND", b""))
            with self.assertRaisesRegex(RuntimeError, "scanlines"):
                smoke.png_dimensions(path)
        for color, depth in ((3, 8), (6, 1)):
            header = chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 1, depth, color, 0, 0, 0))
            path.write_bytes(b"\x89PNG\r\n\x1a\n" + header + chunk(b"IDAT", zlib.compress(b"\0\0\0")) + chunk(b"IEND", b""))
            with self.assertRaisesRegex(RuntimeError, "unsupported"):
                smoke.png_dimensions(path)

    def test_guest_refusal_has_no_native_or_http_side_effect(self):
        with mock.patch.object(smoke.runner, "require_guest", side_effect=RuntimeError("host refused")), \
                mock.patch.object(smoke, "request_json") as request, \
                mock.patch.object(smoke.subprocess, "run") as native:
            with self.assertRaisesRegex(RuntimeError, "host refused"):
                smoke.run_smoke(self.context)
        request.assert_not_called()
        native.assert_not_called()


if __name__ == "__main__":
    unittest.main()
