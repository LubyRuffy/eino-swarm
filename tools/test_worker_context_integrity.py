"""Synthetic integrity counterexamples only; never opens a real trial directory."""

import copy
import contextlib
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest

import worker_context_integrity as audit


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False))
    path.chmod(0o600)


def tool_call(call_id):
    return {"role": "assistant", "tool_calls": [{"id": call_id, "type": "function",
        "function": {"name": "wait_agents", "arguments": '{"agent_ids":["w"]}'}}]}


def tool_result(call_id, content):
    return {"role": "tool", "tool_call_id": call_id, "tool_name": "wait_agents", "content": content}


class AuditTrialTest(unittest.TestCase):
    def build(self, base):
        root = (base / "trial").resolve()
        inputs = (base / "inputs").resolve()
        body = '金额=-73; only if disabled=false; </script>&\u2028paragraph\u2029\n"\\'
        fixture = {"schema_version": 1, "instruction": "common instruction", "messages": [],
            "rounds": [{"user": "first"}, {"user": "second"}],
            "workers": [{"agent_id": "w", "role": "reader", "generation": 7,
                         "status": "done", "result": body, "error": ""}]}
        save(inputs / "case.json", fixture)
        save(inputs / "tasks.json", {"cases": [{"id": "case", "fixture": "case.json"}]})
        # A frozen oracle is deliberately undecodable: the auditor must hash it
        # opaquely, never load it to determine validity or interpret outcomes.
        oracle = inputs / "expectations.json"
        oracle.write_bytes(b"\xffopaque-never-interpret")
        helper_binary = base / "fixed-helper"
        helper_binary.write_bytes(b"fixed executable bytes for a synthetic test")
        manifest = {"schema_version": 1, "helper": str(helper_binary.resolve()), "inputs": str(inputs),
            "case_sources": {"case": {"source_alias": "source", "split": "heldout", "rounds": 2}},
            "hashes": {str(p.resolve()): audit.digest(p) for p in (inputs / "case.json", inputs / "tasks.json", oracle, helper_binary)},
            "order": [{"case": "case", "arm": arm, "repeat": 1} for arm in ("a", "b")]}
        save(root / "manifest.json", manifest)
        native = {"agents": [{"agent_id": "w", "generation": 7, "role": "reader", "status": "done",
                   "result": body, "undelivered": ["do not assume approval"]}], "timed_out": False}
        content = json.dumps(native, ensure_ascii=False)
        first = [{"role": "system", "content": "common instruction"}, {"role": "user", "content": "first"}]
        first_wait = first + [tool_call("c1"), tool_result("c1", content)]
        second = first_wait + [{"role": "assistant", "content": "final not graded"}, {"role": "user", "content": "second"}]
        second_wait = second + [tool_call("c2"), tool_result("c2", content)]
        transcript = second_wait + [{"role": "assistant", "content": "another final not graded"}]
        rows = []
        for arm in ("a", "b"):
            run = root / "runs" / "case" / "repeat-1" / arm
            helper = run / "helper"
            requests = [{"original": copy.deepcopy(messages), "forwarded": copy.deepcopy(messages), "replacements": None}
                        for messages in (first, first_wait, second, second_wait)]
            if arm == "b":
                reference = {"tool_call_id": "c1", "agent_id": "w", "generation": 7,
                             "result_error_sha256": audit.result_hash(body)}
                forwarded = copy.deepcopy(native)
                del forwarded["agents"][0]["result"]
                forwarded["agents"][0]["result_ref"] = reference
                requests[-1]["forwarded"][-1]["content"] = json.dumps(forwarded, ensure_ascii=False)
                requests[-1]["replacements"] = [{"message_index": len(second_wait) - 1,
                                                 **reference, "result_chars": len(body)}]
            metrics = {"requests": 4, "original_chars": 1000, "forwarded_chars": 1000,
                       "reference_count": int(arm == "b"), "duplicate_chars_removed": len(body) if arm == "b" else 0}
            result = {"arm": arm, "error": "", "scope_violations": [], "cost_complete": True,
                "cleanup": {"registry_closed": True, "live_workers": 0},
                "rounds": [{"final": "not graded", "error": "", "wall_s": 1.0}] * 2,
                "calls": [{"agent_id": "manager", "backoff": False, "error": "", "total_tokens": 110,
                           "prompt_tokens": 100, "completion_tokens": 10, "cached_tokens": 0, "duration_ms": 1}] * 4,
                "input_metrics": metrics}
            receipts = [{"tool": "wait_agents", "tool_call_id": call_id,
                         "arguments": '{"agent_ids":["w"]}', "result": content, "error": ""} for call_id in ("c1", "c2")]
            for name, data in (("result.json", result), ("actual-model-inputs.json", requests),
                               ("notifications.json", receipts), ("transcript.json", transcript)):
                save(helper / name, data)
            for name in ("stdout.txt", "stderr.txt"):
                path = run / name
                path.write_text("")
                path.chmod(0o600)
            row = {"case": "case", "arm": arm, "repeat": 1, "source_alias": "source", "split": "heldout",
                   "harness_error": "", "exit_status": 0, "helper_exited": True,
                   "private_config_removed": True, "result": result}
            save(run / "record.json", row)
            rows.append(row)
        save(root / "report.json", {"results": rows})
        return root, audit.digest(root / "manifest.json")

    def perform(self, root, pin, processes=None):
        return audit.audit_trial(root, pin, expected_sessions=2, expected_rounds=2,
                                 process_probe=lambda: [] if processes is None else processes)

    def test_complete_proof_and_opaque_oracle_without_quality_or_benefit(self):
        with tempfile.TemporaryDirectory() as temp:
            root, pin = self.build(Path(temp))
            result = self.perform(root, pin)
            self.assertEqual(result["status"], "verified_integrity_only")
            self.assertEqual(result["proof_counts"]["sessions"], 2)
            self.assertEqual(result["proof_counts"]["references"], 1)
            self.assertEqual(result["proof_counts"]["human_rounds"], 4)
            self.assertNotIn("effect", result)
            self.assertNotIn("quality", result)

    def test_go_marshal_golden_handles_html_control_unicode_and_separators(self):
        value = '<>&\u2028\u2029\n"\\金额=-73'
        expected = b'["\\u003c\\u003e\\u0026\\u2028\\u2029\\n\\"\\\\' + '金额=-73'.encode() + b'",""]'
        self.assertEqual(audit.go_string_array(value), expected)
        self.assertEqual(audit.result_hash(value), hashlib.sha256(expected).hexdigest())

    def test_incomplete_duplicate_or_unpinned_plan_never_produces_a_proof(self):
        for mutation in ("missing-pin", "manifest-change", "frozen-file-change", "missing-session", "duplicate-session"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root, pin = self.build(Path(temp))
                if mutation == "missing-pin":
                    pin = ""
                elif mutation == "manifest-change":
                    (root / "manifest.json").write_text((root / "manifest.json").read_text() + "\n")
                elif mutation == "frozen-file-change":
                    manifest = audit.read_json(root / "manifest.json")
                    Path(manifest["helper"]).write_bytes(b"changed")
                else:
                    report = audit.read_json(root / "report.json")
                    report["results"] = report["results"][:1] if mutation == "missing-session" else [report["results"][0]] * 2
                    save(root / "report.json", report)
                with self.assertRaises(audit.AuditError):
                    self.perform(root, pin)

    def test_reference_hash_generation_metadata_chain_or_order_corruption_refuses(self):
        for mutation in ("hash", "generation", "metadata", "future", "chain", "call-binding", "message-count"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root, pin = self.build(Path(temp))
                path = root / "runs/case/repeat-1/b/helper/actual-model-inputs.json"
                requests = audit.read_json(path)
                item = requests[-1]
                report = audit.decode(item["forwarded"][-1]["content"])
                reference = report["agents"][0]["result_ref"]
                if mutation == "hash": reference["result_error_sha256"] = "0" * 64
                if mutation == "generation": reference["generation"] = 8
                if mutation == "metadata": report["agents"][0]["undelivered"] = []
                if mutation == "future": reference["tool_call_id"] = "c2"
                if mutation == "chain": item["forwarded"][3]["content"] = json.dumps(report)
                if mutation == "call-binding": item["forwarded"][2]["tool_calls"][0]["function"]["name"] = "read"
                if mutation == "message-count": item["forwarded"].pop(0)
                item["forwarded"][-1]["content"] = json.dumps(report)
                save(path, requests)
                with self.assertRaises(audit.AuditError): self.perform(root, pin)

    def test_usage_cleanup_native_ref_and_missing_current_round_wait_refuse(self):
        for mutation in ("cost", "usage", "registry", "native-ref", "no-current-wait"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root, pin = self.build(Path(temp))
                if mutation in ("cost", "usage", "registry"):
                    report = audit.read_json(root / "report.json")
                    row = report["results"][0]
                    if mutation == "cost": row["result"]["cost_complete"] = False
                    if mutation == "usage": row["result"]["calls"][0]["total_tokens"] = 0
                    if mutation == "registry": row["result"]["cleanup"]["live_workers"] = 1
                    run = root / "runs/case/repeat-1/a"
                    save(run / "helper/result.json", row["result"])
                    save(run / "record.json", row)
                    save(root / "report.json", report)
                elif mutation == "native-ref":
                    path = root / "runs/case/repeat-1/a/helper/notifications.json"
                    receipts = audit.read_json(path)
                    document = audit.decode(receipts[0]["result"])
                    document["agents"][0]["result_ref"] = {}
                    receipts[0]["result"] = json.dumps(document)
                    save(path, receipts)
                else:
                    path = root / "runs/case/repeat-1/a/helper/transcript.json"
                    transcript = audit.read_json(path)
                    del transcript[-3:-1]
                    save(path, transcript)
                with self.assertRaises(audit.AuditError): self.perform(root, pin)

    def test_exact_live_helper_out_is_refused_without_exposing_unrelated_processes(self):
        with tempfile.TemporaryDirectory() as temp:
            root, pin = self.build(Path(temp))
            helper = audit.read_json(root / "manifest.json")["helper"]
            out = root / "runs/case/repeat-1/a/helper"
            self.perform(root, pin, [f"123 /other/program --private-secret should-not-be-output"])
            with self.assertRaises(audit.AuditError):
                self.perform(root, pin, [f"123 {helper} --out {out}"])

    def test_outgoing_wait_binding_must_match_actual_native_arguments(self):
        with tempfile.TemporaryDirectory() as temp:
            root, pin = self.build(Path(temp))
            path = root / "runs/case/repeat-1/b/helper/actual-model-inputs.json"
            requests = audit.read_json(path)
            for request in requests:
                for side in ("original", "forwarded"):
                    for message in request[side]:
                        for call in message.get("tool_calls") or []:
                            if call["id"] == "c1":
                                call["function"]["arguments"] = '{"agent_ids":["forged"]}'
            save(path, requests)
            with self.assertRaises(audit.AuditError):
                self.perform(root, pin)

    def test_a_reference_can_target_first_full_entry_in_a_mixed_earlier_report(self):
        def entry(name):
            return {"agent_id": name, "generation": 7, "role": "reader", "status": "done", "result": name + " full body"}
        reports = [{"agents": [entry("a")]}, {"agents": [entry("a"), entry("b")]}, {"agents": [entry("b")]}]
        original = []
        for number, report in enumerate(reports, 1):
            call_id = f"c{number}"
            original += [tool_call(call_id), tool_result(call_id, json.dumps(report))]
        forwarded = copy.deepcopy(original)
        changes = []
        for message_index, agent_index, source, name in ((3, 0, "c1", "a"), (5, 0, "c2", "b")):
            report = audit.decode(forwarded[message_index]["content"])
            body = report["agents"][agent_index].pop("result")
            reference = {"tool_call_id": source, "agent_id": name, "generation": 7, "result_error_sha256": audit.result_hash(body)}
            report["agents"][agent_index]["result_ref"] = reference
            forwarded[message_index]["content"] = json.dumps(report)
            changes.append({"message_index": message_index, **reference, "result_chars": len(body)})
        proof = audit.validate_request({"original": original, "forwarded": forwarded, "replacements": changes}, "b")
        self.assertEqual(proof["references"], 2)

    def test_cli_refuses_incomplete_synthetic_trial_without_success_output(self):
        with tempfile.TemporaryDirectory() as temp:
            root, pin = self.build(Path(temp))
            arguments = ["--root", str(root), "--manifest-sha256", pin,
                         "--expected-sessions", "2", "--expected-rounds", "2"]
            output, errors = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
                self.assertEqual(audit.main(arguments), 0)
            self.assertEqual(json.loads(output.getvalue())["status"], "verified_integrity_only")
            self.assertEqual(errors.getvalue(), "")
            (root / "report.json").unlink()
            output, errors = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
                self.assertEqual(audit.main(arguments), 2)
            self.assertEqual(output.getvalue(), "")
            self.assertIn("integrity audit refused", errors.getvalue())

    def test_native_argument_refusal_can_be_followed_by_valid_round_waits(self):
        for arguments in ('{"agent_ids":', '{"agent_ids":[],"agent_ids":["w"]}'):
            with self.subTest(arguments=arguments), tempfile.TemporaryDirectory() as temp:
                root, _ = self.build(Path(temp))
                helper = root / "runs/case/repeat-1/a/helper"
                transcript = audit.read_json(helper / "transcript.json")
                receipts = audit.read_json(helper / "notifications.json")
                call = tool_call("recoverable")
                call["tool_calls"][0]["function"]["arguments"] = arguments
                content = '{"error":"invalid request; correct it and wait again"}'
                transcript[2:2] = [call, tool_result("recoverable", content)]
                receipts.insert(0, {"tool": "wait_agents", "tool_call_id": "recoverable",
                                   "arguments": arguments, "result": content, "error": ""})
                fixture = audit.read_json(Path(temp) / "inputs/case.json")
                membership, count = audit.audit_native_history(transcript, receipts, fixture, 2)
                audit.audit_outgoing_original(transcript, membership)
                self.assertEqual(count, 3)

    def test_duplicate_worker_report_cannot_be_rewritten_even_if_bodies_match(self):
        entry = {"agent_id": "w", "generation": 7, "status": "done", "result": "full body"}
        original = [tool_call("c1"), tool_result("c1", json.dumps({"agents": [entry]})),
                    tool_call("c2"), tool_result("c2", json.dumps({"agents": [entry, entry]}))]
        reference = {"tool_call_id": "c1", "agent_id": "w", "generation": 7,
                     "result_error_sha256": audit.result_hash(entry["result"])}
        projected = {k: v for k, v in entry.items() if k != "result"}
        projected["result_ref"] = reference
        forwarded = copy.deepcopy(original)
        forwarded[-1]["content"] = json.dumps({"agents": [projected, projected]})
        changes = [{"message_index": 3, **reference, "result_chars": len(entry["result"])}] * 2
        with self.assertRaises(audit.AuditError):
            audit.validate_request({"original": original, "forwarded": forwarded, "replacements": changes}, "b")

    def test_original_repeated_tool_ids_can_stay_full_without_reference_ambiguity(self):
        for repeated_pending in (False, True):
            with self.subTest(repeated_pending=repeated_pending), tempfile.TemporaryDirectory() as temp:
                root, _ = self.build(Path(temp))
                helper = root / "runs/case/repeat-1/a/helper"
                transcript = audit.read_json(helper / "transcript.json")
                receipts = audit.read_json(helper / "notifications.json")
                if repeated_pending:
                    transcript[2]["tool_calls"].append(copy.deepcopy(transcript[2]["tool_calls"][0]))
                    transcript.insert(4, copy.deepcopy(transcript[3]))
                    receipts.insert(1, copy.deepcopy(receipts[0]))
                else:
                    for message in transcript:
                        if message.get("tool_call_id") == "c2":
                            message["tool_call_id"] = "c1"
                        for call in message.get("tool_calls") or []:
                            if call["id"] == "c2":
                                call["id"] = "c1"
                    receipts[1]["tool_call_id"] = "c1"
                fixture = audit.read_json(Path(temp) / "inputs/case.json")
                membership, _ = audit.audit_native_history(transcript, receipts, fixture, 2)
                audit.audit_outgoing_original(transcript, membership)
                proof = audit.validate_request({"original": transcript, "forwarded": copy.deepcopy(transcript),
                                                "replacements": None}, "b")
                self.assertEqual(proof["references"], 0)


if __name__ == "__main__":
    unittest.main()
