"""Offline measurement-integrity regressions; no provider or real history."""

import copy
import contextlib
import io
import json
import os
import sqlite3
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import project_history_eval as evaluation


class ProjectHistoryEvalTest(unittest.TestCase):
    def setUp(self):
        self.raw, self.oracle = evaluation.load_fixture(evaluation.FIXTURE)
        self.case = self.raw["cases"][0]
        self.expected = self.oracle["cases"][self.case["id"]]

    def traces(self, args, result, answer=""):
        return [{"turn": {"id": "tn_target", "final": answer}, "events": [
            {"kind": "tool_call", "agent_id": "worker-1", "tool_call_id": "q1",
             "text": "query_history(" + json.dumps(args) + ")"},
            {"kind": "tool_result", "agent_id": "worker-1", "tool_call_id": "q1",
             "text": json.dumps(result, ensure_ascii=False)},
        ]}]

    def all_current_rows(self):
        return [[message["content"]] for thread in self.case["threads"] if thread["project"] == "current"
                for message in thread["messages"]]

    def test_both_cases_have_independent_opposite_decisions_and_hidden_rubric(self):
        self.assertEqual(len(self.raw["cases"]), 2)
        a, b = self.raw["cases"]
        a_body, b_body = json.dumps(a, ensure_ascii=False), json.dumps(b, ensure_ascii=False)
        self.assertIn("首批明确不提供", a_body)
        self.assertIn("允许纳入首批", b_body)
        for case in (a, b):
            self.assertNotIn("required_sources", case)
            self.assertNotIn("rubric", case)

    def test_real_worker_sql_results_establish_retrieval_but_never_semantic_pass(self):
        traces = self.traces({"sql": "SELECT content FROM messages", "params": []},
                             {"ok": True, "columns": ["content"], "rows": self.all_current_rows()})
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertTrue(review["structural_retrieval_ok"])
        self.assertEqual(review["semantic_status"], "unreviewed")
        self.assertNotIn("passed", review)
        self.assertEqual(review["successful_sql_results"], 1)

    def test_schema_only_or_failed_sql_cannot_establish_retrieval(self):
        for args, result in (({}, {"ok": True, "rows": self.all_current_rows()}),
                             ({"sql": "SELECT * FROM messages"}, {"ok": False, "rows": self.all_current_rows()}),
                             ({"sql": "SELECT * FROM messages"}, {"ok": True, "columns": ["content"]})):
            with self.subTest(args=args, result=result):
                review = evaluation.structural_review(self.traces(args, result), self.case, self.expected)
                self.assertFalse(review["structural_retrieval_ok"])
                self.assertEqual(review["successful_sql_results"], 0)

    def test_wrong_worker_receipt_or_fabricated_answer_cannot_replace_sql_evidence(self):
        traces = self.traces({"sql": "SELECT * FROM messages"}, {"ok": True, "rows": self.all_current_rows()},
                             "\n".join(r[0] for r in self.all_current_rows()))
        traces[0]["events"][1]["agent_id"] = "manager"
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertEqual(review["covered_full_source_ids"], [])
        self.assertFalse(review["structural_retrieval_ok"])

    def test_count_or_partial_excerpt_does_not_claim_full_source_coverage(self):
        for rows in ([[12]], [[self.all_current_rows()[0][0][:20]]]):
            review = evaluation.structural_review(self.traces(
                {"sql": "SELECT count(*) FROM messages"}, {"ok": True, "rows": rows}), self.case, self.expected)
            self.assertFalse(review["all_required_sources_seen"])
            self.assertEqual(review["semantic_status"], "unreviewed")

    def test_baseline_exec_evidence_is_reported_without_requiring_new_tool(self):
        traces = self.traces({"sql": "unused"}, {"ok": True, "rows": self.all_current_rows()})
        traces[0]["events"][0]["text"] = 'exec({"command":"read synthetic database"})'
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertEqual(review["sql_calls"], 0)
        self.assertTrue(review["other_tool_full_source_ids"])
        self.assertEqual(review["semantic_status"], "unreviewed")
        traces[0]["events"][1]["tool_call_id"] = "unmatched-result"
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertEqual(review["any_tool_full_source_ids"], [])

    def test_cross_project_leak_in_retrieval_or_answer_is_retained(self):
        marker = self.expected["foreign_markers"][0]
        for rows, answer in ((self.all_current_rows() + [[marker]], ""), (self.all_current_rows(), marker)):
            review = evaluation.structural_review(self.traces(
                {"sql": "SELECT * FROM messages"}, {"ok": True, "rows": rows}, answer), self.case, self.expected)
            self.assertEqual(review["foreign_markers_leaked"], [marker])
            self.assertFalse(review["structural_retrieval_ok"])

    def test_foreign_scope_rule_applies_equally_to_exec_baseline_and_written_output(self):
        marker = self.expected["foreign_markers"][0]
        traces = self.traces({}, {"output": marker})
        traces[0]["events"][0]["text"] = 'exec({"command":"read synthetic database"})'
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertEqual(review["other_tool_results_foreign_markers"], [marker])
        self.assertEqual(review["foreign_markers_leaked"], [marker])
        traces[0]["events"] = [{"kind": "tool_call", "agent_id": "manager", "tool_call_id": "w1",
                                   "text": 'write(' + json.dumps({"content": marker}) + ')'}]
        review = evaluation.structural_review(traces, self.case, self.expected)
        self.assertEqual(review["write_arguments_foreign_markers"], [marker])
        self.assertEqual(review["foreign_markers_leaked"], [marker])

    def test_evidence_is_private_without_changing_subprocess_umask(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "evidence.json"
            old = os.umask(0o022)
            try:
                path.write_text("previous public file")
                evaluation.private_write(path, "private")
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                probe = Path(temp) / "build-output"
                probe.write_text("ordinary build modes remain ordinary")
                self.assertEqual(probe.stat().st_mode & 0o777, 0o644)
            finally:
                os.umask(old)

    def test_engine_default_paths_and_shell_startup_are_private_without_claiming_confinement(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            engine = evaluation.PrivateEngine(Path("unused"), root, False, 10)
            with patch.dict(os.environ, {"HOME": "/real-home", "ZWAI_HOME": "/real-data",
                                         "OPENAI_API_KEY": "source-secret", "ZDOTDIR": "/real-shell"}):
                env = engine.child_env()
            self.assertEqual(env["ZWAI_HOME"], str(root))
            for name in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "ZDOTDIR"):
                self.assertTrue(Path(env[name]).is_relative_to(root), name)
            self.assertNotIn("OPENAI_API_KEY", env)

    def test_bootstrap_creates_only_titled_threads_and_never_feeds_answer_key(self):
        calls = []

        def http(_base, _method, path, body):
            calls.append((path, body))
            return {"project" if path.endswith("projects") else "thread": {"id": f"id-{len(calls)}"}}

        with patch.object(evaluation.harness, "http_json", side_effect=http):
            ids = evaluation.create_conversations("http://fixture", self.case)
        self.assertEqual(len(ids["threads"]), len(self.case["threads"]))
        for path, body in calls:
            self.assertNotIn("system_prompt", body)
            if path.endswith("threads"):
                self.assertTrue(body["title"])
        self.assertNotIn("rubric", json.dumps(calls))
        self.assertNotIn("turns", json.dumps(calls))

    def test_offline_seed_preserves_original_bytes_identity_dates_and_terminal_state(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            with sqlite3.connect(root / "zwai.db") as db:
                db.executescript("""
                    CREATE TABLE messages (id INTEGER PRIMARY KEY, thread_id TEXT, turn_id TEXT,
                      seq INTEGER, role TEXT, agent_id TEXT, source TEXT, content TEXT, created_at TEXT);
                    CREATE TABLE events (id INTEGER PRIMARY KEY, thread_id TEXT, turn_id TEXT,
                      seq INTEGER, kind TEXT, agent_id TEXT, text TEXT, created_at TEXT);
                    CREATE TABLE turns (id TEXT, thread_id TEXT, seq INTEGER, status TEXT,
                      user_text TEXT, final TEXT, started_at TEXT, ended_at TEXT);
                    CREATE TABLE threads (id TEXT, created_at TEXT, updated_at TEXT, last_active_at TEXT);
                    CREATE TABLE search_docs (id INTEGER PRIMARY KEY, body TEXT);
                    INSERT INTO search_docs (body) VALUES ('stale title-only index');
                """)
            identities = {"threads": {th["key"]: th["key"] for th in self.case["threads"]}}
            with self.assertRaisesRegex(evaluation.harness.EvalError, "owner marker"):
                evaluation.seed_raw_history(root, self.case, identities)
            evaluation.private_write(root / evaluation.OWNER, "synthetic-v1\n")
            mapping = evaluation.seed_raw_history(root, self.case, identities)
            with sqlite3.connect(root / "zwai.db") as db:
                rows = db.execute("SELECT content,created_at FROM messages ORDER BY id").fetchall()
                raw_rows = [(m["content"], m["at"]) for th in self.case["threads"] for m in th["messages"]]
                self.assertEqual(rows, raw_rows)
                self.assertEqual({r[0] for r in db.execute("SELECT status FROM turns")}, {"done"})
                self.assertEqual(db.execute("SELECT COUNT(*) FROM search_docs").fetchone()[0], 0)
            self.assertEqual([s["source_id"] for s in mapping],
                             [m["id"] for th in self.case["threads"] for m in th["messages"]])
            with self.assertRaisesRegex(evaluation.harness.EvalError, "nonempty"):
                evaluation.seed_raw_history(root, self.case, identities)

    def test_bad_oracle_reference_is_rejected_and_original_fixture_not_rewritten(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            oracle = copy.deepcopy(self.oracle)
            oracle["cases"][self.case["id"]]["required_sources"].append(["a13"])
            (root / "raw.json").write_text(json.dumps(self.raw))
            (root / "expectations.json").write_text(json.dumps(oracle))
            with self.assertRaisesRegex(evaluation.harness.EvalError, "foreign"):
                evaluation.load_fixture(root)

    def test_omitted_reasoning_does_not_patch_any_setting(self):
        receipt = {}
        with patch.object(evaluation.harness, "http_json") as http:
            evaluation.apply_reasoning_condition("http://private", "th_target", "", receipt)
        http.assert_not_called()
        self.assertEqual(receipt, {"requested": ""})

    def test_explicit_reasoning_changes_only_private_target_and_requires_readback(self):
        receipt = {}
        replies = [{"thread": {"reasoning_effort": "high"}},
                   {"thread": {"id": "th_target", "reasoning_effort": "high"}}]
        with patch.object(evaluation.harness, "http_json", side_effect=replies) as http:
            evaluation.apply_reasoning_condition("http://private", "th_target", "high", receipt)
        self.assertEqual(http.call_args_list[0].args,
                         ("http://private", "PATCH", "/api/threads/th_target", {"reasoning_effort": "high"}))
        self.assertEqual(http.call_args_list[1].args, ("http://private", "GET", "/api/threads/th_target"))
        self.assertEqual(receipt["readback"], "high")
        for returned in ({"id": "th_target", "reasoning_effort": "low"},
                         {"id": "th_wrong", "reasoning_effort": "high"}):
            with self.subTest(returned=returned), patch.object(evaluation.harness, "http_json", side_effect=[replies[0], {"thread": returned}]):
                with self.assertRaisesRegex(evaluation.harness.EvalError, "readback mismatch"):
                    evaluation.apply_reasoning_condition("http://private", "th_target", "high", {})

    def test_reasoning_trace_does_not_invent_missing_call_fields_or_accept_mismatch(self):
        traces = [{"turn": {"id": "tn", "reasoning_effort": "high"}, "llm_calls": [
            {"id": 1, "agent_id": "manager", "reasoning_tokens": 42},
            {"id": 2, "agent_id": "schedule-answer-policy", "reasoning_effort": "low"}]}]
        receipt = {}
        evaluation.verify_reasoning_trace(traces, "high", receipt)
        self.assertTrue(receipt["turn_effort_verified"])
        self.assertFalse(receipt["model_calls"][0]["effort_field_recorded"])
        self.assertIsNone(receipt["model_calls"][0]["reasoning_effort"])
        self.assertIn("do not prove upstream", receipt["limits"])
        with self.assertRaisesRegex(evaluation.harness.EvalError, "turn reasoning-effort mismatch"):
            evaluation.verify_reasoning_trace([{"turn": {"id": "tn", "reasoning_effort": ""}}], "high", {})
        traces[0]["llm_calls"][0]["reasoning_effort"] = "low"
        with self.assertRaisesRegex(evaluation.harness.EvalError, "model-call reasoning-effort mismatch"):
            evaluation.verify_reasoning_trace(traces, "high", {})

    def test_reasoning_cli_only_accepts_existing_ui_levels(self):
        self.assertEqual(evaluation.parse_args(["--mock"]).reasoning_effort, "")
        for effort in ("low", "medium", "high"):
            self.assertEqual(evaluation.parse_args(["--mock", "--reasoning-effort", effort]).reasoning_effort, effort)
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            evaluation.parse_args(["--mock", "--reasoning-effort", "ultra"])

    def test_action_failure_retains_trace_and_original_error_before_cleanup(self):
        for evidence_fails in (False, True):
            with self.subTest(evidence_fails=evidence_fails), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                binary = root / "binary"
                binary.write_text("identified fixture executable")
                steps = []

                class FakeEngine:
                    def __init__(self, _binary, data_dir, _mock, _timeout):
                        self.data_dir = data_dir
                        self.log_path = data_dir / "engine-stdout.log"
                        self.base = "http://private-engine"
                        self.proc = types.SimpleNamespace(returncode=0)

                    def start(self):
                        steps.append("start")

                    def stop(self):
                        steps.append("stop")

                def fetch(_base, _thread):
                    steps.append("capture")
                    if evidence_fails:
                        raise OSError("secondary-evidence-error")
                    return [{"turn": {"id": "tn_original", "final": "partial source-secret"}, "events": []}]

                args = types.SimpleNamespace(mock=False, boot_timeout=1, max_completion_tokens=0, timeout=1)
                with patch.object(evaluation, "PrivateEngine", FakeEngine), \
                        patch.object(evaluation, "create_conversations", return_value={"target": "th_target"}), \
                        patch.object(evaluation, "seed_raw_history", return_value=[]), \
                        patch.object(evaluation.harness, "http_json", return_value={}), \
                        patch.object(evaluation.harness, "drive_conversation", side_effect=RuntimeError("primary-source-secret")), \
                        patch.object(evaluation.harness, "fetch_traces", side_effect=fetch):
                    result = evaluation.run_case(binary, "baseline", self.case, self.expected, args,
                                                 {"base_url": "http://private-provider", "model": "fixture", "api_key": "source-secret"},
                                                 root / "evidence")
                self.assertEqual(result["harness_error"], "RuntimeError: primary-<redacted>")
                self.assertTrue(result["cleanup"]["private_data_removed"])
                self.assertLess(steps.index("capture"), len(steps) - 1)
                self.assertEqual(steps[-1], "stop")
                if evidence_fails:
                    self.assertIn("secondary-evidence-error", result["failure_evidence_error"])
                else:
                    trace = (root / "evidence" / "traces-at-failure.json").read_text()
                    self.assertIn("tn_original", trace)
                    self.assertNotIn("source-secret", trace)
                self.assertIn("Traceback", (root / "evidence" / "error.txt").read_text())


if __name__ == "__main__":
    unittest.main()
