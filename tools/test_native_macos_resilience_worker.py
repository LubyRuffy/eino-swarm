import json
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

from native_macos_resilience_worker import WorkerFixture


class WorkerFixtureTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.fixture = WorkerFixture(root / "workspace", root / "evidence", heartbeat=.02)
        self.addCleanup(self.fixture.stop)

    def request(self, model, tools=None, messages=None, stream=False):
        data = json.dumps(dict(model=model, tools=tools or [], messages=messages or [], stream=stream)).encode()
        return urllib.request.urlopen(urllib.request.Request(self.fixture.url + "/chat/completions", data,
            headers={"Content-Type": "application/json"}), timeout=3)

    def test_manager_uses_actual_spawn_receipt_and_never_spawns_a_second_worker(self):
        model = self.fixture.models["running"]
        tools = [{"function": {"name": "spawn_agent"}}]
        with self.request(model, tools) as response:
            first = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        self.assertEqual(first["function"]["name"], "spawn_agent")
        receipt = [{"role": "tool", "tool_call_id": first["id"], "content": '{"agent_id":"actual_worker"}'}]
        with self.request(model, tools, receipt) as response:
            second = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        self.assertEqual(second["function"]["name"], "wait_agents")
        self.assertEqual(json.loads(second["function"]["arguments"])["agent_ids"], ["actual_worker"])
        state = self.fixture.status()["running"]
        self.assertEqual(state["worker_id"], "actual_worker")
        self.assertEqual(state["spawn_calls"], 1)

    def test_real_stream_survives_until_release_and_requires_tool_side_effect(self):
        model = self.fixture.models["parked"]
        tools = [{"function": {"name": "exec"}}]
        with self.request(model, tools) as response:
            call = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        marker = Path(self.fixture.status()["parked"]["marker"])
        self.assertIn(str(marker), json.loads(call["function"]["arguments"])["command"])
        marker.touch()  # The native case executes the returned command through the real engine.
        messages = [{"role": "tool", "tool_call_id": call["id"], "content": "exit_code: 0"}]
        finished = threading.Event()
        result = []
        def read():
            with self.request(model, tools, messages, True) as response:
                result.append(response.read())
            finished.set()
        worker = threading.Thread(target=read)
        worker.start()
        self.addCleanup(lambda: (self.fixture.release("parked"), worker.join(3)))
        self.assertTrue(self.fixture.wait_active("parked", 2))
        before = self.fixture.status()["parked"]
        self.assertEqual(before["worker_requests"], 2)
        self.assertFalse(finished.wait(.05))
        self.fixture.release("parked")
        self.assertTrue(finished.wait(2))
        self.assertIn(b"[DONE]", result[0])
        self.assertFalse(self.fixture.status()["parked"]["cancelled_before_release"])

    def test_unexecuted_tool_and_unexpected_model_fail_without_fake_progress(self):
        with self.assertRaises(urllib.error.HTTPError) as rejected:
            self.request("not_a_fixture_model")
        rejected.exception.close()
        model = self.fixture.models["running"]
        with self.request(model, [{"function": {"name": "exec"}}]) as response:
            call = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        with self.assertRaises(urllib.error.HTTPError) as rejected:
            self.request(model, [{"function": {"name": "exec"}}],
                         [{"role": "tool", "tool_call_id": call["id"], "content": "done"}], True)
        rejected.exception.close()
        self.assertFalse(self.fixture.status()["running"]["stream_active"])

    def test_stop_is_bounded_and_is_not_a_successful_release(self):
        self.fixture.stop()
        self.fixture.stop()
        self.assertFalse(self.fixture.status()["running"]["released"])

    def test_keyless_fixture_rejects_credentials_before_any_model_work(self):
        request = urllib.request.Request(self.fixture.url + "/chat/completions",
            json.dumps(dict(model=self.fixture.models["checkpoint"])).encode(),
            headers={"Authorization": "Bearer test-placeholder"})
        with self.assertRaises(urllib.error.HTTPError) as rejected:
            urllib.request.urlopen(request, timeout=2)
        rejected.exception.close()
        self.assertEqual(self.fixture.status()["requests"], [])

    def test_session_briefing_uses_the_fixed_system_protocol_without_consuming_a_worker_request(self):
        header = "Write a dense briefing of the conversation so a later turn can continue the work, and so a later memory review can see what was learned."
        self.assertIn("return `" + header, (Path(__file__).resolve().parents[1] / "internal/engine/session_memory.go").read_text())
        for text in ("Inspect the current operation.", "不同的输入与工具 JSON 不选择场景。"):
            with self.request(self.fixture.models["running"], messages=[
                dict(role="system", content=header + "\n\nReply with only the briefing."),
                dict(role="user", content=text),
            ]) as response:
                message = json.load(response)["choices"][0]["message"]
            self.assertTrue(message.get("content"))
            self.assertFalse(message.get("tool_calls"))
            self.assertEqual(self.fixture.status()["running"]["worker_requests"], 0)
        self.assertEqual([row["role"] for row in self.fixture.status()["requests"]], ["briefing", "briefing"])

    def test_a_human_quote_of_the_briefing_header_does_not_select_the_system_protocol(self):
        with self.request(self.fixture.models["running"], tools=[{"function": {"name": "exec"}}], messages=[
            dict(role="system", content="Inspect the assigned operation."),
            dict(role="user", content="Write a dense briefing of the conversation so a later turn can continue the work, and so a later memory review can see what was learned."),
        ]) as response:
            message = json.load(response)["choices"][0]["message"]
        self.assertEqual(message["tool_calls"][0]["function"]["name"], "exec")
        self.assertEqual(self.fixture.status()["running"]["worker_requests"], 1)

    def test_release_waits_for_actual_done_and_each_wait_call_has_its_own_identity(self):
        model, kind = self.fixture.models["running"], "running"
        tools = [{"function": {"name": "spawn_agent"}}]
        with self.request(model, tools) as response:
            spawn = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        messages = [{"role": "tool", "tool_call_id": spawn["id"], "content": '{"agent_id":"actual_worker"}'}]
        with self.request(model, tools, messages) as response:
            wait = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        self.fixture.release(kind)
        messages.append(dict(role="tool", tool_call_id=wait["id"], content=json.dumps(dict(agents=[dict(agent_id="actual_worker", status="running")]))))
        with self.request(model, tools, messages) as response:
            next_wait = json.load(response)["choices"][0]["message"].get("tool_calls", [{}])[0]
        self.assertEqual(next_wait.get("function", {}).get("name"), "wait_agents")
        self.assertNotEqual(next_wait["id"], wait["id"])
        messages.append(dict(role="tool", tool_call_id=next_wait["id"], content=json.dumps(dict(agents=[dict(agent_id="actual_worker", status="done")]))))
        with self.request(model, tools, messages) as response:
            final = json.load(response)["choices"][0]["message"]
        self.assertFalse(final.get("tool_calls"))

    def test_parked_collection_reports_only_in_an_actual_scheduled_cue(self):
        model = self.fixture.models["parked"]
        tools = [{"function": {"name": "spawn_agent"}}]
        with self.request(model, tools) as response:
            spawn = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        messages = [{"role": "tool", "tool_call_id": spawn["id"], "content": '{"agent_id":"actual_worker"}'}]
        with self.request(model, tools, messages) as response:
            parked = json.load(response)["choices"][0]["message"]
        self.assertFalse(parked.get("tool_calls"))
        self.fixture.release("parked")
        for scheduled in [False, True]:
            turn = messages + [dict(role="user", content="This turn is a scheduled check.\n\nInspect the current operation." if scheduled else "Inspect the scheduled check result.")]
            with self.request(model, tools, turn) as response:
                wait = json.load(response)["choices"][0]["message"]["tool_calls"][0]
            turn.append(dict(role="tool", tool_call_id=wait["id"], content=json.dumps(dict(agents=[dict(agent_id="actual_worker", status="done")]))))
            with self.request(model, tools, turn) as response:
                collected = json.load(response)["choices"][0]["message"]
            self.assertEqual(bool(collected.get("tool_calls")), scheduled)
            if scheduled:
                self.assertEqual(collected["tool_calls"][0]["function"]["name"], "report_schedule")

    def test_multimodal_manager_reads_the_scheduled_wrapper_only_from_text_parts(self):
        model = self.fixture.models["parked"]
        tools = [{"function": {"name": "spawn_agent"}}]
        with self.request(model, tools) as response:
            spawn = json.load(response)["choices"][0]["message"]["tool_calls"][0]
        receipt = dict(role="tool", tool_call_id=spawn["id"], content='{"agent_id":"actual_worker"}')
        for scheduled in (False, True):
            content = [dict(type="image_url", image_url=dict(url="This turn is a scheduled check.")),
                       dict(type="text", text="This turn is a scheduled check.\n\nInspect the operation."
                            if scheduled else "Inspect the operation and its selected image.")]
            with self.request(model, tools, [receipt, dict(role="user", content=content)]) as response:
                message = json.load(response)["choices"][0]["message"]
            self.assertEqual(bool(message.get("tool_calls")), scheduled)
            if scheduled:
                self.assertEqual(message["tool_calls"][0]["function"]["name"], "report_schedule")
        state = self.fixture.status()["parked"]
        self.assertEqual(state["worker_id"], "actual_worker")
        self.assertEqual(state["spawn_calls"], 1)
        self.assertEqual(state["worker_requests"], 0)


if __name__ == "__main__":
    unittest.main()
