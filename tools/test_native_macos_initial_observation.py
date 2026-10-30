"""Initial observation recovery through actual polling, command IO and parsing."""
from contextlib import ExitStack, contextmanager
import json
import subprocess
import unittest
from unittest import mock

import test_native_macos_smoke as fixture

smoke = fixture.smoke


class InitialObservationTests(unittest.TestCase):
    setUp = fixture.SmokeTests.setUp
    native_row = fixture.SmokeTests.native_row

    @staticmethod
    def incomplete(**changes):
        return dict(ax_read_failed=True, ax_error=-25204,
                    ax_attribute="AXRole", ax_read_reason="api_error", **changes)

    @contextmanager
    def boundary(self, responses, *, seconds=1, writer=None):
        context = dict(self.context, ax=self.root / "formal-helper")
        origin = "http://127.0.0.1:4567"
        clock = mock.Mock(return_value=10)
        deadline = smoke.Deadline(seconds, clock=clock)
        sequence = iter(responses) if not callable(responses) else None

        def native_call(argv, **kwargs):
            value = responses(argv) if callable(responses) else next(sequence)
            clock.return_value += .02
            if isinstance(value, BaseException):
                raise value
            code, body = value
            return subprocess.CompletedProcess(argv, code,
                body if isinstance(body, str) else json.dumps(body), "complete stderr")

        with ExitStack() as stack:
            stack.enter_context(mock.patch.object(smoke, "wait_engine", return_value=(origin, {"pid": 456})))
            stack.enter_context(mock.patch.object(smoke, "assert_process", return_value=context["app"] / "Contents/MacOS/zwai"))
            stack.enter_context(mock.patch.object(smoke, "validate_helper", return_value={"binary_sha256": "prepared"}))
            run = stack.enter_context(mock.patch.object(smoke.subprocess, "run", side_effect=native_call))
            pause = stack.enter_context(mock.patch.object(smoke.time, "sleep", side_effect=lambda value: setattr(clock, "return_value", clock.return_value + value)))
            pixels = stack.enter_context(mock.patch.object(smoke, "capture_window", return_value={"content_visible": True}))
            if writer:
                stack.enter_context(mock.patch.object(smoke.readiness, "save_json", side_effect=writer))
            yield context, deadline, run, pause, pixels

    def test_continuous_incomplete_reads_use_original_deadline_and_keep_first_failure(self):
        with self.boundary(lambda argv: (2, self.incomplete()), seconds=.25) as (context, deadline, run, pause, pixels):
            end = deadline.ends
            with self.assertRaises(TimeoutError) as caught:
                smoke.wait_initial_ready(context, deadline)
            self.assertIsInstance(caught.exception.__cause__, smoke.readiness.Failure)
            self.assertEqual(deadline.ends, end)
            self.assertLessEqual(run.call_count, 3)
            self.assertTrue(all(c.args[0][2] == "snapshot" for c in run.call_args_list))
            pixels.assert_not_called()
            first = json.loads((context["evidence"] / "smoke-native-call-0001.json").read_text())
            self.assertEqual(first["returncode"], 2)
            self.assertEqual(json.loads(first["stdout"])["ax_error"], -25204)

    def test_deadline_expiry_does_not_start_another_identity_probe(self):
        with self.boundary(lambda argv: (2, self.incomplete()), seconds=.2) as (context, deadline, run, pause, pixels):
            with mock.patch.object(smoke, "assert_process", wraps=smoke.assert_process) as identity, \
                    self.assertRaises(TimeoutError):
                smoke.wait_initial_ready(context, deadline)
            self.assertEqual(identity.call_count, run.call_count)
            pixels.assert_not_called()

    def test_other_errors_and_contradictory_or_malformed_observations_do_not_poll(self):
        cases = [dict(error="identity refused"), "not JSON",
                 dict(self.incomplete(), ax_error=-25202),
                 dict(self.incomplete(), ax_attribute="AXChildren"),
                 dict(self.incomplete(), ax_read_reason="wrong_type"),
                 dict(self.incomplete(), retryable=False),
                 dict(self.incomplete(), retryable=0),
                 dict(self.incomplete(), truncated=True),
                 dict(self.incomplete(), ready=True),
                 dict(self.incomplete(), ready=1),
                 dict(self.incomplete(), ready=None),
                 dict(self.incomplete(), press_attempted=True)]
        for body in cases:
            with self.subTest(body=body), self.boundary([(2, body)]) as (context, deadline, run, pause, pixels):
                with self.assertRaises(smoke.readiness.Failure):
                    smoke.wait_initial_ready(context, deadline)
                self.assertEqual(run.call_count, 1)
                pause.assert_not_called()
                pixels.assert_not_called()

    def test_current_completed_call_must_match_all_durable_evidence(self):
        save = smoke.readiness.save_json
        for field, value in [("started_wall", -1), ("app_pid", 999), ("streams", {}),
                             ("requested_origin", "http://127.0.0.1:999"), ("returncode", 0)]:
            def corrupt(path, row):
                if path.name.startswith("smoke-native-call-"):
                    row = dict(row, **{field: value})
                save(path, row)
            with self.subTest(field=field), self.boundary([(2, self.incomplete())], writer=corrupt) as (context, deadline, run, pause, pixels):
                with self.assertRaises(smoke.readiness.Failure):
                    smoke.wait_initial_ready(context, deadline)
                self.assertEqual(run.call_count, 1)
                pause.assert_not_called()
                pixels.assert_not_called()

    def test_evidence_io_failure_is_not_reclassified_as_transient_ax(self):
        save = smoke.readiness.save_json
        def fail_one_destination(path, row):
            if path.name.startswith("smoke-native-call-"):
                raise OSError("private evidence unavailable")
            save(path, row)
        with self.boundary([(2, self.incomplete())], writer=fail_one_destination) as (context, deadline, run, pause, pixels):
            with self.assertRaises(smoke.readiness.Failure) as caught:
                smoke.wait_initial_ready(context, deadline)
            self.assertEqual(caught.exception.failure_kind, "native_evidence_failed")
            self.assertEqual(run.call_count, 1)
            pause.assert_not_called()
            self.assertEqual(json.loads((context["evidence"] / "smoke-native-last.json").read_text())["returncode"], 2)

    def test_fresh_duplicate_controls_cannot_be_hidden_by_the_discarded_graph(self):
        duplicate = dict(self.native_row(), ready=True, textareas=2, send_count=2)
        with self.boundary([(2, self.incomplete()), (0, duplicate)]) as (context, deadline, run, pause, pixels):
            with self.assertRaises(smoke.readiness.Failure) as caught:
                smoke.wait_initial_ready(context, deadline)
            self.assertEqual(caught.exception.failure_kind, "native_window_refused")
            self.assertEqual(run.call_count, 2)
            pixels.assert_not_called()

    def test_fresh_wrong_identity_is_an_immediate_refusal(self):
        for wrong in (dict(self.native_row(), ready=True, pid=999),
                      dict(self.native_row(), ready=True, bundle=str(self.root / "other.app"))):
            with self.subTest(wrong=wrong), self.boundary([(2, self.incomplete()), (0, wrong)]) as (context, deadline, run, pause, pixels):
                with self.assertRaises(smoke.readiness.Failure):
                    smoke.wait_initial_ready(context, deadline)
                self.assertEqual(run.call_count, 2)
                pixels.assert_not_called()

    def test_input_failures_and_noninitial_or_result_snapshots_are_never_reacquired(self):
        for action in ("fill", "send", "snapshot"):
            with self.subTest(action=action), self.boundary([(2, self.incomplete())]) as (context, deadline, run, pause, pixels):
                context["origin"] = "http://127.0.0.1:4567"
                with self.assertRaises(smoke.readiness.Failure):
                    if action == "snapshot":
                        smoke.wait_window(context, deadline, expected="expected result")
                    else:
                        smoke.native_snapshot(context, deadline, action=action, message="exact draft")
                self.assertEqual(run.call_count, 1)
                pause.assert_not_called()
                if action != "snapshot":
                    with self.assertRaisesRegex(ValueError, "precede native input"):
                        smoke.wait_initial_ready(context, deadline)
                    self.assertEqual(run.call_count, 1)
        with self.boundary([]) as (context, deadline, run, pause, pixels):
            with self.assertRaises(ValueError):
                smoke.wait_window(context, deadline, expected="result", initial=True)
            run.assert_not_called()

    def test_inventory_cancellation_permission_and_timeout_never_reacquire(self):
        for error in (PermissionError("denied"), OSError("inventory unknown"),
                      KeyboardInterrupt("cancelled"), subprocess.TimeoutExpired(["native-helper"], 8)):
            with self.subTest(error=type(error).__name__), self.boundary([error]) as (context, deadline, run, pause, pixels):
                with self.assertRaises(type(error)) as caught:
                    smoke.wait_initial_ready(context, deadline)
                self.assertIs(caught.exception, error)
                self.assertEqual(run.call_count, 1)
                pause.assert_not_called()


if __name__ == "__main__":
    unittest.main()
