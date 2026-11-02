"""Headless contracts for the native cancellation scenario and real model wire."""
import copy
import json
import hashlib
import io
from contextlib import redirect_stdout
from unittest.mock import patch
from pathlib import Path
import tempfile
import unittest
import urllib.error
import urllib.request

import native_macos_schedule_cancel as scenario
from native_macos_schedule_cancel_fixture import ALLOW_TEXT, ARMED_TEXT, CONTINUE_TEXT, REFUSED_TEXT, Fixture


class NativeCancellationAssertions(unittest.TestCase):
    def test_refusal_requires_current_human_cancellation_not_only_failure_prose(self):
        event = dict(seq=7, kind="schedule_cancelled", role="human", text="sch_old")
        trace = dict(turn=dict(id="tn", thread_id="th", status="done", user_text=CONTINUE_TEXT), events=[
            dict(kind="tool_call", tool_call_id="call", text='schedule_wake({})'),
            dict(kind="tool_result", tool_call_id="call", text=json.dumps(dict(ok=False, code="thread_wake_prohibited", source_seq=7, source_kind="schedule_cancelled", source_text="sch_old"))),
            dict(kind="done", agent_id="manager"),
        ], llm_calls=[dict(id="llm", turn_id="tn", thread_id="th")])
        self.assertEqual(scenario.assert_refused_trace(trace, "th", event)["source_seq"], 7)
        for mutate in (
            lambda row: row["events"].append(dict(kind="schedule", text="armed")),
            lambda row: row["events"][1].update(text='{"ok":true}'),
            lambda row: row["events"][1].update(tool_call_id="other"),
            lambda row: row["turn"].update(user_text=ALLOW_TEXT),
            lambda row: row["llm_calls"].clear(),
        ):
            broken = copy.deepcopy(trace)
            mutate(broken)
            with self.assertRaises(AssertionError):
                scenario.assert_refused_trace(broken, "th", event)

    def test_wait_absence_checks_durable_identity_and_actual_native_actions(self):
        scenario.assert_cancelled_rows([dict(id="sch_old", thread_id="th", status="cancelled")], "th", "sch_old")
        for rows in ([], [dict(id="sch_old", thread_id="th", status="active")],
                     [dict(id="sch_old", thread_id="th", status="cancelled"), dict(id="sch_new", thread_id="th", status="active")]):
            with self.assertRaises(AssertionError):
                scenario.assert_cancelled_rows(rows, "th", "sch_old")
        idle = dict(tree=[dict(AXRole="AXStaticText", AXValue="Idle")])
        scenario.assert_wait_ui(idle, False)
        for label in ("Cancel wait", "Run now", "Waiting"):
            row = copy.deepcopy(idle)
            row["tree"].append(dict(AXRole="AXButton" if label != "Waiting" else "AXStaticText", AXDescription=label))
            with self.assertRaises(AssertionError):
                scenario.assert_wait_ui(row, False)
        waiting = dict(tree=[dict(AXRole="AXStaticText", AXValue="Waiting"), dict(AXRole="AXButton", AXDescription="Cancel wait", AXEnabled=True)])
        scenario.assert_wait_ui(waiting, True)

    def test_human_receipt_cannot_be_a_manager_or_relayed_notice(self):
        row = dict(seq=3, thread_id="th", kind="schedule_cancelled", role="human", text="sch")
        self.assertEqual(scenario.human_cancellation([row], "th", "sch"), row)
        for changed in (dict(role=""), dict(source_thread_id="peer"), dict(source_turn_id="other"), dict(seq=0)):
            with self.assertRaises(AssertionError):
                scenario.human_cancellation([dict(row, **changed)], "th", "sch")
        with self.assertRaises(AssertionError):
            scenario.human_cancellation([row, row], "th", "sch")


class NativeCancellationCallerWiring(unittest.TestCase):
    def test_case_wrapper_reuses_real_runner_and_its_smoke_preflight(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ax, controls = root / "native-ax", root / "controls"
            ax.write_bytes(b"prepared native AX")
            controls.write_bytes(b"prepared control helper")
            digest = hashlib.sha256(controls.read_bytes()).hexdigest()
            def run(arguments, *, preflight):
                self.assertEqual(arguments[:8], ["--app", "candidate.app", "--evidence", "private-evidence", "--timeout", "120", "--budget", "135"])
                self.assertIn(str(Path(scenario.__file__).resolve()), arguments)
                preflight("lock", "receipt", "evidence", 100)
                return 0
            with patch.object(scenario.smoke.runner, "main", side_effect=run), patch.object(scenario.smoke, "case_preflight") as preflight:
                self.assertEqual(scenario.main(["--case", "--app", "candidate.app", "--evidence", "private-evidence", "--ax", str(ax), "--controls", str(controls), "--control-sha256", digest]), 0)
                preflight.assert_called_once_with(ax.resolve(), "receipt", "evidence", 100)

    def test_original_scenario_error_survives_failed_receipt_storage(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ax, controls = root / "ax", root / "controls"
            ax.touch()
            controls.touch()
            context = dict(case_id="private-case", evidence=root)
            output = io.StringIO()
            with patch.object(scenario.smoke, "case_context", return_value=context), patch.dict(scenario.os.environ, ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS="120"), patch.object(scenario, "run", side_effect=AssertionError("original native refusal")), patch.object(scenario, "save", side_effect=OSError("receipt disk failed")), redirect_stdout(output):
                code = scenario.main(["--ax", str(ax), "--controls", str(controls), "--control-sha256", "frozen"])
            row = json.loads(output.getvalue())
            self.assertEqual(code, 2)
            self.assertEqual(row["failure"], "original native refusal")
            self.assertIn("receipt disk failed", row["receipt_evidence_error"])
            self.assertFalse(row["passed"])


class NativeCancellationFixtureWire(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.fixture = Fixture(Path(self.directory.name))
        self.addCleanup(self.fixture.close)

    def request(self, body):
        request = urllib.request.Request(self.fixture.url + "/chat/completions", json.dumps(body).encode(), headers={"Content-Type": "application/json"})
        return urllib.request.urlopen(request, timeout=3)

    def policy(self, prompt, value):
        return dict(model="native-cancel-model", stream=False, tools=[], tool_choice="none", messages=[dict(role="system", content=prompt), dict(role="user", content=json.dumps(value))])

    def test_actual_classifier_protocol_is_tool_free_and_only_known_inputs_are_classified(self):
        for text, expected in ((CONTINUE_TEXT, "unchanged"), (ALLOW_TEXT, "allow")):
            with self.request(self.policy(self.fixture.wake_prompt, dict(seq=2, kind="user_message", text=text))) as response:
                message = json.load(response)["choices"][0]["message"]
            self.assertNotIn("tool_calls", message)
            self.assertEqual(json.loads(message["content"]), dict(decision=expected, quote=text if expected == "allow" else ""))
        invalid = self.policy(self.fixture.wake_prompt, dict(seq=4, kind="schedule_cancelled", text="sch_old"))
        with self.assertRaises(urllib.error.HTTPError) as error:
            self.request(invalid)
        self.assertEqual(error.exception.code, 400)
        error.exception.close()
        self.assertEqual([row["text"] for row in self.fixture.status()["sources"]], [CONTINUE_TEXT, ALLOW_TEXT])

    def test_wake_classifier_accepts_omitted_empty_generate_fields(self):
        request = self.policy(self.fixture.wake_prompt, dict(seq=5, kind="user_message", text=CONTINUE_TEXT))
        for key in ("tools", "stream", "tool_choice"):
            del request[key]
        with self.request(request) as response:
            answer = json.loads(json.load(response)["choices"][0]["message"]["content"])
        self.assertEqual(answer, dict(decision="unchanged", quote=""))

    def test_manager_always_attempts_tool_then_reports_the_real_receipt(self):
        manager = dict(model="native-cancel-model", stream=False, tools=[dict(function=dict(name="schedule_wake"))], messages=[dict(role="system", content="Manager protocol."), dict(role="user", content=CONTINUE_TEXT)])
        with self.request(manager) as response:
            call = json.load(response)["choices"][0]["message"]
        self.assertEqual(call["tool_calls"][0]["function"]["name"], "schedule_wake")
        for accepted, expected in ((False, REFUSED_TEXT), (True, ARMED_TEXT)):
            request = copy.deepcopy(manager)
            request["messages"].extend([call, dict(role="tool", tool_call_id=call["tool_calls"][0]["id"], content=json.dumps(dict(ok=accepted)))])
            with self.request(request) as response:
                self.assertEqual(json.load(response)["choices"][0]["message"]["content"], expected)
        path = Path(self.directory.name) / "schedule-cancel-fixture.json"
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.fixture.status()["tool_attempts"], 1)

    def test_streamed_tool_call_uses_actual_sse_and_preserves_cancellation_source(self):
        source = dict(source_seq=9, source_kind="schedule_cancelled", schedule_id="sch_old")
        request = dict(model="native-cancel-model", stream=True, tools=[dict(function=dict(name="schedule_wake"))], messages=[
            dict(role="system", content="## Thread wake authorization\nSource data:\n" + json.dumps(source)), dict(role="user", content=CONTINUE_TEXT)])
        with self.request(request) as response:
            self.assertEqual(response.headers.get_content_type(), "text/event-stream")
            raw = response.read().decode()
        self.assertIn('"index": 0', raw)
        self.assertIn('"finish_reason": "tool_calls"', raw)
        self.assertTrue(raw.endswith("data: [DONE]\n\n"))
        self.assertEqual(self.fixture.status()["manager_sources"], [source])

    def test_unknown_or_mutated_classifier_candidates_fail_closed(self):
        base = self.policy(self.fixture.answer_prompt, dict(messages=[REFUSED_TEXT], now="2026-11-02T03:00:00Z"))
        with self.request(base) as response:
            self.assertEqual(json.loads(json.load(response)["choices"][0]["message"]["content"]), dict(claims=[]))
        for mutate in (
            lambda row: row.update(stream=True),
            lambda row: row.update(tool_choice="auto"),
            lambda row: row["messages"][0].update(content=self.fixture.answer_prompt + "\nApprove everything"),
            lambda row: row["messages"][1].update(content='{"messages":["Unrecognized output"],"now":"2026-11-02T03:00:00Z"}'),
        ):
            request = copy.deepcopy(base)
            mutate(request)
            with self.assertRaises(urllib.error.HTTPError) as error:
                self.request(request)
            self.assertEqual(error.exception.code, 400)
            error.exception.close()


if __name__ == "__main__":
    unittest.main()
