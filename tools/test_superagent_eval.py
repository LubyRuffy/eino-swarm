"""Offline integrity tests for the constructed-observation decision trial."""

import copy
import contextlib
import io
import json
import os
import stat
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import superagent_eval as evaluation


class SuperagentEvalTest(unittest.TestCase):
    def fixture(self, root):
        raw = {"schema_version": 1, "cases": [{
            "id": "progress", "family": "watch", "instruction": "Check progress; quiet if stable.",
            "observations": {"provenance": "constructed_observation_from_real_task",
                             "evidence": [{"id": "p1", "status": "running"}]}}]}
        oracle = {"schema_version": 1, "cases": {"progress": {
            "decision": "quiet", "action": "continue", "required_evidence_ids": ["p1"],
            "notification_nonempty": False, "semantic_rubric": ["secret expected wording"]}}}
        root.mkdir(parents=True, exist_ok=True)
        for name, data in (("cases.json", raw), ("expectations.json", oracle)):
            (root / name).write_text(json.dumps(data))
        return raw, oracle

    def test_load_refuses_invalid_oracle_and_duplicate_or_missing_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            raw, oracle = self.fixture(root)
            for mutation in ("duplicate", "foreign", "bad-decision", "bad-provenance"):
                a, b = copy.deepcopy(raw), copy.deepcopy(oracle)
                if mutation == "duplicate":
                    a["cases"][0]["observations"]["evidence"] *= 2
                elif mutation == "foreign":
                    b["cases"]["progress"]["required_evidence_ids"] = ["not-real"]
                elif mutation == "bad-decision":
                    b["cases"]["progress"]["decision"] = "success"
                else:
                    a["cases"][0]["observations"]["provenance"] = "production_capture"
                (root / "cases.json").write_text(json.dumps(a))
                (root / "expectations.json").write_text(json.dumps(b))
                with self.subTest(mutation=mutation), self.assertRaises(evaluation.harness.EvalError):
                    evaluation.load_fixture(root)

    def test_arms_share_input_and_output_contract_without_expected_answer(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            raw, _ = self.fixture(root / "source")
            tasks = evaluation.build_tasks(raw["cases"], root / "tasks", timeout=60)
            a, b = tasks["a"]["progress"], tasks["b"]["progress"]
            self.assertEqual(a["prompt"] + "\n\n" + evaluation.TRIAGE, b["prompt"])
            self.assertIn("quiet if stable", a["prompt"])
            for task in (a, b):
                self.assertEqual(json.loads((task["dir"] / "fixture" / "evidence.json").read_text()),
                                 raw["cases"][0]["observations"])
                self.assertNotIn("secret expected wording", task["prompt"])
                self.assertNotIn("required_evidence_ids", task["prompt"])
                self.assertNotIn("expectations.json", task["prompt"])
            for path in (root / "tasks").rglob("*"):
                if path.is_file():
                    self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, path)

    def test_seeded_order_pairs_every_case_and_alternates_first_arm(self):
        order = evaluation.execution_order(["a-case", "b-case"], 4, 42)
        self.assertEqual(order, evaluation.execution_order(["a-case", "b-case"], 4, 42))
        self.assertEqual(len(order), 16)
        for case in ("a-case", "b-case"):
            rows = [r for r in order if r["case"] == case]
            for pair in range(4):
                self.assertEqual({r["arm"] for r in rows[pair * 2:pair * 2 + 2]}, {"a", "b"})
            self.assertNotEqual(rows[0]["arm"], rows[2]["arm"])

    def test_quality_separates_decision_action_evidence_and_notification(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            raw, oracle = self.fixture(root / "fixture")
            path = root / "decision.json"
            decision = {"decision": "quiet", "action": "continue", "evidence_ids": ["p1"], "notification": ""}
            path.write_text(json.dumps(decision))
            q = evaluation.grade_decision(path, raw["cases"][0], oracle["cases"]["progress"])
            self.assertTrue(q["automatic_passed"])
            self.assertEqual(q["semantic_status"], "unreviewed")
            self.assertNotIn("passed", q)
            for key, value in (("decision", "notify"), ("action", "complete"),
                               ("evidence_ids", ["fake"]), ("notification", "noise")):
                bad = {**decision, key: value}
                path.write_text(json.dumps(bad))
                with self.subTest(key=key):
                    q = evaluation.grade_decision(path, raw["cases"][0], oracle["cases"]["progress"])
                    self.assertFalse(q["automatic_passed"])
            path.unlink()
            self.assertFalse(evaluation.grade_decision(path, raw["cases"][0], oracle["cases"]["progress"])["automatic_passed"])

    def test_private_evidence_does_not_change_subprocess_umask(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            old = os.umask(0o022)
            try:
                evaluation.private_write(root / "evidence", "private")
                (root / "ordinary-build").write_text("ordinary")
                self.assertEqual(stat.S_IMODE((root / "evidence").stat().st_mode), 0o600)
                self.assertEqual(stat.S_IMODE((root / "ordinary-build").stat().st_mode), 0o644)
            finally:
                os.umask(old)

    def test_cache_counts_preserve_trace_field_missing_zero_and_positive(self):
        metrics = evaluation.cache_observations([{"llm_calls": [
            {}, {"cached_tokens": 0}, {"cached_tokens": 8, "prompt_tokens": 10}]}])
        self.assertEqual(metrics["field_missing"], 1)
        self.assertEqual(metrics["zero"], 1)
        self.assertEqual(metrics["positive"], 1)

    def test_real_run_once_passes_private_environment_to_owned_child_and_cleans_it(self):
        # This is a real subprocess and HTTP engine adapter, without any model.
        # A helper-only env assertion would miss a wrapper bypassing PrivateEngine.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            raw, oracle = self.fixture(root / "fixture")
            tasks = evaluation.build_tasks(raw["cases"], root / "tasks", 20)
            binary = root / "fake-engine"
            binary.write_text(FAKE_ENGINE.replace("PYTHON", sys.executable))
            binary.chmod(0o700)
            args = types.SimpleNamespace(zwai=str(binary), mock=True, boot_timeout=10,
                                         review_wait=0, max_completion_tokens=0)
            with patch.dict(os.environ, {"OPENAI_API_KEY": "do-not-inherit", "ZDOTDIR": "/real-shell"}):
                rec = evaluation.run_trial(tasks["a"]["progress"], raw["cases"][0],
                    oracle["cases"]["progress"], "a", 1, args, None, root / "out")
            self.assertTrue(rec["automatic_passed"], rec)
            self.assertTrue(rec["fixture_read"]["verified"])
            self.assertTrue(rec["fixture_read"]["decision_written_after_read"])
            self.assertTrue(rec["cost_complete"], rec["cost_incomplete_reasons"])
            self.assertEqual(rec["final_call_snapshot"]["pre_stop_calls"], 1)
            self.assertEqual(rec["final_call_snapshot"]["post_stop_calls"], 2)
            self.assertEqual(rec["metrics"]["total_tokens"], 30)
            self.assertEqual(rec["metrics"]["calls"]["title-namer"]["total_tokens"], 20)
            self.assertTrue(rec["cleanup"]["private_data_removed"])
            self.assertTrue(rec["cleanup"]["engine_exited"])
            evidence = root / "out" / "a" / "runs" / "progress" / "run-1"
            env = json.loads((evidence / "workspace" / "env.json").read_text())
            self.assertNotIn("OPENAI_API_KEY", env)
            for key in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "ZDOTDIR"):
                self.assertTrue(Path(env[key]).is_relative_to(Path(env["ZWAI_HOME"])), key)
            self.assertFalse(Path(env["ZWAI_HOME"]).exists())
            for path in (root / "out").rglob("*"):
                if path.is_file():
                    self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, path)

    def test_actual_read_requires_matching_agent_call_full_bytes_and_order(self):
        with tempfile.TemporaryDirectory() as tmp:
            raw, _ = self.fixture(Path(tmp))
            observations = raw["cases"][0]["observations"]
            text = evaluation.encoded(observations)
            receipt = "encoding=utf-8 path=evidence.json offset=1 limit=2000\n" + "".join(
                f"{i}|{line}\n" for i, line in enumerate(text.splitlines(), 1))
            call = {"kind": "tool_call", "agent_id": "manager", "tool_call_id": "read-1",
                    "text": 'read({"file_path":"evidence.json"})'}
            result = {"kind": "tool_result", "agent_id": "manager", "tool_call_id": "read-1", "text": receipt}
            trace = {"events": [call, result]}
            self.assertTrue(evaluation.fixture_read_receipts([trace], observations)["verified"])
            write = {"kind": "tool_call", "agent_id": "manager", "tool_call_id": "w1",
                     "text": 'write({"file_path":"decision.json","content":"{}"})'}
            self.assertFalse(evaluation.fixture_read_receipts([{"events": [write, call, result]}], observations)["decision_written_after_read"])
            self.assertTrue(evaluation.fixture_read_receipts([{"events": [call, result, write]}], observations)["decision_written_after_read"])
            for events in ([result, call], [call, {**result, "agent_id": "other"}],
                           [call, {**result, "tool_call_id": "other"}],
                           [call, {**result, "text": receipt.replace("running", "failed")}],
                           [call, {**result, "text": receipt[:len(receipt) // 2]}],
                           [{**call, "text": 'exec({"command":"cat evidence.json"})'}, result]):
                with self.subTest(events=events):
                    self.assertFalse(evaluation.fixture_read_receipts([{"events": events}], observations)["verified"])

    def test_provider_freeze_covers_route_settings_but_allows_key_rotation(self):
        provider = {"model": "m", "base_url": "https://same.example/a", "api_key": "private-one", "timeout_seconds": 10}
        original = evaluation.provider_config_digest(provider)
        self.assertEqual(original, evaluation.provider_config_digest({**provider, "api_key": "rotated"}))
        for key, value in (("base_url", "https://same.example/b"), ("api", "responses"), ("timeout_seconds", 11), ("model_context", {"m": 999})):
            self.assertNotEqual(original, evaluation.provider_config_digest({**provider, key: value}))

    def test_plan_only_freezes_inputs_and_execute_refuses_drift_then_runs_once(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root / "fixture")
            binary = root / "fake-engine"
            binary.write_text(FAKE_ENGINE.replace("PYTHON", sys.executable))
            binary.chmod(0o700)
            out = root / "evidence"
            argv = ["--mock", "--fixture-root", str(root / "fixture"), "--cases", "progress", "--zwai", str(binary),
                    "--runs", "1", "--review-wait", "0", "--out", str(out), "--plan-only"]
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(evaluation.main(argv), 0)
            manifest = json.loads((out / "manifest.json").read_text())
            self.assertIn(str(Path(evaluation.__file__).resolve()), manifest["hashes"])
            fixture_path = root / "fixture" / "cases.json"
            original = fixture_path.read_text()
            fixture_path.write_text(original + "\n")
            with self.assertRaisesRegex(evaluation.harness.EvalError, "frozen input changed"):
                evaluation.main(["--mock", "--execute-plan", str(out / "manifest.json")])
            self.assertFalse((out / "report.json").exists())
            fixture_path.write_text(original)
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(evaluation.main(["--mock", "--execute-plan", str(out / "manifest.json")]), 0)
            report = json.loads((out / "report.json").read_text())
            self.assertEqual(len(report["results"]), 2)
            self.assertTrue(all(r["automatic_passed"] for r in report["results"]))
            with self.assertRaisesRegex(evaluation.harness.EvalError, "already executed"):
                evaluation.main(["--mock", "--execute-plan", str(out / "manifest.json")])

    def test_unverified_cleanup_saves_partial_report_and_never_starts_next_round(self):
        for failed_field, failed_value in (("engine_exited", False), ("private_data_removed", False),
                                           ("descendant_cleanup", "unverified_exec_or_spawn")):
            with self.subTest(field=failed_field), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self.fixture(root / "fixture")
                binary = root / "unused-binary"
                binary.write_text("unused")
                out = root / "evidence"
                cleanup = {"engine_exited": True, "private_data_removed": True,
                           "descendant_cleanup": "no_exec_or_spawn_observed", failed_field: failed_value}

                def unsafe_trial(_task, case, _expected, arm, run, *_args):
                    return {"arm": arm, "case": case["id"], "run": run,
                            "harness_error": "", "automatic_passed": False, "cleanup": cleanup}

                argv = ["--mock", "--fixture-root", str(root / "fixture"), "--zwai", str(binary),
                        "--runs", "2", "--out", str(out)]
                with patch.object(evaluation, "run_trial", side_effect=unsafe_trial) as run, contextlib.redirect_stdout(io.StringIO()):
                    status = evaluation.main(argv)
                self.assertEqual(status, 2)
                self.assertEqual(run.call_count, 1)
                report = json.loads((out / "report.json").read_text())
                self.assertEqual(len(report["results"]), 1)
                self.assertEqual(report["stopped_early"]["reason"], "cleanup_unverified")
                self.assertEqual(report["stopped_early"]["remaining_runs"], 3)

    def test_summary_excludes_harness_failure_from_quality_but_counts_it(self):
        records = [
            {"arm": "a", "harness_error": "broken", "automatic_passed": False},
            {"arm": "a", "harness_error": "", "automatic_passed": True,
             "quality": {"checks": {"decision": True}},
             "metrics": {"total_tokens": 100, "duration_ms": 2000}, "review_cut_off": True},
        ]
        s = evaluation.summarize(records)["a"]
        self.assertEqual(s["graded"], 1)
        self.assertEqual(s["harness_errors"], 1)
        self.assertEqual(s["automatic_passed"], 1)
        self.assertEqual(s["reviews_cut_off"], 1)


FAKE_ENGINE = r'''#!PYTHON
import http.server, json, os, pathlib, signal, sqlite3, sys, threading, time
data = pathlib.Path(sys.argv[sys.argv.index("--data-dir") + 1])
with sqlite3.connect(data / "zwai.db") as db:
    db.execute("CREATE TABLE llm_calls (id INTEGER PRIMARY KEY, agent_id TEXT, total_tokens INTEGER, prompt_tokens INTEGER, completion_tokens INTEGER, duration_ms INTEGER, cached_tokens INTEGER)")
    db.execute("INSERT INTO llm_calls VALUES (1,'manager',10,8,2,1,0)")
def finish(*args):
    with sqlite3.connect(data / "zwai.db") as db:
        db.execute("INSERT INTO llm_calls VALUES (2,'title-namer',20,15,5,1,0)")
    os._exit(0)
signal.signal(signal.SIGTERM, finish)
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def reply(self, value):
        body = json.dumps(value).encode()
        self.send_response(200); self.send_header("Content-Length", str(len(body)))
        self.end_headers(); self.wfile.write(body)
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path == "/api/presence": return self.reply({"id":"p"})
        if self.path == "/api/threads": return self.reply({"thread":{"id":"th"}})
        if self.path.endswith("/turns"):
            workspace = data / "workspaces" / "th"
            (workspace / "decision.json").write_text(json.dumps({"decision":"quiet", "action":"continue", "evidence_ids":["p1"], "notification":""}))
            names = ("HOME","XDG_CONFIG_HOME","XDG_CACHE_HOME","XDG_DATA_HOME","ZDOTDIR","ZWAI_HOME","OPENAI_API_KEY")
            (workspace / "env.json").write_text(json.dumps({k:os.environ[k] for k in names if k in os.environ}))
        return self.reply({})
    def do_GET(self):
        if self.path == "/api/meta": return self.reply({"startup":"ready"})
        if self.path == "/api/presence/p":
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.end_headers()
            self.wfile.write(b": ready\n\n"); self.wfile.flush(); time.sleep(60); return
        if self.path == "/api/threads/th": return self.reply({"status":{"running":False}})
        if self.path == "/api/threads/th/turns": return self.reply({"turns":[{"id":"tn", "status":"done"}]})
        fixture = (data / "workspaces" / "th" / "evidence.json").read_text()
        receipt = "encoding=utf-8 path=evidence.json offset=1 limit=2000\n" + "".join(str(i)+"|"+line+"\n" for i,line in enumerate(fixture.splitlines(),1))
        events = [{"kind":"tool_call","agent_id":"manager","tool_call_id":"r1","text":'read({"file_path":"evidence.json"})'},
                  {"kind":"tool_result","agent_id":"manager","tool_call_id":"r1","text":receipt},
                  {"kind":"tool_call","agent_id":"manager","tool_call_id":"w1","text":'write({"file_path":"decision.json","content":"{}"})'}]
        return self.reply({"turn":{"id":"tn","thread_id":"th","status":"done","final":"wrote decision"},"events":events,"llm_calls":[{"agent_id":"manager","total_tokens":10}]})
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
(data / "engine.json").write_text(json.dumps({"pid":os.getpid(),"url":"http://127.0.0.1:"+str(server.server_port)}))
server.serve_forever()
'''


if __name__ == "__main__":
    unittest.main()
