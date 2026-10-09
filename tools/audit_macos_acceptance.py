#!/usr/bin/env python3
"""Decide native Mac acceptance recovery from observations; never operate a VM."""
import argparse
import json
from pathlib import Path
import re


IDENTITY_FIELDS = {"version", "source_sha", "asset_name", "asset_sha256"}


def _object(value, required, optional=()):
    if not isinstance(value, dict) or not required <= value.keys():
        raise ValueError("Observation is missing required fields")
    if value.keys() - required - set(optional):
        # Do not echo unknown fields or values: an accidental credential is private.
        raise ValueError("Observation contains unsupported fields")
    return value


def _boolean(value):
    if type(value) is not bool:
        raise ValueError("Observation flags must be booleans")


def _text(value):
    if not isinstance(value, str) or not value.strip():
        raise ValueError("Observation references must be nonempty strings")


def _evidence(value):
    if not isinstance(value, list):
        raise ValueError("Evidence must be a list of private references")
    for reference in value:
        _text(reference)


def _candidate(value):
    _object(value, IDENTITY_FIELDS)
    for field in IDENTITY_FIELDS:
        _text(value[field])
    if not re.fullmatch(r"\d+\.\d+\.\d+", value["version"]):
        raise ValueError("Candidate version must be numeric x.y.z")
    if not re.fullmatch(r"[0-9a-f]{40}", value["source_sha"]):
        raise ValueError("Candidate source SHA must be a full lowercase Git SHA")
    if not re.fullmatch(r"[0-9a-f]{64}", value["asset_sha256"]):
        raise ValueError("Candidate asset hash must be a full lowercase SHA-256")
    name = value["asset_name"]
    if Path(name).name != name or "\\" in name or not name.endswith(".zip"):
        raise ValueError("Candidate asset must be a ZIP basename")


def _validate(row):
    _object(row, {"candidate", "guest"}, {"host_locked", "native_window"})
    _candidate(row["candidate"])
    if "host_locked" in row:
        _boolean(row["host_locked"])
    guest = _object(row["guest"], {"target", "state", "existing_credentials_available"},
                    {"unlock", "ssh_ok", "process_started"})
    _text(guest["target"])
    if guest["state"] not in ("locked", "ready", "unknown"):
        raise ValueError("Guest state must be locked, ready or unknown")
    _boolean(guest["existing_credentials_available"])
    for flag in ("ssh_ok", "process_started"):
        if flag in guest:
            _boolean(guest[flag])
    if "unlock" in guest:
        unlock = _object(guest["unlock"], {"target", "result", "evidence"})
        if unlock["target"] != guest["target"]:
            raise ValueError("Unlock observation must belong to the target guest")
        if unlock["result"] not in ("not_attempted", "succeeded", "failed"):
            raise ValueError("Unlock result must be not_attempted, succeeded or failed")
        _evidence(unlock["evidence"])
    if "native_window" in row:
        window = _object(row["native_window"],
                         {"guest_target", "candidate", "observed", "behavior_verified", "evidence"})
        _text(window["guest_target"])
        _candidate(window["candidate"])
        _boolean(window["observed"])
        _boolean(window["behavior_verified"])
        _evidence(window["evidence"])


def decide(row):
    """Return the next action, keeping native evidence separate from transport."""
    _validate(row)
    guest = row["guest"]

    def result(status, action, reason, evidence=()):
        return {"status": status, "next_action": action, "reason": reason,
                "guest_target": guest["target"], "candidate": dict(row["candidate"]),
                "evidence_refs": list(evidence)}

    window = row.get("native_window")
    if window and window["observed"] and window["behavior_verified"] and window["evidence"] \
            and window["guest_target"] == guest["target"] \
            and window["candidate"] == row["candidate"]:
        # A later lock does not invalidate an already accepted exact package.
        return result("passed", "record_acceptance", "exact_native_window_verified", window["evidence"])

    # Host locks and SSH/process success do not describe the guest's native screen.
    if guest["state"] == "ready":
        return result("pending", "inspect_native_window", "native_window_evidence_required")
    unlock = guest.get("unlock", {})
    outcome = unlock.get("result", "not_attempted")
    if outcome in ("succeeded", "failed"):
        if not unlock["evidence"]:
            return result("pending", "collect_unlock_evidence", "unlock_result_unverified")
        if outcome == "succeeded":
            return result("pending", "inspect_native_window", "guest_unlocked_native_evidence_required")
        return result("blocked", "resolve_guest_unlock_failure", "guest_unlock_failed", unlock["evidence"])
    if guest["state"] == "unknown":
        return result("pending", "observe_guest_session", "guest_state_unknown")
    if guest["existing_credentials_available"]:
        return result("pending", "unlock_guest", "authorized_guest_login_not_attempted")
    return result("pending", "locate_authorized_credentials", "existing_authorized_account_needed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--observation", required=True, type=Path,
                        help="JSON observation file containing no credentials")
    args = parser.parse_args()
    try:
        decision = decide(json.loads(args.observation.read_text(encoding="utf-8")))
    except (OSError, UnicodeError, ValueError):
        print(json.dumps({"status": "invalid_observation", "next_action": "correct_observation",
                          "reason": "invalid_or_unreadable_observation"}))
        return 2
    print(json.dumps(decision, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
