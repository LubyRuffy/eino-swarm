#!/usr/bin/env python3
"""Opt-in task-completion evals against a real model.

Each run owns a fresh temporary data directory and its own `zwai engine`
child. The harness copies one provider from the user's config into that
directory, seeds the task's fixture and skills, sends one human message over
loopback HTTP, waits for the conversation to settle, then grades the
workspace, the final answer and the turn traces. The temporary directory
holds an API key, so it is removed after every run; evidence written under
--out is redacted first.

This is not part of `go test`, `make test` or `make check`: those never
reach a network or a real model. See docs/TESTING.md.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Callable

import agent_eval_grade as grade

REPO = Path(__file__).resolve().parent.parent
TASKS_DIR = REPO / "evals" / "tasks"
DEFAULT_CONFIG = Path.home() / ".zwai-swarm" / "config.yaml"
EVAL_PROVIDER_ID = "eval"

# The engine exits StartGrace (15s) after start when no presence is held.
# Discovery must comfortably beat that, so engine.json is polled tightly.
ENGINE_POLL_S = 0.2
POLL_S = 2.0

# Inherited variables that would seed or redirect the child engine.
_SCRUBBED_ENV_PREFIXES = ("ZWAI_", "OPENAI_")


class EvalError(Exception):
    """A harness failure, as opposed to a task that the agent failed."""


def http_json(base: str, method: str, path: str, body: Any = None, timeout: float = 30) -> Any:
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode(errors="replace")[:500]
        raise EvalError(f"{method} {path}: HTTP {exc.code}: {detail}") from exc
    return json.loads(raw) if raw else None


class Presence:
    """Holds one presence SSE connection so the engine does not idle out."""

    def __init__(self, base: str):
        self.base = base
        self._resp = None
        self._thread: threading.Thread | None = None
        self._stop = threading.Event()

    def start(self) -> None:
        pid = http_json(self.base, "POST", "/api/presence", {"surface": "tui", "pid": os.getpid()})["id"]
        req = urllib.request.Request(f"{self.base}/api/presence/{pid}", headers={"Accept": "text/event-stream"})
        self._resp = urllib.request.urlopen(req, timeout=60)
        first = self._resp.readline()
        if not first.startswith(b":"):
            raise EvalError(f"presence stream did not acknowledge: {first[:80]!r}")
        self._thread = threading.Thread(target=self._drain, daemon=True)
        self._thread.start()

    def _drain(self) -> None:
        try:
            while not self._stop.is_set():
                if not self._resp.readline():
                    return
        except Exception:
            return

    def stop(self) -> None:
        self._stop.set()
        if self._resp is not None:
            try:
                self._resp.close()
            except Exception:
                pass


class Engine:
    """One `zwai engine` child that this harness started and alone stops."""

    def __init__(self, zwai: Path, data_dir: Path, mock: bool, boot_timeout: float):
        self.zwai = zwai
        self.data_dir = data_dir
        self.mock = mock
        self.boot_timeout = boot_timeout
        self.proc: subprocess.Popen | None = None
        self.base = ""
        self.presence: Presence | None = None
        self.log_path = data_dir / "engine-stdout.log"

    def child_env(self) -> dict[str, str]:
        env = {k: v for k, v in os.environ.items() if not k.startswith(_SCRUBBED_ENV_PREFIXES)}
        env["ZWAI_HOME"] = str(self.data_dir)
        return env

    def start(self) -> None:
        argv = [str(self.zwai), "engine", "--data-dir", str(self.data_dir), "--addr", "127.0.0.1:0"]
        if self.mock:
            argv.append("--mock")
        log = open(self.log_path, "wb")
        self.proc = subprocess.Popen(argv, stdout=log, stderr=subprocess.STDOUT, env=self.child_env(),
                                     cwd=str(self.data_dir), start_new_session=True)
        log.close()
        lease = self.data_dir / "engine.json"
        deadline = time.monotonic() + self.boot_timeout
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise EvalError(f"engine exited during startup with {self.proc.returncode}")
            if lease.exists():
                try:
                    rec = json.loads(lease.read_text())
                except (OSError, json.JSONDecodeError):
                    rec = {}
                if rec.get("pid") == self.proc.pid and rec.get("url"):
                    self.base = rec["url"].rstrip("/")
                    break
            time.sleep(ENGINE_POLL_S)
        if not self.base:
            raise EvalError(f"engine did not publish engine.json within {self.boot_timeout:.0f}s")
        self.presence = Presence(self.base)
        self.presence.start()
        while time.monotonic() < deadline:
            if http_json(self.base, "GET", "/api/meta").get("startup") == "ready":
                return
            time.sleep(ENGINE_POLL_S)
        raise EvalError("engine did not become ready")

    def stop(self) -> None:
        # Stop the engine before closing presence: closing the response while
        # the drain thread is blocked in readline waits for the next ping,
        # whereas the server going away ends that read at once.
        if self.proc is not None and self.proc.poll() is None:
            self.proc.send_signal(signal.SIGTERM)
            try:
                self.proc.wait(timeout=30)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=10)
        if self.presence is not None:
            self.presence.stop()


def load_tasks(selected: list[str]) -> list[dict]:
    tasks = []
    for d in sorted(p for p in TASKS_DIR.iterdir() if p.is_dir()):
        if selected and d.name not in selected:
            continue
        tasks.append(grade.load_task(d))
    missing = set(selected) - {t["id"] for t in tasks}
    if missing:
        raise EvalError(f"unknown task ids: {', '.join(sorted(missing))}")
    return tasks


def seed_data_dir(data_dir: Path, task: dict, provider: dict | None) -> None:
    if provider is not None:
        cfg = data_dir / "config.yaml"
        fd = os.open(cfg, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as fh:
            fh.write(grade.render_eval_config(provider, EVAL_PROVIDER_ID))
    for skill in task.get("skills", []):
        dest = data_dir / "library" / "skills" / skill["name"] / "SKILL.md"
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(task["dir"] / skill["file"], dest)


def prepare_workspace(task: dict, workspace: Path) -> None:
    """Copies the static fixture, then runs the task's deterministic generator."""
    workspace.mkdir(parents=True, exist_ok=True)
    if (task["dir"] / "fixture").is_dir():
        shutil.copytree(task["dir"] / "fixture", workspace, dirs_exist_ok=True)
    if task.get("generate"):
        subprocess.run([sys.executable, str(task["dir"] / task["generate"]), str(workspace)],
                       check=True, capture_output=True, timeout=120)


def interrupt(base: str, thread_id: str) -> None:
    try:
        http_json(base, "POST", f"/api/threads/{thread_id}/interrupt", {})
    except EvalError:
        pass


def drive_conversation(base: str, thread_id: str, task: dict, now: Callable[[], float] = time.monotonic,
                       sleep: Callable[[float], None] = time.sleep) -> dict:
    """Polls until the conversation settles. Returns the outcome record."""
    deadline = now() + task["timeout_s"]
    wait_deadline = None
    outcome = {"state": "done", "asked": False, "iteration_cap": False, "waited": False}
    seen_running = False
    while True:
        if now() > deadline:
            outcome["state"] = "timeout"
            interrupt(base, thread_id)
            return outcome
        st = (http_json(base, "GET", f"/api/threads/{thread_id}") or {}).get("status") or {}
        if st.get("running"):
            seen_running = True
            if st.get("awaiting_answer"):
                outcome["asked"] = True
                outcome["state"] = "asked"
                interrupt(base, thread_id)
                return outcome
            if st.get("awaiting_continue"):
                outcome["iteration_cap"] = True
                outcome["state"] = "iteration_cap"
                interrupt(base, thread_id)
                return outcome
        elif seen_running:
            if st.get("waiting") and task.get("allow_wait"):
                outcome["waited"] = True
                if wait_deadline is None:
                    wait_deadline = now() + task.get("wait_timeout_s", 300)
                if now() > wait_deadline:
                    outcome["state"] = "wait_timeout"
                    return outcome
            else:
                if st.get("waiting"):
                    outcome["waited"] = True
                return outcome
        else:
            # The 202 can precede the runtime marking the turn running; a turn
            # that already finished still lists as a stored turn.
            turns = (http_json(base, "GET", f"/api/threads/{thread_id}/turns") or {}).get("turns") or []
            if turns and all(t.get("status") != "running" for t in turns):
                seen_running = True
                continue
        sleep(POLL_S)


def settled_outcome(outcome: dict, traces: list[dict]) -> dict:
    """A conversation that settled on a failed turn is not a finished task:
    a provider outage must not be graded as the model's wrong answer."""
    last = (traces[-1] or {}).get("turn") or {} if traces else {}
    if outcome["state"] == "done" and last.get("status") == "error":
        return {**outcome, "state": "turn_error", "error": (last.get("error") or "")[:300]}
    return outcome


def fetch_traces(base: str, thread_id: str) -> list[dict]:
    turns = (http_json(base, "GET", f"/api/threads/{thread_id}/turns") or {}).get("turns") or []
    return [http_json(base, "GET", f"/api/trace/{t['id']}") for t in turns]


def wait_for_review(base: str, thread_id: str, timeout_s: float) -> list[dict]:
    """Waits a bounded time for the post-turn memory review so its cost is
    attributed. A review that never starts is not a failure."""
    deadline = time.monotonic() + timeout_s
    traces = fetch_traces(base, thread_id)
    while time.monotonic() < deadline:
        if grade.review_settled(traces):
            return traces
        time.sleep(POLL_S)
        traces = fetch_traces(base, thread_id)
    return traces


def copy_workspace(src: Path, dest: Path, limit_bytes: int = 5 << 20) -> None:
    if not src.exists():
        return
    for path in src.rglob("*"):
        if not path.is_file() or path.is_symlink():
            continue
        rel = path.relative_to(src)
        if any(part in {".git", "node_modules", "__pycache__"} for part in rel.parts):
            continue
        if path.stat().st_size > limit_bytes:
            continue
        out = dest / rel
        out.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, out)


def run_once(task: dict, run_idx: int, args: argparse.Namespace, provider: dict | None, out_dir: Path) -> dict:
    secrets = [provider["api_key"]] if provider and provider.get("api_key") else []
    evidence = out_dir / "runs" / task["id"] / f"run-{run_idx}"
    evidence.mkdir(parents=True, exist_ok=True)
    data_dir = Path(tempfile.mkdtemp(prefix="zwai-eval-"))
    record: dict[str, Any] = {"task": task["id"], "run": run_idx, "harness_error": ""}
    engine = Engine(Path(args.zwai).resolve(), data_dir, args.mock, args.boot_timeout)
    started = time.monotonic()
    phases: dict[str, float] = {}
    mark = [started]

    def phase(name: str) -> None:
        now = time.monotonic()
        phases[name] = round(now - mark[0], 1)
        mark[0] = now

    record["phase_s"] = phases
    try:
        seed_data_dir(data_dir, task, provider)
        engine.start()
        phase("boot")
        # Untitled, as a human creates one, so automatic naming is measured too.
        thread = http_json(engine.base, "POST", "/api/threads", {})["thread"]
        thread_id = thread["id"]
        workspace = data_dir / "workspaces" / thread_id
        workspace.mkdir(parents=True, exist_ok=True)
        prepare_workspace(task, workspace)
        baseline_hashes = grade.hash_tree(workspace)
        http_json(engine.base, "POST", f"/api/threads/{thread_id}/turns", {"text": task["prompt"]})
        outcome = drive_conversation(engine.base, thread_id, task)
        phase("conversation")
        traces = wait_for_review(engine.base, thread_id, args.review_wait)
        phase("review")
        outcome = settled_outcome(outcome, traces)
        answer = grade.final_answer(traces)
        checks = grade.run_checks(grade.expand_task_dir(task["checks"], task["dir"]), workspace, answer, traces, baseline_hashes)
        record.update({
            "outcome": outcome,
            "checks": checks,
            "passed": outcome["state"] in ("done",) and all(c["ok"] for c in checks),
            "metrics": grade.extract_metrics(traces),
            "expect_delegation": task.get("expect_delegation", "optional"),
        })
        (evidence / "answer.md").write_text(grade.redact(answer, secrets))
        (evidence / "traces.json").write_text(grade.redact(json.dumps(traces, indent=2, ensure_ascii=False), secrets))
        copy_workspace(workspace, evidence / "workspace")
    except Exception as exc:  # keep the first failure as evidence; no retry here
        record["harness_error"] = grade.redact(f"{type(exc).__name__}: {exc}", secrets)
        record["passed"] = False
    finally:
        mark[0] = time.monotonic()
        engine.stop()
        phase("stop")
        record["wall_s"] = round(time.monotonic() - started, 1)
        if engine.log_path.exists():
            log = engine.log_path.read_text(errors="replace")
            (evidence / "engine.log").write_text(grade.redact(log, secrets))
            # Its memory-reviewer tokens are then a lower bound, not the cost.
            record["review_cut_off"] = grade.review_cut_off(log)
        shutil.rmtree(data_dir, ignore_errors=True)
    (evidence / "result.json").write_text(grade.redact(json.dumps(record, indent=2, ensure_ascii=False), secrets))
    return record


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--provider", help="provider id in --config; required unless --mock")
    p.add_argument("--config", default=str(DEFAULT_CONFIG), help="config.yaml to read the provider from (read-only)")
    p.add_argument("--runs", type=int, default=2, help="runs per task")
    p.add_argument("--tasks", default="", help="comma-separated task ids (default: all)")
    p.add_argument("--out", default=str(REPO / "output" / "agent-eval"), help="evidence root")
    p.add_argument("--zwai", default=str(REPO / "bin" / "zwai-eval"), help="zwai binary")
    p.add_argument("--boot-timeout", type=float, default=240, help="seconds for engine startup")
    p.add_argument("--review-wait", type=float, default=240, help="seconds to wait for the memory review")
    p.add_argument("--mock", action="store_true", help="scripted offline provider: harness smoke test, not an eval")
    args = p.parse_args(argv)
    if not args.mock and not args.provider:
        p.error("--provider is required unless --mock")
    if args.runs < 1:
        p.error("--runs must be at least 1")
    return args


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    provider = None
    if not args.mock:
        provider = grade.extract_provider(Path(args.config).read_text(), args.provider)
    tasks = load_tasks([t for t in args.tasks.split(",") if t])
    stamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    out_dir = Path(args.out) / stamp
    out_dir.mkdir(parents=True, exist_ok=True)
    secrets = [provider["api_key"]] if provider and provider.get("api_key") else []
    results = []
    for task in tasks:
        for run_idx in range(1, args.runs + 1):
            print(f"[{task['id']} run {run_idx}/{args.runs}] ...", flush=True)
            rec = run_once(task, run_idx, args, provider, out_dir)
            status = "PASS" if rec.get("passed") else ("ERROR" if rec.get("harness_error") else "FAIL")
            print(f"[{task['id']} run {run_idx}/{args.runs}] {status} in {rec['wall_s']}s", flush=True)
            results.append(rec)
    meta = {
        "started": stamp,
        "provider": grade.provider_summary(provider) if provider else {"mock": True},
        "runs_per_task": args.runs,
        "zwai": args.zwai,
        "source": grade.git_describe(REPO),
    }
    summary = grade.summarize(results)
    report = {"meta": meta, "summary": summary, "results": results}
    (out_dir / "report.json").write_text(grade.redact(json.dumps(report, indent=2, ensure_ascii=False), secrets))
    (out_dir / "report.md").write_text(grade.redact(grade.render_markdown(meta, summary, results), secrets))
    print(f"report: {out_dir / 'report.md'}")
    return 0 if all(not r.get("harness_error") for r in results) else 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
