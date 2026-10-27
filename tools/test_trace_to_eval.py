"""Offline tests for tools/trace_to_eval.py, on synthetic traces only."""

import contextlib
import io
import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402
import trace_to_eval as t2e  # noqa: E402


def ev(seq, kind, second, text="", agent="manager", call=""):
    e = {"seq": str(seq), "kind": kind, "agent_id": agent, "text": text,
         "created_at": f"2026-01-01T00:00:{second:02d}Z"}
    if call:
        e["tool_call_id"] = call
    return e


def turn(seq, tid, events, status="done", duration=5000, calls=()):
    return {"turn": {"id": tid, "seq": seq, "status": status, "duration_ms": duration, "model": "m"},
            "events": events, "llm_calls": list(calls)}


ANSWER = json.dumps({"answers": {"scope": {"answers": ["only the first folder"]}}})

CONVERSATION = [
    turn(1, "tn_1", [
        ev(1, "user_message", 0, "summarise the folders"),
        ev(2, "tool_call", 2, 'ask_user({"questions":[]})', call="c1"),
        ev(3, "tool_result", 9, ANSWER, call="c1"),
        ev(4, "steer", 12, "keep it short"),
        ev(5, "tool_call", 13, "read(a.md)", call="c2"),
        ev(6, "tool_result", 14, "text", call="c2"),
        ev(7, "spawned", 15, "", agent="reader"),
        ev(8, "tool_call", 16, 'ask_user({"questions":[]})', agent="reader", call="c3"),
        ev(9, "tool_result", 17, ANSWER, agent="reader", call="c3"),
    ], calls=[{"total_tokens": 100}, {"total_tokens": 50}]),
    # A goal continuation: no human message, so nothing to replay.
    turn(2, "tn_2", [ev(1, "goal_continued", 30, "continuing")]),
    turn(3, "tn_3", [ev(1, "user_message", 40, "now as a table")], duration=3000, calls=[{"total_tokens": 10}]),
]


class TraceToEvalTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.dir)

    def test_human_inputs_keep_their_order_kind_and_offset(self):
        prompt, replay, _ = t2e.human_inputs(CONVERSATION)
        self.assertEqual(prompt, "summarise the folders")
        self.assertEqual(replay, [
            {"kind": "answer", "text": "only the first folder", "after_s": 9.0},
            {"kind": "steer", "text": "keep it short", "after_s": 12.0},
            {"kind": "turn", "text": "now as a table", "after_s": 40.0},
        ])

    def test_runtime_started_turns_and_worker_tools_are_not_human_input(self):
        _, replay, _ = t2e.human_inputs(CONVERSATION)
        self.assertEqual(sum(1 for r in replay if r["kind"] == "answer"), 1)
        self.assertNotIn("continuing", json.dumps(replay))

    def test_written_task_loads_and_replays_through_the_harness(self):
        trace_file = self.dir / "traces.json"
        trace_file.write_text(json.dumps(CONVERSATION))
        fixture = self.dir / "ws"
        (fixture / "sub").mkdir(parents=True)
        (fixture / "sub" / "a.md").write_text("alpha")
        (fixture / ".git").mkdir()
        (fixture / ".git" / "HEAD").write_text("ref")
        root = self.dir / "replays"
        with contextlib.redirect_stdout(io.StringIO()):
            t2e.main(["--trace", str(trace_file), "--id", "folders", "--out", str(root),
                      "--workspace", str(fixture), "--redact", "first folder"])
        task = grade.load_task(root / "folders")
        self.assertEqual(task["prompt"], "summarise the folders")
        self.assertEqual([r["kind"] for r in task["replay"]], ["answer", "steer", "turn"])
        self.assertEqual(task["replay"][0]["text"], "only the <redacted>")
        self.assertEqual(task["timeout_s"], 600)
        self.assertTrue((root / "folders" / "fixture" / "sub" / "a.md").is_file())
        self.assertFalse((root / "folders" / "fixture" / ".git").exists())
        recorded = json.loads((root / "folders" / "recorded.json").read_text())
        self.assertEqual((recorded["total_tokens"], recorded["spawned"], recorded["tool_calls"]["ask_user"]), (160, 1, 2))
        self.assertEqual([t["id"] for t in agent_eval.load_tasks(["folders"], root)], ["folders"])

    def test_existing_task_and_bad_input_fail_instead_of_overwriting(self):
        trace_file = self.dir / "one.json"
        trace_file.write_text(json.dumps(CONVERSATION[0]))
        root = self.dir / "replays"
        with contextlib.redirect_stdout(io.StringIO()):
            t2e.main(["--trace", str(trace_file), "--id", "one", "--out", str(root)])
        with self.assertRaises(SystemExit):
            t2e.main(["--trace", str(trace_file), "--id", "one", "--out", str(root)])
        with self.assertRaises(SystemExit):
            t2e.main(["--trace", str(trace_file), "--id", "Bad Id", "--out", str(root)])
        (self.dir / "bad.json").write_text(json.dumps({"events": []}))
        with self.assertRaises(SystemExit):
            t2e.load_traces(self.dir / "bad.json")
        with self.assertRaises(SystemExit):
            t2e.human_inputs([turn(1, "tn_x", [ev(1, "goal_continued", 0, "x")])])

    def test_unreadable_answer_payload_is_skipped(self):
        self.assertEqual(t2e._answer_text("not json"), "")
        self.assertEqual(t2e._answer_text(json.dumps({"answers": {"a": {"answers": [" "]}}})), "")


if __name__ == "__main__":
    unittest.main()
