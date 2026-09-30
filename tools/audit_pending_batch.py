#!/usr/bin/env python3
"""Reconcile outstanding publication independently of GitHub Issue closure."""
import argparse
import json
import os
import subprocess
from datetime import datetime, timezone
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
delivered = {"published", "published_remote_verified", "delivered",
             "not_required", "not_required_by_behavior_change", "published_previous_release"}
ios_rows = {r["issue"]: r for r in ios["pending_changes"] if r.get("status") not in ("published", "delivered")}
# A closed Issue can still be in an unpublished batch or awaiting TestFlight.
candidates = set(state["pending_new_batch"]["issues"]) | set(ios_rows)
# Accepted desktop/server-only fixes need no iOS pending row. Their code
# records survive closure and the current allocated batch's recovery.
candidates |= {n for n, row in rows.items()
               if row.get("code") == "verified_integrated_and_pushed"}
accepted = []
for number in sorted(candidates):
    row = rows.get(number)
    if not row or row.get("code") != "verified_integrated_and_pushed":
        raise SystemExit(f"Issue #{number}: missing accepted code evidence in delivery ledger")
    if row.get("count_as_new_release_issue") is False:
        continue
    sha = row.get("fix_sha") or row.get("branch_sha")
    if not sha:
        raise SystemExit(f"Issue #{number}: missing fix SHA")
    subprocess.run(["git", "merge-base", "--is-ancestor", sha, "HEAD"], cwd=repo, check=True)
    pending = any(row.get(p) not in delivered for p in ("android", "macos", "ios", "server"))
    if pending:
        accepted.append(number)
locked = state["pending_new_batch"].get("version_locked", False)
current_batch = set(state["pending_new_batch"]["issues"])
allocated = {n for n in accepted if rows[n].get("batch_version") and (locked or n not in current_batch)}
if locked:
    allocated |= current_batch
unallocated = [n for n in accepted if n not in allocated]
commit_count = 0
oldest = None
additional = {}
if unallocated:
    baseline = state.get("last_published_source_sha")
    if not baseline and locked:
        baseline = state["pending_new_batch"].get("source_sha")
    if not baseline:
        raise SystemExit("Missing last published source SHA for the new batch audit")
    subprocess.run(["git", "merge-base", "--is-ancestor", baseline, "HEAD"], cwd=repo, check=True)
    next_batch = state.get("next_unallocated_batch", {})
    if next_batch.get("additional_platforms"):
        if set(next_batch.get("issues", [])) != set(unallocated):
            raise SystemExit("Extra platform scope does not match the unallocated batch")
        for platform, entry in next_batch["additional_platforms"].items():
            shas = entry.get("source_shas", [])
            if platform not in ("macos", "android", "ios") or not shas:
                raise SystemExit(f"Invalid extra platform scope: {platform}")
            for sha in shas:
                subprocess.run(["git", "merge-base", "--is-ancestor", baseline, sha], cwd=repo, check=True)
                subprocess.run(["git", "merge-base", "--is-ancestor", sha, "HEAD"], cwd=repo, check=True)
            if entry.get("status") not in delivered:
                additional[platform] = entry
    commit_count = int(subprocess.check_output(
        ["git", "rev-list", "--first-parent", "--count", f"{baseline}..HEAD"],
        cwd=repo, text=True).strip())
    for number in unallocated:
        row = rows[number]
        stamp = row.get("integrated_at") or row.get("accepted_at") or row.get("closed_at")
        if not stamp:
            stamp = subprocess.check_output(
                ["git", "show", "-s", "--format=%cI", row.get("main_sha") or row["fix_sha"]],
                cwd=repo, text=True).strip()
        moment = datetime.fromisoformat(stamp.replace("Z", "+00:00"))
        if moment.tzinfo is None:
            raise SystemExit(f"Issue #{number}: acceptance time lacks timezone")
        oldest = moment if oldest is None or moment < oldest else oldest
age_hours = (datetime.now(timezone.utc) - oldest.astimezone(timezone.utc)).total_seconds() / 3600 if oldest else 0
platforms = [p for p in ("macos", "android", "ios") if p in additional or any(
    rows[n].get(p) not in delivered for n in unallocated)]
qualified = bool(platforms) and (commit_count > 3 or age_hours > 24)
preallocated = bool(state["pending_new_batch"].get("version")) and not locked
print(json.dumps({
    "issues": accepted,
    "closed_pending_issues": [n for n in accepted if github_states.get(n) == "closed"],
    "distinct_code_accepted_pending_count": len(accepted),
    "allocated_pending_issues": [n for n in accepted if n in allocated],
    "unallocated_issues": unallocated,
    "distinct_unallocated_pending_count": len(unallocated),
    "commit_threshold_exclusive": 3,
    "max_wait_hours_exclusive": 24,
    "pending_commit_count": commit_count,
    "oldest_pending_at": oldest.isoformat() if oldest else None,
    "pending_age_hours": round(age_hours, 3),
    "platforms_to_release": platforms,
    "additional_platforms": additional,
    "qualified": qualified,
    "batch_version": state["pending_new_batch"].get("version"),
    "version_locked": locked,
    "allocate_new_version": qualified and not preallocated,
    "resume_allocated_version": qualified and preallocated,
    "main_sha": subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=repo, text=True).strip(),
}, ensure_ascii=False))
