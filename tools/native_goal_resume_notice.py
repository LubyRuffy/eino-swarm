#!/usr/bin/env python3
"""Verify one goal-resume notice, durable events and reopen in a private Mac case.

Run through native_macos_case.py in ci-macOS. All AX helpers must be prepared
before the timed case; the scenario never compiles or installs dependencies.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.request

import native_macos_smoke as smoke


NOTICE_TEXTS = {"继续追求目标。", "Continuing the standing objective.", "Resuming the standing objective."}
SESSION_SOURCE = Path(__file__).with_name("native_goal_resume_notice_ax.m")


def validate_session_helper(binary):
    binary = binary.resolve(strict=True)
    receipt = json.loads(Path(str(binary) + ".json").read_text())
    assert (receipt.get("source_name") == SESSION_SOURCE.name
            and receipt.get("source_sha256") == hashlib.sha256(SESSION_SOURCE.read_bytes()).hexdigest()
            and receipt.get("binary_sha256") == hashlib.sha256(binary.read_bytes()).hexdigest()
            and receipt.get("compiler") == "/usr/bin/xcrun clang" and receipt.get("flags") == smoke.AX_FLAGS
            and os.access(binary, os.X_OK)), "prepared goal-session helper identity differs from source/build receipt"
    return receipt


def prepare_session_helper(binary):
    binary = binary.absolute()
    binary.parent.mkdir(parents=True, exist_ok=True)
    source = SESSION_SOURCE.read_bytes()
    with tempfile.TemporaryDirectory(prefix=".goal-session-ax-", dir=binary.parent) as temporary:
        frozen = Path(temporary) / SESSION_SOURCE.name
        frozen.write_bytes(source)
        candidate = Path(temporary) / "native-goal-session-ax"
        subprocess.run(["/usr/bin/xcrun", "clang", *smoke.AX_FLAGS, str(frozen), "-o", str(candidate)], check=True, timeout=120)
        assert SESSION_SOURCE.read_bytes() == source, "goal-session helper source changed during preparation"
        receipt = {"source_name": SESSION_SOURCE.name, "source_sha256": hashlib.sha256(source).hexdigest(),
                   "binary_sha256": hashlib.sha256(candidate.read_bytes()).hexdigest(),
                   "compiler": "/usr/bin/xcrun clang", "flags": smoke.AX_FLAGS}
        smoke.runner.save_json(Path(str(candidate) + ".json"), receipt)
        os.replace(candidate, binary)
        os.replace(Path(str(candidate) + ".json"), Path(str(binary) + ".json"))
    return validate_session_helper(binary)


def notice_count(row):
    return sum(1 for node in row.get("tree", [])
               if node.get("AXRole") in ("AXStaticText", "AXHeading")
               and (node.get("AXValue") or node.get("AXTitle") or "").strip() in NOTICE_TEXTS)


def assert_single_notice(row):
    count = notice_count(row)
    assert count == 1, "native transcript must contain one visible continuation notice; found " + str(count)
    return count


def assert_notice_in_view(row):
    assert row.get("notice_in_view") is True and type(row.get("notice_count")) is int and row["notice_count"] == 1, "notice must be inside the actual viewport"
    bounds = []
    for name in ("notice_frame", "viewport_frame"):
        value = row.get(name, {})
        assert all(type(value.get(key)) in (int, float) and math.isfinite(value[key]) for key in ("x", "y", "width", "height")), "notice viewport geometry must be finite"
        assert value["width"] > 0 and value["height"] > 0, "notice viewport geometry must be nonempty"
        bounds.append(value)
    notice, viewport = bounds
    assert (notice["x"] >= viewport["x"] and notice["y"] >= viewport["y"] and
            notice["x"] + notice["width"] <= viewport["x"] + viewport["width"] and
            notice["y"] + notice["height"] <= viewport["y"] + viewport["height"]), "notice must be fully inside the actual viewport"


def assert_resume_events(events, before, after):
    resumed = [event for event in events if event.get("kind") == "goal_resumed"]
    continued = [event for event in events if event.get("kind") == "goal_continued"]
    assert len(resumed) == len(continued) == 1, "one resume must retain both distinct durable events exactly once"
    old, new = resumed[0], continued[0]
    assert old.get("role") == "human" and old.get("turn_id") == before, "resume must retain human origin and previous turn"
    assert new.get("turn_id") == after and after != before, "continuation must belong to the new turn"
    assert 0 < old.get("seq", 0) < new.get("seq", 0), "resume event must precede continuation by durable identity"
    return [old["seq"], new["seq"]]


def eventually(context, deadline, probe, stage):
    last = None
    while True:
        deadline.remaining(stage)
        try:
            value = probe()
            if value:
                return value
        except (OSError, ValueError, AssertionError, RuntimeError, KeyError) as error:
            last = str(error)
        smoke.runner.save_json(context["evidence"] / "goal-notice-progress.json", {"stage": stage, "last": last})
        deadline.pause(stage)


def api(context, deadline, path, body=None, method=None):
    raw = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(context["origin"] + path, data=raw,
                                    headers={"Content-Type": "application/json"}, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), smoke.NoRedirect())
    with opener.open(request, timeout=min(2, deadline.remaining("private HTTP " + path))) as response:
        return json.load(response)


def assert_goal_trace(trace, thread_id, turn_id, objective=None):
    turn, events, calls = trace.get("turn", {}), trace.get("events", []), trace.get("llm_calls", [])
    assert (turn.get("id") == turn_id and turn.get("thread_id") == thread_id
            and turn.get("status") == "done" and not turn.get("error")), "goal Trace turn identity or completion differs"
    assert calls and all(call.get("id") and call.get("thread_id") == thread_id and call.get("turn_id") == turn_id
                         and not call.get("err") for call in calls), "goal Trace lacks its own successful model calls"
    own = [event for event in events if event.get("thread_id") == thread_id and event.get("turn_id") == turn_id]
    completed = {event.get("tool_call_id") for event in own
                 if event.get("kind") == "tool_call" and event.get("text", "").startswith("complete_goal(") and event.get("tool_call_id")}
    results = {event.get("tool_call_id") for event in own if event.get("kind") == "tool_result" and not event.get("err")}
    assert (completed and completed <= results and any(event.get("kind") == "goal_complete" for event in own)
            and any(event.get("kind") == "done" and event.get("agent_id") == "manager" for event in own)), "goal Trace lacks paired completion and durable manager finish"
    if objective is not None:
        assert (turn.get("user_text") == objective and any(event.get("kind") == "user_message" and event.get("text") == objective
                                                         for event in own)), "goal Trace input differs from the API fixture objective"


def prepare_goal_fixture(context, deadline):
    title = "Native notice " + context["case_id"][:8]
    objective = "Verify the assigned inputs. native-notice-" + context["case_id"][:8]
    created = api(context, deadline, "/api/threads", {"title": title}, "POST")
    thread_id = created["thread"]["id"]
    files = api(context, deadline, "/api/threads/" + thread_id + "/files")
    workspace, data = Path(files["workspace"]).resolve(strict=True), context["data"].resolve(strict=True)
    assert Path(files["workspace"]).is_absolute() and workspace != data and workspace.is_relative_to(data), "API fixture needs its own private conversation workspace"
    request = {"text": "/goal " + objective}
    accepted = api(context, deadline, "/api/threads/" + thread_id + "/turns", request, "POST")
    first_id = accepted["turn"]["id"]
    assert first_id and accepted["turn"].get("thread_id") == thread_id, "API fixture accepted another turn identity"
    receipt = {"setup": "normal local API /goal with actual offline completion", "thread_request": {"title": title},
               "thread_response": created, "turn_request": request, "turn_response": accepted,
               "thread_id": thread_id, "initial_turn_id": first_id, "workspace": str(workspace)}
    smoke.runner.save_json(context["evidence"] / "goal-notice-fixture.json", receipt)

    def completed():
        thread = api(context, deadline, "/api/threads/" + thread_id)["thread"]
        turns = api(context, deadline, "/api/threads/" + thread_id + "/turns")["turns"]
        assert len(turns) == 1 and turns[0]["id"] == first_id, "API fixture must execute exactly its initial turn"
        assert thread.get("id") == thread_id and thread.get("goal") == objective, "API fixture goal identity differs"
        if turns[0]["status"] != "done" or thread.get("goal_complete") is not True:
            return None
        trace = api(context, deadline, "/api/trace/" + first_id)
        assert_goal_trace(trace, thread_id, first_id, objective)
        smoke.runner.save_json(context["evidence"] / "goal-notice-fixture-trace.json", trace)
        return thread, turns[0]

    thread, first = eventually(context, deadline, completed, "completed API goal fixture and actual model Trace")
    receipt.update(goal_complete=True, completed_turn=first)
    smoke.runner.save_json(context["evidence"] / "goal-notice-fixture.json", receipt)
    return thread, first, title, objective


def control(context, deadline, action, value=None):
    smoke.assert_process(context, context["app_pid"], "desktop")
    smoke.wait_window(context, deadline)
    assert hashlib.sha256(context["controls"].read_bytes()).hexdigest() == context["control_sha256"], "prepared control helper changed"
    command = [str(context["controls"]), str(context["app_pid"]), action]
    if value is not None:
        command.append(value)
    result = subprocess.run(command, capture_output=True, text=True,
                            timeout=min(8, deadline.remaining("native " + action)))
    smoke.runner.save_json(context["evidence"] / "goal-notice-control-last.json",
                           {"action": action, "value": value, "pid": context["app_pid"], "returncode": result.returncode,
                            "stdout": result.stdout, "stderr": result.stderr})
    assert result.returncode == 0, "native control failed: " + result.stdout + result.stderr
    row = json.loads(result.stdout)
    assert row.get("ax_error") == 0 and row.get("pid") == context["app_pid"], "native control returned another target"
    return row


def press_available(context, deadline, value):
    while True:
        deadline.remaining("native control " + value)
        row = smoke.wait_window(context, deadline)
        matches = [node for node in row.get("tree", [])
                   if node.get("AXRole") in ("AXButton", "AXRadioButton", "AXCheckBox")
                   and any(value in node.get(key, "") for key in ("AXTitle", "AXDescription"))]
        exact = [node for node in matches if value in (node.get("AXTitle"), node.get("AXDescription"))]
        matches = exact or matches
        smoke.runner.save_json(context["evidence"] / "goal-notice-availability-last.json",
                               {"value": value, "matches": len(matches), "snapshot": row})
        assert len(matches) <= 1, "native control title is ambiguous: " + value
        if matches and matches[0].get("AXEnabled") is True:
            break
        deadline.pause("available native control " + value)
    return control(context, deadline, "press", value)


def session_summary_label(row):
    labels = [(node.get("AXTitle") or node.get("AXDescription") or "").strip()
              for node in row.get("tree", []) if node.get("AXRole") == "AXButton"]
    matches = [label for label in labels if label.startswith(("Worked for ", "工作了 "))]
    assert len(matches) == 1, "native fixture requires one completed session disclosure"
    return matches[0]


def expand_session(context, deadline):
    row = smoke.wait_window(context, deadline)
    session_summary_label(row)
    state = session_control(context, deadline, "inspect")
    if state["expanded"] is False:
        # An older turn may already show resumed while continued is unmounted
        # here. Only the disclosure's actual state can decide whether to press.
        session_control(context, deadline, "expand")
        while True:
            state = session_control(context, deadline, "inspect")
            if state["expanded"] is True:
                break
            deadline.pause("expanded goal session content")
    return smoke.wait_window(context, deadline)


def session_control(context, deadline, action):
    executable = smoke.assert_process(context, context["app_pid"], "desktop")
    if action != "startup":
        smoke.wait_window(context, deadline)
    validate_session_helper(context["session_ax"])
    result = subprocess.run([str(context["session_ax"]), str(context["app_pid"]), action,
                             str(executable.parent.parent.parent)], capture_output=True, text=True,
                            timeout=min(8, deadline.remaining("native session " + action)))
    observation = {"action": action, "returncode": result.returncode, "pid": context["app_pid"],
                   "stdout": result.stdout, "stderr": result.stderr}
    smoke.runner.save_json(context["evidence"] / "goal-notice-session-last.json", observation)
    if action == "startup":
        context["startup_observations"] = context.get("startup_observations", 0) + 1
        smoke.runner.save_json(context["evidence"] / ("goal-notice-startup-%03d.json" % context["startup_observations"]), observation)
    assert result.returncode == 0, "native session control failed: " + result.stdout + result.stderr
    row = json.loads(result.stdout)
    assert (row.get("pid") == context["app_pid"] and row.get("action") == action
            and Path(row["bundle"]).resolve(strict=True) == context["app"].resolve(strict=True)), "invalid native session target identity"
    if action == "startup":
        observed, registered = row.get("observed_bundle"), row.get("registered")
        assert (type(registered) is bool and ((registered is False and observed is None) or
                (registered is True and isinstance(observed, str) and
                 Path(observed).resolve(strict=True) == context["app"].resolve(strict=True)))), "invalid startup bundle identity"
    else:
        assert (row.get("ax_error") == 0 and type(row.get("expanded")) is bool
                and type(row.get("pressed")) is bool), "invalid native session state or identity"
    return row


def wait_registered_application(context, deadline):
    while True:
        state = session_control(context, deadline, "startup")
        if state["registered"] is True:
            return state
        deadline.pause("private native app bundle registration")


def capture(context, deadline, name):
    row = expand_session(context, deadline)
    assert_single_notice(row)
    visible = session_control(context, deadline, "reveal-notice")
    smoke.runner.save_json(context["evidence"] / (name + ".viewport.json"), visible)
    assert_notice_in_view(visible)
    row = smoke.wait_window(context, deadline)
    assert_single_notice(row)
    smoke.runner.save_json(context["evidence"] / (name + ".ax.json"), row)
    image = context["evidence"] / (name + ".png")
    subprocess.run(["/usr/sbin/screencapture", "-x", "-l", str(row["window_id"]), str(image)],
                   check=True, capture_output=True, timeout=deadline.remaining("native screenshot"))
    dimensions = smoke.png_dimensions(image)
    smoke.runner.save_json(context["evidence"] / (name + ".image.json"),
                           {"pid": context["app_pid"], "window_id": row["window_id"],
                            "dimensions": dimensions, "sha256": hashlib.sha256(image.read_bytes()).hexdigest()})
    return row


def run(context):
    smoke.runner.require_guest()
    deadline = smoke.Deadline(context["timeout"])
    context["ax_identity"] = smoke.validate_helper(context["ax"])
    origin, lease = smoke.wait_engine(context, deadline)
    context["origin"] = origin
    wait_registered_application(context, deadline)
    assert api(context, deadline, "/api/threads")["threads"] == [], "case must have fresh conversations"
    settings = api(context, deadline, "/api/settings")["settings"]
    api(context, deadline, "/api/settings", {
        "ui": {**settings["ui"], "locale": "zh", "transcript_mode": "user"},
        "swarm": {**settings["swarm"], "auto_title": False},
        "memory": {**settings["memory"], "auto_review": False},
    }, "PUT")
    thread, first, title, objective = prepare_goal_fixture(context, deadline)
    control(context, deadline, "reload")
    for label in ("对话", title, "开始目标"):
        press_available(context, deadline, label)

    def completed_resume():
        turns = api(context, deadline, "/api/threads/" + thread["id"] + "/turns")["turns"]
        assert len(turns) <= 2, "one native resume created duplicate turns"
        if len(turns) != 2 or turns[1]["status"] != "done":
            return None
        log = api(context, deadline, "/api/threads/" + thread["id"] + "/log?limit=1000")
        assert not log.get("has_more"), "native fixture log must be complete"
        assert_resume_events(log["events"], first["id"], turns[1]["id"])
        return turns[1], log

    second, log = eventually(context, deadline, completed_resume, "completed native resume and durable pair")
    traces = [api(context, deadline, "/api/trace/" + turn["id"]) for turn in (first, second)]
    traced = [event for trace in traces for event in trace["events"]]
    sequences = assert_resume_events(traced, first["id"], second["id"])
    assert_goal_trace(traces[0], thread["id"], first["id"], objective)
    assert_goal_trace(traces[1], thread["id"], second["id"])
    smoke.runner.save_json(context["evidence"] / "goal-notice-log.json", log)
    smoke.runner.save_json(context["evidence"] / "goal-notice-traces.json", {"traces": traces})
    eventually(context, deadline, lambda: capture(context, deadline, "01-resumed"), "one live native notice")
    old_pid = context["app_pid"]
    control(context, deadline, "close")
    eventually(context, deadline, lambda: smoke.runner.process_argv(old_pid) is None, "desktop exit before reopen")
    executable = context["app"] / "Contents/MacOS/zwai"
    with (context["evidence"] / "reopened.stdout.log").open("wb") as out, (context["evidence"] / "reopened.stderr.log").open("wb") as err:
        process = subprocess.Popen([str(executable), "desktop", "--mock", "--data-dir", str(context["data"])],
                                   cwd=context["workspace"], stdout=out, stderr=err, start_new_session=True)
    context["app_pid"] = process.pid
    context["origin"], reopened_lease = smoke.wait_engine(context, deadline)
    wait_registered_application(context, deadline)
    for label in ("对话", title):
        press_available(context, deadline, label)
    eventually(context, deadline, lambda: capture(context, deadline, "02-reopened"), "one reopened native notice")
    after = api(context, deadline, "/api/threads/" + thread["id"] + "/log?limit=1000")
    assert_resume_events(after["events"], first["id"], second["id"])
    turns = api(context, deadline, "/api/threads/" + thread["id"] + "/turns")["turns"]
    assert len(turns) == 2, "reopening must not execute another turn"
    receipt = {"passed": True, "case_id": context["case_id"], "thread_id": thread["id"],
               "turn_ids": [first["id"], second["id"]], "durable_event_seqs": sequences,
               "native_notice_counts": [1, 1], "desktop_pids": [old_pid, context["app_pid"]],
               "engine_pids": [lease["pid"], reopened_lease["pid"]],
               "executable_sha256": hashlib.sha256(executable.read_bytes()).hexdigest(),
               "fixture_setup": "normal local API /goal with actual offline completion",
               "native_actions": ["Conversations tab", "select thread", "Start goal", "close", "reopen", "Conversations tab", "select thread"],
               "scope": "native Start goal, one displayed notice, both Trace events and native close/reopen; runner verifies cleanup",
               "elapsed_seconds": time.monotonic() - deadline.started}
    control(context, deadline, "close")
    eventually(context, deadline, lambda: smoke.runner.process_argv(process.pid) is None, "reopened desktop exit")
    receipt["reopened_desktop_exit_code"] = process.wait(timeout=deadline.remaining("reopened desktop reap"))
    assert receipt["reopened_desktop_exit_code"] == 0, "reopened desktop did not close normally"
    smoke.runner.save_json(context["evidence"] / "native-goal-resume-notice.json", receipt)
    return receipt


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--prepare-session-ax", type=Path, help="compile the narrow AXExpanded helper before the case")
    mode.add_argument("--ax", type=Path, help="prepared native_macos_ax helper with its receipt")
    parser.add_argument("--session-ax", type=Path, help="prepared native_goal_resume_notice_ax helper with its receipt")
    parser.add_argument("--controls", type=Path, help="prepared native_generation_interruption_ax helper")
    parser.add_argument("--control-sha256", help="frozen control helper binary SHA-256")
    args = parser.parse_args(argv)
    if args.prepare_session_ax is not None:
        print(json.dumps(prepare_session_helper(args.prepare_session_ax)))
        return 0
    if args.session_ax is None or args.controls is None or not args.control_sha256:
        parser.error("a case requires --session-ax, --controls and --control-sha256")
    context = smoke.case_context(os.environ)
    context.update(ax=args.ax.resolve(strict=True), session_ax=args.session_ax.resolve(strict=True),
                   controls=args.controls.resolve(strict=True), control_sha256=args.control_sha256)
    try:
        print(json.dumps(run(context)))
        return 0
    except (OSError, ValueError, RuntimeError, AssertionError, subprocess.SubprocessError) as error:
        receipt = {"passed": False, "failure": str(error), "case_id": context["case_id"]}
        smoke.runner.save_json(context["evidence"] / "native-goal-resume-notice.json", receipt)
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
