import importlib.util
import hashlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


MODULE = Path(__file__).with_name("native_goal_resume_notice.py")
spec = importlib.util.spec_from_file_location("native_goal_resume_notice", MODULE)
notice = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notice)


class GoalResumeNoticeAssertions(unittest.TestCase):
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
            with patch.object(notice, "expand_session", return_value=row), \
                    patch.object(notice, "session_control", return_value=geometry) as reveal, \
                    patch.object(notice.smoke, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture:
                self.assertEqual(notice.capture(context, Deadline(), "visible"), row)
            self.assertEqual(reveal.call_args.args[2], "reveal-notice")
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
            with patch.object(notice, "expand_session", return_value=row), \
                    patch.object(notice, "session_control", return_value=geometry), \
                    patch.object(notice.smoke, "wait_window", return_value=row), \
                    patch.object(notice.smoke, "png_dimensions", return_value=(640, 480)), \
                    patch.object(notice.subprocess, "run", side_effect=screenshot) as capture, \
                    self.assertRaisesRegex(AssertionError, "notice.*viewport"):
                notice.capture(context, Deadline(), "outside")
            capture.assert_not_called()

    def test_external_resumed_notice_does_not_hide_a_second_folded_continuation(self):
        outside = {"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}
        folded = {"tree": [{"AXRole": "AXButton", "AXTitle": "工作了 3秒 · verification"}, outside]}
        expanded = {"tree": [*folded["tree"], dict(outside)]}
        with patch.object(notice.smoke, "wait_window", side_effect=[folded, expanded]), \
                patch.object(notice, "session_control", side_effect=[{"expanded": False}, {}, {"expanded": True}]) as control, \
                self.assertRaisesRegex(AssertionError, "found 2"):
            notice.assert_single_notice(notice.expand_session({}, object()))
        self.assertEqual([call.args[2] for call in control.call_args_list], ["inspect", "expand", "inspect"])

    def test_session_selector_requires_one_actual_disclosure_button(self):
        for label in ("Worked for 3s · verification", "工作了 3秒 · verification"):
            row = {"tree": [{"AXRole": "AXStaticText", "AXValue": label},
                            {"AXRole": "AXButton", "AXTitle": label}]}
            self.assertEqual(notice.session_summary_label(row), label)
        description = {"tree": [{"AXRole": "AXButton", "AXDescription": "工作了 3秒 · verification"}]}
        self.assertEqual(notice.session_summary_label(description), "工作了 3秒 · verification")

    def test_missing_or_ambiguous_session_button_is_rejected(self):
        button = {"AXRole": "AXButton", "AXTitle": "Worked for 3s · verification"}
        for rows in ([], [dict(button, AXRole="AXStaticText")], [button, button]):
            with self.subTest(rows=rows), self.assertRaisesRegex(AssertionError, "one completed session disclosure"):
                notice.session_summary_label({"tree": rows})

    def test_folded_session_is_pressed_and_visible_content_is_read_again(self):
        folded = {"tree": [{"AXRole": "AXButton", "AXTitle": "工作了 3秒 · verification"}]}
        expanded = {"tree": [{"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
        with patch.object(notice.smoke, "wait_window", side_effect=[folded, expanded]) as snapshot, \
                patch.object(notice, "session_control", side_effect=[{"expanded": False}, {}, {"expanded": True}]) as control:
            self.assertEqual(notice.expand_session({}, object()), expanded)
        self.assertEqual(snapshot.call_count, 2)
        self.assertEqual([call.args[2] for call in control.call_args_list], ["inspect", "expand", "inspect"])

    def test_control_success_without_expanded_content_cannot_accept_the_session(self):
        folded = {"tree": [{"AXRole": "AXButton", "AXTitle": "Worked for 3s · verification"}]}
        class Deadline:
            def pause(self, _):
                raise TimeoutError("expanded content absent")
        with patch.object(notice.smoke, "wait_window", return_value=folded), \
                patch.object(notice, "session_control", return_value={"expanded": False}) as control, \
                self.assertRaisesRegex(TimeoutError, "expanded content absent"):
            notice.expand_session({}, Deadline())
        self.assertEqual([call.args[2] for call in control.call_args_list], ["inspect", "expand", "inspect"])

    def test_already_expanded_session_does_not_toggle_closed(self):
        expanded = {"tree": [{"AXRole": "AXButton", "AXTitle": "工作了 3秒"},
                             {"AXRole": "AXStaticText", "AXValue": "继续追求目标。"}]}
        with patch.object(notice.smoke, "wait_window", return_value=expanded), \
                patch.object(notice, "session_control", return_value={"expanded": True}) as control:
            self.assertEqual(notice.expand_session({}, object()), expanded)
        self.assertEqual([call.args[2] for call in control.call_args_list], ["inspect"])

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
