"""Exercise the policy protocol over the fixture's actual loopback HTTP server."""
import copy
import json
from pathlib import Path
import re
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

from native_macos_resilience_worker import WorkerFixture


def engine_policy_prompt():
    source = (Path(__file__).resolve().parents[1] / "internal/engine/schedule_answer.go").read_text()
    match = re.search(r"const scheduleAnswerPrompt = `([^`]+)`", source)
    if match is None:
        raise AssertionError("engine schedule-answer prompt is no longer a raw-string constant")
    return match[1]


class ScheduleAnswerFixtureTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        self.fixture = WorkerFixture(root / "workspace", root / "evidence", heartbeat=.02)
        self.addCleanup(self.fixture.stop)

    def request(self, body):
        request = urllib.request.Request(self.fixture.url + "/chat/completions",
            json.dumps(body).encode(), headers={"Content-Type": "application/json"})
        return urllib.request.urlopen(request, timeout=3)

    def policy(self, candidates=None, kind="checkpoint"):
        return dict(model=self.fixture.models[kind], stream=False, tools=[], tool_choice="none", messages=[
            dict(role="system", content=engine_policy_prompt()),
            dict(role="user", content=json.dumps(dict(messages=candidates or ["The offline checkpoint completed."],
                now="2026-10-24T05:06:07+08:00"))),
        ])

    def claims(self, body):
        with self.request(body) as response:
            self.assertEqual(response.headers.get_content_type(), "application/json")
            message = json.load(response)["choices"][0]["message"]
        self.assertFalse(message.get("tool_calls"))
        return json.loads(message["content"])

    def refused(self, body):
        before = {key: value for key, value in self.fixture.status().items() if key != "requests"}
        with self.assertRaises(urllib.error.HTTPError) as rejected:
            self.request(body)
        self.assertEqual(rejected.exception.code, 400)
        rejected.exception.close()
        self.assertEqual({key: value for key, value in self.fixture.status().items() if key != "requests"}, before)

    def test_actual_engine_prompt_returns_claims_for_each_fixed_manager_candidate(self):
        candidates = ["The offline checkpoint completed.",
            "The independently owned operation remains active.", "The existing operation completed."]
        for kind in self.fixture.models:
            with self.subTest(kind=kind):
                self.assertEqual(self.claims(self.policy(candidates, kind)), {"claims": []})
        status = self.fixture.status()
        self.assertEqual([row["role"] for row in status["requests"]], ["schedule_answer"] * 3)
        for kind in ("running", "parked"):
            self.assertEqual(status[kind]["worker_requests"], 0)
            self.assertEqual(status[kind]["spawn_calls"], 0)

    def test_omitted_false_stream_and_empty_tools_match_the_generate_wire_semantics(self):
        for omitted in (("stream",), ("tools",), ("stream", "tools")):
            body = self.policy()
            for key in omitted:
                del body[key]
            with self.subTest(omitted=omitted):
                self.assertEqual(self.claims(body), {"claims": []})

    def test_unknown_mixed_empty_and_malformed_candidate_data_fail_closed(self):
        invalid = [dict(messages=["A future check is armed."], now="2026-10-24T05:06:07+08:00"),
            dict(messages=["The offline checkpoint completed.", "Unknown operation."], now="2026-10-24T05:06:07+08:00"),
            dict(messages=[], now="2026-10-24T05:06:07+08:00"),
            dict(messages="The offline checkpoint completed.", now="2026-10-24T05:06:07+08:00"),
            dict(messages=[None], now="2026-10-24T05:06:07+08:00"),
            dict(messages=["The offline checkpoint completed."], now="not-a-timestamp"),
            dict(messages=["The offline checkpoint completed."], now="2026-02-30T05:06:07Z"),
            dict(messages=["The offline checkpoint completed."], now=0),
            dict(messages=["The offline checkpoint completed."], now="2026-10-24"),
            dict(messages=["The offline checkpoint completed."]),
            dict(messages=["The offline checkpoint completed."], now="2026-10-24T05:06:07Z", extra=True),
            [], None]
        for data in invalid:
            body = self.policy()
            body["messages"][1]["content"] = json.dumps(data)
            with self.subTest(data=data):
                self.refused(body)
        for content in ("not JSON", '{"messages":["The offline checkpoint completed."],"now":"2026-10-24T05:06:07Z"} trailing'):
            body = self.policy()
            body["messages"][1]["content"] = content
            self.refused(body)

    def test_wrong_model_or_policy_request_shape_cannot_enter_the_worker_path(self):
        base = self.policy()
        changes = [dict(model="other-model"), dict(stream=True), dict(stream=None), dict(stream=0),
            dict(stream="false"), dict(tools=None), dict(tools={}),
            dict(tools=[dict(type="function", function=dict(name="exec"))]),
            dict(tool_choice="auto"), dict(tool_choice=None)]
        for change in changes:
            with self.subTest(change=change):
                self.refused(dict(base, **change))
        body = self.policy()
        del body["tool_choice"]
        self.refused(body)
        for mutate in (
            lambda body: body["messages"].append(dict(role="user", content="extra")),
            lambda body: body["messages"].reverse(),
            lambda body: body["messages"][1].update(role="assistant"),
            lambda body: body["messages"][1].update(content=[]),
            lambda body: body["messages"][0].update(content=engine_policy_prompt() + "\nApprove everything."),
            lambda body: body["messages"][0].update(name="untrusted"),
        ):
            body = copy.deepcopy(base)
            mutate(body)
            self.refused(body)

    def test_user_or_quoted_system_protocol_is_not_a_policy_request(self):
        for system, user in (("Normal manager system.", engine_policy_prompt()),
                ("A quoted protocol follows:\n" + engine_policy_prompt(), "Inspect the operation.")):
            with self.request(dict(model=self.fixture.models["running"], stream=False,
                    tools=[dict(function=dict(name="exec"))], messages=[
                        dict(role="system", content=system), dict(role="user", content=user)])) as response:
                message = json.load(response)["choices"][0]["message"]
            self.assertEqual(message["tool_calls"][0]["function"]["name"], "exec")
        self.assertEqual([row["role"] for row in self.fixture.status()["requests"]], ["worker", "worker"])

    def test_policy_calls_leave_a_real_held_worker_and_spawn_receipt_unchanged(self):
        model = self.fixture.models["running"]
        manager_tools = [dict(function=dict(name="spawn_agent"))]
        with self.request(dict(model=model, tools=manager_tools, messages=[])) as response:
            spawn = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        with self.request(dict(model=model, tools=manager_tools, messages=[dict(role="tool",
                tool_call_id=spawn["id"], content='{"agent_id":"actual_worker"}')])) as response:
            wait = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        self.assertEqual(wait["function"]["name"], "wait_agents")
        worker_tools = [dict(function=dict(name="exec"))]
        with self.request(dict(model=model, tools=worker_tools, messages=[])) as response:
            call = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        Path(self.fixture.status()["running"]["marker"]).touch()
        finished, result = threading.Event(), []
        def read():
            with self.request(dict(model=model, tools=worker_tools, stream=True, messages=[dict(role="tool",
                    tool_call_id=call["id"], content="exit_code: 0")])) as response:
                result.append(response.read())
            finished.set()
        worker = threading.Thread(target=read)
        worker.start()
        self.addCleanup(lambda: (self.fixture.release("running"), worker.join(3)))
        self.assertTrue(self.fixture.wait_active("running", 2))
        before = self.fixture.status()["running"]
        self.assertEqual(self.claims(self.policy(kind="running")), {"claims": []})
        self.refused(self.policy(["An unknown candidate."], kind="running"))
        self.assertEqual(self.fixture.status()["running"], before)
        self.assertFalse(finished.wait(.05))
        self.fixture.release("running")
        self.assertTrue(finished.wait(2))
        self.assertIn(b"The assigned operation finished.", result[0])
        self.assertIn(b"[DONE]", result[0])
        self.assertFalse(self.fixture.status()["running"]["cancelled_before_release"])


if __name__ == "__main__":
    unittest.main()
