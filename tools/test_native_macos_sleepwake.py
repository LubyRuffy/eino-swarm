"""Real sleep/wake caller and cleanup; power, AX and business IO are HOST seams."""
import argparse
from contextlib import ExitStack
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("sleepwake_harness", Path(__file__).with_name("native_macos_resilience.py"))
h = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = h
SPEC.loader.exec_module(h)


class SleepWakeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.context = dict(case_id="a" * 32, evidence=self.root, awake_process=object())
        self.expected = dict(text="unsent", quotes=[], files=[], state="editing")
        self.result = dict(snapshots=[], sleep_cycles=0)
        self.clock, self.calls, self.polls = 100.0, [], 0
        self.advance = {}
        self.own_event_remains = False
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.stack.enter_context(mock.patch.object(h.time, "monotonic", side_effect=lambda: self.clock))
        self.stack.enter_context(mock.patch.object(h.time, "sleep", side_effect=self.sleep))
        self.stack.enter_context(mock.patch.object(h.subprocess, "run", side_effect=self.power))
        self.stack.enter_context(mock.patch.object(h.subprocess, "check_output", side_effect=self.output))
        self.stop = self.stack.enter_context(mock.patch.object(h.background, "stop_awake"))
        self.start = self.stack.enter_context(mock.patch.object(h.background, "start_awake"))
        self.verify = self.stack.enter_context(mock.patch.object(h, "verify_draft", side_effect=self.verified))
        self.capture = self.stack.enter_context(mock.patch.object(h, "snapshot", side_effect=self.snapshot))

    def sleep(self, seconds):
        self.clock += seconds

    def power(self, argv, **kwargs):
        self.calls.append(tuple(argv))
        return subprocess.CompletedProcess(argv, 0, stdout=b"", stderr=b"")

    def output(self, argv, **kwargs):
        self.calls.append(tuple(argv))
        if argv[-1] == "sched":
            return "zwai-case-" + self.context["case_id"] + "-0" if self.own_event_remains else ""
        self.polls += 1
        if self.polls % 2:
            return "previous power log\n"
        self.clock += self.advance.get("wake_poll", 0)
        return "previous power log\nEntering Sleep\nWake from Normal Sleep\n"

    def verified(self, context, expected, timeout):
        self.calls.append(("verify", timeout))
        self.clock += self.advance.get("verify", 0)
        return expected

    def snapshot(self, context, name):
        self.calls.append(("snapshot", name))
        self.clock += self.advance.get("snapshot", 0)
        return name + ".png"

    def execute(self, hook=None, cycles=3):
        if hook is None:
            return h.sleepwake(self.context, self.expected, cycles, 10, self.result)
        return h.sleepwake(self.context, self.expected, cycles, 10, self.result, after_wake=hook)

    def assert_cleaned(self, cycles=1, held=True):
        cancellations = [row for row in self.calls if "cancel" in row]
        self.assertEqual(len(cancellations), cycles)
        for number, row in enumerate(cancellations):
            self.assertEqual(row[-1], "zwai-case-" + self.context["case_id"] + "-" + str(number))
            receipt = h.read_json(self.root / ("wake-cleanup-" + str(number) + ".json"))
            self.assertEqual(receipt["owner"], row[-1])
            self.assertFalse(receipt["own_event_remaining"])
        self.assertEqual(self.stop.call_count, cycles)
        self.assertEqual(self.start.call_count, cycles if held else 0)

    def test_default_three_cycles_keep_actual_power_and_cleanup_sequence(self):
        self.execute()
        self.assertEqual(self.result["sleep_cycles"], 3)
        self.assertEqual(self.result["snapshots"], ["wake-0.png", "wake-1.png", "wake-2.png"])
        self.assertEqual(self.verify.call_count, 3)
        self.assert_cleaned(3)

    def test_hook_sees_saved_log_and_same_original_deadline_and_does_not_authorize_by_return(self):
        deadlines = []
        def after(context, cycle, deadline):
            self.assertIs(context, self.context)
            self.assertIn("Wake from", (self.root / ("sleepwake-" + str(cycle) + ".log")).read_text())
            self.assertEqual(deadline.ends, self.clock + 10)
            deadlines.append(deadline)
            self.clock += 3
            return False  # Refusal is an exception; a truthy result is not authority.
        self.execute(after)
        self.assertEqual([call.args[2] for call in self.verify.call_args_list], [7, 7, 7])
        self.assertEqual(len({id(item) for item in deadlines}), 3)
        self.assertEqual(self.result["sleep_cycles"], 3)
        self.assert_cleaned(3)

    def test_unknown_refusal_preserves_primary_and_stops_before_verification_or_next_sleep(self):
        cause = ValueError("raw lock key absent")
        refusal = RuntimeError("unknown is not unlocked")
        refusal.__cause__ = cause
        def after(*args):
            raise refusal
        with self.assertRaises(RuntimeError) as caught:
            self.execute(after)
        self.assertIs(caught.exception, refusal)
        self.assertIs(caught.exception.__cause__, cause)
        self.verify.assert_not_called()
        self.capture.assert_not_called()
        self.assertEqual(self.result["sleep_cycles"], 0)
        self.assert_cleaned()

    def test_cancellation_during_hook_preserves_instance_and_cleans_original_wake(self):
        cancellation = KeyboardInterrupt()
        def after(*args):
            raise cancellation
        with self.assertRaises(KeyboardInterrupt) as caught:
            self.execute(after)
        self.assertIs(caught.exception, cancellation)
        self.verify.assert_not_called()
        self.assert_cleaned()

    def test_slow_hook_cannot_restart_verification_budget(self):
        def after(*args):
            self.clock += 11
        with self.assertRaises(TimeoutError):
            self.execute(after)
        self.verify.assert_not_called()
        self.capture.assert_not_called()
        self.assertEqual(self.result["sleep_cycles"], 0)
        self.assert_cleaned()

    def test_slow_wake_query_cannot_advance_to_hook_or_verification(self):
        self.advance["wake_poll"] = 11
        hook = mock.Mock()
        with self.assertRaises(TimeoutError):
            self.execute(hook)
        hook.assert_not_called()
        self.verify.assert_not_called()
        self.assert_cleaned()

    def test_slow_saved_wake_log_cannot_advance_to_hook_or_verification(self):
        original = Path.write_text
        def write(path, *args, **kwargs):
            result = original(path, *args, **kwargs)
            if path.name == "sleepwake-0.log":
                self.clock += 11
            return result
        hook = mock.Mock()
        with mock.patch.object(Path, "write_text", write), self.assertRaises(TimeoutError):
            self.execute(hook)
        hook.assert_not_called()
        self.verify.assert_not_called()
        self.assert_cleaned()

    def test_slow_verification_does_not_start_snapshot_or_credit_cycle(self):
        self.advance["verify"] = 11
        with self.assertRaises(TimeoutError):
            self.execute()
        self.capture.assert_not_called()
        self.assertEqual(self.result["sleep_cycles"], 0)
        self.assert_cleaned()

    def test_slow_snapshot_does_not_credit_cycle_or_start_next_sleep(self):
        self.advance["snapshot"] = 11
        with self.assertRaises(TimeoutError):
            self.execute()
        self.assertEqual(self.result["sleep_cycles"], 0)
        self.assertEqual(self.result["snapshots"], [])
        self.assert_cleaned()

    def test_slow_progress_write_cannot_credit_cycle_or_start_next_sleep(self):
        original = h.atomic_json
        def save(path, value):
            original(path, value)
            if path.name == "progress.json":
                self.clock += 11
        with mock.patch.object(h, "atomic_json", side_effect=save), self.assertRaises(TimeoutError):
            self.execute()
        self.assertEqual(self.result["sleep_cycles"], 0)
        self.assertEqual(self.result["snapshots"], [])
        self.assert_cleaned()

    def test_missing_wake_evidence_is_polled_with_the_same_deadline(self):
        original = self.output
        calls = []
        def output(argv, **kwargs):
            if argv[-1] == "log" and self.polls == 1 and not calls:
                calls.append(kwargs["timeout"])
                return "previous power log\n"
            return original(argv, **kwargs)
        with mock.patch.object(h.subprocess, "check_output", side_effect=output):
            self.execute(cycles=1)
        self.assertEqual(calls, [10])
        self.assertAlmostEqual(self.verify.call_args.args[2], 9.8)
        self.assert_cleaned()

    def test_no_assertion_is_invented_when_the_case_did_not_hold_one(self):
        del self.context["awake_process"]
        self.execute(cycles=1)
        self.assert_cleaned(held=False)

    def test_remaining_own_schedule_still_refuses_the_case(self):
        self.own_event_remains = True
        with self.assertRaisesRegex(RuntimeError, "wake schedule remains"):
            self.execute(cycles=1)
        self.assertTrue(h.read_json(self.root / "wake-cleanup-0.json")["own_event_remaining"])

    def test_real_scenario_default_routes_into_actual_sleepwake_without_an_actor(self):
        plan = h.phase_plan(64800, 3, 60)
        manifest = dict(phases=plan, budgets=h.phase_budgets(plan, 10), helper="/fixture-helper")
        campaign = self.root / "campaign.json"
        h.atomic_json(campaign, manifest)
        (self.root / "engine.json").write_text(json.dumps(dict(pid=9)))
        context = dict(self.context, engine_pid=9, origin="http://127.0.0.1:1", workspace=self.root, thread_id="saved")
        env = dict(ZWAI_NATIVE_CASE_ID=self.context["case_id"], ZWAI_NATIVE_DATA=str(self.root), ZWAI_NATIVE_EVIDENCE=str(self.root))
        def submit(ctx, text, timeout, result, **kwargs):
            result["turn_ids"].append("checkpoint")
        def start(ctx, *args):
            ctx["background"] = dict(running=dict(turn_id="running"), parked=dict(turn_id="parked"))
        with ExitStack() as stack:
            stack.enter_context(mock.patch.dict(h.os.environ, env))
            for name, value in {"require_guest":None, "verify_inputs":"actual-input-seam", "context_for":context,
                                "native":dict(textareas=1), "collect_draft":self.expected,
                                "draft_snapshot":self.expected, "turn_rows":[]}.items():
                stack.enter_context(mock.patch.object(h, name, return_value=value))
            stack.enter_context(mock.patch.object(h, "submit", side_effect=submit))
            for name in ("prepare", "resources", "check", "finish", "retain"):
                stack.enter_context(mock.patch.object(h.background, name))
            stack.enter_context(mock.patch.object(h.background, "start", side_effect=start))
            actual = stack.enter_context(mock.patch.object(h, "sleepwake", wraps=h.sleepwake))
            self.assertEqual(h.scenario(argparse.Namespace(campaign=campaign, phase="sleepwake", timeout=10)), 0)
            self.assertEqual(actual.call_count, 1)
            self.assertEqual(actual.call_args.kwargs, {})
        receipt = h.read_json(self.root / "resilience.json")
        self.assertTrue(receipt["passed"])
        self.assertEqual(receipt["sleep_cycles"], 3)
        self.assertEqual(receipt["engine_pid_before"], receipt["engine_pid_after"])
        self.assertEqual(len([row for row in self.calls if "sleepnow" in row]), 3)


if __name__ == "__main__":
    unittest.main()
