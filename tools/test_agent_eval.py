"""Offline tests for the opt-in agent eval harness.

None of these reach an engine or a model. They pin what would silently make
an eval number wrong: a leaked key, a check that passes on missing output, a
task answer key that no longer matches its generated fixture, or a poll loop
that misreads a parked or questioning conversation.
"""

import contextlib
import csv
import importlib.util
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


def call(agent, tokens, chars=0, cached=0):
    return {"agent_id": agent, "total_tokens": tokens, "prompt_tokens": tokens, "completion_tokens": 0,
            "duration_ms": 10, "input_chars": chars, "cached_tokens": cached}


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

    def test_another_model_keeps_the_endpoint_and_its_own_context_window(self):
        # Two models on one endpoint are compared by --model; a wrong context
        # window would change when compaction starts and confound the comparison.
        p = grade.select_model(grade.extract_provider(CONFIG, "default"), " other ")
        self.assertEqual(p["model"], "other")
        self.assertEqual(p["base_url"], "https://one.example/v1")
        text = grade.render_eval_config(p, "eval")
        self.assertIn('model: "other"', text)
        self.assertIn('"other": 8192', text)
        self.assertNotIn("model-one", text)
        same = grade.extract_provider(CONFIG, "default")
        self.assertIs(grade.select_model(same, ""), same)

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
        self.assertEqual(m["manager_first_call"], {"prompt_tokens": 100, "cached_tokens": 0})
        self.assertEqual(m["workers"], ["a-1", "b-2"])
        self.assertEqual((m["spawn_calls"], m["fork_context"]), (2, 1))
        self.assertEqual(m["skill_view"], 1)
        self.assertEqual(m["tools_workers"], {"skill_view": 1, "read": 1})
        self.assertEqual(m["model_retry"], {"model_idle": 1, "unknown": 1})
        self.assertEqual((m["schedule_events"], m["errors"], m["duration_ms"]), (1, 1, 1500))

    # Cost per caller is only comparable between architectures when the
    # provider's cache is visible: a cheap-looking run with a cold prefix
    # and a dear-looking one with a warm prefix bill very differently.
    def test_cached_tokens_are_kept_per_caller(self):
        m = grade.extract_metrics([trace([], [call("manager", 100, cached=40), call("manager", 50, cached=30),
                                              call("w-1", 20), call("memory-reviewer", 10, cached=5)])])
        self.assertEqual(m["calls"]["manager"]["cached_tokens"], 70)
        self.assertEqual(m["calls"]["worker"]["cached_tokens"], 0)
        self.assertEqual(m["calls"]["memory-reviewer"]["cached_tokens"], 5)

    # Delegation only saves time if the manager stays out of the workers'
    # way. Work it does itself while a worker is still running is the
    # duplicate-investigation signal; lifecycle tools and its own work after
    # the last worker finished are not.
    def test_manager_tool_calls_while_workers_run_are_counted(self):
        events = [
            ev("tool_call", text='read({"file_path":"before"})'),
            ev("tool_call", text='spawn_agent({"role":"a","task":"x"})'),
            ev("tool_call", text='spawn_agent({"role":"b","task":"y"})'),
            ev("spawned", agent="a-1"),
            ev("spawned", agent="b-2"),
            ev("tool_call", agent="a-1", text='read({"file_path":"worker"})'),
            ev("tool_call", text='read({"file_path":"mine"})'),
            ev("tool_call", text='grep({"pattern":"p"})'),
            ev("tool_call", text='wait_agents({"agent_ids":["a-1"]})'),
            ev("finished", agent="a-1"),
            ev("tool_call", text='exec({"command":"still b running"})'),
            ev("finished", agent="b-2"),
            ev("tool_call", text='write({"file_path":"after"})'),
            ev("tool_call", text='close_agent({"agent_id":"a-1"})'),
        ]
        m = grade.extract_metrics([trace(events, [])])
        self.assertEqual(m["manager_tools_while_workers_run"], 3)
        self.assertEqual(grade.extract_metrics([trace([ev("tool_call", text="read({})")], [])])
                         ["manager_tools_while_workers_run"], 0)

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

    def test_command_argv_can_name_a_checker_kept_beside_the_task(self):
        # A hidden test must not sit in the workspace the agent can read, so a
        # command check names it through the task directory placeholder.
        task_dir = self.ws.parent / (self.ws.name + "-task")
        task_dir.mkdir()
        self.addCleanup(shutil.rmtree, task_dir)
        (task_dir / "check.py").write_text("import sys\nsys.exit(0 if open('keep.txt').read() == 'same' else 1)\n")
        spec = {"type": "command", "argv": ["python3", "{task_dir}/check.py"]}
        expanded = grade.expand_task_dir([spec, {"type": "any_of", "checks": [spec]}], task_dir)
        self.assertEqual(expanded[0]["argv"][1], f"{task_dir}/check.py")
        self.assertEqual(expanded[1]["checks"][0]["argv"][1], f"{task_dir}/check.py")
        self.assertEqual(spec["argv"][1], "{task_dir}/check.py", "the task's own spec must not be rewritten")
        self.assertTrue(grade.run_checks(expanded, self.ws, "", [], self.baseline)[0]["ok"])
        (self.ws / "keep.txt").write_text("other")
        self.assertFalse(grade.run_checks(expanded, self.ws, "", [], self.baseline)[0]["ok"])

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
        self.assertGreaterEqual(len(tasks), 16)
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

    def test_steered_aggregate_grades_the_steer_against_the_same_data(self):
        steered = grade.load_task(agent_eval.TASKS_DIR / "csv-aggregate-steer")
        self.assertEqual([r["kind"] for r in steered["replay"]], ["steer"])
        self.assertNotIn("units_by_region", steered["prompt"], "only the steer may ask for the extra field")
        rows = list(csv.DictReader((self.prepared("csv-aggregate-steer") / "sales.csv").read_text().splitlines()))
        by_region = defaultdict(int)
        for r in rows:
            by_region[r["region"]] += int(r["units"])
        self.assertEqual(self.expected("csv-aggregate-steer", "units_by_region"), dict(by_region))
        base = grade.load_task(agent_eval.TASKS_DIR / "csv-aggregate")
        self.assertEqual(steered["checks"][:-1], base["checks"])

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
        for task_id in ("multi-dir-survey", "multi-dir-survey-large"):
            ws = self.prepared(task_id)
            key = self.expected(task_id, "survey")
            self.assertEqual(sorted(key), sorted(p.name for p in (ws / "services").iterdir()), task_id)
            for name, want in key.items():
                text = "".join(p.read_text() for p in (ws / "services" / name).rglob("*") if p.is_file())
                self.assertIn(str(want["port"]), text, name)
                self.assertIn(want["team"], text, name)

    # The large variant is only a scale test if its bulk is noise. Filler that
    # happened to state a port or name a current team would let a worker
    # answer from the wrong file, and would make the key depend on luck.
    def test_large_survey_filler_never_states_an_answer(self):
        ws = self.prepared("multi-dir-survey-large")
        key = self.expected("multi-dir-survey-large", "survey")
        teams = {v["team"] for v in key.values()}
        filler = [p for p in (ws / "services").rglob("*.md") if "notes" in p.parts]
        self.assertGreaterEqual(len(filler), 12 * 20)
        for path in filler:
            text = path.read_text()
            self.assertIsNone(re.search(r"\b(9\d{3}|10\d{3})\b", text), path)
            self.assertFalse({w for w in re.findall(r"[a-z]+", text.lower())} & teams, path)

    def test_large_survey_is_large_and_its_ports_are_distinct(self):
        ws = self.prepared("multi-dir-survey-large")
        key = self.expected("multi-dir-survey-large", "survey")
        self.assertEqual(len(key), 12)
        self.assertEqual(len({v["port"] for v in key.values()}), 12)
        size = sum(p.stat().st_size for p in (ws / "services").rglob("*") if p.is_file())
        self.assertGreater(size, 400_000)

    def unittest_in(self, ws):
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
        return subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", "tests", "-t", ".", "-q"],
                              cwd=ws, capture_output=True, env=env).returncode

    # A bugfix task is only a measurement if every package can be fixed by
    # its two substitutions and its tests notice each one; otherwise a
    # failed run could be the fixture's fault.
    def test_multi_module_fixture_starts_broken_and_is_fixable(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "multi-module-bugfix")
        suites = [c for c in task["checks"] if c["type"] == "command"]
        self.assertEqual(len(suites), 6)
        broken = self.prepared("multi-module-bugfix")
        for c in suites:
            self.assertEqual(subprocess.run(c["argv"], cwd=broken, capture_output=True,
                                            env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1")).returncode,
                             1, c["name"])
        gen = agent_eval.TASKS_DIR / "multi-module-bugfix" / "generate.py"
        fixed = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, fixed)
        subprocess.run([sys.executable, str(gen), str(fixed), "--fixed"], check=True)
        results = grade.run_checks(task["checks"], fixed, "", [], grade.hash_tree(broken))
        self.assertTrue(all(r["ok"] for r in results), [r for r in results if not r["ok"]])

    def test_multi_module_each_defect_is_caught_by_its_own_tests(self):
        spec = importlib.util.spec_from_file_location(
            "multi_module_generate", agent_eval.TASKS_DIR / "multi-module-bugfix" / "generate.py")
        gen = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(gen)
        for name, (_, subs, _) in gen.PACKAGES.items():
            self.assertEqual(len(subs), 2, name)
            for i in range(len(subs)):
                ws = Path(tempfile.mkdtemp())
                self.addCleanup(shutil.rmtree, ws)
                gen.main(ws, fixed=True)
                (ws / "pkgs" / name / f"{name}.py").write_text(gen.render(name, [i]))
                code = subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", f"pkgs/{name}", "-q"],
                                      cwd=ws, capture_output=True,
                                      env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1")).returncode
                self.assertNotEqual(code, 0, f"{name} defect {i} is not caught")

    # The slow variant is a measurement of overlapped waiting only if it is
    # the same fixture plus the wait; a different one could not be compared.
    def test_slow_multi_module_fixture_is_the_same_packages_plus_a_wait(self):
        base, slow = self.prepared("multi-module-bugfix"), self.prepared("multi-module-bugfix-slow")
        names = sorted(p.name for p in (base / "pkgs").iterdir())
        self.assertEqual(names, sorted(p.name for p in (slow / "pkgs").iterdir()))
        for name in names:
            self.assertEqual((base / "pkgs" / name / f"{name}.py").read_text(),
                             (slow / "pkgs" / name / f"{name}.py").read_text())
            plain = (base / "pkgs" / name / f"test_{name}.py").read_text()
            waited = (slow / "pkgs" / name / f"test_{name}.py").read_text()
            self.assertTrue(waited.endswith(plain), name)
            self.assertRegex(waited[: len(waited) - len(plain)], r"time\.sleep\(\d+\)")
        slow_spec = grade.load_task(agent_eval.TASKS_DIR / "multi-module-bugfix-slow")
        base_spec = grade.load_task(agent_eval.TASKS_DIR / "multi-module-bugfix")
        self.assertEqual([c["name"] for c in slow_spec["checks"]], [c["name"] for c in base_spec["checks"]])
        self.assertEqual(slow_spec["prompt"], base_spec["prompt"])

    # The review pair is a measurement only if the hidden rules reach past the
    # plain cases an agent sees: a solution that satisfies the visible tests
    # must still miss rules, or every arm would pass and show nothing.
    PLAIN_CASE_SOLUTIONS = {
        "durations": """import re
def parse_duration(text):
    size = {"d": 86400, "h": 3600, "m": 60, "s": 1}
    return sum(int(n) * size[u] for n, u in re.findall(r"(\\d+)([dhms])", text))
def format_duration(seconds):
    out = ""
    for u, size in (("d", 86400), ("h", 3600), ("m", 60), ("s", 1)):
        n, seconds = divmod(seconds, size)
        if n:
            out += "%d%s" % (n, u)
    return out
""",
        "semver": """def parse(text):
    core, _, pre = text.partition("-")
    a, b, c = (int(x) for x in core.split("."))
    return (a, b, c, tuple(pre.split(".")) if pre else ())
def compare(x, y):
    px, py = parse(x), parse(y)
    return (px > py) - (px < py)
def sort_versions(vs):
    return sorted(vs, key=parse)
""",
        "ranges": """def parse_ranges(text, max_page):
    pages = set()
    for item in text.split(","):
        a, _, b = item.partition("-")
        pages.update(range(int(a), int(b or a) + 1))
    return sorted(pages)
def format_ranges(pages):
    pages = sorted(set(pages))
    out, i = [], 0
    while i < len(pages):
        j = i
        while j + 1 < len(pages) and pages[j + 1] == pages[j] + 1:
            j += 1
        out.append("%d-%d" % (pages[i], pages[j]) if j > i else str(pages[i]))
        i = j + 1
    return ",".join(out)
""",
    }

    def spec_modules_check(self, ws, module):
        hidden = agent_eval.TASKS_DIR / "spec-modules" / "hidden" / "run_hidden.py"
        return subprocess.run([sys.executable, str(hidden), module], cwd=ws, capture_output=True, text=True,
                              env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))

    def test_spec_modules_reference_meets_every_hidden_rule(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "spec-modules")
        broken = self.prepared("spec-modules")
        fixed = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, fixed)
        subprocess.run([sys.executable, str(task["dir"] / "generate.py"), str(fixed), "--fixed"], check=True)
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), fixed, "", [],
                                   grade.hash_tree(broken))
        self.assertTrue(all(r["ok"] for r in results), [r for r in results if not r["ok"]])

    def test_spec_modules_starts_without_a_solution_and_hides_its_rule_tests(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "spec-modules")
        ws = self.prepared("spec-modules")
        names = sorted(p.relative_to(ws).as_posix() for p in ws.rglob("*") if p.is_file())
        self.assertEqual(names, sorted(f"{m}/{f}" for m in ("durations", "semver", "ranges")
                                       for f in ("README.md", "test_basic.py")))
        results = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), ws, "", [],
                                   grade.hash_tree(ws))
        self.assertEqual([r["ok"] for r in results[:3]], [False] * 3, "nothing written yet must not pass")
        self.assertTrue(all(r["ok"] for r in results[3:]), "the untouched README and tests are the baseline")

    def test_spec_modules_hidden_rules_reject_a_plain_case_solution(self):
        for module, source in self.PLAIN_CASE_SOLUTIONS.items():
            ws = self.prepared("spec-modules")
            (ws / module / f"{module}.py").write_text(source)
            basic = subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", module, "-q"], cwd=ws,
                                   capture_output=True, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
            self.assertEqual(basic.returncode, 0, f"{module}: the plain solution must satisfy the visible tests")
            hidden = self.spec_modules_check(ws, module)
            self.assertEqual(hidden.returncode, 1, module)
            failed = hidden.stdout.strip().splitlines()[-1].removeprefix("failed rules: ").split(",")
            self.assertGreaterEqual(len(failed), 4, f"{module}: only {failed} are beyond the plain cases")

    def test_spec_modules_reports_a_missing_module_instead_of_crashing(self):
        ws = self.prepared("spec-modules")
        out = self.spec_modules_check(ws, "durations")
        self.assertEqual(out.returncode, 1)
        self.assertIn("failed rules: import", out.stdout)

    def test_review_arms_differ_only_in_the_request_for_a_check(self):
        base = grade.load_task(agent_eval.TASKS_DIR / "spec-modules")
        selfreview = grade.load_task(agent_eval.TASKS_DIR / "spec-modules-selfreview")
        independent = grade.load_task(agent_eval.TASKS_DIR / "spec-modules-review")
        files = lambda ws: {p.relative_to(ws).as_posix(): p.read_bytes() for p in ws.rglob("*") if p.is_file()}
        reference = files(self.prepared("spec-modules"))
        self.assertEqual(files(self.prepared("spec-modules-selfreview")), reference)
        self.assertEqual(files(self.prepared("spec-modules-review")), reference)
        for arm in (selfreview, independent):
            self.assertTrue(arm["prompt"].startswith(base["prompt"]))
            self.assertGreater(len(arm["prompt"]), len(base["prompt"]))
        self.assertIn("worker", independent["prompt"])
        self.assertNotIn("worker", selfreview["prompt"])
        self.assertEqual(selfreview["checks"], base["checks"])
        self.assertEqual(independent["checks"][:-1], base["checks"])
        self.assertEqual(independent["checks"][-1]["tool"], "spawn_agent")

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
            if body and body.get("text"):
                calls.append(("text", body["text"], clock[0]))
            if path.endswith(("/interrupt", "/steer", "/answers")):
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

    # A replay is the recorded human side of a conversation. Sending a steer
    # early, answering a question nobody asked, or dropping a later turn would
    # make the replayed run a different conversation from the recorded one.
    def test_replayed_steer_waits_for_its_offset(self):
        task = {"timeout_s": 100, "replay": [{"kind": "steer", "text": "also check b", "after_s": 3}]}
        out, calls = self.drive([{"running": True}] * 3 + [{"running": False}], task=task)
        self.assertEqual((out["state"], out["replayed"]), ("done", 1))
        sent = [c for c in calls if c[0] == "text"]
        self.assertEqual(sent, [("text", "also check b", 4.0)])
        self.assertIn(("POST", "/api/threads/th/steer"), calls)

    def test_replayed_answer_continues_the_turn_instead_of_interrupting(self):
        task = {"timeout_s": 100, "replay": [{"kind": "answer", "text": "the second option", "after_s": 9}]}
        out, calls = self.drive([{"running": True, "awaiting_answer": True}, {"running": True}, {"running": False}],
                                task=task)
        self.assertEqual((out["state"], out["asked"], out["replayed"]), ("done", False, 1))
        self.assertIn(("POST", "/api/threads/th/answers"), calls)
        self.assertNotIn(("POST", "/api/threads/th/interrupt"), calls)

    def test_replayed_later_turn_starts_after_the_conversation_settles(self):
        task = {"timeout_s": 100, "replay": [{"kind": "turn", "text": "now summarise", "after_s": 50}]}
        out, calls = self.drive([{"running": True}, {"running": False}, {"running": True}, {"running": False}],
                                task=task, turns=[{"status": "done"}])
        self.assertEqual((out["state"], out["replayed"]), ("done", 1))
        self.assertIn(("POST", "/api/threads/th/turns"), calls)
        self.assertNotIn("replay_unused", out)

    def test_an_answer_the_run_never_asked_for_is_reported_not_sent(self):
        task = {"timeout_s": 100, "replay": [{"kind": "answer", "text": "yes", "after_s": 0}]}
        out, calls = self.drive([{"running": True}, {"running": False}], task=task)
        self.assertEqual((out["state"], out["replayed"], out["replay_unused"]), ("done", 0, ["answer"]))
        self.assertNotIn(("POST", "/api/threads/th/answers"), calls)

    def test_replay_entries_are_validated_when_the_task_loads(self):
        d = Path(tempfile.mkdtemp()) / "replay-x"
        d.mkdir()
        try:
            base = {"id": "replay-x", "prompt": "p", "checks": [{"type": "answer_regex", "pattern": "x"}]}
            for bad in ([{"kind": "shout", "text": "x"}], [{"kind": "steer", "text": " "}],
                        [{"kind": "steer", "text": "x", "after_s": -1}]):
                (d / "task.json").write_text(json.dumps({**base, "replay": bad}))
                with self.assertRaises(grade.TaskError):
                    grade.load_task(d)
            (d / "task.json").write_text(json.dumps({**base, "replay": [{"kind": "turn", "text": "next", "after_s": 2}]}))
            self.assertEqual(grade.load_task(d)["replay"], [{"kind": "turn", "text": "next", "after_s": 2.0}])
            self.assertEqual([t["id"] for t in agent_eval.load_tasks([], d.parent)], ["replay-x"])
        finally:
            shutil.rmtree(d.parent)

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

    # The architecture question is "did folding the work into the manager
    # win or lose here", so runs of one task are split by whether it
    # delegated, with medians, instead of one blended mean.
    def test_runs_are_split_by_whether_they_delegated(self):
        def run(n, spawned, tokens, ms, cached=0, mine=0):
            events = [ev("spawned", agent=f"w-{i}") for i in range(spawned)]
            m = grade.extract_metrics([trace(events, [call("manager", tokens, 1000, cached)], duration=ms)])
            m["manager_tools_while_workers_run"] = mine
            return {"task": "t", "run": n, "passed": True, "harness_error": "", "outcome": {"state": "done"},
                    "checks": [], "metrics": m, "expect_delegation": "optional"}

        results = [run(1, 0, 100, 20000), run(2, 0, 300, 40000), run(3, 2, 900, 60000, cached=450, mine=4),
                   run(4, 2, 700, 80000, cached=350, mine=8), run(5, 2, 800, 70000, mine=6)]
        s = grade.summarize(results)
        split = s["tasks"]["t"]["delegation_split"]
        self.assertEqual((split["solo"]["runs"], split["solo"]["passed"]), (2, 2))
        self.assertEqual((split["solo"]["duration_ms_median"], split["solo"]["tokens_median"]), (30000, 200))
        self.assertEqual((split["delegated"]["runs"], split["delegated"]["duration_ms_median"],
                          split["delegated"]["tokens_median"]), (3, 70000, 800))
        self.assertEqual(split["delegated"]["manager_tools_while_workers_run_median"], 6)
        self.assertEqual(s["cache_hit_by_category"], {"manager": round(800 / 2800, 3)})
        md = grade.render_markdown({"started": "x", "provider": {}, "source": {}, "runs_per_task": 5}, s, results)
        self.assertIn("## Delegated vs solo", md)
        self.assertIn("| t | 2 | 30.0 | 200 | 3 | 70.0 | 800 | 6 |", md)
        self.assertIn("| manager | 2800 | 1.0 | 800 | 0.286 |", md)
        # One call per run here, so every call is a run's first call.
        self.assertEqual(s["first_call_cache"], {"prompt_tokens": 2800, "cached_tokens": 800, "hit": 0.286})
        self.assertIn("First manager call of each run: 800 of 2800 prompt tokens cached (0.286)", md)

    def test_a_task_that_never_delegated_has_an_empty_delegated_side(self):
        m = grade.extract_metrics([trace([], [call("manager", 10, 1000)])])
        s = grade.summarize([{"task": "t", "run": 1, "passed": True, "harness_error": "", "outcome": {},
                              "checks": [], "metrics": m, "expect_delegation": "none"}])
        self.assertEqual(s["tasks"]["t"]["delegation_split"]["delegated"]["runs"], 0)
        md = grade.render_markdown({"started": "x", "provider": {}, "source": {}, "runs_per_task": 1}, s, [])
        self.assertIn("| t | 1 |", md)

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
