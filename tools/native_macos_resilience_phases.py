"""Public-OS fault stage orchestration for one private native case."""
import importlib
import json
from pathlib import Path
import re
import subprocess
import time


def journal_rows(context):
    return [json.loads(line) for path in (context["data"] / "diagnostics").glob("desktop-*.jsonl")
            for line in path.read_text().splitlines() if line.strip()]


def ready_session(context, harness):
    rows = [row for row in journal_rows(context) if row.get("phase") == "ready"
            and row.get("shell_pid") == context["app_pid"] and row.get("engine_pid") == context["engine_pid"]
            and re.fullmatch(r"[0-9a-f]{16}", row.get("session_id", ""))]
    if not rows:
        raise RuntimeError("current private shell has no public pixel-ready session")
    return max(rows, key=lambda row:harness.timestamp(row["at"]))


def ready_matches(row, harness, *, pid, engine, session, generation, since):
    return (row.get("phase") == "ready" and row.get("shell_pid") == pid and row.get("engine_pid") == engine
            and type(row.get("generation")) is int and row["generation"] == generation
            and type(row.get("attempt")) is int and 1 <= row["attempt"] <= 2
            and row.get("session_id") == session and bool(re.fullmatch(r"[0-9a-f]{16}", session))
            and harness.timestamp(row["at"]) > since)


def validate_replacement(verified, initial, old, engine, harness):
    try:
        ready, recovered, replacing = (verified[key] for key in ("ready", "recovered", "replacing"))
        exited, session = verified["original_exit"], verified["session_id"]
        start, pixel, commit = (harness.timestamp(row["at"]) for row in (replacing, ready, recovered))
        if (initial.get("phase") != "ready" or initial.get("shell_pid") != old["pid"]
                or initial.get("session_id") != session or initial.get("engine_pid") != engine
                or type(initial.get("generation")) is not int
                or not ready_matches(ready, harness, pid=ready["shell_pid"], engine=engine, session=session,
                                     generation=initial["generation"] + 1, since=verified["since"])
                or type(ready["shell_pid"]) is not int or ready["shell_pid"] <= 1 or ready["shell_pid"] == old["pid"]
                or any(row not in verified["journal"] for row in (initial, ready, recovered, replacing))
                or any(row.get("phase") != phase or row.get("shell_pid") != old["pid"]
                       or row.get("session_id") != session or row.get("engine_pid") != engine
                       or row.get("generation") != initial["generation"] or row.get("attempt") != ready["attempt"]
                       for row,phase in ((recovered,"recovered"),(replacing,"replacing")))
                or exited.get("identity") != old or exited.get("outcome") != "exited"
                or not verified["since"] < start <= pixel <= commit <= exited["observed_at"]
                or not 0 < exited["observed_at"] - start <= 30):
            raise RuntimeError("replacement ready, commit or original exit evidence differs")
        return True
    except (KeyError, TypeError, ValueError) as error:
        raise RuntimeError("replacement evidence is missing or malformed") from error


def replacement_ready(context, harness, *, original_pid, since):
    metadata = harness.api(context["origin"], "/api/meta")
    lease = harness.read_json(context["data"] / "engine.json")
    if (lease["pid"] != context["engine_pid"] or lease["url"] != context["origin"]
            or metadata.get("data_dir") != str(context["data"])
            or metadata.get("mock") is not context.get("expected_mock", True)):
        raise RuntimeError("fault recovery changed the original engine identity")
    for client in reversed(metadata.get("clients", [])):
        if client.get("surface") != "desktop" or client["pid"] == original_pid:
            continue
        pid = client["pid"]
        argv, marker = harness.runner.process_identity(pid)
        if (marker != context["case_id"] or not argv
                or not Path(argv[0]).is_relative_to(context["app"].parent)
                or ".app/Contents/MacOS/" not in argv[0] or "engine" in argv[1:]):
            raise RuntimeError("replacement desktop presence is not this private case shell")
        context["app_pid"] = pid
        if context.get("activated_pid") != pid:
            activated = harness.native(context, "activate")
            if not activated.get("active") or activated.get("frontmost_pid") != pid:
                return None
            context["activated_pid"] = pid
        rows = [row for row in journal_rows(context) if ready_matches(row, harness, pid=pid,
                engine=context["engine_pid"], session=context.get("recovery_session_id", ""),
                generation=context.get("recovery_generation", 0) + 1, since=since)]
        if rows:
            return max(rows, key=lambda row:harness.timestamp(row["at"]))
    return None


def exhaustion_events(rows, harness, *, since, shell_pid, session_id, path, engine_pid, generation):
    current = [row for row in rows if row.get("shell_pid") == shell_pid
               and row.get("session_id") == session_id and harness.timestamp(row["at"]) > since]
    attempts = [row.get("attempt") for row in current if row.get("phase") == "replacing"]
    failures = [row for row in current if row.get("phase") == "replacement_failed"]
    exhausted = [row for row in current if row.get("phase") == "exhausted"]
    events = [row for row in current if row.get("phase") in ("replacing","replacement_failed","exhausted","ready","recovered")]
    order = [("replacing",1),("replacement_failed",1),("replacing",2),("replacement_failed",2),("exhausted",2)]
    if ([(row.get("phase"),row.get("attempt")) for row in events] != order
            or any(row.get("engine_pid") != engine_pid or row.get("generation") != generation for row in events)
            or any(harness.timestamp(later["at"]) < harness.timestamp(first["at"]) for first,later in zip(events,events[1:]))):
        raise RuntimeError("native exhaustion did not make exactly two ordered bounded attempts on its original engine and generation")
    expected = "start recovery shell: fork/exec " + path + ": permission denied"
    if any(row.get("reason") != expected for row in failures):
        raise RuntimeError("native exhaustion lacks both actual private-copy permission failures")
    return dict(failed_attempts=attempts, failures=failures, exhausted=exhausted[0])


def run(context, harness, phase, timeout, result):
    fault = importlib.import_module("native_macos_resilience_fault")
    harness.background.reopen(context, harness, timeout)
    boundaries = {name:harness.read_json(context["evidence"] / ("public-process-ownership-" + name + ".json"))
                  for name in fault.BOUNDARIES}
    ready = ready_session(context, harness)
    context["recovery_session_id"] = ready["session_id"]
    context["recovery_generation"] = ready["generation"]
    original = harness.process_identity(Path(context["helper"]), context["app_pid"])
    harness.atomic_json(context["evidence"] / "fault-proof.json", dict(boundaries=boundaries, ready=ready))
    observations, latest = 0, None
    def snapshot():
        nonlocal observations, latest
        label = "fault-live-" + str(observations)
        observations += 1
        harness.background.resources(context, harness, label)
        latest = harness.read_json(context["evidence"] / ("public-process-ownership-" + label + ".json"))
        return latest
    seconds = min(90, timeout)
    if seconds <= 5:
        raise ValueError("fault stage requires time for bounded resume cleanup")
    since = time.time()
    verified = dict(case_id=context["case_id"], kind=phase, since=since, session_id=ready["session_id"], passed=False)
    kind = "renderer" if phase == "exhaustion" else phase
    def stalled():
        return fault.stall(boundaries, kind, case_id=context["case_id"], private_app=context["app"],
                           evidence=context["evidence"], snapshot=snapshot, seconds=seconds)
    if phase == "exhaustion":
        with fault.deny_successor_launch(case_id=context["case_id"], private_app=context["app"],
                 evidence=context["evidence"], session_id=ready["session_id"]) as copy:
            with stalled() as injection:
                def exhausted():
                    rows = journal_rows(context)
                    if not any(row.get("phase") == "exhausted" and row.get("session_id") == ready["session_id"] for row in rows):
                        return None
                    summary = exhaustion_events(rows, harness, since=since, shell_pid=original["pid"],
                                                session_id=ready["session_id"], path=copy["path"],
                                                engine_pid=context["engine_pid"], generation=ready["generation"])
                    dialog = harness.native(context, "error-dialog")
                    return dict(summary=summary, dialog=dialog)
                error = harness.wait_for(exhausted, harness.Deadline(seconds - 5))
                path = context["evidence"] / "native-exhausted.png"
                subprocess.run(["/usr/sbin/screencapture", "-x", "-l", str(error["dialog"]["window_id"]), str(path)],
                               check=True, capture_output=True, timeout=8)
                image = harness.native(context, "image", str(path))
                if image.get("buckets", 0) < 16 or image.get("dominant_fraction", 1) > .985:
                    raise RuntimeError("native exhaustion screenshot is blank")
                result["snapshots"].append(path.name)
                verified.update(error, private_copy=copy)
        # Releasing the OS fault allows a late healthy callback to arrive. The
        # same exhausted lineage must still refuse a third automatic launch.
        harness.native(context, "dismiss-error")
        observed = []
        while True:
            if harness.case_shells(context) != {original["pid"]}:
                raise RuntimeError("exhausted recovery automatically launched another shell")
            harness.background.check(context, harness)
            exhaustion_events(journal_rows(context), harness, since=since, shell_pid=original["pid"],
                              session_id=ready["session_id"], path=copy["path"],
                                                engine_pid=context["engine_pid"], generation=ready["generation"])
            observed.append(dict(monotonic=time.monotonic(), shell_pid=original["pid"], engine_pid=context["engine_pid"]))
            harness.atomic_json(context["evidence"] / "fault-no-respawn.json", dict(observations=observed))
            if observed[-1]["monotonic"] - observed[0]["monotonic"] >= 15:
                break
            time.sleep(.5)
        verified["no_respawn_seconds"] = observed[-1]["monotonic"] - observed[0]["monotonic"]
        harness.background.reopen(context, harness, timeout)
    else:
        with stalled() as injection:
            def committed():
                current = replacement_ready(context, harness, original_pid=original["pid"], since=since)
                if current is None:
                    return None
                recovered = [row for row in journal_rows(context) if row.get("phase") == "recovered"
                    and row.get("session_id") == ready["session_id"] and row.get("shell_pid") == original["pid"]
                    and row.get("engine_pid") == context["engine_pid"] and harness.timestamp(row["at"]) > since]
                old = fault.process_identity(original["pid"])
                if old is not None and old != original:
                    raise RuntimeError("original shell PID was reused before recovery verification")
                if not recovered or old is not None:
                    return None
                launches = [row for row in journal_rows(context) if row.get("phase") == "replacing"
                    and row.get("session_id") == ready["session_id"] and row.get("shell_pid") == original["pid"]
                    and row.get("attempt") == current["attempt"] and harness.timestamp(row["at"]) > since]
                if not launches or time.time() - harness.timestamp(launches[-1]["at"]) > 30:
                    raise RuntimeError("replacement did not commit and retire its original shell within thirty seconds")
                return dict(ready=current, recovered=recovered[-1], replacing=launches[-1],
                    original_exit=dict(identity=original, outcome="exited", observed_at=time.time()))
            verified.update(harness.wait_for(committed, harness.Deadline(seconds - 5)))
    state = injection["state"]
    if (state.get("expired") or not state.get("stopped") or not state.get("stopped_stat", "").startswith("T")
            or state.get("resume", {}).get("outcome") not in ("resumed", "exited")):
        raise RuntimeError("OS fault was not observed and safely resumed before its finite deadline")
    verified.update(target=injection["target"], state=state, current=latest, passed=True)
    verified["journal"] = journal_rows(context)
    if phase != "exhaustion":
        validate_replacement(verified, ready, original, context["engine_pid"], harness)
    harness.atomic_json(context["evidence"] / "fault-verification.json", verified)
    result["fault_verified"] = True


def accept_evidence(directory, result, cleanup, name, harness):
    fault = importlib.import_module("native_macos_resilience_fault")
    verified = harness.read_json(directory / "fault-verification.json")
    proof = harness.read_json(directory / "fault-proof.json")
    if (not result.get("fault_verified") or verified.get("passed") is not True
            or verified.get("case_id") != result["case_id"] or verified.get("kind") != name):
        raise RuntimeError("fault stage lacks its complete actual verification")
    target = verified["target"]
    selected = fault.select_target(proof["boundaries"], verified["current"],
        "renderer" if name == "exhaustion" else name, case_id=result["case_id"],
        private_app=cleanup["app_copy"], now=target["recorded_at"])
    claims = [json.loads(line) for line in (directory / fault.TARGETS).read_text().splitlines() if line.strip()]
    retained = journal_rows(dict(data=directory))
    initial = proof["ready"]
    if (not verified.get("journal") or any(row not in retained for row in verified["journal"])
            or initial not in retained or initial.get("phase") != "ready"
            or initial.get("shell_pid") != target["shell"]["pid"]
            or initial.get("engine_pid") != result["engine_pid_before"]
            or initial.get("session_id") != verified["session_id"]
            or not re.fullmatch(r"[0-9a-f]{16}", verified["session_id"])
            or type(initial.get("generation")) is not int or initial["generation"] != 0
            or initial.get("attempt") != 0 or harness.timestamp(initial["at"]) > verified["since"]):
        raise RuntimeError("fault retained diagnostic journal or initial ready identity differs")
    state = verified["state"]
    if (selected != target or claims != [target] or state.get("expired") is not False
            or state.get("stopped") is not True or not state.get("stopped_stat", "").startswith("T")
            or state.get("resume", {}).get("outcome") not in ("resumed", "exited")):
        raise RuntimeError("fault identity, real stop, or finite resume evidence differs")
    receipts = cleanup.get("renderer_fault_cleanup", [])
    if len(receipts) != 2:
        raise RuntimeError("fault stage lacks both runner cleanup hooks")
    for index, receipt in enumerate(receipts):
        if (receipt.get("case_id") != result["case_id"] or receipt.get("private_app") != target["private_app"]
                or receipt.get("complete") is not True or receipt.get("require_exit") is not bool(index)
                or len(receipt.get("targets", [])) != 1
                or receipt["targets"][0].get("identity") != target["target"]
                or receipt["targets"][0].get("outcome") not in (("resumed", "exited") if index == 0 else ("exited",))):
            raise RuntimeError("fault cleanup did not confirm its exact target resumed and exited")
    if name == "exhaustion":
        copy = verified["private_copy"]
        claims = [json.loads(line) for line in (directory / fault.COPIES).read_text().splitlines() if line.strip()]
        fault._validated_copy(copy, result["case_id"], cleanup["app_copy"])
        if claims != [copy] or copy["session_marker"] != verified["session_id"]:
            raise RuntimeError("exhaustion modified another private recovery copy")
        exhaustion_events(verified["journal"], harness, since=verified["since"], shell_pid=target["shell"]["pid"],
                          session_id=verified["session_id"], path=copy["path"],
                          engine_pid=result["engine_pid_before"], generation=proof["ready"]["generation"])
        dialog = verified["dialog"]
        rows = harness.read_json(directory / "fault-no-respawn.json")["observations"]
        if (not dialog.get("error_dialog") or dialog.get("case_id") != result["case_id"]
                or dialog.get("pid") != target["shell"]["pid"] or "已停止自动重启" not in dialog.get("text", "")
                or len(rows) < 3 or rows[-1]["monotonic"] - rows[0]["monotonic"] < 15
                or any(row["shell_pid"] != target["shell"]["pid"] or row["engine_pid"] != result["engine_pid_before"] for row in rows)
                or any(not 0 < later["monotonic"] - first["monotonic"] <= 5 for first, later in zip(rows, rows[1:]))):
            raise RuntimeError("native exhaustion or the bounded no-respawn observation is incomplete")
        for receipt in receipts:
            if (len(receipt.get("copies", [])) != 1 or receipt["copies"][0].get("path") != copy["path"]
                    or receipt["copies"][0].get("outcome") not in ("restored", "removed")):
                raise RuntimeError("private recovery copy permissions were not restored or removed")
    else:
        validate_replacement(verified, proof["ready"], target["shell"], result["engine_pid_before"], harness)
    return True
