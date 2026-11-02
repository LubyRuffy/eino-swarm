"""Pair identity prevents fast failures or missing usage looking like gains."""

import copy
import sys
import unittest
import json
import tempfile
from collections import defaultdict
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import agent_eval_pair as pair


def result(arm, pair_id="pair-1", passed=True, seconds=5, tokens=100, fixture="same", error=""):
    return {"task": "task", "run": 1, "passed": passed, "harness_error": error, "task_s": seconds,
            "review_settled": True,
            "run_identity": {"arm": arm, "pair_id": pair_id}, "task_sha256": "taskhash",
            "fixture_sha256": fixture, "budget": {"timeout_s": 20},
            "metrics": {"usage_facts": {"total_tokens": tokens}}}


class ScheduleTest(unittest.TestCase):
    def test_project_instruction_rejects_nonstring_values(self):
        base = {"version": 1, "runs": 1, "arms": [{"id": "a", "project_mode": "default"}, {"id": "b", "project_mode": "nuwa"}]}
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "protocol.json"
            for value in (None, False, 0, [], {}):
                spec = copy.deepcopy(base)
                spec["arms"][1]["project_instruction"] = value
                path.write_text(json.dumps(spec))
                with self.subTest(value=value), self.assertRaisesRegex(ValueError, "project_instruction.*string"):
                    pair.load_spec(path)
            for value in ("", "Use independent evidence checks."):
                spec = copy.deepcopy(base)
                spec["arms"][1]["project_instruction"] = value
                path.write_text(json.dumps(spec))
                self.assertEqual(pair.load_spec(path), spec)
            path.write_text(json.dumps(base))
            self.assertEqual(pair.load_spec(path), base)

    def test_paired_runs_keep_arm_instruction_separate_from_common_task(self):
        spec = {"version": 1, "runs": 1, "tasks": ["task"], "arms": [
            {"id": "a", "project_mode": "default", "project_instruction": "Use independent evidence checks."},
            {"id": "b", "project_mode": "nuwa", "project_instruction": "Keep a compact audit trail."},
            {"id": "c", "project_mode": "nuwa"}]}
        observed = {}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            binary = root / "candidate"
            binary.write_bytes(b"offline candidate fixture")
            path = root / "protocol.json"
            path.write_text(json.dumps(spec))
            task = {"id": "task", "dir": root / "task", "prompt": "Evaluate the supplied evidence.",
                    "project_prompt": "Use the provided sources.", "timeout_s": 20}
            task["dir"].mkdir()
            (task["dir"] / "input.txt").write_text("Source material.\n")
            original = copy.deepcopy(task)

            def run_once(current, replicate, args, provider, out):
                arm = args.run_identity["arm"]
                observed[arm] = getattr(args, "project_instruction", None)
                self.assertEqual(current, original)
                row = result(arm, args.run_identity["pair_id"])
                row["task_sha256"] = pair.protocol.task_fingerprint(current)
                return row

            with patch.object(pair.runner, "load_tasks", return_value=[task]), \
                    patch.object(pair.runner, "run_once", side_effect=run_once), \
                    patch.object(pair.runner, "unwind_on_termination"):
                self.assertEqual(pair.main(["--protocol", str(path), "--zwai", str(binary),
                                            "--out", str(root / "evidence"), "--mock"]), 0)
            frozen = json.loads(next((root / "evidence").glob("*/protocol.json")).read_text())
            report = json.loads(next((root / "evidence").glob("*/report.json")).read_text())
            self.assertEqual(frozen["protocol"], spec)
            unhashed = {k: v for k, v in frozen.items() if k not in ("protocol_sha256", "schedule")}
            self.assertEqual(frozen["protocol_sha256"], pair.protocol.digest(unhashed))
            changed = copy.deepcopy(unhashed)
            changed["protocol"]["arms"][0]["project_instruction"] += " Confirm uncertainty."
            self.assertNotEqual(frozen["protocol_sha256"], pair.protocol.digest(changed))
            self.assertTrue(all(row["valid_pairs"] == 1 for row in report["summary"]["comparisons"].values()))
            self.assertEqual(task, original)
        self.assertEqual(observed, {"a": spec["arms"][0]["project_instruction"],
                                    "b": spec["arms"][1]["project_instruction"], "c": ""})

    def test_unrecognized_budget_or_external_score_claim_is_refused(self):
        base = {"version": 1, "runs": 1, "arms": [{"id": "a", "project_mode": "default"}, {"id": "b", "project_mode": "nuwa"}]}
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "protocol.json"
            for change in ({"budget": {"total_tokens": 100}}, {"benchmark": "unverified-external-benchmark"}):
                path.write_text(json.dumps({**base, **change}))
                with self.assertRaises(ValueError):
                    pair.load_spec(path)

    def test_every_repeat_is_interleaved_reproducible_and_balanced(self):
        first = pair.plan_schedule(["x", "y"], ["a", "b", "c"], 3, 17, "protocol")
        self.assertEqual(first, pair.plan_schedule(["x", "y"], ["a", "b", "c"], 3, 17, "protocol"))
        self.assertNotEqual(first, pair.plan_schedule(["x", "y"], ["a", "b", "c"], 3, 18, "protocol"))
        groups = defaultdict(list)
        for row in first:
            groups[(row["task"], row["replicate"])].append(row)
        for group in groups.values():
            self.assertEqual({r["arm"] for r in group}, {"a", "b", "c"})
            self.assertEqual(len({r["pair_id"] for r in group}), 1)
        for task in ("x", "y"):
            self.assertEqual({groups[(task, repeat)][0]["arm"] for repeat in (1, 2, 3)}, {"a", "b", "c"})


class FairSummaryTest(unittest.TestCase):
    def test_unsettled_or_cut_off_auxiliary_work_cannot_prove_token_savings(self):
        for flags in ({"review_settled": False}, {"review_cut_off": True}, {"review_settled": None}):
            with self.subTest(flags=flags):
                candidate = result("b", tokens=1)
                candidate.update(flags)
                observed = pair.paired_summary([result("a"), candidate], ["a", "b"])["comparisons"]["b"]
                self.assertEqual(observed["token_comparison"], "unavailable")
                self.assertIsNone(observed["token_delta_median"])

    def test_mock_usage_cannot_be_exported_as_a_real_performance_difference(self):
        summary = pair.paired_summary([result("a"), result("b", seconds=1, tokens=1)], ["a", "b"], "offline_mock")
        observed = summary["comparisons"]["b"]
        self.assertEqual(observed["token_comparison"], "synthetic_mock_only")
        self.assertIsNone(observed["token_delta_median"])
        self.assertIsNone(observed["successful_time_delta_median_s"])

    def test_unknown_tokens_prohibit_a_token_saving_conclusion(self):
        summary = pair.paired_summary([result("a", tokens=None), result("b", tokens=20)], ["a", "b"])
        comparison = summary["comparisons"]["b"]
        self.assertEqual(comparison["token_pairs"], 0)
        self.assertIsNone(comparison["token_delta_median"])
        self.assertEqual(comparison["token_comparison"], "unavailable")

    def test_fast_failed_attempt_does_not_count_as_successful_speedup(self):
        summary = pair.paired_summary([result("a"), result("b", passed=False, seconds=0.01)], ["a", "b"])
        comparison = summary["comparisons"]["b"]
        self.assertEqual(comparison["success_delta"], -1)
        self.assertEqual(comparison["both_passed"], 0)
        self.assertIsNone(comparison["successful_time_delta_median_s"])

    def test_different_fixtures_and_budgets_are_not_a_pair(self):
        for changed in ({"fixture_sha256": "changed"}, {"budget": {"timeout_s": 30}}, {"task_sha256": "changed"}):
            b = result("b")
            b.update(changed)
            comparison = pair.paired_summary([result("a"), b], ["a", "b"])["comparisons"]["b"]
            self.assertEqual(comparison["valid_pairs"], 0)
            self.assertEqual(len(comparison["invalid_pairs"]), 1)

    def test_failures_and_infrastructure_errors_are_retained(self):
        rows = [result("a"), result("b", error="unreachable"), result("a", "p2"), result("b", "p2", False)]
        summary = pair.paired_summary(rows, ["a", "b"])
        self.assertEqual(summary["arms"]["b"]["harness_errors"], 1)
        self.assertEqual(summary["comparisons"]["b"]["infrastructure_excluded_pairs"], ["pair-1"])
        self.assertEqual(summary["comparisons"]["b"]["success_delta"], -1)

    def test_duplicate_run_identity_is_refused(self):
        row = result("a")
        with self.assertRaises(ValueError):
            pair.paired_summary([row, copy.deepcopy(row), result("b")], ["a", "b"])

    def test_improvements_and_regressions_are_reported_with_signed_deltas(self):
        summary = pair.paired_summary([result("a", seconds=5, tokens=100), result("b", seconds=7, tokens=80)], ["a", "b"])
        comparison = summary["comparisons"]["b"]
        self.assertEqual(comparison["successful_time_delta_median_s"], 2)
        self.assertEqual(comparison["token_delta_median"], -20)
        self.assertEqual(comparison["token_comparison"], "complete")


if __name__ == "__main__":
    unittest.main()
