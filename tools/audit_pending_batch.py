#!/usr/bin/env python3
"""Reconcile outstanding publication independently of GitHub Issue closure."""
import argparse
import json
import os
import subprocess
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
home = Path(os.environ.get("ZWAI_HOME", Path.home() / ".zwai-swarm"))
parser.add_argument("--state", type=Path,
                    default=home / "issue-automation/delivery-state.json")
state_path = parser.parse_args().state
root = state_path.parent
state = json.loads(state_path.read_text())
ios = json.loads((root.parent / "ios-signing/testflight-releases.json").read_text())
repo = state["integration_checkout"]
raw = subprocess.check_output([
    "gh", "api", "--paginate", "--slurp",
    "repos/" + state["repository"] + "/issues?state=all&per_page=100",
], text=True)
github_states = {r["number"]: r["state"] for page in json.loads(raw)
                 for r in page if "pull_request" not in r}
rows = {int(n): r for n, r in state["issues"].items()}
ios_rows = {r["issue"]: r for r in ios["pending_changes"] if r.get("status") not in ("published", "delivered")}
# A closed Issue can still be in an unpublished batch or awaiting TestFlight.
candidates = set(state["pending_new_batch"]["issues"]) | set(ios_rows)
accepted = []
for number in sorted(candidates):
    row = rows.get(number)
    if not row or row.get("code") != "verified_integrated_and_pushed":
        raise SystemExit(f"Issue #{number}: missing accepted code evidence in delivery ledger")
    sha = row.get("fix_sha") or row.get("branch_sha")
    if not sha:
        raise SystemExit(f"Issue #{number}: missing fix SHA")
    subprocess.run(["git", "merge-base", "--is-ancestor", sha, "HEAD"], cwd=repo, check=True)
    pending = any(row.get(p) not in ("published", "delivered", "not_required", "not_required_by_behavior_change", "published_previous_release") for p in ("android", "macos", "ios", "server"))
    if pending:
        accepted.append(number)
minimum = state["minimum_distinct_issues"]
locked = state["pending_new_batch"].get("version_locked", False)
print(json.dumps({
    "issues": accepted,
    "closed_pending_issues": [n for n in accepted if github_states.get(n) == "closed"],
    "distinct_code_accepted_pending_count": len(accepted),
    "minimum": minimum,
    "qualified": len(accepted) >= minimum,
    "batch_version": state["pending_new_batch"].get("version"),
    "version_locked": locked,
    "allocate_new_version": len(accepted) >= minimum and not locked,
    "main_sha": subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip(),
}, ensure_ascii=False))
