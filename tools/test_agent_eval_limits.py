"""The eval harness's run limits: the output cap a run is measured under, and
what an interrupted harness leaves behind."""

import os
import signal
import shutil
import sys
import tempfile
import types
import unittest
from pathlib import Path

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


if __name__ == "__main__":
    unittest.main()
