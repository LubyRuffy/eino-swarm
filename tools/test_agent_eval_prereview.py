"""Offline checks of agent_eval_prereview: the evidence that a worker's review
fixed something. Grading only the final workspace hides it, which is how
three review fixtures first read as having nothing for a review to catch."""

import io
import json
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval_prereview as pre  # noqa: E402

CHECK = "import sys; sys.exit(0 if open('a.txt').read() == 'good' else 1)"


def call(seq, name, args, agent="manager", call_id=None):
    return {"seq": seq, "kind": "tool_call", "agent_id": agent, "tool_call_id": call_id or f"c{seq}",
            "text": f"{name}({json.dumps(args)})"}


def result(call_id, text):
    return {"kind": "tool_result", "tool_call_id": call_id, "text": text}


class ReplayTest(unittest.TestCase):
    def test_patch_hunks_apply_and_unknown_context_is_left_alone(self):
        patch = "*** Begin Patch\n*** Update File: a.py\n@@\n x = 1\n-y = 2\n+y = 3\n@@\n-absent\n+never\n*** End Patch"
        self.assertEqual(pre.apply_patch("x = 1\ny = 2\n", patch), "x = 1\ny = 3\n")

    def test_files_at_the_first_report_exclude_fixes_made_after_it(self):
        events = [
            call(1, "write", {"file_path": "a.txt", "content": "bad v1"}),
            call(2, "edit", {"file_path": "a.txt", "search_block": "v1", "replace_block": "v2"}),
            call(3, "spawn_agent", {"role": "reviewer", "task": "review a.txt"}),
            # The worker's own wait and a write it does are not the manager collecting a report.
            call(4, "wait_agents", {}, agent="reviewer-1"),
            call(5, "write", {"file_path": "probe.py", "content": "probe"}, agent="reviewer-1"),
            call(6, "wait_agents", {"ids": ["reviewer-1"]}),
            call(7, "write", {"file_path": "a.txt", "content": "good"}),
            call(8, "edit", {"file_path": "a.txt", "search_block": "good", "replace_block": "oops"}, call_id="bad"),
            result("bad", "error: search_block matched twice"),
        ]
        final, first_report, roles = pre.replay(events)
        self.assertEqual(first_report, {"a.txt": "bad v2", "probe.py": "probe"})
        self.assertEqual(final["a.txt"], "good")
        self.assertEqual(roles, ["reviewer"])

    def test_a_run_that_never_spawned_has_no_first_report(self):
        final, first_report, roles = pre.replay([call(1, "write", {"file_path": "a.txt", "content": "x"}),
                                                 call(2, "wait_agents", {})])
        self.assertIsNone(first_report)
        self.assertEqual((final, roles), ({"a.txt": "x"}, []))


class InspectRunTest(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.root)
        task = self.root / "tasks" / "t"
        task.mkdir(parents=True)
        (task / "prompt.md").write_text("do it")
        (task / "task.json").write_text(json.dumps({"id": "t", "checks": [
            {"name": "a.txt is good", "type": "command", "argv": [sys.executable, "-c", CHECK]},
            {"name": "a.txt kept", "type": "file_unchanged", "path": "a.txt"}]}))

    def run_dir(self, events, final_text):
        run = self.root / "out" / "r" / "runs" / "t" / "run-1"
        (run / "workspace").mkdir(parents=True)
        (run / "workspace" / "a.txt").write_text(final_text)
        (run / "result.json").write_text(json.dumps({"task": "t", "outcome": {"state": "done"}}))
        (run / "traces.json").write_text(json.dumps([{"events": events}]))
        return run

    def test_a_fix_after_the_report_shows_in_both_columns(self):
        run = self.run_dir([call(1, "write", {"file_path": "a.txt", "content": "bad"}),
                            call(2, "spawn_agent", {"role": "reviewer"}),
                            call(3, "wait_agents", {}),
                            call(4, "write", {"file_path": "a.txt", "content": "good"})], "good")
        row = pre.inspect_run(run, self.root / "tasks")
        self.assertEqual(row["first_report"], ["a.txt is good (exit 1:)"])
        self.assertEqual(row["final"], [])
        self.assertTrue(row["replay_exact"])
        # Only command checks grade files; file_unchanged needs a baseline the replay lacks.
        self.assertEqual(pre.run_dirs(self.root / "out"), [run])

    def test_a_file_changed_through_exec_marks_the_replay(self):
        run = self.run_dir([call(1, "write", {"file_path": "a.txt", "content": "bad"}),
                            call(2, "exec", {"command": "printf good > a.txt"})], "good")
        row = pre.inspect_run(run, self.root / "tasks")
        self.assertFalse(row["replay_exact"])
        self.assertIsNone(row["first_report"])

    def test_the_table_names_runs_without_workers(self):
        run = self.run_dir([call(1, "write", {"file_path": "a.txt", "content": "bad"})], "bad")
        out = io.StringIO()
        tasks = pre.agent_eval.TASKS_DIR
        pre.agent_eval.TASKS_DIR = self.root / "tasks"
        try:
            with redirect_stdout(out):
                self.assertEqual(pre.main([str(run)]), 0)
        finally:
            pre.agent_eval.TASKS_DIR = tasks
        self.assertIn("| t/run-1 | done | - | no worker | a.txt is good (exit 1:) | exact |", out.getvalue())
        self.assertEqual(pre.main([]), 2)


if __name__ == "__main__":
    unittest.main()
