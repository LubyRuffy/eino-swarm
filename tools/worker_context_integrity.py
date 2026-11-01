#!/usr/bin/env python3
"""Integrity-only audit of a completed fixed-worker context-reference trial.

No model calls, oracle interpretation, source-map reads, answer grading, or
A/B benefit calculation. Frozen files are hashed as opaque bytes. Content
inspection uses only the frozen model fixtures and recorded runtime artifacts.
"""

from __future__ import annotations

import argparse
from collections import Counter, defaultdict
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys


class AuditError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise AuditError(message)


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate JSON object key")
        value[key] = item
    return value


def invalid_constant(_value):
    raise AuditError("non-finite JSON number")


def decode(text):
    try:
        return json.loads(text, object_pairs_hook=unique_object, parse_constant=invalid_constant)
    except (ValueError, TypeError) as error:
        raise AuditError("malformed or ambiguous JSON") from error


def read_json(path, private=False):
    require(path.is_file() and not path.is_symlink(), "required regular artifact is missing")
    if private:
        require(stat.S_IMODE(path.stat().st_mode) == 0o600, "private artifact mode is not 0600")
    return decode(path.read_text(encoding="utf-8"))


def digest(path):
    require(path.is_file() and not path.is_symlink(), "frozen regular file is missing")
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            value.update(block)
    return value.hexdigest()


def go_string_array(result, error=""):
    """Match encoding/json.Marshal([2]string{result,error}), including HTML/JS escapes."""
    text = json.dumps([result, error], ensure_ascii=False, separators=(",", ":"))
    for character, escaped in (("<", r"\u003c"), (">", r"\u003e"), ("&", r"\u0026"),
                               ("\u2028", r"\u2028"), ("\u2029", r"\u2029")):
        text = text.replace(character, escaped)
    return text.encode("utf-8")


def result_hash(result, error=""):
    return hashlib.sha256(go_string_array(result, error)).hexdigest()


def equal(left, right):
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(equal(left[k], right[k]) for k in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(equal(a, b) for a, b in zip(left, right))
    return left == right


def integer(value, minimum=0):
    return type(value) is int and value >= minimum


def identity(row):
    require(isinstance(row, dict), "invalid session row")
    case, arm, repeat = row.get("case"), row.get("arm"), row.get("repeat")
    require(isinstance(case, str) and re.fullmatch(r"[a-z0-9][a-z0-9-]*", case), "unsafe case identity")
    require(arm in ("a", "b") and integer(repeat, 1), "invalid arm/repetition identity")
    return case, arm, repeat


def bindings(messages):
    calls, results = defaultdict(list), defaultdict(list)
    for index, message in enumerate(messages):
        require(isinstance(message, dict), "model message is not an object")
        if message.get("role") == "assistant":
            for call in message.get("tool_calls") or []:
                require(isinstance(call, dict) and isinstance(call.get("id"), str), "bad tool call identity")
                calls[call["id"]].append((index, call.get("function", {}).get("name")))
        if message.get("role") == "tool":
            results[message.get("tool_call_id")].append(index)
    return calls, results


def unique_wait(messages, call_id, before=None):
    calls, results = bindings(messages)
    require(isinstance(call_id, str) and bool(call_id), "reference lacks a call identity")
    require(len(calls[call_id]) == len(results[call_id]) == 1, "ambiguous reference call/result binding")
    call_index, tool = calls[call_id][0]
    result_index = results[call_id][0]
    require(tool == "wait_agents" and call_index < result_index, "reference is not a real earlier wait binding")
    require(before is None or result_index < before, "reference does not point into the current earlier context")
    require(messages[result_index].get("tool_name", "wait_agents") == "wait_agents", "wait tool name mismatch")
    return result_index


def native_report(text):
    document = decode(text)
    require(isinstance(document, dict) and "result_ref" not in document, "native wait is not a full report")
    entries = document.get("agents")
    if entries is None and isinstance(document.get("error"), str):
        return document, []
    require(isinstance(entries, list), "native wait lacks its agents list")
    for entry in entries:
        require(isinstance(entry, dict) and "result_ref" not in entry, "native output/history contains a context reference")
    return document, entries


def unique_workers(entries):
    seen = set()
    for entry in entries:
        worker, generation = entry.get("agent_id"), entry.get("generation")
        if not isinstance(worker, str) or not worker or not integer(generation, 1):
            continue
        identity = worker, generation
        if identity in seen:
            return False
        seen.add(identity)
    return True


def validate_request(item, arm):
    original, forwarded = item.get("original"), item.get("forwarded")
    require(isinstance(original, list) and isinstance(forwarded, list) and len(original) == len(forwarded), "message count changed")
    changes, changed_messages, recovered_chars = [], 0, 0
    for index, (old, new) in enumerate(zip(original, forwarded)):
        require(isinstance(old, dict) and isinstance(new, dict), "invalid model message")
        require(equal({k: v for k, v in old.items() if k != "content"},
                      {k: v for k, v in new.items() if k != "content"}), "non-content message metadata changed")
        if equal(old.get("content"), new.get("content")):
            continue
        require(arm == "b" and old.get("role") == "tool", "non-treatment message content changed")
        call_id = old.get("tool_call_id")
        require(unique_wait(original, call_id) == index, "rewritten message lacks its real wait binding")
        source_report, old_entries = native_report(old.get("content"))
        require(unique_workers(old_entries), "duplicate worker report must remain full")
        destination = decode(new.get("content"))
        require(isinstance(destination, dict), "forwarded report is not an object")
        new_entries = destination.get("agents")
        require(isinstance(new_entries, list) and len(old_entries) == len(new_entries), "worker count changed")
        require(equal({k: v for k, v in source_report.items() if k != "agents"},
                      {k: v for k, v in destination.items() if k != "agents"}), "report metadata changed")
        changed_messages += 1
        for old_entry, new_entry in zip(old_entries, new_entries):
            if equal(old_entry, new_entry):
                continue
            require(isinstance(new_entry, dict), "rewritten worker is not an object")
            body, worker, generation = old_entry.get("result"), old_entry.get("agent_id"), old_entry.get("generation")
            require(old_entry.get("status") == "done" and integer(generation, 1)
                    and isinstance(body, str) and bool(body) and old_entry.get("error", "") == "", "unsafe terminal result was referenced")
            reference = new_entry.get("result_ref")
            require(isinstance(reference, dict) and set(reference) == {"tool_call_id", "agent_id", "generation", "result_error_sha256"}
                    and "result" not in new_entry, "bad reference envelope")
            require(reference["agent_id"] == worker and type(reference["generation"]) is int
                    and reference["generation"] == generation, "reference worker generation changed")
            require(reference["result_error_sha256"] == result_hash(body, old_entry.get("error", "")), "Go result/error hash mismatch")
            source_index = unique_wait(forwarded, reference["tool_call_id"], before=index)
            # An earlier report may contain references for other workers while
            # preserving this worker's first full body. Resolve only the exact
            # referenced entry; never require the entire report to be ref-free.
            full_report = decode(forwarded[source_index].get("content"))
            require(isinstance(full_report, dict) and isinstance(full_report.get("agents"), list)
                    and all(isinstance(entry, dict) for entry in full_report["agents"]), "bad earlier source report")
            full_entries = full_report["agents"]
            require(unique_workers(full_entries), "duplicate worker report cannot be a full source")
            matches = [entry for entry in full_entries if entry.get("agent_id") == worker and entry.get("generation") == generation]
            require(len(matches) == 1, "earlier full worker identity is not unique")
            full = matches[0]
            require("result_ref" not in full and full.get("status") == "done" and full.get("error", "") == ""
                    and equal(full.get("role"), old_entry.get("role")) and full.get("result") == body,
                    "reference does not resolve to this exact earlier full body")
            expanded = copy.deepcopy(new_entry)
            del expanded["result_ref"]
            expanded["result"] = full["result"]
            require(equal(expanded, old_entry), "reference expansion changed worker metadata/body")
            changes.append({"message_index": index, **reference, "result_chars": len(body)})
            recovered_chars += len(body)
    require(equal(item.get("replacements") or [], changes), "replacement proof metadata differs from observed rewrite")
    return {"messages": len(original), "references": len(changes),
            "changed_tool_messages": changed_messages, "recovered_result_chars": recovered_chars}


def fixture_inputs(manifest):
    root = Path(manifest["inputs"]).resolve()
    # tasks.json is orchestration metadata, used only for case->model-file binding.
    require(str(root / "tasks.json") in manifest["hashes"], "fixture binding metadata is not frozen")
    tasks = read_json(root / "tasks.json")
    selected = {}
    for row in tasks.get("cases", []):
        name = row.get("id")
        require(name not in selected, "duplicate model fixture identity")
        path = (root / row["fixture"]).resolve()
        require(path.is_relative_to(root) and str(path) in manifest["hashes"], "model fixture is outside frozen inputs")
        selected[name] = read_json(path)
    require(set(selected) == set(manifest["case_sources"]), "model fixture cases differ from frozen cases")
    return selected


def receipt_key(call_id, arguments, result, error=""):
    # Tool arguments are opaque call bytes. Native tools may return a handled
    # refusal for malformed JSON, followed by a successful corrected wait.
    require(all(isinstance(value, str) for value in (call_id, arguments, result, error)), "invalid native receipt strings")
    return call_id, hashlib.sha256(arguments.encode()).hexdigest(), hashlib.sha256(result.encode()).hexdigest(), error


def audit_native_history(transcript, receipts, fixture, expected_rounds):
    require(isinstance(transcript, list) and isinstance(receipts, list), "missing native transcript/receipts")
    initial_users = [m["content"] for m in fixture.get("messages", []) if m["role"] == "user"]
    users = initial_users + [r["user"] for r in fixture["rounds"]]
    require([m.get("content") for m in transcript if m.get("role") == "user"] == users, "human round transcript changed")
    workers = {w["agent_id"]: w for w in fixture["workers"]}
    require(len(workers) == len(fixture["workers"]), "duplicate model worker identity")
    coverage = [set() for _ in range(expected_rounds)]
    pending = defaultdict(list)
    actual_receipts = Counter()
    for receipt in receipts:
        require(receipt.get("tool") == "wait_agents" and receipt.get("error") == "", "out-of-scope or failed tool receipt")
        native_report(receipt["result"])
        actual_receipts[receipt_key(receipt["tool_call_id"], receipt["arguments"], receipt["result"])] += 1
    matched = Counter()
    user_count = 0
    for message in transcript:
        if message.get("role") == "user":
            user_count += 1
        current_round = user_count - len(initial_users) - 1
        if message.get("role") == "assistant":
            for call in message.get("tool_calls") or []:
                require(call.get("function", {}).get("name") == "wait_agents", "non-wait operation in transcript")
                pending[call["id"]].append((current_round, call["function"]["arguments"]))
        if message.get("role") != "tool":
            continue
        call_id = message.get("tool_call_id")
        require(bool(pending[call_id]), "native transcript result has no preceding call")
        # Original IDs need not be unique. Pair occurrences in their recorded
        # request/result order; only a newly created reference needs uniqueness.
        round_index, arguments = pending[call_id].pop(0)
        require(0 <= round_index < expected_rounds, "wait tool result has no current human round")
        _, entries = native_report(message.get("content"))
        matched[receipt_key(call_id, arguments, message["content"])] += 1
        for entry in entries:
            worker = workers.get(entry.get("agent_id"))
            if worker is None:
                require(entry.get("status") == "unknown" and not entry.get("result"), "native wait invented a worker body")
                continue
            require(entry.get("generation") == worker["generation"] and entry.get("role") == worker["role"]
                    and entry.get("status") == worker["status"] and entry.get("result", "") == worker["result"]
                    and entry.get("error", "") == worker.get("error", ""), "native wait differs from the frozen model worker")
            coverage[round_index].add(entry["agent_id"])
    require(actual_receipts == matched and all(not values for values in pending.values()), "native tool receipts/transcript disagree")
    require(all(ids == set(workers) for ids in coverage), "a human round did not actually wait for all fixture worker identities")
    return set(actual_receipts), len(receipts)


def audit_outgoing_original(messages, native_membership):
    """Join each original body to its actual call arguments and native receipt."""
    pending = defaultdict(list)
    for message in messages:
        if message.get("role") == "assistant":
            for call in message.get("tool_calls") or []:
                function = call.get("function", {})
                require(function.get("name") == "wait_agents", "outgoing context contains a non-wait call")
                pending[call["id"]].append(function["arguments"])
        if message.get("role") != "tool":
            continue
        call_id = message.get("tool_call_id")
        require(bool(pending[call_id]), "outgoing original result has no preceding call")
        arguments = pending[call_id].pop(0)
        native_report(message.get("content"))
        require(receipt_key(call_id, arguments, message["content"]) in native_membership,
                "outgoing original wait binding/body lacks an actual native receipt")


def current_processes():
    try:
        return subprocess.check_output(["ps", "-axo", "pid=,command="], text=True).splitlines()
    except (OSError, subprocess.CalledProcessError) as error:
        raise AuditError("independent process liveness observation failed") from error


def active_helpers(manifest, outputs, process_lines):
    helper = str(Path(manifest["helper"]).resolve())
    active = []
    for line in process_lines:
        parts = line.strip().split(None, 1)
        if len(parts) != 2:
            continue
        command = parts[1]
        if command != helper and not command.startswith(helper + " "):
            continue
        try:
            arguments = shlex.split(command)
        except ValueError as error:
            raise AuditError("cannot interpret a matching helper process") from error
        require(arguments[0] == helper, "ambiguous helper process executable path")
        for index, argument in enumerate(arguments):
            out = argument.partition("=")[2] if argument.startswith("--out=") else (
                arguments[index + 1] if argument == "--out" and index + 1 < len(arguments) else None)
            if out is not None and str(Path(out).resolve()) in outputs:
                active.append(int(parts[0]))
    return active


def audit_trial(root, manifest_sha256, expected_sessions=30, expected_rounds=2, process_probe=current_processes):
    root = Path(root).resolve()
    require(integer(expected_sessions, 1) and integer(expected_rounds, 1), "expected coverage must be positive")
    require(re.fullmatch(r"[0-9a-f]{64}", manifest_sha256 or ""), "a pinned manifest SHA256 is required")
    require(digest(root / "manifest.json") == manifest_sha256, "manifest differs from the independent frozen pin")
    manifest = read_json(root / "manifest.json", private=True)
    order = manifest.get("order")
    require(isinstance(order, list) and len(order) == expected_sessions, "planned session count differs")
    planned = Counter(identity(row) for row in order)
    require(len(planned) == expected_sessions and all(n == 1 for n in planned.values()), "planned session identities are not unique")
    paired = defaultdict(set)
    for case, arm, repeat in planned:
        paired[case, repeat].add(arm)
    require(all(arms == {"a", "b"} for arms in paired.values()), "unpaired frozen session order")
    hashes = manifest.get("hashes")
    require(isinstance(hashes, dict) and bool(hashes), "missing frozen artifact hashes")
    require(str(Path(manifest["helper"]).resolve()) in hashes, "helper binary is not frozen")
    for name, expected in hashes.items():
        require(digest(Path(name)) == expected, "a frozen file changed")
    fixtures = fixture_inputs(manifest)
    require({row[0] for row in planned} == set(fixtures), "unused or missing frozen model fixture")
    for case, f in fixtures.items():
        require(f.get("schema_version") == 1 and len(f.get("rounds", [])) == expected_rounds
                and manifest["case_sources"][case]["rounds"] == expected_rounds, "wrong frozen human round count")
    report = read_json(root / "report.json", private=True)
    require(not report.get("stopped_early"), "trial stopped before the frozen plan completed")
    rows = report.get("results")
    require(isinstance(rows, list) and Counter(identity(row) for row in rows) == planned, "observed sessions do not exactly match the full plan")
    proof = Counter()
    outputs = set()
    for row in rows:
        case, arm, repeat = identity(row)
        source = manifest["case_sources"][case]
        require(row.get("source_alias") == source["source_alias"] and row.get("split") == source["split"], "frozen source/split binding changed")
        require(row.get("harness_error") == "" and row.get("exit_status") == 0 and row.get("helper_exited") is True
                and row.get("private_config_removed") is True and not row.get("cancelled"), "helper exit/private-config cleanup receipts are incomplete")
        run = root / "runs" / case / f"repeat-{repeat}" / arm
        require(equal(read_json(run / "record.json", private=True), row), "durable session record differs from report")
        helper = run / "helper"
        outputs.add(str(helper.resolve()))
        result = read_json(helper / "result.json", private=True)
        require(equal(result, row.get("result")), "helper result differs from session record")
        require(result.get("arm") == arm and result.get("error") == "" and result.get("scope_violations") == [], "session reported an error or scope violation")
        cleanup = result.get("cleanup", {})
        require(cleanup.get("registry_closed") is True and cleanup.get("live_workers") == 0, "Registry cleanup/liveness receipt failed")
        rounds = result.get("rounds", [])
        require(len(rounds) == expected_rounds and all(r.get("error") == "" for r in rounds), "human rounds incomplete or failed")
        require(all(type(r.get("wall_s")) in (int, float) and math.isfinite(r["wall_s"]) and r["wall_s"] >= 0 for r in rounds), "invalid round timing receipt")
        calls = result.get("calls", [])
        requests = read_json(helper / "actual-model-inputs.json", private=True)
        metrics = result.get("input_metrics", {})
        require(result.get("cost_complete") is True and isinstance(requests, list) and bool(requests)
                and len(calls) == len(requests) == metrics.get("requests"), "provider requests/usage receipts are incomplete")
        for call in calls:
            require(call.get("backoff") is False and call.get("error") == "" and call.get("agent_id") == "manager"
                    and integer(call.get("total_tokens"), 1)
                    and all(integer(call.get(key)) for key in ("prompt_tokens", "completion_tokens", "cached_tokens", "duration_ms")), "unknown/malformed provider usage or non-request receipt")
        transcript = read_json(helper / "transcript.json", private=True)
        receipts = read_json(helper / "notifications.json", private=True)
        membership, wait_count = audit_native_history(transcript, receipts, fixtures[case], expected_rounds)
        observed_refs, observed_chars = 0, 0
        for request in requests:
            counts = validate_request(request, arm)
            observed_refs += counts["references"]
            observed_chars += counts["recovered_result_chars"]
            proof.update(counts)
            audit_outgoing_original(request["original"], membership)
        require(metrics.get("reference_count") == observed_refs and metrics.get("duplicate_chars_removed") == observed_chars,
                "aggregate input-projection counters differ from exact proof")
        require(all(integer(metrics.get(key)) for key in ("original_chars", "forwarded_chars")), "invalid message character counters")
        if arm == "a":
            require(metrics["original_chars"] == metrics["forwarded_chars"], "control message character counts changed")
        for name in ("stdout.txt", "stderr.txt"):
            path = run / name
            require(path.is_file() and stat.S_IMODE(path.stat().st_mode) == 0o600, "private subprocess stream evidence is missing")
        require(not (helper / "workspace").exists(), "unexpected workspace in the minimal wait-only protocol")
        proof.update(sessions=1, human_rounds=expected_rounds, model_requests=len(requests), provider_call_receipts=len(calls),
                     native_wait_receipts=wait_count, rounds_with_actual_full_worker_coverage=expected_rounds,
                     cleanup_receipts=1)
    require(not active_helpers(manifest, outputs, process_probe()), "an exact helper/primary-out process is still live")
    return {"status": "verified_integrity_only", "manifest_sha256": manifest_sha256,
            "frozen_hashes_verified": len(hashes), "proof_counts": dict(proof),
            "matching_live_helpers": 0,
            "cleanup_scope": "Exit and private-config deletion are instrument receipts; current helper absence independently observed. No independent per-config-directory identity proof is claimed.",
            "exclusions": "No oracle/source-map interpretation, answer grading, A/B benefit calculation or production acceptance."}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--expected-sessions", type=int, default=30)
    parser.add_argument("--expected-rounds", type=int, default=2)
    args = parser.parse_args(argv)
    try:
        result = audit_trial(args.root, args.manifest_sha256, args.expected_sessions, args.expected_rounds)
    except (AuditError, OSError, KeyError, TypeError) as error:
        print(f"integrity audit refused: {type(error).__name__}: {error}", file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
