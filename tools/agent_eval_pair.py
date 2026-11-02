#!/usr/bin/env python3
"""Run frozen, interleaved project-mode eval arms without presuming a winner.

Each arm owns fresh engine/data/workspace state. All arms in a pair must
have identical task bytes, initial fixture hashes and limits. No model call
is made by --dry-run. Real runs are explicitly opt-in, like agent_eval.py.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import math
import random
import shutil
import statistics
import sys
import tempfile
import types
from pathlib import Path

import agent_eval as runner
import agent_eval_grade as grade
import agent_eval_protocol as protocol


def plan_schedule(tasks: list[str], arms: list[str], runs: int, seed: int, protocol_hash: str) -> list[dict]:
    rng = random.Random(seed)
    orders = {task: rng.sample(arms, len(arms)) for task in tasks}
    blocks = [(task, repeat) for task in tasks for repeat in range(1, runs + 1)]
    rng.shuffle(blocks)
    schedule = []
    for task, repeat in blocks:
        order = orders[task]
        offset = (repeat - 1) % len(order)
        order = order[offset:] + order[:offset]
        pair_id = protocol.digest({"protocol": protocol_hash, "task": task, "replicate": repeat})[:24]
        schedule.extend({"task": task, "replicate": repeat, "arm": arm, "pair_id": pair_id, "order": position}
                        for position, arm in enumerate(order))
    return schedule


def _median(values):
    return statistics.median(values) if values else None


def _tokens(row: dict):
    # The older numeric metric cannot prove that the provider reported usage.
    # A partial auxiliary request can also outlive the last captured Trace.
    if row.get("review_settled") is not True or row.get("review_cut_off"):
        return None
    return row.get("metrics", {}).get("usage_facts", {}).get("total_tokens")


def paired_summary(results: list[dict], arms: list[str], measurement_kind: str = "real_model_calls") -> dict:
    index, per_arm = {}, {}
    for row in results:
        identity = row["run_identity"]
        key = (identity["pair_id"], identity["arm"])
        if key in index:
            raise ValueError("duplicate pair/arm identity")
        index[key] = row
    for arm in arms:
        rows = [r for r in results if r["run_identity"]["arm"] == arm]
        graded = [r for r in rows if not r.get("harness_error")]
        known = [_tokens(r) for r in graded if _tokens(r) is not None]
        per_arm[arm] = {"runs": len(rows), "graded": len(graded), "passed": sum(bool(r.get("passed")) for r in graded),
                        "harness_errors": len(rows) - len(graded),
                        "task_time_median_s": _median([r["task_s"] for r in graded if "task_s" in r]),
                        "total_tokens": sum(known) if graded and len(known) == len(graded) else None,
                        "known_token_runs": len(known)}
    comparisons = {}
    pair_ids = sorted({key[0] for key in index})
    for arm in arms[1:]:
        valid, invalid, excluded, incomplete = [], [], [], []
        for pair_id in pair_ids:
            a, b = index.get((pair_id, arms[0])), index.get((pair_id, arm))
            if a is None or b is None:
                incomplete.append(pair_id)
                continue
            if a.get("harness_error") or b.get("harness_error"):
                excluded.append(pair_id)
                continue
            mismatches = [key for key in ("task", "task_sha256", "fixture_sha256", "budget") if a.get(key) != b.get(key)]
            missing = [key + "_missing" for key in ("task_sha256", "fixture_sha256", "budget") if not a.get(key) or not b.get(key)]
            if mismatches or missing:
                invalid.append({"pair_id": pair_id, "fields": mismatches + missing})
                continue
            valid.append((a, b))
        successful = [(a, b) for a, b in valid if a.get("passed") and b.get("passed")]
        token_pairs = [(a, b) for a, b in valid if _tokens(a) is not None and _tokens(b) is not None]
        complete_tokens = bool(valid) and len(token_pairs) == len(valid)
        comparisons[arm] = {
            "reference": arms[0], "valid_pairs": len(valid), "both_passed": len(successful),
            "success_delta": sum(bool(b.get("passed")) - bool(a.get("passed")) for a, b in valid),
            "successful_time_delta_median_s": _median([b["task_s"] - a["task_s"] for a, b in successful
                                                        if "task_s" in a and "task_s" in b]),
            "token_pairs": len(token_pairs), "token_comparison": "complete" if complete_tokens else "unavailable",
            "token_delta_median": _median([_tokens(b) - _tokens(a) for a, b in token_pairs]) if complete_tokens else None,
            "invalid_pairs": invalid, "infrastructure_excluded_pairs": excluded, "incomplete_pairs": incomplete,
        }
        if measurement_kind == "offline_mock":
            comparisons[arm].update({"token_comparison": "synthetic_mock_only", "token_delta_median": None,
                                     "successful_time_delta_median_s": None})
    return {"measurement_kind": measurement_kind, "arms": per_arm, "comparisons": comparisons}


def render(summary: dict, complete: bool, measurement_kind: str = "real_model_calls") -> str:
    lines = ["# Paired agent evaluation", "", f"Completed schedule: {complete}.", "",
             *(["OFFLINE MOCK: responses and usage are scripted. This validates the harness and API lifecycle only; "
                "it is not model capability, speed, token or billing evidence.", ""] if measurement_kind == "offline_mock" else []),
             "Descriptive observations only. Task success is primary; fast failures are not successful speedups. "
             "Token comparison is unavailable when any paired run lacks complete usage evidence.", "",
             "| arm | passed / graded | harness errors | median task seconds | reported total tokens |",
             "|---|---|---|---|---|"]
    for arm, stats in summary["arms"].items():
        lines.append(f"| {arm} | {stats['passed']} / {stats['graded']} | {stats['harness_errors']} | "
                     f"{stats['task_time_median_s']} | {stats['total_tokens']} |")
    lines += ["", "Differences below are candidate minus reference; negative means a lower measured value. "
              "Time differences use pairs where both arms passed. Tokens include failed graded attempts too.", "",
              "| candidate | valid pairs | success count difference | both passed | median time difference (s) | token comparison | median token difference |",
              "|---|---|---|---|---|---|---|"]
    for arm, c in summary["comparisons"].items():
        lines.append(f"| {arm} | {c['valid_pairs']} | {c['success_delta']} | {c['both_passed']} | "
                     f"{c['successful_time_delta_median_s']} | {c['token_comparison']} | {c['token_delta_median']} |")
    lines.extend(["", "## Pair exclusions", ""])
    for arm, c in summary["comparisons"].items():
        lines.append(f"- {arm}: invalid={json.dumps(c['invalid_pairs'])}; "
                     f"infrastructure={json.dumps(c['infrastructure_excluded_pairs'])}; "
                     f"incomplete={json.dumps(c['incomplete_pairs'])}")
    return "\n".join(lines) + "\n"


def load_spec(path: Path) -> dict:
    spec = json.loads(path.read_text())
    arms = spec.get("arms", [])
    if spec.get("version") != 1 or len(arms) < 2:
        raise ValueError("protocol needs version 1 and at least two arms")
    if spec.get("benchmark", "generic-task-eval") not in ("generic-task-eval", "generic-harness-smoke"):
        raise ValueError("the generic runner cannot certify an external benchmark score")
    ids = [a.get("id", "") for a in arms]
    if len(set(ids)) != len(ids) or any(not i or not all(c.isalnum() or c in "-_" for c in i) for i in ids):
        raise ValueError("arm ids must be unique path-safe names")
    if type(spec.get("runs")) is not int or spec["runs"] < 1:
        raise ValueError("runs must be a positive integer")
    for arm in arms:
        if arm.get("project_mode") not in ("default", "nuwa"):
            raise ValueError("each arm needs project_mode default or nuwa")
        if not isinstance(arm.get("project_instruction", ""), str):
            raise ValueError("project_instruction must be a string")
        for key in ("planner", "executor_preference"):
            choice = arm.get(key)
            if choice is not None and (not isinstance(choice, dict) or not choice.get("provider_id") or not choice.get("model")):
                raise ValueError("model choices need provider_id and model")
    budget = spec.get("budget", {})
    if not isinstance(budget, dict) or set(budget) - {"max_completion_tokens", "review_wait_s", "timeout_s"}:
        raise ValueError("unknown budget fields are not silently ignored")
    cap = budget.get("max_completion_tokens", 0)
    if type(cap) is not int or cap < 0:
        raise ValueError("max_completion_tokens must be a nonnegative integer")
    for name in ("review_wait_s", "timeout_s"):
        value = budget.get(name, 30)
        if type(value) not in (int, float) or not math.isfinite(value) or value < 0 or (name == "timeout_s" and value == 0):
            raise ValueError("time budgets must be finite nonnegative numbers; task timeout must be positive")
    return spec


def _ref(choice: dict | None) -> str:
    return choice["provider_id"] + "\t" + choice["model"] if choice else ""


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--protocol", required=True)
    parser.add_argument("--config", default=str(runner.DEFAULT_CONFIG))
    parser.add_argument("--zwai", default=str(runner.REPO / "bin" / "zwai-eval"))
    parser.add_argument("--out", default=str(Path(tempfile.gettempdir()) / "zwai-agent-pairs"))
    parser.add_argument("--task-root", default="")
    parser.add_argument("--dry-run", action="store_true", help="freeze schedule/config choices without starting an engine or model")
    parser.add_argument("--mock", action="store_true", help="offline harness behavior only; never model capability evidence")
    parser.add_argument("--boot-timeout", type=float, default=240)
    args = parser.parse_args(argv)
    runner.unwind_on_termination()
    spec = load_spec(Path(args.protocol))
    task_root = Path(args.task_root or spec.get("task_root", str(runner.TASKS_DIR)))
    if not task_root.is_absolute():
        task_root = runner.REPO / task_root
    tasks = runner.load_tasks(spec.get("tasks", []), task_root)
    if not tasks:
        raise ValueError("protocol selected no tasks")
    binary = Path(args.zwai).resolve()
    if not args.dry_run and not binary.is_file():
        raise ValueError("build the identified candidate before running the protocol")
    config_text = Path(args.config).read_text() if not args.mock else ""
    models = {}
    for arm in spec["arms"]:
        planner = arm.get("planner")
        if not args.mock:
            if not planner:
                raise ValueError("real-model arms require an explicit planner")
            models[arm["id"]] = protocol.prepare_models(config_text, planner["provider_id"],
                                                         planner_ref=_ref(planner), executor_ref=_ref(arm.get("executor_preference")))
        else:
            models[arm["id"]] = None
    limits = spec.get("budget", {})
    cap, review_wait = int(limits.get("max_completion_tokens", 0)), float(limits.get("review_wait_s", 30))
    if "timeout_s" in limits:
        if float(limits["timeout_s"]) <= 0:
            raise ValueError("task timeout must be positive")
        tasks = [{**task, "timeout_s": float(limits["timeout_s"])} for task in tasks]
    frozen = {"protocol": spec, "source": grade.git_describe(runner.REPO),
              "measurement_kind": "offline_mock" if args.mock else "real_model_calls",
              "binary_sha256": protocol.file_digest(binary) if binary.is_file() else None,
              "task_hashes": {t["id"]: protocol.task_fingerprint(t) for t in tasks},
              "models": {arm: protocol.model_summary(m) if m else {"mock": True} for arm, m in models.items()}}
    frozen["protocol_sha256"] = protocol.digest(frozen)
    arm_ids = [a["id"] for a in spec["arms"]]
    schedule = plan_schedule([t["id"] for t in tasks], arm_ids, spec["runs"], int(spec.get("seed", 0)), frozen["protocol_sha256"])
    stamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    out = Path(args.out).resolve() / stamp
    if out.is_relative_to(runner.REPO.resolve()):
        raise ValueError("paired evidence must be outside the source repository")
    protocol.private_write(out / "protocol.json", json.dumps({**frozen, "schedule": schedule}, indent=2, ensure_ascii=False))
    if args.dry_run:
        print(f"protocol only (no model calls): {out / 'protocol.json'}")
        return 0
    candidate = out / "candidate" / binary.name
    candidate.parent.mkdir(mode=0o700)
    shutil.copyfile(binary, candidate)
    candidate.chmod(0o700)
    if protocol.file_digest(candidate) != frozen["binary_sha256"]:
        raise ValueError("candidate changed while being frozen; no model run started")
    arm_by_id = {a["id"]: a for a in spec["arms"]}
    task_by_id = {t["id"]: t for t in tasks}
    results = []
    for index, item in enumerate(schedule):
        if protocol.task_fingerprint(task_by_id[item["task"]]) != frozen["task_hashes"][item["task"]]:
            raise ValueError("task changed after protocol freeze; no further model run started")
        arm, model_setup = arm_by_id[item["arm"]], models[item["arm"]]
        run_args = types.SimpleNamespace(zwai=str(candidate), mock=args.mock, boot_timeout=args.boot_timeout,
                                        review_wait=review_wait, max_completion_tokens=cap,
                                        project_mode=arm["project_mode"], models=model_setup, run_identity=item,
                                        project_instruction=arm.get("project_instruction", ""))
        provider = model_setup["providers"][0] if model_setup else None
        print(f"[{index + 1}/{len(schedule)}] {item['task']} replicate {item['replicate']} {item['arm']}", flush=True)
        result = runner.run_once(task_by_id[item["task"]], item["replicate"], run_args, provider, out / "arms" / item["arm"])
        results.append(result)
        complete = len(results) == len(schedule)
        summary = paired_summary(results, arm_ids, frozen["measurement_kind"])
        protocol.private_write(out / "report.json", json.dumps({"meta": frozen, "complete": complete,
                                                                 "summary": summary, "results": results}, indent=2, ensure_ascii=False))
        protocol.private_write(out / "report.md", render(summary, complete, frozen["measurement_kind"]))
    print(f"paired report: {out / 'report.md'}")
    return 2 if any(r.get("harness_error") for r in results) else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
