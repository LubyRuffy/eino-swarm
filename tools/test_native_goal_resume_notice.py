import importlib.util
import hashlib
import json
import platform
from pathlib import Path
import subprocess
import sys
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
    def test_private_json_and_screenshot_keep_mode_0600_without_changing_normal_umask(self):
        with tempfile.TemporaryDirectory() as temporary:
            script = '''
import json, os, sys
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
sys.path.insert(0, sys.argv[1])
import native_goal_resume_notice as notice
root = Path(sys.argv[2])
receipt = root / "own-receipt.json"
notice.save_private_json(receipt, {"retained": "first"})
receipt.chmod(0o644)
notice.save_private_json(receipt, {"retained": "updated"})
image = root / "native.png"
image.write_bytes(b"previous fixture")
image.chmod(0o644)
def capture(command, **kwargs):
    assert image.stat().st_mode & 0o777 == 0o600
    assert kwargs["umask"] == 0o077
    image.write_bytes(b"captured fixture")
    return SimpleNamespace(returncode=0, stdout="capture output", stderr="capture diagnostic")
with patch.object(notice.subprocess, "run", side_effect=capture), patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)):
    notice.save_window({"evidence": root, "app_pid": 123}, SimpleNamespace(remaining=lambda _: 5), "native", {"window_id": 456})
with os.fdopen(os.open(root / "normal-umask", os.O_WRONLY | os.O_CREAT, 0o666), "w") as output:
    output.write("normal umask remains")
print(json.dumps({"modes": {path.name: path.stat().st_mode & 0o777 for path in root.iterdir()}, "receipt": json.loads(receipt.read_text())}))
'''
            result = subprocess.run([sys.executable, "-c", script, str(MODULE.parent), temporary],
                                    capture_output=True, text=True, timeout=10, umask=0o022)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            evidence = json.loads(result.stdout)
            self.assertEqual(evidence["receipt"], {"retained": "updated"})
            self.assertEqual(evidence["modes"].pop("normal-umask"), 0o644)
            self.assertTrue(evidence["modes"])
            self.assertEqual(set(evidence["modes"].values()), {0o600})

    @staticmethod
    def reading_row():
        return {"notice_count": 1, "notice_in_view": True, "notice_reveal_requested": True, "notice_reveal_status": 0,
                "notice_frame": {"x": 110, "y": 190, "width": 20, "height": 20},
                "wheel_posted": True, "wheel_count": 1, "wheel_delta": 80,
                "scrollarea_verified": True, "hit_verified": True, "hit_status": 0,
                "point": {"x": 120, "y": 200},
                "viewport_frame": {"x": 100, "y": 100, "width": 400, "height": 500}}

    def test_reading_admission_requires_a_verified_single_native_wheel_and_real_geometry(self):
        area = {"x": 100, "y": 100, "width": 400, "height": 500}
        good = self.reading_row()
        notice.assert_reading_admission(good)
        for change in ({"wheel_posted": False}, {"wheel_count": 2}, {"wheel_delta": -80},
                       {"hit_verified": False}, {"scrollarea_verified": False},
                       {"point": {"x": 0, "y": 0}}, {"viewport_frame": {}}):
            with self.subTest(change=change), self.assertRaises(AssertionError):
                notice.assert_reading_admission({**good, **change})

    def test_target_geometry_rejects_presence_without_visible_viewport_or_unique_opening(self):
        rectangle = {"x": 100, "y": 100, "width": 100, "height": 20}
        target = {"owner_pid": 123, "in_view": True, "frame": rectangle,
                  "viewport": {"x": 0, "y": 0, "width": 640, "height": 480}}
        notice.assert_target_geometry({"pid": 123, "matched_count": 1, "target_count": 1, "targets": [target]}, unique=True)
        for state in ({"pid": 123, "matched_count": 1, "target_count": 0, "targets": []},
                      {"pid": 123, "matched_count": 2, "target_count": 1, "targets": [target]},
                      {"pid": 123, "matched_count": 1, "target_count": 1, "targets": [{**target, "in_view": False}]},
                      {"pid": 123, "matched_count": 1, "target_count": 1, "targets": [{**target, "frame": {**rectangle, "y": 600}}]}):
            with self.subTest(state=state), self.assertRaises(AssertionError):
                notice.assert_target_geometry(state, unique=True)

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
            with patch.object(notice, "wait_window", side_effect=[{"tree": []}, loaded, loaded, loaded, loaded, loaded]) as snapshot, \
                    patch.object(notice, "capture_target", return_value=loaded) as capture, \
                    patch.object(notice, "session_control", return_value=self.reading_row()), patch.object(notice, "control") as control:
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
            with patch.object(notice, "wait_window", return_value={"tree": []}), \
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
                    patch.object(notice, "save_private_json", side_effect=OSError("evidence unavailable")), \
                    self.assertRaises(subprocess.TimeoutExpired) as raised:
                notice.run_native_helper(context, deadline, "session", "inspect", ["helper", "inspect"])
            self.assertIs(raised.exception, failure)

    def test_success_without_durable_helper_streams_cannot_pass(self):
        context = {"evidence": Path("unused"), "app_pid": 123}
        deadline = SimpleNamespace(remaining=lambda _: 5)
        with patch.object(notice.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout="{}", stderr="")), \
                patch.object(notice, "save_private_json", side_effect=OSError("evidence unavailable")), \
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
                        patch.object(notice, "wait_window"), patch.object(notice, "validate_session_helper"), \
                        patch.object(notice.subprocess, "run", return_value=result), \
                        patch.object(notice, "save_private_json", side_effect=OSError("evidence volume unavailable")), \
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
                save = notice.save_private_json
                writes = []
                def save_with_first_failure(path, value):
                    writes.append(str(path))
                    if len(writes) == 1 or permanent:
                        raise OSError("first helper evidence unavailable")
                    return save(path, value)
                with self.subTest(timed_out=timed_out, permanent=permanent), patch.dict(notice.os.environ, {"ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS": "150"}), \
                        patch.object(notice.smoke, "case_context", return_value=dict(context)), \
                        patch.object(notice.smoke, "assert_process"), patch.object(notice, "wait_window"), \
                        patch.object(notice, "run", side_effect=lambda target: notice.control(target, deadline, "press", "unique")), \
                        patch.object(notice.subprocess, "run", side_effect=timeout if timed_out else None, return_value=result) as action, \
                        patch.object(notice, "save_private_json", side_effect=save_with_first_failure), redirect_stdout(output):
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
            context = {"evidence": root, "timeout": 150, "ax": root / "ax", "app_pid": 123,
                       "session_ax": root / "session", "app": root / "private.app",
                       "workspace": root, "data": root / "data", "case_id": "owned-case"}
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
                trace["turn"]["final"] = "Opening actual " + trace["turn"]["id"] + " observation.\n\nShared actual closing observation."
                return trace
            def snapshot(target, _, expected=""):
                self.assertNotEqual(expected, "开始目标")
                buttons = [{"AXRole": "AXButton", "AXTitle": label, "AXEnabled": True}
                           for label in ("对话", "Known title", "开始目标")]
                if target["app_pid"] == 456:
                    buttons.append({"AXRole": "AXButton", "AXTitle": "对话", "AXEnabled": True})
                body = ["Opening actual turn-1 observation.", "Opening actual turn-2 observation.",
                        "Shared actual closing observation.", "Shared actual closing observation."]
                return {"window_id": 456, "tree": buttons + [{"AXRole": "AXStaticText", "AXValue": text} for text in body]}
            helpers = []
            def native(command, **_):
                helpers.append(command)
                row = {"pid": context["app_pid"], "bundle": str(context["app"]), "action": command[2],
                       "ax_error": 0, "folds": [], "pressed": False}
                if command[2] == "read-up":
                    row.update(self.reading_row())
                else:
                    self.assertIn(command[2], ("inspect-text", "reveal-text"))
                    request = json.loads(command[4])
                    box = {"x": 100, "y": 100, "width": 100, "height": 20}
                    row.update(matched_count=1, target_count=1, selected_path=[0], reveal_count=int(command[2] == "reveal-text"),
                               targets=[{"owner_pid": context["app_pid"], "text": request["witness"], "path": [0],
                               "frame": box, "viewport": box, "in_view": True}])
                return SimpleNamespace(returncode=0, stdout=json.dumps(row), stderr="actual fixture native streams")
            actions = []
            with patch.object(notice.smoke.runner, "require_guest"), patch.object(notice.smoke, "validate_helper"), \
                    patch.object(notice.smoke, "wait_engine", return_value=("http://127.0.0.1:1", {"pid": 999})), \
                    patch.object(notice, "wait_window", side_effect=snapshot), \
                    patch.object(notice.smoke.runner, "process_argv", return_value=None), \
                    patch.object(notice, "wait_registered_application"), patch.object(notice, "api", side_effect=api), \
                    patch.object(notice, "prepare_goal_fixture", return_value=({"id": "thread-1"}, first, "Known title", "objective")), \
                    patch.object(notice, "control", side_effect=lambda _, __, action, value=None: actions.append((action, value))), \
                    patch.object(notice.smoke, "assert_process", return_value=executable), \
                    patch.object(notice, "validate_session_helper"), patch.object(notice.subprocess, "run", side_effect=native), \
                    patch.object(notice, "save_window") as pictures, \
                    patch.object(notice, "compact_fixture", return_value="stored real summary"), \
                    patch.object(notice, "exercise_inline_briefing"), patch.object(notice, "assert_inline_briefing"), \
                    patch.object(notice, "capture", return_value={"captured": True}) as capture, \
                    patch.object(notice.subprocess, "Popen", return_value=SimpleNamespace(pid=456, wait=lambda **_: 0)):
                receipt = notice.run(context)
            self.assertEqual(actions, [("reload", None), ("press", "对话"), ("press", "Known title"),
                                       ("reload", None), ("press", "Known title"), ("close", None), ("press", "Known title"), ("close", None)])
            self.assertEqual([call.args[2] for call in capture.call_args_list], ["01-resumed", "02-reloaded", "03-reopened"])
            self.assertEqual([call[2] for call in helpers], (["read-up"] + ["reveal-text", "inspect-text", "inspect-text"] * 4) * 3)
            self.assertEqual([call.args[2] for call in pictures.call_args_list],
                             [phase + "-answer" + str(turn) + "-" + edge for phase in ("01-resumed", "02-reloaded", "03-reopened")
                              for turn in (1, 2) for edge in ("opening", "closing")])
            for phase in ("01-resumed", "02-reloaded", "03-reopened"):
                self.assertTrue(json.loads((root / (phase + ".reading.json")).read_text())["wheel_posted"])
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
            with patch.object(notice, "wait_window", side_effect=states) as snapshot, \
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
                with self.subTest(rows=rows), patch.object(notice, "wait_window", return_value={"tree": rows}), \
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
            with patch.object(notice, "wait_window", return_value={"tree": [button, button]}), \
                    patch.object(notice, "control") as control, self.assertRaisesRegex(AssertionError, "ambiguous"):
                notice.press_available(context, Deadline(), "开始目标")
            control.assert_not_called()
            with patch.object(notice, "wait_window", return_value={"tree": [button]}) as snapshot, \
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
            with patch.object(notice, "wait_window", return_value={"tree": []}), \
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
            with patch.object(notice.smoke, "assert_process"), patch.object(notice, "wait_window"), \
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
                    patch.object(notice, "wait_window") as window, \
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
                self.assertEqual(Path(argv[-1]).stat().st_mode & 0o777, 0o600)
                Path(argv[-1]).write_bytes(b"captured fixture")
                return SimpleNamespace(returncode=0, stdout="capture stdout", stderr="capture stderr")
            with patch.object(notice, "session_control", side_effect=[{"folds": [{"expanded": False}]}, geometry]) as reveal, \
                    patch.object(notice, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture:
                self.assertEqual(notice.capture(context, Deadline(), "visible"), row)
            self.assertEqual([call.args[2] for call in reveal.call_args_list], ["inspect", "reveal-notice"])
            self.assertEqual(capture.call_args.args[0][3], "456")
            self.assertEqual(json.loads((Path(temporary) / "visible.viewport.json").read_text()), geometry)
            image = json.loads((Path(temporary) / "visible.image.json").read_text())
            self.assertEqual((image["pid"], image["window_id"]), (123, 456))
            for path in Path(temporary).iterdir():
                self.assertEqual(path.stat().st_mode & 0o777, 0o600, path.name)

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
                return SimpleNamespace(returncode=0, stdout="capture stdout", stderr="capture stderr")
            with patch.object(notice, "session_control", side_effect=[{"folds": [{"expanded": False}]}, geometry]), \
                    patch.object(notice, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture, \
                    self.assertRaisesRegex(AssertionError, "notice.*viewport"):
                notice.capture(context, Deadline(), "outside")
            capture.assert_not_called()

    def test_default_notice_capture_refuses_expanded_work_without_pressing_a_disclosure(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 123}
            row = {"tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
            with patch.object(notice, "wait_window", return_value=row), \
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
        with patch.object(notice, "wait_window", side_effect=[hidden, visible, hidden]), \
                patch.object(notice, "capture_target", return_value=visible) as target, \
                patch.object(notice, "press_available") as press, patch.object(notice, "save_window") as capture:
            notice.exercise_inline_briefing({}, object(), summary)
        self.assertEqual([call.args[2] for call in press.call_args_list], ["查看压缩后的简报", "收起简报"])
        self.assertEqual([call.args[2] for call in target.call_args_list], ["inline-briefing-opening", "inline-briefing-closing"])
        self.assertEqual([call.args[2] for call in capture.call_args_list], ["inline-briefing-collapsed"])
        with patch.object(notice, "wait_window", side_effect=[hidden, hidden]), \
                patch.object(notice, "press_available") as press, self.assertRaisesRegex(AssertionError, "briefing"):
            notice.exercise_inline_briefing({}, object(), summary)
        self.assertEqual(press.call_count, 1)

    def test_reading_refusal_and_post_capture_drift_keep_first_evidence_without_retry(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            context = {"evidence": root, "app_pid": 123}
            traces = [{"turn": {"final": "Opening actual observation.\n\nClosing actual observation."}}]
            loaded = {"tree": [{"AXRole": "AXStaticText", "AXValue": text} for text in notice.answer_witnesses(traces[0]["turn"]["final"])]}
            with patch.object(notice, "wait_window", return_value=loaded), \
                    patch.object(notice, "session_control", return_value={"wheel_posted": False}) as native, \
                    patch.object(notice, "capture_target") as target, self.assertRaisesRegex(AssertionError, "notice.*viewport"):
                notice.capture_answers(context, object(), "refused", traces)
            self.assertEqual([call.args[2] for call in native.call_args_list], ["read-up"])
            target.assert_not_called()
            self.assertFalse(json.loads((root / "refused.reading.json").read_text())["wheel_posted"])
            target = {"owner_pid": 123, "in_view": True, "frame": {"x": 10, "y": 20, "width": 100, "height": 20},
                      "viewport": {"x": 0, "y": 0, "width": 640, "height": 480}}
            before = {"pid": 123, "matched_count": 1, "target_count": 1, "selected_path": [0], "reveal_count": 1, "targets": [target]}
            after = {**before, "targets": [{**target, "frame": {**target["frame"], "y": 600}, "in_view": False}]}
            with patch.object(notice, "wait_window", return_value=loaded) as reveal, \
                    patch.object(notice, "session_control", side_effect=[before, {**before, "reveal_count": 0}, {**after, "reveal_count": 0}]) as inspect, \
                    patch.object(notice, "save_window") as screenshot, self.assertRaisesRegex(AssertionError, "remain inside"):
                notice.capture_target(context, object(), "drift", "Opening actual observation.", unique=True)
            self.assertEqual(reveal.call_count, 1)
            self.assertEqual([call.args[2] for call in inspect.call_args_list], ["reveal-text", "inspect-text", "inspect-text"])
            self.assertEqual([json.loads(call.args[3]).get("path") for call in inspect.call_args_list], [None, [0], [0]])
            screenshot.assert_called_once()
            self.assertEqual(json.loads((root / "drift.target-after.json").read_text()), {**after, "reveal_count": 0})

    def test_target_inspection_is_read_only_and_window_and_screenshot_calls_keep_complete_timing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "private.app"
            app.mkdir()
            context = {"evidence": root, "app_pid": 123, "app": app, "ax": root / "ax", "session_ax": root / "session",
                       "case_id": "own-case", "origin": "http://127.0.0.1:1"}
            deadline = SimpleNamespace(remaining=lambda _: 7)
            row = {"pid": 123, "bundle": str(app), "action": "inspect-text", "folds": [], "pressed": False, "ax_error": 0}
            def native(command, **_):
                if command[2] == "snapshot":
                    return SimpleNamespace(returncode=0, stdout=json.dumps({"bundle": str(app), "origin": context["origin"]}), stderr="window stderr")
                if command[0] == "/usr/sbin/screencapture":
                    Path(command[-1]).write_bytes(b"native capture fixture")
                    return SimpleNamespace(returncode=0, stdout="capture stdout", stderr="capture stderr")
                return SimpleNamespace(returncode=0, stdout=json.dumps(row), stderr="inspect stderr")
            with patch.object(notice.smoke, "assert_process", return_value=app / "Contents/MacOS/zwai"), \
                    patch.object(notice.smoke, "validate_helper", return_value={"binary_sha256": "pinned"}), \
                    patch.object(notice, "validate_session_helper"), patch.object(notice, "wait_window") as wait, \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=native) as calls:
                notice.session_control(context, deadline, "inspect-text", "unique opening witness")
                wait.assert_not_called()
                snapshot = notice.window_snapshot(context, deadline)
                notice.save_window(context, deadline, "target", {**snapshot, "window_id": 456})
            receipts = [json.loads(path.read_text()) for path in sorted(root.glob("goal-notice-helper-*.json"))]
            self.assertEqual([row["stderr"] for row in receipts], ["inspect stderr", "window stderr", "capture stderr"])
            self.assertEqual(receipts[0]["command"][-1], "unique opening witness")
            self.assertEqual(receipts[1]["command"][2:], ["snapshot", "own-case", str(app), context["origin"], ""])
            for receipt in receipts:
                self.assertGreaterEqual(receipt["finished_monotonic"], receipt["started_monotonic"])
                self.assertEqual(receipt["elapsed_seconds"], receipt["finished_monotonic"] - receipt["started_monotonic"])
            self.assertEqual(calls.call_args.kwargs["timeout"], 7)
            image = json.loads((root / "target.image.json").read_text())
            self.assertEqual(image["capture_started_monotonic"], receipts[-1]["started_monotonic"])

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


# Compile the complete current helper; only system observations/input are seams.
READING_OS_SEAMS = r"""
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <sys/sysctl.h>
static NSString *mode;
static AXUIElementRef app, win, area, web, group, receipt, second;
static int reveals=0, inputs=0, lookups=0;
@interface ReadingApp:NSObject
@property(readonly) BOOL active;
@property(readonly) BOOL finishedLaunching;
@property(readonly) pid_t processIdentifier;
@property(readonly) NSURL *bundleURL;
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid;
@end
@implementation ReadingApp
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid {lookups++; return [self new];}
-(BOOL)active {return ![mode isEqual:@"inactive"];}
-(BOOL)finishedLaunching {return YES;}
-(pid_t)processIdentifier {return 123;}
-(NSURL *)bundleURL {return [NSURL fileURLWithPath:([mode isEqual:@"refreshed-bundle"] && lookups>1) ? @"/other.app" : @"/tmp/reading-private.app"];}
@end
static int qa_vm(const char *n,void *out,size_t *len,void *in,size_t size) {*(int *)out=1;return 0;}
static bool qa_trusted(void) {return true;}
static AXUIElementRef qa_application(pid_t pid) {return (AXUIElementRef)CFRetain(app);}
static AXError qa_owner(AXUIElementRef e,pid_t *pid) {
    *pid=([mode isEqual:@"wrong-window-owner"] && CFEqual(e,win)) ||
         ([mode isEqual:@"wrong-notice-owner"] && CFEqual(e,receipt)) ? 999 : 123;
    return kAXErrorSuccess;
}
static AXError qa_get(AXUIElementRef e,CFStringRef key,CFTypeRef *out) {
    id value=nil;
    if (CFEqual(key,kAXWindowsAttribute)) value=[mode isEqual:@"proxy-window"] ? @[@"proxy"] : @[(__bridge id)win];
    if (CFEqual(key,kAXRoleAttribute)) value=CFEqual(e,win) ? ([mode isEqual:@"wrong-window-role"] ? @"AXGroup" : @"AXWindow") :
        CFEqual(e,area) ? @"AXScrollArea" : CFEqual(e,web) ? @"AXWebArea" :
        (CFEqual(e,receipt)||CFEqual(e,second)) ? @"AXStaticText" : @"AXGroup";
    if (CFEqual(key,kAXValueAttribute) && [mode isEqual:@"unknown-text"] && CFEqual(e,receipt)) return kAXErrorCannotComplete;
    if (CFEqual(key,kAXValueAttribute) && (CFEqual(e,receipt)||CFEqual(e,second)))
        value=[mode isEqual:@"no-notice"] ? @"other receipt" : @"继续追求目标。";
    if (CFEqual(key,kAXParentAttribute)) value=CFEqual(e,receipt)||CFEqual(e,second) ? (__bridge id)group :
        CFEqual(e,group) ? (__bridge id)web : CFEqual(e,web) ? (__bridge id)area :
        CFEqual(e,area) ? (__bridge id)win : nil;
    if (CFEqual(key,kAXChildrenAttribute)) value=CFEqual(e,win) ? @[(__bridge id)area] :
        CFEqual(e,area) ? @[(__bridge id)web] : CFEqual(e,web) ? @[(__bridge id)group] :
        CFEqual(e,group) ? (([mode isEqual:@"two-notices"]||[mode isEqual:@"shared-closing"]) ? @[(__bridge id)receipt,(__bridge id)second] : @[(__bridge id)receipt]) : @[];
    if (CFEqual(key,kAXPositionAttribute)||CFEqual(key,kAXSizeAttribute)) {
        if ([mode isEqual:@"unknown-ancestor"] && CFEqual(e,group)) return kAXErrorCannotComplete;
        CGPoint p=CGPointMake(0,30); CGSize s=CGSizeMake(800,600);
        if (CFEqual(e,group)) {p=CGPointMake(100,100);s=CGSizeMake(500,400);}
        if (CFEqual(e,receipt)||CFEqual(e,second)) {p=CGPointMake(120,[mode isEqual:@"outside-notice"]?700:200);s=CGSizeMake(100,20);}
        *out=CFEqual(key,kAXPositionAttribute) ? AXValueCreate(kAXValueCGPointType,&p) : AXValueCreate(kAXValueCGSizeType,&s);
        return kAXErrorSuccess;
    }
    if (!value) return kAXErrorNoValue;
    *out=CFBridgingRetain(value);return kAXErrorSuccess;
}
static AXError qa_action(AXUIElementRef e,CFStringRef a) {
    reveals++; return [mode isEqual:@"reveal-refused"] ? kAXErrorCannotComplete : kAXErrorSuccess;
}
static AXError qa_hit(AXUIElementRef e,float x,float y,AXUIElementRef *out) {
    *out=(AXUIElementRef)CFRetain([mode isEqual:@"different-hit"] ? group : receipt);return kAXErrorSuccess;
}
static void qa_post(pid_t pid,CGEventRef event) {
    if (pid!=123 || CGEventGetType(event)!=kCGEventScrollWheel || CGEventGetIntegerValueField(event,kCGScrollWheelEventDeltaAxis1)<=0) abort();
    inputs++;
}
#define NSRunningApplication ReadingApp
#define sysctlbyname qa_vm
#define AXIsProcessTrusted qa_trusted
#define AXUIElementCreateApplication qa_application
#define AXUIElementGetPid qa_owner
#define AXUIElementCopyAttributeValue qa_get
#define AXUIElementPerformAction qa_action
#define AXUIElementCopyElementAtPosition qa_hit
#define CGEventPostToPid qa_post
#define main reading_actual_main
"""


@unittest.skipUnless(platform.system() == "Darwin", "real Objective-C SDK required")
class ActualReadingHelper(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="goal-reading-actual-main-")
        cls.addClassCleanup(cls.temporary.cleanup)
        root = Path(cls.temporary.name)
        source = root / "reading.m"
        source.write_text(READING_OS_SEAMS + '#include "' + str(notice.SESSION_SOURCE) + '"\n' + r"""
#undef main
#undef AXUIElementCreateApplication
int main(int argc,const char **argv) {@autoreleasepool {
    if(argc!=3)return 7;mode=@(argv[1]);
    app=AXUIElementCreateApplication(1);win=AXUIElementCreateApplication(2);area=AXUIElementCreateApplication(3);
    web=AXUIElementCreateApplication(4);group=AXUIElementCreateApplication(5);
    receipt=AXUIElementCreateApplication(6);second=AXUIElementCreateApplication(7);
    setenv("ZWAI_NATIVE_APP","/tmp/reading-private.app",1);setenv("ZWAI_NATIVE_CASE_ID","reading-case",1);
    const char *descriptor=strcmp(argv[2],"inspect-text")==0 ? "{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[0,0,0,0]}" :
        "{\"witness\":\"继续追求目标。\",\"unique\":true}";
    if ([mode isEqual:@"shared-closing"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":false}";
    if ([mode isEqual:@"wrong-path"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[9]}";
    if ([mode isEqual:@"malformed-path"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[false,false,false,false]}";
    const char *args[]={"reading","123",argv[2],"/tmp/reading-private.app",descriptor};
    int code=reading_actual_main(strstr(argv[2],"-text")?5:4,args);
    fprintf(stderr,"{\"reveals\":%d,\"inputs\":%d}\n",reveals,inputs);return code;
}}
""")
        cls.binary = root / "reading"
        built = subprocess.run(["/usr/bin/xcrun", "clang", *notice.smoke.AX_FLAGS, str(source), "-o", str(cls.binary)],
                               capture_output=True, text=True)
        if built.returncode:
            raise RuntimeError(built.stderr)
        cls.real_binary = root / "unmodified-main"
        notice.prepare_session_helper(cls.real_binary)

    def test_real_sdk_main_refuses_before_any_target_or_ui_is_requested(self):
        result = subprocess.run([str(self.real_binary)], capture_output=True, text=True, timeout=3)
        self.assertEqual(result.returncode, 2)
        self.assertIn(json.loads(result.stdout)["error"],
                      ("macOS VM required; physical host refused", "expected private pid, action, bundle and optional text"))
        notice.validate_session_helper(self.real_binary)

    def observe(self, mode="good", action="read-up"):
        result = subprocess.run([str(self.binary), mode, action], capture_output=True, text=True, timeout=3)
        return result.returncode, json.loads(result.stdout), json.loads(result.stderr)

    def test_actual_main_posts_one_upward_wheel_only_at_the_unique_notice(self):
        code, row, actions = self.observe()
        self.assertEqual((code, actions), (0, {"reveals": 1, "inputs": 1}))
        notice.assert_reading_admission(row)
        self.assertEqual(row["viewport_frame"], {"x": 100, "y": 100, "width": 500, "height": 400})
        self.assertEqual(row["notice_frame"], {"x": 120, "y": 200, "width": 100, "height": 20})
        self.assertEqual(row["point"], {"x": 170, "y": 210})

    def test_real_window_notice_ancestry_hit_and_refresh_refusals_never_post_input(self):
        modes = ("proxy-window", "wrong-window-role", "wrong-window-owner", "wrong-notice-owner",
                 "no-notice", "two-notices", "unknown-text", "unknown-ancestor", "outside-notice", "different-hit",
                 "reveal-refused", "inactive", "refreshed-bundle")
        for mode in modes:
            with self.subTest(mode=mode):
                code, row, actions = self.observe(mode)
                self.assertNotEqual(code, 0)
                self.assertIn("error", row)
                self.assertEqual(actions["inputs"], 0)
                self.assertLessEqual(actions["reveals"], 1)

    def test_one_reveal_selects_one_recorded_path_even_for_explicit_common_closing_evidence(self):
        for mode, matched in (("good", 1), ("shared-closing", 2)):
            code, row, actions = self.observe(mode, "reveal-text")
            self.assertEqual((code, actions), (0, {"reveals": 1, "inputs": 0}))
            self.assertEqual((row["matched_count"], row["selected_path"], row["reveal_count"]), (matched, [0, 0, 0, 0], 1))
            notice.assert_target_geometry(row, unique=matched == 1)
        for mode in ("two-notices", "wrong-path", "malformed-path"):
            code, row, actions = self.observe(mode, "inspect-text" if mode != "two-notices" else "reveal-text")
            self.assertNotEqual(code, 0)
            self.assertEqual(actions, {"reveals": 0, "inputs": 0})
            self.assertIn("error", row)

    def test_actual_inspection_reads_clipped_target_geometry_without_reveal_or_input(self):
        code, row, actions = self.observe(action="inspect-text")
        self.assertEqual((code, actions), (0, {"reveals": 0, "inputs": 0}))
        notice.assert_target_geometry(row, unique=True)
        self.assertEqual(row["targets"][0]["viewport"], {"x": 100, "y": 100, "width": 500, "height": 400})


if __name__ == "__main__":
    unittest.main()
