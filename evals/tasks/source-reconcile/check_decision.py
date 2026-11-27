"""Standalone semantic grader for the supplied record-reconciliation task.

Expected values come exclusively from the outside, trusted fixture. Both a
component-keyed map and an array with unique component names satisfy the task.
This module needs only the standard library and performs no model/network call.
"""
import argparse
import hashlib
import json
import math
from pathlib import Path
import posixpath
import re

GRADER_VERSION = "nuwa-source-reconcile-semantic-v1"


class TrustedFixtureError(ValueError):
    """The controller's expected records or provenance cannot be trusted."""


def _pairs(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON object key: " + key)
        value[key] = item
    return value


def _constant(value):
    raise ValueError("nonfinite JSON constant: " + value)


def _load(path):
    return json.loads(Path(path).read_text(encoding="utf-8"), object_pairs_hook=_pairs, parse_constant=_constant)


def _finite(value):
    return type(value) is int or (type(value) is float and math.isfinite(value))


def _digest(value):
    encoded = json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def _hashes(root):
    hashes = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise TrustedFixtureError("trusted fixture contains a symbolic link")
        if path.is_file():
            hashes[path.relative_to(root).as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    return hashes


def _expected(fixture, workspace):
    try:
        fixture = Path(fixture).resolve(strict=True)
        workspace = Path(workspace).resolve()
        if not fixture.is_dir() or fixture.is_relative_to(workspace) or workspace.is_relative_to(fixture):
            raise TrustedFixtureError("trusted fixture and model workspace must be separate directories")
        records = fixture / "records"
        if records.is_symlink() or not records.is_dir():
            raise TrustedFixtureError("trusted fixture has no regular records directory")
        full_hashes = _hashes(fixture)
        latest, sources = {}, {}
        for path in sorted(records.rglob("*.json")):
            record = _load(path)
            if (not isinstance(record, dict) or not isinstance(record.get("component"), str)
                    or not record["component"] or not _finite(record.get("revision"))
                    or not isinstance(record.get("status"), str) or not record["status"]
                    or not _finite(record.get("passed_tests")) or record["passed_tests"] < 0):
                raise TrustedFixtureError("trusted record fields are invalid: " + path.name)
            relative = path.relative_to(fixture).as_posix()
            sources[relative] = full_hashes[relative]
            name = record["component"]
            selected = latest.get(name)
            if selected is None or record["revision"] > selected["revision"]:
                latest[name] = dict(revision=record["revision"], status=record["status"],
                                    passed_tests=record["passed_tests"], evidence=[relative])
            elif record["revision"] == selected["revision"]:
                if record["status"] != selected["status"] or record["passed_tests"] != selected["passed_tests"]:
                    raise TrustedFixtureError("trusted highest revision conflicts for component: " + name)
                selected["evidence"].append(relative)
        if not latest:
            raise TrustedFixtureError("trusted fixture contains no component records")
        total = sum(record["passed_tests"] for record in latest.values())
        if not _finite(total):
            raise TrustedFixtureError("trusted total is not finite")
        return dict(components=latest, total_passed_tests=total,
                    ready=all(record["status"] == "accepted" for record in latest.values())), sources, full_hashes
    except TrustedFixtureError:
        raise
    except (OSError, ValueError, TypeError) as error:
        raise TrustedFixtureError("trusted fixture cannot be read: " + str(error)) from error


def _regular_source(workspace, relative):
    root = Path(workspace).resolve()
    path = root / relative
    current = root
    for part in Path(relative).parts:
        current /= part
        if current.is_symlink():
            return None
    if not path.resolve().is_relative_to(root) or not path.is_file():
        return None
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _components(value):
    if isinstance(value, dict):
        if any(not isinstance(entry, dict) for entry in value.values()):
            raise ValueError("component map entries must be objects")
        return value
    if isinstance(value, list):
        out = {}
        for entry in value:
            if not isinstance(entry, dict) or not isinstance(entry.get("component"), str) or not entry["component"]:
                raise ValueError("component array entries need a component name")
            name = entry["component"]
            if name in out:
                raise ValueError("duplicate component array entry: " + name)
            out[name] = entry
        return out
    raise ValueError("components must be an object map or a unique component array")


def _evidence(value):
    if not isinstance(value, str) or not value or posixpath.isabs(value):
        return None
    normalized = posixpath.normpath(value)
    if normalized in (".", "..") or normalized.startswith("../"):
        return None
    return normalized


def evaluate(fixture, workspace, baseline=None, fixture_sha256=None):
    expected, sources, fixture_hashes = _expected(fixture, workspace)
    computed_fixture_sha = _digest(fixture_hashes)
    if baseline is not None and (not isinstance(baseline, dict) or baseline != fixture_hashes):
        raise TrustedFixtureError("initial baseline differs from the trusted fixture's complete file hashes")
    if fixture_sha256 is not None:
        if not isinstance(fixture_sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", fixture_sha256):
            raise TrustedFixtureError("fixture_sha256 must be the recorded lowercase SHA-256")
        if fixture_sha256 != computed_fixture_sha:
            raise TrustedFixtureError("recorded fixture_sha256 differs from the trusted initial fixture")
    report = dict(grader_version=GRADER_VERSION, passed=False, checks=[], expected=expected,
                  source_hashes=sources, fixture_sha256=computed_fixture_sha,
                  baseline_verified=baseline is not None, fixture_sha256_verified=fixture_sha256 is not None)

    def check(name, ok, detail=""):
        report["checks"].append(dict(name=name, ok=bool(ok), detail=detail if not ok else ""))

    for relative, trusted_hash in sources.items():
        try:
            actual_hash = _regular_source(workspace, relative)
            check("source unchanged: " + relative, actual_hash == trusted_hash,
                  "source missing, changed or not a regular in-workspace file")
        except OSError as error:
            check("source unchanged: " + relative, False, "source unreadable: " + type(error).__name__)
    decision_path = Path(workspace) / "decision.json"
    try:
        if decision_path.is_symlink() or not decision_path.is_file():
            raise ValueError("decision.json must be a regular file")
        decision = _load(decision_path)
        if not isinstance(decision, dict):
            raise ValueError("decision.json must contain an object")
        check("decision JSON", True)
    except (OSError, ValueError, TypeError) as error:
        check("decision JSON", False, str(error))
        return report
    try:
        components = _components(decision.get("components"))
        check("component representation", True)
    except ValueError as error:
        check("component representation", False, str(error))
        return report
    check("all and only observed components", set(components) == set(expected["components"]),
          "component names differ from the trusted observed set")
    for name, selected in expected["components"].items():
        entry = components.get(name, {})
        check(name + " current revision", _finite(entry.get("revision")) and entry["revision"] == selected["revision"],
              "revision must equal the highest trusted observed revision")
        check(name + " current status", entry.get("status") == selected["status"],
              "status must match the selected trusted record")
        check(name + " selected evidence", _evidence(entry.get("evidence")) in selected["evidence"],
              "evidence must name a relative selected-record filename")
    total = decision.get("total_passed_tests")
    check("selected revisions total", _finite(total) and total == expected["total_passed_tests"],
          "total must sum only selected trusted revisions")
    check("release readiness", type(decision.get("ready")) is bool and decision["ready"] is expected["ready"],
          "ready must be a boolean and equal all current statuses being accepted")
    report["passed"] = all(item["ok"] for item in report["checks"])
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--fixture", type=Path, required=True, help="trusted fixture directory outside model workspace")
    parser.add_argument("--workspace", type=Path, default=Path.cwd())
    parser.add_argument("--baseline", type=Path, help="controller's initial-files.json, never the agent's expected values")
    parser.add_argument("--fixture-sha256", help="recorded initial fixture digest from the original run")
    args = parser.parse_args(argv)
    try:
        try:
            baseline = _load(args.baseline) if args.baseline else None
        except (OSError, ValueError, TypeError) as error:
            raise TrustedFixtureError("initial baseline cannot be read: " + str(error)) from error
        report = evaluate(args.fixture, args.workspace, baseline, args.fixture_sha256)
        code = 0 if report["passed"] else 1
    except TrustedFixtureError as error:
        report = dict(grader_version=GRADER_VERSION, passed=False, error_category="trusted_fixture_error", error=str(error))
        code = 2
    print(json.dumps(report, sort_keys=True, ensure_ascii=False, allow_nan=False))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
