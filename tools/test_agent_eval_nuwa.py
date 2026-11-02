"""Offline contracts for project-mode evals, model choices and fair pairing."""

import copy
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import agent_eval
import agent_eval_grade as grade
import agent_eval_protocol as protocol
import agent_eval_usage as usage
from test_agent_eval import CONFIG


class ModelProtocolTest(unittest.TestCase):
    def test_responses_protocol_survives_private_config_and_evidence_summary(self):
        config = CONFIG.replace("          model: model-one\n",
                                "          model: model-one\n          api_mode: responses\n")
        models = protocol.prepare_models(config, "default")
        rendered = protocol.render_model_config(models)
        self.assertIn('api_mode: "responses"', rendered)
        self.assertEqual(grade.extract_provider(rendered, "default")["api_mode"], "responses")
        self.assertEqual(protocol.model_summary(models)["provider_settings"][0]["api_mode"], "responses")
        self.assertIn('api_mode: "responses"', grade.render_eval_config(
            grade.extract_provider(config, "default"), "eval"))
        self.assertNotRegex(rendered, r"(?m)^\s+api:")

    def test_nonempty_builtin_tools_refuse_instead_of_silently_changing_provider(self):
        for builtins in ("builtin_tools:\n            - type: web_search",
                         "builtin_tools:\n          - type: web_search",
                         'builtin_tools: [{"type": "web_search"}]'):
            config = CONFIG.replace("          model: model-one\n",
                                    "          model: model-one\n          " + builtins + "\n")
            with self.subTest(builtins=builtins), self.assertRaisesRegex(grade.TaskError, "builtin_tools"):
                protocol.prepare_models(config, "default")
        models = protocol.prepare_models(CONFIG, "default")
        models["providers"][0]["builtin_tools"] = [{"type": "web_search"}]
        with self.assertRaisesRegex(grade.TaskError, "builtin_tools"):
            protocol.render_model_config(models)
        with self.assertRaisesRegex(grade.TaskError, "builtin_tools"):
            grade.render_eval_config(models["providers"][0], "eval")

    def test_empty_builtin_tools_leave_ordinary_provider_available(self):
        for value in ("[]", "[ ]", "null", "~", ""):
            config = CONFIG.replace("          model: model-one\n",
                                    "          model: model-one\n          builtin_tools: " + value + "\n")
            with self.subTest(value=value):
                self.assertTrue(protocol.prepare_models(config, "default")["catalog"])

    def test_cli_choice_id_uses_tab_and_keeps_slashes_in_model_names(self):
        self.assertEqual(protocol.parse_ref("p\tvendor/model"),
                         {"provider_id": "p", "model": "vendor/model", "ref": "p\tvendor/model"})
        for ref in ("p/model", "p\t", "\tm", "p\tm\textra"):
            with self.assertRaises(ValueError):
                protocol.parse_ref(ref)

    def test_full_catalog_reaches_private_config_without_changing_real_config(self):
        original = str(CONFIG)
        models = protocol.prepare_models(CONFIG, "default", planner_ref="default\tmodel-one",
                                         executor_ref="default\tother")
        text = protocol.render_model_config(models, 4096)
        self.assertEqual(CONFIG, original)
        self.assertEqual({c["ref"] for c in models["catalog"]},
                         {"default\tmodel-one", "default\tother", "provider-2\tmodel-two"})
        self.assertEqual(grade.extract_provider(text, "default")["catalog"], ["model-one", "other"])
        self.assertEqual(grade.extract_provider(text, "default")["model_context"]["other"], 8192)
        self.assertNotIn("api_key", json.dumps(protocol.model_summary(models)))
        self.assertIn("max_completion_tokens: 4096", text)

    def test_unknown_or_disabled_choices_refuse_before_starting_a_run(self):
        with self.assertRaises(ValueError):
            protocol.prepare_models(CONFIG, "default", executor_ref="default\tunknown")
        disabled = CONFIG.replace('label: "second: quoted"', 'enabled: false\n          label: "second: quoted"')
        with self.assertRaises(ValueError):
            protocol.prepare_models(disabled, "default", executor_ref="provider-2\tmodel-two")

    def test_preferences_do_not_define_roles_or_change_domain_task(self):
        models = protocol.prepare_models(CONFIG, "default", executor_ref="default\tother")
        task = {"project_prompt": "Reason from the supplied records."}
        prompt = protocol.project_prompt(task, models)
        self.assertTrue(prompt.startswith(task["project_prompt"]))
        self.assertIn('"provider_id": "default"', prompt)
        self.assertIn('"model": "other"', prompt)
        self.assertIn("list_models", prompt)
        self.assertNotIn("default\\tother", prompt)
        self.assertIn("may choose", prompt)
        self.assertNotIn("researcher", prompt)


class UsageFactsTest(unittest.TestCase):
    def test_dynamic_role_names_are_not_confused_with_auxiliary_agents(self):
        selected = protocol.parse_ref("p\texecutor")
        calls = [{"agent_id": "memory-audit-1", **selected}, {"agent_id": "memory-reviewer", **selected}]
        self.assertEqual(usage.routes(calls, None, selected)["worker_calls"], 1)

    def test_raw_missing_zero_and_positive_usage_are_distinct(self):
        rows = [{"raw_usage": {}}, {"raw_usage": {"total_tokens": 0}},
                {"raw_usage": {"total_tokens": 17, "prompt_tokens_details": {"cached_tokens": 0}}}]
        facts = usage.extract(rows)
        self.assertEqual([c["total_tokens"]["state"] for c in facts["calls"]], ["absent", "zero", "value"])
        self.assertEqual(facts["known_total_tokens"], 17)
        self.assertIsNone(facts["total_tokens"])
        self.assertEqual(facts["calls"][2]["cached_tokens"]["state"], "zero")

    def test_legacy_zero_does_not_prove_no_cache_or_no_usage(self):
        self.assertEqual(usage.field_fact({"cached_tokens": 0}, "cached_tokens")["state"], "unknown")
        self.assertEqual(usage.field_fact({"cached_tokens": 0, "usage_presence": {"cached_tokens": True}},
                                         "cached_tokens")["state"], "zero")

    def test_routing_is_observed_from_actual_calls_and_keeps_no_delegation(self):
        planner = protocol.parse_ref("p\tplanner")
        executor = protocol.parse_ref("p\texecutor")
        calls = [{"agent_id": "manager", **planner}, {"agent_id": "x-1", **executor}]
        self.assertTrue(usage.routes(calls, planner, executor)["executor_preference_realized"])
        self.assertFalse(usage.routes(calls[:1], planner, executor)["executor_preference_realized"])


class ProjectRunTest(unittest.TestCase):
    def test_reconciliation_answer_key_follows_the_latest_source_records(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "nuwa-source-reconcile")
        with tempfile.TemporaryDirectory() as tmp:
            workspace = Path(tmp)
            agent_eval.prepare_workspace(task, workspace)
            before = grade.hash_tree(workspace)
            selected = {}
            for path in (workspace / "records").glob("*.json"):
                value = json.loads(path.read_text())
                old = selected.get(value["component"])
                if not old or value["revision"] > old["revision"]:
                    selected[value["component"]] = {**value, "evidence": path.relative_to(workspace).as_posix()}
            answer = {"ready": all(x["status"] == "accepted" for x in selected.values()),
                      "total_passed_tests": sum(x["passed_tests"] for x in selected.values()), "components": selected}
            (workspace / "decision.json").write_text(json.dumps(answer))
            self.assertTrue(all(c["ok"] for c in grade.run_checks(task["checks"], workspace, "", [], before)))
            answer["total_passed_tests"] += 10
            (workspace / "decision.json").write_text(json.dumps(answer))
            self.assertFalse(all(c["ok"] for c in grade.run_checks(task["checks"], workspace, "", [], before)))

    def test_nuwa_run_uses_project_api_and_actual_thread_model_before_turn(self):
        calls = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
            models = protocol.prepare_models(CONFIG, "default", planner_ref="default\tother")
            args = types.SimpleNamespace(zwai="zwai", mock=False, boot_timeout=1, review_wait=0,
                                         project_mode="nuwa", models=models, max_completion_tokens=0)

            class Engine:
                def __init__(self, zwai, data_dir, mock, boot_timeout):
                    self.base, self.log_path = "http://owned.invalid", data_dir / "engine.log"
                def start(self):
                    pass
                def stop(self):
                    pass

            def http(base, method, path, body=None, **kwargs):
                calls.append((method, path, copy.deepcopy(body)))
                if path == "/api/projects":
                    return {"project": {"id": "pj", "nuwa_enabled": True}}
                if path == "/api/threads":
                    return {"thread": {"id": "th"}}
                return {"thread": {"model": "other", "provider_id": "default"}}

            traces = [{"turn": {"status": "done", "duration_ms": 3}, "events": [], "llm_calls": []}]
            with patch.object(agent_eval, "Engine", Engine), patch.object(agent_eval, "http_json", http), \
                    patch.object(agent_eval, "drive_conversation", return_value={"state": "done"}), \
                    patch.object(agent_eval, "wait_for_review", return_value=traces), \
                    patch.object(grade, "run_checks", return_value=[]):
                result = agent_eval.run_once(task, 1, args, models["providers"][0], root / "out")
            self.assertFalse(result["harness_error"], result["harness_error"])
            self.assertEqual(calls[0][1], "/api/projects")
            self.assertTrue(calls[0][2]["nuwa_enabled"])
            self.assertEqual(calls[1][2]["project_id"], "pj")
            self.assertEqual(calls[2], ("PATCH", "/api/threads/th", {"provider_id": "default", "model": "other"}))
            self.assertEqual(calls[3][1], "/api/threads/th/turns")
            self.assertTrue(result["fixture_sha256"])


if __name__ == "__main__":
    unittest.main()
