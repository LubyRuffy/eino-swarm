"""Offline tests for the opt-in agent eval harness.

None of these reach an engine or a model. They pin what would silently make
an eval number wrong: a leaked key, a check that passes on missing output, a
task answer key that no longer matches its generated fixture, or a poll loop
that misreads a parked or questioning conversation.
"""

import contextlib
import csv
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import types
import unittest
from collections import defaultdict
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402

FAKE_KEY = "sk-test-0123456789abcdef"

CONFIG = f"""server:
    addr: 127.0.0.1:8787
models:
    default: default
    providers:
        - id: default
          label: first
          base_url: https://one.example/v1
          api_key: {FAKE_KEY}
          model: model-one
          catalog:
            - model-one
            - other
          first_byte_timeout_seconds: 60
          timeout_seconds: 300
          context_window: 0
          model_context:
            model-one: 1048576
            other: 8192
        - id: provider-2
          label: "second: quoted"
          base_url: "http://10.0.0.2:8888/v1"
          api_key: ""
          model: model-two
swarm:
    max_concurrent: 6
"""


def trace(events, calls, status="done", final="", duration=1000):
    return {"turn": {"id": "tn_1", "status": status, "final": final, "duration_ms": duration},
            "events": events, "llm_calls": calls}


def ev(kind, agent="manager", text=""):
    return {"kind": kind, "agent_id": agent, "text": text}


def call(agent, tokens, chars=0):
    return {"agent_id": agent, "total_tokens": tokens, "prompt_tokens": tokens, "completion_tokens": 0,
            "duration_ms": 10, "input_chars": chars}


class ProviderConfigTest(unittest.TestCase):
    def test_reads_only_the_selected_provider(self):
        p = grade.extract_provider(CONFIG, "default")
        self.assertEqual(p["base_url"], "https://one.example/v1")
        self.assertEqual(p["api_key"], FAKE_KEY)
        self.assertEqual(p["model"], "model-one")
        self.assertEqual(p["first_byte_timeout_seconds"], "60")
        self.assertEqual(p["model_context"], {"model-one": 1048576, "other": 8192})

    def test_unquotes_scalars_of_a_later_provider(self):
        p = grade.extract_provider(CONFIG, "provider-2")
        self.assertEqual(p["base_url"], "http://10.0.0.2:8888/v1")
        self.assertEqual(p["api_key"], "")
        self.assertEqual(p["model"], "model-two")

    def test_unknown_or_incomplete_provider_fails_instead_of_guessing(self):
        with self.assertRaises(grade.TaskError):
            grade.extract_provider(CONFIG, "missing")
        with self.assertRaises(grade.TaskError):
            grade.extract_provider(CONFIG.replace("model: model-two", ""), "provider-2")

    def test_rendered_config_has_one_provider_and_round_trips(self):
        p = grade.extract_provider(CONFIG, "default")
        text = grade.render_eval_config(p, "eval")
        self.assertEqual(text.count("- id:"), 1)
        self.assertNotIn("model-two", text)
        back = grade.extract_provider(text, "eval")
        for key in ("base_url", "api_key", "model", "timeout_seconds", "first_byte_timeout_seconds"):
            self.assertEqual(back[key], p[key])
        self.assertEqual(back["model_context"], {"model-one": 1048576})

    def test_summary_names_the_model_without_credentials(self):
        p = grade.extract_provider(CONFIG, "default")
        s = grade.provider_summary(p)
        self.assertEqual(s, {"model": "model-one", "host": "one.example"})
        self.assertNotIn(FAKE_KEY, json.dumps(s))

    def test_seeded_config_file_is_private(self):
        d = Path(tempfile.mkdtemp())
        try:
            task = {"skills": [], "dir": d}
            agent_eval.seed_data_dir(d, task, grade.extract_provider(CONFIG, "default"))
            self.assertEqual(os.stat(d / "config.yaml").st_mode & 0o777, 0o600)
        finally:
            shutil.rmtree(d)


class RedactionTest(unittest.TestCase):
    def test_redact_replaces_the_key_everywhere(self):
        self.assertEqual(grade.redact(f"a {FAKE_KEY} b {FAKE_KEY}", [FAKE_KEY]), "a <redacted> b <redacted>")
        self.assertEqual(grade.redact("abc", ["", "ab"]), "abc")

    def test_harness_failure_evidence_never_contains_the_key(self):
        # A provider error can echo its request; run_once must redact the
        # first failure before persisting it.
        class FailingEngine:
            def __init__(self, zwai, data_dir, mock, boot_timeout):
                self.base = ""
                self.log_path = data_dir / "engine-stdout.log"
                self.data_dir = data_dir

            def start(self):
                self.log_path.write_text(f"auth header Bearer {FAKE_KEY}\n")
                raise agent_eval.EvalError(f"upstream rejected key {FAKE_KEY}")

            def stop(self):
                pass

        out = Path(tempfile.mkdtemp())
        original = agent_eval.Engine
        agent_eval.Engine = FailingEngine
        try:
            task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
            args = types.SimpleNamespace(zwai="zwai", mock=False, boot_timeout=1, review_wait=0)
            rec = agent_eval.run_once(task, 1, args, grade.extract_provider(CONFIG, "default"), out)
            self.assertFalse(rec["passed"])
            self.assertIn("<redacted>", rec["harness_error"])
            for path in out.rglob("*"):
                if path.is_file():
                    self.assertNotIn(FAKE_KEY, path.read_text(), path)
        finally:
            agent_eval.Engine = original
            shutil.rmtree(out)


class TraceMetricsTest(unittest.TestCase):
    def sample(self):
        events = [
            ev("user_message"),
            ev("tool_call", text='spawn_agent({"role":"a","task":"x","fork_context":true})'),
            ev("spawned", agent="a-1"),
            ev("tool_call", text='spawn_agent({"role":"b","task":"y"})'),
            ev("spawned", agent="b-2"),
            ev("tool_call", agent="a-1", text='skill_view({"name":"s"})'),
            ev("tool_call", agent="a-1", text='read({"path":"f"})'),
            ev("tool_call", text="wait_agents({})"),
            ev("model_retry", text='{"attempt":1,"cap":3,"reason":"model_idle"}'),
            ev("model_retry", text="not json"),
            ev("schedule"),
            ev("error"),
        ]
        calls = [call("manager", 100, chars=5000), call("manager", 50, chars=9000), call("a-1", 30),
                 call("b-2", 20), call("title-namer", 5), call("schedule-answer-policy", 7)]
        return [trace(events, calls, final="answer", duration=1500)]

    def test_parse_tool_call(self):
        self.assertEqual(grade.parse_tool_call('read({"path":"a(b)"})'), ("read", {"path": "a(b)"}))
        self.assertEqual(grade.parse_tool_call("wait_agents()"), ("wait_agents", {}))
        self.assertEqual(grade.parse_tool_call("broken({"), ("broken({", None))
        self.assertEqual(grade.parse_tool_call("x({bad json})"), ("x", None))

    def test_metrics_split_callers_and_count_behaviour(self):
        m = grade.extract_metrics(self.sample())
        self.assertEqual(m["calls"]["manager"]["total_tokens"], 150)
        self.assertEqual(m["calls"]["worker"]["calls"], 2)
        self.assertEqual(m["calls"]["title-namer"]["total_tokens"], 5)
        self.assertEqual(m["calls"]["schedule-answer-policy"]["calls"], 1)
        self.assertEqual(m["total_tokens"], 212)
        self.assertEqual(m["manager_prompt_chars"], 5000)
        self.assertEqual(m["workers"], ["a-1", "b-2"])
        self.assertEqual((m["spawn_calls"], m["fork_context"]), (2, 1))
        self.assertEqual(m["skill_view"], 1)
        self.assertEqual(m["tools_workers"], {"skill_view": 1, "read": 1})
        self.assertEqual(m["model_retry"], {"model_idle": 1, "unknown": 1})
        self.assertEqual((m["schedule_events"], m["errors"], m["duration_ms"]), (1, 1, 1500))

    def test_final_answer_prefers_the_latest_turn(self):
        first = trace([], [], final="I will check later")
        later = trace([ev("done", text="done text")], [], final="")
        self.assertEqual(grade.final_answer([first, later]), "done text")
        self.assertEqual(grade.final_answer([first, trace([], [], final="result")]), "result")
        self.assertEqual(grade.final_answer([]), "")

    def test_review_settles_only_on_its_event_after_the_turn(self):
        self.assertFalse(grade.review_settled([]))
        self.assertFalse(grade.review_settled([trace([ev("memory_review")], [], status="running")]))
        self.assertFalse(grade.review_settled([trace([ev("done")], [])]))
        self.assertTrue(grade.review_settled([trace([ev("memory_review")], [])]))


class ChecksTest(unittest.TestCase):
    def setUp(self):
        self.ws = Path(tempfile.mkdtemp())
        (self.ws / "reports").mkdir()
        (self.ws / "reports" / "R-1.md").write_text("format: v1\nbody\n")
        (self.ws / "out.json").write_text(json.dumps({"a": {"n": 1.004, "s": "x"}, "list": [1, 2]}))
        (self.ws / "keep.txt").write_text("same")
        (self.ws / "token.txt").write_text("tok-123\n")
        self.baseline = grade.hash_tree(self.ws)

    def tearDown(self):
        shutil.rmtree(self.ws)

    def check(self, spec, answer="", traces=None):
        return grade.run_checks([spec], self.ws, answer, traces or [], self.baseline)[0]

    def test_file_checks_fail_on_missing_output(self):
        self.assertTrue(self.check({"type": "file_exists", "path": "reports/R-*.md"})["ok"])
        self.assertFalse(self.check({"type": "file_exists", "path": "nope.md"})["ok"])
        self.assertFalse(self.check({"type": "file_regex", "path": "nope.md", "pattern": "x", "negate": True})["ok"])
        self.assertFalse(self.check({"type": "file_exists", "path": "../" + self.ws.name + "/keep.txt/.."})["ok"])

    def test_file_regex_and_negation(self):
        self.assertTrue(self.check({"type": "file_regex", "path": "reports/*.md", "pattern": r"\Aformat: v1"})["ok"])
        self.assertFalse(self.check({"type": "file_regex", "path": "reports/*.md", "pattern": "v2"})["ok"])
        self.assertTrue(self.check({"type": "file_regex", "path": "keep.txt", "pattern": "other", "negate": True})["ok"])
        self.assertFalse(self.check({"type": "file_regex", "path": "keep.txt", "pattern": "same", "negate": True})["ok"])

    def test_json_equals_uses_tolerance_and_exact_keys(self):
        ok = {"type": "json_equals", "path": "out.json", "pointer": "/a", "value": {"n": 1.0, "s": "x"}, "tolerance": 0.01}
        self.assertTrue(self.check(ok)["ok"])
        self.assertFalse(self.check(dict(ok, tolerance=0.001))["ok"])
        self.assertFalse(self.check(dict(ok, value={"n": 1.0}))["ok"])
        self.assertTrue(self.check({"type": "json_equals", "path": "out.json", "pointer": "/list/1", "value": 2})["ok"])
        self.assertFalse(self.check({"type": "json_equals", "path": "out.json", "pointer": "/missing", "value": 1})["ok"])
        (self.ws / "bad.json").write_text("{")
        self.assertFalse(self.check({"type": "json_equals", "path": "bad.json", "pointer": "", "value": {}})["ok"])

    def test_values_equal_keeps_booleans_distinct_from_numbers(self):
        self.assertFalse(grade.values_equal(True, 1, 0.1))
        self.assertTrue(grade.values_equal([1, {"a": 2}], [1.0, {"a": 2}], 1e-9))

    def test_file_unchanged_detects_edits_and_deletion(self):
        spec = {"type": "file_unchanged", "path": "keep.txt"}
        self.assertTrue(self.check(spec)["ok"])
        (self.ws / "keep.txt").write_text("changed")
        self.assertEqual(self.check(spec)["detail"], "modified")
        (self.ws / "keep.txt").unlink()
        self.assertEqual(self.check(spec)["detail"], "missing")

    def test_command_runs_in_the_workspace(self):
        self.assertTrue(self.check({"type": "command", "argv": ["sh", "-c", "test -f keep.txt"]})["ok"])
        self.assertFalse(self.check({"type": "command", "argv": ["sh", "-c", "exit 3"]})["ok"])
        self.assertFalse(self.check({"type": "command", "argv": ["/nonexistent-binary"]})["ok"])

    def test_answer_checks(self):
        self.assertTrue(self.check({"type": "answer_regex", "pattern": "4096"}, answer="it is 4096")["ok"])
        self.assertFalse(self.check({"type": "answer_regex", "pattern": "2048", "negate": True}, answer="2048")["ok"])
        self.assertTrue(self.check({"type": "answer_contains_file", "path": "token.txt"}, answer="got tok-123.")["ok"])
        self.assertFalse(self.check({"type": "answer_contains_file", "path": "token.txt"}, answer="later")["ok"])
        self.assertFalse(self.check({"type": "answer_contains_file", "path": "none.txt"}, answer="x")["ok"])

    def test_answer_chart_requires_matching_values(self):
        chart = {"type": "bar", "x": "config", "y": "p95", "data": [{"config": "a", "p95": 10.0}, {"config": "b", "p95": 5}]}
        answer = "best is b\n\n```chart\n" + json.dumps(chart) + "\n```\n"
        spec = {"type": "answer_chart", "expect": {"a": 10, "b": 5}}
        self.assertTrue(self.check(spec, answer=answer)["ok"])
        self.assertFalse(self.check(dict(spec, expect={"a": 11}), answer=answer)["ok"])
        self.assertEqual(self.check(spec, answer="no chart")["detail"], "no chart block")
        self.assertFalse(self.check(spec, answer="```chart\n{bad\n```")["ok"])

    def test_counted_checks_and_any_of(self):
        traces = [trace([ev("tool_call", agent="w-1", text='skill_view({"name":"s"})'), ev("schedule")], [])]
        self.assertTrue(self.check({"type": "tool_count", "tool": "skill_view", "min": 1}, traces=traces)["ok"])
        self.assertFalse(self.check({"type": "tool_count", "tool": "skill_view", "agent": "manager", "min": 1},
                                    traces=traces)["ok"])
        self.assertFalse(self.check({"type": "event_count", "kind": "schedule", "max": 0}, traces=traces)["ok"])
        either = {"type": "any_of", "checks": [{"type": "file_exists", "path": "nope"},
                                                {"type": "event_count", "kind": "schedule", "min": 1}]}
        self.assertTrue(self.check(either, traces=traces)["ok"])


class TaskCatalogTest(unittest.TestCase):
    def test_every_task_loads_and_is_well_formed(self):
        tasks = agent_eval.load_tasks([])
        self.assertGreaterEqual(len(tasks), 10)
        for t in tasks:
            self.assertTrue(t["prompt"], t["id"])
            self.assertIn(t["expect_delegation"], grade.DELEGATION)

    def test_unknown_task_id_is_refused(self):
        with self.assertRaises(agent_eval.EvalError):
            agent_eval.load_tasks(["no-such-task"])

    def test_malformed_tasks_are_refused(self):
        d = Path(tempfile.mkdtemp()) / "bad"
        d.mkdir()
        try:
            (d / "prompt.md").write_text("do it")
            for spec in ({"checks": []},
                         {"checks": [{"type": "nope"}]},
                         {"id": "other", "checks": [{"type": "answer_regex", "pattern": "x"}]},
                         {"checks": [{"type": "file_exists"}]},
                         {"checks": [{"type": "any_of", "checks": []}]},
                         {"checks": [{"type": "command"}]},
                         {"expect_delegation": "always", "checks": [{"type": "answer_regex", "pattern": "x"}]},
                         {"skills": [{"name": "Bad Name", "file": "x"}], "checks": [{"type": "answer_regex", "pattern": "x"}]},
                         {"skills": [{"name": "ok", "file": "missing.md"}], "checks": [{"type": "answer_regex", "pattern": "x"}]}):
                (d / "task.json").write_text(json.dumps(spec))
                with self.assertRaises(grade.TaskError, msg=json.dumps(spec)):
                    grade.load_task(d)
        finally:
            shutil.rmtree(d.parent)

    def prepared(self, task_id):
        d = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, d)
        agent_eval.prepare_workspace(grade.load_task(agent_eval.TASKS_DIR / task_id), d)
        return d

    def expected(self, task_id, name):
        task = grade.load_task(agent_eval.TASKS_DIR / task_id)
        return next(c for c in task["checks"] if c["name"] == name)["value"]

    def task_checks(self, task_id, ws, answer=""):
        task = grade.load_task(agent_eval.TASKS_DIR / task_id)
        return [r["ok"] for r in grade.run_checks(task["checks"], ws, answer, [], {})]

    # A correct result with extra explanation once failed the baseline. The
    # checks grade the deliverable, so context around it must not flip them,
    # while the wrong deliverable still must.

    def test_lookup_grades_the_stated_value_not_the_explanation(self):
        ws = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, ws)
        answer = "**{}**\n\n(value on line 13; {} and {} belong to other sections)"
        self.assertTrue(all(self.task_checks("direct-lookup", ws, answer.format(4096, 2048, 16384))))
        self.assertFalse(all(self.task_checks("direct-lookup", ws, answer.format(2048, 4096, 16384))))

    def test_endpoint_list_grades_table_rows_not_notes(self):
        ws = self.prepared("two-deliverables")
        rows = ["| GET | /v1/items | a |", "| POST | /v1/items | b |", "| GET | /v1/items/{id} | c |",
                "| PUT | /v1/items/{id} | d |", "| DELETE | /v1/items/{id} | e |", "| GET | /healthz | f |"]
        removed = "| GET | /v0/items | g |"
        endpoints = ws / "endpoints.md"
        endpoints.write_text("\n".join(rows + ["", "> /v0/items was removed."]) + "\n")
        self.assertTrue(all(self.task_checks("two-deliverables", ws)[1:]))
        endpoints.write_text("\n".join(rows + [removed]) + "\n")
        self.assertFalse(all(self.task_checks("two-deliverables", ws)[1:]))

    # The answer keys below were derived once from the generators. These
    # tests recompute them independently so a generator edit cannot leave a
    # stale key that marks correct agent output as failed.

    def test_csv_aggregate_key_matches_its_data(self):
        rows = list(csv.DictReader((self.prepared("csv-aggregate") / "sales.csv").read_text().splitlines()))
        revenue = defaultdict(Decimal)
        units = defaultdict(int)
        for r in rows:
            revenue[r["region"]] += int(r["units"]) * Decimal(r["unit_price"])
            units[r["product"]] += int(r["units"])
        self.assertEqual(self.expected("csv-aggregate", "total_units"), sum(units.values()))
        self.assertEqual(self.expected("csv-aggregate", "revenue_by_region"), {k: float(v) for k, v in revenue.items()})
        self.assertEqual(self.expected("csv-aggregate", "top_product_by_units"), max(units, key=units.get))

    def test_noisy_log_key_counts_only_error_lines(self):
        found = {}
        for line in (self.prepared("noisy-log") / "app.log").read_text().splitlines():
            m = re.match(r"(\S+) ERROR .*code=(E\d+)", line)
            if m:
                slot = found.setdefault(m.group(2), {"count": 0, "first_seen": m.group(1)})
                slot["count"] += 1
        self.assertEqual(self.expected("noisy-log", "errors"), found)

    def test_two_deliverables_key_matches_its_data(self):
        temps = defaultdict(list)
        for r in csv.DictReader((self.prepared("two-deliverables") / "data" / "temps.csv").read_text().splitlines()):
            temps[r["city"]].append(Decimal(r["temp_c"]))
        means = {c: float((sum(v) / len(v)).quantize(Decimal("0.1"), ROUND_HALF_UP)) for c, v in temps.items()}
        self.assertEqual(self.expected("two-deliverables", "city means"), means)

    def test_survey_key_is_stated_in_each_service(self):
        ws = self.prepared("multi-dir-survey")
        key = self.expected("multi-dir-survey", "survey")
        self.assertEqual(sorted(key), sorted(p.name for p in (ws / "services").iterdir()))
        for name, want in key.items():
            text = "".join(p.read_text() for p in (ws / "services" / name).rglob("*") if p.is_file())
            self.assertIn(str(want["port"]), text, name)
            self.assertIn(want["team"], text, name)

    def unittest_in(self, ws):
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
        return subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", "tests", "-t", ".", "-q"],
                              cwd=ws, capture_output=True, env=env).returncode

    def test_bugfix_fixture_starts_broken(self):
        self.assertNotEqual(self.unittest_in(self.prepared("bugfix-python")), 0)

    def test_rename_fixture_starts_green_with_the_old_name(self):
        ws = self.prepared("cross-file-rename")
        self.assertEqual(self.unittest_in(ws), 0)
        self.assertIn("def norm_ws(", (ws / "textkit" / "core.py").read_text())


class DriveConversationTest(unittest.TestCase):
    """Uses scripted status replies and a fake clock instead of an engine."""

    def drive(self, statuses, task=None, turns=None):
        calls = []
        replies = iter(statuses)
        clock = [0.0]

        def fake_http(base, method, path, body=None, timeout=30):
            calls.append((method, path))
            if path.endswith("/interrupt"):
                return None
            if path.endswith("/turns"):
                return {"turns": turns or []}
            return {"status": next(replies)}

        def sleep(s):
            clock[0] += s

        original = agent_eval.http_json
        agent_eval.http_json = fake_http
        try:
            task = task or {"timeout_s": 100}
            out = agent_eval.drive_conversation("http://x", "th", task, now=lambda: clock[0], sleep=sleep)
        finally:
            agent_eval.http_json = original
        return out, calls

    def test_finished_turn_is_done(self):
        out, _ = self.drive([{"running": True}, {"running": False}])
        self.assertEqual(out["state"], "done")

    def test_turn_finished_before_first_poll_is_done(self):
        out, _ = self.drive([{"running": False}, {"running": False}], turns=[{"status": "done"}])
        self.assertEqual(out["state"], "done")

    def test_question_is_recorded_and_interrupted(self):
        out, calls = self.drive([{"running": True, "awaiting_answer": True}])
        self.assertEqual(out["state"], "asked")
        self.assertIn(("POST", "/api/threads/th/interrupt"), calls)

    def test_iteration_cap_is_recorded(self):
        out, _ = self.drive([{"running": True, "awaiting_continue": True}])
        self.assertEqual(out["state"], "iteration_cap")

    def test_wait_is_followed_only_when_the_task_allows_it(self):
        out, _ = self.drive([{"running": True}, {"running": False, "waiting": True}])
        self.assertEqual((out["state"], out["waited"]), ("done", True))
        allowed = {"timeout_s": 1000, "allow_wait": True, "wait_timeout_s": 100}
        out, _ = self.drive([{"running": True}, {"running": False, "waiting": True},
                             {"running": True}, {"running": False}], task=allowed)
        self.assertEqual((out["state"], out["waited"]), ("done", True))

    def test_a_failed_final_turn_is_not_a_finished_task(self):
        done = {"state": "done", "asked": False, "iteration_cap": False, "waited": False}
        failed = [{"turn": {"status": "done"}}, {"turn": {"status": "error", "error": "the model service is unavailable"}}]
        out = agent_eval.settled_outcome(done, failed)
        self.assertEqual((out["state"], out["error"]), ("turn_error", "the model service is unavailable"))
        self.assertEqual(agent_eval.settled_outcome(done, [{"turn": {"status": "done"}}])["state"], "done")
        self.assertEqual(agent_eval.settled_outcome(done, [])["state"], "done")
        asked = {**done, "state": "asked"}
        self.assertEqual(agent_eval.settled_outcome(asked, failed)["state"], "asked")

    def test_wait_and_overall_deadlines(self):
        allowed = {"timeout_s": 1000, "allow_wait": True, "wait_timeout_s": 3}
        out, _ = self.drive([{"running": True}] + [{"running": False, "waiting": True}] * 10, task=allowed)
        self.assertEqual(out["state"], "wait_timeout")
        out, calls = self.drive([{"running": True}] * 10, task={"timeout_s": 5})
        self.assertEqual(out["state"], "timeout")
        self.assertIn(("POST", "/api/threads/th/interrupt"), calls)


class ReportTest(unittest.TestCase):
    def test_summary_and_markdown_cover_passes_failures_and_harness_errors(self):
        metrics = grade.extract_metrics([trace([ev("spawned", agent="w-1")],
                                               [call("manager", 90, 1000), call("memory-reviewer", 10)])])
        results = [
            {"task": "t", "run": 1, "passed": True, "harness_error": "", "outcome": {"state": "done"},
             "checks": [{"name": "c", "ok": True, "detail": ""}], "metrics": metrics, "expect_delegation": "none"},
            {"task": "t", "run": 2, "passed": False, "harness_error": "", "outcome": {"state": "done"},
             "checks": [{"name": "c", "ok": False, "detail": "missing"}], "metrics": metrics, "expect_delegation": "none",
             "review_cut_off": True},
            {"task": "t", "run": 3, "passed": False, "harness_error": "EvalError: boom"},
        ]
        s = grade.summarize(results)
        t = s["tasks"]["t"]
        self.assertEqual((s["runs"], s["passed"], s["harness_errors"]), (3, 1, 1))
        self.assertEqual((s["reviews_cut_off"], t["reviews_cut_off"]), (1, 1))
        self.assertEqual((t["passed"], t["runs_with_workers"], t["failed_checks"]), (1, 2, {"c": 1}))
        self.assertEqual(t["aux_token_share"], 0.1)
        self.assertEqual(s["token_share_by_category"], {"manager": 0.9, "memory-reviewer": 0.1})
        md = grade.render_markdown({"started": "x", "provider": {"mock": True}, "source": {}, "runs_per_task": 3},
                                   s, results)
        self.assertIn("| t | 1/3 | none |", md)
        self.assertIn("t run 2: outcome done; c (missing)", md)
        self.assertIn("t run 3: harness error: EvalError: boom", md)
        self.assertIn("memory reviews cut off at shutdown: 1", md)

    # A review still running when the harness stops the engine was reported
    # as a cheap review in the first baseline. Detection relies on the
    # engine's shutdown warning, so a reworded warning must fail here.
    def test_review_cut_off_is_detected_from_the_engines_own_warning(self):
        engine_go = (agent_eval.REPO / "internal" / "engine" / "engine.go").read_text()
        self.assertIn(grade.REVIEW_CUT_OFF_LOG, engine_go)
        self.assertTrue(grade.review_cut_off(f'level=WARN msg="{grade.REVIEW_CUT_OFF_LOG}; its notes may be incomplete"'))
        self.assertFalse(grade.review_cut_off("level=INFO msg=stopped"))


class ArgsTest(unittest.TestCase):
    def test_provider_is_required_for_a_real_eval(self):
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                agent_eval.parse_args([])
            with self.assertRaises(SystemExit):
                agent_eval.parse_args(["--provider", "x", "--runs", "0"])
        self.assertTrue(agent_eval.parse_args(["--mock"]).mock)

    def test_child_environment_cannot_be_seeded_by_the_callers_variables(self):
        os.environ["OPENAI_API_KEY"] = "leak"
        os.environ["ZWAI_MODEL_NAME"] = "leak"
        try:
            env = agent_eval.Engine(Path("zwai"), Path("/tmp/data"), False, 1).child_env()
        finally:
            del os.environ["OPENAI_API_KEY"], os.environ["ZWAI_MODEL_NAME"]
        self.assertNotIn("OPENAI_API_KEY", env)
        self.assertNotIn("ZWAI_MODEL_NAME", env)
        self.assertEqual(env["ZWAI_HOME"], "/tmp/data")


if __name__ == "__main__":
    unittest.main()
