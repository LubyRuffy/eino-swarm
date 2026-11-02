#!/usr/bin/env python3
"""Native wait cancellation, Continue refusal and explicit reauthorization.

Run inside native_macos_case.py in ci-macOS with prepared helpers. REST creates
one cross-origin wait so the selected target has exactly one real banner X;
creation itself is setup, not a claimed native user action. This case also
closes its private window/engine and reopens the same private data to observe
persisted denial. The outer case runner owns final process/data cleanup.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import urllib.error

import native_goal_resume_notice as notice
import native_macos_smoke as smoke
from native_macos_schedule_cancel_fixture import ALLOW_TEXT, ARMED_TEXT, CONTINUE_TEXT, REFUSED_TEXT, Fixture


save = smoke.readiness.save_json
api = notice.api


def labels(row):
    return [node.get("AXValue") or node.get("AXTitle") or node.get("AXDescription") or ""
            for node in row.get("tree", []) if node.get("AXRole") in ("AXStaticText", "AXHeading")]


def buttons(row, title):
    return [node for node in row.get("tree", []) if node.get("AXRole") == "AXButton"
            and title in (node.get("AXTitle"), node.get("AXDescription"))]


def assert_wait_ui(row, waiting):
    texts = labels(row)
    if waiting:
        assert "Waiting" in texts and buttons(row, "Cancel wait"), "native target lacks Waiting and its actual wait control"
    else:
        assert "Idle" in texts and "Waiting" not in texts, "native target did not return to Idle"
        assert not buttons(row, "Cancel wait") and not buttons(row, "Run now"), "native target still has an armed wait action"


def assert_cancelled_rows(rows, thread_id, old_id):
    own = [row for row in rows if row.get("thread_id") == thread_id]
    assert len(own) == 1 and own[0].get("id") == old_id and own[0].get("status") == "cancelled", "cancelled wait was changed or a replacement wait appeared"
    return own[0]


def human_cancellation(events, thread_id, schedule_id):
    matches = [row for row in events if row.get("kind") == "schedule_cancelled" and row.get("role") == "human"
               and row.get("thread_id") == thread_id and row.get("text") == schedule_id
               and not row.get("source_thread_id") and not row.get("source_turn_id")]
    assert len(matches) == 1 and type(matches[0].get("seq")) is int and matches[0]["seq"] > 0, "one exact local human cancellation receipt is required"
    return matches[0]


def tool_receipt(trace):
    calls = [row for row in trace.get("events", []) if row.get("kind") == "tool_call" and row.get("text", "").startswith("schedule_wake(")]
    assert len(calls) == 1 and calls[0].get("tool_call_id"), "native human turn must attempt one actual schedule_wake"
    results = [row for row in trace["events"] if row.get("kind") == "tool_result" and row.get("tool_call_id") == calls[0]["tool_call_id"]]
    assert len(results) == 1 and not results[0].get("err"), "actual schedule_wake must have one paired tool result"
    return json.loads(results[0]["text"])


def assert_trace_identity(trace, thread_id, text):
    turn, calls = trace.get("turn", {}), trace.get("llm_calls", [])
    assert turn.get("id") and turn.get("thread_id") == thread_id and turn.get("status") == "done" and turn.get("user_text") == text and not turn.get("error"), "native turn identity or completion differs"
    assert calls and all(row.get("id") and row.get("turn_id") == turn["id"] and row.get("thread_id") == thread_id and not row.get("err") for row in calls), "native Trace lacks its actual successful model calls"
    assert any(row.get("kind") == "done" and row.get("agent_id") == "manager" for row in trace.get("events", [])), "native Trace lacks durable manager completion"


def assert_refused_trace(trace, thread_id, cancellation):
    assert_trace_identity(trace, thread_id, CONTINUE_TEXT)
    result = tool_receipt(trace)
    assert (result.get("ok") is False and result.get("code") == "thread_wake_prohibited"
            and result.get("source_kind") == "schedule_cancelled" and result.get("source_text") == cancellation["text"]
            and result.get("source_seq") == cancellation["seq"]), "ordinary Continue bypassed the exact human cancellation"
    assert not any(row.get("kind") == "schedule" for row in trace["events"]), "refused continuation recorded a new armed wait"
    return result


def wait_for(context, deadline, probe, stage):
    while True:
        deadline.remaining(stage)
        result = probe()
        if result is not None and result is not False:
            return result
        deadline.pause(stage)


def capture(context, deadline, name, row=None):
    row = row or smoke.wait_window(context, deadline)
    notice.save_window(context, deadline, name, row)
    image = context["evidence"] / (name + ".png")
    pixels = smoke.readiness.pixel_observation(image)
    assert pixels["content_visible"], "native screenshot is blank or almost uniform"
    save(context["evidence"] / (name + ".pixels.json"), pixels)
    return row


def wait_ui(context, deadline, waiting, name):
    def state():
        row = smoke.wait_window(context, deadline)
        try:
            assert_wait_ui(row, waiting)
        except AssertionError as error:
            save(context["evidence"] / (name + ".pending.json"), dict(reason=str(error), snapshot=row))
            return None
        return row
    return wait_for(context, deadline, state, "native wait state " + name)


def wait_fixture_engine(context, deadline, retired_pid):
    def ready():
        try:
            lease = json.loads((context["data"] / "engine.json").read_text())
        except FileNotFoundError:
            return None
        assert isinstance(lease, dict) and type(lease.get("pid")) is int, "private engine lease is malformed"
        if lease["pid"] == retired_pid:
            return None
        origin = smoke.engine_origin(lease["url"])
        assert lease.get("mock") is False, "reopened native case still uses the neutral mock classifier"
        smoke.assert_process(context, lease["pid"], "engine")
        try:
            meta = smoke.request_json(origin, "/api/meta", deadline)
        except (ConnectionError, TimeoutError):
            return None
        except urllib.error.URLError as error:
            if isinstance(error.reason, (ConnectionError, TimeoutError)):
                return None
            raise
        assert meta.get("mock") is False and meta.get("data_dir") == str(context["data"]), "private fixture engine HTTP identity differs"
        clients = meta.get("clients", [])
        assert isinstance(clients, list) and all(isinstance(row, dict) for row in clients), "private fixture engine clients are malformed"
        if not any(row.get("surface") == "desktop" and row.get("pid") == context["app_pid"] for row in clients):
            return None
        context["origin"] = origin
        return lease
    return wait_for(context, deadline, ready, "nonmock private engine and acknowledged desktop presence")


def reopen(context, deadline, lease, name):
    old_pid = context["app_pid"]
    previous_readiness = dict(pid=old_pid, stage=context.get("readiness_stage"),
                              native_input_attempted=bool(context.get("native_input_attempted")))
    notice.control(context, deadline, "close")
    handle = next((process for process in context.get("reopened_processes", []) if process.pid == old_pid), None)
    wait_for(context, deadline, lambda: handle.poll() is not None if handle else smoke.runner.process_argv(old_pid) is None, "private native desktop exit")
    if handle is not None:
        assert handle.wait(timeout=deadline.remaining("private desktop reap")) == 0, "private native desktop did not close normally"
    if smoke.runner.process_argv(lease["pid"]) is not None:
        smoke.assert_process(context, lease["pid"], "engine")
        os.kill(lease["pid"], signal.SIGTERM)
        wait_for(context, deadline, lambda: smoke.runner.process_argv(lease["pid"]) is None, "private engine exit before cold reopen")
    executable = context["app"] / "Contents/MacOS/zwai"
    def logstream(suffix):
        descriptor = os.open(context["evidence"] / (name + suffix), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        return os.fdopen(descriptor, "wb")
    with logstream(".stdout.log") as out, logstream(".stderr.log") as err:
        process = subprocess.Popen([str(executable), "desktop", "--data-dir", str(context["data"])], cwd=context["workspace"], stdout=out, stderr=err, start_new_session=True)
    context.setdefault("reopened_processes", []).append(process)
    context["app_pid"] = process.pid
    current = wait_fixture_engine(context, deadline, lease["pid"])
    smoke.assert_process(context, process.pid, "desktop")
    # Initial observation belongs to this new, identity-checked process. The
    # retired PID's inputs remain recorded; they never become eligible to retry.
    context["readiness_stage"], context["native_input_attempted"] = "window", False
    smoke.wait_window(context, deadline, initial=True)
    context["readiness_stage"] = "scenario"
    save(context["evidence"] / (name + ".json"), dict(old_desktop_pid=old_pid, old_engine_pid=lease["pid"], new_desktop_pid=process.pid, lease=current, previous_readiness=previous_readiness, setup="normal private desktop close; SIGTERM only identity-validated case engine; same-data native reopen"))
    return current


def select_target(context, deadline, title):
    notice.press_available(context, deadline, "Conversations")
    notice.press_available(context, deadline, title)


def send(context, deadline, thread_id, text, name):
    before = {row["id"] for row in api(context, deadline, "/api/threads/" + thread_id + "/turns")["turns"]}
    smoke.submit_native(context, deadline, text)
    def complete():
        turns = api(context, deadline, "/api/threads/" + thread_id + "/turns")["turns"]
        fresh = [row for row in turns if row["id"] not in before]
        assert len(fresh) <= 1, "one native Send created multiple turns"
        if not fresh:
            return None
        assert fresh[0].get("user_text") == text, "native Send accepted different human content"
        if fresh[0]["status"] in ("error", "cancelled"):
            raise AssertionError("native human turn failed: " + str(fresh[0]))
        if fresh[0]["status"] != "done":
            return None
        trace = api(context, deadline, "/api/trace/" + fresh[0]["id"])
        assert_trace_identity(trace, thread_id, text)
        save(context["evidence"] / (name + ".trace.json"), trace)
        return trace
    return wait_for(context, deadline, complete, "one completed native human turn")


def log(context, deadline, thread_id):
    result = api(context, deadline, "/api/threads/" + thread_id + "/log?limit=1000")
    assert result.get("has_more") is False, "private native fixture log must be complete"
    return result["events"]


def no_wait(context, deadline, thread_id, old_id):
    rows = api(context, deadline, "/api/schedules")["schedules"]
    assert_cancelled_rows(rows, thread_id, old_id)
    assert api(context, deadline, "/api/threads/" + thread_id + "/schedules")["schedules"] == [], "cancelled target still owns an active wait"
    return rows


def run(context):
    smoke.runner.require_guest()
    deadline = smoke.Deadline(context["timeout"])
    context["ax_identity"] = smoke.validate_helper(context["ax"])
    assert hashlib.sha256(context["controls"].read_bytes()).hexdigest() == context["control_sha256"], "prepared control binary differs"
    _, lease, _ = smoke.wait_initial_ready(context, deadline)
    assert api(context, deadline, "/api/threads")["threads"] == [], "case must start with fresh private data"
    fixture = Fixture(context["evidence"])
    failure = None
    try:
        settings = api(context, deadline, "/api/settings")["settings"]
        api(context, deadline, "/api/settings", {
            "models": {"default": "native-cancel-provider", "providers": [{"id": "native-cancel-provider", "label": "Offline native cancellation acceptance", "base_url": fixture.url, "model": "native-cancel-model", "api_key": "", "context_window": 128000}]},
            "ui": {**settings["ui"], "locale": "en", "transcript_mode": "user"},
            "swarm": {**settings["swarm"], "auto_title": False},
            "memory": {**settings["memory"], "auto_review": False},
        }, "PUT")
        lease = reopen(context, deadline, lease, "01-fixture-engine")
        title = "Native cancellation " + context["case_id"][:8]
        origin = api(context, deadline, "/api/threads", {"title": "Native origin " + context["case_id"][:8]}, "POST")["thread"]["id"]
        thread_id = api(context, deadline, "/api/threads", {"title": title}, "POST")["thread"]["id"]
        request = dict(kind="thread", thread_id=thread_id, origin_thread_id=origin, title="Native pending check", prompt="Inspect the current work state.", delay_s=3600)
        initial = api(context, deadline, "/api/schedules", request, "POST")["schedule"]
        old_id = initial["id"]
        save(context["evidence"] / "schedule-cancel-setup.json", dict(setup="REST-created cross-origin wait; first wait creation is not a native action", origin_thread_id=origin, target_thread_id=thread_id, request=request, schedule=initial))
        select_target(context, deadline, title)
        armed = wait_ui(context, deadline, True, "02-initial-wait")
        assert len(buttons(armed, "Cancel wait")) == 1, "cross-origin setup must leave exactly one actual banner X"
        capture(context, deadline, "02-initial-wait", armed)
        notice.press_available(context, deadline, "Cancel wait")
        def cancelled():
            row = api(context, deadline, "/api/schedules/" + old_id)["schedule"]
            return row if row.get("status") == "cancelled" else None
        wait_for(context, deadline, cancelled, "native X durable cancellation")
        cancellation = human_cancellation(log(context, deadline, thread_id), thread_id, old_id)
        capture(context, deadline, "03-cancelled", wait_ui(context, deadline, False, "03-cancelled"))
        continued = send(context, deadline, thread_id, CONTINUE_TEXT, "04-continued")
        refusal = assert_refused_trace(continued, thread_id, cancellation)
        refused = wait_ui(context, deadline, False, "04-continued")
        assert REFUSED_TEXT in labels(refused), "native transcript does not display the actual tool refusal result"
        capture(context, deadline, "04-continued", refused)
        observations, until = [], time.monotonic() + 5
        while True:
            snapshot = smoke.wait_window(context, deadline)
            assert_wait_ui(snapshot, False)
            no_wait(context, deadline, thread_id, old_id)
            observations.append(dict(wall=time.time(), monotonic=time.monotonic(), no_active_wait=True))
            save(context["evidence"] / "schedule-cancel-observations.json", observations)
            if time.monotonic() >= until:
                break
            deadline.pause("bounded no-recreation observation")
        lease = reopen(context, deadline, lease, "05-cold-reopened")
        select_target(context, deadline, title)
        no_wait(context, deadline, thread_id, old_id)
        assert human_cancellation(log(context, deadline, thread_id), thread_id, old_id)["seq"] == cancellation["seq"], "cold reopen changed the human cancellation identity"
        capture(context, deadline, "05-cold-reopened", wait_ui(context, deadline, False, "05-cold-reopened"))
        allowed = send(context, deadline, thread_id, ALLOW_TEXT, "06-allowed")
        accepted = tool_receipt(allowed)
        assert accepted.get("ok") is True and accepted.get("id") and accepted["id"] != old_id, "later explicit native allowance did not arm one new ID"
        rows = api(context, deadline, "/api/threads/" + thread_id + "/schedules")["schedules"]
        assert len(rows) == 1 and rows[0].get("id") == accepted["id"] and rows[0].get("status") == "active", "later allowance has no unique stored active wait"
        capture(context, deadline, "06-allowed", wait_ui(context, deadline, True, "06-allowed"))
        status = fixture.status()
        assert status["errors"] == [] and status["tool_attempts"] == 2, "fixture did not receive exactly both actual scheduling attempts"
        assert [row["text"] for row in status["sources"]] == [CONTINUE_TEXT, ALLOW_TEXT], "classifier received generated, repeated or different human input"
        assert status["manager_sources"] and all(row == dict(source_seq=cancellation["seq"], source_kind="schedule_cancelled", schedule_id=old_id) for row in status["manager_sources"]), "actual manager input lost the cancellation identity"
        receipt = dict(passed=True, case_id=context["case_id"], thread_id=thread_id, origin_thread_id=origin,
                       cancelled_id=old_id, fresh_id=accepted["id"], cancellation=cancellation, refusal=refusal,
                       turn_ids=[continued["turn"]["id"], allowed["turn"]["id"]],
                       observation_seconds=observations[-1]["monotonic"] - observations[0]["monotonic"],
                       cold_reopen_verified=True, engine_pid=lease["pid"], desktop_pid=context["app_pid"],
                       executable_sha256=hashlib.sha256((context["app"] / "Contents/MacOS/zwai").read_bytes()).hexdigest(),
                       scope="REST cross-origin wait setup; native banner X, Continue refusal, bounded live absence, same-data cold process reopen, explicit allowance and new waiting ID; outer runner verifies final cleanup",
                       functional_budget_seconds=context["timeout"], elapsed_seconds=time.monotonic() - deadline.started)
        save(context["evidence"] / "native-schedule-cancel.json", receipt)
        return receipt
    except BaseException as error:
        failure = error
        raise
    finally:
        try:
            fixture.close()
        except BaseException as error:
            try:
                save(context["evidence"] / "schedule-cancel-fixture-cleanup-error.json", dict(failure=str(error), original_failure=str(failure) if failure else None))
            except OSError as evidence_error:
                error.evidence_error = str(evidence_error)
            if failure is None:
                raise


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", action="store_true", help="run the isolated lifecycle with the existing smoke preflight")
    parser.add_argument("--app", type=Path)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--budget", type=float, default=135)
    parser.add_argument("--ax", type=Path, required=True, help="prepared native_macos_ax plus adjacent identity receipt")
    parser.add_argument("--controls", type=Path, required=True, help="prepared native_generation_interruption_ax")
    parser.add_argument("--control-sha256", required=True, help="frozen controls binary SHA-256")
    args = parser.parse_args(argv)
    if args.case:
        if args.app is None or args.evidence is None:
            parser.error("--case requires --app and --evidence")
        binary, controls = args.ax.resolve(strict=True), args.controls.resolve(strict=True)
        assert hashlib.sha256(controls.read_bytes()).hexdigest() == args.control_sha256, "prepared controls binary differs"
        return smoke.runner.main(["--app", str(args.app), "--evidence", str(args.evidence), "--timeout", str(args.timeout), "--budget", str(args.budget), "--", sys.executable, str(Path(__file__).resolve()), "--ax", str(binary), "--controls", str(controls), "--control-sha256", args.control_sha256],
            preflight=lambda lock, receipt, evidence, deadline: smoke.case_preflight(binary, receipt, evidence, deadline))
    if args.app is not None or args.evidence is not None:
        parser.error("--app and --evidence require --case")
    context = smoke.case_context(os.environ)
    context.update(timeout=float(os.environ["ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS"]), ax=args.ax.resolve(strict=True), controls=args.controls.resolve(strict=True), control_sha256=args.control_sha256)
    try:
        print(json.dumps(run(context)))
        return 0
    except (OSError, ValueError, RuntimeError, AssertionError, subprocess.SubprocessError) as error:
        receipt = dict(passed=False, case_id=context["case_id"], failure=str(error), **smoke.readiness.failure_record(error, "scenario"))
        try:
            save(context["evidence"] / "native-schedule-cancel.json", receipt)
        except OSError as evidence_error:
            receipt["receipt_evidence_error"] = str(evidence_error)
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
