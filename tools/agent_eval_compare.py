#!/usr/bin/env python3
"""Compare agent-eval reports across arms (models, prompt variants, binaries).

Each arm is LABEL=DIR, where DIR holds one or more report.json files written by
tools/agent_eval.py (an --out root or a single report directory); repeating a
label adds another directory to that arm. The first arm
is the reference: every task whose pass rate differs from it is listed, because
a total can hide a task that broke while another started passing. Reads only.

--gate exits 2 when a later arm lowers a graded reference task's pass rate,
omits that task, or adds a harness error on it. Time and tokens stay
descriptive. A spec hidden-rule failure is self_attested only when the run's
Trace shows a successful execution of that task's visible test_basic.py.
"""

from __future__ import annotations

import argparse
import json
import re
import statistics
import sys
from pathlib import Path

import agent_eval_grade as grade


TASKS_DIR = Path(__file__).resolve().parent.parent / "evals" / "tasks"


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
        for item in report.get("results", []):
            if not isinstance(item, dict):
                continue
            row = dict(item)
            row["_report_dir"] = str(p.parent)
            results.append(row)
        metas.append(report.get("meta", {}))
    if not results:
        raise SystemExit(f"{root}: no report.json with results")
    return results, metas


def _median(values: list[float]) -> float | None:
    return statistics.median(values) if values else None


def _known_int(metrics: dict, key: str) -> int | None:
    if not isinstance(metrics, dict) or key not in metrics:
        return None
    value = metrics.get(key)
    if type(value) is int and value >= 0:
        return value
    return None


def _usage_total(metrics: dict) -> int | None:
    """Provider usage when every call reported it. Missing usage is not zero."""
    if not isinstance(metrics, dict):
        return None
    facts = metrics.get("usage_facts")
    if isinstance(facts, dict) and "total_tokens" in facts:
        return _known_int(facts, "total_tokens")
    return _known_int(metrics, "total_tokens")


def _measurement(values: list[int | None]) -> tuple[bool | None, float | None]:
    """None when nothing was graded; False when any graded value is missing."""
    if not values:
        return None, None
    if any(value is None for value in values):
        return False, None
    return True, _median([value for value in values if value is not None])


def task_stats(results: list[dict]) -> dict:
    """Graded runs only: a harness error is not the model's result."""
    graded = [r for r in results if not r.get("harness_error")]
    metrics = [r.get("metrics", {}) for r in graded]
    tokens_known, tokens = _measurement([_usage_total(item) for item in metrics])
    seconds_known, millis = _measurement([_known_int(item, "duration_ms") for item in metrics])
    return {
        "runs": len(graded),
        "passed": sum(1 for r in graded if r.get("passed")),
        "harness_errors": len(results) - len(graded),
        "tokens": tokens,
        "tokens_known": tokens_known,
        "seconds": None if millis is None else millis / 1000,
        "seconds_known": seconds_known,
        "manager_calls": _median([item.get("manager_calls", 0) for item in metrics]),
        "spawned": sum(1 for item in metrics if item.get("spawn_calls", 0) > 0),
    }


def arm_stats(results: list[dict]) -> dict:
    by_task: dict[str, list[dict]] = {}
    for r in results:
        by_task.setdefault(r["task"], []).append(r)
    graded = [r for r in results if not r.get("harness_error")]
    metrics = [r.get("metrics", {}) for r in graded]
    first = [item.get("manager_first_call") or {} for item in metrics]
    prompt = sum(item.get("prompt_tokens", 0) for item in first)
    cached = sum(item.get("cached_tokens", 0) for item in first)
    tokens_known, tokens = _measurement([_usage_total(item) for item in metrics])
    seconds_known, millis = _measurement([_known_int(item, "duration_ms") for item in metrics])
    return {
        "tasks": {task: task_stats(rows) for task, rows in by_task.items()},
        "runs": len(graded),
        "passed": sum(1 for r in graded if r.get("passed")),
        "harness_errors": len(results) - len(graded),
        "tokens_total": None if not tokens_known else sum(_usage_total(item) or 0 for item in metrics),
        "tokens_median": tokens,
        "tokens_known": tokens_known,
        "seconds_median": None if millis is None else millis / 1000,
        "seconds_known": seconds_known,
        "prompt_chars": _median([item.get("manager_prompt_chars", 0) for item in metrics if item.get("manager_prompt_chars")]),
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
    tasks = sorted({task for arm in arms.values() for task in arm["tasks"]})
    out = []
    for task in tasks:
        rates = {}
        for label in labels:
            stats = arms[label]["tasks"].get(task)
            rates[label] = (stats["passed"], stats["runs"]) if stats else None
        base = rates[ref]
        base_rate = base[0] / base[1] if base and base[1] else None
        changed = []
        for label in labels[1:]:
            rate = rates[label]
            value = rate[0] / rate[1] if rate and rate[1] else None
            if value != base_rate:
                changed.append(label)
        if changed:
            out.append({"task": task, "rates": rates, "changed": changed})
    return out


def regression_failures(arms: dict[str, dict]) -> list[str]:
    """Reasons a candidate cannot replace the reference. Cost is not one."""
    labels = list(arms)
    if len(labels) < 2:
        return ["a candidate arm is required"]
    reference = labels[0]
    failures = []
    for label in labels[1:]:
        for task, base in sorted(arms[reference]["tasks"].items()):
            other = arms[label]["tasks"].get(task)
            if other is None:
                failures.append(f"{label} is missing {task}")
                continue
            if base["runs"] and other["harness_errors"]:
                failures.append(f"{label} {task} has a harness error after {reference} graded it")
            if base["runs"] and other["runs"] and other["passed"] * base["runs"] < base["passed"] * other["runs"]:
                failures.append(
                    f"{label} {task} pass rate {other['passed']}/{other['runs']} "
                    f"is below {reference} {base['passed']}/{base['runs']}")
    return failures


def _num(v: float | None, fmt: str = "{:.0f}") -> str:
    return "–" if v is None else fmt.format(v)


def _k(v: float | None) -> str:
    return "–" if v is None else f"{v / 1000:.0f}K"


def _shown(known: bool | None, text: str) -> str:
    if known is None:
        return "–"
    if not known:
        return "incomparable"
    return text


def _task_cell(stats: dict | None, show_cost: bool) -> str:
    if not stats:
        return "–"
    cell = f"{stats['passed']}/{stats['runs']}"
    if show_cost and stats["runs"]:
        cell += f" · {_shown(stats['tokens_known'], _k(stats['tokens']))}"
        cell += f" · {_shown(stats['seconds_known'], _num(stats['seconds']))}s"
    if stats["spawned"]:
        cell += f" · spawned {stats['spawned']}"
    return cell


def render(arms: dict[str, dict], metas: dict[str, list[dict]]) -> str:
    labels = list(arms)
    lines = ["# Agent eval comparison", ""]
    for label in labels:
        models = sorted({(item.get("provider") or {}).get("model", "") for item in metas[label]})
        bins = sorted({item.get("zwai", "") for item in metas[label]})
        lines.append(f"- **{label}**: model {', '.join(models)}; binary {', '.join(bins)}")
    lines += ["", "| arm | passed | harness errors | median tokens | total tokens | median s | system prompt chars | first-call cache |",
              "|---|---|---|---|---|---|---|---|"]
    for label in labels:
        arm = arms[label]
        hit = "–" if arm["first_call_hit"] is None else f"{arm['first_call_hit']:.0%}"
        lines.append(
            f"| {label} | {arm['passed']}/{arm['runs']} | {arm['harness_errors']} | "
            f"{_shown(arm['tokens_known'], _k(arm['tokens_median']))} | "
            f"{_shown(arm['tokens_known'], _k(arm['tokens_total']))} | "
            f"{_shown(arm['seconds_known'], _num(arm['seconds_median']))} | "
            f"{_num(arm['prompt_chars'])} | {hit} |")
    tasks = sorted({task for arm in arms.values() for task in arm["tasks"]})
    lines += ["", "## By task", "", "| task | " + " | ".join(labels) + " |", "|---|" + "---|" * len(labels)]
    for task in tasks:
        show_cost = all(arms[label]["tasks"].get(task, {}).get("runs", 0) > 0 for label in labels)
        cells = [_task_cell(arms[label]["tasks"].get(task), show_cost) for label in labels]
        lines.append(f"| {task} | " + " | ".join(cells) + " |")
    lines += ["",
              "Cells: passed/graded runs. Median tokens and seconds appear only when every arm graded that task. Incomplete usage is incomparable, not zero.",
              ""]
    changed = flips(arms)
    lines.append(f"## Tasks whose pass rate differs from {labels[0]}")
    lines.append("")
    if not changed:
        lines.append("None.")
    for item in changed:
        rates = ", ".join(f"{label} {rate[0]}/{rate[1]}" if rate else f"{label} –"
                          for label, rate in item["rates"].items())
        lines.append(f"- {item['task']}: {rates}")
    lines += ["", "## Failed runs", ""]
    any_failure = False
    for label in labels:
        for failure in arms[label]["failures"]:
            any_failure = True
            lines.append(f"- {label} · {failure['task']} run {failure['run']} · {failure['state']} · "
                         f"{', '.join(failure['failed']) or 'no failed check'}")
    if not any_failure:
        lines.append("None.")
    return "\n".join(lines) + "\n"


def task_definition(task_id: str, tasks_dir: Path = TASKS_DIR) -> dict | None:
    path = tasks_dir / task_id / "task.json"
    if not path.is_file():
        return None
    try:
        data = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def hidden_check_names(task: dict) -> set[str]:
    names = set()
    for check in task.get("checks") or []:
        if not isinstance(check, dict) or check.get("type") != "command":
            continue
        argv = check.get("argv") or []
        if any("hidden/" in str(part) for part in argv):
            name = check.get("name")
            if isinstance(name, str) and name:
                names.add(name)
    return names


def visible_test_paths(task: dict) -> tuple[str, ...]:
    paths = []
    for check in task.get("checks") or []:
        if not isinstance(check, dict) or check.get("type") != "file_unchanged":
            continue
        path = check.get("path")
        if isinstance(path, str) and Path(path).name == "test_basic.py":
            paths.append(path)
    return tuple(paths)


def _trace_events(traces) -> list[dict]:
    if isinstance(traces, dict):
        traces = [traces]
    if not isinstance(traces, list):
        return []
    events = []
    for turn in traces:
        if not isinstance(turn, dict):
            continue
        nested = turn.get("events")
        if isinstance(nested, list):
            events.extend(event for event in nested if isinstance(event, dict))
        elif turn.get("kind"):
            events.append(turn)
    return events


def _mentions_path(value, paths: tuple[str, ...]) -> bool:
    if isinstance(value, str):
        for part in re.split(r"[\s\"'`|;]+", value.replace("\\", "/")):
            part = part.rstrip(");")
            if any(part == rel or part.endswith("/" + rel) for rel in paths):
                return True
        return False
    if isinstance(value, dict):
        return any(_mentions_path(item, paths) for item in value.values())
    if isinstance(value, list):
        return any(_mentions_path(item, paths) for item in value)
    return False


def _result_succeeded(text: str) -> bool:
    raw = (text or "").strip()
    if raw.lower().startswith(("error", "refused")):
        return False
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return False
    if not isinstance(payload, dict) or payload.get("failed") is True:
        return False
    return payload.get("exit_code") == 0


def visible_test_passed(events: list[dict], paths: tuple[str, ...]) -> bool:
    """A visible test counts only when its own tool result exited successfully."""
    if not paths:
        return False
    targeted = set()
    for event in events:
        if event.get("kind") != "tool_call":
            continue
        _, args = grade.parse_tool_call(event.get("text", ""))
        call_id = event.get("tool_call_id")
        if call_id and _mentions_path(args, paths):
            targeted.add(call_id)
    return any(event.get("kind") == "tool_result" and event.get("tool_call_id") in targeted
               and _result_succeeded(event.get("text") or "") for event in events)


def classify_hidden_failure(result: dict, tasks_dir: Path = TASKS_DIR) -> str | None:
    """self_attested, unknown, or checked. None when this run did not fail a hidden rule."""
    if result.get("harness_error"):
        return None
    task = task_definition(str(result.get("task") or ""), tasks_dir)
    if task is None:
        return None
    names = hidden_check_names(task)
    failed = [check.get("name") for check in result.get("checks") or []
              if isinstance(check, dict) and not check.get("ok")]
    if not names or not any(name in names for name in failed):
        return None
    root = result.get("_report_dir")
    run = result.get("run")
    if not root or run is None:
        return "unknown"
    path = Path(root) / "runs" / str(result.get("task")) / f"run-{run}" / "traces.json"
    if not path.is_file():
        return "unknown"
    try:
        traces = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError):
        return "unknown"
    if visible_test_passed(_trace_events(traces), visible_test_paths(task)):
        return "self_attested"
    return "checked"


def attestation_counts(results: list[dict], tasks_dir: Path = TASKS_DIR) -> dict[str, dict[str, int]]:
    counts: dict[str, dict[str, int]] = {}
    for result in results:
        status = classify_hidden_failure(result, tasks_dir)
        if status not in ("self_attested", "unknown"):
            continue
        bucket = counts.setdefault(str(result.get("task")), {"self_attested": 0, "unknown": 0})
        bucket[status] += 1
    return counts


def render_attestation(by_arm: dict[str, dict[str, dict[str, int]]]) -> str:
    lines = []
    for label, tasks in by_arm.items():
        for task, counts in sorted(tasks.items()):
            if not lines:
                lines = ["", "## Hidden-rule failures", ""]
            lines.append(f"- {label} {task}: self_attested {counts['self_attested']}, unknown {counts['unknown']}")
    return ("\n".join(lines) + "\n") if lines else ""


def render_gate(reference: str, failures: list[str]) -> str:
    lines = ["", "## Regression gate", ""]
    if failures:
        lines.append(f"Reference: {reference}")
        lines.extend(f"- {failure}" for failure in failures)
    else:
        lines.append(f"No pass-rate drop, missing task, or new harness error against {reference}.")
    lines.append("A higher pass rate is an observation from these runs, not a significance claim.")
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
        if root.resolve() in {item.resolve() for item in dirs}:
            raise SystemExit(f"{root} is given twice for arm {label.strip()!r}")
        dirs.append(root)
    return list(arms.items())


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--arm", action="append", required=True, help="LABEL=DIR; the first arm is the reference; repeat a label to add a directory")
    parser.add_argument("--out", default="", help="also write the markdown here")
    parser.add_argument("--gate", action="store_true",
                        help="exit 2 when a candidate drops, omits, or fails to grade a reference task")
    args = parser.parse_args(argv)
    arms, metas, results_by = {}, {}, {}
    for label, roots in parse_arms(args.arm):
        results, meta = [], []
        for root in roots:
            loaded, loaded_meta = load_arm(root)
            results += loaded
            meta += loaded_meta
        results_by[label] = results
        arms[label] = arm_stats(results)
        metas[label] = meta
    text = render(arms, metas)
    text += render_attestation({label: attestation_counts(results) for label, results in results_by.items()})
    if args.gate:
        failures = regression_failures(arms)
        text += render_gate(next(iter(arms)), failures)
    else:
        failures = []
    if args.out:
        Path(args.out).write_text(text)
    sys.stdout.write(text)
    return 2 if args.gate and failures else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
