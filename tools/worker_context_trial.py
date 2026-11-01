#!/usr/bin/env python3
"""Freeze and run a paired, opt-in Registry/ADK context-reference experiment.

The helper uses real manager model calls over fixed historical worker results.
It does not repeat the workers' original operations or measure production cost.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import signal
import subprocess
import sys
import tempfile
import time

import agent_eval_grade as grade
from superagent_eval import encoded, private_write, provider_config_digest

REPO = Path(__file__).resolve().parent.parent


class TrialCancelled(Exception):
    pass


def install_termination_handlers() -> None:
    def cancel(signum, _frame):
        raise TrialCancelled(f"received {signal.Signals(signum).name}")
    signal.signal(signal.SIGTERM, cancel)
    signal.signal(signal.SIGINT, cancel)


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def paired_order(ids: list[str], repeats: int, seed: int) -> list[dict]:
    rng = random.Random(seed)
    ids = list(ids)
    rng.shuffle(ids)
    order = []
    for case in ids:
        first = rng.choice(("a", "b"))
        for repeat in range(1, repeats + 1):
            arms = [first, "b" if first == "a" else "a"]
            if repeat % 2 == 0:
                arms.reverse()
            order.extend({"case": case, "repeat": repeat, "arm": arm} for arm in arms)
    return order


def load_inputs(root: Path) -> tuple[dict, dict]:
    tasks = json.loads((root / "tasks.json").read_text())
    oracle = json.loads((root / "expectations.json").read_text())
    cases = tasks.get("cases", [])
    ids = [c.get("id") for c in cases]
    if not cases or len(set(ids)) != len(ids):
        raise ValueError("case ids must be unique and nonempty")
    for case in cases:
        name = case["id"]
        if not isinstance(name, str) or not name or any(x not in "abcdefghijklmnopqrstuvwxyz0123456789-" for x in name):
            raise ValueError("case id must be a safe lowercase name")
        fixture = (root / case["fixture"]).resolve()
        if not fixture.is_relative_to(root.resolve()) or not fixture.is_file():
            raise ValueError("fixture must remain in input directory")
        body = json.loads(fixture.read_text())
        if not body.get("workers") or not body.get("rounds") or not body.get("instruction"):
            raise ValueError("fixture requires workers, rounds and instruction")
        # The model file is deliberately smaller than source/oracle metadata.
        if set(body) - {"schema_version", "instruction", "messages", "rounds", "workers"}:
            raise ValueError("model fixture contains non-model metadata")
        if any(set(w) - {"agent_id", "role", "generation", "status", "result", "error"} for w in body["workers"]):
            raise ValueError("worker fixture contains source/oracle metadata")
    if set(oracle.get("cases", {})) != set(ids):
        raise ValueError("oracle and task identities differ")
    return tasks, oracle


def freeze(args, provider: dict) -> dict:
    out = Path(args.out).resolve()
    if out.is_relative_to(REPO):
        raise ValueError("private evidence must remain outside repository")
    tasks, _ = load_inputs(Path(args.inputs).resolve())
    paths = [Path(args.helper).resolve(), Path(__file__).resolve(),
             Path(grade.__file__).resolve(), REPO / "tools/superagent_eval.py",
             REPO / "tools/agent_eval.py", REPO / "tools/worker_context_analysis.py", REPO / "go.mod", REPO / "go.sum"]
    paths += sorted((REPO / "tools/workercontext_eval").glob("*.go"))
    paths += [Path(p).resolve() for p in args.freeze_file]
    paths += [p for p in Path(args.inputs).resolve().rglob("*") if p.is_file()]
    if any(p.is_symlink() for p in paths):
        raise ValueError("frozen inputs must not be symbolic links")
    manifest = {"schema_version": 1, "measurement": "fixed-worker Registry/ADK manager coordination",
        "source": grade.git_describe(REPO), "provider": grade.provider_summary(provider),
        "provider_config_sha256": provider_config_digest(provider),
        "helper": str(Path(args.helper).resolve()), "inputs": str(Path(args.inputs).resolve()),
        "case_sources": {c["id"]: {"source_alias": c["source_alias"], "split": c["split"],
            "rounds": len(json.loads((Path(args.inputs) / c["fixture"]).read_text())["rounds"])} for c in tasks["cases"]},
        "hashes": {str(p): digest(p) for p in paths},
        "conditions": {k: getattr(args, k) for k in ("max_completion_tokens", "max_iterations", "timeout_seconds")},
        "order": paired_order([c["id"] for c in tasks["cases"]], args.repeats, args.seed),
        "seed": args.seed, "subprocess_umask": "inherited unchanged",
        "scope": "Real manager calls; fixed worker outputs; not full upstream architecture or production execution."}
    out.mkdir(mode=0o700, parents=True, exist_ok=False)
    private_write(out / "manifest.json", encoded(manifest))
    return manifest


def verify(manifest: dict, provider: dict) -> None:
    if manifest["provider_config_sha256"] != provider_config_digest(provider):
        raise ValueError("effective provider differs from frozen plan")
    for name, expected in manifest["hashes"].items():
        if digest(Path(name)) != expected:
            raise ValueError(f"frozen input changed: {name}")


def run_one(row: dict, manifest: dict, provider: dict, out: Path) -> dict:
    tasks, _ = load_inputs(Path(manifest["inputs"]))
    case = next(c for c in tasks["cases"] if c["id"] == row["case"])
    run_dir = out / "runs" / row["case"] / f"repeat-{row['repeat']}" / row["arm"]
    run_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
    config_parent = Path(tempfile.mkdtemp(prefix="worker-context-provider-"))
    config_file = config_parent / "config.yaml"
    private_write(config_file, grade.render_eval_config(provider, "eval", manifest["conditions"]["max_completion_tokens"]))
    env = {k: v for k, v in os.environ.items() if not k.startswith(("ZWAI_", "OPENAI_"))}
    home = config_parent / "home"
    home.mkdir(mode=0o700)
    env.update(HOME=str(home), ZDOTDIR=str(home), XDG_CONFIG_HOME=str(home / ".config"),
               XDG_CACHE_HOME=str(home / ".cache"), XDG_DATA_HOME=str(home / ".local/share"))
    argv = [manifest["helper"], "--config", str(config_file), "--provider", "eval",
            "--fixture", str(Path(manifest["inputs"]) / case["fixture"]),
            "--out", str(run_dir / "helper"), "--arm", row["arm"]]
    for key, value in manifest["conditions"].items():
        argv.extend(["--" + key.replace("_", "-"), str(value)])
    started = time.monotonic()
    record = {**row, "source_alias": case["source_alias"], "split": case["split"], "harness_error": ""}
    proc = None
    stdout = stderr = ""
    try:
        proc = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                env=env, cwd=str(config_parent), start_new_session=True)
        rounds = len(json.loads((Path(manifest["inputs"]) / case["fixture"]).read_text())["rounds"])
        try:
            stdout, stderr = proc.communicate(timeout=manifest["conditions"]["timeout_seconds"] * rounds + 30)
        except subprocess.TimeoutExpired:
            proc.send_signal(signal.SIGTERM)
            try:
                stdout, stderr = proc.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                stdout, stderr = proc.communicate(timeout=10)
            record["harness_error"] = "owned helper exceeded whole-session deadline"
        result_file = run_dir / "helper" / "result.json"
        if result_file.is_file():
            record["result"] = json.loads(result_file.read_text())
        else:
            record["harness_error"] = record["harness_error"] or "helper did not retain result.json"
        record["exit_status"] = proc.returncode
        if proc.returncode:
            record["harness_error"] = record["harness_error"] or f"helper exited {proc.returncode}"
    except Exception as exc:
        record["harness_error"] = f"{type(exc).__name__}: {exc}"
        record["cancelled"] = isinstance(exc, TrialCancelled)
    finally:
        if proc is not None and proc.poll() is None:
            proc.send_signal(signal.SIGTERM)
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)
        record["helper_exited"] = proc is None or proc.poll() is not None
        record["wall_s"] = time.monotonic() - started
        for name, value in (("stdout.txt", stdout), ("stderr.txt", stderr)):
            private_write(run_dir / name, grade.redact(value, [provider.get("api_key", "")]))
        # Only this run's credential/home directory; helper artifacts stay durable.
        import shutil
        if record["helper_exited"]:
            shutil.rmtree(config_parent)
        record["private_config_removed"] = not config_parent.exists()
    private_write(run_dir / "record.json", grade.redact(encoded(record), [provider.get("api_key", "")]))
    return record


def execute(manifest: dict, provider: dict, out: Path) -> dict:
    verify(manifest, provider)
    if (out / "execution-started.json").exists() or (out / "runs").exists():
        raise ValueError("plan already started; preserve original observations")
    private_write(out / "execution-started.json", encoded({"started_at_unix": time.time()}))
    report = {"schema_version": 1, "results": [], "semantic_status": "unreviewed"}
    try:
        for row in manifest["order"]:
            verify(manifest, provider)
            print(f"{row['case']} {row['arm']} repeat {row['repeat']}", flush=True)
            record = run_one(row, manifest, provider, out)
            report["results"].append(record)
            if record.get("cancelled"):
                report["stopped_early"] = "cancellation"
                break
            cleanup = record.get("result", {}).get("cleanup", {})
            if (not record["helper_exited"] or not record["private_config_removed"]
                    or not cleanup.get("registry_closed") or cleanup.get("live_workers") != 0):
                report["stopped_early"] = "owned cleanup unverified"
                break
    finally:
        private_write(out / "report.json", encoded(report))
    return report


def main(argv=None) -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--config", required=True)
    p.add_argument("--provider", required=True)
    p.add_argument("--model", default="")
    p.add_argument("--helper")
    p.add_argument("--inputs")
    p.add_argument("--out")
    p.add_argument("--freeze-file", action="append", default=[], help="Additional protocol/source provenance inputs to freeze")
    p.add_argument("--repeats", type=int, default=3)
    p.add_argument("--seed", type=int, default=20261102)
    p.add_argument("--max-completion-tokens", type=int, default=0)
    p.add_argument("--max-iterations", type=int, default=20)
    p.add_argument("--timeout-seconds", type=float, default=240)
    mode = p.add_mutually_exclusive_group(required=True)
    mode.add_argument("--plan-only", action="store_true")
    mode.add_argument("--execute-plan")
    args = p.parse_args(argv)
    if args.repeats < 1 or args.max_iterations < 1 or args.timeout_seconds <= 0 or args.max_completion_tokens < 0:
        p.error("invalid trial budget")
    provider = grade.select_model(grade.extract_provider(Path(args.config).read_text(), args.provider), args.model)
    if args.execute_plan:
        install_termination_handlers()
        path = Path(args.execute_plan).resolve()
        report = execute(json.loads(path.read_text()), provider, path.parent)
        return 2 if report.get("stopped_early") or any(r["harness_error"] for r in report["results"]) else 0
    if not all((args.helper, args.inputs, args.out)):
        p.error("--helper, --inputs and --out are required to freeze a plan")
    freeze(args, provider)
    print(Path(args.out).resolve() / "manifest.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
