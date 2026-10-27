#!/usr/bin/env python3
"""What did a review change? Grade each run's files as they stood when the
manager first collected a worker result, next to the final files.

    python3 tools/agent_eval_prereview.py DIR [DIR ...]

DIR is an --out root, a report directory or a single run-N directory written
by tools/agent_eval.py. For every run that spawned a worker, the files the
trace wrote (`write`, `edit`'s search/replace or patch form) are replayed up to
the manager's first `wait_agents` call after a spawn and graded with the
task's `command` checks; the final workspace is graded the same way. A check
that fails at the first report and passes at the end was fixed after a worker
reported. Files changed through `exec` are invisible to the replay, so a run
whose replay does not reproduce a file still in its final workspace is
marked and its first-report column is not evidence. Reads only; workspaces
are graded in temporary copies.
"""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import agent_eval  # noqa: E402
import agent_eval_grade as grade  # noqa: E402


def apply_patch(text: str, patch: str) -> str:
    """Applies the hunks of a `*** Begin Patch` edit; a hunk whose context is
    not found is skipped, which the replay check then reports."""
    hunks: list[tuple[list[str], list[str]]] = []
    for line in patch.split("\n"):
        if line.startswith("@@"):
            hunks.append(([], []))
        elif hunks and not line.startswith("***"):
            tag, body = (line[:1], line[1:]) if line[:1] in " +-" else (" ", line)
            if tag in " -":
                hunks[-1][0].append(body)
            if tag in " +":
                hunks[-1][1].append(body)
    for old, new in hunks:
        before, after = "\n".join(old), "\n".join(new)
        if before in text:
            text = text.replace(before, after, 1)
    return text


def replay(events: list[dict]) -> tuple[dict[str, str], dict[str, str] | None, list[str]]:
    """Files written by the trace at its end and at the manager's first
    wait_agents after a spawn (None when it never spawned), and the roles
    spawned."""
    failed = {e.get("tool_call_id") for e in events if e.get("kind") == "tool_result"
              and (e.get("text") or "").lower().startswith(("error", "refused"))}
    files: dict[str, str] = {}
    first_report, roles = None, []
    for e in events:
        if e.get("kind") != "tool_call":
            continue
        name, args = grade.parse_tool_call(e.get("text", ""))
        manager = e.get("agent_id") == "manager"
        if manager and name == "spawn_agent":
            roles.append((args or {}).get("role", ""))
        if manager and name == "wait_agents" and roles and first_report is None:
            first_report = dict(files)
        if name not in ("write", "edit") or not isinstance(args, dict) or e.get("tool_call_id") in failed:
            continue
        path = args.get("file_path") or args.get("path")
        if not path:
            continue
        if name == "write":
            files[path] = args.get("content", "")
        elif path in files and "patch" in args:
            files[path] = apply_patch(files[path], args["patch"])
        elif path in files and args.get("search_block") is not None and args["search_block"] in files[path]:
            files[path] = files[path].replace(args["search_block"], args.get("replace_block", ""), 1)
    return files, first_report, roles


def failing_checks(task: dict, files: dict[str, str] | None, workspace: Path | None = None) -> list[str]:
    """Names of the task's command checks that fail on a fresh workspace with
    files laid over it, or on a copy of an existing workspace."""
    checks = [c for c in grade.expand_task_dir(task["checks"], task["dir"]) if c["type"] == "command"]
    tmp = Path(tempfile.mkdtemp(prefix="zwai-prereview-"))
    try:
        ws = tmp / "ws"
        if workspace is not None:
            shutil.copytree(workspace, ws)
        else:
            agent_eval.prepare_workspace(task, ws)
            for rel, text in (files or {}).items():
                target = ws / rel
                if target.resolve().is_relative_to(ws.resolve()):
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_text(text)
        return [r["name"] + brief(r.get("detail", "")) for r in grade.run_checks(checks, ws, "", [], {}) if not r["ok"]]
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def brief(detail: str) -> str:
    """The rule names a hidden runner prints, else a short first line."""
    lines = [ln.strip() for ln in detail.splitlines() if ln.strip()]
    named = [ln for ln in lines if "failed rules:" in ln]
    text = named[-1].split("failed rules:", 1)[1].strip() if named else (lines[0][:80] if lines else "")
    return " (%s)" % text if text else ""


def run_dirs(root: Path) -> list[Path]:
    if (root / "result.json").is_file():
        return [root]
    return sorted(p.parent for p in root.rglob("result.json") if p.parent.name.startswith("run-"))


def inspect_run(run: Path, tasks_dir: Path | None = None) -> dict:
    result = json.loads((run / "result.json").read_text())
    task = grade.load_task((tasks_dir or agent_eval.TASKS_DIR) / result["task"])
    events = grade._events(json.loads((run / "traces.json").read_text()))
    final_replay, first_report, roles = replay(events)
    ws = run / "workspace"
    # A file the run later deleted (a reviewer's probe script) says nothing
    # about the replay; one that still exists must match byte for byte.
    exact = all((ws / rel).read_text() == text for rel, text in final_replay.items() if (ws / rel).is_file())
    return {
        "run": f"{result['task']}/{run.name}",
        "state": (result.get("outcome") or {}).get("state", ""),
        "roles": roles,
        "final": failing_checks(task, None, ws) if ws.is_dir() else ["no workspace"],
        "first_report": None if first_report is None else failing_checks(task, first_report),
        "replay_exact": exact,
    }


def render(rows: list[dict]) -> str:
    out = ["| run | state | workers spawned | failing at first report | failing at the end | replay |",
           "|---|---|---|---|---|---|"]
    for r in rows:
        first = "no worker" if r["first_report"] is None else (", ".join(r["first_report"]) or "none")
        out.append("| %s | %s | %s | %s | %s | %s |" % (
            r["run"], r["state"], ", ".join(r["roles"]) or "-", first, ", ".join(r["final"]) or "none",
            "exact" if r["replay_exact"] else "differs (exec edits)"))
    return "\n".join(out) + "\n"


def main(argv: list[str]) -> int:
    if not argv:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    rows = [inspect_run(run) for arg in argv for run in run_dirs(Path(arg))]
    if not rows:
        print("no run directories found", file=sys.stderr)
        return 2
    sys.stdout.write(render(rows))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
