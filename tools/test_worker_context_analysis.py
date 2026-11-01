import copy
import hashlib
from pathlib import Path
import tempfile
import unittest
import worker_context_analysis as analysis


class AnalysisTests(unittest.TestCase):
    def inputs(self):
        rows = []
        reviews = {}
        for i in range(3):
            for repeat in (1, 2):
                for arm, cost in (("a", 100), ("b", 60)):
                    row = {"case": f"case-{i}", "source_alias": f"source-{i}", "repeat": repeat,
                        "arm": arm, "split": "heldout", "wall_s": 1, "harness_error": "",
                        "helper_exited": True, "private_config_removed": True,
                        "result": {"cost_complete": True, "rounds": [{"error": ""}, {"error": ""}],
                        "cleanup": {"registry_closed": True, "live_workers": 0}, "calls": [{"total_tokens": cost}]}}
                    rows.append(row)
                    reviews["/".join(map(str, analysis.identity(row)))] = {"passed": True, "critical_errors": 0}
        sources = {f"case-{i}": {"source_alias": f"source-{i}", "split": "heldout", "rounds": 2} for i in range(3)}
        return {"order": rows, "case_sources": sources}, {"results": rows}, reviews

    def test_repetitions_are_nested_not_independent_sources(self):
        args = self.inputs()
        result = analysis.analyze(*args)
        self.assertTrue(result["screen_passed"])
        self.assertEqual(result["heldout_effect"]["sources"], 3)
        self.assertAlmostEqual(result["heldout_effect"]["reduction"], .4)

    def test_duplicate_or_missing_session_never_passes(self):
        manifest, report, reviews = self.inputs()
        report = copy.deepcopy(report)
        report["results"][0] = report["results"][1]
        result = analysis.analyze(manifest, report, reviews)
        self.assertFalse(result["coverage_complete"])
        self.assertFalse(result["screen_passed"])

    def test_missing_cost_never_becomes_zero_savings(self):
        manifest, report, reviews = self.inputs()
        report = copy.deepcopy(report)
        report["results"][1]["result"]["calls"][0]["total_tokens"] = 0
        result = analysis.analyze(manifest, report, reviews)
        self.assertEqual(result["arms"]["b"]["cost_incomplete"], 1)
        self.assertFalse(result["screen_passed"])

    def test_review_missing_and_critical_miss_each_block_acceptance(self):
        manifest, report, reviews = self.inputs()
        key = next(k for k in reviews if k.endswith("/b"))
        self.assertFalse(analysis.analyze(manifest, report, {k: v for k, v in reviews.items() if k != key})["screen_passed"])
        reviews[key]["critical_errors"] = 1
        self.assertFalse(analysis.analyze(manifest, report, reviews)["screen_passed"])

    def test_same_noncritical_failure_in_both_arms_cannot_pass_quality_gate(self):
        manifest, report, reviews = self.inputs()
        for value in reviews.values():
            value["passed"] = False
        self.assertFalse(analysis.analyze(manifest, report, reviews)["screen_passed"])

    def test_zero_denominator_has_no_interval(self):
        self.assertIsNone(analysis.bootstrap([(0, 1), (2, 1)])["interval_95"])

    def test_analysis_refuses_changed_frozen_instrument(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "instrument"
            path.write_text("frozen")
            manifest = {"hashes": {str(path): hashlib.sha256(path.read_bytes()).hexdigest()}}
            analysis.verify_inputs(manifest)
            path.write_text("changed")
            with self.assertRaisesRegex(ValueError, "changed before analysis"):
                analysis.verify_inputs(manifest)

    def test_source_group_cannot_be_changed_after_freeze(self):
        manifest, report, reviews = self.inputs()
        report = copy.deepcopy(report)
        report["results"][0]["source_alias"] = "another-source"
        self.assertFalse(analysis.analyze(manifest, report, reviews)["screen_passed"])

    def test_helper_cost_incomplete_and_live_worker_each_refuse_screen(self):
        for field, value in (("cost_complete", False), ("live_workers", 1), ("rounds", [])):
            manifest, report, reviews = self.inputs()
            report = copy.deepcopy(report)
            result = report["results"][1]["result"]
            if field == "live_workers":
                result["cleanup"][field] = value
            else:
                result[field] = value
            with self.subTest(field=field):
                self.assertFalse(analysis.analyze(manifest, report, reviews)["screen_passed"])


if __name__ == "__main__":
    unittest.main()
