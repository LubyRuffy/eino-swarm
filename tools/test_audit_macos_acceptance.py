"""A locked host must not hide an available native Mac acceptance path."""
import copy
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("audit_macos_acceptance.py")


def observation():
    return {
        "candidate": {
            "version": "1.2.3", "source_sha": "a" * 40,
            "asset_name": "zwai-1.2.3-darwin-arm64.zip",
            "asset_sha256": "b" * 64,
        },
        "host_locked": True,
        "guest": {
            "target": "test-macOS", "state": "locked",
            "existing_credentials_available": True,
        },
    }


class MacAcceptanceTests(unittest.TestCase):
    def decide(self, row):
        return runpy.run_path(str(SCRIPT))["decide"](row)

    def unlock(self, row, result, evidence=None):
        row["guest"]["unlock"] = {
            "target": row["guest"]["target"], "result": result,
            "evidence": evidence if evidence is not None else ["private/login-observation.json"],
        }

    def window(self, row):
        row["native_window"] = {
            "guest_target": row["guest"]["target"],
            "candidate": copy.deepcopy(row["candidate"]),
            "observed": True, "behavior_verified": True,
            "evidence": ["private/native-window.png", "private/behavior-receipt.json"],
        }

    def trusted_terminal(self, row):
        self.window(row)
        row["native_window"].update(
            observed=False, behavior_verified=False,
            ssh_helper_permission_denied=True, trusted_guest_terminal_available=True,
            evidence=["private/current-native-control.json"],
        )

    def test_ssh_helper_denial_uses_an_evidenced_trusted_guest_terminal(self):
        row = observation()
        row["guest"]["state"] = "ready"
        self.trusted_terminal(row)
        before = copy.deepcopy(row)
        result = self.decide(row)
        self.assertEqual(result["status"], "pending")
        self.assertEqual(result["next_action"], "inspect_native_window_via_guest_terminal")
        self.assertEqual(result["reason"], "trusted_guest_terminal_control_available")
        self.assertEqual(result["evidence_refs"], row["native_window"]["evidence"])
        self.assertEqual(row, before)

    def test_terminal_fallback_requires_both_current_control_flags(self):
        for denied in (True, False, None):
            for available in (True, False, None):
                if denied is True and available is True:
                    continue
                with self.subTest(denied=denied, available=available):
                    row = observation()
                    row["guest"]["state"] = "ready"
                    self.trusted_terminal(row)
                    for field, value in (("ssh_helper_permission_denied", denied),
                                         ("trusted_guest_terminal_available", available)):
                        if value is None:
                            del row["native_window"][field]
                        else:
                            row["native_window"][field] = value
                    self.assertEqual(self.decide(row)["next_action"], "inspect_native_window")

    def test_terminal_fallback_requires_matching_guest_candidate_and_evidence(self):
        for field, value in (
            ("guest_target", "another-macOS"), ("evidence", []),
            ("version", "1.2.4"), ("source_sha", "c" * 40),
            ("asset_name", "another-package.zip"), ("asset_sha256", "d" * 64),
        ):
            with self.subTest(field=field):
                row = observation()
                row["guest"]["state"] = "ready"
                self.trusted_terminal(row)
                target = row["native_window"]
                if field in row["candidate"]:
                    target = target["candidate"]
                target[field] = value
                self.assertEqual(self.decide(row)["next_action"], "inspect_native_window")

    def test_terminal_control_evidence_cannot_replace_current_guest_observation(self):
        for state, action in (("unknown", "observe_guest_session"), ("locked", "unlock_guest")):
            with self.subTest(state=state):
                row = observation()
                row["guest"]["state"] = state
                self.trusted_terminal(row)
                self.assertEqual(self.decide(row)["next_action"], action)

    def test_completed_acceptance_precedes_terminal_control_recovery(self):
        for state in ("ready", "unknown", "locked"):
            with self.subTest(state=state):
                row = observation()
                row["guest"]["state"] = state
                self.trusted_terminal(row)
                row["native_window"].update(
                    observed=True, behavior_verified=True,
                    evidence=["private/native-window.png", "private/behavior-receipt.json"],
                )
                result = self.decide(row)
                self.assertEqual(result["status"], "passed")
                self.assertEqual(result["next_action"], "record_acceptance")

    def test_terminal_control_flags_reject_non_boolean_claims(self):
        for field in ("ssh_helper_permission_denied", "trusted_guest_terminal_available"):
            for value in (1, "true", None):
                with self.subTest(field=field, value=value):
                    row = observation()
                    self.trusted_terminal(row)
                    row["native_window"][field] = value
                    with self.assertRaises(ValueError):
                        self.decide(row)

    def test_locked_guest_with_authorized_account_returns_unlock_guest(self):
        result = self.decide(observation())
        self.assertEqual(result["status"], "pending")
        self.assertEqual(result["next_action"], "unlock_guest")

    def test_explicit_not_attempted_is_still_unlockable(self):
        row = observation()
        self.unlock(row, "not_attempted", [])
        self.assertEqual(self.decide(row)["next_action"], "unlock_guest")

    def test_host_lock_does_not_block_ready_guest(self):
        for host_locked in (True, False, None):
            with self.subTest(host_locked=host_locked):
                row = observation()
                row["guest"]["state"] = "ready"
                if host_locked is None:
                    del row["host_locked"]
                else:
                    row["host_locked"] = host_locked
                result = self.decide(row)
                self.assertEqual(result["status"], "pending")
                self.assertEqual(result["next_action"], "inspect_native_window")

    def test_successful_unlock_reaches_window_inspection(self):
        row = observation()
        self.unlock(row, "succeeded")
        self.assertEqual(self.decide(row)["next_action"], "inspect_native_window")

    def test_unlock_success_without_evidence_needs_observation(self):
        row = observation()
        self.unlock(row, "succeeded", [])
        self.assertEqual(self.decide(row)["next_action"], "collect_unlock_evidence")

    def test_real_guest_unlock_failure_is_blocked(self):
        row = observation()
        self.unlock(row, "failed")
        result = self.decide(row)
        self.assertEqual(result["status"], "blocked")
        self.assertEqual(result["reason"], "guest_unlock_failed")
        self.assertEqual(result["next_action"], "resolve_guest_unlock_failure")

    def test_failure_claim_without_evidence_is_not_a_blocker(self):
        row = observation()
        self.unlock(row, "failed", [])
        result = self.decide(row)
        self.assertEqual(result["status"], "pending")
        self.assertEqual(result["next_action"], "collect_unlock_evidence")

    def test_missing_existing_account_is_not_an_observed_unlock_failure(self):
        row = observation()
        row["guest"]["existing_credentials_available"] = False
        self.assertEqual(self.decide(row)["next_action"], "locate_authorized_credentials")

    def test_unknown_guest_state_requires_fresh_observation(self):
        for host_locked in (True, False, None):
            for previous_unlock in (None, "succeeded", "failed"):
                with self.subTest(host_locked=host_locked, previous_unlock=previous_unlock):
                    row = observation()
                    row["guest"]["state"] = "unknown"
                    if host_locked is None:
                        del row["host_locked"]
                    else:
                        row["host_locked"] = host_locked
                    if previous_unlock is not None:
                        self.unlock(row, previous_unlock, ["private/previous-session-login.json"])
                    result = self.decide(row)
                    self.assertEqual(result["status"], "pending")
                    self.assertEqual(result["next_action"], "observe_guest_session")
                    self.assertEqual(result["reason"], "guest_state_unknown")
                    self.assertEqual(result["evidence_refs"], [])

    def test_resumed_acceptance_reobserves_the_guest_but_retains_exact_window_evidence(self):
        # Old login evidence remains history after resumption, not the current
        # session state. A completed exact-package acceptance is durable evidence.
        row = observation()
        self.unlock(row, "failed", ["private/previous-session-login.json"])
        self.assertEqual(self.decide(row)["status"], "blocked")
        row["host_locked"] = False
        row["guest"]["state"] = "unknown"
        self.assertEqual(self.decide(row)["next_action"], "observe_guest_session")
        row["guest"]["state"] = "ready"
        self.assertEqual(self.decide(row)["next_action"], "inspect_native_window")
        self.window(row)
        self.assertEqual(self.decide(row)["status"], "passed")
        row["guest"]["state"] = "unknown"
        row["host_locked"] = True
        self.assertEqual(self.decide(row)["status"], "passed")

    def test_ssh_and_process_success_do_not_accept_a_native_window(self):
        row = observation()
        row["guest"].update(state="ready", ssh_ok=True, process_started=True)
        self.assertEqual(self.decide(row)["status"], "pending")
        self.assertEqual(self.decide(row)["next_action"], "inspect_native_window")

    def test_exact_candidate_and_behavior_window_evidence_pass(self):
        row = observation()
        self.window(row)
        self.assertEqual(self.decide(row)["status"], "passed")

    def test_window_observation_alone_does_not_verify_behavior(self):
        row = observation()
        row["guest"]["state"] = "ready"
        self.window(row)
        row["native_window"]["behavior_verified"] = False
        self.assertEqual(self.decide(row)["status"], "pending")

    def test_window_receipt_needs_real_observation_and_evidence(self):
        for field, value in (("observed", False), ("evidence", [])):
            with self.subTest(field=field):
                row = observation()
                row["guest"]["state"] = "ready"
                self.window(row)
                row["native_window"][field] = value
                self.assertEqual(self.decide(row)["status"], "pending")

    def test_every_candidate_identity_field_must_match(self):
        for field, value in (
            ("version", "1.2.4"), ("source_sha", "c" * 40),
            ("asset_name", "zwai-1.2.3-darwin-amd64.zip"),
            ("asset_sha256", "d" * 64),
        ):
            with self.subTest(field=field):
                row = observation()
                row["guest"]["state"] = "ready"
                self.window(row)
                row["native_window"]["candidate"][field] = value
                self.assertEqual(self.decide(row)["status"], "pending")

    def test_other_guest_window_cannot_accept_this_guest(self):
        row = observation()
        row["guest"]["state"] = "ready"
        self.window(row)
        row["native_window"]["guest_target"] = "another-macOS"
        self.assertEqual(self.decide(row)["status"], "pending")

    def test_unlock_result_must_belong_to_target_guest(self):
        row = observation()
        self.unlock(row, "failed")
        row["guest"]["unlock"]["target"] = "another-macOS"
        with self.assertRaises(ValueError):
            self.decide(row)

    def test_credential_fields_and_non_boolean_claims_are_rejected(self):
        for change in (
            lambda row: row.update(password="do-not-print"),
            lambda row: row["guest"].update(password="do-not-print"),
            lambda row: row["guest"].update(existing_credentials_available=1),
            lambda row: row.update(host_locked="true"),
        ):
            with self.subTest(change=change):
                row = observation()
                change(row)
                with self.assertRaises(ValueError) as error:
                    self.decide(row)
                self.assertNotIn("do-not-print", str(error.exception))

    def test_candidate_requires_full_hashes_and_safe_zip_basename(self):
        for field, value in (("source_sha", "a" * 7), ("asset_sha256", "b" * 7),
                             ("version", "latest"), ("asset_name", "../app.zip")):
            with self.subTest(field=field):
                row = observation()
                row["candidate"][field] = value
                with self.assertRaises(ValueError):
                    self.decide(row)

    def test_decision_does_not_mutate_observation(self):
        row = observation()
        before = copy.deepcopy(row)
        self.decide(row)
        self.assertEqual(row, before)

    def cli(self, contents):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "observation.json"
            path.write_text(contents)
            return subprocess.run([sys.executable, str(SCRIPT), "--observation", str(path)],
                                  text=True, capture_output=True, check=False)

    def test_cli_returns_json_without_operating_the_guest(self):
        result = self.cli(json.dumps(observation()))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["next_action"], "unlock_guest")

    def test_cli_returns_trusted_terminal_action_without_accepting_a_window(self):
        row = observation()
        row["guest"]["state"] = "ready"
        self.trusted_terminal(row)
        result = self.cli(json.dumps(row))
        self.assertEqual(result.returncode, 0, result.stderr)
        decision = json.loads(result.stdout)
        self.assertEqual(decision["status"], "pending")
        self.assertEqual(decision["next_action"], "inspect_native_window_via_guest_terminal")

    def test_cli_unknown_guest_ignores_a_historical_login_failure(self):
        row = observation()
        row["guest"]["state"] = "unknown"
        self.unlock(row, "failed", ["private/previous-session-login.json"])
        result = self.cli(json.dumps(row))
        self.assertEqual(result.returncode, 0, result.stderr)
        decision = json.loads(result.stdout)
        self.assertEqual(decision["status"], "pending")
        self.assertEqual(decision["next_action"], "observe_guest_session")
        self.assertEqual(decision["evidence_refs"], [])

    def test_cli_invalid_input_does_not_echo_private_values(self):
        for contents in ('{"password":"do-not-print"}', '{"private":"do-not-print",'):
            with self.subTest(contents=contents):
                result = self.cli(contents)
                self.assertEqual(result.returncode, 2)
                self.assertNotIn("do-not-print", result.stdout + result.stderr)
                self.assertEqual(json.loads(result.stdout)["status"], "invalid_observation")


if __name__ == "__main__":
    unittest.main()
