"""A wall-clock summary cannot stand in for eighteen hours of active observations."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import os
import threading
from types import SimpleNamespace
import unittest
from unittest import mock
import native_macos_resilience as native_driver

SPEC = importlib.util.spec_from_file_location("soak", Path(__file__).with_name("native_macos_resilience_soak.py"))
soak = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(soak)


class Clock:
    def __init__(self):
        self.monotonic, self.wall = 1000.0, 1000000.0

    def advance(self, seconds):
        self.monotonic += seconds
        self.wall += seconds


class SoakTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="issue94-private-soak-unit-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.clock = Clock()
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(soak.time, "monotonic", side_effect=lambda: self.clock.monotonic).start()
        mock.patch.object(soak.time, "time", side_effect=lambda: self.clock.wall).start()
        mock.patch.object(soak.time, "sleep", side_effect=lambda seconds: self.clock.advance(seconds)).start()
        self.phase = dict(name="soak", duration_seconds=64800, interval_seconds=60)
        self.budget = dict(scenario_seconds=64980, cleanup_seconds=60, whole_case_seconds=65040)
        self.expected = dict(text="Unsent fixture context 中文", quotes=[dict(id="q", text="source")],
                             files=[dict(id="f", name="binary", kind="file", sha256="a" * 64)], state="editing")
        self.draft = copy.deepcopy(self.expected)
        self.background = dict(running=dict(thread_id="running-thread", turn_id="running-turn", worker_id="worker-a"),
                               parked=dict(thread_id="parked-thread", turn_id="parked-turn", worker_id="worker-b", wake_id="wake"))
        self.context = dict(case_id="a" * 32, engine_pid=100, app_pid=200, evidence=self.directory,
                            data=self.directory / "data", background=copy.deepcopy(self.background))
        self.engine_pid = 100
        self.actions, self.resource_samples = [], 0
        self.fill_ack, self.fill_changes = True, True

        def check(context, harness):
            self.clock.advance(.1)
            return copy.deepcopy(self.background)

        def resources(context, harness):
            self.resource_samples += 1
            self.clock.advance(.1)

        def native(context, action="snapshot", value=""):
            self.actions.append((action, value))
            self.clock.advance(.1)
            if action == "fill" and self.fill_changes:
                self.draft["text"] = value
            return dict(case_id=context["case_id"], pid=context["app_pid"], filled=self.fill_ack,
                        has_web_area=True, textareas=1, values=[self.draft["text"]])

        def verify(context, expected, timeout):
            self.clock.advance(.1)
            if self.draft != expected:
                raise RuntimeError("actual draft/UI readback differs")
            return copy.deepcopy(self.draft)

        self.harness = SimpleNamespace(
            background=SimpleNamespace(check=check, resources=resources), native=native, verify_draft=verify,
            draft_snapshot=lambda context: copy.deepcopy(self.draft),
            read_json=lambda path: dict(pid=self.engine_pid),
            atomic_json=lambda path, value: Path(path).write_text(json.dumps(value)))

    def begin(self):
        return soak.start(self.context, self.harness, self.phase, self.budget)

    def tick(self, state):
        return soak.record_tick(self.context, self.harness, state, soak.begin_tick(state))

    def delayed_actual_io(self, corrupt=False):
        stored = dict(value=copy.deepcopy(self.expected), pending=None, due=0, reads=0)
        original = self.harness.native
        def native(context, action="snapshot", value=""):
            row = original(context, action, value)
            if action == "fill":
                stored.update(pending=copy.deepcopy(self.draft), due=self.clock.monotonic + .4)
            return dict(row, text="binary")
        def snapshot(context):
            stored["reads"] += 1
            if stored["pending"] is not None and self.clock.monotonic >= stored["due"]:
                stored["value"], stored["pending"] = stored["pending"], None
                if corrupt:
                    stored["value"]["files"][0]["sha256"] = "b" * 64
            return copy.deepcopy(stored["value"])
        self.harness.native, self.harness.draft_snapshot = native, snapshot
        mock.patch.object(native_driver, "current_shell", return_value=True).start()
        mock.patch.object(native_driver, "native", side_effect=native).start()
        mock.patch.object(native_driver, "draft_snapshot", side_effect=snapshot).start()
        mock.patch.object(native_driver.time, "sleep", side_effect=self.clock.advance).start()
        # Run the production verifier and its real wait/deadline implementation;
        # only the native/API transport and passage of time are controlled.
        self.harness.verify_draft = mock.Mock(wraps=native_driver.verify_draft)
        return stored

    def complete(self):
        state = self.begin()
        while True:
            self.tick(state)
            elapsed = self.clock.monotonic - state["started_monotonic"]
            if elapsed >= self.phase["duration_seconds"]:
                break
            self.clock.advance(min(self.phase["interval_seconds"], self.phase["duration_seconds"] - elapsed))
        result = dict(case_id=self.context["case_id"], phase="soak", engine_pid_before=100, engine_pid_after=100,
                      background=copy.deepcopy(self.background))
        soak.finish(self.context, self.harness, state, result)
        return state, result

    def test_real_actions_and_durable_ticks_cover_the_whole_interval(self):
        state, result = self.complete()
        accepted = soak.accept_evidence(self.directory, result, self.phase, self.budget)
        self.assertGreaterEqual(accepted["elapsed_seconds"], 64800)
        self.assertGreater(accepted["tick_count"], 1000)
        self.assertEqual(len(self.actions), accepted["tick_count"] * 2)
        self.assertEqual(self.resource_samples, accepted["tick_count"])
        self.assertEqual(self.draft, self.expected)
        rows = [json.loads(line) for line in (self.directory / soak.TICKS).read_text().splitlines()]
        self.assertEqual([row["seq"] for row in rows], list(range(1, len(rows) + 1)))
        self.assertTrue(all(row["passed"] and row["ui"]["fill"]["filled"] and row["ui"]["restore"]["filled"] for row in rows))
        self.assertTrue(all(row["draft_sha256"] == rows[0]["draft_sha256"] != row["edited_sha256"] for row in rows))
        self.assertNotIn(self.expected["text"], (self.directory / soak.TICKS).read_text())

    def test_actual_verifier_waits_for_full_autosave_after_edit_and_restore(self):
        stored = self.delayed_actual_io()
        receipt = self.tick(self.begin())
        self.assertTrue(receipt["passed"])
        self.assertEqual(stored["value"], self.expected)
        self.assertGreater(stored["reads"], 5)
        budgets = [call.args[2] for call in self.harness.verify_draft.call_args_list]
        self.assertEqual(len(budgets), 3)
        self.assertTrue(all(0 < later < earlier <= 180 for earlier, later in zip(budgets, budgets[1:])))

    def test_full_snapshot_mismatch_cannot_be_hidden_by_matching_visible_text(self):
        self.delayed_actual_io(corrupt=True)
        state = self.begin()
        started = self.clock.monotonic
        with self.assertRaisesRegex(TimeoutError, "persist"):
            self.tick(state)
        self.assertLess(self.clock.monotonic - started, 181)
        self.assertEqual(self.draft, self.expected)
        self.assertFalse(json.loads((self.directory / soak.TICKS).read_text().splitlines()[-1])["passed"])

    def test_an_existing_attempt_is_never_overwritten(self):
        self.begin()
        before = (self.directory / soak.START).read_bytes()
        with self.assertRaises(FileExistsError):
            self.begin()
        self.assertEqual((self.directory / soak.START).read_bytes(), before)

    def test_an_existing_attempt_is_refused_before_any_observation_can_replace_evidence(self):
        self.begin()
        with mock.patch.object(self.harness.background, "check", side_effect=AssertionError("overwrote first observation")):
            with self.assertRaises(FileExistsError):
                self.begin()

    def test_both_clocks_reject_pause_overnight_and_backward_gaps(self):
        state = self.begin()
        self.tick(state)
        for field, change in (("wall", 64800), ("monotonic", 64800), ("wall", -1), ("monotonic", -1)):
            with self.subTest(field=field, change=change):
                original = getattr(self.clock, field)
                setattr(self.clock, field, original + change)
                with self.assertRaisesRegex(RuntimeError, "clock|gap"):
                    soak.begin_tick(copy.deepcopy(state))
                setattr(self.clock, field, original)

    def test_missing_real_actions_cannot_be_recorded_as_success(self):
        state = self.begin()
        self.fill_changes = False  # A mock/native helper claiming filled is not readback.
        with self.assertRaisesRegex(TimeoutError, "persist"):
            self.tick(state)
        rows = [json.loads(line) for line in (self.directory / soak.TICKS).read_text().splitlines()]
        self.assertFalse(rows[-1]["passed"])
        self.assertIn("persist", rows[-1]["error"])
        with self.assertRaisesRegex(RuntimeError, "failed"):
            soak.finish(self.context, self.harness, state, {})

    def test_failed_action_ack_restores_the_original_draft_and_preserves_failure(self):
        state = self.begin()
        self.fill_ack = False
        with self.assertRaisesRegex(RuntimeError, "native|acknowledge"):
            self.tick(state)
        self.assertEqual(self.draft, self.expected)
        self.assertFalse(json.loads((self.directory / soak.TICKS).read_text().splitlines()[-1])["passed"])

    def test_a_failed_restore_cannot_hide_the_original_action_error(self):
        state = self.begin()
        original = self.harness.native
        def fail_restore(context, action="snapshot", value=""):
            if value == self.expected["text"]:
                raise OSError("restore unavailable")
            return dict(original(context, action, value), filled=False)
        self.harness.native = fail_restore
        with self.assertRaisesRegex(RuntimeError, "acknowledged"):
            self.tick(state)
        receipt = json.loads((self.directory / soak.TICKS).read_text().splitlines()[-1])
        self.assertIn("acknowledged", receipt["error"])
        self.assertIn("restore unavailable", receipt["restore_error"])

    def test_original_worker_or_engine_change_refuses_the_next_tick(self):
        state = self.begin()
        self.engine_pid = 101
        with self.assertRaisesRegex(RuntimeError, "engine"):
            self.tick(state)
        self.assertEqual(self.actions, [])

    def test_a_new_live_worker_cannot_replace_the_original_between_ticks(self):
        state = self.begin()
        self.background["parked"]["worker_id"] = "replacement-worker"
        with self.assertRaisesRegex(RuntimeError, "worker identity changed"):
            self.tick(state)
        self.assertEqual(self.actions, [])

    def test_successful_ui_actions_cannot_count_a_tick_beyond_its_action_budget(self):
        state = self.begin()
        original = self.harness.native
        def delayed(context, action="snapshot", value=""):
            self.clock.advance(200)
            return original(context, action, value)
        self.harness.native = delayed
        with self.assertRaisesRegex(TimeoutError, "budget"):
            self.tick(state)
        self.assertEqual(self.draft, self.expected)
        self.assertFalse(json.loads((self.directory / soak.TICKS).read_text().splitlines()[-1])["passed"])

    def test_a_replaced_worker_is_not_frozen_as_the_original_at_start(self):
        self.background["running"]["worker_id"] = "replacement"
        with self.assertRaisesRegex(RuntimeError, "worker|identity"):
            self.begin()

    def test_only_one_pending_tick_can_own_a_write(self):
        state = self.begin()
        token = soak.begin_tick(state)
        with self.assertRaisesRegex(RuntimeError, "pending"):
            soak.begin_tick(state)
        with self.assertRaisesRegex(RuntimeError, "tick|token"):
            soak.record_tick(self.context, self.harness, state, dict(token))

    def test_summary_elapsed_cannot_accept_a_missing_or_single_final_tick(self):
        state = self.begin()
        self.tick(state)
        result = dict(case_id=self.context["case_id"], phase="soak", engine_pid_before=100, engine_pid_after=100,
                      background=copy.deepcopy(self.background), soak_elapsed_seconds=64800)
        with self.assertRaisesRegex(RuntimeError, "duration|tick|summary"):
            soak.accept_evidence(self.directory, result, self.phase, self.budget)

    def test_acceptance_independently_rejects_durable_tampering(self):
        _, result = self.complete()
        path = self.directory / soak.TICKS
        raw = path.read_text()
        original = [json.loads(line) for line in raw.splitlines()]
        mutations = [
            lambda rows: rows.pop(1),
            lambda rows: rows[2].update(engine_pid=101),
            lambda rows: rows[2]["background"]["parked"].update(worker_id="other"),
            lambda rows: rows[2].update(started_at=rows[2]["started_at"] + 64800),
            lambda rows: rows[2].update(edited_sha256=rows[2]["draft_sha256"]),
            lambda rows: rows[2].update(edited_sha256=rows[1]["edited_sha256"]),
            lambda rows: rows[2]["ui"]["restore"].update(filled=False),
            lambda rows: rows[2].update(passed=False),
        ]
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                rows = copy.deepcopy(original)
                mutate(rows)
                path.write_text("".join(json.dumps(row) + "\n" for row in rows))
                forged = copy.deepcopy(result)
                forged["soak_evidence"]["ledger_sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
                (self.directory / soak.SUMMARY).write_text(json.dumps(forged["soak_evidence"]) + "\n")
                with self.assertRaises(RuntimeError):
                    soak.accept_evidence(self.directory, forged, self.phase, self.budget)
        path.write_text(raw)
        (self.directory / soak.SUMMARY).write_text(json.dumps(result["soak_evidence"]) + "\n")
        result["soak_elapsed_seconds"] += 64800
        with self.assertRaisesRegex(RuntimeError, "summary"):
            soak.accept_evidence(self.directory, result, self.phase, self.budget)

    def test_a_nonregular_ledger_refuses_without_waiting_for_a_writer(self):
        self.begin()
        path = self.directory / soak.TICKS
        path.unlink()
        os.mkfifo(path)
        errors = []
        def accept():
            try:
                soak.accept_evidence(self.directory, {}, self.phase, self.budget)
            except RuntimeError as error:
                errors.append(error)
        worker = threading.Thread(target=accept, daemon=True)
        worker.start()
        worker.join(.2)
        prompt = not worker.is_alive()
        if not prompt:
            descriptor = os.open(path, os.O_WRONLY | os.O_NONBLOCK)
            os.close(descriptor)
            worker.join(1)
        self.assertTrue(prompt, "invalid ledger waited for another process instead of refusing")
        self.assertEqual(len(errors), 1)

    def test_sampling_bounds_are_derived_from_the_frozen_budget(self):
        for change in (dict(duration_seconds=1), dict(interval_seconds=181), dict(interval_seconds=float("nan"))):
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, "soak|sampling|finite"):
                soak.start(self.context, self.harness, dict(self.phase, **change), self.budget)


if __name__ == "__main__":
    unittest.main()
