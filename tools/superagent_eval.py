#!/usr/bin/env python3
"""Opt-in constructed-observation decision trial, never production scheduling QA.

Arm a retains each ordinary monitoring instruction, including existing quiet
rules. Arm b appends one generic triage instruction. Both receive identical
constructed observations; hidden expected decisions never enter the workspace.
The real agent-eval HTTP/process runner supplies execution and caller costs.
Automatic checks and independent semantic review are reported separately.
"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import random
import re
import sqlite3
import statistics
import sys
from pathlib import Path
from typing import Any

import agent_eval as harness
import agent_eval_grade as grade

REPO = Path(__file__).resolve().parent.parent
FIXTURE = REPO / "evals" / "superagent"
DECISIONS = {"quiet", "notify", "unknown"}
ACTIONS = {"continue", "investigate", "verify", "complete", "escalate", "unknown"}
TRIAGE = """应用低打扰分诊原则：只有可用的新结果、失败或需要人采取行动时才通知；
稳定且正常的进度保持安静。截止时间本身也是需要判断的事件。观测缺失或过期不能
作为成功结论；保留不确定性，按输出契约选择下一步。通知应说明新证据和
需要的行动，避免重复已知信息。"""
OUTPUT_CONTRACT = """这是一次基于构造观测的决策实验。必须先调用 read 工具完整读取当前工作区 evidence.json；
不要查询真实服务、历史数据库或外部系统，也不要执行监控操作。read/write 工具已足够，
不要调用 shell/exec 或创建子 Agent。基于提供的观测，
将判断写入当前工作区 decision.json，JSON 格式：
{"decision":"quiet|notify|unknown","action":"continue|investigate|verify|complete|escalate|unknown",
 "evidence_ids":["引用的观测ID"],"notification":"实际应通知的内容；无需通知时为空字符串"}。
decision: quiet=本次无应通知事件；notify=有已确认的应通知事件；unknown=当前探测不可用，
状态不能判断，需要非空的、可行动的观测失败通知。
action: continue=正常推进并保持原节奏；investigate=对停滞或越过截止时间补证，不宣告失败；
verify=已有成功迹象但必要证据不齐，补验证；complete=必要验证与收尾状态回读齐全；
escalate=已证实失败，报告并建议既有恢复预案；unknown=当前探测不可用，先恢复可靠观测。
evidence_ids 只能引用 evidence.json 中已提供的 ID。完成后简短确认文件已写入。
"""


def encoded(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def private_write(path: Path, value: str | bytes) -> None:
    """Create private evidence explicitly, preserving the build/child umask."""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "wb") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(value.encode() if isinstance(value, str) else value)


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def provider_config_digest(provider: dict | None) -> str:
    effective = {key: value for key, value in (provider or {}).items() if key != "api_key"}
    return hashlib.sha256(json.dumps(effective, sort_keys=True).encode()).hexdigest()


class PrivateEngine(harness.Engine):
    """Private default discovery paths; tools still retain their full access."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        private_write(self.log_path, "")

    def child_env(self) -> dict[str, str]:
        env = super().child_env()
        home = self.data_dir / "home"
        home.mkdir(mode=0o700, exist_ok=True)
        env.update({"HOME": str(home), "XDG_CONFIG_HOME": str(home / ".config"),
                    "XDG_CACHE_HOME": str(home / ".cache"), "XDG_DATA_HOME": str(home / ".local/share"),
                    "ZDOTDIR": str(home)})
        return env


def load_fixture(root: Path) -> tuple[dict, dict]:
    raw = json.loads((root / "cases.json").read_text())
    oracle = json.loads((root / "expectations.json").read_text())
    if raw.get("schema_version") != 1 or oracle.get("schema_version") != 1:
        raise harness.EvalError("fixture schema_version must be 1")
    cases = raw.get("cases") or []
    ids = [case.get("id", "") for case in cases]
    if not ids or len(set(ids)) != len(ids) or any(not re.fullmatch(r"[a-z0-9][a-z0-9-]*", x) for x in ids):
        raise harness.EvalError("fixture requires unique safe case ids")
    if set(oracle.get("cases", {})) != set(ids):
        raise harness.EvalError("case and expectation ids differ")
    for case in cases:
        observed = case.get("observations") or {}
        rows = observed.get("evidence") or []
        evidence_ids = [row.get("id", "") for row in rows]
        expected = oracle["cases"][case["id"]]
        if not case.get("instruction") or observed.get("provenance") != "constructed_observation_from_real_task":
            raise harness.EvalError(f"{case['id']}: only explicit constructed observations are accepted")
        if not evidence_ids or not all(isinstance(x, str) and x for x in evidence_ids) or len(set(evidence_ids)) != len(rows):
            raise harness.EvalError(f"{case['id']}: missing or duplicate evidence ids")
        if expected.get("decision") not in DECISIONS or expected.get("action") not in ACTIONS:
            raise harness.EvalError(f"{case['id']}: invalid expected decision/action")
        if not set(expected.get("required_evidence_ids", [])) <= set(evidence_ids):
            raise harness.EvalError(f"{case['id']}: expected evidence id is absent")
        if not isinstance(expected.get("notification_nonempty"), bool):
            raise harness.EvalError(f"{case['id']}: notification expectation must be boolean")
    return raw, oracle


def build_tasks(cases: list[dict], root: Path, timeout: float) -> dict:
    tasks = {"a": {}, "b": {}}
    for case in cases:
        prompt = case["instruction"].strip() + "\n\n" + OUTPUT_CONTRACT.strip()
        for arm in tasks:
            dest = root / arm / case["id"]
            private_write(dest / "fixture" / "evidence.json", encoded(case["observations"]))
            private_write(dest / "prompt.md", prompt + ("\n\n" + TRIAGE if arm == "b" else ""))
            private_write(dest / "task.json", encoded({"id": case["id"], "timeout_s": timeout,
                "purpose": "Constructed observation decision trial; not production behavior acceptance.",
                "checks": [{"name": "decision artifact exists", "type": "file_exists", "path": "decision.json"},
                           {"name": "no runtime error", "type": "event_count", "kind": "error", "max": 0}]}))
            tasks[arm][case["id"]] = grade.load_task(dest)
    return tasks


def execution_order(case_ids: list[str], runs: int, seed: int) -> list[dict]:
    rng = random.Random(seed)
    ids = list(case_ids)
    rng.shuffle(ids)
    order = []
    for case in ids:
        first = rng.choice(("a", "b"))
        for run in range(1, runs + 1):
            arms = [first, "b" if first == "a" else "a"]
            if run % 2 == 0:
                arms.reverse()
            order.extend({"case": case, "run": run, "arm": arm} for arm in arms)
    return order


def grade_decision(path: Path, case: dict, expected: dict) -> dict:
    result = {"automatic_passed": False, "semantic_status": "unreviewed", "checks": {},
              "expected_notify": expected["notification_nonempty"],
              "critical": bool(expected.get("critical", case.get("critical", False)))}
    try:
        doc = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        return {**result, "artifact_error": f"{type(exc).__name__}: {exc}"}
    if not isinstance(doc, dict):
        return {**result, "artifact_error": "decision must be a JSON object"}
    ids = doc.get("evidence_ids")
    evidence_valid = isinstance(ids, list) and all(isinstance(x, str) for x in ids)
    cited = set(ids) if evidence_valid else set()
    available = {row["id"] for row in case["observations"]["evidence"]}
    notification = doc.get("notification")
    result["checks"] = {
        "decision": doc.get("decision") == expected["decision"],
        "action": doc.get("action") == expected["action"],
        "grounded_evidence": evidence_valid and cited <= available,
        "required_evidence": evidence_valid and set(expected.get("required_evidence_ids", [])) <= cited,
        "notification": isinstance(notification, str) and bool(notification.strip()) == expected["notification_nonempty"],
    }
    result.update({"automatic_passed": all(result["checks"].values()), "actual": doc,
                   "excess_notification": isinstance(notification, str) and bool(notification.strip()) and not expected["notification_nonempty"],
                   "missed_notification": expected["notification_nonempty"] and (not isinstance(notification, str) or not notification.strip())})
    return result


def cache_observations(traces: list[dict]) -> dict:
    calls = [c for trace in traces for c in trace.get("llm_calls", [])]
    return {"field_missing": sum("cached_tokens" not in c or c["cached_tokens"] is None for c in calls),
            "zero": sum(c.get("cached_tokens") == 0 for c in calls),
            "positive": sum(isinstance(c.get("cached_tokens"), (int, float)) and c["cached_tokens"] > 0 for c in calls),
            "note": "Trace field observations only; persisted zero may represent an upstream missing value."}


def fixture_read_receipts(traces: list[dict], observations: dict, workspace_paths: tuple[Path, ...] = ()) -> dict:
    """Require the actual read result's complete numbered bytes, bound to its call."""
    pending = {}
    receipts = []
    decision_write_seen, wrote_before_read = False, False
    expected = encoded(observations).rstrip("\n")
    for trace in traces:
        for event in trace.get("events", []):
            key = (event.get("agent_id"), event.get("tool_call_id"))
            if not all(key):
                continue
            if event.get("kind") == "tool_call":
                tool, args = grade.parse_tool_call(event.get("text", ""))
                if tool == "write" and isinstance(args, dict):
                    output_path = Path(str(args.get("file_path", "")))
                    if output_path == Path("decision.json") or output_path in tuple(p.with_name("decision.json") for p in workspace_paths):
                        decision_write_seen = True
                        wrote_before_read = wrote_before_read or not receipts
                if tool == "read" and isinstance(args, dict):
                    path = Path(str(args.get("file_path", "")))
                    absolute_ok = path.is_absolute() and path in workspace_paths
                    base_ok = not args.get("base_dir") or Path(args["base_dir"]) / "evidence.json" in workspace_paths
                    if (path == Path("evidence.json") or absolute_ok) and base_ok:
                        pending[key] = str(path)
            elif event.get("kind") == "tool_result" and key in pending:
                text = event.get("text", "")
                header = re.match(r"^encoding=utf-8 path=(.+) offset=1 limit=\d+\n", text)
                if not header:
                    continue
                read_path = Path(header[1])
                if read_path != Path("evidence.json") and read_path not in workspace_paths:
                    continue
                numbered = [re.fullmatch(r"(\d+)\|(.*)", line) for line in text.splitlines()[1:]]
                if not numbered or any(line is None for line in numbered):
                    continue
                if [int(line[1]) for line in numbered] != list(range(1, len(numbered) + 1)):
                    continue
                if "\n".join(line[2] for line in numbered) == expected:
                    receipts.append({"agent_id": key[0], "tool_call_id": key[1], "path": pending[key]})
    return {"verified": bool(receipts), "receipts": receipts,
            "decision_written_after_read": decision_write_seen and not wrote_before_read,
            "scope": "matching actual read result contains the complete fixture bytes"}


def run_trial(task: dict, case: dict, expected: dict, arm: str, run: int,
              args: argparse.Namespace, provider: dict | None, out: Path) -> dict:
    evidence = out / arm / "runs" / case["id"] / f"run-{run}"
    for name in ("answer.md", "traces.json", "engine.log", "result.json"):
        private_write(evidence / name, "")
    secrets = [provider.get("api_key", "")] if provider else []
    engines = []

    class CapturedEngine(PrivateEngine):
        def __init__(self, *a, **kw):
            super().__init__(*a, **kw)
            engines.append(self)
            self.final_calls = None
            self.final_calls_error = ""

        def stop(self):
            super().stop()
            if self.proc is not None and self.proc.poll() is None:
                self.final_calls_error = "owned engine has not exited"
                return
            try:
                # Only our just-exited child's private DB is read; no Store.Open,
                # schema migration, production directory or writable connection.
                with contextlib.closing(sqlite3.connect((self.data_dir / "zwai.db").as_uri() + "?mode=ro", uri=True)) as db:
                    db.row_factory = sqlite3.Row
                    self.final_calls = [dict(row) for row in db.execute("SELECT * FROM llm_calls ORDER BY id")]
                private_write(evidence / "final-calls.json", grade.redact(encoded(self.final_calls), secrets))
            except (OSError, sqlite3.Error) as exc:
                self.final_calls_error = f"{type(exc).__name__}: {exc}"

    def private_copy(src: Path, dest: Path, limit_bytes: int = 5 << 20) -> None:
        for path in src.rglob("*"):
            rel = path.relative_to(src)
            if path.is_symlink() or not path.is_file() or path.stat().st_size > limit_bytes:
                continue
            if any(part in {".git", "node_modules", "__pycache__"} for part in rel.parts):
                continue
            data = path.read_bytes()
            for secret in secrets:
                if secret and len(secret) >= 4:
                    data = data.replace(secret.encode(), b"<redacted>")
            private_write(dest / rel, data)

    original_engine, original_copy = harness.Engine, harness.copy_workspace
    try:
        # Execution is intentionally serial; restore adapters before another run.
        harness.Engine, harness.copy_workspace = CapturedEngine, private_copy
        record = harness.run_once(task, run, args, provider, out / arm)
    finally:
        harness.Engine, harness.copy_workspace = original_engine, original_copy
    record.update({"arm": arm, "case": case["id"],
        "measurement": "constructed observation decision trial", "structural_passed": record.get("passed", False),
        "cleanup": {"engine_exited": all(e.proc is None or e.proc.poll() is not None for e in engines),
                    "exit_statuses": [e.proc.returncode if e.proc else None for e in engines],
                    "private_data_removed": all(not e.data_dir.exists() for e in engines)}})
    record["quality"] = grade_decision(evidence / "workspace" / "decision.json", case, expected)
    record["automatic_passed"] = record["structural_passed"] and record["quality"]["automatic_passed"]
    record["semantic_status"] = "unreviewed"
    traces_path = evidence / "traces.json"
    traces = json.loads(traces_path.read_text()) if traces_path.stat().st_size else []
    known_paths = tuple(e.data_dir / "workspaces" / t["turn"]["thread_id"] / "evidence.json"
                        for e in engines for t in traces if t.get("turn", {}).get("thread_id"))
    record["fixture_read"] = fixture_read_receipts(traces, case["observations"], known_paths)
    record["automatic_passed"] = record["automatic_passed"] and record["fixture_read"]["verified"] and record["fixture_read"]["decision_written_after_read"]
    calls = engines[-1].final_calls if engines else None
    record["final_call_snapshot"] = {"available": calls is not None,
        "error": engines[-1].final_calls_error if engines else "engine was not constructed",
        "pre_stop_calls": sum(len(t.get("llm_calls", [])) for t in traces),
        "post_stop_calls": len(calls) if calls is not None else None}
    metric_traces = [{**t, "llm_calls": []} for t in traces]
    if calls is not None:
        if metric_traces:
            metric_traces[0]["llm_calls"] = calls
        else:
            metric_traces = [{"turn": {}, "events": [], "llm_calls": calls}]
        record["metrics"] = grade.extract_metrics(metric_traces)
    else:
        metric_traces = traces
    record["cache"] = cache_observations(metric_traces)
    log = (evidence / "engine.log").read_text(errors="replace")
    cutoff_messages = ("a session briefing was still running at shutdown",
        "a memory review was still running at shutdown", "a conversation was still being named at shutdown",
        "a compact was still running at shutdown", "could not record a model call")
    record["cost_incomplete_reasons"] = [message for message in cutoff_messages if message in log]
    if calls is None:
        record["cost_incomplete_reasons"].append("final private DB call snapshot unavailable")
    if not calls or any(not isinstance(c.get("total_tokens"), (int, float)) or c["total_tokens"] <= 0 for c in calls):
        record["cost_incomplete_reasons"].append("usage missing or zero")
    if any(e.proc is None or e.proc.returncode != 0 for e in engines):
        record["cost_incomplete_reasons"].append("engine did not exit normally")
    record["cost_complete"] = not record["cost_incomplete_reasons"]
    tools = {grade.parse_tool_call(ev.get("text", ""))[0] for t in traces for ev in t.get("events", []) if ev.get("kind") == "tool_call"}
    record["cleanup"]["descendant_cleanup"] = "unverified_exec_or_spawn" if tools & {"exec", "spawn_agent", "resume_agent"} else "no_exec_or_spawn_observed"
    record["automatic_passed"] = record["automatic_passed"] and not tools & {"exec", "spawn_agent", "resume_agent"}
    private_write(evidence / "semantic-review.json", encoded({"status": "unreviewed",
        "items": [{"rule": item, "status": "unreviewed", "answer_evidence": ""}
                  for item in expected.get("semantic_rubric", [])]}))
    private_write(evidence / "result.json", grade.redact(encoded(record), secrets))
    return record


def summarize(records: list[dict]) -> dict:
    summary = {}
    for arm in ("a", "b"):
        rows = [r for r in records if r["arm"] == arm]
        graded = [r for r in rows if not r.get("harness_error")]
        metrics = [r.get("metrics", {}) for r in graded]
        summary[arm] = {"graded": len(graded), "harness_errors": len(rows) - len(graded),
            "automatic_passed": sum(r.get("automatic_passed", False) for r in graded),
            "excess_notifications": sum(bool(r.get("quality", {}).get("excess_notification")) for r in graded),
            "missed_notifications": sum(bool(r.get("quality", {}).get("missed_notification")) for r in graded),
            "critical_failures": sum(r.get("quality", {}).get("critical", False) and not r.get("automatic_passed") for r in graded),
            "reviews_cut_off": sum(bool(r.get("review_cut_off")) for r in rows),
            "cost_incomplete": sum(not r.get("cost_complete", False) for r in rows),
            "total_tokens": sum(m.get("total_tokens", 0) for m in metrics),
            "median_tokens": statistics.median([m.get("total_tokens", 0) for m in metrics]) if metrics else None,
            "median_seconds": statistics.median([m.get("duration_ms", 0) / 1000 for m in metrics]) if metrics else None,
            "by_caller": {}, "cache": {key: sum(r.get("cache", {}).get(key, 0) for r in rows) for key in ("field_missing", "zero", "positive")}}
        for metric in metrics:
            for caller, values in metric.get("calls", {}).items():
                slot = summary[arm]["by_caller"].setdefault(caller, {})
                for key, value in values.items():
                    slot[key] = slot.get(key, 0) + value
    return summary


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--provider")
    parser.add_argument("--config", default=str(harness.DEFAULT_CONFIG))
    parser.add_argument("--model", default="")
    parser.add_argument("--zwai", default=str(REPO / "bin" / "zwai-eval"))
    parser.add_argument("--fixture-root", default=str(FIXTURE))
    parser.add_argument("--cases", default="", help="comma-separated case ids")
    parser.add_argument("--runs", type=int, default=2)
    parser.add_argument("--seed", type=int, default=20261101)
    parser.add_argument("--timeout", type=float, default=180)
    parser.add_argument("--boot-timeout", type=float, default=240)
    parser.add_argument("--review-wait", type=float, default=10, help="cut-off reviews make cost incomplete")
    parser.add_argument("--max-completion-tokens", type=int, default=0)
    parser.add_argument("--out", help="new private evidence directory, outside repository")
    parser.add_argument("--mock", action="store_true", help="infrastructure only, never effectiveness evidence")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--plan-only", action="store_true")
    mode.add_argument("--execute-plan", help="frozen manifest.json to verify and execute once")
    args = parser.parse_args(argv)
    if args.runs < 1 or args.timeout <= 0 or args.boot_timeout <= 0 or args.review_wait < 0 or args.max_completion_tokens < 0:
        parser.error("invalid run count or time/token budget")
    if not args.execute_plan and not args.out:
        parser.error("--out is required unless --execute-plan is supplied")
    if not args.mock and not args.provider:
        parser.error("--provider is required unless --mock")
    return args


def render_report(report: dict) -> str:
    lines = ["# Constructed observation decision trial", "",
        "Automatic quality checks are separate from independent semantic review. This trial does not accept production scheduling or notifications.", "",
        "| arm | automatic pass/graded | harness errors | excess notifications | missed notifications | tokens | median s | incomplete costs |",
        "|---|---|---|---|---|---|---|---|"]
    for arm, row in report["summary"].items():
        lines.append(f"| {arm} | {row['automatic_passed']}/{row['graded']} | {row['harness_errors']} | {row['excess_notifications']} | {row['missed_notifications']} | {row['total_tokens']} | {row['median_seconds']} | {row['cost_incomplete']} |")
    lines += ["", "Semantic status: unreviewed. Review each saved semantic-review.json against evidence and decision.json.",
        "Cache fields are Trace observations: missing, zero, or positive; persisted zero cannot prove upstream explicit zero.",
        "Costs include all recorded callers; cut-off reviews or missing usage forbid a cost-improvement claim.",
        "Repeats are nested within cases; any confidence interval must resample cases, not individual runs."]
    if report.get("stopped_early"):
        lines += ["", "Stopped before the next round because cleanup was not verified. This is an execution-evidence failure, not a triage-quality finding."]
    return "\n".join(lines) + "\n"


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    harness.unwind_on_termination()
    provider = None if args.mock else grade.select_model(grade.extract_provider(Path(args.config).read_text(), args.provider), args.model)
    provider_info = grade.provider_summary(provider) if provider else {"mock": True}
    if args.execute_plan:
        manifest_path = Path(args.execute_plan).resolve()
        out = manifest_path.parent
        manifest = json.loads(manifest_path.read_text())
        if manifest["provider"] != provider_info or manifest["provider_config_sha256"] != provider_config_digest(provider):
            raise harness.EvalError("effective provider conditions differ from frozen plan")
        for name, value in manifest["conditions"].items():
            setattr(args, name, value)
        args.zwai, args.fixture_root = manifest["binary"]["path"], manifest["fixture_root"]
        for path, expected in manifest["hashes"].items():
            if digest(Path(path)) != expected:
                raise harness.EvalError(f"frozen input changed: {path}")
        if (out / "report.json").exists() or any((out / a / "runs").exists() for a in ("a", "b")):
            raise harness.EvalError("plan already executed; preserve its first observations")
        raw, oracle = load_fixture(Path(args.fixture_root))
        selected = {row["case"] for row in manifest["order"]}
        cases = [case for case in raw["cases"] if case["id"] in selected]
        tasks = {arm: {case["id"]: grade.load_task(out / "tasks" / arm / case["id"]) for case in cases} for arm in ("a", "b")}
    else:
        raw, oracle = load_fixture(Path(args.fixture_root))
        selected = set(args.cases.split(",")) if args.cases else {case["id"] for case in raw["cases"]}
        if selected - {case["id"] for case in raw["cases"]}:
            raise harness.EvalError("unknown --cases id")
        cases = [case for case in raw["cases"] if case["id"] in selected]
        out = Path(args.out).resolve()
        if out.is_relative_to(REPO):
            raise harness.EvalError("evidence must be outside the repository")
        out.mkdir(mode=0o700, parents=True, exist_ok=False)
        tasks = build_tasks(cases, out / "tasks", args.timeout)
        binary = Path(args.zwai).resolve()
        fixture_root = Path(args.fixture_root).resolve()
        paths = [binary, fixture_root / "cases.json", fixture_root / "expectations.json",
                 Path(__file__).resolve(), Path(harness.__file__).resolve(), Path(grade.__file__).resolve()]
        paths += [p for p in (out / "tasks").rglob("*") if p.is_file()]
        manifest = {"measurement": "constructed observation decision trial", "schema_version": 1,
            "provider": provider_info, "provider_config_sha256": provider_config_digest(provider),
            "binary": {"path": str(binary), "sha256": digest(binary)},
            "fixture_root": str(fixture_root), "hashes": {str(p): digest(p) for p in paths},
            "conditions": {key: getattr(args, key) for key in ("mock", "boot_timeout", "review_wait", "max_completion_tokens")},
            "order": execution_order([c["id"] for c in cases], args.runs, args.seed), "seed": args.seed,
            "source": grade.git_describe(REPO), "subprocess_umask": "inherited unchanged"}
        private_write(out / "manifest.json", encoded(manifest))
    if args.plan_only:
        print(out / "manifest.json")
        return 0
    report = {"meta": manifest, "results": [], "semantic_status": "unreviewed"}
    case_map = {case["id"]: case for case in cases}
    try:
        for row in manifest["order"]:
            print(f"[{row['case']} {row['arm']} run {row['run']}] ...", flush=True)
            result = run_trial(tasks[row["arm"]][row["case"]], case_map[row["case"]], oracle["cases"][row["case"]], row["arm"], row["run"], args, provider, out)
            report["results"].append(result)
            print(f"automatic_passed={result['automatic_passed']} harness_error={bool(result.get('harness_error'))}", flush=True)
            cleanup = result.get("cleanup", {})
            if (not cleanup.get("engine_exited") or not cleanup.get("private_data_removed")
                    or cleanup.get("descendant_cleanup") != "no_exec_or_spawn_observed"):
                report["stopped_early"] = {"reason": "cleanup_unverified", "case": row["case"],
                    "arm": row["arm"], "run": row["run"],
                    "remaining_runs": len(manifest["order"]) - len(report["results"])}
                break
    finally:
        report["summary"] = summarize(report["results"])
        private_write(out / "report.json", encoded(report))
        private_write(out / "report.md", render_report(report))
    print(out / "report.md")
    return 2 if report.get("stopped_early") or any(r.get("harness_error") for r in report["results"]) else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
