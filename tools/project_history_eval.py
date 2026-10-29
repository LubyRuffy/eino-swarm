#!/usr/bin/env python3
"""Opt-in real-model project-history measurement on synthetic conversations.

The same natural request and raw fixture run on each identified binary. Only
the selected provider is copied from config; no real conversations are read.
SQL is chosen by the model through query_history, never supplied by this
harness. Structural checks prove retrieval, not correctness of the prose:
independent semantic rubrics remain unreviewed until a human/agent audits the
saved answer and cited raw records. --mock is infrastructure smoke only.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import shutil
import sqlite3
import sys
import tempfile
import time
import traceback
from pathlib import Path
from typing import Any

import agent_eval as harness
import agent_eval_grade as grade

REPO = Path(__file__).resolve().parent.parent
FIXTURE = REPO / "evals" / "project-history"
OWNER = ".project-history-eval-owner"


class PrivateEngine(harness.Engine):
    """Private default paths, while the product's full-access model remains."""

    def child_env(self) -> dict[str, str]:
        env = super().child_env()
        home = self.data_dir / "home"
        home.mkdir(mode=0o700, exist_ok=True)
        env.update({"HOME": str(home), "XDG_CONFIG_HOME": str(home / ".config"),
                    "XDG_CACHE_HOME": str(home / ".cache"), "XDG_DATA_HOME": str(home / ".local/share"),
                    "ZDOTDIR": str(home)})
        return env


def private_write(path: Path, value: str) -> None:
    """Explicit file permissions without changing the compiler/child umask."""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        os.fchmod(fh.fileno(), 0o600)
        fh.write(value)


def encoded(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load_fixture(root: Path) -> tuple[dict, dict]:
    raw = json.loads((root / "raw.json").read_text())
    expected = json.loads((root / "expectations.json").read_text())
    if raw.get("synthetic") is not True or raw.get("version") != 1:
        raise harness.EvalError("only versioned synthetic fixtures may be seeded")
    cases = raw.get("cases") or []
    if not cases or len({c["id"] for c in cases}) != len(cases):
        raise harness.EvalError("fixture needs unique case ids")
    for case in cases:
        rows = [m for th in case["threads"] for m in th["messages"]]
        ids = {m["id"] for m in rows}
        if len(ids) != len(rows) or not case.get("prompt"):
            raise harness.EvalError(f"{case['id']}: duplicate source id or missing prompt")
        rubric = expected["cases"][case["id"]]
        current = {m["id"] for th in case["threads"] if th["project"] == "current"
                   for m in th["messages"]}
        refs = {s for group in rubric["required_sources"] for s in group}
        refs |= {s for item in rubric["rubric"] for s in item["sources"]}
        if not refs <= current:
            raise harness.EvalError(f"{case['id']}: rubric cites missing/foreign sources")
        for th in case["threads"]:
            if th["project"] not in ("current", "other") or len(th["messages"]) % 2:
                raise harness.EvalError("fixture conversations need paired user/assistant messages")
            if [m["role"] for m in th["messages"]] != ["user", "assistant"] * (len(th["messages"]) // 2):
                raise harness.EvalError("fixture roles must alternate user/assistant")
    return raw, expected


def create_conversations(base: str, case: dict) -> dict:
    """Use real HTTP creation; explicit titles prevent unrelated title calls."""
    projects = {}
    for scope, name in (("current", case["project"]), ("other", "独立干扰项目")):
        projects[scope] = harness.http_json(base, "POST", "/api/projects", {
            "name": name, "memory_enabled": False,
        })["project"]["id"]
    threads = {}
    for th in case["threads"]:
        threads[th["key"]] = harness.http_json(base, "POST", "/api/threads", {
            "title": th["title"], "project_id": projects[th["project"]],
        })["thread"]["id"]
    target = harness.http_json(base, "POST", "/api/threads", {
        "title": "项目接口提炼", "project_id": projects["current"],
    })["thread"]["id"]
    return {"projects": projects, "threads": threads, "target": target}


def seed_raw_history(data_dir: Path, case: dict, identities: dict) -> list[dict]:
    """Insert only frozen synthetic text while the private engine is stopped.

    The owner marker and expected empty history prevent accidentally seeding a
    user's existing database. Turns are terminal; no restoration runs models.
    """
    if not (data_dir / OWNER).is_file() or (data_dir / OWNER).read_text() != "synthetic-v1\n":
        raise harness.EvalError("refusing history seed without private owner marker")
    source_map = []
    with sqlite3.connect(data_dir / "zwai.db") as db:
        if db.execute("SELECT COUNT(*) FROM messages").fetchone()[0]:
            raise harness.EvalError("refusing to seed nonempty message history")
        for th in case["threads"]:
            tid = identities["threads"][th["key"]]
            for index in range(0, len(th["messages"]), 2):
                user, assistant = th["messages"][index:index + 2]
                turn_id = f"tn_eval_{case['id']}_{th['key']}_{index // 2}"
                db.execute("INSERT INTO turns (id,thread_id,seq,status,user_text,final,started_at,ended_at) "
                           "VALUES (?,?,?,'done',?,?,?,?)",
                           (turn_id, tid, index // 2 + 1, user["content"], assistant["content"],
                            user["at"], assistant["at"]))
                for offset, message in enumerate((user, assistant), start=index + 1):
                    cursor = db.execute("INSERT INTO messages "
                                        "(thread_id,turn_id,seq,role,agent_id,source,content,created_at) "
                                        "VALUES (?,?,?,?,?,?,?,?)",
                                        (tid, turn_id, offset, message["role"], "manager",
                                         "human" if message["role"] == "user" else "",
                                         message["content"], message["at"]))
                    db.execute("INSERT INTO events "
                               "(thread_id,turn_id,seq,kind,agent_id,text,created_at) VALUES (?,?,?,?,?,?,?)",
                               (tid, turn_id, offset,
                                "user_message" if message["role"] == "user" else "agent_message",
                                "manager", message["content"], message["at"]))
                    source_map.append({"source_id": message["id"], "project": th["project"],
                                       "thread_id": tid, "title": th["title"], "seq": offset,
                                       "message_id": cursor.lastrowid, "turn_id": turn_id,
                                       "role": message["role"], "at": message["at"]})
            db.execute("UPDATE threads SET created_at=?,updated_at=?,last_active_at=? WHERE id=?",
                       (th["messages"][0]["at"], th["messages"][-1]["at"], th["messages"][-1]["at"], tid))
        # Startup's real Store.ensureSearch rebuilds an empty derived index.
        # Leaving title-only API-created docs would handicap the original arm's
        # existing keyword-search path after our offline raw-message seed.
        db.execute("DELETE FROM search_docs")
    return source_map


def structural_review(traces: list[dict], case: dict, expected: dict) -> dict:
    """Do not infer prose correctness from tool counts, snippets or keywords."""
    queries, retrievals, by_call, tool_paths, other_results, write_arguments = [], [], {}, {}, [], []
    for trace in traces:
        for ev in trace.get("events") or []:
            call_id = (trace.get("turn", {}).get("id"), ev.get("agent_id"), ev.get("tool_call_id"))
            if ev.get("kind") == "tool_call":
                name, args = grade.parse_tool_call(ev.get("text", ""))
                by_call[call_id] = (name, args)
                tool_paths[name] = tool_paths.get(name, 0) + 1
                if name in ("write", "edit"):
                    write_arguments.append(json.dumps(args, ensure_ascii=False))
                if name == "query_history":
                    queries.append({"agent_id": ev.get("agent_id"), "call_id": ev.get("tool_call_id"),
                                    "args": args})
            elif ev.get("kind") == "tool_result" and by_call.get(call_id, (None,))[0] not in (None, "query_history"):
                other_results.append(ev.get("text", ""))
            elif ev.get("kind") == "tool_result" and by_call.get(call_id, (None,))[0] == "query_history":
                args = by_call[call_id][1]
                try:
                    result = json.loads(ev.get("text", ""))
                except (TypeError, json.JSONDecodeError):
                    continue
                if isinstance(result, dict) and result.get("ok") is True and isinstance(args, dict) \
                        and str(args.get("sql", "")).strip() and isinstance(result.get("rows"), list):
                    retrievals.append({"args": args, "result": result, "agent_id": ev.get("agent_id")})
    returned = json.dumps([r["result"]["rows"] for r in retrievals], ensure_ascii=False)
    # Exact full-record text establishes which raw sources were returned. SQL
    # substr/aggregate results may also be useful, but need manual trace review.
    covered, other_covered = [], []
    other_returned = "\n".join(other_results)
    for thread in case["threads"]:
        if thread["project"] != "current":
            continue
        for message in thread["messages"]:
            if message["content"] in returned:
                covered.append(message["id"])
            if message["content"] in other_returned:
                other_covered.append(message["id"])
    groups = expected["required_sources"]
    coverage = [any(source in covered for source in group) for group in groups]
    leaked_query = [marker for marker in expected["foreign_markers"] if marker in returned]
    leaked_other = [marker for marker in expected["foreign_markers"] if marker in other_returned]
    leaked_write = [marker for marker in expected["foreign_markers"] if marker in "\n".join(write_arguments)]
    leaked_answer = [marker for marker in expected["foreign_markers"] if marker in grade.final_answer(traces)]
    leaked = sorted(set(leaked_query + leaked_other + leaked_write + leaked_answer))
    return {"sql_calls": len(queries), "successful_sql_results": len(retrievals),
            "queries": queries, "tool_call_counts": tool_paths,
            "covered_full_source_ids": covered,
            "other_tool_full_source_ids": other_covered,
            "any_tool_full_source_ids": sorted(set(covered + other_covered)),
            "required_source_groups_seen": coverage, "all_required_sources_seen": all(coverage),
            "foreign_markers_leaked": leaked,
            "query_results_foreign_markers": leaked_query, "answer_foreign_markers": leaked_answer,
            "other_tool_results_foreign_markers": leaked_other,
            "write_arguments_foreign_markers": leaked_write,
            "structural_retrieval_ok": bool(retrievals) and all(coverage) and not leaked,
            "semantic_status": "unreviewed",
            "limits": "Full-source coverage measures retrieval only. Substring paging and prose require independent review."}


def semantic_review_form(expected: dict) -> dict:
    return {"status": "unreviewed", "notice": "Audit answer.md against raw.json, source-map.json and traces.json. "
            "Do not accept retrieval coverage as answer correctness. Preserve failed observations.",
            "items": [{**item, "status": "unreviewed", "answer_evidence": "", "notes": ""}
                      for item in expected["rubric"]]}


def apply_reasoning_condition(base: str, thread_id: str, effort: str, receipt: dict) -> None:
    """Change only this fresh private target, never the user's global config."""
    receipt["requested"] = effort
    if not effort:
        return
    patched = harness.http_json(base, "PATCH", f"/api/threads/{thread_id}", {"reasoning_effort": effort})
    receipt["patch_readback"] = (patched.get("thread") or {}).get("reasoning_effort")
    view = harness.http_json(base, "GET", f"/api/threads/{thread_id}")
    thread = view.get("thread") or {}
    receipt["readback_thread_id"] = thread.get("id")
    receipt["readback"] = thread.get("reasoning_effort")
    if receipt["readback_thread_id"] != thread_id or receipt["readback"] != effort:
        raise harness.EvalError("private target reasoning-effort readback mismatch")
    if receipt["patch_readback"] is not None and receipt["patch_readback"] != effort:
        raise harness.EvalError("private target reasoning-effort PATCH receipt mismatch")


def verify_reasoning_trace(traces: list[dict], effort: str, receipt: dict) -> None:
    """Check recorded application state; absent call fields are not invented."""
    if not effort:
        return
    turns = [t.get("turn") or {} for t in traces]
    receipt["turns"] = [{"id": t.get("id"), "reasoning_effort": t.get("reasoning_effort")} for t in turns]
    if not turns or any(t.get("reasoning_effort") != effort for t in turns):
        raise harness.EvalError("Trace turn reasoning-effort mismatch")
    calls = [c for t in traces for c in t.get("llm_calls") or [] if c.get("agent_id") not in grade.AUX_AGENTS]
    receipt["model_calls"] = [{"id": c.get("id"), "agent_id": c.get("agent_id"),
                               "effort_field_recorded": "reasoning_effort" in c,
                               "reasoning_effort": c.get("reasoning_effort"),
                               "reasoning_tokens": c.get("reasoning_tokens")} for c in calls]
    if any("reasoning_effort" in c and c["reasoning_effort"] != effort for c in calls):
        raise harness.EvalError("Trace model-call reasoning-effort mismatch")
    receipt["turn_effort_verified"] = True
    receipt["limits"] = "Application requested effort only; HTTP acceptance or reasoning-token counts do not prove upstream adoption or extra internal computation."


def run_case(binary: Path, label: str, case: dict, expected: dict, args: argparse.Namespace,
             provider: dict | None, evidence: Path) -> dict:
    data_dir = Path(tempfile.mkdtemp(prefix="zwai-project-history-eval-"))
    secrets = [provider["api_key"]] if provider and provider.get("api_key") else []
    engine = PrivateEngine(binary, data_dir, args.mock, args.boot_timeout)
    record = {"label": label, "case": case["id"], "mode": "mock-smoke" if args.mock else "real-model",
              "binary": {"path": str(binary)},
              "harness_error": "", "semantic_status": "unreviewed",
              "reasoning_condition": {"requested": getattr(args, "reasoning_effort", "")}}
    started = time.monotonic()
    target = ""

    def save(name: str, value: str) -> None:
        private_write(evidence / name, grade.redact(value, secrets))

    try:
        record["binary"]["sha256"] = digest(binary)
        private_write(data_dir / OWNER, "synthetic-v1\n")
        private_write(engine.log_path, "")
        if provider:
            config = grade.render_eval_config(provider, harness.EVAL_PROVIDER_ID, args.max_completion_tokens)
            private_write(data_dir / "config.yaml", config + "memory:\n    enabled: false\n")
        engine.start()
        identities = create_conversations(engine.base, case)
        engine.stop()
        save("bootstrap.log", engine.log_path.read_text(errors="replace"))
        source_map = seed_raw_history(data_dir, case, identities)
        save("source-map.json", encoded({"identities": identities, "sources": source_map}))
        # A fresh process owns fresh sequence counters after the offline seed.
        engine = PrivateEngine(binary, data_dir, args.mock, args.boot_timeout)
        engine.start()
        target = identities["target"]
        apply_reasoning_condition(engine.base, target, getattr(args, "reasoning_effort", ""), record["reasoning_condition"])
        harness.http_json(engine.base, "POST", f"/api/threads/{target}/turns", {"text": case["prompt"]})
        outcome = harness.drive_conversation(engine.base, target, {"timeout_s": args.timeout})
        traces = harness.fetch_traces(engine.base, target)
        outcome = harness.settled_outcome(outcome, traces)
        save("answer.md", grade.final_answer(traces))
        save("traces.json", encoded(traces))
        save("semantic-review.json", encoded(semantic_review_form(expected)))
        record["outcome"] = outcome
        verify_reasoning_trace(traces, getattr(args, "reasoning_effort", ""), record["reasoning_condition"])
        record.update({"outcome": outcome, "retrieval": structural_review(traces, case, expected),
                       "metrics": grade.extract_metrics(traces)})
    except BaseException as exc:
        record["harness_error"] = grade.redact(f"{type(exc).__name__}: {exc}", secrets)
        try:
            save("error.txt", traceback.format_exc())
        except Exception as error_storage:
            record["error_evidence_error"] = grade.redact(f"{type(error_storage).__name__}: {error_storage}", secrets)
        # Evidence collection precedes destructive cleanup, including Ctrl-C.
        # Keep the original failure if a second failure prevents collection.
        if target and engine.base:
            try:
                traces = harness.fetch_traces(engine.base, target)
                save("traces-at-failure.json", encoded(traces))
                save("answer-at-failure.md", grade.final_answer(traces))
            except Exception as evidence_error:
                record["failure_evidence_error"] = grade.redact(
                    f"{type(evidence_error).__name__}: {evidence_error}", secrets)
        if not isinstance(exc, Exception):
            raise
    finally:
        try:
            engine.stop()
            record["cleanup"] = {"engine_exit": engine.proc.returncode if engine.proc else None,
                                 "private_data_removed": False}
            if engine.log_path.exists():
                save("engine.log", engine.log_path.read_text(errors="replace"))
            shutil.rmtree(data_dir)
            record["cleanup"]["private_data_removed"] = True
        except Exception as exc:
            record["cleanup_error"] = grade.redact(f"{type(exc).__name__}: {exc}", secrets)
        record["wall_s"] = round(time.monotonic() - started, 2)
        save("result.json", encoded(record))
    return record


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--provider", help="provider id copied read-only from --config")
    p.add_argument("--model", default="")
    p.add_argument("--config", type=Path, default=harness.DEFAULT_CONFIG)
    p.add_argument("--zwai", type=Path, default=REPO / "bin" / "zwai-eval")
    p.add_argument("--baseline-zwai", type=Path, help="optional original binary; one raw run per case")
    p.add_argument("--arm", choices=("baseline", "candidate"), default="candidate",
                   help="label when measuring one binary separately; baseline runs once per case")
    p.add_argument("--fixture", type=Path, default=FIXTURE)
    p.add_argument("--out", type=Path, default=REPO / "output" / "project-history-eval")
    p.add_argument("--runs", type=int, default=1, help="candidate repetitions per independent case")
    p.add_argument("--cases", default="", help="comma-separated case ids; default both independent cases")
    p.add_argument("--timeout", type=float, default=600)
    p.add_argument("--boot-timeout", type=float, default=90)
    p.add_argument("--max-completion-tokens", type=int, default=0)
    p.add_argument("--reasoning-effort", choices=("low", "medium", "high"), default="",
                   help="private target conversation setting; omitted keeps the engine default; not proof of upstream adoption")
    p.add_argument("--mock", action="store_true", help="offline infrastructure smoke; never an efficacy result")
    args = p.parse_args(argv)
    if not args.mock and not args.provider:
        p.error("--provider is required unless --mock")
    if args.runs < 1 or args.timeout <= 0 or args.boot_timeout <= 0 or args.max_completion_tokens < 0:
        p.error("runs/timeouts must be positive and output cap nonnegative")
    if args.arm == "baseline" and args.baseline_zwai:
        p.error("--arm baseline cannot also compare --baseline-zwai")
    return args


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    harness.unwind_on_termination()
    raw, expected = load_fixture(args.fixture)
    selected = {s for s in args.cases.split(",") if s}
    known = {case["id"] for case in raw["cases"]}
    if selected - known:
        raise harness.EvalError("unknown case ids: " + ", ".join(sorted(selected - known)))
    provider = None if args.mock else grade.select_model(
        grade.extract_provider(args.config.read_text(), args.provider), args.model)
    root = args.out / dt.datetime.now(dt.timezone.utc).strftime("%Y%m%d-%H%M%S-%f")
    root.mkdir(parents=True, mode=0o700)
    private_write(root / "raw.json", encoded(raw))
    private_write(root / "expectations.json", encoded(expected))
    runs = []
    arms = []
    if args.baseline_zwai:
        arms.append(("baseline", args.baseline_zwai.resolve(), 1))
    arms.append((args.arm, args.zwai.resolve(), 1 if args.arm == "baseline" else args.runs))
    for label, binary, count in arms:
        for case in raw["cases"]:
            if selected and case["id"] not in selected:
                continue
            for index in range(1, count + 1):
                print(f"{label}/{case['id']}/{index}: running", flush=True)
                rec = run_case(binary, label, case, expected["cases"][case["id"]], args, provider,
                               root / label / case["id"] / f"run-{index}")
                runs.append(rec)
                print(f"{label}/{case['id']}/{index}: " +
                      ("harness error" if rec.get("harness_error") else "semantic review pending"), flush=True)
    report = {"mode": "mock-smoke" if args.mock else "real-model", "semantic_status": "unreviewed",
              "reasoning_effort": args.reasoning_effort or "unset",
              "fixture_sha256": digest(args.fixture / "raw.json"),
              "rubric_sha256": digest(args.fixture / "expectations.json"),
              "source": grade.git_describe(REPO), "runs": runs,
              "notice": "No automatic efficacy verdict. Review every semantic-review.json, including baseline and failures."}
    private_write(root / "report.json", encoded(report))
    print(str(root), flush=True)
    return 1 if any(r.get("harness_error") or r.get("cleanup_error") for r in runs) else 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
