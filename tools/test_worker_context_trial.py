import argparse
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import worker_context_trial as trial


class TrialTests(unittest.TestCase):
    def fixture(self, root):
        inputs = root / "inputs"
        inputs.mkdir()
        (inputs / "case.json").write_text(json.dumps({"schema_version": 1,
            "instruction": "Use retained evidence.", "workers": [{"agent_id": "w", "generation": 1,
            "status": "done", "result": "retained"}], "rounds": [{"user": "Collect results."}]}))
        (inputs / "tasks.json").write_text(json.dumps({"cases": [{"id": "case", "fixture": "case.json",
            "source_alias": "source", "split": "heldout"}]}))
        (inputs / "expectations.json").write_text(json.dumps({"cases": {"case": {}}}))
        return inputs

    def test_order_has_exact_pairs_and_alternates_first_arm(self):
        order = trial.paired_order(["a", "b"], 3, 7)
        self.assertEqual(order, trial.paired_order(["a", "b"], 3, 7))
        self.assertEqual(len(order), 12)
        for case in ("a", "b"):
            rows = [r for r in order if r["case"] == case]
            self.assertEqual({(r["repeat"], r["arm"]) for r in rows},
                             {(n, arm) for n in (1, 2, 3) for arm in ("a", "b")})
            self.assertNotEqual(rows[0]["arm"], rows[2]["arm"])

    def test_model_fixture_cannot_contain_private_source_or_oracle(self):
        with tempfile.TemporaryDirectory() as d:
            inputs = self.fixture(Path(d))
            path = inputs / "case.json"
            body = json.loads(path.read_text())
            body["raw_source_private"] = "must not reach model"
            path.write_text(json.dumps(body))
            with self.assertRaisesRegex(ValueError, "non-model metadata"):
                trial.load_inputs(inputs)

    def test_fixture_cannot_escape_input_directory(self):
        with tempfile.TemporaryDirectory() as d:
            inputs = self.fixture(Path(d))
            tasks = json.loads((inputs / "tasks.json").read_text())
            tasks["cases"][0]["fixture"] = "../outside.json"
            (inputs / "tasks.json").write_text(json.dumps(tasks))
            with self.assertRaisesRegex(ValueError, "remain in input"):
                trial.load_inputs(inputs)

    def test_changed_inputs_and_provider_fail_before_execution(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "input"
            path.write_text("frozen")
            provider = {"model": "model", "base_url": "http://local", "api_key": "one"}
            manifest = {"provider_config_sha256": trial.provider_config_digest(provider),
                        "hashes": {str(path): trial.digest(path)}}
            trial.verify(manifest, {**provider, "api_key": "rotation"})
            with self.assertRaisesRegex(ValueError, "provider"):
                trial.verify(manifest, {**provider, "model": "other"})
            path.write_text("changed")
            with self.assertRaisesRegex(ValueError, "input changed"):
                trial.verify(manifest, provider)

    def test_execute_is_once_only_and_preserves_harness_failures(self):
        with tempfile.TemporaryDirectory() as d:
            out = Path(d)
            manifest = {"order": [{"case": "case", "arm": "a", "repeat": 1}]}
            result = {"harness_error": "first failure", "helper_exited": True, "private_config_removed": True}
            with patch.object(trial, "verify"), patch.object(trial, "run_one", return_value=result):
                report = trial.execute(manifest, {}, out)
                self.assertEqual(report["results"], [result])
                with self.assertRaisesRegex(ValueError, "already started"):
                    trial.execute(manifest, {}, out)
            self.assertEqual(json.loads((out / "report.json").read_text())["results"][0]["harness_error"], "first failure")

    def test_cleanup_refusal_stops_next_planned_call(self):
        with tempfile.TemporaryDirectory() as d:
            out = Path(d)
            row = {"case": "case", "arm": "a", "repeat": 1}
            result = {"harness_error": "", "helper_exited": False, "private_config_removed": False}
            with patch.object(trial, "verify"), patch.object(trial, "run_one", return_value=result) as run:
                report = trial.execute({"order": [row, row]}, {}, out)
            self.assertEqual(run.call_count, 1)
            self.assertIn("stopped_early", report)

    def test_cancellation_never_starts_another_model_session(self):
        with tempfile.TemporaryDirectory() as d:
            result = {"harness_error": "cancelled", "cancelled": True,
                      "helper_exited": True, "private_config_removed": True}
            row = {"case": "case", "arm": "a", "repeat": 1}
            with patch.object(trial, "verify"), patch.object(trial, "run_one", return_value=result) as run:
                report = trial.execute({"order": [row, row]}, {}, Path(d))
            self.assertEqual(run.call_count, 1)
            self.assertEqual(report["stopped_early"], "cancellation")

    def test_real_helper_process_has_private_config_and_is_reaped(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            inputs = self.fixture(root)
            helper = root / "helper.py"
            helper.write_text('''#!/usr/bin/env python3
import json,os,pathlib,sys
args=dict(zip(sys.argv[1::2],sys.argv[2::2]))
config=pathlib.Path(args['--config'])
out=pathlib.Path(args['--out']);out.mkdir()
assert config.stat().st_mode & 0o777 == 0o600
assert pathlib.Path(os.environ['HOME']).is_relative_to(config.parent)
assert not any(k.startswith('OPENAI_') for k in os.environ)
(out/'result.json').write_text(json.dumps({'config_path':str(config),'calls':[]}))
print('API-SECRET')
''')
            helper.chmod(0o700)
            provider = {"model": "model", "base_url": "http://local", "api_key": "API-SECRET"}
            manifest = {"helper": str(helper), "inputs": str(inputs), "conditions": {
                "max_completion_tokens": 0, "max_iterations": 2, "timeout_seconds": 1}}
            result = trial.run_one({"case": "case", "arm": "a", "repeat": 1}, manifest, provider, root / "out")
            self.assertEqual(result["harness_error"], "")
            self.assertTrue(result["helper_exited"])
            self.assertTrue(result["private_config_removed"])
            self.assertFalse(Path(result["result"]["config_path"]).exists())
            stdout = root / "out/runs/case/repeat-1/a/stdout.txt"
            self.assertNotIn("API-SECRET", stdout.read_text())
            self.assertEqual(stdout.stat().st_mode & 0o777, 0o600)


if __name__ == "__main__":
    unittest.main()
