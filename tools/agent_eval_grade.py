"""Task loading, grading, metrics and reports for tools/agent_eval.py.

Everything here works on files and already-fetched trace JSON so it can be
tested without an engine or a model. Checks assert structure and values that
the task's fixture determines; they never match a model's wording.
"""

from __future__ import annotations

import hashlib
import json
import math
import re
import statistics
import subprocess
from collections import Counter
from pathlib import Path
from typing import Any

MANAGER_ID = "manager"
# Host-side model calls outside the manager/worker loop, keyed by the
# agent_id the engine records on llm_calls.
AUX_AGENTS = (
    "title-namer",
    "memory-reviewer",
    "schedule-answer-policy",
    "thread-wake-policy",
    "session-memory",
    "compact-summarizer",
)
DELEGATION = ("none", "optional", "parallel")
CHECK_TYPES = (
    "file_exists",
    "file_regex",
    "json_equals",
    "file_unchanged",
    "command",
    "answer_regex",
    "answer_chart",
    "answer_contains_file",
    "tool_count",
    "event_count",
    "any_of",
)
_PROVIDER_SCALARS = (
    "base_url",
    "api_key",
    "model",
    "api",
    "timeout_seconds",
    "first_byte_timeout_seconds",
    "context_window",
)


class TaskError(ValueError):
    pass


# ---------------------------------------------------------------- tasks


def load_task(task_dir: Path) -> dict:
    spec = json.loads((task_dir / "task.json").read_text())
    prompt_file = task_dir / "prompt.md"
    prompt = prompt_file.read_text().strip() if prompt_file.exists() else str(spec.get("prompt", "")).strip()
    if not prompt:
        raise TaskError(f"{task_dir.name}: empty prompt")
    if spec.get("id", task_dir.name) != task_dir.name:
        raise TaskError(f"{task_dir.name}: id must match its directory")
    checks = spec.get("checks") or []
    if not checks:
        raise TaskError(f"{task_dir.name}: no checks")
    for check in checks:
        _validate_check(task_dir.name, check)
    delegation = spec.get("expect_delegation", "optional")
    if delegation not in DELEGATION:
        raise TaskError(f"{task_dir.name}: expect_delegation must be one of {DELEGATION}")
    for skill in spec.get("skills", []):
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", skill.get("name", "")):
            raise TaskError(f"{task_dir.name}: invalid skill name {skill.get('name')!r}")
        if not (task_dir / skill.get("file", "")).is_file():
            raise TaskError(f"{task_dir.name}: missing skill file {skill.get('file')!r}")
    return {
        "id": task_dir.name,
        "dir": task_dir,
        "prompt": prompt,
        "checks": checks,
        "expect_delegation": delegation,
        "skills": spec.get("skills", []),
        "generate": spec.get("generate", ""),
        "timeout_s": float(spec.get("timeout_s", 900)),
        "allow_wait": bool(spec.get("allow_wait", False)),
        "wait_timeout_s": float(spec.get("wait_timeout_s", 300)),
        "purpose": spec.get("purpose", ""),
        "replay": _validated_replay(task_dir.name, spec.get("replay") or []),
    }


REPLAY_KINDS = ("steer", "answer", "turn")


def _validated_replay(task_id: str, replay: list) -> list[dict]:
    out = []
    for i, entry in enumerate(replay):
        if not isinstance(entry, dict) or entry.get("kind") not in REPLAY_KINDS:
            raise TaskError(f"{task_id}: replay[{i}] kind must be one of {REPLAY_KINDS}")
        if not str(entry.get("text", "")).strip():
            raise TaskError(f"{task_id}: replay[{i}] has no text")
        after = entry.get("after_s", 0)
        if not isinstance(after, (int, float)) or after < 0:
            raise TaskError(f"{task_id}: replay[{i}] after_s must be a non-negative number")
        out.append({"kind": entry["kind"], "text": str(entry["text"]), "after_s": float(after)})
    return out


def _validate_check(task_id: str, check: dict) -> None:
    kind = check.get("type")
    if kind not in CHECK_TYPES:
        raise TaskError(f"{task_id}: unknown check type {kind!r}")
    if kind == "any_of":
        if not check.get("checks"):
            raise TaskError(f"{task_id}: any_of needs checks")
        for sub in check["checks"]:
            _validate_check(task_id, sub)
    if kind in ("file_exists", "file_regex", "json_equals", "file_unchanged", "answer_contains_file") \
            and not check.get("path"):
        raise TaskError(f"{task_id}: {kind} needs path")
    if kind in ("file_regex", "answer_regex") and not check.get("pattern"):
        raise TaskError(f"{task_id}: {kind} needs pattern")
    if kind == "command" and not check.get("argv"):
        raise TaskError(f"{task_id}: command needs argv")


# ------------------------------------------------------------- provider


def extract_provider(config_text: str, provider_id: str) -> dict:
    """Reads one provider from the engine's own config.yaml serialization.

    Only scalar fields and the model_context map are read. This is not a YAML
    parser: the input is the file the engine writes, and anything that does
    not match that shape fails instead of guessing.
    """
    lines = config_text.splitlines()
    in_providers = False
    providers_indent = -1
    item: dict | None = None
    item_indent = -1
    found: list[dict] = []
    sub_key = ""
    sub_indent = -1
    for raw in lines:
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        indent = len(raw) - len(raw.lstrip(" "))
        text = raw.strip()
        if text == "providers:":
            in_providers, providers_indent = True, indent
            continue
        if not in_providers:
            continue
        if indent <= providers_indent and not text.startswith("- "):
            break
        if text.startswith("- ") and indent <= providers_indent + 4 and ":" in text:
            item = {"model_context": {}}
            found.append(item)
            item_indent = indent + 2
            sub_key = ""
            text = text[2:]
            indent = item_indent
        if item is None:
            continue
        if sub_key and indent > sub_indent:
            if sub_key == "model_context" and ":" in text:
                k, v = text.split(":", 1)
                item["model_context"][_scalar(k)] = _int_or_str(_scalar(v))
            continue
        sub_key = ""
        if indent != item_indent or ":" not in text:
            continue
        key, value = text.split(":", 1)
        key = key.strip()
        value = value.strip()
        if value == "":
            sub_key, sub_indent = key, indent
            continue
        if key == "id" or key in _PROVIDER_SCALARS:
            item[key] = _scalar(value)
    for p in found:
        if p.get("id") == provider_id:
            if not p.get("base_url") or not p.get("model"):
                raise TaskError(f"provider {provider_id!r} has no base_url or model")
            return p
    raise TaskError(f"provider {provider_id!r} not found in config")


def _scalar(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        inner = value[1:-1]
        return json.loads(value) if value[0] == '"' else inner.replace("''", "'")
    return value


def _int_or_str(value: str) -> Any:
    try:
        return int(value)
    except ValueError:
        return value


def render_eval_config(provider: dict, provider_id: str, max_completion_tokens: int = 0) -> str:
    """A minimal config.yaml with one provider. The engine fills defaults,
    including the output cap unless max_completion_tokens sets one."""
    q = json.dumps
    out = [
        "models:",
        f"    default: {provider_id}",
        "    providers:",
        f"        - id: {provider_id}",
        f"          base_url: {q(provider['base_url'])}",
        f"          api_key: {q(provider.get('api_key', ''))}",
        f"          model: {q(provider['model'])}",
    ]
    if provider.get("api"):
        out.append(f"          api: {q(provider['api'])}")
    for key in ("timeout_seconds", "first_byte_timeout_seconds", "context_window"):
        if str(provider.get(key, "")).isdigit():
            out.append(f"          {key}: {int(provider[key])}")
    window = provider.get("model_context", {}).get(provider["model"])
    if isinstance(window, int):
        out.append("          model_context:")
        out.append(f"            {q(provider['model'])}: {window}")
    if max_completion_tokens > 0:
        out += ["swarm:", f"    max_completion_tokens: {int(max_completion_tokens)}"]
    return "\n".join(out) + "\n"


def select_model(provider: dict, model: str) -> dict:
    """The same endpoint and key with another of its models, so two models are
    compared through one provider entry instead of two hand-edited configs."""
    model = model.strip()
    if not model:
        return provider
    return {**provider, "model": model}


def provider_summary(provider: dict) -> dict:
    host = re.sub(r"^[a-z]+://", "", provider.get("base_url", "")).split("/")[0]
    return {"model": provider.get("model", ""), "host": host}


def redact(text: str, secrets: list[str]) -> str:
    for s in secrets:
        if s and len(s) >= 4:
            text = text.replace(s, "<redacted>")
    return text


def git_describe(repo: Path) -> dict:
    def run(*args: str) -> str:
        try:
            return subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True,
                                  timeout=10).stdout.strip()
        except (OSError, subprocess.TimeoutExpired):
            return ""
    return {"head": run("rev-parse", "HEAD"), "dirty": bool(run("status", "--porcelain"))}


# -------------------------------------------------------------- traces


def hash_tree(root: Path) -> dict[str, str]:
    out = {}
    if not root.exists():
        return out
    for path in sorted(root.rglob("*")):
        if path.is_file() and not path.is_symlink():
            out[path.relative_to(root).as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    return out


def parse_tool_call(text: str) -> tuple[str, Any]:
    """Splits a stored tool_call event text, `name({json})`, into its parts."""
    m = re.match(r"^([A-Za-z0-9_.\-]+)\((.*)\)\s*$", text or "", re.S)
    if not m:
        return (text or "").strip(), None
    try:
        return m.group(1), json.loads(m.group(2)) if m.group(2).strip() else {}
    except json.JSONDecodeError:
        return m.group(1), None


def _events(traces: list[dict]) -> list[dict]:
    return [e for t in traces for e in (t or {}).get("events") or []]


def _calls(traces: list[dict]) -> list[dict]:
    return [c for t in traces for c in (t or {}).get("llm_calls") or []]


def final_answer(traces: list[dict]) -> str:
    for trace in reversed(traces):
        final = ((trace or {}).get("turn") or {}).get("final") or ""
        if final.strip():
            return final
        for e in reversed((trace or {}).get("events") or []):
            if e.get("kind") == "done" and (e.get("text") or "").strip():
                return e["text"]
    return ""


def review_settled(traces: list[dict]) -> bool:
    if not traces:
        return False
    last = traces[-1] or {}
    if ((last.get("turn") or {}).get("status")) == "running":
        return False
    return any(e.get("kind") == "memory_review" for e in last.get("events") or [])


# Logged by internal/engine/engine.go when Close finds a review in flight.
REVIEW_CUT_OFF_LOG = "a memory review was still running at shutdown"


def review_cut_off(engine_log: str) -> bool:
    return REVIEW_CUT_OFF_LOG in engine_log


def agent_category(agent_id: str) -> str:
    if agent_id == MANAGER_ID:
        return MANAGER_ID
    if agent_id in AUX_AGENTS:
        return agent_id
    return "worker"


# Tools a manager uses to run its workers rather than to do the task.
LIFECYCLE_TOOLS = ("spawn_agent", "wait_agents", "send_message", "close_agent", "resume_agent")


def manager_tools_while_workers_run(events: list[dict]) -> int:
    """Manager tool calls made while a worker it spawned has not finished.

    Work the manager does itself in that window is the duplicate
    investigation delegation was meant to avoid; a worker that never
    finishes keeps the window open to the end of the trace."""
    running: set[str] = set()
    count = 0
    for e in events:
        kind = e.get("kind")
        if kind == "spawned":
            running.add(e.get("agent_id", ""))
        elif kind == "finished":
            running.discard(e.get("agent_id", ""))
        elif kind == "tool_call" and running and e.get("agent_id") == MANAGER_ID:
            if parse_tool_call(e.get("text", ""))[0] not in LIFECYCLE_TOOLS:
                count += 1
    return count


def extract_metrics(traces: list[dict]) -> dict:
    events = _events(traces)
    calls = _calls(traces)
    by_cat: dict[str, dict] = {}
    for c in calls:
        cat = agent_category(c.get("agent_id", ""))
        slot = by_cat.setdefault(cat, {"calls": 0, "prompt_tokens": 0, "completion_tokens": 0,
                                       "total_tokens": 0, "cached_tokens": 0, "duration_ms": 0})
        slot["calls"] += 1
        for key in ("prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "duration_ms"):
            slot[key] += int(c.get(key) or 0)
    manager_calls = [c for c in calls if c.get("agent_id") == MANAGER_ID]
    tools_manager: Counter = Counter()
    tools_workers: Counter = Counter()
    fork = 0
    for e in events:
        if e.get("kind") != "tool_call":
            continue
        name, args = parse_tool_call(e.get("text", ""))
        (tools_manager if e.get("agent_id") == MANAGER_ID else tools_workers)[name] += 1
        if name == "spawn_agent" and isinstance(args, dict) and args.get("fork_context") is True:
            fork += 1
    retries: Counter = Counter()
    for e in events:
        if e.get("kind") == "model_retry":
            try:
                retries[json.loads(e.get("text") or "{}").get("reason", "unknown")] += 1
            except json.JSONDecodeError:
                retries["unknown"] += 1
    workers = sorted({e.get("agent_id", "") for e in events if e.get("kind") == "spawned"})
    turns = [(t or {}).get("turn") or {} for t in traces]
    return {
        "turns": len(turns),
        "turn_statuses": [t.get("status", "") for t in turns],
        "duration_ms": sum(int(t.get("duration_ms") or 0) for t in turns),
        "calls": by_cat,
        "total_tokens": sum(v["total_tokens"] for v in by_cat.values()),
        "manager_prompt_chars": int(manager_calls[0].get("input_chars") or 0) if manager_calls else 0,
        "manager_calls": len(manager_calls),
        # The first call of a run has no earlier call of its own to reuse, so
        # what the provider cached there is the prefix shared with other runs.
        "manager_first_call": {
            "prompt_tokens": int(manager_calls[0].get("prompt_tokens") or 0),
            "cached_tokens": int(manager_calls[0].get("cached_tokens") or 0),
        } if manager_calls else {"prompt_tokens": 0, "cached_tokens": 0},
        "workers": workers,
        "spawn_calls": tools_manager.get("spawn_agent", 0),
        "manager_tools_while_workers_run": manager_tools_while_workers_run(events),
        "fork_context": fork,
        "tools_manager": dict(tools_manager),
        "tools_workers": dict(tools_workers),
        "skill_view": tools_manager.get("skill_view", 0) + tools_workers.get("skill_view", 0),
        "asks": tools_manager.get("ask_user", 0),
        "schedule_events": sum(1 for e in events if e.get("kind") == "schedule"),
        "schedule_fired": sum(1 for e in events if e.get("kind") == "schedule_fired"),
        "model_retry": dict(retries),
        "errors": sum(1 for e in events if e.get("kind") == "error"),
    }


# --------------------------------------------------------------- checks


def expand_task_dir(checks: list[dict], task_dir: Path) -> list[dict]:
    """Copies of checks with `{task_dir}` in command argv replaced by the task
    directory, so a hidden test can live beside the task instead of in the
    workspace the agent reads. The task's own spec is left as written."""
    out = []
    for check in checks:
        check = dict(check)
        if "argv" in check:
            check["argv"] = [str(a).replace("{task_dir}", str(task_dir)) for a in check["argv"]]
        if "checks" in check:
            check["checks"] = expand_task_dir(check["checks"], task_dir)
        out.append(check)
    return out


def run_checks(checks: list[dict], workspace: Path, answer: str, traces: list[dict],
               baseline: dict[str, str]) -> list[dict]:
    return [_run_check(c, workspace, answer, traces, baseline) for c in checks]


def _result(check: dict, ok: bool, detail: str = "") -> dict:
    return {"name": check.get("name") or f"{check['type']}:{check.get('path') or check.get('tool') or ''}".rstrip(":"),
            "ok": bool(ok), "detail": detail[:300]}


def _flags(spec: str) -> int:
    f = 0
    for ch in spec or "":
        f |= {"i": re.I, "m": re.M, "s": re.S}.get(ch, 0)
    return f


def _read(workspace: Path, rel: str) -> str | None:
    texts = _read_all(workspace, rel)
    return texts[0] if texts else None


def _read_all(workspace: Path, rel: str) -> list[str]:
    """Contents of the workspace files a path or glob names, sorted by path.
    A path that escapes the workspace matches nothing."""
    root = workspace.resolve()
    paths = sorted(root.glob(rel)) if any(ch in rel for ch in "*?[") else [root / rel]
    out = []
    for path in paths:
        path = path.resolve()
        if path.is_relative_to(root) and path.is_file():
            out.append(path.read_text(errors="replace"))
    return out


def _run_check(check: dict, workspace: Path, answer: str, traces: list[dict], baseline: dict[str, str]) -> dict:
    kind = check["type"]
    if kind == "file_exists":
        found = bool(_read_all(workspace, check["path"]))
        return _result(check, found, "" if found else "missing")
    if kind == "file_regex":
        texts = _read_all(workspace, check["path"])
        if not texts:
            return _result(check, False, "missing")
        flags = _flags(check.get("flags", ""))
        # negate: no matched file may contain the pattern; otherwise one must.
        if check.get("negate"):
            ok = not any(re.search(check["pattern"], t, flags) for t in texts)
        else:
            ok = any(re.search(check["pattern"], t, flags) for t in texts)
        return _result(check, ok, "" if ok else "pattern mismatch")
    if kind == "answer_contains_file":
        text = _read(workspace, check["path"])
        if text is None or not text.strip():
            return _result(check, False, "missing or empty")
        ok = text.strip() in (answer or "")
        return _result(check, ok, "" if ok else "answer does not contain the file's content")
    if kind == "answer_regex":
        hit = re.search(check["pattern"], answer or "", _flags(check.get("flags", ""))) is not None
        return _result(check, hit != bool(check.get("negate")), "" if hit != bool(check.get("negate")) else "pattern mismatch")
    if kind == "json_equals":
        text = _read(workspace, check["path"])
        if text is None:
            return _result(check, False, "missing")
        try:
            doc = json.loads(text)
        except json.JSONDecodeError as exc:
            return _result(check, False, f"invalid JSON: {exc}")
        ok, got = _pointer(doc, check.get("pointer", ""))
        if not ok:
            return _result(check, False, f"no value at {check.get('pointer', '')!r}")
        same = values_equal(got, check.get("value"), float(check.get("tolerance", 1e-9)))
        return _result(check, same, "" if same else f"got {json.dumps(got)[:120]}")
    if kind == "file_unchanged":
        path = check["path"]
        now = hash_tree(workspace).get(path)
        ok = path in baseline and now == baseline[path]
        return _result(check, ok, "" if ok else ("missing" if now is None else "modified"))
    if kind == "command":
        try:
            proc = subprocess.run(check["argv"], cwd=workspace, capture_output=True, text=True,
                                  timeout=float(check.get("timeout_s", 120)))
        except (OSError, subprocess.TimeoutExpired) as exc:
            return _result(check, False, f"{type(exc).__name__}: {exc}")
        # stdout last: a hidden-rule runner's one-line summary is on stdout,
        # after a traceback that would otherwise push it out of the tail.
        tail = (proc.stderr + proc.stdout)[-280:]
        return _result(check, proc.returncode == 0, "" if proc.returncode == 0 else f"exit {proc.returncode}: {tail}")
    if kind == "answer_chart":
        ok, detail = chart_matches(answer, check.get("expect") or {}, float(check.get("tolerance", 1e-6)))
        return _result(check, ok, detail)
    if kind == "tool_count":
        metrics = extract_metrics(traces)
        scope = check.get("agent", "any")
        n = 0
        if scope in ("any", "manager"):
            n += metrics["tools_manager"].get(check["tool"], 0)
        if scope in ("any", "workers"):
            n += metrics["tools_workers"].get(check["tool"], 0)
        return _bounded(check, n)
    if kind == "event_count":
        n = sum(1 for e in _events(traces) if e.get("kind") == check["kind"])
        return _bounded(check, n)
    if kind == "any_of":
        subs = [_run_check(c, workspace, answer, traces, baseline) for c in check["checks"]]
        ok = any(s["ok"] for s in subs)
        return _result(check, ok, "" if ok else "; ".join(f"{s['name']}: {s['detail']}" for s in subs))
    raise TaskError(f"unknown check type {kind!r}")


def _bounded(check: dict, n: int) -> dict:
    lo = check.get("min")
    hi = check.get("max")
    ok = (lo is None or n >= lo) and (hi is None or n <= hi)
    return _result(check, ok, f"count {n}")


def _pointer(doc: Any, pointer: str) -> tuple[bool, Any]:
    if pointer in ("", "/"):
        return True, doc
    cur = doc
    for raw in pointer.lstrip("/").split("/"):
        part = raw.replace("~1", "/").replace("~0", "~")
        if isinstance(cur, dict) and part in cur:
            cur = cur[part]
        elif isinstance(cur, list) and part.isdigit() and int(part) < len(cur):
            cur = cur[int(part)]
        else:
            return False, None
    return True, cur


def values_equal(got: Any, want: Any, tol: float) -> bool:
    if isinstance(want, bool) or isinstance(got, bool):
        return got is want
    if isinstance(want, (int, float)) and isinstance(got, (int, float)):
        return math.isclose(float(got), float(want), rel_tol=0, abs_tol=tol)
    if isinstance(want, dict) and isinstance(got, dict):
        return set(got) == set(want) and all(values_equal(got[k], want[k], tol) for k in want)
    if isinstance(want, list) and isinstance(got, list):
        return len(got) == len(want) and all(values_equal(g, w, tol) for g, w in zip(got, want))
    return got == want


def chart_blocks(answer: str) -> list[Any]:
    out = []
    for m in re.finditer(r"```chart[ \t]*\n(.*?)```", answer or "", re.S):
        try:
            out.append(json.loads(m.group(1)))
        except json.JSONDecodeError:
            out.append(None)
    return out


def chart_matches(answer: str, expect: dict, tol: float) -> tuple[bool, str]:
    blocks = chart_blocks(answer)
    if not blocks:
        return False, "no chart block"
    for spec in blocks:
        if not isinstance(spec, dict) or spec.get("type") not in ("bar", "line", "area", "pie"):
            continue
        x = spec.get("x")
        ys = spec.get("y")
        ys = ys if isinstance(ys, list) else [ys]
        rows = spec.get("data")
        if not isinstance(x, str) or not isinstance(rows, list):
            continue
        values: dict[str, list[float]] = {}
        for row in rows:
            if isinstance(row, dict) and x in row:
                values[str(row[x])] = [row[y] for y in ys if isinstance(row.get(y), (int, float))]
        if all(any(math.isclose(v, float(want), abs_tol=tol) for v in values.get(str(label), []))
               for label, want in expect.items()):
            return True, ""
    return False, f"{len(blocks)} chart block(s), none matched the expected values"


# -------------------------------------------------------------- reports


def summarize(results: list[dict]) -> dict:
    by_task: dict[str, list[dict]] = {}
    for r in results:
        by_task.setdefault(r["task"], []).append(r)
    tasks = {}
    totals_by_cat: Counter = Counter()
    for task_id, runs in by_task.items():
        graded = [r for r in runs if not r.get("harness_error")]
        metrics = [r["metrics"] for r in graded]
        durations = [m["duration_ms"] for m in metrics]
        for m in metrics:
            for cat, slot in m["calls"].items():
                totals_by_cat[cat] += slot["total_tokens"]
        tasks[task_id] = {
            "runs": len(runs),
            "harness_errors": len(runs) - len(graded),
            "passed": sum(1 for r in graded if r.get("passed")),
            "expect_delegation": runs[0].get("expect_delegation", ""),
            "duration_ms_median": int(statistics.median(durations)) if durations else 0,
            "duration_ms_max": max(durations) if durations else 0,
            "tokens_mean": _mean([m["total_tokens"] for m in metrics]),
            "manager_prompt_chars": _mean([m["manager_prompt_chars"] for m in metrics]),
            "runs_with_workers": sum(1 for m in metrics if m["workers"]),
            "workers_mean": _mean([len(m["workers"]) for m in metrics]),
            "fork_context": sum(m["fork_context"] for m in metrics),
            "skill_view_runs": sum(1 for m in metrics if m["skill_view"]),
            "asks": sum(m["asks"] for m in metrics),
            "schedule_events": sum(m["schedule_events"] for m in metrics),
            "model_retry": dict(sum((Counter(m["model_retry"]) for m in metrics), Counter())),
            "aux_token_share": _share(metrics),
            "delegation_split": _delegation_split(graded),
            "outcomes": dict(Counter((r.get("outcome") or {}).get("state", "harness_error") for r in runs)),
            "reviews_cut_off": sum(1 for r in runs if r.get("review_cut_off")),
            "failed_checks": dict(Counter(c["name"] for r in graded for c in r.get("checks", []) if not c["ok"])),
        }
    all_tokens = sum(totals_by_cat.values())
    prompt_by_cat: Counter = Counter()
    cached_by_cat: Counter = Counter()
    for r in results:
        if r.get("harness_error"):
            continue
        for cat, slot in r["metrics"]["calls"].items():
            prompt_by_cat[cat] += slot["prompt_tokens"]
            cached_by_cat[cat] += slot.get("cached_tokens", 0)
    graded_all = [r for r in results if not r.get("harness_error")]
    first_prompt = sum(r["metrics"].get("manager_first_call", {}).get("prompt_tokens", 0) for r in graded_all)
    first_cached = sum(r["metrics"].get("manager_first_call", {}).get("cached_tokens", 0) for r in graded_all)
    return {
        "first_call_cache": {"prompt_tokens": first_prompt, "cached_tokens": first_cached,
                             "hit": round(first_cached / first_prompt, 3) if first_prompt else 0},
        "runs": len(results),
        "passed": sum(1 for r in graded_all if r.get("passed")),
        "harness_errors": len(results) - len(graded_all),
        "reviews_cut_off": sum(1 for r in results if r.get("review_cut_off")),
        "tokens_by_category": dict(totals_by_cat),
        "token_share_by_category": {k: round(v / all_tokens, 3) for k, v in totals_by_cat.items()} if all_tokens else {},
        "cached_tokens_by_category": dict(cached_by_cat),
        "cache_hit_by_category": {k: round(cached_by_cat[k] / v, 3) for k, v in prompt_by_cat.items() if v},
        "tasks": tasks,
    }


def _delegation_split(graded: list[dict]) -> dict:
    """Medians for the runs of one task that kept the work and the runs that
    spawned workers; the model picks, so the two groups are not randomized."""
    def side(runs: list[dict]) -> dict:
        ms = [r["metrics"] for r in runs]
        return {
            "runs": len(runs),
            "passed": sum(1 for r in runs if r.get("passed")),
            "duration_ms_median": int(statistics.median(m["duration_ms"] for m in ms)) if ms else 0,
            "tokens_median": int(statistics.median(m["total_tokens"] for m in ms)) if ms else 0,
            "manager_tools_while_workers_run_median":
                int(statistics.median(m.get("manager_tools_while_workers_run", 0) for m in ms)) if ms else 0,
        }
    return {"solo": side([r for r in graded if not r["metrics"]["workers"]]),
            "delegated": side([r for r in graded if r["metrics"]["workers"]])}


def _mean(values: list[float]) -> float:
    return round(sum(values) / len(values), 1) if values else 0.0


def _share(metrics: list[dict]) -> float:
    aux = sum(slot["total_tokens"] for m in metrics for cat, slot in m["calls"].items() if cat in AUX_AGENTS)
    total = sum(m["total_tokens"] for m in metrics)
    return round(aux / total, 3) if total else 0.0


def render_markdown(meta: dict, summary: dict, results: list[dict]) -> str:
    lines = [
        "# Agent task eval",
        "",
        f"- started: {meta.get('started')}",
        f"- provider: {json.dumps(meta.get('provider'), ensure_ascii=False)}",
        f"- source: {json.dumps(meta.get('source'))}",
        f"- runs per task: {meta.get('runs_per_task')}",
        *([f"- output cap per request: {meta['max_completion_tokens']}"] if "max_completion_tokens" in meta else []),
        f"- passed: {summary['passed']}/{summary['runs']} (harness errors: {summary['harness_errors']})",
        f"- memory reviews cut off at shutdown: {summary.get('reviews_cut_off', 0)} "
        "(their memory-reviewer tokens are a lower bound)",
        "",
        "## Tasks",
        "",
        "| task | pass | expect | median s | max s | tokens | prompt chars | runs w/ workers | workers | fork | skill_view runs | asks | sched | aux share | review cut | outcomes |",
        "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|",
    ]
    for task_id, t in summary["tasks"].items():
        lines.append(
            f"| {task_id} | {t['passed']}/{t['runs']} | {t['expect_delegation']} | "
            f"{t['duration_ms_median'] / 1000:.1f} | {t['duration_ms_max'] / 1000:.1f} | {t['tokens_mean']:.0f} | "
            f"{t['manager_prompt_chars']:.0f} | {t['runs_with_workers']} | {t['workers_mean']} | {t['fork_context']} | "
            f"{t['skill_view_runs']} | {t['asks']} | {t['schedule_events']} | {t['aux_token_share']} | "
            f"{t.get('reviews_cut_off', 0)} | {json.dumps(t['outcomes'])} |"
        )
    lines += ["", "## Delegated vs solo", "",
              "Runs that kept the work against runs that spawned workers, per task (medians; the model "
              "chose, so the groups are not randomized). `own calls` are manager tool calls made while a "
              "worker was still running.", "",
              "| task | solo runs | solo s | solo tokens | delegated runs | delegated s | delegated tokens "
              "| own calls | solo pass | delegated pass |",
              "|---|---|---|---|---|---|---|---|---|---|"]
    for task_id, t in summary["tasks"].items():
        solo, deleg = t["delegation_split"]["solo"], t["delegation_split"]["delegated"]
        def cell(side, key, scale=1.0, fmt="{:.0f}"):
            return fmt.format(side[key] / scale) if side["runs"] else "-"
        lines.append(
            f"| {task_id} | {solo['runs']} | {cell(solo, 'duration_ms_median', 1000, '{:.1f}')} | "
            f"{cell(solo, 'tokens_median')} | {deleg['runs']} | {cell(deleg, 'duration_ms_median', 1000, '{:.1f}')} | "
            f"{cell(deleg, 'tokens_median')} | {cell(deleg, 'manager_tools_while_workers_run_median')} | "
            f"{solo['passed']}/{solo['runs']} | {deleg['passed']}/{deleg['runs']} |")
    lines += ["", "## Tokens by caller", "",
              "`hit rate` is cached prompt tokens over prompt tokens, as the provider reports them.", "",
              "| caller | tokens | share | cached | hit rate |", "|---|---|---|---|---|"]
    for cat, n in sorted(summary["tokens_by_category"].items(), key=lambda kv: -kv[1]):
        lines.append(f"| {cat} | {n} | {summary['token_share_by_category'].get(cat, 0)} | "
                     f"{summary['cached_tokens_by_category'].get(cat, 0)} | "
                     f"{summary['cache_hit_by_category'].get(cat, 0)} |")
    first = summary.get("first_call_cache") or {}
    lines += ["", f"First manager call of each run: {first.get('cached_tokens', 0)} of "
              f"{first.get('prompt_tokens', 0)} prompt tokens cached ({first.get('hit', 0)}). Only a prefix "
              "shared with another conversation can be cached there.",
              "", "## Failures", ""]
    any_fail = False
    for r in results:
        if r.get("passed"):
            continue
        any_fail = True
        if r.get("harness_error"):
            lines.append(f"- {r['task']} run {r['run']}: harness error: {r['harness_error']}")
            continue
        failed = [f"{c['name']} ({c['detail']})" for c in r.get("checks", []) if not c["ok"]]
        state = (r.get("outcome") or {}).get("state")
        lines.append(f"- {r['task']} run {r['run']}: outcome {state}; " + ("; ".join(failed) or "all checks passed"))
    if not any_fail:
        lines.append("- none")
    lines.append("")
    return "\n".join(lines)
