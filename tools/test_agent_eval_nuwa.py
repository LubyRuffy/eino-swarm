"""Offline contracts for project-mode evals, model choices and fair pairing."""

import copy
import http.server
import json
import sys
import tempfile
import threading
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
    def test_task_fingerprint_ignores_generated_bytecode_but_guards_other_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "rules.py"
            source.write_text("rule = 1\n")
            task = {"dir": root, "prompt": "Apply the supplied rules.", "timeout_s": 600}
            original = protocol.task_fingerprint(task)
            cache = root / "hidden" / "__pycache__"
            cache.mkdir(parents=True)
            bytecode = cache / "rules.cpython-314.pyc"
            bytecode.write_bytes(b"generated")
            self.assertEqual(protocol.task_fingerprint(task), original)
            bytecode.write_bytes(b"regenerated")
            self.assertEqual(protocol.task_fingerprint(task), original)
            self.assertIn("hidden/__pycache__/rules.cpython-314.pyc", grade.hash_tree(root))
            extra = cache / "source.json"
            extra.write_text("{}")
            self.assertNotEqual(protocol.task_fingerprint(task), original)
            extra.unlink()
            outside = root / "rules.pyc"
            outside.write_bytes(b"outside the generated cache directory")
            self.assertNotEqual(protocol.task_fingerprint(task), original)
            outside.unlink()
            source.write_text("rule = 2\n")
            self.assertNotEqual(protocol.task_fingerprint(task), original)

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

    def test_arm_instruction_is_last_and_leaves_common_task_unchanged(self):
        models = protocol.prepare_models(CONFIG, "default", executor_ref="default\tother")
        task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
        task["project_prompt"] = "Evaluate the supplied evidence."
        original, fingerprint = copy.deepcopy(task), protocol.task_fingerprint(task)
        base = protocol.project_prompt(task, models)
        instructions = ("Use independent evidence checks.", "Keep a compact audit trail.")
        for instruction in instructions:
            self.assertEqual(protocol.project_prompt(task, models, instruction), base + "\n\n" + instruction)
        self.assertEqual(protocol.project_prompt(task, models, " \n "), base)
        self.assertEqual(protocol.project_prompt(task, None, instructions[0]),
                         task["project_prompt"] + "\n\n" + instructions[0])
        self.assertEqual(protocol.project_prompt({}, None, instructions[1]), instructions[1])
        self.assertEqual(task, original)
        self.assertEqual(protocol.task_fingerprint(task), fingerprint)


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
            checks = grade.expand_task_dir(task["checks"], task["dir"])
            selected = {}
            for path in (workspace / "records").glob("*.json"):
                value = json.loads(path.read_text())
                old = selected.get(value["component"])
                if not old or value["revision"] > old["revision"]:
                    selected[value["component"]] = {**value, "evidence": path.relative_to(workspace).as_posix()}
            answer = {"ready": all(x["status"] == "accepted" for x in selected.values()),
                      "total_passed_tests": sum(x["passed_tests"] for x in selected.values()), "components": selected}
            (workspace / "decision.json").write_text(json.dumps(answer))
            self.assertTrue(all(c["ok"] for c in grade.run_checks(checks, workspace, "", [], before)))
            array_answer = {**answer, "components": [dict(entry, component=name) for name, entry in selected.items()]}
            (workspace / "decision.json").write_text(json.dumps(array_answer))
            self.assertTrue(all(c["ok"] for c in grade.run_checks(checks, workspace, "", [], before)))
            array_answer["components"][0]["status"] = "pending"
            (workspace / "decision.json").write_text(json.dumps(array_answer))
            self.assertFalse(all(c["ok"] for c in grade.run_checks(checks, workspace, "", [], before)))
            answer["total_passed_tests"] += 10
            (workspace / "decision.json").write_text(json.dumps(answer))
            self.assertFalse(all(c["ok"] for c in grade.run_checks(checks, workspace, "", [], before)))

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
            self.assertEqual(calls[0][2]["nuwa_planner_model"], "other")
            self.assertEqual(calls[0][2]["nuwa_executor_model"], "model-one")
            self.assertEqual(calls[1][2]["project_id"], "pj")
            self.assertEqual(calls[2], ("PATCH", "/api/threads/th", {"provider_id": "default", "model": "other"}))
            self.assertEqual(calls[3][1], "/api/threads/th/turns")
            self.assertTrue(result["fixture_sha256"])

    def test_arm_instructions_reach_project_api_without_changing_task_identity(self):
        task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
        task["project_prompt"] = "Evaluate the supplied evidence."
        original = copy.deepcopy(task)
        models = protocol.prepare_models(CONFIG, "default", executor_ref="default\tother")
        calls, results = [], []

        class Engine:
            def __init__(self, zwai, data_dir, mock, boot_timeout):
                self.base, self.log_path = "http://owned.invalid", data_dir / "engine.log"
            def start(self):
                pass
            def stop(self):
                pass

        def http(base, method, path, body=None, **kwargs):
            if path == "/api/projects":
                calls.append(copy.deepcopy(body))
                return {"project": {"id": "pj", "nuwa_enabled": True}}
            return {"thread": {"id": "th", "model": "model-one", "provider_id": "default"}}

        traces = [{"turn": {"status": "done", "duration_ms": 3}, "events": [], "llm_calls": []}]
        with tempfile.TemporaryDirectory() as tmp, \
                patch.object(agent_eval, "Engine", Engine), patch.object(agent_eval, "http_json", http), \
                patch.object(agent_eval, "drive_conversation", return_value={"state": "done"}), \
                patch.object(agent_eval, "wait_for_review", return_value=traces), \
                patch.object(grade, "run_checks", return_value=[]):
            for index, instruction in enumerate((None, "Use independent evidence checks.", "Keep a compact audit trail.")):
                args = types.SimpleNamespace(zwai="zwai", mock=False, boot_timeout=1, review_wait=0,
                                             project_mode="nuwa", models=models, max_completion_tokens=0)
                if instruction is not None:
                    args.project_instruction = instruction
                results.append(agent_eval.run_once(task, index + 1, args, models["providers"][0], Path(tmp)))
        self.assertTrue(all(not row["harness_error"] for row in results), results)
        base = protocol.project_prompt(task, models)
        self.assertEqual([row["system_prompt"] for row in calls],
                         [base, base + "\n\nUse independent evidence checks.", base + "\n\nKeep a compact audit trail."])
        self.assertTrue(all(row["nuwa_planner_model"] == "model-one" and row["nuwa_executor_model"] == "other" for row in calls))
        self.assertEqual(len({row["task_sha256"] for row in results}), 1)
        self.assertEqual(len({row["fixture_sha256"] for row in results}), 1)
        self.assertEqual(task, original)


class ReasoningRunTest(unittest.TestCase):
    def run_over_http(self, requested, *, models_enabled=True, acknowledgement="correct"):
        calls = []
        thread = {"id": "owned-thread", "provider_id": "default", "model": "model-one", "reasoning_effort": ""}

        class API(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def request(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                calls.append((self.command, self.path, body))
                if self.path == "/api/projects":
                    reply = {"project": {"id": "owned-project", "nuwa_enabled": True}}
                elif self.path == "/api/threads/owned-thread" and self.command == "PATCH":
                    thread.update(body)
                    if acknowledgement == "wrong":
                        thread["reasoning_effort"] = "high"
                    elif acknowledgement == "missing":
                        thread.pop("reasoning_effort", None)
                    reply = {"thread": thread}
                else:
                    reply = {"thread": thread}
                encoded = json.dumps(reply).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            do_POST = request
            do_PATCH = request

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), API)
        serving = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01))
        serving.start()

        class Engine:
            def __init__(self, zwai, data_dir, mock, boot_timeout):
                self.base = "http://127.0.0.1:" + str(server.server_port)
                self.log_path = data_dir / "engine.log"
            def start(self):
                pass
            def stop(self):
                pass

        try:
            with tempfile.TemporaryDirectory() as tmp:
                task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
                models = protocol.prepare_models(CONFIG, "default") if models_enabled else None
                args = types.SimpleNamespace(zwai="zwai", mock=not models_enabled, boot_timeout=1, review_wait=0,
                                             project_mode="nuwa", models=models, max_completion_tokens=0)
                if requested is not None:
                    args.reasoning_effort = requested
                traces = [{"turn": {"status": "done", "duration_ms": 3}, "events": [], "llm_calls": []}]
                with patch.object(agent_eval, "Engine", Engine), \
                        patch.object(agent_eval, "drive_conversation", return_value={"state": "done"}), \
                        patch.object(agent_eval, "wait_for_review", return_value=traces), \
                        patch.object(grade, "run_checks", return_value=[]):
                    provider = models["providers"][0] if models else None
                    result = agent_eval.run_once(task, 1, args, provider, Path(tmp))
                saved = json.loads((Path(tmp) / "runs" / task["id"] / "run-1" / "result.json").read_text())
                self.assertEqual(saved, result)
        finally:
            server.shutdown()
            server.server_close()
            serving.join(timeout=1)
        self.assertFalse(serving.is_alive())
        return result, calls

    def test_supported_reasoning_choices_reach_http_patch_and_result_before_turn(self):
        identities = []
        for requested in ("", "low", "medium", "high"):
            with self.subTest(requested=requested):
                result, calls = self.run_over_http(requested)
                self.assertFalse(result["harness_error"], result["harness_error"])
                self.assertEqual(calls[1][0:2], ("POST", "/api/threads"))
                self.assertEqual(calls[2], ("PATCH", "/api/threads/owned-thread",
                                           {"provider_id": "default", "model": "model-one", "reasoning_effort": requested}))
                self.assertEqual(calls[3][0:2], ("POST", "/api/threads/owned-thread/turns"))
                self.assertEqual(result.get("reasoning_effort"), requested)
                identities.append((result["task_sha256"], result["fixture_sha256"]))
        self.assertEqual(len(set(identities)), 1)
        result, calls = self.run_over_http("low", models_enabled=False)
        self.assertFalse(result["harness_error"], result["harness_error"])
        self.assertEqual(calls[2], ("PATCH", "/api/threads/owned-thread", {"reasoning_effort": "low"}))
        self.assertEqual(result.get("reasoning_effort"), "low")

    def test_omitted_reasoning_sends_no_override_and_records_actual_api_default(self):
        for enabled in (True, False):
            with self.subTest(models_enabled=enabled):
                result, calls = self.run_over_http(None, models_enabled=enabled)
                self.assertFalse(result["harness_error"], result["harness_error"])
                self.assertTrue(all("reasoning_effort" not in body for _, _, body in calls))
                self.assertEqual(sum(method == "PATCH" for method, _, _ in calls), int(enabled))
                self.assertEqual(result.get("reasoning_effort"), "")
        result, _ = self.run_over_http(None, acknowledgement="missing")
        self.assertFalse(result["harness_error"], result["harness_error"])
        self.assertIsNone(result["reasoning_effort"])

    def test_unconfirmed_reasoning_prevents_turn_submission(self):
        for acknowledgement, requested in (("wrong", "low"), ("missing", "")):
            with self.subTest(acknowledgement=acknowledgement):
                result, calls = self.run_over_http(requested, acknowledgement=acknowledgement)
                self.assertIn("reasoning", result["harness_error"])
                self.assertFalse(result["passed"])
                self.assertFalse(any(path.endswith("/turns") for _, path, _ in calls))
                if acknowledgement == "wrong":
                    self.assertEqual(result.get("reasoning_effort"), "high")
                elif acknowledgement == "missing":
                    self.assertIsNone(result["reasoning_effort"])


if __name__ == "__main__":
    unittest.main()
