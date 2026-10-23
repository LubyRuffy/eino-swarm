"""Real running and parked workers for a private native resilience case.

Only test setup uses the API. Composer text, attachment and recovery assertions
continue through the real native window. No fixture is selected by prompt text.
"""
import importlib
import json
import math
import os
from pathlib import Path
import plistlib
import re
import signal
import subprocess
import time


def start_awake(context, harness):
    seconds = math.ceil(context["awake_until"] - time.monotonic())
    if seconds <= 0 or "awake_process" in context:
        raise RuntimeError("case display assertion deadline or ownership is invalid")
    command = ["/usr/bin/caffeinate", "-d", "-i", "-u", "-t", str(seconds), "-w", str(os.getpid())]
    with (context["evidence"] / "display-assertion.log").open("ab") as log:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
    context["awake_process"] = process
    context["awake_identity"] = harness.process_identity(Path(context["helper"]), process.pid)
    if process.poll() is not None:
        raise RuntimeError("case display assertion exited before readiness")
    harness.atomic_json(context["evidence"] / "display-assertion.json", dict(case_id=context["case_id"],
                        identity=context["awake_identity"], command=command, timeout_seconds=seconds))


def stop_awake(context, harness):
    process = context.get("awake_process")
    if process is None:
        return
    if process.poll() is None:
        expected = context["awake_identity"]
        if process.pid != expected["pid"] or expected.get("executable") != "/usr/bin/caffeinate":
            raise RuntimeError("display assertion child identity differs")
        try:
            current = harness.process_identity(Path(context["helper"]), process.pid)
        except (OSError, subprocess.SubprocessError):
            if process.poll() is None:
                raise
        else:
            if current != expected:
                raise RuntimeError("display assertion public process identity changed")
            # System utilities may omit their environment from KERN_PROCARGS2.
            # The unreaped Popen child and fresh birth/path establish this owner.
            process.terminate()
    process.wait(timeout=5)
    exited = process.poll() is not None
    harness.atomic_json(context["evidence"] / "display-assertion-cleanup.json",
                        dict(identity=context["awake_identity"], exited=exited))
    if not exited:
        raise RuntimeError("case-owned display assertion did not exit")
    context.pop("awake_process")


def prepare(context, harness, manifest, timeout):
    module = importlib.import_module("native_macos_resilience_worker")
    evidence = context["evidence"] / "offline-fixture"
    evidence.mkdir()
    fixture = module.WorkerFixture(context["workspace"], evidence)
    try:
        settings = harness.api(context["origin"], "/api/settings")["settings"]
        providers = [dict(id="resilience-" + kind, label="Offline resilience " + kind,
                          base_url=fixture.url, model=model, api_key="", context_window=128000)
                     for kind, model in fixture.models.items()]
        harness.api(context["origin"], "/api/settings", "PUT", dict(
            models=dict(default="resilience-checkpoint", providers=providers),
            swarm=dict(settings["swarm"], auto_title=False, agent_timeout_seconds=86400,
                       manager_max_iterations=20000, compact_provider="resilience-checkpoint",
                       compact_model=fixture.models["checkpoint"]), memory=dict(settings["memory"], auto_review=False)))
        # The generic runner initially launches --mock. Configure a keyless
        # adapter, then reopen the identical private package before taking the
        # accepted engine/turn/worker identity snapshot.
        for key in ("app_pid", "engine_pid"):
            pid = context[key]
            argv, marker = harness.runner.process_identity(pid)
            if marker != context["case_id"] or not argv or not Path(argv[0]).is_relative_to(context["app"].parent):
                raise RuntimeError("offline setup lost its private launch ownership")
            expected = harness.process_identity(Path(context["helper"]), pid)
            harness.signal_identified(Path(context["helper"]), expected, signal.SIGTERM)
            harness.wait_for(lambda: not harness.runner.process_identity(pid)[0], harness.Deadline(timeout))
        with (evidence / "configured.stdout.log").open("wb") as out, (evidence / "configured.stderr.log").open("wb") as err:
            subprocess.Popen([str(context["app"] / "Contents/MacOS/zwai"), "desktop", "--data-dir", str(context["data"])],
                             stdout=out, stderr=err, stdin=subprocess.DEVNULL, start_new_session=True)
        configured = harness.context_for(dict(manifest, expected_mock=False), timeout)
        context.update(configured, expected_mock=False, offline_model=fixture.models["checkpoint"],
                       offline_models=list(fixture.models.values()), fixture=fixture)
        return fixture
    except BaseException:
        fixture.stop()
        raise


def start(context, harness, timeout):
    fixture = context["fixture"]
    own = context["thread_id"]
    harness.api(context["origin"], "/api/threads/" + own, "PATCH",
                dict(provider_id="resilience-running", model=fixture.models["running"],
                     title="Native recovery " + context["case_id"]))
    context["thread_title"] = "Native recovery " + context["case_id"]
    parked = harness.api(context["origin"], "/api/threads", "POST", dict(provider_id="resilience-parked"))["thread"]["id"]
    wake = harness.api(context["origin"], "/api/schedules", "POST", dict(kind="thread", thread_id=parked,
                       title="Native recovery parked worker " + context["case_id"],
                       prompt="Inspect the assigned operation state.", every_s=86400))["schedule"]["id"]
    context["background"] = dict(running=dict(thread_id=own), parked=dict(thread_id=parked, wake_id=wake))
    for kind, row in context["background"].items():
        accepted = harness.api(context["origin"], "/api/threads/" + row["thread_id"] + "/steer", "POST",
                               dict(text="Inspect the assigned operation and verify its persisted result."))
        row["turn_id"] = accepted["turn"]["id"]
    def ready():
        status = fixture.status()
        for kind, row in context["background"].items():
            state = status.get(kind, {})
            if not state.get("stream_active") or not state.get("worker_id"):
                return None
            row["worker_id"] = state["worker_id"]
        try:
            return check(context, harness, initial=True)
        except RuntimeError:
            return None
    before = harness.wait_for(ready, harness.Deadline(timeout))
    context["background_before"] = before
    harness.atomic_json(context["evidence"] / "background-before.json", before)
    return before


def validate_worker(kind, expected, current, trace, state):
    """Persisted lifecycle and a live stream jointly prove the same worker."""
    worker = expected["worker_id"]
    spawned = [row for row in trace["events"] if row.get("kind") == "spawned"]
    if (len(spawned) != 1 or spawned[0].get("agent_id") != worker
            or any(row.get("kind") in ("finished", "cleanup") and row.get("agent_id") == worker for row in trace["events"])
            or state.get("worker_id") != worker or state.get("worker_requests") != 2
            or state.get("spawn_calls") != 1
            or state.get("stream_active") is not True or state.get("cancelled_before_release") is True
            or state.get("released") is True or state.get("errors") or current.get("workers") != 1):
        raise RuntimeError(kind + " original worker is no longer live with its original request")
    if kind == "running" and (current.get("running") is not True or current.get("turn_id") != expected["turn_id"]):
        raise RuntimeError("the original running turn changed identity or stopped")
    if kind == "parked" and (current.get("running") is True or current.get("waiting") is not True):
        raise RuntimeError("the original parked worker lost its future wake ownership")


def validate_schedule(expected, current):
    # This future wake must not fire or mutate during the case. Freeze the real
    # complete API row, including routing, instruction and expiration fields.
    row = dict(current)
    if (row["id"] != expected["wake_id"] or row["kind"] != "thread" or row["thread_id"] != expected["thread_id"]
            or row["status"] != "active" or row["every_s"] != 86400 or not row["next_run_at"]
            or row.get("title_auto") is not False
            or ("schedule" in expected and row != expected["schedule"])):
        raise RuntimeError("background parked schedule identity, ownership or cadence changed")
    return row


def check(context, harness, initial=False):
    if "background" not in context:
        return None
    fixture = context["fixture"].status()
    rows = {}
    for kind, expected in context["background"].items():
        detail = harness.api(context["origin"], "/api/threads/" + expected["thread_id"])
        trace = harness.api(context["origin"], "/api/trace/" + expected["turn_id"])
        current = dict(detail["status"], waiting=detail.get("thread", {}).get("waiting", detail["status"].get("waiting")))
        state = fixture[kind]
        validate_worker(kind, expected, current, trace, state)
        marker = Path(state["marker"])
        if not marker.is_relative_to(context["workspace"]) or not marker.is_file():
            raise RuntimeError("the original worker has no real private tool side effect")
        row = dict(expected, spawn_seq=next(event["seq"] for event in trace["events"] if event["kind"] == "spawned"),
                   marker_sha256=harness.sha256(marker), marker_size=marker.stat().st_size,
                   marker_name=marker.relative_to(context["workspace"]).as_posix(),
                   worker_requests=state["worker_requests"])
        if kind == "parked":
            schedule = harness.api(context["origin"], "/api/schedules/" + expected["wake_id"])["schedule"]
            row["schedule"] = validate_schedule(dict(expected, **({} if initial else
                dict(schedule=context["background_before"][kind]["schedule"]))), schedule)
        else:
            schedule = None
        if not initial and row != context["background_before"][kind]:
            raise RuntimeError("background worker, spawn receipt or side effect changed during renderer recovery")
        rows[kind] = row
        harness.atomic_json(context["evidence"] / ("background-" + kind + ("-before.json" if initial else "-live.json")),
                            dict(detail=detail, trace=trace, fixture=state, schedule=schedule))
    return rows


def validate_completion(kind, expected, original, collected):
    if (original.get("turn", {}).get("id") != expected["turn_id"]
            or original["turn"].get("thread_id") != expected["thread_id"]
            or not any(event.get("kind") == "finished" and event.get("agent_id") == expected["worker_id"]
                       and not event.get("err") for event in original.get("events", []))
            or collected.get("turn", {}).get("thread_id") != expected["thread_id"]
            or collected["turn"].get("status") != "done"):
        raise RuntimeError("background completion lacks the original worker finish receipt")
    calls = {event["tool_call_id"] for event in collected.get("events", [])
             if event.get("kind") == "tool_call" and event.get("text", "").startswith("wait_agents(")
             and event.get("tool_call_id")}
    receipts = [json.loads(event.get("text", "{}")) for event in collected.get("events", [])
                if event.get("kind") == "tool_result" and event.get("tool_call_id") in calls]
    if not receipts or not any(agent.get("agent_id") == expected["worker_id"] and agent.get("status") == "done"
               and not agent.get("err") and not agent.get("error")
               for agent in receipts[-1].get("agents", [])):
        raise RuntimeError("background completion lacks a real same-worker wait_agents receipt")
    if kind == "parked" and any(event.get("kind") in ("spawned", "cleanup") for event in collected["events"]):
        raise RuntimeError("background parked collection replaced or stopped its worker")


def accept_evidence(directory, result):
    try:
        before = json.loads((directory / "background-before.json").read_text())
        after = json.loads((directory / "background-after.json").read_text())
        if (result.get("background_verified") is not True or set(before) != {"running", "parked"}
                or after["identities"] != before or result.get("background") != before):
            raise RuntimeError("background identity snapshots differ")
        for kind, expected in before.items():
            if (not all(expected.get(key) for key in ("thread_id", "turn_id", "worker_id", "spawn_seq", "marker_sha256"))
                    or expected.get("worker_requests") != 2):
                raise RuntimeError("background original operation evidence is incomplete")
            if kind == "parked":
                validate_schedule(expected, expected["schedule"])
            for stage in ("before", "live"):
                live = json.loads((directory / ("background-" + kind + "-" + stage + ".json")).read_text())
                detail, trace = live["detail"], live["trace"]
                current = dict(detail["status"], waiting=detail.get("thread", {}).get("waiting", detail["status"].get("waiting")))
                validate_worker(kind, expected, current, trace, live["fixture"])
                if trace["turn"].get("id") != expected["turn_id"] or trace["turn"].get("thread_id") != expected["thread_id"]:
                    raise RuntimeError("background live trace belongs to another operation")
                if kind == "parked":
                    validate_schedule(expected, live["schedule"])
            state = after["fixture"][kind]
            if (state.get("worker_id") != expected["worker_id"] or state.get("worker_requests") != 2
                    or state.get("spawn_calls") != 1 or state.get("released") is not True
                    or state.get("stream_active") is not False or state.get("cancelled_before_release") or state.get("errors")):
                raise RuntimeError("background original stream continuity is missing")
            original = json.loads((directory / ("background-" + kind + "-original.json")).read_text())
            collected = json.loads((directory / ("background-" + kind + "-finished.json")).read_text())
            validate_completion(kind, expected, original, collected)
    except (OSError, KeyError, ValueError, TypeError) as error:
        raise RuntimeError("background acceptance evidence is missing or malformed") from error


def finish(context, harness, timeout, result):
    before = check(context, harness)
    fixture = context["fixture"]
    for kind in ("running", "parked"):
        fixture.release(kind)
    for kind, expected in context["background"].items():
        def naturally_finished():
            trace = harness.api(context["origin"], "/api/trace/" + expected["turn_id"])
            return next((row for row in trace["events"] if row.get("kind") == "finished"
                         and row.get("agent_id") == expected["worker_id"] and not row.get("err")), None)
        harness.wait_for(naturally_finished, harness.Deadline(timeout))
        original = harness.api(context["origin"], "/api/trace/" + expected["turn_id"])
        harness.atomic_json(context["evidence"] / ("background-" + kind + "-original.json"), original)
        if kind == "parked":
            accepted = harness.api(context["origin"], "/api/schedules/" + expected["wake_id"] + "/run", "POST")
            turn = accepted["turn"]["id"]
        else:
            turn = expected["turn_id"]
        def completed():
            trace = harness.api(context["origin"], "/api/trace/" + turn)
            if trace["turn"]["status"] == "error":
                raise RuntimeError("original background operation failed after release")
            return trace if trace["turn"]["status"] == "done" else None
        trace = harness.wait_for(completed, harness.Deadline(timeout))
        validate_completion(kind, expected, original, trace)
        harness.atomic_json(context["evidence"] / ("background-" + kind + "-finished.json"), trace)
        result["turn_ids"].append(turn)
    state = fixture.status()
    if any(state[kind].get("worker_requests") != 2 or state[kind].get("cancelled_before_release") for kind in ("running", "parked")):
        raise RuntimeError("background worker request was restarted or cancelled before release")
    result["background_verified"] = True
    result["background"] = before
    context["offline_model"] = fixture.models["running"]
    harness.atomic_json(context["evidence"] / "background-after.json", dict(identities=before, fixture=state))


def select_thread(context, harness, timeout):
    if context["thread_title"] not in harness.native(context).get("text", ""):
        harness.native(context, "press", "Conversations")
    harness.wait_for(lambda: harness.native(context, "select-thread", context["thread_title"]).get("pressed"),
                     harness.Deadline(timeout))


def reopen(context, harness, timeout):
    previous = context["app_pid"]
    executable = context["app"] / "Contents/MacOS/zwai"
    resources(context, harness, "before-quit")
    if harness.native(context, "quit").get("quit_requested") is not True:
        raise RuntimeError("normal native Quit was not acknowledged")
    harness.wait_for(lambda: not harness.runner.process_identity(previous)[0], harness.Deadline(timeout))
    # A completed AppKit shutdown must stay closed while the engine's actual
    # running and parked workers retain their original identities.
    closed = time.monotonic()
    while time.monotonic() - closed < 2:
        if harness.case_shells(context):
            raise RuntimeError("normal native Quit unexpectedly revived a shell")
        check(context, harness)
        time.sleep(.2)
    resources(context, harness, "after-quit")
    resources(context, harness, "before-reopen")
    with (context["evidence"] / "reopen.stdout.log").open("wb") as out, (context["evidence"] / "reopen.stderr.log").open("wb") as err:
        subprocess.Popen([str(executable), "desktop", "--data-dir", str(context["data"])],
                         stdin=subprocess.DEVNULL, stdout=out, stderr=err, start_new_session=True)
    reopened = harness.context_for(dict(helper=context["helper"], package=dict(version=context["package_version"]),
                                       expected_mock=False), timeout)
    if reopened["engine_pid"] != context["engine_pid"] or reopened["origin"] != context["origin"]:
        raise RuntimeError("normal UI reopen changed the original engine")
    context.update(reopened)
    resources(context, harness, "after-reopen")
    select_thread(context, harness, timeout)


def resources(context, harness, boundary=None):
    output = subprocess.check_output(["/bin/ps", "-axo", "pid=,ppid=,rss=,comm="], text=True, timeout=5)
    rows = []
    for line in output.splitlines():
        parts = line.split(None, 3)
        if len(parts) != 4:
            continue
        pid, parent, rss = map(int, parts[:3])
        name = Path(parts[3]).name
        own = pid in (context["app_pid"], context["engine_pid"])
        if own or name.startswith(("com.apple.WebKit.WebContent", "com.apple.WebKit.GPU")):
            rows.append(dict(pid=pid, parent_pid=parent, rss_kib=rss, executable_name=name,
                             ownership="case" if own else "observed_unattributed"))
    path = context["evidence"] / "process-resources.jsonl"
    with path.open("a") as stream:
        stream.write(json.dumps(dict(at=time.time(), case_id=context["case_id"], rows=rows)) + "\n")
    suffix = boundary or str(context["app_pid"])
    destination = context["evidence"] / ("public-process-ownership-" + suffix + ".json")
    if not destination.exists():
        process_evidence(context, harness, rows, destination)


def procinfo_fields(output):
    return [line.strip() for line in output.splitlines() if re.match(
        r"^\s*(?:program path|pid|ppid|unique pid|responsible (?:pid|unique pid|path)|(?:resource |jetsam )?coalition(?: id| members)?)\s*=", line)]


def process_evidence(context, harness, rows, destination):
    try:
        shell = harness.process_identity(Path(context["helper"]), context["app_pid"])
        context["observed_shell_identity"] = shell
    except (OSError, ValueError, subprocess.SubprocessError):
        shell = None
    observations = []
    for row in rows:
        if row["pid"] == context["engine_pid"]:
            continue
        try:
            identity = harness.process_identity(Path(context["helper"]), row["pid"])
            info = subprocess.run(["/usr/bin/sudo", "-n", "/bin/launchctl", "procinfo", str(row["pid"])],
                                  capture_output=True, text=True, timeout=10)
            signature = subprocess.run(["/usr/bin/codesign", "-dv", "--verbose=2", identity["executable"]],
                                       capture_output=True, text=True, timeout=5)
            fields = procinfo_fields(info.stdout)
            versions = {}
            for parent in Path(identity["executable"]).parents:
                if parent.name.endswith(".framework"):
                    plist = parent / "Versions/A/Resources/Info.plist"
                    if plist.is_file():
                        metadata = plistlib.loads(plist.read_bytes())
                        versions = {key: metadata[key] for key in ("CFBundleIdentifier", "CFBundleVersion", "CFBundleShortVersionString") if key in metadata}
                    break
            observations.append(dict(identity=identity, procinfo_exit=info.returncode, procinfo_fields=fields,
                framework_version=versions,
                coalition_observed=any("coalition" in line for line in fields), signature_exit=signature.returncode,
                signature=[line for line in signature.stderr.splitlines() if line.startswith(("Identifier=", "TeamIdentifier=", "CDHash="))]))
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            observations.append(dict(pid=row["pid"], observation_error=str(error)))
    harness.atomic_json(destination, dict(case_id=context["case_id"], at=time.time(), shell=shell,
        prior_observed_shell=context.get("observed_shell_identity"), process_rows=rows,
        guest_os=subprocess.check_output(["/usr/bin/sw_vers"], text=True, timeout=5),
        observations=observations, purpose="read-only diagnostics; no candidate selected or fault authorized"))


def retain(context, harness=None):
    diagnostics = context["data"] / "diagnostics"
    destination = context["evidence"] / "diagnostics"
    try:
        if harness is not None and context.get("thread_id"):
            try:
                harness.atomic_json(context["evidence"] / "final-native-draft-observation.json",
                    dict(api=harness.draft_snapshot(context), native=harness.native(context)))
            except Exception as error:
                harness.atomic_json(context["evidence"] / "final-observation-error.json", dict(error=str(error)))
        if diagnostics.is_dir():
            import shutil
            shutil.copytree(diagnostics, destination, dirs_exist_ok=True)
    finally:
        if "fixture" in context:
            context["fixture"].stop()
