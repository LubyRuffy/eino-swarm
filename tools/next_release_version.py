#!/usr/bin/env python3
"""Read remote and allocated release versions; suggest the next numeric patch."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from release import version_code

ROOT = Path(__file__).resolve().parents[1]
NUMERIC_VERSION = re.compile(r"[vV]?(\d+)\.(\d+)\.(\d+)\Z")


def numeric_version(raw):
    match = NUMERIC_VERSION.fullmatch(raw) if isinstance(raw, str) else None
    if not match:
        return None
    parts = tuple(map(int, match.groups()))
    version_code(".".join(map(str, parts)))
    return parts


def allocated_versions(state):
    """Retain assigned versions even when their batch has not been published."""
    if not isinstance(state, dict):
        raise ValueError("Delivery state must be an object")
    versions = []

    def add(raw):
        if numeric_version(raw) is None:
            raise ValueError("Allocated batch has an invalid numeric version")
        versions.append(raw)

    for key in ("pending_new_batch", "next_unallocated_batch"):
        batch = state.get(key, {})
        if not isinstance(batch, dict):
            raise ValueError(f"{key} must be an object")
        if batch.get("version") is not None:
            add(batch["version"])
    completed = state.get("completed_batches", {})
    if not isinstance(completed, dict):
        raise ValueError("completed_batches must be an object")
    for version, entry in completed.items():
        add(version)
        if not isinstance(entry, dict) or not isinstance(entry.get("batch", {}), dict):
            raise ValueError("Completed batch must be an object")
        if entry.get("batch", {}).get("version") is not None:
            add(entry["batch"]["version"])
    return versions


def choose_next_version(tags, releases, allocated):
    occupied = {parsed for raw in [*tags, *releases, *allocated]
                if (parsed := numeric_version(raw)) is not None}
    if not occupied:
        raise ValueError("No confirmed numeric release version is available")
    major, minor, patch = max(occupied)
    patch += 1
    # The existing release/versionCode contract reserves two decimal places
    # each for minor and patch; carry instead of allocating an invalid value.
    if patch == 100:
        minor, patch = minor + 1, 0
    if minor == 100:
        major, minor = major + 1, 0
    version = f"{major}.{minor}.{patch}"
    version_code(version)
    return {
        "version": version,
        "highest_reserved": ".".join(map(str, max(occupied))),
        "reserved_versions": [".".join(map(str, value)) for value in sorted(occupied)],
    }


def inspect_versions(root, state_path, command):
    """Only read the real origin, GitHub Releases (including drafts) and ledger."""
    tags = []
    for line in command(["git", "ls-remote", "--tags", "origin"], root).splitlines():
        fields = line.split()
        if len(fields) != 2 or not fields[1].startswith("refs/tags/"):
            raise ValueError("Remote tag response is malformed")
        tags.append(fields[1].removeprefix("refs/tags/").removesuffix("^{}"))
    repo = command(["gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"], root).strip()
    if not re.fullmatch(r"[\w.-]+/[\w.-]+", repo):
        raise ValueError("GitHub repository response is malformed")
    pages = json.loads(command(["gh", "api", "--paginate", "--slurp", f"repos/{repo}/releases"], root))
    if not isinstance(pages, list) or any(not isinstance(page, list) for page in pages):
        raise ValueError("GitHub release response is malformed")
    releases = []
    for page in pages:
        for release in page:
            if not isinstance(release, dict) or not isinstance(release.get("tag_name"), str):
                raise ValueError("GitHub release entry has no tag name")
            releases.append(release["tag_name"])
    state = json.loads(state_path.read_text())
    return choose_next_version(tags, releases, allocated_versions(state))


def command(args, root):
    return subprocess.check_output(args, cwd=root, text=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", type=Path, default=ROOT)
    parser.add_argument("--state", type=Path, default=Path(os.environ.get(
        "DELIVERY_STATE", Path(os.environ.get("ZWAI_HOME", Path.home() / ".zwai-swarm")) /
        "issue-automation/delivery-state.json")))
    parser.add_argument("--plain", action="store_true", help="print only the suggested version")
    args = parser.parse_args(argv)
    result = inspect_versions(args.source_dir.resolve(), args.state, command)
    print(result["version"] if args.plain else json.dumps(result))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"Version selection failed: {error}", file=sys.stderr)
        sys.exit(1)
