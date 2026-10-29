#!/usr/bin/env python3
"""Model-check the engine's ordering protocols with TLC.

    python3 tools/model_check.py [--fetch] [--jar PATH] [--out DIR] [CASE ...]

Each model under models/ states one protocol: models/lifecycle/EngineLifecycle.tla
(C-027 handoff, Stop, parked workers, C-001 idle exit and shell presence) and
models/stream/EventStream.tla (C-003 stored order, slow readers, catch-up,
rewind and restart). Every case sets its model's switch constants and states
the expected outcome: the current design must pass every safety invariant;
each historical or seeded behaviour must break the named invariant (checked
alone with TypeOK, so the reported invariant does not depend on which
counterexample TLC's workers reach first); each reachability witness must be
violated so that a passing invariant cannot be vacuous. A case whose outcome
differs fails the run.

TLC is tla2tools.jar, found through --jar, $TLA2TOOLS_JAR or the user cache.
--fetch downloads the pinned release into that cache and checks its SHA-256;
nothing is downloaded without it. Each case runs in its own directory under
--out (a fresh temporary directory by default), which keeps its config, TLC
output and any counterexample trace.
"""

from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

TLA_VERSION = "v1.7.4"
TLA_URL = f"https://github.com/tlaplus/tlaplus/releases/download/{TLA_VERSION}/tla2tools.jar"
TLA_SHA256 = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
CACHE_JAR = Path.home() / ".cache" / "tlaplus" / TLA_VERSION / "tla2tools.jar"


@dataclass(frozen=True)
class Model:
    name: str
    spec: Path
    switches: tuple[str, ...]
    safety: tuple[str, ...]
    # Bounds that keep the state space finite.
    constants: tuple[tuple[str, int], ...]


MODELS = {
    m.name: m
    for m in (
        Model(
            "lifecycle", ROOT / "models" / "lifecycle" / "EngineLifecycle.tla",
            ("DeadlineKillsChild", "RestoreBeforeListen", "StopRacesRestore", "PresenceAfterWindow",
             "IdleCommitNonAtomic", "FinishUngated", "ParkedNotCheckpointed", "IdleCountsOnlyManagers",
             "ParkedAdmissionNonAtomic"),
            ("TypeOK", "WorkHasLiveOwner", "CheckpointHasOwner", "StopIsFinal", "WindowHasEngine",
             "RegistryAlwaysAccounted"),
            (("MaxTurns", 2),),
        ),
        Model(
            "stream", ROOT / "models" / "stream" / "EventStream.tla",
            ("MarkerUnstored", "MarkerAfterUnlock", "BroadcastAfterUnlock", "DropWhenSlow"),
            ("TypeOK", "ConvergedView", "SeqIdentity"),
            (("MaxSeq", 5), ("Cap", 1)),
        ),
    )
}


@dataclass(frozen=True)
class Case:
    name: str
    source: str
    switches: frozenset[str] = field(default_factory=frozenset)
    # None: every safety invariant holds. Otherwise the invariant TLC must report.
    violates: str | None = None
    # A witness checks only its own invariant, which the design must violate.
    witness: str | None = None
    model: str = "lifecycle"

    def invariants(self) -> tuple[str, ...]:
        if self.witness:
            return (self.witness,)
        if self.violates:
            return ("TypeOK", self.violates)
        return MODELS[self.model].safety

    def expected(self) -> str | None:
        return self.witness or self.violates


def _case(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
          witness: str | None = None, model: str = "lifecycle") -> Case:
    return Case(name, source, frozenset(switches), violates, witness, model)


def _stream(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
            witness: str | None = None) -> Case:
    return _case(name, source, switches, violates, witness, "stream")


CASES: tuple[Case, ...] = (
    _case("current", "current design: every switch off"),
    # Historical: the shipped code before these commits had the behaviour.
    _case("pre-d77e8d4", "historical, before d77e8d4: the restart deadline bounded the replacement "
          "child, which restored every task before publishing its lease",
          ("DeadlineKillsChild", "RestoreBeforeListen"), "CheckpointHasOwner"),
    _case("deadline-kills-child", "historical part of d77e8d4: only the deadline kill",
          ("DeadlineKillsChild",), "CheckpointHasOwner"),
    _case("pre-f341e05", "historical, before f341e05: the window opened before holding presence "
          "and idle exit acted on an earlier sample",
          ("PresenceAfterWindow", "IdleCommitNonAtomic"), "WindowHasEngine"),
    _case("hold-without-atomic-idle", "historical part of f341e05: holding presence first is not "
          "enough while idle exit acts on an earlier sample",
          ("IdleCommitNonAtomic",), "WindowHasEngine"),
    _case("window-before-presence", "historical part of f341e05: an atomic idle commit is not "
          "enough while the window opens before holding presence",
          ("PresenceAfterWindow",), "WindowHasEngine"),
    # Design hazards: guards the current code has, each removed in turn.
    _case("stop-races-restore", "guard added with d77e8d4: restoreMu serializes Stop with one "
          "conversation's restoration once the replacement serves before restoring",
          ("StopRacesRestore",), "StopIsFinal"),
    _case("finish-ungated", "guard from 5d7b305: handoff waits for a normal finalization "
          "(lockFinish)", ("FinishUngated",), "WorkHasLiveOwner"),
    _case("parked-not-checkpointed", "guard from 5d7b305: the checkpoint covers parked registries",
          ("ParkedNotCheckpointed",), "WorkHasLiveOwner"),
    _case("idle-counts-only-managers", "guard required by f341e05: idle load includes parked "
          "workers, restoration and handoff (Engine.ActiveWork)",
          ("IdleCountsOnlyManagers",), "WorkHasLiveOwner"),
    _case("parked-successor-detached", "seeded split choose/take then occupy: retained workers "
          "temporarily disappear from the runtime before successor admission",
          ("ParkedAdmissionNonAtomic",), "RegistryAlwaysAccounted"),
    # Witnesses: the scenarios behind the invariants are reachable.
    _case("reach-restored-window", "witness", witness="NeverRestoredIntoWindow"),
    _case("reach-parked-checkpoint", "witness", witness="NeverParkedCheckpoint"),
    _case("reach-parked-successor", "witness", witness="NeverParkedSuccessor"),
    _case("reach-refused-parked-successor", "witness", witness="NeverRefusedParkedSuccessor"),
    _case("reach-stop-before-restore", "witness", witness="NeverStopBeforeRestore"),
    _case("reach-opened-after-deadline", "witness", witness="NeverOpenedAfterDeadline"),
    _case("reach-idle-exit-after-close", "witness", witness="NeverIdleExitAfterClose"),
    _case("reach-relaunch-after-idle-commit", "witness", witness="NeverRelaunchAfterIdleCommit"),
    _case("reach-finish-waits-for-handoff", "witness", witness="NeverFinishWaitsForHandoff"),
    # C-003 event stream.
    _stream("stream-current", "current design: every switch off"),
    _stream("stream-v0.1.45-lost-marker", "historical, shipped through v0.1.45: the rewind marker was "
            "broadcast after recordMu with seq 0 and never stored, so a slow or reconnecting reader "
            "kept the truncated rows", ("MarkerUnstored", "MarkerAfterUnlock"), "ConvergedView"),
    _stream("stream-v0.1.45-seq-reuse", "historical, shipped through v0.1.45: with nothing stored at "
            "the cut, a restart reseeded the counter below seqs already issued",
            ("MarkerUnstored", "MarkerAfterUnlock"), "SeqIdentity"),
    _stream("stream-marker-unstored", "historical part of the v0.1.45 design: an unstored marker alone loses "
            "the cut for a slow reader", ("MarkerUnstored",), "ConvergedView"),
    _stream("stream-marker-unstored-reuse", "historical part of the v0.1.45 design: an unstored marker alone "
            "lets a restart reuse seqs", ("MarkerUnstored",), "SeqIdentity"),
    _stream("stream-marker-after-unlock", "historical part of the v0.1.45 design: a marker broadcast after "
            "recordMu races a concurrent row, which the reader then drops or erases",
            ("MarkerAfterUnlock",), "ConvergedView"),
    _stream("stream-broadcast-after-unlock", "guard since cb0edb5: Engine.record broadcasts under "
            "recordMu, so stored and streamed order agree", ("BroadcastAfterUnlock",), "ConvergedView"),
    _stream("stream-drop-when-slow", "guard since cb0edb5: a stored row that does not fit a slow "
            "subscriber flags it lagged", ("DropWhenSlow",), "ConvergedView"),
    _stream("stream-reach-catch-up-across-rewind", "witness", witness="NeverCatchUpAcrossRewind"),
    _stream("stream-reach-record-after-restart", "witness", witness="NeverRecordAfterRestart"),
)


def render_config(case: Case) -> str:
    model = MODELS[case.model]
    lines = ["SPECIFICATION Spec", "CONSTANTS"]
    for name in model.switches:
        lines.append(f"  {name} = {'TRUE' if name in case.switches else 'FALSE'}")
    for name, value in model.constants:
        lines.append(f"  {name} = {value}")
    lines.append("INVARIANTS " + " ".join(case.invariants()))
    return "\n".join(lines) + "\n"


@dataclass(frozen=True)
class Outcome:
    passed: bool  # TLC finished without finding an error
    violated: str | None  # invariant TLC reported, if any
    distinct: int | None  # distinct states found
    error: str | None  # anything TLC reported that is neither


_VIOLATED = re.compile(r"^Error: Invariant (\w+) is violated\.", re.M)
_DISTINCT = re.compile(r"(\d+) distinct states found")


def parse_output(text: str) -> Outcome:
    violated = _VIOLATED.search(text)
    distinct = _DISTINCT.search(text)
    passed = "Model checking completed. No error has been found." in text
    error = None
    if not passed and not violated:
        error = next((ln for ln in text.splitlines() if ln.startswith("Error:")), "TLC did not finish")
    return Outcome(passed, violated.group(1) if violated else None,
                   int(distinct.group(1)) if distinct else None, error)


def judge(case: Case, outcome: Outcome) -> str | None:
    """Returns why the outcome is not the expected one, or None."""
    if outcome.error:
        return outcome.error
    want = case.expected()
    if want is None:
        return None if outcome.passed else f"expected no violation, TLC reported {outcome.violated}"
    if outcome.violated == want:
        return None
    if outcome.passed:
        return f"expected {want} to be violated, TLC found no error"
    return f"expected {want} to be violated, TLC reported {outcome.violated}"


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def find_jar(explicit: str | None, fetch: bool) -> Path:
    for candidate in (explicit, os.environ.get("TLA2TOOLS_JAR")):
        if candidate:
            path = Path(candidate).expanduser()
            if not path.is_file():
                raise SystemExit(f"tla2tools.jar not found at {path}")
            return path
    if not CACHE_JAR.is_file():
        if not fetch:
            raise SystemExit(f"tla2tools.jar not found; pass --fetch to download {TLA_VERSION} "
                             f"into {CACHE_JAR}, or set TLA2TOOLS_JAR")
        CACHE_JAR.parent.mkdir(parents=True, exist_ok=True)
        partial = CACHE_JAR.with_suffix(".part")
        with urllib.request.urlopen(TLA_URL, timeout=120) as resp, partial.open("wb") as out:
            shutil.copyfileobj(resp, out)
        if sha256(partial) != TLA_SHA256:
            partial.unlink()
            raise SystemExit(f"downloaded tla2tools.jar does not match the pinned SHA-256 {TLA_SHA256}")
        partial.replace(CACHE_JAR)
    elif sha256(CACHE_JAR) != TLA_SHA256:
        raise SystemExit(f"{CACHE_JAR} does not match the pinned SHA-256 {TLA_SHA256}")
    return CACHE_JAR


def run_case(case: Case, jar: Path, out: Path, timeout: int = 600) -> tuple[Outcome, Path]:
    # TLC resolves the config beside the module, so each case gets a copy.
    spec = MODELS[case.model].spec
    workdir = out / case.name
    workdir.mkdir(parents=True)
    shutil.copy(spec, workdir / spec.name)
    (workdir / "case.cfg").write_text(render_config(case))
    proc = subprocess.run(
        ["java", "-XX:+UseParallelGC", "-cp", str(jar), "tlc2.TLC", "-deadlock", "-workers", "auto",
         "-metadir", str(workdir / "states"), "-config", "case.cfg", spec.name],
        cwd=workdir, capture_output=True, text=True, timeout=timeout,
    )
    log = workdir / "tlc.out"
    log.write_text(proc.stdout + proc.stderr)
    shutil.rmtree(workdir / "states", ignore_errors=True)
    return parse_output(proc.stdout + proc.stderr), log


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("cases", nargs="*", help="case names (default: all)")
    ap.add_argument("--jar", help="path to tla2tools.jar")
    ap.add_argument("--fetch", action="store_true", help=f"download TLC {TLA_VERSION} if missing")
    ap.add_argument("--out", help="directory for per-case configs and TLC output")
    ap.add_argument("--list", action="store_true", help="list cases and exit")
    args = ap.parse_args(argv)

    by_name = {c.name: c for c in CASES}
    if args.list:
        for c in CASES:
            print(f"{c.name:38} {c.model:9} expect {c.expected() or 'pass':26} {c.source}")
        return 0
    unknown = [n for n in args.cases if n not in by_name]
    if unknown:
        ap.error(f"unknown case: {', '.join(unknown)}")
    selected = [by_name[n] for n in args.cases] or list(CASES)

    jar = find_jar(args.jar, args.fetch)
    out = Path(args.out) if args.out else Path(tempfile.mkdtemp(prefix="zwai-model-check-"))
    out.mkdir(parents=True, exist_ok=True)

    failures = 0
    for case in selected:
        outcome, log = run_case(case, jar, out)
        problem = judge(case, outcome)
        result = "pass" if outcome.passed else f"violates {outcome.violated}"
        states = f"{outcome.distinct} states" if outcome.distinct is not None else "-"
        print(f"{'ok  ' if problem is None else 'FAIL'} {case.name:38} {result:30} {states}")
        if problem:
            failures += 1
            print(f"     {problem}; see {log}")
    print(f"{len(selected) - failures}/{len(selected)} cases as expected; output in {out}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
