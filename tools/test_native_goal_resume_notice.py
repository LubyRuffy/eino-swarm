import importlib.util
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from contextlib import redirect_stdout
import io


MODULE = Path(__file__).with_name("native_goal_resume_notice.py")
spec = importlib.util.spec_from_file_location("native_goal_resume_notice", MODULE)
notice = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notice)


class GoalResumeNoticeAssertions(unittest.TestCase):
    def test_default_answers_require_both_real_final_bodies_even_when_the_answers_are_identical(self):
        traces = [{"turn": {"final": "## Result\n\nRequest: own fixture\n\nFinal observation."}}] * 2
        rows = [{"AXRole": "AXStaticText", "AXValue": text}
                for text in ("Request: own fixture", "Final observation.")]
        with self.assertRaisesRegex(AssertionError, "answer"):
            notice.assert_default_answers({"tree": rows}, traces)
        self.assertEqual(len(notice.assert_default_answers({"tree": rows * 2}, traces)), 2)
        with self.assertRaisesRegex(AssertionError, "final"):
            notice.assert_default_answers({"tree": rows * 2}, [{"turn": {"final": ""}}])

    def test_default_tools_need_actual_boolean_collapsed_disclosures(self):
        notice.assert_work_collapsed({"folds": [{"label": "思考 · 3 个工具", "expanded": False}]})
        for state in ({"folds": []}, {"folds": [{"expanded": True}]},
                      {"folds": [{"expanded": 0}]}, {"folds": [{"expanded": None}]}):
            with self.subTest(state=state), self.assertRaisesRegex(AssertionError, "collapsed"):
                notice.assert_work_collapsed(state)

    def test_selected_conversation_waits_read_only_for_both_answers_on_the_same_deadline(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            traces = [{"turn": {"final": "## Result\n\nOpening actual observation.\n\nClosing actual observation."}}] * 2
            loaded = {"tree": [{"AXRole": "AXStaticText", "AXValue": value}
                               for value in ["Opening actual observation.", "Closing actual observation."] * 2]}
            pauses = []
            deadline = SimpleNamespace(pause=lambda stage: pauses.append(stage))
            with patch.object(notice.smoke, "wait_window", side_effect=[{"tree": []}, loaded, loaded, loaded, loaded, loaded]) as snapshot, \
                    patch.object(notice, "save_window") as capture, patch.object(notice, "control") as control:
                notice.capture_answers(context, deadline, "restored", traces)
            self.assertEqual(len(pauses), 1)
            self.assertEqual(capture.call_count, 4)
            self.assertTrue(all(call.args[1] is deadline for call in snapshot.call_args_list))
            control.assert_not_called()
            first = json.loads((Path(temporary) / "restored-answers-first-pending.json").read_text())
            self.assertEqual(first["snapshot"], {"tree": []})

    def test_missing_answers_expire_without_repeating_navigation_or_allocating_a_new_deadline(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            traces = [{"turn": {"final": "Opening actual observation.\n\nClosing actual observation."}}]
            deadline = SimpleNamespace(pause=lambda _: (_ for _ in ()).throw(TimeoutError("original answer deadline")))
            with patch.object(notice.smoke, "wait_window", return_value={"tree": []}), \
                    patch.object(notice, "control") as control, patch.object(notice, "save_window") as capture, \
                    self.assertRaisesRegex(TimeoutError, "original answer deadline"):
                notice.capture_answers(context, deadline, "missing", traces)
            control.assert_not_called()
            capture.assert_not_called()

    def test_inline_briefing_requires_every_summary_character_and_no_dialog(self):
        summary = "First recorded constraint.\nA final distinct condition survives."
        tree = [{"AXRole": "AXStaticText", "AXValue": "First recorded constraint."},
                {"AXRole": "AXStaticText", "AXValue": "A final distinct condition survives."}]
        notice.assert_inline_briefing({"tree": tree}, summary, True)
        for rows in (tree[:1], tree + [{"AXRole": "AXDialog"}],
                     [{"AXRole": "AXStaticText", "AXValue": summary[:-1]}]):
            with self.subTest(rows=rows), self.assertRaisesRegex(AssertionError, "briefing|dialog"):
                notice.assert_inline_briefing({"tree": rows}, summary, True)
        notice.assert_inline_briefing({"tree": []}, summary, False)
        with self.assertRaisesRegex(AssertionError, "hidden"):
            notice.assert_inline_briefing({"tree": tree}, summary, False)

    def test_helper_timeouts_keep_original_streams_command_and_exception_even_if_evidence_write_fails(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 123}
            deadline = SimpleNamespace(remaining=lambda _: 5)
            failure = subprocess.TimeoutExpired(["helper", "inspect"], 5, output=b"partial stdout", stderr=b"original stderr")
            with patch.object(notice.subprocess, "run", side_effect=failure), self.assertRaises(subprocess.TimeoutExpired) as raised:
                notice.run_native_helper(context, deadline, "session", "inspect", ["helper", "inspect"])
            self.assertIs(raised.exception, failure)
            saved = json.loads((Path(temporary) / "goal-notice-helper-001.json").read_text())
            self.assertEqual((saved["stdout"], saved["stderr"], saved["command"], saved["timed_out"]),
                             ("partial stdout", "original stderr", ["helper", "inspect"], True))
            with patch.object(notice.subprocess, "run", side_effect=failure), \
                    patch.object(notice.smoke.runner, "save_json", side_effect=OSError("evidence unavailable")), \
                    self.assertRaises(subprocess.TimeoutExpired) as raised:
                notice.run_native_helper(context, deadline, "session", "inspect", ["helper", "inspect"])
            self.assertIs(raised.exception, failure)

    def test_success_without_durable_helper_streams_cannot_pass(self):
        context = {"evidence": Path("unused"), "app_pid": 123}
        deadline = SimpleNamespace(remaining=lambda _: 5)
        with patch.object(notice.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout="{}", stderr="")), \
                patch.object(notice.smoke.runner, "save_json", side_effect=OSError("evidence unavailable")), \
                self.assertRaisesRegex(OSError, "evidence unavailable"):
            notice.run_native_helper(context, deadline, "session", "inspect", ["helper", "inspect"])

    def test_actual_control_and_session_failures_survive_evidence_storage_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "private.app"
            executable = app / "Contents/MacOS/zwai"
            executable.parent.mkdir(parents=True)
            controls = root / "controls"
            controls.write_bytes(b"pinned control")
            context = {"evidence": root, "app_pid": 123, "app": app, "controls": controls,
                       "session_ax": root / "session", "control_sha256": hashlib.sha256(controls.read_bytes()).hexdigest()}
            result = SimpleNamespace(returncode=3, stdout='{"matches":0,"ax_error":-25212}', stderr="original AX refusal")
            deadline = SimpleNamespace(remaining=lambda _: 5)
            for caller in (lambda: notice.control(context, deadline, "press", "unique input"),
                           lambda: notice.session_control(context, deadline, "inspect")):
                with self.subTest(caller=caller), patch.object(notice.smoke, "assert_process", return_value=executable), \
                        patch.object(notice.smoke, "wait_window"), patch.object(notice, "validate_session_helper"), \
                        patch.object(notice.subprocess, "run", return_value=result), \
                        patch.object(notice.smoke.runner, "save_json", side_effect=OSError("evidence volume unavailable")), \
                        self.assertRaises(subprocess.CalledProcessError) as raised:
                    caller()
                self.assertEqual((raised.exception.returncode, raised.exception.output, raised.exception.stderr),
                                 (3, result.stdout, result.stderr))
                self.assertEqual(raised.exception.evidence_error, "evidence volume unavailable")

    def test_main_failure_receipt_keeps_actual_control_refusal_or_timeout_when_helper_evidence_first_fails(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files = {name: root / name for name in ("ax", "session", "controls")}
            for path in files.values():
                path.write_bytes(b"prepared fixture")
            context = {"evidence": root, "app_pid": 123, "app": root, "case_id": "owned-case"}
            deadline = SimpleNamespace(remaining=lambda _: 5)
            argv = ["--ax", str(files["ax"]), "--session-ax", str(files["session"]),
                    "--controls", str(files["controls"]), "--control-sha256", hashlib.sha256(files["controls"].read_bytes()).hexdigest()]
            for timed_out, permanent in ((False, False), (False, True), (True, False), (True, True)):
                output = io.StringIO()
                result = SimpleNamespace(returncode=3, stdout='{"ax_error":-25212}', stderr="the original native refusal")
                timeout = subprocess.TimeoutExpired([str(files["controls"]), "123", "press", "unique"], 5,
                                                     output=b"original partial stdout", stderr=b"original timeout stderr")
                save = notice.smoke.runner.save_json
                writes = []
                def save_with_first_failure(path, value):
                    writes.append(str(path))
                    if len(writes) == 1 or permanent:
                        raise OSError("first helper evidence unavailable")
                    return save(path, value)
                with self.subTest(timed_out=timed_out, permanent=permanent), patch.dict(notice.os.environ, {"ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS": "150"}), \
                        patch.object(notice.smoke, "case_context", return_value=dict(context)), \
                        patch.object(notice.smoke, "assert_process"), patch.object(notice.smoke, "wait_window"), \
                        patch.object(notice, "run", side_effect=lambda target: notice.control(target, deadline, "press", "unique")), \
                        patch.object(notice.subprocess, "run", side_effect=timeout if timed_out else None, return_value=result) as action, \
                        patch.object(notice.smoke.runner, "save_json", side_effect=save_with_first_failure), redirect_stdout(output):
                    self.assertEqual(notice.main(argv), 2)
                self.assertEqual(action.call_count, 1)
                receipt = json.loads(output.getvalue())
                if permanent:
                    self.assertEqual(receipt["receipt_evidence_error"], "first helper evidence unavailable")
                else:
                    self.assertEqual(receipt, json.loads((root / "native-goal-resume-notice.json").read_text()))
                helper = receipt["helper"]
                self.assertEqual(helper["timed_out"], timed_out)
                self.assertEqual(helper["evidence_error"], "first helper evidence unavailable")
                self.assertEqual((helper["stdout"], helper["stderr"]),
                                 ("original partial stdout", "original timeout stderr") if timed_out else (result.stdout, result.stderr))
                self.assertEqual(helper["returncode"], None if timed_out else 3)

    def test_reopened_chats_pane_selects_its_visible_title_without_pressing_ambiguous_navigation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            executable = root / "private.app/Contents/MacOS/zwai"
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"private fixture executable")
            context = {"evidence": root, "timeout": 45, "ax": root / "ax", "app_pid": 123,
                       "app": root / "private.app", "workspace": root, "data": root / "data", "case_id": "owned-case"}
            first, second = {"id": "turn-1", "status": "done"}, {"id": "turn-2", "status": "done"}
            pair = [{"kind": "goal_resumed", "seq": 10, "turn_id": "turn-1", "role": "human"},
                    {"kind": "goal_continued", "seq": 11, "turn_id": "turn-2"}]
            def api(_, __, path, body=None, method=None):
                if path == "/api/threads":
                    return {"threads": []}
                if path == "/api/settings":
                    return {"settings": {"ui": {}, "swarm": {}, "memory": {}}}
                if path == "/api/threads/thread-1" and method == "PATCH":
                    self.assertEqual(body, {"goal_resume": True})
                    return {"thread": {"id": "thread-1"}}
                if path.endswith("/turns"):
                    return {"turns": [first, second]}
                if "/log?" in path:
                    return {"events": pair}
                trace = self.goal_trace(turn=path.rsplit("/", 1)[-1], objective="objective")
                trace["events"].append(pair[0 if trace["turn"]["id"] == "turn-1" else 1])
                return trace
            def snapshot(target, _):
                buttons = [{"AXRole": "AXButton", "AXTitle": label, "AXEnabled": True}
                           for label in ("对话", "Known title", "开始目标")]
                if target["app_pid"] == 456:
                    buttons.append({"AXRole": "AXButton", "AXTitle": "对话", "AXEnabled": True})
                return {"tree": buttons}
            actions = []
            with patch.object(notice.smoke.runner, "require_guest"), patch.object(notice.smoke, "validate_helper"), \
                    patch.object(notice.smoke, "wait_engine", return_value=("http://127.0.0.1:1", {"pid": 999})), \
                    patch.object(notice.smoke, "wait_window", side_effect=snapshot), \
                    patch.object(notice.smoke.runner, "process_argv", return_value=None), \
                    patch.object(notice, "wait_registered_application"), patch.object(notice, "api", side_effect=api), \
                    patch.object(notice, "prepare_goal_fixture", return_value=({"id": "thread-1"}, first, "Known title", "objective")), \
                    patch.object(notice, "control", side_effect=lambda _, __, action, value=None: actions.append((action, value))), \
                    patch.object(notice, "capture_answers", return_value=[["opening", "closing"]]), \
                    patch.object(notice, "compact_fixture", return_value="stored real summary"), \
                    patch.object(notice, "exercise_inline_briefing"), patch.object(notice, "assert_inline_briefing"), \
                    patch.object(notice, "capture", return_value={"captured": True}) as capture, \
                    patch.object(notice.subprocess, "Popen", return_value=SimpleNamespace(pid=456, wait=lambda **_: 0)):
                receipt = notice.run(context)
            self.assertEqual(actions, [("reload", None), ("press", "对话"), ("press", "Known title"),
                                       ("reload", None), ("press", "Known title"), ("close", None), ("press", "Known title"), ("close", None)])
            self.assertEqual([call.args[2] for call in capture.call_args_list], ["01-resumed", "02-reloaded", "03-reopened"])
            self.assertEqual(receipt["native_actions"], ["Conversations tab", "select thread", "expand briefing", "collapse briefing", "reload", "select thread after reload", "close", "reopen", "select thread"])

    def test_completed_goal_fixture_uses_recovery_api_after_native_selection_and_never_presses_obsolete_start(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "timeout": 45, "ax": Path("fixture")}
            actions = []
            def control(_, __, action, value=None):
                actions.append((action, value))
            with patch.object(notice.smoke.runner, "require_guest"), \
                    patch.object(notice.smoke, "validate_helper"), \
                    patch.object(notice.smoke, "wait_engine", return_value=("http://127.0.0.1:1", {})), \
                    patch.object(notice, "wait_registered_application"), \
                    patch.object(notice, "prepare_goal_fixture", return_value=({"id": "thread-1"}, {"id": "turn-1"}, "Known title", "objective")), \
                    patch.object(notice, "api", side_effect=[{"threads": []}, {"settings": {"ui": {}, "swarm": {}, "memory": {}}}, {}, StopIteration("resume observation")]), \
                    patch.object(notice, "control", side_effect=control), \
                    patch.object(notice, "press_available", side_effect=lambda _, __, value: actions.append(("available press", value)), create=True), \
                    self.assertRaisesRegex(StopIteration, "resume observation"):
                notice.run(context)
            self.assertEqual(actions, [("reload", None), ("available press", "对话"),
                                       ("available press", "Known title")])

    def test_native_button_waits_read_only_for_one_enabled_match_then_presses_once(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            button = {"AXRole": "AXButton", "AXDescription": "Known title", "AXEnabled": True}
            states = [{"tree": [{**button, "AXDescription": "Another title"}, {**button, "AXRole": "AXStaticText"}]},
                      {"tree": [{**button, "AXEnabled": False}]}, {"tree": [button]}]
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    pass
            with patch.object(notice.smoke, "wait_window", side_effect=states) as snapshot, \
                    patch.object(notice, "control", return_value={"pressed": True}) as control:
                self.assertEqual(notice.press_available(context, Deadline(), "Known title"), {"pressed": True})
            self.assertEqual(snapshot.call_count, 3)
            control.assert_called_once_with(context, unittest.mock.ANY, "press", "Known title")
            receipt = json.loads((Path(temporary) / "goal-notice-availability-last.json").read_text())
            self.assertEqual((receipt["value"], receipt["matches"], receipt["snapshot"]), ("Known title", 1, states[-1]))

    def test_native_button_matching_uses_the_control_helpers_exact_preference_then_unique_contains(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            exact = {"AXRole": "AXRadioButton", "AXTitle": "Known title", "AXEnabled": True}
            suffix = {"AXRole": "AXButton", "AXDescription": "Known title · Done", "AXEnabled": True}
            class Deadline:
                def remaining(self, _):
                    return 5
            for rows in ([suffix, exact], [suffix]):
                with self.subTest(rows=rows), patch.object(notice.smoke, "wait_window", return_value={"tree": rows}), \
                        patch.object(notice, "control") as control:
                    notice.press_available(context, Deadline(), "Known title")
                    control.assert_called_once_with(context, unittest.mock.ANY, "press", "Known title")

    def test_ambiguous_native_button_or_failed_press_is_never_retried(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            button = {"AXRole": "AXButton", "AXTitle": "开始目标", "AXEnabled": True}
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    raise AssertionError("mutation must not retry")
            with patch.object(notice.smoke, "wait_window", return_value={"tree": [button, button]}), \
                    patch.object(notice, "control") as control, self.assertRaisesRegex(AssertionError, "ambiguous"):
                notice.press_available(context, Deadline(), "开始目标")
            control.assert_not_called()
            with patch.object(notice.smoke, "wait_window", return_value={"tree": [button]}) as snapshot, \
                    patch.object(notice, "control", side_effect=AssertionError("press refused")) as control, \
                    self.assertRaisesRegex(AssertionError, "press refused"):
                notice.press_available(context, Deadline(), "开始目标")
            self.assertEqual((snapshot.call_count, control.call_count), (1, 1))

    def test_missing_native_button_expires_at_the_existing_deadline_without_pressing(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    raise TimeoutError("case deadline")
            with patch.object(notice.smoke, "wait_window", return_value={"tree": []}), \
                    patch.object(notice, "control") as control, self.assertRaisesRegex(TimeoutError, "case deadline"):
                notice.press_available(context, Deadline(), "Known title")
            control.assert_not_called()

    def test_native_control_failure_keeps_the_requested_label_with_the_actual_helper_result(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            controls = root / "controls"
            controls.write_bytes(b"prepared controls")
            context = {"evidence": root, "app_pid": 123, "controls": controls,
                       "control_sha256": hashlib.sha256(controls.read_bytes()).hexdigest()}
            class Deadline:
                def remaining(self, _):
                    return 5
            result = SimpleNamespace(returncode=3, stdout='{"matches":0}', stderr="")
            with patch.object(notice.smoke, "assert_process"), patch.object(notice.smoke, "wait_window"), \
                    patch.object(notice.subprocess, "run", return_value=result), \
                    self.assertRaisesRegex(AssertionError, "native control failed"):
                notice.control(context, Deadline(), "press", "Known title")
            row = json.loads((root / "goal-notice-control-last.json").read_text())
            self.assertEqual((row["value"], row["returncode"], row["stdout"]), ("Known title", 3, result.stdout))

    def goal_trace(self, thread="thread-1", turn="turn-1", objective="fixture objective"):
        events = [{"kind": "user_message", "text": objective},
                  {"kind": "tool_call", "text": "complete_goal({})", "tool_call_id": "tool-1"},
                  {"kind": "tool_result", "tool_call_id": "tool-1"},
                  {"kind": "goal_complete"}, {"kind": "done", "agent_id": "manager"}]
        return {"turn": {"id": turn, "thread_id": thread, "status": "done", "user_text": objective},
                "events": [{"thread_id": thread, "turn_id": turn, **event} for event in events],
                "llm_calls": [{"id": "call-1", "thread_id": thread, "turn_id": turn, "agent_id": "manager"}]}

    def test_api_fixture_creates_one_private_workspace_and_real_goal_turn_without_native_submission(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            data = root / "data"
            workspace = data / "workspaces/thread-1"
            workspace.mkdir(parents=True)
            context = {"evidence": root, "data": data, "case_id": "owned-case-123"}
            requests = []
            objective = None
            def request(_, __, path, body=None, method=None):
                nonlocal objective
                requests.append((path, body, method))
                if path == "/api/threads":
                    self.assertEqual(method, "POST")
                    return {"thread": {"id": "thread-1", "title": body["title"]}}
                if path.endswith("/files"):
                    return {"workspace": str(workspace)}
                if path.endswith("/turns") and method == "POST":
                    self.assertTrue(body["text"].startswith("/goal "))
                    objective = body["text"][len("/goal "):]
                    return {"turn": {"id": "turn-1", "thread_id": "thread-1", "status": "running"}}
                if path == "/api/threads/thread-1":
                    return {"thread": {"id": "thread-1", "goal": objective, "goal_complete": True}}
                if path.endswith("/turns"):
                    return {"turns": [{"id": "turn-1", "thread_id": "thread-1", "status": "done"}]}
                if path == "/api/trace/turn-1":
                    return self.goal_trace(objective=objective)
                self.fail("unexpected fixture request: " + path)
            class Deadline:
                def remaining(self, _):
                    return 5
            with patch.object(notice, "api", side_effect=request), patch.object(notice, "control") as control:
                thread, turn, title, text = notice.prepare_goal_fixture(context, Deadline())
            self.assertEqual((thread["id"], turn["id"]), ("thread-1", "turn-1"))
            self.assertEqual(text, objective)
            self.assertEqual(requests[0], ("/api/threads", {"title": title}, "POST"))
            self.assertEqual([path for path, _, method in requests if method == "POST"], ["/api/threads", "/api/threads/thread-1/turns"])
            control.assert_not_called()
            receipt = json.loads((root / "goal-notice-fixture.json").read_text())
            self.assertEqual(receipt["workspace"], str(workspace.resolve(strict=True)))
            self.assertEqual(receipt["initial_turn_id"], "turn-1")
            self.assertTrue((root / "goal-notice-fixture-trace.json").is_file())

    def test_api_fixture_refuses_a_workspace_outside_its_private_data_before_starting_a_turn(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "data").mkdir()
            context = {"evidence": root, "data": root / "data", "case_id": "owned-case"}
            with patch.object(notice, "api", side_effect=[{"thread": {"id": "thread-1"}}, {"workspace": str(root)}]) as request, \
                    self.assertRaisesRegex(AssertionError, "private.*workspace"):
                notice.prepare_goal_fixture(context, object())
            self.assertEqual(request.call_count, 2)

    def test_completed_goal_recovery_uses_its_normal_api_identity_without_any_native_start_action(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            acknowledged = {"thread": {"id": "own-thread", "goal_complete": False}}
            with patch.object(notice, "api", return_value=acknowledged) as api, \
                    patch.object(notice, "control", side_effect=AssertionError("obsolete native Start must never be pressed")) as control:
                notice.resume_goal_fixture(context, object(), "own-thread")
            self.assertEqual(api.call_args.args[2:], ("/api/threads/own-thread", {"goal_resume": True}, "PATCH"))
            control.assert_not_called()
            saved = json.loads((Path(temporary) / "goal-notice-resume-fixture.json").read_text())
            self.assertEqual((saved["request"], saved["response"]), ({"goal_resume": True}, acknowledged))
            with patch.object(notice, "api", return_value={"thread": {"id": "another-thread"}}), \
                    self.assertRaisesRegex(AssertionError, "another conversation"):
                notice.resume_goal_fixture(context, object(), "own-thread")

    def test_completed_goal_requires_its_own_successful_model_and_paired_completion_trace(self):
        good = self.goal_trace()
        notice.assert_goal_trace(good, "thread-1", "turn-1", "fixture objective")
        changed = []
        for key, value in (("id", "other"), ("thread_id", "other"), ("status", "running"),
                           ("user_text", "different"), ("error", "failed")):
            changed.append({**good, "turn": {**good["turn"], key: value}})
        changed += [{**good, "llm_calls": []}, {**good, "llm_calls": [{**good["llm_calls"][0], "turn_id": "other"}]},
                    {**good, "llm_calls": [{**good["llm_calls"][0], "err": "failed"}]}]
        for kind in ("tool_result", "goal_complete", "done", "user_message"):
            changed.append({**good, "events": [event for event in good["events"] if event["kind"] != kind]})
        for trace in changed:
            with self.subTest(trace=trace), self.assertRaises(AssertionError):
                notice.assert_goal_trace(trace, "thread-1", "turn-1", "fixture objective")

    def test_startup_waits_only_for_nil_bundle_registration_and_keeps_every_observation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "private.app"
            executable = app / "Contents/MacOS/zwai"
            executable.parent.mkdir(parents=True)
            executable.write_text("fixture")
            context = {"evidence": root, "app_pid": 123, "app": app, "session_ax": root / "helper"}
            base = {"pid": 123, "bundle": str(app), "action": "startup", "registered": False, "observed_bundle": None}
            receipts = [base, base, {**base, "registered": True, "observed_bundle": str(app)}]
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    pass
            results = [SimpleNamespace(returncode=0, stdout=json.dumps(row), stderr="") for row in receipts]
            with patch.object(notice.smoke, "assert_process", return_value=executable) as identity, \
                    patch.object(notice.smoke, "wait_window") as window, \
                    patch.object(notice, "validate_session_helper"), \
                    patch.object(notice.subprocess, "run", side_effect=results):
                self.assertEqual(notice.wait_registered_application(context, Deadline()), receipts[-1])
            self.assertEqual(identity.call_count, 3)
            window.assert_not_called()
            observations = sorted(root.glob("goal-notice-startup-*.json"))
            self.assertEqual(len(observations), 3)
            self.assertEqual([json.loads(json.loads(path.read_text())["stdout"])["registered"] for path in observations], [False, False, True])

    def test_nonempty_wrong_startup_bundle_is_fatal_and_not_retried(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "private.app"
            app.mkdir()
            executable = app / "Contents/MacOS/zwai"
            context = {"evidence": root, "app_pid": 123, "app": app, "session_ax": root / "helper"}
            row = {"pid": 123, "bundle": str(app), "action": "startup", "registered": False, "observed_bundle": "/another/app.app"}
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    raise AssertionError("wrong bundle must never retry")
            result = SimpleNamespace(returncode=0, stdout=json.dumps(row), stderr="")
            with patch.object(notice.smoke, "assert_process", return_value=executable), \
                    patch.object(notice, "validate_session_helper"), \
                    patch.object(notice.subprocess, "run", return_value=result) as run, \
                    self.assertRaisesRegex(AssertionError, "startup.*identity"):
                notice.wait_registered_application(context, Deadline())
            self.assertEqual(run.call_count, 1)
            self.assertEqual(len(list(root.glob("goal-notice-startup-*.json"))), 1)

    def test_missing_startup_registration_stops_at_the_existing_case_deadline(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            class Deadline:
                def pause(self, _):
                    raise TimeoutError("registration deadline reached")
            with patch.object(notice, "session_control", return_value={"registered": False}), \
                    self.assertRaisesRegex(TimeoutError, "registration deadline"):
                notice.wait_registered_application(context, Deadline())

    def test_startup_helper_failure_is_preserved_and_never_treated_as_pending_registration(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "private.app"
            app.mkdir()
            context = {"evidence": root, "app_pid": 123, "app": app, "session_ax": root / "helper"}
            error = {"error": "registered native target bundle identity mismatch", "observed_bundle": "/another/app.app"}
            class Deadline:
                def remaining(self, _):
                    return 5
                def pause(self, _):
                    raise AssertionError("failed identity must never retry")
            result = SimpleNamespace(returncode=2, stdout=json.dumps(error), stderr="")
            with patch.object(notice.smoke, "assert_process", return_value=app / "Contents/MacOS/zwai"), \
                    patch.object(notice, "validate_session_helper"), \
                    patch.object(notice.subprocess, "run", return_value=result) as run, \
                    self.assertRaisesRegex(AssertionError, "native session control failed"):
                notice.wait_registered_application(context, Deadline())
            self.assertEqual(run.call_count, 1)
            observation = json.loads((root / "goal-notice-startup-001.json").read_text())
            self.assertEqual((observation["returncode"], json.loads(observation["stdout"])), (2, error))

    def test_notice_geometry_requires_the_whole_notice_inside_finite_viewport_bounds(self):
        valid = {"notice_count": 1, "notice_in_view": True,
                 "notice_frame": {"x": 10, "y": 20, "width": 100, "height": 20},
                 "viewport_frame": {"x": 0, "y": 0, "width": 640, "height": 480}}
        notice.assert_notice_in_view(valid)
        for change in ({"notice_in_view": 1}, {"notice_count": True}, {"notice_count": 2},
                       {"notice_frame": {}}, {"notice_frame": {"x": 10, "y": -1, "width": 100, "height": 20}},
                       {"notice_frame": {"x": 600, "y": 20, "width": 100, "height": 20}},
                       {"notice_frame": {"x": 10, "y": 20, "width": 0, "height": 20}},
                       {"notice_frame": {"x": float("nan"), "y": 20, "width": 100, "height": 20}},
                       {"viewport_frame": {"x": 0, "y": 0, "width": 640, "height": float("inf")}}):
            with self.subTest(change=change), self.assertRaises(AssertionError):
                notice.assert_notice_in_view({**valid, **change})

    def test_capture_reveals_the_notice_and_keeps_geometry_with_the_same_window_screenshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 123}
            row = {"window_id": 456, "tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
            geometry = {"notice_count": 1, "notice_in_view": True,
                        "notice_frame": {"x": 10, "y": 20, "width": 100, "height": 20},
                        "viewport_frame": {"x": 0, "y": 0, "width": 640, "height": 480}}
            class Deadline:
                def remaining(self, _):
                    return 5
            def screenshot(argv, **_):
                Path(argv[-1]).write_bytes(b"captured fixture")
            with patch.object(notice, "session_control", side_effect=[{"folds": [{"expanded": False}]}, geometry]) as reveal, \
                    patch.object(notice.smoke, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture:
                self.assertEqual(notice.capture(context, Deadline(), "visible"), row)
            self.assertEqual([call.args[2] for call in reveal.call_args_list], ["inspect", "reveal-notice"])
            self.assertEqual(capture.call_args.args[0][3], "456")
            self.assertEqual(json.loads((Path(temporary) / "visible.viewport.json").read_text()), geometry)
            image = json.loads((Path(temporary) / "visible.image.json").read_text())
            self.assertEqual((image["pid"], image["window_id"]), (123, 456))

    def test_capture_rejects_a_notice_outside_the_viewport_before_taking_a_screenshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 123}
            row = {"window_id": 456, "tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
            geometry = {"notice_count": 1, "notice_in_view": False,
                        "notice_frame": {"x": 10, "y": -50, "width": 100, "height": 20},
                        "viewport_frame": {"x": 0, "y": 0, "width": 640, "height": 480}}
            class Deadline:
                def remaining(self, _):
                    return 5
            def screenshot(argv, **_):
                Path(argv[-1]).write_bytes(b"captured fixture")
            with patch.object(notice, "session_control", side_effect=[{"folds": [{"expanded": False}]}, geometry]), \
                    patch.object(notice.smoke, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture, \
                    self.assertRaisesRegex(AssertionError, "notice.*viewport"):
                notice.capture(context, Deadline(), "outside")
            capture.assert_not_called()

    def test_default_notice_capture_refuses_expanded_work_without_pressing_a_disclosure(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 123}
            row = {"tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
            with patch.object(notice.smoke, "wait_window", return_value=row), \
                    patch.object(notice, "session_control", return_value={"folds": [{"expanded": True}]}) as control, \
                    self.assertRaisesRegex(AssertionError, "collapsed"):
                notice.capture(context, object(), "default")
            self.assertEqual([call.args[2] for call in control.call_args_list], ["inspect"])

    def test_compact_fixture_uses_normal_endpoint_and_the_new_durable_summary_without_a_turn(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary)}
            old = {"kind": "compacted", "seq": 1, "text": json.dumps({"summary": "older summary"})}
            fresh = {"kind": "compacted", "seq": 9, "text": json.dumps({"summary": "complete new durable summary"})}
            replies = [{"settings": {"swarm": {"compact_keep_messages": 6}}}, {}, {"events": []}, {},
                       {"events": [fresh]}, {"turns": [{"id": "first"}, {"id": "second"}]}]
            with patch.object(notice, "api", side_effect=replies) as api:
                self.assertEqual(notice.compact_fixture(context, object(), "own-thread", ["first", "second"]),
                                 "complete new durable summary")
            self.assertEqual(api.call_args_list[3].args[2:], ("/api/threads/own-thread/compact", {}, "POST"))
            saved = json.loads((Path(temporary) / "goal-notice-compact.json").read_text())
            self.assertEqual(saved["event"], fresh)
            with patch.object(notice, "api", side_effect=[*replies[:2], {"events": [old]}]) as api, \
                    self.assertRaisesRegex(AssertionError, "no earlier compacted"):
                notice.compact_fixture(context, object(), "own-thread", ["first", "second"])
            self.assertEqual(api.call_count, 3)
            for bad in ({**fresh, "err": "failed"}, {**fresh, "text": '{"summary":""}'}, old):
                with self.subTest(bad=bad), patch.object(notice, "api", side_effect=[*replies[:4], {"events": [bad, bad]} if bad is old else {"events": [bad]}, replies[-1]]), \
                        self.assertRaises(AssertionError):
                    notice.compact_fixture(context, object(), "own-thread", ["first", "second"])

    def test_inline_briefing_presses_each_toggle_once_and_checks_the_complete_body(self):
        summary = "Opening durable evidence.\nFinal condition still required."
        hidden = {"tree": []}
        visible = {"tree": [{"AXRole": "AXStaticText", "AXValue": summary}]}
        with patch.object(notice.smoke, "wait_window", side_effect=[hidden, visible, visible, visible, hidden]), \
                patch.object(notice, "press_available") as press, patch.object(notice, "save_window") as capture:
            notice.exercise_inline_briefing({}, object(), summary)
        self.assertEqual([call.args[2] for call in press.call_args_list], ["查看压缩后的简报", "收起简报"])
        self.assertEqual([call.args[2] for call in capture.call_args_list],
                         ["inline-briefing-opening", "inline-briefing-closing", "inline-briefing-collapsed"])
        with patch.object(notice.smoke, "wait_window", side_effect=[hidden, hidden]), \
                patch.object(notice, "press_available") as press, self.assertRaisesRegex(AssertionError, "briefing"):
            notice.exercise_inline_briefing({}, object(), summary)
        self.assertEqual(press.call_count, 1)

    def test_prepared_session_helper_requires_matching_source_binary_and_flags(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "session-helper"
            binary.write_bytes(b"prepared helper fixture")
            binary.chmod(0o755)
            manifest = {"source_name": notice.SESSION_SOURCE.name,
                        "source_sha256": hashlib.sha256(notice.SESSION_SOURCE.read_bytes()).hexdigest(),
                        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                        "compiler": "/usr/bin/xcrun clang", "flags": notice.smoke.AX_FLAGS}
            receipt = Path(str(binary) + ".json")
            receipt.write_text(json.dumps(manifest))
            self.assertEqual(notice.validate_session_helper(binary), manifest)
            for key, value in (("source_sha256", "changed"), ("binary_sha256", "changed"),
                               ("compiler", "other compiler"), ("flags", [])):
                receipt.write_text(json.dumps({**manifest, key: value}))
                with self.subTest(key=key), self.assertRaises(AssertionError):
                    notice.validate_session_helper(binary)

    def test_one_native_notice_counts_each_accessibility_node_once(self):
        row = {"tree": [{"AXRole": "AXStaticText", "AXTitle": "继续追求目标。", "AXValue": "继续追求目标。"}]}
        self.assertEqual(notice.assert_single_notice(row), 1)

    def test_two_native_notices_are_rejected_even_when_text_is_identical(self):
        row = {"tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}] * 2}
        with self.assertRaisesRegex(AssertionError, "one visible continuation"):
            notice.assert_single_notice(row)

    def test_missing_notice_or_wrong_role_cannot_accept_window(self):
        for rows in ([], [{"AXRole": "AXButton", "AXTitle": "继续追求目标。"}]):
            with self.subTest(rows=rows), self.assertRaises(AssertionError):
                notice.assert_single_notice({"tree": rows})

    def test_both_durable_events_are_preserved_across_the_two_turns(self):
        events = [
            {"kind": "goal_resumed", "seq": 8, "turn_id": "before", "role": "human"},
            {"kind": "goal_continued", "seq": 9, "turn_id": "after"},
        ]
        self.assertEqual(notice.assert_resume_events(events, "before", "after"), [8, 9])

    def test_missing_duplicate_reordered_or_misattributed_events_are_rejected(self):
        resumed = {"kind": "goal_resumed", "seq": 8, "turn_id": "before", "role": "human"}
        continued = {"kind": "goal_continued", "seq": 9, "turn_id": "after"}
        invalid = [[], [resumed], [continued], [resumed, continued, continued],
                   [dict(resumed, seq=10), continued],
                   [dict(resumed, role="manager"), continued],
                   [dict(resumed, turn_id="after"), continued],
                   [resumed, dict(continued, turn_id="before")]]
        for events in invalid:
            with self.subTest(events=events), self.assertRaises(AssertionError):
                notice.assert_resume_events(events, "before", "after")


if __name__ == "__main__":
    unittest.main()
