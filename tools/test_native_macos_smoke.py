#!/usr/bin/env python3
"""Offline boundaries for the guest-only native smoke scenario."""
import importlib.util
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
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

    def native_row(self):
        return dict(pid=123, title="zwai", frontmost=True, has_web_area=True,
                    textareas=1, rect=[10, 20, 800, 600], expected_visible=True,
                    bundle=str(self.context["app"]), case_verified=True,
                    origin="http://127.0.0.1:4567", screen_capture_allowed=True,
                    web_urls=["http://127.0.0.1:4567/?shell=desktop&pid=123"],
                    window_count=1, window_id=17, window_owner_pid=123,
                    windows=[dict(id=17, owner_pid=123)],
                    area_values=[""], send_count=1, send_enabled=False)

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
        row = self.native_row()
        smoke.assert_window(row, 123, require_result=True)
        for field, value in (("pid", 124), ("title", ""), ("frontmost", False),
                             ("has_web_area", False), ("textareas", 0),
                             ("expected_visible", False), ("rect", [0, 0, 0, 1])):
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                smoke.assert_window(dict(row, **{field: value}), 123, require_result=True)

    def test_rendered_window_must_have_our_web_origin_shell_pid_and_window_owner(self):
        row = self.native_row()
        for field, value in (("web_urls", ["http://127.0.0.1:9999/?shell=desktop&pid=123"]),
                             ("web_urls", ["http://127.0.0.1:4567/?shell=web&pid=123"]),
                             ("web_urls", ["http://127.0.0.1:4567/?shell=desktop&pid=124"]),
                             ("window_owner_pid", 124), ("window_count", 2),
                             ("window_id", 0), ("case_verified", False)):
            with self.subTest(field=field, value=value), self.assertRaises(RuntimeError):
                smoke.assert_window(dict(row, **{field: value}), 123)

    def test_send_waits_for_exact_unique_draft_and_enabled_unique_button(self):
        row = dict(self.native_row(), area_values=["exact draft"], send_enabled=True)
        smoke.assert_draft(row, "exact draft")
        for field, value in (("area_values", ["partial"]), ("area_values", ["exact draft", "exact draft"]),
                             ("send_enabled", False), ("send_count", 2), ("textareas", 2)):
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                smoke.assert_draft(dict(row, **{field: value}), "exact draft")

    def test_native_dispatch_uses_prepared_helper_and_remaining_budget_without_global_input(self):
        context = dict(self.context, ax=self.root / "prepared-helper", origin="http://127.0.0.1:4567")
        row = self.native_row()
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(1, clock=clock)
        clock.return_value = 10.75
        with mock.patch.object(smoke, "assert_process", return_value=self.context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(row), "")) as run:
            smoke.native_snapshot(context, deadline)
        command = run.call_args.args[0]
        self.assertEqual(command[0], str(context["ax"]))
        self.assertNotIn("/usr/bin/osascript", command)
        self.assertIn(context["case_id"], command)
        self.assertIn(context["origin"], command)
        self.assertAlmostEqual(run.call_args.kwargs["timeout"], .25)

    def test_native_bundle_alias_preserves_real_identity_and_keeps_origin_exact(self):
        alias = self.root / "bundle-alias.app"
        alias.symlink_to(self.context["app"], target_is_directory=True)
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        row = dict(self.native_row(), bundle=str(alias))
        with mock.patch.object(smoke, "assert_process", return_value=self.context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(row), "")):
            self.assertEqual(smoke.native_snapshot(context, smoke.Deadline(1))["bundle"], str(alias))
        row["origin"] = "http://127.0.0.1:9999"
        with mock.patch.object(smoke, "assert_process", return_value=self.context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(row), "")), \
                self.assertRaisesRegex(RuntimeError, "origin"):
            smoke.native_snapshot(context, smoke.Deadline(1))

    def test_window_wait_consumes_registration_pending_before_strict_window_acceptance(self):
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        pending = dict(pid=123, origin=context["origin"], bundle=str(context["app"]),
                       requested_bundle=str(context["app"]), ready=False,
                       registration_pending=True, bundle_verified=False, case_verified=False)
        ready = dict(self.native_row(), ready=True)
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(1, clock=clock)
        responses = [subprocess.CompletedProcess([], 0, json.dumps(row), "") for row in (pending, ready)]
        with mock.patch.object(smoke, "assert_process", return_value=context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", side_effect=responses) as run, \
                mock.patch.object(smoke.time, "sleep", side_effect=lambda seconds: setattr(clock, "return_value", clock.return_value + seconds)) as pause:
            self.assertEqual(smoke.wait_window(context, deadline), ready)
        self.assertEqual(run.call_count, 2)
        self.assertEqual(pause.call_count, 1)
        self.assertLess(run.call_args_list[1].kwargs["timeout"], run.call_args_list[0].kwargs["timeout"])
        self.assertTrue(all(call.args[0][2] == "snapshot" for call in run.call_args_list))

    def test_registration_pending_cannot_reset_the_existing_window_deadline(self):
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        pending = dict(pid=123, origin=context["origin"], bundle=str(context["app"]), ready=False,
                       registration_pending=True, bundle_verified=False, case_verified=False)
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(.2, clock=clock)
        response = subprocess.CompletedProcess([], 0, json.dumps(pending), "")
        with mock.patch.object(smoke, "assert_process", return_value=context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", return_value=response) as run, \
                mock.patch.object(smoke.time, "sleep", side_effect=lambda seconds: setattr(clock, "return_value", clock.return_value + seconds)), \
                self.assertRaisesRegex(TimeoutError, "native composer"):
            smoke.wait_window(context, deadline)
        self.assertLessEqual(run.call_count, 3)
        self.assertGreaterEqual(clock.return_value, deadline.ends)

    def test_registered_application_keeps_formal_window_polling_until_ready(self):
        context = dict(self.context, ax=self.root / "formal-helper", origin="http://127.0.0.1:4567")
        pending = dict(self.native_row(), ready=False, registration_pending=False,
                       bundle_verified=True, activation_requested=True, activation_ready=True,
                       title="", has_web_area=False, textareas=0, rect=[0, 0, 0, 0],
                       window_count=0, windows=[], expected_visible=False)
        ready = dict(self.native_row(), ready=True)
        responses = [pending, dict(pending), ready]
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(1, clock=clock)
        original_end = deadline.ends

        def native_call(argv, **kwargs):
            # Exercise wait_window and native_snapshot together. Only the OS
            # subprocess is replaced; every empty-window poll must still reach
            # the formal snapshot entry that owns application activation.
            self.assertEqual(argv[0], str(context["ax"]))
            self.assertEqual(argv[1:4], ["123", "snapshot", context["case_id"]])
            row = responses.pop(0)
            clock.return_value += .05
            return subprocess.CompletedProcess(argv, 0, json.dumps(row), "")

        with mock.patch.object(smoke, "assert_process", return_value=context["app"] / "Contents/MacOS/zwai"), \
                mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                mock.patch.object(smoke.subprocess, "run", side_effect=native_call) as run, \
                mock.patch.object(smoke.time, "sleep", side_effect=lambda seconds: setattr(clock, "return_value", clock.return_value + seconds)) as pause, \
                mock.patch.object(smoke, "assert_window", wraps=smoke.assert_window) as strict:
            self.assertEqual(smoke.wait_window(context, deadline), ready)
        self.assertEqual(responses, [])
        self.assertEqual(run.call_count, 3)
        self.assertEqual(pause.call_count, 2)
        strict.assert_called_once_with(ready, context["app_pid"], require_result=False)
        timeouts = [call.kwargs["timeout"] for call in run.call_args_list]
        self.assertAlmostEqual(timeouts[0], 1)
        self.assertTrue(all(earlier > later > 0 for earlier, later in zip(timeouts, timeouts[1:])))
        self.assertEqual(deadline.ends, original_end)
        self.assertLess(clock.return_value, original_end)

    def test_window_wait_propagates_identity_refusal_and_cancellation_without_retry(self):
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        cancelled = KeyboardInterrupt("owned case cancelled")
        for response, expected in ((subprocess.CompletedProcess([], 2, '{"error":"bundle mismatch"}', ""), RuntimeError),
                                   (cancelled, KeyboardInterrupt)):
            with self.subTest(error=expected.__name__), \
                    mock.patch.object(smoke, "assert_process", return_value=context["app"] / "Contents/MacOS/zwai"), \
                    mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                    mock.patch.object(smoke.subprocess, "run", **({"side_effect": response} if isinstance(response, BaseException) else {"return_value": response})) as run, \
                    mock.patch.object(smoke.time, "sleep") as pause, \
                    self.assertRaises(expected) as caught:
                smoke.wait_window(context, smoke.Deadline(1))
            self.assertEqual(run.call_count, 1)
            pause.assert_not_called()
            if expected is KeyboardInterrupt:
                self.assertIs(caught.exception, cancelled)

    def test_native_bundle_rejects_another_directory_and_missing_identity(self):
        other = self.root / "other.app"
        other.mkdir()
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        for returned in (other, self.root / "missing.app"):
            row = dict(self.native_row(), bundle=str(returned))
            with self.subTest(bundle=str(returned)), \
                    mock.patch.object(smoke, "assert_process", return_value=self.context["app"] / "Contents/MacOS/zwai"), \
                    mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                    mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(row), "")), \
                    self.assertRaisesRegex(RuntimeError, "bundle"):
                smoke.native_snapshot(context, smoke.Deadline(1))

    def test_helper_error_and_bad_json_keep_raw_output_before_validation(self):
        context = dict(self.context, ax=self.root / "unused-helper", origin="http://127.0.0.1:4567")
        for code, stdout, stderr, error in ((2, '{"error":"case mismatch"}', "native stderr", RuntimeError),
                                            (0, "not JSON", "decoder stderr", json.JSONDecodeError)):
            with self.subTest(code=code), \
                    mock.patch.object(smoke, "assert_process", return_value=self.context["app"] / "Contents/MacOS/zwai"), \
                    mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                    mock.patch.object(smoke.subprocess, "run", return_value=subprocess.CompletedProcess([], code, stdout, stderr)), \
                    self.assertRaises(error):
                smoke.native_snapshot(context, smoke.Deadline(1))
            receipt = json.loads((context["evidence"] / "smoke-native-last.json").read_text())
            self.assertEqual(receipt["returncode"], code)
            self.assertEqual(receipt["stdout"], stdout)
            self.assertEqual(receipt["stderr"], stderr)
            self.assertEqual(receipt["requested_bundle"], str(self.context["app"]))

    def test_preparation_reuses_verified_source_and_binary_hash_and_runtime_refuses_drift(self):
        output = self.root / "prepared-helper"
        def compile_helper(command, **kwargs):
            Path(command[command.index("-o")+1]).write_bytes(b"offline compile fixture")
            return subprocess.CompletedProcess(command, 0)
        with mock.patch.object(smoke.subprocess, "run", side_effect=compile_helper) as compile_run:
            first = smoke.prepare_helper(output)
            second = smoke.prepare_helper(output)
            self.assertEqual(compile_run.call_count, 1)
            self.assertEqual(first["source_sha256"], second["source_sha256"])
        smoke.validate_helper(output)
        output.write_bytes(b"unverified replacement")
        with self.assertRaisesRegex(RuntimeError, "helper"):
            smoke.validate_helper(output)

    def test_preparation_never_attributes_a_changed_source_to_an_older_compile(self):
        source = self.root / "native_macos_ax.m"
        source.write_text("frozen source")
        output = self.root / "prepared-helper"
        def compile_then_edit(command, **kwargs):
            self.assertEqual(Path(command[-3]).read_text(), "frozen source")
            source.write_text("new source during compilation")
            Path(command[command.index("-o")+1]).write_bytes(b"binary from frozen source")
            return subprocess.CompletedProcess(command, 0)
        with mock.patch.object(smoke, "AX_SOURCE", source), \
                mock.patch.object(smoke.subprocess, "run", side_effect=compile_then_edit):
            with self.assertRaisesRegex(RuntimeError, "source changed"):
                smoke.prepare_helper(output)
        self.assertFalse(output.exists())

    def test_preparation_rebuilds_after_source_or_flag_receipts_drift(self):
        source = self.root / "native_macos_ax.m"
        source.write_text("first source")
        output = self.root / "prepared-helper"
        def compile_helper(command, **kwargs):
            Path(command[-1]).write_bytes(Path(command[-3]).read_bytes())
            return subprocess.CompletedProcess(command, 0)
        with mock.patch.object(smoke, "AX_SOURCE", source), \
                mock.patch.object(smoke.subprocess, "run", side_effect=compile_helper) as compile_run:
            smoke.prepare_helper(output)
            source.write_text("changed source")
            with self.assertRaisesRegex(RuntimeError, "helper"):
                smoke.validate_helper(output)
            smoke.prepare_helper(output)
            manifest = smoke.helper_manifest_path(output)
            row = json.loads(manifest.read_text())
            row["flags"] = ["unverified compiler flags"]
            manifest.write_text(json.dumps(row))
            with self.assertRaisesRegex(RuntimeError, "helper"):
                smoke.validate_helper(output)
            smoke.prepare_helper(output)
            self.assertEqual(compile_run.call_count, 3)
            self.assertEqual(output.read_bytes(), b"changed source")

    def test_fill_is_single_and_send_happens_only_after_exact_draft(self):
        deadline = smoke.Deadline(2)
        sequence = []
        def native(context, shared, action="snapshot", message="", **kwargs):
            self.assertIs(shared, deadline)
            sequence.append(action)
            return {"filled": True} if action == "fill" else {"submitted": True}
        drafts = iter((dict(self.native_row(), area_values=["partial"], send_enabled=False),
                       dict(self.native_row(), area_values=["exact draft"], send_enabled=True)))
        def window(context, shared, **kwargs):
            self.assertIs(shared, deadline)
            sequence.append("draft")
            return next(drafts)
        with mock.patch.object(smoke, "native_snapshot", side_effect=native), \
                mock.patch.object(smoke, "wait_window", side_effect=window), \
                mock.patch.object(deadline, "pause"):
            smoke.submit_native(self.context, deadline, "exact draft")
        self.assertEqual(sequence, ["fill", "draft", "draft", "send"])
        row = json.loads((self.context["evidence"] / "smoke-window-draft.json").read_text())
        self.assertEqual(row["area_values"], ["exact draft"])

    def test_run_smoke_uses_one_case_sentence_without_relaxing_native_send_guards(self):
        # Exercise the generated fixture through the actual fill/draft/Send
        # path; one AX window cannot make three CG windows safe for input.
        executable = self.context["app"] / "Contents/MacOS/zwai"
        executable.parent.mkdir(parents=True)
        executable.write_bytes(b"offline candidate identity")
        (executable.parent.parent / "Info.plist").write_bytes(smoke.plistlib.dumps({}))
        expected = "Inspect this workspace for case " + self.context["case_id"] + " and report findings."
        for extra_windows, helper_ready in ((False, True), (True, False), (True, True)):
            with self.subTest(extra_windows=extra_windows, helper_ready=helper_ready):
                context = dict(self.context, ax=self.root / "prepared-helper")
                clock = mock.Mock(return_value=10)
                deadline = smoke.Deadline(.4, clock=clock)
                actions, messages = [], []

                def native(command, **kwargs):
                    self.assertEqual(command[:2], [str(context["ax"]), "123"])
                    self.assertEqual(command[3], context["case_id"])
                    self.assertIn(command[2], ("snapshot", "fill", "send"))
                    actions.append(command[2])
                    row = dict(self.native_row(), ready=True)
                    if command[2] == "fill":
                        messages.append(command[6])
                        row["filled"] = True
                    elif command[2] == "send":
                        self.assertEqual(command[6], messages[0])
                        row["submitted"] = True
                    elif messages:
                        row.update(area_values=[messages[0]], send_enabled=True)
                        if actions.count("snapshot") == 2:
                            row.update(area_values=["partial"], send_enabled=False)
                        elif extra_windows:
                            row.update(ready=helper_ready, windows=[dict(id=value, owner_pid=123) for value in (17, 18, 19)])
                            if not helper_ready:
                                row.pop("window_id")
                                row.pop("window_owner_pid")
                    return subprocess.CompletedProcess(command, 0, json.dumps(row), "")

                with mock.patch.object(smoke, "Deadline", return_value=deadline), \
                        mock.patch.object(smoke.runner, "require_guest"), \
                        mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}), \
                        mock.patch.object(smoke, "assert_process", return_value=executable), \
                        mock.patch.object(smoke, "wait_engine", return_value=("http://127.0.0.1:4567", {"pid": 456})), \
                        mock.patch.object(smoke, "request_json", return_value={"threads": []}), \
                        mock.patch.object(smoke.subprocess, "run", side_effect=native), \
                        mock.patch.object(smoke.time, "sleep", side_effect=lambda seconds: setattr(clock, "return_value", clock.return_value + seconds)), \
                        mock.patch.object(smoke, "wait_trace", side_effect=RuntimeError("Trace boundary reached")) as trace:
                    expected_error = TimeoutError if extra_windows and not helper_ready else RuntimeError
                    expected_reason = ("native window lacks" if helper_ready else "native composer") if extra_windows else "Trace boundary reached"
                    with self.assertRaisesRegex(expected_error, expected_reason):
                        smoke.run_smoke(context)

                self.assertEqual(messages, [expected])
                self.assertEqual(messages[0].count(context["case_id"]), 1)
                self.assertEqual(actions.count("fill"), 1)
                if extra_windows:
                    self.assertNotIn("send", actions)
                    trace.assert_not_called()
                    last = json.loads((context["evidence"] / "smoke-native-last.json").read_text())
                    blocked = json.loads(last["stdout"])
                    self.assertEqual(blocked["window_count"], 1)
                    self.assertEqual(len(blocked["windows"]), 3)
                else:
                    self.assertEqual(actions, ["snapshot", "fill", "snapshot", "snapshot", "send"])
                    trace.assert_called_once_with(context["origin"], context, expected, deadline)
                    draft = json.loads((context["evidence"] / "smoke-window-draft.json").read_text())
                    self.assertEqual(draft["area_values"], [expected])

    def test_native_helper_source_contains_only_pid_targeted_keyboard_posts(self):
        source = smoke.AX_SOURCE.read_text()
        self.assertIn("CGEventPostToPid(pid, down)", source)
        self.assertNotIn("CGEventPost(", source)
        self.assertNotIn("CGEventPostToPSN", source)
        self.assertLess(source.index('"kern.hv_vmm_present"'), source.index("if (!AXIsProcessTrusted())"))
        self.assertLess(source.index("if (!validOrigin(origin)) return fail"), source.index("if (!AXIsProcessTrusted())"))

    @unittest.skipUnless(sys.platform == "darwin" and Path("/usr/bin/xcrun").exists(), "macOS SDK required for pure helper checks")
    def test_direct_helper_origin_and_case_parser_without_accessibility_or_gui_calls(self):
        source = self.root / "helper-pure-checks.m"
        source.write_text('#define main native_macos_helper_main\n#include ' + json.dumps(str(smoke.AX_SOURCE.resolve())) + '''
#undef main
int main(void) { @autoreleasepool {
    if (!pageMatches(@"http://127.0.0.1:4567/?shell=desktop&pid=123", @"http://127.0.0.1:4567", 123)) return 1;
    if (pageMatches(@"https://example.invalid:443/?shell=desktop&pid=123", @"https://example.invalid:443", 123)) return 2;
    if (pageMatches(@"http://127.0.0.1:4567/other?shell=desktop&pid=123", @"http://127.0.0.1:4567", 123)) return 3;
    if (pageMatches(@"http://127.0.0.1:4567/?shell=desktop&pid=124", @"http://127.0.0.1:4567", 123)) return 4;
    if (!ownsCase(getpid(), @"pure-parser-fixture") || ownsCase(getpid(), @"wrong-case")) return 5;
    if (![normalized(@"a\\n  b") isEqualToString:@"a b"]) return 6;
    return 0;
} }
''')
        binary = self.root / "helper-pure-checks"
        subprocess.run(["/usr/bin/xcrun", "clang", *smoke.AX_FLAGS, str(source), "-o", str(binary)],
                       capture_output=True, text=True, check=True, timeout=30)
        environment = dict(os.environ, ZWAI_NATIVE_CASE_ID="pure-parser-fixture")
        result = subprocess.run([str(binary)], capture_output=True, text=True, env=environment, timeout=5)
        self.assertEqual(result.returncode, 0, "headless parser/identity contract failed with code " + str(result.returncode))

    @unittest.skipUnless(sys.platform == "darwin" and Path("/usr/bin/xcrun").exists(), "macOS SDK required for production JSON boxing")
    def test_production_web_area_field_serializes_real_json_booleans(self):
        production = smoke.AX_SOURCE.read_text()
        expression = re.search(r'@"has_web_area"\s*:\s*@\(([^)]*)\)', production).group(1)
        declaration = ""
        if expression.strip().isidentifier():
            declaration = re.search(r'\bBOOL\s+' + re.escape(expression.strip()) + r'\s*=\s*[^;]+;', production).group(0)
        source = self.root / "helper-json-checks.m"
        source.write_text('''#import <Foundation/Foundation.h>
static NSDictionary *snapshot(NSArray *urls) {
''' + declaration + '\nreturn @{@"has_web_area": @(' + expression + ')};\n}\n' + '''
int main(void) { @autoreleasepool {
    NSData *json=[NSJSONSerialization dataWithJSONObject:@[snapshot(@[]),snapshot(@[@"http://127.0.0.1:4567/"])] options:0 error:nil];
    fwrite(json.bytes,1,json.length,stdout); return 0;
} }
''')
        binary = self.root / "helper-json-checks"
        subprocess.run(["/usr/bin/xcrun", "clang", "-fobjc-arc", "-framework", "Foundation", str(source), "-o", str(binary)],
                       capture_output=True, text=True, check=True, timeout=30)
        result = subprocess.run([str(binary)], capture_output=True, text=True, check=True, timeout=5)
        snapshots = json.loads(result.stdout)
        self.assertIs(snapshots[0]["has_web_area"], False)
        self.assertIs(snapshots[1]["has_web_area"], True)

    @unittest.skipUnless(sys.platform == "darwin" and Path("/usr/bin/xcrun").exists(), "macOS SDK required for in-memory AX graph checks")
    def test_direct_collect_handles_semantic_revisits_and_bounds_actual_work(self):
        source = self.root / "helper-graph-checks.m"
        source.write_text('''#import <Foundation/Foundation.h>
#import <ApplicationServices/ApplicationServices.h>
@interface FixtureNode : NSObject
@property NSString *identity;
@property NSDictionary *attributes;
@property NSMutableArray *children;
@end
@implementation FixtureNode
- (NSUInteger)hash { return self.identity.hash; }
- (BOOL)isEqual:(id)other { return [other isKindOfClass:FixtureNode.class] && [self.identity isEqual:((FixtureNode *)other).identity]; }
@end
static FixtureNode *node(NSString *identity, NSString *role) {
    FixtureNode *value=[FixtureNode new]; value.identity=identity; value.children=[NSMutableArray array];
    value.attributes=role ? @{@"AXRole":role} : @{}; return value;
}
static AXError fixtureCopy(AXUIElementRef element, CFStringRef key, CFTypeRef *output) {
    FixtureNode *value=(__bridge FixtureNode *)element;
    id result=CFEqual(key,kAXChildrenAttribute) ? value.children : value.attributes[(__bridge NSString *)key];
    if (!result) return kAXErrorNoValue;
    *output=CFBridgingRetain(result); return kAXErrorSuccess;
}
static AXError fixturePerform(AXUIElementRef element, CFStringRef action) { abort(); }
#define AXUIElementCopyAttributeValue fixtureCopy
#define AXUIElementPerformAction fixturePerform
#define main unused_native_main
#include ''' + json.dumps(str(smoke.AX_SOURCE.resolve())) + '''
#undef main
static BOOL check(NSString *label, FixtureNode *root, NSUInteger expectedRows, NSUInteger expectedSends, BOOL expectedTruncated) {
    NSMutableArray *tree=[NSMutableArray array], *areas=[NSMutableArray array], *sends=[NSMutableArray array], *urls=[NSMutableArray array];
    BOOL visible=NO,truncated=NO;
    collect((__bridge AXUIElementRef)root,tree,areas,sends,urls,@"",CGRectZero,&visible,&truncated,0);
    BOOL passed=truncated==expectedTruncated && (expectedTruncated || (tree.count==expectedRows && sends.count==expectedSends));
    if (!passed) fprintf(stderr,"%s: rows=%lu sends=%lu truncated=%d\\n",label.UTF8String,(unsigned long)tree.count,(unsigned long)sends.count,truncated);
    return passed;
}
int main(void) { @autoreleasepool {
    BOOL passed=YES;
    FixtureNode *selfLoop=node(@"self",@"AXGroup"); [selfLoop.children addObject:selfLoop];
    passed=check(@"self-loop",selfLoop,1,0,NO) && passed;
    FixtureNode *a=node(@"a",@"AXGroup"), *b=node(@"b",@"AXGroup");
    [a.children addObject:b]; [b.children addObject:a];
    passed=check(@"two-node-cycle",a,2,0,NO) && passed;
    FixtureNode *root=node(@"root",@"AXGroup"), *first=node(@"shared",@"AXButton"), *alias=node(@"shared",@"AXButton");
    first.attributes=alias.attributes=@{@"AXRole":@"AXButton",@"AXTitle":@"Send",@"AXEnabled":@YES};
    [root.children addObjectsFromArray:@[first,alias]];
    passed=check(@"equal-different-wrappers",root,2,1,NO) && passed;
    alias.identity=@"independent";
    passed=check(@"different-identity-same-text",root,3,2,NO) && passed;
    FixtureNode *emptyRoot=node(@"empty-root",@"AXGroup");
    for(int i=0;i<5000;i++) [emptyRoot.children addObject:node([NSString stringWithFormat:@"empty-%d",i],nil)];
    passed=check(@"empty-node-work-limit",emptyRoot,0,0,YES) && passed;
    [emptyRoot.children removeLastObject];
    passed=check(@"exact-work-limit",emptyRoot,1,0,NO) && passed;
    FixtureNode *deep=node(@"deep-root",@"AXGroup"), *cursor=deep;
    for(int i=0;i<46;i++) { FixtureNode *next=node([NSString stringWithFormat:@"depth-%d",i],@"AXGroup"); [cursor.children addObject:next]; cursor=next; }
    passed=check(@"unique-depth-limit",deep,0,0,YES) && passed;
    return passed ? 0 : 1;
} }
''')
        binary = self.root / "helper-graph-checks"
        subprocess.run(["/usr/bin/xcrun", "clang", *smoke.AX_FLAGS, str(source), "-o", str(binary)],
                       capture_output=True, text=True, check=True, timeout=30)
        result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)

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
