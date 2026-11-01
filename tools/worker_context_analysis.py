#!/usr/bin/env python3
"""Inspect paired fixed-worker trials without turning repetitions into tasks."""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
import hashlib
import json
from pathlib import Path
import random
import statistics

from superagent_eval import encoded, private_write


def identity(row):
    return row["case"], row["repeat"], row["arm"]


def verify_inputs(manifest):
    for name, expected in manifest["hashes"].items():
        if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
            raise ValueError(f"frozen input changed before analysis: {name}")


def tokens(row):
    result = row.get("result", {})
    calls = result.get("calls")
    if (not isinstance(calls, list) or not calls or row.get("harness_error") or result.get("error")
            or result.get("cost_complete") is not True
            or any(r.get("error") for r in result.get("rounds", []))):
        return None
    requests = [c for c in calls if not c.get("backoff")]
    if not requests or any(c.get("error") or not isinstance(c.get("total_tokens"), (int, float))
                           or c["total_tokens"] <= 0 for c in requests):
        return None
    return sum(c["total_tokens"] for c in requests)


def bootstrap(pairs, seed=20261102, samples=10000):
    """Resample task sources after averaging nested repetitions per source."""
    if len(pairs) < 2 or any(a <= 0 or b < 0 for a, b in pairs):
        return {"reduction": None, "interval_95": None}
    base = sum(p[0] for p in pairs)
    effect = 1 - sum(p[1] for p in pairs) / base
    rng = random.Random(seed)
    estimates = []
    for _ in range(samples):
        draw = rng.choices(pairs, k=len(pairs))
        denominator = sum(p[0] for p in draw)
        if denominator <= 0:
            return {"reduction": effect, "interval_95": None}
        estimates.append(1 - sum(p[1] for p in draw) / denominator)
    estimates.sort()
    return {"reduction": effect, "interval_95": [estimates[int(samples * .025)], estimates[int(samples * .975) - 1]],
            "unit": "source, repetitions averaged first", "sources": len(pairs),
            "qualification": "Conditional on selected fixed historical result sources; not a population estimate."}


def analyze(manifest, report, reviews):
    rows = report.get("results", [])
    expected = Counter(identity(r) for r in manifest["order"])
    actual = Counter(identity(r) for r in rows)
    coverage_complete = actual == expected and all(n == 1 for n in actual.values())
    sources = manifest.get("case_sources", {})
    source_binding_complete = all(
        r["case"] in sources and r.get("source_alias") == sources[r["case"]]["source_alias"]
        and r.get("split") == sources[r["case"]]["split"] for r in rows)
    rounds_complete = all(r["case"] in sources and
        len(r.get("result", {}).get("rounds", [])) == sources[r["case"]]["rounds"] for r in rows)
    by_source = defaultdict(lambda: defaultdict(list))
    for row in rows:
        by_source[row["source_alias"]][row["arm"]].append(row)
    summary = {"coverage_complete": coverage_complete, "source_binding_complete": source_binding_complete,
               "rounds_complete": rounds_complete, "planned_sessions": sum(expected.values()),
               "observed_sessions": len(rows), "by_source": {}, "arms": {}}
    for arm in ("a", "b"):
        selected = [r for r in rows if r["arm"] == arm]
        usage = [tokens(r) for r in selected]
        verdicts = [reviews.get("/".join(map(str, identity(r)))) for r in selected]
        summary["arms"][arm] = {"sessions": len(selected), "tokens": sum(v for v in usage if v is not None),
            "cost_incomplete": sum(v is None for v in usage),
            "harness_errors": sum(bool(r.get("harness_error")) for r in selected),
            "semantic_reviewed": sum(v is not None for v in verdicts),
            "semantic_passed": sum(v is not None and v.get("passed") is True for v in verdicts),
            "critical_errors": sum(v.get("critical_errors", 0) for v in verdicts if v is not None),
            "median_session_seconds": statistics.median([r["wall_s"] for r in selected]) if selected else None,
            "cleanup_unverified": sum(not r.get("helper_exited") or not r.get("private_config_removed")
                                      or not r.get("result", {}).get("cleanup", {}).get("registry_closed")
                                      or r.get("result", {}).get("cleanup", {}).get("live_workers") != 0 for r in selected),
            "scope_violations": sum(len(r.get("result", {}).get("scope_violations", [])) for r in selected)}
    heldout_pairs = []
    for source, arms in by_source.items():
        slot = {"split": next(iter(arms.values()))[0]["split"]}
        for arm in ("a", "b"):
            usage = [tokens(r) for r in arms.get(arm, [])]
            slot[arm] = {"sessions": len(usage), "mean_tokens": statistics.mean(usage) if usage and None not in usage else None}
        summary["by_source"][source] = slot
        if slot["split"] == "heldout" and all(slot[a]["mean_tokens"] is not None for a in ("a", "b")):
            heldout_pairs.append((slot["a"]["mean_tokens"], slot["b"]["mean_tokens"]))
    summary["heldout_effect"] = bootstrap(heldout_pairs)
    a, b = summary["arms"]["a"], summary["arms"]["b"]
    reviewed = all(v["semantic_reviewed"] == v["sessions"] for v in (a, b))
    complete = coverage_complete and source_binding_complete and rounds_complete and not report.get("stopped_early") and all(
        not v[k] for v in (a, b) for k in ("cost_incomplete", "harness_errors", "cleanup_unverified", "scope_violations"))
    effect = summary["heldout_effect"]
    interval = effect["interval_95"]
    summary["screen_passed"] = bool(complete and reviewed and len(heldout_pairs) >= 3
        and b["critical_errors"] == 0 and b["semantic_passed"] == b["sessions"]
        and b["semantic_passed"] >= a["semantic_passed"]
        and effect["reduction"] is not None and effect["reduction"] >= .20 and interval and interval[0] > 0)
    summary["adoption"] = "not accepted: a passing screen still requires actual engine/HTTP integration and affected product regression"
    return summary


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("root", type=Path)
    p.add_argument("--reviews", type=Path, required=True)
    args = p.parse_args()
    manifest = json.loads((args.root / "manifest.json").read_text())
    verify_inputs(manifest)
    summary = analyze(manifest,
                      json.loads((args.root / "report.json").read_text()), json.loads(args.reviews.read_text()))
    private_write(args.root / "analysis.json", encoded(summary))
    print(args.root / "analysis.json")


if __name__ == "__main__":
    main()
