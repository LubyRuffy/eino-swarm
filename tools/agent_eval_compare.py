#!/usr/bin/env python3
"""Compare agent-eval reports across arms (models, prompt variants, binaries).

Each arm is LABEL=DIR, where DIR holds one or more report.json files written by
tools/agent_eval.py (an --out root or a single report directory); repeating a
label adds another directory to that arm. The first arm
is the reference: every task whose pass rate differs from it is listed, because
a total can hide a task that broke while another started passing. Reads only.
"""

from __future__ import annotations

import argparse
import json
import statistics
import sys
from pathlib import Path


def load_arm(root: Path) -> tuple[list[dict], list[dict]]:
    """Results and metas from every report.json under root, in path order."""
    # Evidence under runs/ holds workspace copies, and a task may write its own
    # report.json there; only the harness's reports describe runs.
    paths = [root] if root.is_file() else sorted(
        p for p in root.rglob("report.json") if "runs" not in p.relative_to(root).parts)
    results, metas = [], []
    for p in paths:
        report = json.loads(p.read_text())
        if not isinstance(report, dict) or "meta" not in report:
            continue
        results.extend(report.get("results", []))
        metas.append(report.get("meta", {}))
    if not results:
        raise SystemExit(f"{root}: no report.json with results")
    return results, metas


def _median(values: list[float]) -> float | None:
    return statistics.median(values) if values else None


def task_stats(results: list[dict]) -> dict:
    """Graded runs only: a harness error is not the model's result."""
    graded = [r for r in results if not r.get("harness_error")]
    m = [r.get("metrics", {}) for r in graded]
    return {
        "runs": len(graded),
        "passed": sum(1 for r in graded if r.get("passed")),
        "harness_errors": len(results) - len(graded),
        "tokens": _median([x.get("total_tokens", 0) for x in m]),
        "seconds": _median([x.get("duration_ms", 0) / 1000 for x in m]),
        "manager_calls": _median([x.get("manager_calls", 0) for x in m]),
        "spawned": sum(1 for x in m if x.get("spawn_calls", 0) > 0),
    }


def arm_stats(results: list[dict]) -> dict:
    by_task: dict[str, list[dict]] = {}
    for r in results:
        by_task.setdefault(r["task"], []).append(r)
    graded = [r for r in results if not r.get("harness_error")]
    m = [r.get("metrics", {}) for r in graded]
    first = [x.get("manager_first_call") or {} for x in m]
    prompt = sum(f.get("prompt_tokens", 0) for f in first)
    cached = sum(f.get("cached_tokens", 0) for f in first)
    return {
        "tasks": {t: task_stats(rs) for t, rs in by_task.items()},
        "runs": len(graded),
        "passed": sum(1 for r in graded if r.get("passed")),
        "harness_errors": len(results) - len(graded),
        "tokens_total": sum(x.get("total_tokens", 0) for x in m),
        "tokens_median": _median([x.get("total_tokens", 0) for x in m]),
        "seconds_median": _median([x.get("duration_ms", 0) / 1000 for x in m]),
        "prompt_chars": _median([x.get("manager_prompt_chars", 0) for x in m if x.get("manager_prompt_chars")]),
        "first_call_hit": (cached / prompt) if prompt else None,
        "failures": [
            {"task": r["task"], "run": r.get("run"), "state": (r.get("outcome") or {}).get("state", ""),
             "failed": [c["name"] for c in r.get("checks", []) if not c.get("ok")]}
            for r in graded if not r.get("passed")
        ],
    }


def flips(arms: dict[str, dict]) -> list[dict]:
    labels = list(arms)
    ref = labels[0]
    tasks = sorted({t for a in arms.values() for t in a["tasks"]})
    out = []
    for t in tasks:
        rates = {}
        for label in labels:
            s = arms[label]["tasks"].get(t)
            rates[label] = (s["passed"], s["runs"]) if s else None
        base = rates[ref]
        base_rate = base[0] / base[1] if base and base[1] else None
        changed = []
        for label in labels[1:]:
            r = rates[label]
            rate = r[0] / r[1] if r and r[1] else None
            if rate != base_rate:
                changed.append(label)
        if changed:
            out.append({"task": t, "rates": rates, "changed": changed})
    return out


def _num(v: float | None, fmt: str = "{:.0f}") -> str:
    return "–" if v is None else fmt.format(v)


def _k(v: float | None) -> str:
    return "–" if v is None else f"{v / 1000:.0f}K"


def render(arms: dict[str, dict], metas: dict[str, list[dict]]) -> str:
    labels = list(arms)
    lines = ["# Agent eval comparison", ""]
    for label in labels:
        models = sorted({(m.get("provider") or {}).get("model", "") for m in metas[label]})
        bins = sorted({m.get("zwai", "") for m in metas[label]})
        lines.append(f"- **{label}**: model {', '.join(models)}; binary {', '.join(bins)}")
    lines += ["", "| arm | passed | harness errors | median tokens | total tokens | median s | system prompt chars | first-call cache |",
              "|---|---|---|---|---|---|---|---|"]
    for label in labels:
        a = arms[label]
        hit = "–" if a["first_call_hit"] is None else f"{a['first_call_hit']:.0%}"
        lines.append(f"| {label} | {a['passed']}/{a['runs']} | {a['harness_errors']} | {_k(a['tokens_median'])} | "
                     f"{_k(a['tokens_total'])} | {_num(a['seconds_median'])} | {_num(a['prompt_chars'])} | {hit} |")
    tasks = sorted({t for a in arms.values() for t in a["tasks"]})
    lines += ["", "## By task", "", "| task | " + " | ".join(labels) + " |", "|---|" + "---|" * len(labels)]
    for t in tasks:
        cells = []
        for label in labels:
            s = arms[label]["tasks"].get(t)
            cells.append("–" if not s else f"{s['passed']}/{s['runs']} · {_k(s['tokens'])} · {_num(s['seconds'])}s"
                         + (f" · spawned {s['spawned']}" if s["spawned"] else ""))
        lines.append(f"| {t} | " + " | ".join(cells) + " |")
    lines += ["", "Cells: passed/graded runs · median tokens · median seconds · runs that spawned a worker.", ""]
    changed = flips(arms)
    lines.append(f"## Tasks whose pass rate differs from {labels[0]}")
    lines.append("")
    if not changed:
        lines.append("None.")
    for f in changed:
        rates = ", ".join(f"{k} {v[0]}/{v[1]}" if v else f"{k} –" for k, v in f["rates"].items())
        lines.append(f"- {f['task']}: {rates}")
    lines += ["", "## Failed runs", ""]
    any_failure = False
    for label in labels:
        for f in arms[label]["failures"]:
            any_failure = True
            lines.append(f"- {label} · {f['task']} run {f['run']} · {f['state']} · {', '.join(f['failed']) or 'no failed check'}")
    if not any_failure:
        lines.append("None.")
    return "\n".join(lines) + "\n"


def parse_arms(specs: list[str]) -> list[tuple[str, list[Path]]]:
    """Arms in first-appearance order. A repeated label adds another directory
    to that arm (runs split across processes); the same directory twice would
    count its runs twice and is refused."""
    arms: dict[str, list[Path]] = {}
    for spec in specs:
        label, sep, path = spec.partition("=")
        if not sep or not label.strip() or not path.strip():
            raise SystemExit(f"--arm wants LABEL=DIR, got {spec!r}")
        root = Path(path.strip())
        dirs = arms.setdefault(label.strip(), [])
        if root.resolve() in {d.resolve() for d in dirs}:
            raise SystemExit(f"{root} is given twice for arm {label.strip()!r}")
        dirs.append(root)
    return list(arms.items())


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--arm", action="append", required=True, help="LABEL=DIR; the first arm is the reference; repeat a label to add a directory")
    p.add_argument("--out", default="", help="also write the markdown here")
    args = p.parse_args(argv)
    arms, metas = {}, {}
    for label, roots in parse_arms(args.arm):
        results, meta = [], []
        for root in roots:
            r, m = load_arm(root)
            results += r
            meta += m
        arms[label] = arm_stats(results)
        metas[label] = meta
    text = render(arms, metas)
    if args.out:
        Path(args.out).write_text(text)
    sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
