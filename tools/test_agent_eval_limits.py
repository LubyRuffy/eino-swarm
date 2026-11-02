"""The eval harness's run limits: the output cap a run is measured under, and
what an interrupted harness leaves behind."""

import os
import contextlib
import http.server
import json
import signal
import shutil
import sys
import tempfile
import threading
import types
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402

PROVIDER = {"base_url": "https://example.invalid/v1", "api_key": "sk-limits-test-key", "model": "m1"}


class OutputCapTest(unittest.TestCase):
    # A reasoning model can spend the engine's default cap thinking before it
    # writes anything; a comparison must say which cap it measured under.
    def test_an_explicit_cap_reaches_the_engine_config(self):
        text = grade.render_eval_config(PROVIDER, "eval", 64000)
        self.assertIn("swarm:\n    max_completion_tokens: 64000\n", text)

    def test_without_a_cap_the_engine_default_applies(self):
        self.assertNotIn("max_completion_tokens", grade.render_eval_config(PROVIDER, "eval"))

    def test_the_cap_is_a_flag_and_never_negative(self):
        self.assertEqual(agent_eval.parse_args(["--mock"]).max_completion_tokens, 0)
        self.assertEqual(agent_eval.parse_args(["--mock", "--max-completion-tokens", "64000"]).max_completion_tokens, 64000)
        with self.assertRaises(SystemExit):
            agent_eval.parse_args(["--mock", "--max-completion-tokens", "-1"])

    def test_the_report_states_the_cap(self):
        s = grade.summarize([])
        md = grade.render_markdown({"started": "x", "provider": {}, "source": {}, "runs_per_task": 1,
                                    "max_completion_tokens": 64000}, s, [])
        self.assertIn("output cap per request: 64000", md)


class TerminationTest(unittest.TestCase):
    # A killed harness used to leave its temporary data directory, and the
    # provider key in its config.yaml, behind.
    def test_termination_unwinds_through_the_runs_cleanup(self):
        previous = {sig: signal.getsignal(sig) for sig in (signal.SIGTERM, signal.SIGHUP)}
        self.addCleanup(lambda: [signal.signal(sig, h) for sig, h in previous.items()])
        agent_eval.unwind_on_termination()
        seen = {}

        class Engine:
            def __init__(self, zwai, data_dir, mock, boot_timeout):
                self.base, self.log_path, self.data_dir = "", data_dir / "engine.log", data_dir
                seen["data_dir"] = data_dir

            def start(self):
                self.log_path.write_text("booted\n")
                os.kill(os.getpid(), signal.SIGTERM)

            def stop(self):
                seen["stopped"] = True

        out = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, out)
        original = agent_eval.Engine
        agent_eval.Engine = Engine
        self.addCleanup(setattr, agent_eval, "Engine", original)
        task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
        args = types.SimpleNamespace(zwai="zwai", mock=False, boot_timeout=1, review_wait=0)
        with self.assertRaises(SystemExit) as stop:
            agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
        self.assertEqual(stop.exception.code, 128 + signal.SIGTERM)
        self.assertTrue(seen.get("stopped"), "the run's own engine must be stopped")
        self.assertFalse(seen["data_dir"].exists(), "the data directory holding the key must be removed")


class CleanupFailureTest(unittest.TestCase):
    @contextlib.contextmanager
    def run_fixture(self, failure="api", cleanup_fails=True):
        # Exercise run_once's real HTTP/catch/finally boundary. A context-only
        # exception test cannot see the first error swallowed into its record.
        seen = {"engines": 0, "stops": 0, "requests": []}
        secret = PROVIDER["api_key"]

        class API(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                self.rfile.read(int(self.headers["Content-Length"]))
                seen["requests"].append(self.path)
                failed = failure == "api" and self.path == "/api/threads"
                body = {"error": "api failure " + secret} if failed else {"thread": {"id": "owned"}}
                raw = json.dumps(body).encode()
                self.send_response(500 if failed else 200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), API)
        serving = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01))
        serving.start()
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary)
            task = grade.load_task(agent_eval.TASKS_DIR / "direct-lookup")
            evidence = out / "runs" / task["id"] / "run-1"

            class Engine:
                def __init__(self, zwai, data_dir, mock, boot_timeout):
                    seen["engines"] += 1
                    seen["data_dir"] = data_dir
                    self.base = "http://127.0.0.1:" + str(server.server_port)
                    self.log_path = data_dir / "engine-stdout.log"

                def start(self):
                    self.log_path.write_text("owned engine log\n")

                def stop(self):
                    seen["stops"] += 1
                    seen["premature_result"] = (evidence / "result.json").exists()
                    snapshot = evidence / "pre-cleanup.json"
                    seen["snapshot"] = json.loads(snapshot.read_text()) if snapshot.exists() else None
                    if cleanup_fails:
                        raise agent_eval.EvalError("cleanup failure " + secret)

            traces = [{"turn": {"status": "done"}, "events": [], "llm_calls": []}]
            args = types.SimpleNamespace(zwai="zwai", mock=False, boot_timeout=1, review_wait=0)
            grading = ValueError("grade failure " + secret) if failure == "grade" else None
            try:
                with patch.object(agent_eval, "Engine", Engine), \
                        patch.object(agent_eval, "drive_conversation", return_value={"state": "done"}), \
                        patch.object(agent_eval, "wait_for_review", return_value=traces), \
                        patch.object(grade, "run_checks", side_effect=grading, return_value=[]):
                    yield task, args, out, evidence, seen
            finally:
                server.shutdown()
                server.server_close()
                serving.join(timeout=2)
                self.assertFalse(serving.is_alive())
                if "data_dir" in seen:
                    shutil.rmtree(seen["data_dir"], ignore_errors=True)

    def test_api_and_grade_first_errors_survive_cleanup_failure_and_stop_the_batch(self):
        for failure in ("api", "grade"):
            with self.subTest(failure=failure), self.run_fixture(failure) as (task, args, out, evidence, seen):
                with self.assertRaises(agent_eval.EvalError) as raised:
                    for _ in range(2):
                        agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                self.assertNotIn(PROVIDER["api_key"], str(raised.exception))
                self.assertEqual(seen["engines"], 1, "cleanup failure must stop the next case")
                self.assertEqual(seen["stops"], 1)
                self.assertFalse(seen["premature_result"])
                self.assertEqual(seen["snapshot"]["stage"], "pre_cleanup")
                record = json.loads((evidence / "result.json").read_text())
                self.assertIn(failure + " failure", record["harness_error"])
                self.assertIn("cleanup failure", record["cleanup_error"])
                self.assertFalse(record["passed"])
                receipt = json.loads((evidence / "cleanup.json").read_text())
                self.assertFalse(receipt["complete"])
                self.assertTrue(receipt["runtime_retained"])
                self.assertTrue(seen["data_dir"].is_dir())
                for path in evidence.rglob("*"):
                    if path.is_file():
                        self.assertNotIn(PROVIDER["api_key"], path.read_text())
                        self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_cleanup_failure_without_a_task_error_is_not_a_graded_success(self):
        class LegacyEvalError(agent_eval.EvalError):
            add_note = None

        for error_type in (agent_eval.EvalError, LegacyEvalError):
            with self.subTest(error_type=error_type.__name__), self.run_fixture(None) as (task, args, out, evidence, seen), \
                    patch.object(agent_eval, "EvalError", error_type):
                with self.assertRaises(error_type):
                    agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                record = json.loads((evidence / "result.json").read_text())
                self.assertTrue(record["harness_error"])
                self.assertIn("cleanup failure", record["cleanup_error"])
                self.assertFalse(record["passed"])
                self.assertTrue(seen["data_dir"].exists())

    def test_evidence_write_failures_still_stop_and_preserve_the_first_api_error(self):
        original_write = agent_eval.protocol.private_write
        for failed in ("pre-cleanup.json", "engine.log", "cleanup.json", "result.json"):
            with self.subTest(failed=failed), self.run_fixture("api", cleanup_fails=False) as (task, args, out, evidence, seen):
                def write(path, text):
                    if path.name == failed:
                        raise OSError("evidence write failure")
                    original_write(path, text)
                with patch.object(agent_eval.protocol, "private_write", side_effect=write):
                    with self.assertRaisesRegex(agent_eval.EvalError, "api failure"):
                        agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                self.assertEqual(seen["stops"], 1)
                if failed != "result.json":
                    record = json.loads((evidence / "result.json").read_text())
                    self.assertIn("api failure", record["harness_error"])
                    self.assertIn("evidence write failure", json.dumps(record["evidence_errors"]))
                else:
                    snapshot = json.loads((evidence / "pre-cleanup.json").read_text())
                    self.assertIn("api failure", snapshot["record"]["harness_error"])

    def test_required_artifact_failures_retain_runtime_and_abort_before_the_next_case(self):
        original_write = agent_eval.protocol.private_write
        original_copy = agent_eval.copy_workspace
        for failed in ("initial-files.json", "answer.md", "traces.json", "workspace"):
            with self.subTest(failed=failed), self.run_fixture(None, cleanup_fails=False) as (task, args, out, evidence, seen):
                def write(path, text):
                    if path.name == failed:
                        raise OSError("required artifact failure")
                    original_write(path, text)
                def copy(src, dest):
                    if failed == "workspace":
                        raise OSError("required artifact failure")
                    original_copy(src, dest)
                with patch.object(agent_eval.protocol, "private_write", side_effect=write), \
                        patch.object(agent_eval, "copy_workspace", side_effect=copy):
                    with self.assertRaisesRegex(agent_eval.EvalError, "required artifact failure"):
                        for _ in range(2):
                            agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                self.assertEqual(seen["engines"], 1)
                self.assertEqual(seen["stops"], 1)
                self.assertTrue(seen["data_dir"].exists())
                self.assertTrue((seen["data_dir"] / "config.yaml").exists())
                turns = [path for path in seen["requests"] if path.endswith("/turns")]
                self.assertEqual(len(turns), 0 if failed == "initial-files.json" else 1)
                record = json.loads((evidence / "result.json").read_text())
                self.assertIn("required artifact failure", record["harness_error"])
                self.assertIn(failed, json.dumps(record["evidence_errors"]))
                self.assertEqual(record["cleanup_error"], "")
                self.assertFalse(record["passed"])
                self.assertTrue(json.loads((evidence / "cleanup.json").read_text())["runtime_retained"])

    def test_api_and_grade_errors_remain_ordinary_when_cleanup_and_evidence_succeed(self):
        for failure in ("api", "grade"):
            with self.subTest(failure=failure), self.run_fixture(failure, cleanup_fails=False) as (task, args, out, evidence, seen):
                record = agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                self.assertIn(failure + " failure", record["harness_error"])
                self.assertNotIn("evidence_errors", record)
                self.assertEqual(record["cleanup_error"], "")
                self.assertEqual(seen["stops"], 1)
                self.assertFalse(seen["data_dir"].exists())
                self.assertTrue(json.loads((evidence / "cleanup.json").read_text())["complete"])

    def test_runtime_removal_failure_is_fatal_and_preserves_the_first_api_error(self):
        with self.run_fixture("api", cleanup_fails=False) as (task, args, out, evidence, seen):
            with patch.object(agent_eval.shutil, "rmtree", side_effect=PermissionError("runtime removal failure")):
                with self.assertRaisesRegex(agent_eval.EvalError, "api failure"):
                    agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
            record = json.loads((evidence / "result.json").read_text())
            self.assertIn("api failure", record["harness_error"])
            self.assertIn("runtime removal failure", record["cleanup_error"])
            self.assertTrue(seen["data_dir"].exists())
            self.assertFalse(json.loads((evidence / "cleanup.json").read_text())["complete"])

    def test_control_exit_survives_cleanup_and_result_write_failure(self):
        class LegacyKeyboardInterrupt(KeyboardInterrupt):
            add_note = None

        class LegacySystemExit(SystemExit):
            @property
            def add_note(self):
                raise AttributeError("notes API is unavailable")

        for interrupted in (KeyboardInterrupt("human stop"), SystemExit(143),
                            LegacyKeyboardInterrupt("human stop"), LegacySystemExit(143)):
            with self.subTest(interrupted=type(interrupted).__name__), self.run_fixture(None) as (task, args, out, evidence, seen):
                original_write = agent_eval.protocol.private_write
                def write(path, text):
                    if path.name == "result.json":
                        raise OSError("result write failure")
                    original_write(path, text)
                with patch.object(agent_eval, "drive_conversation", side_effect=interrupted), \
                        patch.object(agent_eval.protocol, "private_write", side_effect=write):
                    with self.assertRaises(type(interrupted)) as raised:
                        agent_eval.run_once(task, 1, args, dict(PROVIDER), out)
                self.assertIs(raised.exception, interrupted)
                if isinstance(interrupted, SystemExit):
                    self.assertEqual(raised.exception.code, 143)
                self.assertEqual(seen["stops"], 1)
                snapshot = json.loads((evidence / "pre-cleanup.json").read_text())
                self.assertIn(type(interrupted).__name__, snapshot["record"]["harness_error"])
                receipt = json.loads((evidence / "cleanup.json").read_text())
                self.assertIn("cleanup failure", receipt["error"])
                self.assertTrue(seen["data_dir"].exists())


if __name__ == "__main__":
    unittest.main()
