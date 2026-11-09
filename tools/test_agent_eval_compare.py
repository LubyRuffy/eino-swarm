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

    def test_gate_fails_a_single_task_drop_even_when_the_totals_match(self):
        # The same total can hide one task that broke while another started passing.
        write_report(self.dir / "a", "m1", [result("x", 1, True), result("y", 1, False)])
        write_report(self.dir / "b", "m2", [result("x", 1, False), result("y", 1, True)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = cmp.main(["--gate", "--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        text = out.getvalue()
        self.assertEqual(code, 2)
        self.assertIn("b x pass rate 0/1 is below a 1/1", text)
        self.assertNotIn("improved", text.lower())

    def test_without_gate_a_pass_rate_drop_still_exits_zero(self):
        write_report(self.dir / "a", "m1", [result("x", 1, True)])
        write_report(self.dir / "b", "m2", [result("x", 1, False)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = cmp.main(["--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        self.assertEqual(code, 0)
        self.assertNotIn("## Regression gate", out.getvalue())

    def test_gate_treats_a_new_harness_error_as_adoption_failure_not_a_graded_miss(self):
        write_report(self.dir / "a", "m1", [result("x", 1, True)])
        broken = result("x", 1, False, harness_error="boot")
        write_report(self.dir / "b", "m2", [broken])
        graded = cmp.arm_stats(cmp.load_arm(self.dir / "b")[0])
        self.assertEqual(graded["failures"], [])
        self.assertEqual((graded["passed"], graded["runs"]), (0, 0))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = cmp.main(["--gate", "--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        self.assertEqual(code, 2)
        text = out.getvalue()
        self.assertIn("b x has a harness error after a graded it", text)
        self.assertIn("| x | 1/1 | 0/0 |", text)

    def test_gate_fails_when_the_candidate_omits_a_reference_task(self):
        write_report(self.dir / "a", "m1", [result("x", 1, True), result("y", 1, True)])
        write_report(self.dir / "b", "m2", [result("x", 1, True)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = cmp.main(["--gate", "--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        self.assertEqual(code, 2)
        self.assertIn("b is missing y", out.getvalue())

    def test_missing_usage_is_incomparable_rather_than_zero(self):
        row = result("x", 1, True)
        row["metrics"]["usage_facts"] = {"total_tokens": None}
        write_report(self.dir / "a", "m1", [row])
        write_report(self.dir / "b", "m2", [result("x", 1, True)])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            cmp.main(["--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'b'}"])
        text = out.getvalue()
        self.assertIn("| a | 1/1 | 0 | incomparable | incomparable |", text)
        self.assertNotIn("| a | 1/1 | 0 | 0K |", text)
        self.assertIn("| x | 1/1 · incomparable · 2s | 1/1 · 1K · 2s |", text)

    def test_hidden_rule_failure_records_self_attested_or_unknown(self):
        # The visible test is the workspace file the task keeps unchanged.
        # A successful run of it does not satisfy the hidden rule.
        failed = {
            "name": "money meets every README rule", "ok": False,
        }
        attested = result("spec-edges", 1, False)
        attested["checks"] = [failed]
        unknown = result("spec-edges", 2, False)
        unknown["checks"] = [failed]
        rejected = result("spec-edges", 3, False)
        rejected["checks"] = [failed]
        write_report(self.dir / "a", "m1", [attested, unknown, rejected])
        runs = self.dir / "a" / "runs" / "spec-edges"
        (runs / "run-1").mkdir(parents=True)
        (runs / "run-1" / "traces.json").write_text(json.dumps([{
            "events": [
                {"kind": "tool_call", "tool_call_id": "visible",
                 "text": 'exec({"command": "python3 money/test_basic.py"})'},
                {"kind": "tool_result", "tool_call_id": "visible",
                 "text": '{"exit_code": 0, "failed": false}'},
            ],
        }]))
        (runs / "run-3").mkdir(parents=True)
        (runs / "run-3" / "traces.json").write_text(json.dumps([{
            "events": [
                {"kind": "tool_call", "tool_call_id": "visible",
                 "text": 'exec({"command": "python3 money/test_basic.py"})'},
                {"kind": "tool_result", "tool_call_id": "visible",
                 "text": '{"exit_code": 1, "failed": true}'},
            ],
        }]))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = cmp.main(["--arm", f"a={self.dir / 'a'}", "--arm", f"b={self.dir / 'a'}"])
        self.assertEqual(code, 0)
        self.assertIn("a spec-edges: self_attested 1, unknown 1", out.getvalue())


if __name__ == "__main__":
    unittest.main()
