"""Continuous native observations; a final elapsed summary is not evidence."""
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import time

START = "soak-start.json"
TICKS = "soak-ticks.jsonl"
SUMMARY = "soak-summary.json"


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _policy(phase, budget):
    try:
        duration, interval = phase["duration_seconds"], phase["interval_seconds"]
        timeout = budget["scenario_seconds"] - duration
        if (phase["name"] != "soak" or not all(_number(value) for value in (duration, interval, timeout))
                or duration < 64800 or not 0 < interval <= timeout):
            raise ValueError("soak sampling requires eighteen hours and a finite interval within the action timeout")
        return dict(duration_seconds=duration, interval_seconds=interval, action_timeout=timeout, max_gap=interval + timeout)
    except (KeyError, TypeError) as error:
        raise ValueError("soak phase or frozen budget is malformed") from error


def _owners(value):
    try:
        if set(value) != {"running", "parked"}:
            raise ValueError()
        result = {}
        for kind in ("running", "parked"):
            keys = ("thread_id", "turn_id", "worker_id") + (("wake_id",) if kind == "parked" else ())
            row = {key: value[kind][key] for key in keys}
            if any(not isinstance(item, str) or not 0 < len(item) <= 128 for item in row.values()):
                raise ValueError()
            result[kind] = row
        return result
    except (KeyError, TypeError, ValueError) as error:
        raise RuntimeError("soak original worker identity is missing or changed") from error


def _digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()).hexdigest()


def _clock():
    return dict(monotonic=time.monotonic(), wall=time.time())


def _gap(previous, current, maximum, *, active=False):
    if not all(_number(row.get(key)) and row[key] >= 0 for row in (previous, current) for key in ("monotonic", "wall")):
        raise RuntimeError("soak clock evidence is malformed")
    mono, wall = (current[key] - previous[key] for key in ("monotonic", "wall"))
    # Per-segment comparison tolerates ordinary clock slewing, but rejects a
    # suspended VM whose clocks resume differently, even if only one advanced.
    if min(mono, wall) < 0 or max(mono, wall) > maximum or abs(mono - wall) > 1 or (active and min(mono, wall) <= 0):
        raise RuntimeError("soak clock moved backwards, diverged or exceeded the sampling gap")


def _write(path, row, *, exclusive=False):
    flags = os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK | (os.O_EXCL if exclusive else os.O_APPEND)
    descriptor = os.open(path, flags, 0o600)
    with os.fdopen(descriptor, "a", encoding="utf-8") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise RuntimeError("soak evidence is not a regular file")
        stream.write(json.dumps(row, sort_keys=True, allow_nan=False) + "\n")
        stream.flush()
        os.fsync(stream.fileno())


def _read(path, *, lines=False):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, encoding="utf-8") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise RuntimeError("soak evidence is not a regular file")
        rows = []
        while True:
            line = stream.readline(131073)
            if not line:
                break
            if len(line) > 131072 or not line.endswith("\n"):
                raise RuntimeError("soak evidence is truncated or oversized")
            rows.append(json.loads(line))
            if not lines and len(rows) > 1:
                raise RuntimeError("soak start evidence has multiple records")
    return rows if lines else rows[0]


def _engine(context, harness, expected):
    pid = harness.read_json(Path(context["data"]) / "engine.json")["pid"]
    if type(pid) is not int or pid <= 1 or pid != expected or context["engine_pid"] != expected:
        raise RuntimeError("soak original engine identity changed")
    return pid


def start(context, harness, phase, budget):
    policy = _policy(phase, budget)
    directory = Path(context["evidence"])
    if any(os.path.lexists(directory / name) for name in (START, TICKS, SUMMARY)):
        raise FileExistsError("a soak attempt already owns its first evidence")
    case_id = context["case_id"]
    if not re.fullmatch(r"[0-9a-f]{32}", case_id):
        raise RuntimeError("soak case identity is malformed")
    owners = _owners(context["background"])
    if _owners(harness.background.check(context, harness)) != owners:
        raise RuntimeError("soak original worker identity changed before start")
    engine = _engine(context, harness, context["engine_pid"])
    expected = harness.draft_snapshot(context)
    if expected.get("state") != "editing" or not isinstance(expected.get("text"), str):
        raise RuntimeError("soak requires the original editable draft")
    now = _clock()
    header = dict(schema=1, case_id=case_id, policy=policy, engine_pid=engine, background=owners,
                  draft_sha256=_digest(expected), started_monotonic=now["monotonic"], started_at=now["wall"])
    _write(directory / START, header, exclusive=True)
    descriptor = os.open(directory / TICKS, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    os.close(descriptor)
    return dict(header=header, expected=copy.deepcopy(expected), directory=directory, phase=copy.deepcopy(phase),
                budget=copy.deepcopy(budget), started_monotonic=now["monotonic"], last=now, tick_count=0,
                failed=False, finished=False, pending=None)


def begin_tick(state):
    if state["failed"] or state["finished"]:
        raise RuntimeError("soak attempt already failed or finished")
    if state["pending"] is not None:
        raise RuntimeError("a soak tick is already pending")
    current = _clock()
    try:
        _gap(state["last"], current, state["header"]["policy"]["max_gap"])
    except RuntimeError:
        state["failed"] = True
        raise
    if not state["tick_count"]:
        state["started_monotonic"] = current["monotonic"]
    state["pending"] = dict(current, seq=state["tick_count"] + 1)
    return state["pending"]


def _ui(context, row):
    if (row.get("filled") is not True or row.get("case_id") != context["case_id"]
            or row.get("pid") != context["app_pid"]):
        raise RuntimeError("native soak edit was not acknowledged by this case")
    return {key: row[key] for key in ("case_id", "pid", "filled")}


def _remaining(started, timeout):
    now = _clock()
    _gap(started, now, float("inf"))
    remaining = min(started[key] + timeout - now[key] for key in ("monotonic", "wall"))
    if remaining <= 0:
        raise TimeoutError("soak persisted draft/action budget expired")
    return remaining


def _verify_saved(context, harness, expected, started, timeout):
    # AX changes before the composer's autosave debounce. Wait for the entire
    # durable snapshot before the strict verifier, which treats a mismatch as
    # data loss rather than a retryable transport result.
    while True:
        _remaining(started, timeout)
        saved = harness.draft_snapshot(context)
        remaining = _remaining(started, timeout)
        if saved == expected:
            break
        time.sleep(min(.2, remaining))
    verified = harness.verify_draft(context, expected, _remaining(started, timeout))
    _remaining(started, timeout)
    return verified


def record_tick(context, harness, state, started):
    if state["pending"] is not started or state["failed"] or state["finished"]:
        raise RuntimeError("soak tick token is not its pending owner")
    header, policy = state["header"], state["header"]["policy"]
    receipt = dict(schema=1, case_id=header["case_id"], seq=started["seq"], passed=False,
                   started_monotonic=started["monotonic"], started_at=started["wall"])
    try:
        if context["case_id"] != header["case_id"]:
            raise RuntimeError("soak case identity changed")
        receipt["engine_pid"] = _engine(context, harness, header["engine_pid"])
        receipt["background"] = _owners(harness.background.check(context, harness))
        if receipt["background"] != header["background"]:
            raise RuntimeError("soak original worker identity changed")
        harness.background.resources(context, harness)
        expected = state["expected"]
        if _digest(_verify_saved(context, harness, expected, started, policy["action_timeout"])) != header["draft_sha256"]:
            raise RuntimeError("soak original draft readback changed")
        edited = dict(expected, text=expected["text"] + "\n[soak tick " + str(started["seq"]) + "]")
        primary = None
        try:
            filled = _ui(context, harness.native(context, "fill", edited["text"]))
            changed = _verify_saved(context, harness, edited, started, policy["action_timeout"])
            if changed != edited:
                raise RuntimeError("native soak edit has no actual draft/UI readback")
            receipt["edited_sha256"] = _digest(changed)
        except BaseException as error:
            primary = error
            raise
        finally:
            try:
                restored = _ui(context, harness.native(context, "fill", expected["text"]))
            except BaseException as error:
                if primary is None:
                    raise
                receipt["restore_error"] = type(error).__name__ + ": " + str(error)[:512]
        actual = _verify_saved(context, harness, expected, started, policy["action_timeout"])
        receipt.update(draft_sha256=_digest(actual), ui=dict(fill=filled, restore=restored))
        if actual != expected:
            raise RuntimeError("native soak restore has no original draft readback")
        completed = _clock()
        _gap(started, completed, policy["action_timeout"], active=True)
        receipt.update(finished_monotonic=completed["monotonic"], finished_at=completed["wall"], passed=True)
        _write(state["directory"] / TICKS, receipt)
        state.update(last=completed, tick_count=started["seq"], pending=None)
        return receipt
    except BaseException as error:
        state["failed"] = True
        completed = _clock()
        receipt.update(passed=False, error=type(error).__name__ + ": " + str(error)[:512],
                       finished_monotonic=completed["monotonic"], finished_at=completed["wall"])
        _write(state["directory"] / TICKS, receipt)
        raise


def _validate(directory, phase, budget):
    directory = Path(directory)
    header = _read(directory / START)
    policy = _policy(phase, budget)
    if (header.get("schema") != 1 or header.get("policy") != policy
            or not re.fullmatch(r"[0-9a-f]{32}", header.get("case_id", ""))
            or type(header.get("engine_pid")) is not int or header["engine_pid"] <= 1
            or not re.fullmatch(r"[0-9a-f]{64}", header.get("draft_sha256", ""))):
        raise RuntimeError("soak start identity or frozen policy changed")
    owners = _owners(header["background"])
    previous = dict(monotonic=header["started_monotonic"], wall=header["started_at"])
    first, last, count, edits = None, None, 0, set()
    for row in _read(directory / TICKS, lines=True):
        if (row.get("schema") != 1 or row.get("passed") is not True or row.get("seq") != count + 1
                or row.get("case_id") != header["case_id"] or row.get("engine_pid") != header["engine_pid"]
                or _owners(row["background"]) != owners or row.get("draft_sha256") != header["draft_sha256"]
                or not re.fullmatch(r"[0-9a-f]{64}", row.get("edited_sha256", ""))
                or row["edited_sha256"] == row["draft_sha256"] or row["edited_sha256"] in edits):
            raise RuntimeError("soak tick identity, sequence, draft or action failed")
        edits.add(row["edited_sha256"])
        for action in ("fill", "restore"):
            ui = row["ui"][action]
            if (ui.get("filled") is not True or ui.get("case_id") != header["case_id"]
                    or type(ui.get("pid")) is not int or ui["pid"] <= 1):
                raise RuntimeError("soak tick lacks an actual native action result")
        started = dict(monotonic=row["started_monotonic"], wall=row["started_at"])
        last = dict(monotonic=row["finished_monotonic"], wall=row["finished_at"])
        _gap(previous, started, policy["max_gap"])
        _gap(started, last, policy["action_timeout"], active=True)
        first = first or started
        previous, count = last, count + 1
    if count < 2:
        raise RuntimeError("soak duration requires multiple real ticks")
    elapsed = min(last[key] - first[key] for key in ("monotonic", "wall"))
    if elapsed < policy["duration_seconds"]:
        raise RuntimeError("soak real tick duration did not cover eighteen hours")
    summary = dict(tick_count=count, elapsed_seconds=elapsed, first_monotonic=first["monotonic"],
                   last_monotonic=last["monotonic"], first_at=first["wall"], last_at=last["wall"],
                   ledger_sha256=hashlib.sha256((directory / TICKS).read_bytes()).hexdigest())
    return header, summary


def finish(context, harness, state, result):
    if state["failed"] or state["finished"] or state["pending"] is not None:
        raise RuntimeError("soak attempt failed, finished or has a pending tick")
    header, summary = _validate(state["directory"], state["phase"], state["budget"])
    if context["case_id"] != header["case_id"]:
        raise RuntimeError("soak final case identity changed")
    _engine(context, harness, header["engine_pid"])
    _write(state["directory"] / SUMMARY, summary, exclusive=True)
    result.update(soak_elapsed_seconds=summary["elapsed_seconds"], soak_evidence=summary)
    state["finished"] = True
    return summary


def accept_evidence(directory, result, phase, budget):
    try:
        header, summary = _validate(directory, phase, budget)
        if (result.get("phase") != "soak" or result.get("case_id") != header["case_id"]
                or result.get("engine_pid_before") != header["engine_pid"] or result.get("engine_pid_after") != header["engine_pid"]
                or _owners(result["background"]) != header["background"]
                or result.get("soak_elapsed_seconds") != summary["elapsed_seconds"]
                or result.get("soak_evidence") != summary or _read(Path(directory) / SUMMARY) != summary):
            raise RuntimeError("soak final summary does not match its durable tick evidence")
        return summary
    except (OSError, ValueError, KeyError, TypeError, IndexError) as error:
        raise RuntimeError("soak durable evidence is missing or malformed") from error
