"""Offline tests for tools/agent_eval_compare.py."""

import contextlib
import io
import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval_compare as cmp  # noqa: E402


def result(task, run, passed, tokens=1000, ms=2000, harness_error="", spawn=0, state="done", first=(100, 50)):
    return {
        "task": task, "run": run, "passed": passed, "harness_error": harness_error,
        "outcome": {"state": state},
        "checks": [{"name": "answer is right", "ok": passed}],
        "metrics": {"total_tokens": tokens, "duration_ms": ms, "manager_calls": 3, "spawn_calls": spawn,
                    "manager_prompt_chars": 9000,
                    "manager_first_call": {"prompt_tokens": first[0], "cached_tokens": first[1]}},
    }


def write_report(root: Path, model: str, results: list[dict]) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / "report.json").write_text(json.dumps(
        {"meta": {"provider": {"model": model}, "zwai": "/bin/zwai"}, "results": results}))


class CompareTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.dir)

    def test_a_task_that_broke_is_listed_even_when_the_totals_match(self):
        # One task breaking while another starts passing leaves the total
        # unchanged; the comparison exists to show exactly that trade.
        write_report(self.dir / "a" / "t1", "m1", [result("x", 1, True), result("y", 1, False)])
        write_report(self.dir / "b" / "t1", "m2", [result("x", 1, False), result("y", 1, True)])
        arms = {"a": cmp.arm_stats(cmp.load_arm(self.dir / "a")[0]),
                "b": cmp.arm_stats(cmp.load_arm(self.dir / "b")[0])}
        self.assertEqual(arms["a"]["passed"], arms["b"]["passed"])
        self.assertEqual([f["task"] for f in cmp.flips(arms)], ["x", "y"])

    def test_harness_errors_are_not_graded_as_failures(self):
        stats = cmp.arm_stats([result("x", 1, True), result("x", 2, False, harness_error="boot")])
        self.assertEqual((stats["passed"], stats["runs"], stats["harness_errors"]), (1, 1, 1))
        self.assertEqual(stats["failures"], [])

    def test_reports_under_one_root_are_merged(self):
        write_report(self.dir / "a" / "part1" / "ts", "m1", [result("x", 1, True)])
        write_report(self.dir / "a" / "part2" / "ts", "m1", [result("y", 1, True)])
        results, metas = cmp.load_arm(self.dir / "a")
        self.assertEqual(sorted(r["task"] for r in results), ["x", "y"])
        self.assertEqual(len(metas), 2)

    def test_a_task_output_named_report_json_is_not_a_run(self):
        write_report(self.dir / "a" / "ts", "m1", [result("x", 1, True)])
        copy = self.dir / "a" / "ts" / "runs" / "x" / "run-1" / "workspace"
        copy.mkdir(parents=True)
        (copy / "report.json").write_text(json.dumps({"meta": {}, "results": [result("x", 9, False)]}))
        (self.dir / "a" / "ts" / "report-like").mkdir()
        (self.dir / "a" / "ts" / "report-like" / "report.json").write_text(json.dumps({"total": 3}))
        results, metas = cmp.load_arm(self.dir / "a")
        self.assertEqual([r["run"] for r in results], [1])
        self.assertEqual(len(metas), 1)

    def test_rendered_report_names_models_medians_and_failed_runs(self):
        write_report(self.dir / "a", "model-one", [result("x", 1, True, tokens=1000), result("x", 2, True, tokens=3000)])
        write_report(self.dir / "b", "model-two", [result("x", 1, False, state="turn_error"), result("x", 2, True)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            cmp.main(["--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        text = out.getvalue()
        self.assertIn("model-one", text)
        self.assertIn("model-two", text)
        self.assertIn("| a | 2/2 | 0 | 2K |", text)
        self.assertIn("- x: a 2/2, b 1/2", text)
        self.assertIn("b · x run 1 · turn_error · answer is right", text)
        self.assertIn("| a | 2/2 | 0 | 2K | 4K | 2 | 9000 | 50% |", text)

    def test_a_repeated_label_adds_runs_from_another_process(self):
        write_report(self.dir / "p1", "m1", [result("x", 1, True)])
        write_report(self.dir / "p2", "m1", [result("y", 1, False)])
        write_report(self.dir / "b", "m2", [result("x", 1, True), result("y", 1, True)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            cmp.main(["--arm", f"a={self.dir / 'p1'}", "--arm", f"b={self.dir / 'b'}", "--arm", f"a={self.dir / 'p2'}"])
        text = out.getvalue()
        self.assertIn("| a | 1/2 |", text)
        self.assertLess(text.index("| a |"), text.index("| b |"), "the first label stays the reference")

    def test_bad_arm_specs_fail_instead_of_guessing(self):
        with self.assertRaises(SystemExit):
            cmp.parse_arms(["nolabel"])
        with self.assertRaises(SystemExit):
            cmp.parse_arms(["a=/x", "a=/x/"])
        with self.assertRaises(SystemExit):
            cmp.load_arm(self.dir)


if __name__ == "__main__":
    unittest.main()
