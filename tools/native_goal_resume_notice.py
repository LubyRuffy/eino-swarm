#!/usr/bin/env python3
"""Verify visible goal answers, inline briefing and one resume notice in a Mac case.

Run through native_macos_case.py in ci-macOS. All AX helpers must be prepared
before the timed case; the scenario never compiles or installs dependencies.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request

import native_macos_smoke as smoke


NOTICE_TEXTS = {"继续追求目标。", "Continuing the standing objective.", "Resuming the standing objective."}
SESSION_SOURCE = Path(__file__).with_name("native_goal_resume_notice_ax.m")
VIEWPORT_BASIS = "native_window_scrollarea_webarea"


def save_private_json(path, row):
    encoded = json.dumps(row, indent=2) + "\n"
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o600)
        output.write(encoded)


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
        save_private_json(Path(str(candidate) + ".json"), receipt)
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


def visible_text(row):
    return [node.get("AXValue") or node.get("AXTitle") or "" for node in row.get("tree", [])
            if node.get("AXRole") in ("AXStaticText", "AXHeading")]


def normalized(text):
    return " ".join(text.split())


def answer_witnesses(final):
    assert isinstance(final, str) and final.strip(), "completed turn needs its durable final answer"
    # Use the actual answer's prose, excluding code/table/chart renderer syntax.
    lines, fenced = [], False
    for line in final.splitlines():
        if line.strip().startswith("```"):
            fenced = not fenced
            continue
        if fenced or not line.strip() or line.lstrip().startswith(("|", "#")):
            continue
        text = re.sub(r"^(?:>\s*|\d+\.\s+|-\s+)", "", line.strip()).replace("`", "").replace("**", "")
        if len(text) >= 16:
            lines.append(text)
    assert len(lines) >= 2, "final answer needs distinct opening and closing prose witnesses"
    return [lines[0], lines[-1]]


def assert_default_answers(row, traces):
    texts, required, witnesses = [normalized(text) for text in visible_text(row)], {}, []
    for trace in traces:
        pair = answer_witnesses(trace.get("turn", {}).get("final"))
        witnesses.append(pair)
        for text in pair:
            key = normalized(text)
            required[key] = required.get(key, 0) + 1
    for text, count in required.items():
        assert sum(value.count(text) for value in texts) >= count, "both completed answers must be readable without expanding work: " + text
    return witnesses


def assert_work_collapsed(state):
    folds = state.get("folds")
    assert isinstance(folds, list) and folds and all(row.get("expanded") is False for row in folds), "actual work disclosures must default to collapsed"
    return folds


def finite_rectangle(value):
    return (isinstance(value, dict) and all(type(value.get(key)) in (int, float) and math.isfinite(value[key])
            for key in ("x", "y", "width", "height")) and value["width"] > 0 and value["height"] > 0)


def assert_root_binding(row, binding):
    root = row.get("root_window", {})
    assert (type(row.get("pid")) is int and row["pid"] > 0 and isinstance(root, dict)
            and type(root.get("owner_pid")) is int and root["owner_pid"] == row["pid"]
            and root.get("role") == "AXWindow" and isinstance(binding, dict)
            and binding.get("children_reachable") is True and binding.get("parent_chain_root") is True
            and type(binding.get("root_owner_pid")) is int and binding["root_owner_pid"] == root["owner_pid"]
            and binding.get("root_role") == root["role"]), "target must be bound to the exact private root window"


def assert_reading_admission(row):
    assert_notice_in_view(row)
    assert_root_binding(row, row.get("binding"))
    clip, point = row.get("viewport_frame"), row.get("point", {})
    assert (row.get("wheel_posted") is True and type(row.get("wheel_count")) is int and row["wheel_count"] == 1
            and row.get("viewport_basis") == VIEWPORT_BASIS
            and type(row.get("wheel_delta")) is int and row["wheel_delta"] > 0
            and row.get("notice_reveal_requested") is True and row.get("notice_reveal_status") == 0
            and row.get("scrollarea_verified") is True and row.get("hit_verified") is True and row.get("hit_status") == 0
            and type(row.get("notice_owner_pid")) is int and row["notice_owner_pid"] > 0
            and type(row.get("hit_owner_pid")) is int and row["hit_owner_pid"] == row["notice_owner_pid"]
            and finite_rectangle(clip) and all(type(point.get(key)) in (int, float) and math.isfinite(point[key]) for key in ("x", "y"))
            and clip["x"] < point["x"] < clip["x"] + clip["width"]
            and clip["y"] < point["y"] < clip["y"] + clip["height"]), "native reading wheel needs one verified transcript target"


def assert_target_geometry(row, *, unique):
    targets, count = row.get("targets"), row.get("target_count")
    assert isinstance(targets, list) and type(count) is int and count == len(targets) and count > 0, "target readback is incomplete"
    assert count == 1, "one selected text target is required"
    matched = row.get("matched_count")
    assert type(matched) is int and matched > 0 and (not unique or matched == 1), "opening witness must identify one actual text target"
    visible = False
    for target in targets:
        box, clip = target.get("frame"), target.get("viewport")
        assert_root_binding(row, target.get("binding"))
        assert (type(target.get("owner_pid")) is int and target["owner_pid"] > 0 and finite_rectangle(box) and finite_rectangle(clip)
                and target.get("viewport_basis") == VIEWPORT_BASIS
                and type(target.get("in_view")) is bool), "target geometry and owner must be known"
        inside = (box["x"] >= clip["x"] and box["y"] >= clip["y"] and
                  box["x"] + box["width"] <= clip["x"] + clip["width"] and
                  box["y"] + box["height"] <= clip["y"] + clip["height"])
        assert target["in_view"] == inside, "target containment disagrees with its native scroller viewport"
        visible |= inside
    assert visible, "target must remain inside its actual native scroller viewport"


def assert_inline_briefing(row, summary, expanded):
    assert summary.strip(), "briefing must come from a nonempty durable compacted summary"
    assert not any(node.get("AXRole") in ("AXDialog", "AXSheet") for node in row.get("tree", [])), "inline briefing must not open a dialog"
    present = normalized(summary) in normalized("\n".join(visible_text(row)))
    assert present if expanded else not present, "complete inline briefing must be readable" if expanded else "collapsed briefing must be hidden"


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
        save_private_json(context["evidence"] / "goal-notice-progress.json", {"stage": stage, "last": last})
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
    save_private_json(context["evidence"] / "goal-notice-fixture.json", receipt)

    def completed():
        thread = api(context, deadline, "/api/threads/" + thread_id)["thread"]
        turns = api(context, deadline, "/api/threads/" + thread_id + "/turns")["turns"]
        assert len(turns) == 1 and turns[0]["id"] == first_id, "API fixture must execute exactly its initial turn"
        assert thread.get("id") == thread_id and thread.get("goal") == objective, "API fixture goal identity differs"
        if turns[0]["status"] != "done" or thread.get("goal_complete") is not True:
            return None
        trace = api(context, deadline, "/api/trace/" + first_id)
        assert_goal_trace(trace, thread_id, first_id, objective)
        save_private_json(context["evidence"] / "goal-notice-fixture-trace.json", trace)
        return thread, turns[0]

    thread, first = eventually(context, deadline, completed, "completed API goal fixture and actual model Trace")
    receipt.update(goal_complete=True, completed_turn=first)
    save_private_json(context["evidence"] / "goal-notice-fixture.json", receipt)
    return thread, first, title, objective


def resume_goal_fixture(context, deadline, thread_id):
    # Completed goals intentionally have no native Start banner. Exercise the
    # existing recovery API, then accept its actual results in the native UI.
    request = {"goal_resume": True}
    acknowledged = api(context, deadline, "/api/threads/" + thread_id, request, "PATCH")
    assert acknowledged.get("thread", {}).get("id") == thread_id, "resume fixture acknowledged another conversation"
    save_private_json(context["evidence"] / "goal-notice-resume-fixture.json",
                           {"setup": "normal local API goal_resume; completed goal has no native Start",
                            "thread_id": thread_id, "request": request, "response": acknowledged})


def control(context, deadline, action, value=None):
    smoke.assert_process(context, context["app_pid"], "desktop")
    wait_window(context, deadline)
    assert hashlib.sha256(context["controls"].read_bytes()).hexdigest() == context["control_sha256"], "prepared control helper changed"
    command = [str(context["controls"]), str(context["app_pid"]), action]
    if value is not None:
        command.append(value)
    result, _ = run_native_helper(context, deadline, "control", action, command, value)
    assert result.returncode == 0, "native control failed: " + result.stdout + result.stderr
    row = json.loads(result.stdout)
    assert row.get("ax_error") == 0 and row.get("pid") == context["app_pid"], "native control returned another target"
    return row


def run_native_helper(context, deadline, kind, action, command, value=None, limit=8):
    started, failure, result = time.monotonic(), None, None
    try:
        left = deadline.remaining("native " + action)
        # A screenshot writer may replace its precreated output. Restrict only
        # that child; the parent and helper/compiler processes keep their umask.
        permissions = {"umask": 0o077} if kind == "screenshot" else {}
        result = subprocess.run(command, capture_output=True, text=True,
                                timeout=left if limit is None else min(limit, left), **permissions)
    except subprocess.TimeoutExpired as error:
        failure = error
    def stream(value):
        return value.decode("utf-8", errors="replace") if isinstance(value, bytes) else value or ""
    observation = {"action": action, "value": value, "command": command, "pid": context["app_pid"],
                   "returncode": result.returncode if result else None, "timed_out": failure is not None,
                   "stdout": stream(result.stdout if result else failure.stdout),
                   "stderr": stream(result.stderr if result else failure.stderr),
                   "started_monotonic": started, "finished_monotonic": time.monotonic()}
    observation["elapsed_seconds"] = observation["finished_monotonic"] - started
    context["helper_observations"] = context.get("helper_observations", 0) + 1
    try:
        for name in ("goal-notice-helper-%03d.json" % context["helper_observations"], "goal-notice-" + kind + "-last.json"):
            save_private_json(context["evidence"] / name, observation)
    except OSError as evidence_error:
        if failure is None and result.returncode:
            failure = subprocess.CalledProcessError(result.returncode, command, result.stdout, result.stderr)
        if failure is None:
            raise
        failure.evidence_error = str(evidence_error)
    if failure is not None:
        raise failure
    return result, observation


def window_snapshot(context, deadline):
    executable = smoke.assert_process(context, context["app_pid"], "desktop")
    bundle = executable.parent.parent.parent
    identity = smoke.validate_helper(context["ax"])
    assert context.get("ax_identity", identity)["binary_sha256"] == identity["binary_sha256"], "native window helper changed"
    command = [str(context["ax"]), str(context["app_pid"]), "snapshot", context["case_id"],
               str(bundle), context["origin"], ""]
    result, observation = run_native_helper(context, deadline, "window", "snapshot", command)
    assert result.returncode == 0, "native window snapshot failed: " + result.stdout + result.stderr
    row = json.loads(result.stdout)
    assert row.get("origin") == context["origin"] and Path(row["bundle"]).resolve(strict=True) == bundle.resolve(strict=True), "native window snapshot identity differs"
    row["native_call"] = {"receipt": "goal-notice-helper-%03d.json" % context["helper_observations"],
                          "started_monotonic": observation["started_monotonic"], "finished_monotonic": observation["finished_monotonic"]}
    return row


def wait_window(context, deadline):
    # Snapshot only: all target reveals belong to the narrow, path-bound helper.
    while True:
        deadline.remaining("native composer")
        row = window_snapshot(context, deadline)
        if row.get("ready"):
            smoke.assert_window(row, context["app_pid"])
            return row
        deadline.pause("native composer")


def press_available(context, deadline, value):
    while True:
        deadline.remaining("native control " + value)
        row = wait_window(context, deadline)
        matches = [node for node in row.get("tree", [])
                   if node.get("AXRole") in ("AXButton", "AXRadioButton", "AXCheckBox")
                   and any(value in node.get(key, "") for key in ("AXTitle", "AXDescription"))]
        exact = [node for node in matches if value in (node.get("AXTitle"), node.get("AXDescription"))]
        matches = exact or matches
        save_private_json(context["evidence"] / "goal-notice-availability-last.json",
                               {"value": value, "matches": len(matches), "snapshot": row})
        assert len(matches) <= 1, "native control title is ambiguous: " + value
        if matches and matches[0].get("AXEnabled") is True:
            break
        deadline.pause("available native control " + value)
    return control(context, deadline, "press", value)


def session_control(context, deadline, action, value=None):
    executable = smoke.assert_process(context, context["app_pid"], "desktop")
    if action not in ("startup", "inspect-text", "reveal-text"):
        wait_window(context, deadline)
    validate_session_helper(context["session_ax"])
    command = [str(context["session_ax"]), str(context["app_pid"]), action, str(executable.parent.parent.parent)]
    if value is not None:
        command.append(value)
    result, observation = run_native_helper(context, deadline, "session", action, command, value)
    if action == "startup":
        context["startup_observations"] = context.get("startup_observations", 0) + 1
        save_private_json(context["evidence"] / ("goal-notice-startup-%03d.json" % context["startup_observations"]), observation)
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
        assert (row.get("ax_error") == 0 and isinstance(row.get("folds"), list)
                and row.get("pressed") is False), "invalid native session state or identity"
    if action in ("read-up", "inspect-text", "reveal-text"):
        row["native_call"] = {"receipt": "goal-notice-helper-%03d.json" % context["helper_observations"],
                              "started_monotonic": observation["started_monotonic"], "finished_monotonic": observation["finished_monotonic"]}
    return row


def wait_registered_application(context, deadline):
    while True:
        state = session_control(context, deadline, "startup")
        if state["registered"] is True:
            return state
        deadline.pause("private native app bundle registration")


def capture(context, deadline, name):
    row = wait_window(context, deadline)
    assert_single_notice(row)
    state = session_control(context, deadline, "inspect")
    assert_work_collapsed(state)
    save_private_json(context["evidence"] / (name + ".folds.json"), state)
    visible = session_control(context, deadline, "reveal-notice")
    save_private_json(context["evidence"] / (name + ".viewport.json"), visible)
    assert_notice_in_view(visible)
    row = wait_window(context, deadline)
    assert_single_notice(row)
    save_window(context, deadline, name, row)
    return row


def save_window(context, deadline, name, row):
    save_private_json(context["evidence"] / (name + ".ax.json"), row)
    image = context["evidence"] / (name + ".png")
    with os.fdopen(os.open(image, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "wb") as output:
        os.fchmod(output.fileno(), 0o600)
    command = ["/usr/sbin/screencapture", "-x", "-l", str(row["window_id"]), str(image)]
    result, observation = run_native_helper(context, deadline, "screenshot", name, command, limit=None)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command, result.stdout, result.stderr)
    assert image.stat().st_mode & 0o777 == 0o600, "native screenshot output must remain private"
    dimensions = smoke.png_dimensions(image)
    save_private_json(context["evidence"] / (name + ".image.json"),
                           {"pid": context["app_pid"], "window_id": row["window_id"],
                            "dimensions": dimensions, "sha256": hashlib.sha256(image.read_bytes()).hexdigest(),
                            "capture_started_monotonic": observation["started_monotonic"],
                            "capture_finished_monotonic": observation["finished_monotonic"],
                            "native_call_receipt": "goal-notice-helper-%03d.json" % context["helper_observations"]})


def capture_target(context, deadline, name, expected, *, unique):
    request = {"witness": expected, "unique": unique}
    revealed = session_control(context, deadline, "reveal-text", json.dumps(request))
    save_private_json(context["evidence"] / (name + ".target-reveal.json"), revealed)
    assert revealed.get("reveal_count") == 1, "target must be revealed exactly once"
    assert_target_geometry(revealed, unique=unique)
    path = revealed.get("selected_path")
    assert isinstance(path, list) and path and all(type(index) is int and index >= 0 for index in path), "revealed target path must be known"
    request["path"] = path
    before = session_control(context, deadline, "inspect-text", json.dumps(request))
    save_private_json(context["evidence"] / (name + ".target-before.json"), before)
    assert_target_geometry(before, unique=unique)
    assert before.get("reveal_count") == 0 and before.get("selected_path") == path and before["matched_count"] == revealed["matched_count"], "revealed target identity changed"
    row = wait_window(context, deadline)
    save_window(context, deadline, name, row)
    after = session_control(context, deadline, "inspect-text", json.dumps(request))
    save_private_json(context["evidence"] / (name + ".target-after.json"), after)
    assert_target_geometry(after, unique=unique)
    assert after.get("reveal_count") == 0 and after.get("selected_path") == path and after["matched_count"] == revealed["matched_count"], "captured target identity changed"
    return row


def capture_answers(context, deadline, name, traces):
    for trace in traces:
        answer_witnesses(trace.get("turn", {}).get("final"))
    pending = 0
    while True:
        row = wait_window(context, deadline)
        try:
            pairs = assert_default_answers(row, traces)
            break
        except AssertionError as error:
            observation = {"failure": str(error), "snapshot": row}
            if pending == 0:
                save_private_json(context["evidence"] / (name + "-answers-first-pending.json"), observation)
            save_private_json(context["evidence"] / (name + "-answers-last-pending.json"), observation)
            pending += 1
            deadline.pause("selected conversation's completed answers")
    reading = session_control(context, deadline, "read-up")
    save_private_json(context["evidence"] / (name + ".reading.json"), reading)
    assert_reading_admission(reading)
    for index, pair in enumerate(pairs):
        for edge, expected in zip(("opening", "closing"), pair):
            row = capture_target(context, deadline, "%s-answer%d-%s" % (name, index + 1, edge), expected, unique=edge == "opening")
            assert_default_answers(row, traces)
    return pairs


def compact_fixture(context, deadline, thread_id, turn_ids):
    settings = api(context, deadline, "/api/settings")["settings"]
    api(context, deadline, "/api/settings", {"swarm": {**settings["swarm"], "compact_keep_messages": 2}}, "PUT")
    before = api(context, deadline, "/api/threads/" + thread_id + "/log?limit=1000")
    assert not before.get("has_more"), "compact fixture requires complete original history"
    previous = {event["seq"] for event in before["events"] if event.get("kind") == "compacted"}
    assert not previous, "private fixture must have no earlier compacted events before selecting its unique briefing"
    api(context, deadline, "/api/threads/" + thread_id + "/compact", {}, "POST")
    after = api(context, deadline, "/api/threads/" + thread_id + "/log?limit=1000")
    fresh = [event for event in after["events"] if event.get("kind") == "compacted" and event["seq"] not in previous]
    assert not after.get("has_more") and len(fresh) == 1 and not fresh[0].get("err"), "normal compact must record one successful durable event"
    summary = json.loads(fresh[0]["text"])["summary"]
    assert isinstance(summary, str) and summary.strip(), "normal compact must store its real complete summary"
    turns = api(context, deadline, "/api/threads/" + thread_id + "/turns")["turns"]
    assert [turn["id"] for turn in turns] == turn_ids, "compression must not create another turn"
    save_private_json(context["evidence"] / "goal-notice-compact.json", {"event": fresh[0], "log": after, "turn_ids": turn_ids})
    return summary


def exercise_inline_briefing(context, deadline, summary):
    row = wait_window(context, deadline)
    assert_inline_briefing(row, summary, False)
    press_available(context, deadline, "查看压缩后的简报")
    row = wait_window(context, deadline)
    assert_inline_briefing(row, summary, True)
    lines = [line.strip() for line in summary.splitlines() if line.strip()]
    for edge, expected in zip(("opening", "closing"), (lines[0], lines[-1])):
        row = capture_target(context, deadline, "inline-briefing-" + edge, expected, unique=False)
        assert_inline_briefing(row, summary, True)
    press_available(context, deadline, "收起简报")
    row = wait_window(context, deadline)
    assert_inline_briefing(row, summary, False)
    save_window(context, deadline, "inline-briefing-collapsed", row)


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
        "swarm": {**settings["swarm"], "auto_title": False, "goal_auto_compact_percent": 100,
                  "auto_compact_tokens": 1_000_000, "context_char_budget": 1_000_000},
        "memory": {**settings["memory"], "auto_review": False},
    }, "PUT")
    thread, first, title, objective = prepare_goal_fixture(context, deadline)
    control(context, deadline, "reload")
    for label in ("对话", title):
        press_available(context, deadline, label)
    resume_goal_fixture(context, deadline, thread["id"])

    def completed_resume():
        turns = api(context, deadline, "/api/threads/" + thread["id"] + "/turns")["turns"]
        assert len(turns) <= 2, "one API recovery created duplicate turns"
        if len(turns) != 2 or turns[1]["status"] != "done":
            return None
        log = api(context, deadline, "/api/threads/" + thread["id"] + "/log?limit=1000")
        assert not log.get("has_more"), "native fixture log must be complete"
        assert_resume_events(log["events"], first["id"], turns[1]["id"])
        return turns[1], log

    second, log = eventually(context, deadline, completed_resume, "completed API recovery and durable pair")
    traces = [api(context, deadline, "/api/trace/" + turn["id"]) for turn in (first, second)]
    traced = [event for trace in traces for event in trace["events"]]
    sequences = assert_resume_events(traced, first["id"], second["id"])
    assert_goal_trace(traces[0], thread["id"], first["id"], objective)
    assert_goal_trace(traces[1], thread["id"], second["id"])
    save_private_json(context["evidence"] / "goal-notice-log.json", log)
    save_private_json(context["evidence"] / "goal-notice-traces.json", {"traces": traces})
    witnesses = capture_answers(context, deadline, "01-resumed", traces)
    eventually(context, deadline, lambda: capture(context, deadline, "01-resumed"), "one live native notice")
    summary = compact_fixture(context, deadline, thread["id"], [first["id"], second["id"]])
    exercise_inline_briefing(context, deadline, summary)
    control(context, deadline, "reload")
    # Native Reload returns to the welcome view. Select the identified fixture
    # again, just as reopen does, before accepting its historical transcript.
    press_available(context, deadline, title)
    capture_answers(context, deadline, "02-reloaded", traces)
    assert_inline_briefing(wait_window(context, deadline), summary, False)
    eventually(context, deadline, lambda: capture(context, deadline, "02-reloaded"), "one reloaded native notice")
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
    press_available(context, deadline, title)
    capture_answers(context, deadline, "03-reopened", traces)
    assert_inline_briefing(wait_window(context, deadline), summary, False)
    eventually(context, deadline, lambda: capture(context, deadline, "03-reopened"), "one reopened native notice")
    after = api(context, deadline, "/api/threads/" + thread["id"] + "/log?limit=1000")
    assert_resume_events(after["events"], first["id"], second["id"])
    turns = api(context, deadline, "/api/threads/" + thread["id"] + "/turns")["turns"]
    assert len(turns) == 2, "reopening must not execute another turn"
    receipt = {"passed": True, "case_id": context["case_id"], "thread_id": thread["id"],
               "turn_ids": [first["id"], second["id"]], "durable_event_seqs": sequences,
               "native_notice_counts": [1, 1, 1], "desktop_pids": [old_pid, context["app_pid"]],
               "engine_pids": [lease["pid"], reopened_lease["pid"]],
               "executable_sha256": hashlib.sha256(executable.read_bytes()).hexdigest(),
               "fixture_setup": "normal local API /goal and PATCH goal_resume, both with actual offline completion",
               "answer_witnesses": witnesses, "briefing_sha256": hashlib.sha256(summary.encode()).hexdigest(),
               "native_actions": ["Conversations tab", "select thread", "expand briefing", "collapse briefing", "reload", "select thread after reload", "close", "reopen", "select thread"],
               "scope": "functional native goal answers and collapsed work, real inline briefing, one continuation notice, Trace identity, reload and reopen; runner verifies cleanup",
               "functional_budget_seconds": context["timeout"],
               "elapsed_seconds": time.monotonic() - deadline.started}
    control(context, deadline, "close")
    eventually(context, deadline, lambda: smoke.runner.process_argv(process.pid) is None, "reopened desktop exit")
    receipt["reopened_desktop_exit_code"] = process.wait(timeout=deadline.remaining("reopened desktop reap"))
    assert receipt["reopened_desktop_exit_code"] == 0, "reopened desktop did not close normally"
    save_private_json(context["evidence"] / "native-goal-resume-notice.json", receipt)
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
    # This feature scenario has its own explicit runner budget, not the daily
    # smoke's 45-second scenario cap. The runner still owns total/cleanup expiry.
    context["timeout"] = float(os.environ["ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS"])
    context.update(ax=args.ax.resolve(strict=True), session_ax=args.session_ax.resolve(strict=True),
                   controls=args.controls.resolve(strict=True), control_sha256=args.control_sha256)
    try:
        print(json.dumps(run(context)))
        return 0
    except (OSError, ValueError, RuntimeError, AssertionError, subprocess.SubprocessError) as error:
        receipt = {"passed": False, "failure": str(error), "case_id": context["case_id"]}
        if isinstance(error, (subprocess.CalledProcessError, subprocess.TimeoutExpired)):
            decode = lambda value: value.decode("utf-8", errors="replace") if isinstance(value, bytes) else value or ""
            receipt["helper"] = {"command": error.cmd, "returncode": getattr(error, "returncode", None),
                                 "timed_out": isinstance(error, subprocess.TimeoutExpired),
                                 "stdout": decode(error.output), "stderr": decode(error.stderr),
                                 "evidence_error": getattr(error, "evidence_error", None)}
        try:
            save_private_json(context["evidence"] / "native-goal-resume-notice.json", receipt)
        except OSError as evidence_error:
            receipt["receipt_evidence_error"] = str(evidence_error)
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
