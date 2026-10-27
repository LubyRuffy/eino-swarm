#!/usr/bin/env python3
"""Turn a recorded conversation's Trace into a replayable agent-eval task.

Input is what `GET /api/trace/:turn` returns for each turn of one conversation
(a single object, or a list in turn order such as an eval run's traces.json).
The first human message becomes prompt.md; every later human input becomes a
`replay` entry with its offset from the first message: steers, answers to
`ask_user`, and the messages that started later turns. Turns the runtime
started itself (goal continuations, scheduled checks) are not human input and
are skipped. The task's checks are structural only (no error event, a
non-empty answer); add the checks that define success for that conversation
before trusting a pass.

The output carries the conversation's own words, so it defaults to the
git-ignored output/replays/ directory rather than evals/tasks/. --redact
replaces every match of a regular expression in what is written. Reads only.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import shutil
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
DEFAULT_OUT = REPO / "output" / "replays"
ASK_TOOL = "ask_user"


def _time(value: str) -> dt.datetime:
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def load_traces(path: Path) -> list[dict]:
    doc = json.loads(path.read_text())
    traces = doc if isinstance(doc, list) else [doc]
    if not traces or not all(isinstance(t, dict) and "events" in t and "turn" in t for t in traces):
        raise SystemExit(f"{path}: not a trace (want the JSON of GET /api/trace/:turn, or a list of them)")
    return sorted(traces, key=lambda t: int(t["turn"].get("seq") or 0))


def _answer_text(result: str) -> str:
    """The human's answers to one ask_user call, as one line of free text.

    Question ids are minted per run, so a replay answers with the chosen
    labels and typed text rather than the recorded ids."""
    try:
        answers = json.loads(result).get("answers") or {}
    except (json.JSONDecodeError, AttributeError):
        return ""
    parts = []
    for qid in sorted(answers):
        for a in (answers[qid] or {}).get("answers") or []:
            if str(a).strip():
                parts.append(str(a).strip())
    return "; ".join(parts)


def human_inputs(traces: list[dict]) -> tuple[str, list[dict], dt.datetime]:
    """(first message, later inputs with offsets, time of the first message)."""
    first_text, start = "", None
    replay: list[dict] = []
    for index, trace in enumerate(traces):
        events = sorted(trace["events"], key=lambda e: int(e.get("seq") or 0))
        asks = {e.get("tool_call_id") for e in events
                if e.get("kind") == "tool_call" and str(e.get("text", "")).startswith(ASK_TOOL + "(")
                and e.get("agent_id") == "manager"}
        for e in events:
            kind, at = e.get("kind"), e.get("created_at")
            if not at:
                continue
            when = _time(at)
            if kind == "user_message" and e.get("agent_id") == "manager":
                text = str(e.get("text", "")).strip()
                if not text:
                    continue
                if start is None:
                    first_text, start = text, when
                    continue
                replay.append({"kind": "turn", "text": text, "at": when})
            elif kind == "steer" and start is not None and str(e.get("text", "")).strip():
                replay.append({"kind": "steer", "text": str(e["text"]).strip(), "at": when})
            elif kind == "tool_result" and e.get("tool_call_id") in asks and start is not None:
                text = _answer_text(str(e.get("text", "")))
                if text:
                    replay.append({"kind": "answer", "text": text, "at": when})
        if index == 0 and start is None:
            raise SystemExit("the first turn has no human message to start from")
    out = [{"kind": r["kind"], "text": r["text"], "after_s": round((r["at"] - start).total_seconds(), 1)}
           for r in sorted(replay, key=lambda r: r["at"])]
    return first_text, out, start


def recorded_summary(traces: list[dict]) -> dict:
    """What the recorded run cost, for comparing the replay against it."""
    calls = [c for t in traces for c in t.get("llm_calls") or []]
    tools: dict[str, int] = {}
    for t in traces:
        for e in t["events"]:
            if e.get("kind") == "tool_call":
                name = str(e.get("text", "")).split("(", 1)[0].strip()
                tools[name] = tools.get(name, 0) + 1
    return {
        "turns": [{"id": t["turn"].get("id"), "status": t["turn"].get("status"),
                   "duration_ms": t["turn"].get("duration_ms"), "model": t["turn"].get("model")} for t in traces],
        "model_calls": len(calls),
        "total_tokens": sum(int(c.get("total_tokens") or 0) for c in calls),
        "tool_calls": dict(sorted(tools.items())),
        "spawned": sum(1 for t in traces for e in t["events"] if e.get("kind") == "spawned"),
    }


def redact(value, patterns: list[re.Pattern]):
    if isinstance(value, str):
        for p in patterns:
            value = p.sub("<redacted>", value)
        return value
    if isinstance(value, list):
        return [redact(v, patterns) for v in value]
    if isinstance(value, dict):
        return {k: redact(v, patterns) for k, v in value.items()}
    return value


def copy_fixture(src: Path, dest: Path, limit_bytes: int = 5 << 20) -> int:
    copied = 0
    for path in sorted(src.rglob("*")):
        rel = path.relative_to(src)
        if path.is_symlink() or not path.is_file() or path.stat().st_size > limit_bytes:
            continue
        if any(part in {".git", "node_modules", "__pycache__"} for part in rel.parts):
            continue
        out = dest / rel
        out.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, out)
        copied += 1
    return copied


def build_task(traces: list[dict], task_id: str, timeout_s: float | None) -> tuple[dict, str, dict]:
    prompt, replay, _ = human_inputs(traces)
    recorded = recorded_summary(traces)
    took = sum(int(t.get("duration_ms") or 0) for t in recorded["turns"]) / 1000
    spec = {
        "id": task_id,
        "purpose": "Replay of a recorded conversation. Add checks that define success before comparing builds.",
        "expect_delegation": "optional",
        "timeout_s": timeout_s or max(600, round(took * 3)),
        "replay": replay,
        "checks": [
            {"name": "no error event", "type": "event_count", "kind": "error", "max": 0},
            {"name": "the last answer is not empty", "type": "answer_regex", "pattern": "\\S"},
        ],
    }
    return spec, prompt, recorded


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--trace", required=True, help="trace JSON: one GET /api/trace/:turn object or a list of them")
    p.add_argument("--id", required=True, help="task id; also the directory name")
    p.add_argument("--out", default=str(DEFAULT_OUT), help="task root to write into (default: output/replays)")
    p.add_argument("--workspace", default="", help="directory to copy in as the task's starting fixture")
    p.add_argument("--timeout-s", type=float, default=None, help="default: three times the recorded duration, at least 600")
    p.add_argument("--redact", action="append", default=[], help="regular expression to replace in everything written")
    args = p.parse_args(argv)
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", args.id):
        raise SystemExit("--id must be lowercase letters, digits and hyphens")
    patterns = [re.compile(r) for r in args.redact]
    spec, prompt, recorded = build_task(load_traces(Path(args.trace)), args.id, args.timeout_s)
    task_dir = Path(args.out) / args.id
    if task_dir.exists():
        raise SystemExit(f"{task_dir} already exists; choose another --id or remove it")
    task_dir.mkdir(parents=True)
    (task_dir / "task.json").write_text(json.dumps(redact(spec, patterns), indent=2, ensure_ascii=False) + "\n")
    (task_dir / "prompt.md").write_text(redact(prompt, patterns) + "\n")
    (task_dir / "recorded.json").write_text(json.dumps(redact(recorded, patterns), indent=2, ensure_ascii=False) + "\n")
    copied = copy_fixture(Path(args.workspace), task_dir / "fixture") if args.workspace else 0
    print(f"{task_dir}: prompt, {len(spec['replay'])} replayed inputs, {copied} fixture files")
    print(f"run: python3 tools/agent_eval.py --task-root {Path(args.out)} --tasks {args.id} --provider <id>")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
