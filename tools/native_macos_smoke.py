#!/usr/bin/env python3
"""Exercise one native offline turn inside native_macos_case.py's private case.

The scenario has its own 45-second ceiling, further capped by the runner's
remaining work budget. Readiness, UI, turn, Trace and screenshot share it.
"""
import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import plistlib
import re
import struct
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib


_spec = importlib.util.spec_from_file_location("native_macos_case", Path(__file__).with_name("native_macos_case.py"))
runner = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(runner)
_ready_spec = importlib.util.spec_from_file_location("native_macos_readiness", Path(__file__).with_name("native_macos_readiness.py"))
readiness = importlib.util.module_from_spec(_ready_spec)
_ready_spec.loader.exec_module(readiness)
AX_SOURCE = Path(__file__).with_name("native_macos_ax.m")
AX_FLAGS = ["-fobjc-arc", "-mmacosx-version-min=13.0", "-framework", "Foundation",
            "-framework", "AppKit", "-framework", "ApplicationServices", "-framework", "CoreGraphics"]


class Deadline:
    def __init__(self, seconds, clock=time.monotonic):
        if not math.isfinite(seconds) or seconds <= 0:
            raise ValueError("scenario timeout must be positive and finite")
        self.clock = clock
        self.started = clock()
        self.ends = self.started + seconds

    def remaining(self, stage):
        remaining = self.ends - self.clock()
        if remaining <= 0:
            raise TimeoutError("shared scenario deadline expired during " + stage)
        return remaining

    def pause(self, stage):
        time.sleep(min(.1, self.remaining(stage)))


def helper_manifest_path(binary):
    return Path(str(binary) + ".json")


def validate_helper(binary):
    binary = binary.resolve(strict=True)
    try:
        manifest = json.loads(helper_manifest_path(binary).read_text())
        if (not isinstance(manifest, dict) or manifest.get("schema_version") != 1 or manifest.get("source_name") != AX_SOURCE.name
                or manifest.get("source_sha256") != hashlib.sha256(AX_SOURCE.read_bytes()).hexdigest()
                or manifest.get("binary_sha256") != hashlib.sha256(binary.read_bytes()).hexdigest()
                or manifest.get("compiler") != "/usr/bin/xcrun clang" or manifest.get("flags") != AX_FLAGS
                or not os.access(binary, os.X_OK)):
            raise ValueError("helper source, binary or build flags differ from preparation")
    except (OSError, ValueError, TypeError) as error:
        raise RuntimeError("prepared native helper identity is invalid; prepare it before the case") from error
    return dict(manifest, binary=str(binary), current_source=str(AX_SOURCE.resolve()))


def prepare_helper(binary):
    """Compile only in preparation; runtime never installs or builds anything."""
    binary = binary.absolute()
    try:
        return validate_helper(binary)
    except (OSError, RuntimeError):
        pass
    binary.parent.mkdir(parents=True, exist_ok=True)
    source_bytes = AX_SOURCE.read_bytes()
    with tempfile.TemporaryDirectory(prefix=".native-ax-", dir=binary.parent) as temporary:
        source = Path(temporary) / AX_SOURCE.name
        source.write_bytes(source_bytes)
        candidate = Path(temporary) / "native-macos-ax"
        subprocess.run(["/usr/bin/xcrun", "clang", *AX_FLAGS, str(source), "-o", str(candidate)],
                       check=True, timeout=120)
        if AX_SOURCE.read_bytes() != source_bytes:
            raise RuntimeError("native helper source changed during preparation; candidate was not published")
        candidate.chmod(0o755)
        receipt = dict(schema_version=1, source_name=AX_SOURCE.name,
                       source_sha256=hashlib.sha256(source_bytes).hexdigest(),
                       binary_sha256=hashlib.sha256(candidate.read_bytes()).hexdigest(),
                       compiler="/usr/bin/xcrun clang", flags=AX_FLAGS)
        receipt_path = helper_manifest_path(candidate)
        runner.save_json(receipt_path, receipt)
        os.replace(candidate, binary)
        os.replace(receipt_path, helper_manifest_path(binary))
    return validate_helper(binary)


def readiness_blockers(row, case_id):
    if (not isinstance(row, dict) or type(row.get("schema_version")) is not int
            or row["schema_version"] != 1 or row.get("case_id") != case_id
            or type(row.get("uid")) is not int or row["uid"] != os.getuid()
            or "locked" not in row
            or any(type(row.get(key)) is not bool for key in
                   ("ax_trusted", "screen_capture_allowed", "lock_key_present"))
            or (row["lock_key_present"] and type(row.get("locked")) is not bool)
            or (not row["lock_key_present"] and row.get("locked") is not None)):
        raise ValueError("native preflight has malformed or mismatched current observations")
    blockers = []
    if not row["ax_trusted"]:
        blockers.append("current_invocation_accessibility_unavailable")
    if not row["screen_capture_allowed"]:
        blockers.append("current_invocation_screen_capture_unavailable")
    if (type(row.get("session_uid")) is not int or row["session_uid"] != row["uid"]
            or row.get("on_console") is not True or row.get("login_done") is not True):
        blockers.append("current_console_login_unready")
    if row.get("locked") is True:
        blockers.append("current_guest_locked")
    return blockers


def save_preflight(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n")


def case_preflight(binary, receipt, evidence, deadline):
    """Fail this invocation before copying/launching; no observations are reused."""
    result = dict(scope="current_invocation", status="failed", case_id=receipt["case_id"],
                  native_behavior_accepted=False)
    receipt["preflight"] = result
    try:
        identity = validate_helper(binary)
        result["helper"] = identity
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise runner.CaseBudgetExpired("native case work budget exhausted before readiness probe")
        command = [identity["binary"], "--preflight", receipt["case_id"]]
        result.update(command=command, timeout_seconds=min(5, remaining))
        probe_started = time.monotonic()
        try:
            probe = subprocess.run(command, capture_output=True, text=True, timeout=result["timeout_seconds"])
        finally:
            result["elapsed_seconds"] = time.monotonic() - probe_started
        result.update(returncode=probe.returncode, stdout=probe.stdout, stderr=probe.stderr)
        if probe.returncode:
            raise RuntimeError("current invocation native readiness probe failed")
        row = json.loads(probe.stdout)
        blockers = readiness_blockers(row, receipt["case_id"])
        result.update(observation=row, blockers=blockers,
                      login_lock_state="unknown" if row["locked"] is None else "locked" if row["locked"] else "unlocked")
        if blockers:
            result["status"] = "blocked"
            raise RuntimeError("native smoke prerequisites unavailable: " + ", ".join(blockers))
        # An absent SDK lock flag stays unknown. Only the later real window and
        # behavior assertions can accept the case, including after session changes.
        result["status"] = "ready_for_attempt"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
        receipt["failure_kind"] = "native_preflight"
        if isinstance(error, (subprocess.TimeoutExpired, runner.CaseBudgetExpired)):
            raise
        raise RuntimeError(str(error) + "; inspect native-preflight.json for this invocation") from error
    finally:
        primary_error = sys.exc_info()[1]
        try:
            save_preflight(evidence / "native-preflight.json", result)
        except (OSError, ValueError, TypeError) as write_error:
            receipt["preflight_evidence_error"] = str(write_error)
            if primary_error is None:
                result.update(status="failed", failure="native preflight evidence could not be saved")
                receipt["failure_kind"] = "native_preflight"
                raise RuntimeError(result["failure"]) from write_error


def case_context(environment):
    context = {key: Path(environment[variable]).resolve(strict=True) for key, variable in (
        ("data", "ZWAI_NATIVE_DATA"), ("app", "ZWAI_NATIVE_APP"),
        ("workspace", "ZWAI_NATIVE_WORKSPACE"), ("evidence", "ZWAI_NATIVE_EVIDENCE"), ("home", "HOME"))}
    context["case_id"] = environment["ZWAI_NATIVE_CASE_ID"]
    context["app_pid"] = int(environment["ZWAI_NATIVE_APP_PID"])
    seconds = float(environment["ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS"])
    if not math.isfinite(seconds) or seconds <= 0 or context["app_pid"] <= 0 or not context["case_id"]:
        raise ValueError("invalid runner case identity or remaining budget")
    root = context["data"].parent
    if (any(context[key].parent != root for key in ("app", "workspace", "home"))
            or Path(environment["ZWAI_HOME"]).resolve(strict=True) != context["data"]
            or context["evidence"].is_relative_to(root)):
        raise ValueError("scenario requires the runner's private sibling paths and external evidence")
    context["timeout"] = min(45, seconds)
    return context


def assert_process(context, pid, kind):
    argv, marker = runner.process_identity(pid)
    allowed = [context["app"], context["home"] / "Library/Caches/zwai/zwai.app"]
    if (marker != context["case_id"] or not argv or not any(
            Path(argv[0]).is_relative_to(bundle / "Contents/MacOS") for bundle in allowed)
            or (kind == "engine" and not runner.owns_argv(argv, Path("/no-bundle"), context["data"]))):
        raise readiness.Failure("native_identity_refused", "engine" if kind == "engine" else "window",
                                kind + " process identity does not belong to this private native case")
    return Path(argv[0])


def engine_origin(value):
    parsed = urllib.parse.urlsplit(value)
    if (parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or not parsed.port
            or parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment):
        raise ValueError("engine origin must be an exact numeric loopback HTTP origin")
    return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, newurl):
        raise RuntimeError("private engine HTTP redirect refused")


def request_json(origin, path, deadline):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(origin + path, headers={"Accept": "application/json"})
    with opener.open(request, timeout=min(1, deadline.remaining("HTTP " + path))) as response:
        if response.status != 200:
            raise RuntimeError("unexpected private engine HTTP status " + str(response.status))
        value = json.load(response)
    if not isinstance(value, dict):
        raise RuntimeError("private engine returned a non-object JSON response")
    return value


def wait_engine(context, deadline):
    while True:
        deadline.remaining("engine readiness")
        try:
            record = json.loads((context["data"] / "engine.json").read_text())
        except FileNotFoundError:
            pending(context, deadline, "engine", "engine_lease_pending")
            continue
        if not isinstance(record, dict):
            raise readiness.Failure("native_identity_refused", "engine", "engine lease is not an identity object")
        origin = engine_origin(record["url"])
        if record.get("mock") is not True or type(record.get("pid")) is not int:
            raise readiness.Failure("native_identity_refused", "engine", "engine lease identity is not the case's offline engine")
        assert_process(context, record["pid"], "engine")
        try:
            metadata = request_json(origin, "/api/meta", deadline)
        except urllib.error.HTTPError as error:
            raise readiness.Failure("native_http_refused", "engine", "private engine HTTP refused readiness") from error
        except urllib.error.URLError as error:
            if not isinstance(error.reason, (ConnectionError, TimeoutError)):
                raise readiness.Failure("native_http_refused", "engine", "private engine HTTP observation failed") from error
            pending(context, deadline, "engine", "engine_listener_pending")
            continue
        except (ConnectionError, TimeoutError):
            pending(context, deadline, "engine", "engine_listener_pending")
            continue
        clients = metadata.get("clients", [])
        if (metadata.get("mock") is not True or metadata.get("data_dir") != str(context["data"])
                or not isinstance(clients, list) or any(not isinstance(row, dict) for row in clients)):
            raise readiness.Failure("native_identity_refused", "engine", "engine HTTP identity differs from the private offline lease")
        if not any(row.get("surface") == "desktop" and row.get("pid") == context["app_pid"] for row in clients):
            pending(context, deadline, "engine", "desktop_presence_pending")
            continue
        return origin, record


def pending(context, deadline, stage, reason):
    """A pending read never authorizes replaying an input or a failed case."""
    observation = dict(stage=stage, reason=reason, retryable=True, input_repeated=False,
                       elapsed_seconds=deadline.clock()-deadline.started,
                       shared_deadline_monotonic=deadline.ends)
    observations = context.setdefault("readiness_observations", [])
    observations.append(observation)
    readiness.save_json(context["evidence"] / "smoke-readiness.json", dict(scope="ordinary_startup", observations=observations))
    deadline.pause("native rendered result" if stage == "result" else "native composer" if stage == "window" else stage)


def observed_command(context, deadline, command, action, timeout, identity=None):
    """Persist each complete outcome before parsing; unknown outcomes refuse."""
    timeout = min(timeout, deadline.remaining(action))
    index = context.get("native_call_count", 0)+1
    context["native_call_count"] = index
    row = dict(action=action, argv=command, timeout_seconds=timeout,
               shared_deadline_monotonic=deadline.ends, started_monotonic=deadline.clock(), started_wall=time.time())
    row.update(identity or {})
    original = None
    result = None
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
        row.update(returncode=result.returncode, stdout=result.stdout, stderr=result.stderr,
                   streams=dict(stdout=readiness.stream_record(result.stdout), stderr=readiness.stream_record(result.stderr)))
        return result
    except BaseException as error:
        original = error
        row.update(exception=type(error).__name__, failure=str(error), returncode=None,
                   streams=dict(stdout=readiness.stream_record(getattr(error, "stdout", None)),
                                stderr=readiness.stream_record(getattr(error, "stderr", None))))
        row.update(readiness.failure_record(error, context.get("readiness_stage", "window")))
        raise
    finally:
        row["elapsed_seconds"] = deadline.clock()-row["started_monotonic"]
        # Try both destinations independently. Failure evidence must never be
        # replaced by a secondary storage exception, nor overwritten by a poll.
        write_error = None
        for path in (context["evidence"] / ("smoke-native-call-%04d.json" % index),
                     context["evidence"] / "smoke-native-last.json"):
            try:
                readiness.save_json(path, row)
            except (OSError, ValueError, TypeError) as error:
                context.setdefault("evidence_errors", []).append(dict(path=str(path), error=str(error)))
                if write_error is None:
                    write_error = error
        if original is None and write_error is not None:
            raise readiness.Failure("native_evidence_failed", action, "native command evidence could not be saved") from write_error
        if original is None and result is not None:
            result.native_observation = dict(row)


def native_snapshot(context, deadline, action="snapshot", message="", expected=""):
    if action != "snapshot":
        context["native_input_attempted"] = True
    executable = assert_process(context, context["app_pid"], "desktop")
    requested_bundle = executable.parent.parent.parent
    try:
        identity = validate_helper(context["ax"])
    except (OSError, RuntimeError) as error:
        raise readiness.Failure("native_source_refused", "window", "native helper source or binary identity is invalid") from error
    if context.get("ax_identity", identity)["binary_sha256"] != identity["binary_sha256"]:
        raise readiness.Failure("native_source_refused", "window", "native helper changed during the case")
    command = [str(context["ax"]), str(context["app_pid"]), action,
               context["case_id"], str(requested_bundle), context["origin"],
               message if action in ("fill", "send") else expected]
    result = observed_command(context, deadline, command, action, 8,
                              dict(app_pid=context["app_pid"], requested_bundle=str(requested_bundle),
                                   requested_origin=context["origin"]))
    if result.returncode:
        error = readiness.Failure("native_helper_refused", "window", "guest native Accessibility/window check failed: " + (result.stdout + result.stderr).strip()[:500])
        error.native_observation = dict(index=context["native_call_count"], argv=command,
                                        returncode=result.returncode, stdout=result.stdout, stderr=result.stderr,
                                        row=getattr(result, "native_observation", None))
        raise error
    row = json.loads(result.stdout)
    if (not isinstance(row, dict) or row.get("origin") != context["origin"] or row.get("pid") != context["app_pid"]
            or ("case_id" in row and row["case_id"] != context["case_id"])):
        raise readiness.Failure("native_identity_refused", "window", "native helper returned another origin or PID")
    try:
        same_bundle = Path(row.get("bundle")).resolve(strict=True) == requested_bundle.resolve(strict=True)
    except (OSError, ValueError, TypeError) as error:
        raise readiness.Failure("native_identity_refused", "window", "native helper returned a missing or invalid bundle identity") from error
    if not same_bundle:
        raise readiness.Failure("native_identity_refused", "window", "native helper returned another bundle")
    assert_web_origin(row, context["app_pid"], required=False)
    return row


def assert_web_origin(row, pid, required=True):
    origin = engine_origin(row.get("origin", ""))
    urls = row.get("web_urls", [])
    if not isinstance(urls, list) or (required and not urls):
        raise readiness.Failure("native_identity_refused", "window", "native window has no bound webview origin")
    for value in urls:
        if not isinstance(value, str):
            raise readiness.Failure("native_identity_refused", "window", "native webview origin is malformed")
        url = urllib.parse.urlsplit(value)
        query = urllib.parse.parse_qs(url.query)
        if (url.scheme + "://" + url.netloc != origin or query.get("shell") != ["desktop"]
                or query.get("pid") != [str(pid)] or url.path not in ("", "/")
                or url.username or url.password or url.fragment):
            raise readiness.Failure("native_identity_refused", "window", "native webview origin, shell or PID belongs to another client")


def assert_window(row, pid, require_result=False):
    rect = row.get("rect", [])
    if (row.get("pid") != pid or not row.get("title") or row.get("frontmost") is not True
            or row.get("has_web_area") is not True or row.get("textareas", 0) < 1
            or len(rect) != 4 or not all(isinstance(n, (int, float)) and math.isfinite(n) for n in rect)
            or rect[2] <= 0 or rect[3] <= 0
            or row.get("case_verified") is not True or row.get("window_count") != 1
            or type(row.get("window_id")) is not int or row["window_id"] <= 0
            or row.get("window_owner_pid") != pid or row.get("screen_capture_allowed") is not True
            or row.get("windows") != [dict(id=row["window_id"], owner_pid=pid)]
            or (require_result and row.get("expected_visible") is not True)):
        raise RuntimeError("native window lacks this case's rendered page or visible final result")
    assert_web_origin(row, pid)


def assert_draft(row, message):
    if (row.get("textareas") != 1 or row.get("area_values") != [message]
            or row.get("send_count") != 1 or row.get("send_enabled") is not True):
        raise RuntimeError("native draft is not exact or its unique Send button is not enabled")


def initial_window_snapshot(context, deadline):
    """Reacquire an incomplete initial observation, never replay an input."""
    if context.get("readiness_stage") != "window" or context.get("native_input_attempted"):
        raise ValueError("Initial observation must precede native input in the startup window stage")
    first = None
    while True:
        try:
            deadline.remaining("initial native observation")
            return native_snapshot(context, deadline)
        except readiness.Failure as error:
            record = getattr(error, "native_observation", None)
            if error.failure_kind != "native_helper_refused" or not isinstance(record, dict):
                raise
            try:
                index = record["index"]
                if type(index) is not int or index <= 0:
                    raise ValueError("Initial observation has no exact call index")
                path = context["evidence"] / ("smoke-native-call-%04d.json" % index)
                if path.is_symlink():
                    raise ValueError("Initial observation evidence is a symlink")
                raw = path.read_bytes()
                saved = json.loads(raw)
                body = json.loads(record["stdout"])
                argv = record["argv"]
                eligible = (not context.get("evidence_errors") and saved == record["row"]
                    and index == context["native_call_count"]
                    and len(argv) == 7 and argv[:4] == [str(context["ax"]), str(context["app_pid"]), "snapshot", context["case_id"]]
                    and argv[5:] == [context["origin"], ""] and saved.get("argv") == argv
                    and saved.get("action") == "snapshot" and saved.get("app_pid") == context["app_pid"]
                    and saved.get("requested_bundle") == argv[4] and saved.get("requested_origin") == argv[5]
                    and saved.get("shared_deadline_monotonic") == deadline.ends
                    and type(saved.get("returncode")) is int and saved["returncode"] == record["returncode"] == 2
                    and saved.get("stdout") == record["stdout"] and saved.get("stderr") == record["stderr"]
                    and saved.get("streams") == dict(stdout=readiness.stream_record(record["stdout"]),
                                                     stderr=readiness.stream_record(record["stderr"]))
                    and "exception" not in saved and isinstance(body, dict)
                    and body.get("ax_read_failed") is True and type(body.get("ax_error")) is int
                    and body["ax_error"] == -25204 and body.get("ax_attribute") == "AXRole"
                    and body.get("ax_read_reason") == "api_error"
                    and ("retryable" not in body or body["retryable"] is True)
                    and all(key not in body or body[key] is False
                            for key in ("ready", "truncated", "filled", "submitted", "press_attempted")))
                if not eligible:
                    raise error
                first = first or error
                observation = dict(failed_call=index, failed_call_path=str(path), failed_call_sha256=hashlib.sha256(raw).hexdigest(),
                                   partial_graph_discarded=True, input_repeated=False,
                                   activation_policy="unchanged helper policy; at most one activation per invocation",
                                   shared_deadline_monotonic=deadline.ends)
                readiness.save_json(context["evidence"] / ("smoke-incomplete-initial-%04d.json" % index), observation)
            except (OSError, ValueError, TypeError, KeyError) as secondary:
                error.__notes__ = [*getattr(error, "__notes__", []), "initial observation evidence refused: " + type(secondary).__name__]
                raise error from secondary
            try:
                pending(context, deadline, "window", "initial_ax_role_messaging_incomplete")
            except TimeoutError as exhausted:
                raise exhausted from first
            except (OSError, ValueError, TypeError) as secondary:
                error.__notes__ = [*getattr(error, "__notes__", []), "initial observation evidence refused: " + type(secondary).__name__]
                raise error from secondary
        except TimeoutError as exhausted:
            if first is not None:
                raise exhausted from first
            raise


def wait_window(context, deadline, expected="", *, initial=False):
    if initial and expected:
        raise ValueError("Initial observation cannot scroll to an expected result")
    while True:
        deadline.remaining("native rendered result" if expected else "native composer")
        row = initial_window_snapshot(context, deadline) if initial else native_snapshot(context, deadline, expected=expected)
        reason = readiness.window_pending(row, context["app_pid"])
        if reason is None and (not expected or row.get("expected_visible")):
            assert_window(row, context["app_pid"], require_result=bool(expected))
            return row
        if reason is None:
            assert_window(row, context["app_pid"])
        pending(context, deadline, "result" if expected else "window", reason or "rendered_result_pending")


def submit_native(context, deadline, message):
    filled = native_snapshot(context, deadline, action="fill", message=message)
    if filled.get("filled") is not True:
        raise RuntimeError("native composer did not accept the fill action")
    while True:
        draft = wait_window(context, deadline)
        try:
            assert_draft(draft, message)
            break
        except RuntimeError:
            deadline.pause("exact native draft and enabled Send")
    runner.save_json(context["evidence"] / "smoke-window-draft.json", draft)
    sent = native_snapshot(context, deadline, action="send", message=message)
    if sent.get("submitted") is not True:
        raise RuntimeError("native composer did not accept the single Send action")


def wait_trace(origin, context, message, deadline):
    turn, thread = None, None
    while True:
        deadline.remaining("offline turn and Trace")
        if turn is None:
            matches = []
            for row in request_json(origin, "/api/threads", deadline).get("threads", []):
                turns = request_json(origin, "/api/threads/" + row["id"] + "/turns", deadline)
                matches.extend((row["id"], item["id"]) for item in turns.get("turns", [])
                               if item.get("user_text") == message)
            if len(matches) > 1:
                raise RuntimeError("one native send created duplicate smoke turns")
            if matches:
                thread, turn = matches[0]
        if turn is not None:
            trace = request_json(origin, "/api/trace/" + turn, deadline)
            runner.save_json(context["evidence"] / "smoke-trace.json", trace)
            status = trace.get("turn", {}).get("status")
            if status in ("error", "cancelled"):
                raise RuntimeError("native offline turn terminated with status " + status)
            if status == "done":
                return trace, assert_trace(trace, thread, turn, message)
        deadline.pause("offline turn and Trace")


def assert_trace(trace, thread, turn, message):
    row, events, calls = trace.get("turn", {}), trace.get("events", []), trace.get("llm_calls", [])
    if (row.get("id") != turn or row.get("thread_id") != thread or row.get("status") != "done"
            or row.get("user_text") != message or row.get("error") or not events
            or any(e.get("turn_id") != turn or e.get("thread_id") != thread or e.get("err") for e in events)):
        raise RuntimeError("Trace identity/input/terminal evidence is inconsistent")
    spawned = {e.get("agent_id") for e in events if e.get("kind") == "spawned" and e.get("agent_id") != "manager"}
    finished = {e.get("agent_id") for e in events if e.get("kind") == "finished"}
    call_ids = {e.get("tool_call_id") for e in events if e.get("kind") == "tool_call"}
    result_ids = {e.get("tool_call_id") for e in events if e.get("kind") == "tool_result"}
    if (not spawned or not spawned <= finished or not call_ids or None in call_ids
            or not call_ids <= result_ids or not any(e.get("kind") == "done" and e.get("agent_id") == "manager" for e in events)
            or not any(e.get("kind") == "user_message" and e.get("text") == message for e in events)
            or not calls or any(c.get("turn_id") != turn or c.get("err") for c in calls)
            or not spawned <= {c.get("agent_id") for c in calls}):
        raise RuntimeError("Trace lacks completed workers, paired tools, or successful model-call receipts")
    worker_writes(trace)
    # Derive the UI assertion from the actual final answer, excluding echoed input.
    for line in row.get("final", "").splitlines():
        if line.strip().startswith("```"):
            continue
        fragment = re.sub(r"^[#>*\s\d.\-]+", "", line).strip()
        fragment = re.sub(r"\[([^]]+)\]\([^)]+\)", r"\1", fragment)
        fragment = re.sub(r"[*_`]", "", fragment)
        if len(fragment) >= 24 and message not in fragment and not fragment.startswith("```"):
            return " ".join(fragment.split())[:100].rsplit(" ", 1)[0]
    raise RuntimeError("Trace final answer contains no result distinct from the input")


def worker_writes(trace):
    events = trace["events"]
    workers = {e["agent_id"] for e in events if e.get("kind") == "spawned" and e.get("agent_id") != "manager"}
    results = {(e.get("agent_id"), e.get("tool_call_id")) for e in events if e.get("kind") == "tool_result"}
    writes = []
    for event in events:
        text = event.get("text", "")
        if event.get("kind") != "tool_call" or event.get("agent_id") not in workers or not text.startswith("write(") or not text.endswith(")"):
            continue
        try:
            arguments = json.loads(text[len("write("):-1])
            path, content = arguments["file_path"], arguments["content"]
            if not isinstance(path, str) or not isinstance(content, str) or not path or Path(path).is_absolute() or ".." in Path(path).parts:
                raise ValueError("invalid workspace write")
        except (KeyError, TypeError, ValueError) as error:
            raise RuntimeError("worker write Trace arguments are invalid") from error
        if (event["agent_id"], event.get("tool_call_id")) not in results:
            raise RuntimeError("worker write lacks its own matching tool result")
        writes.append(dict(agent_id=event["agent_id"], path=path, content=content))
    if not workers or workers != {item["agent_id"] for item in writes}:
        raise RuntimeError("each offline worker must have its own completed workspace write")
    return writes


def assert_workspace_outputs(origin, context, trace, deadline):
    files = request_json(origin, "/api/threads/" + trace["turn"]["thread_id"] + "/files", deadline)
    workspace = Path(files["workspace"]).resolve(strict=True)
    if not workspace.is_relative_to(context["data"]):
        raise RuntimeError("offline workspace does not belong to the private case data")
    outputs = []
    for item in worker_writes(trace):
        deadline.remaining("worker output bytes")
        try:
            path = (workspace / item["path"]).resolve(strict=True)
            if not path.is_relative_to(workspace):
                raise RuntimeError("worker write escaped its private workspace")
            raw = path.read_bytes()
        except FileNotFoundError as error:
            raise RuntimeError("worker write output is missing") from error
        if raw != item["content"].encode("utf-8"):
            raise RuntimeError("worker output bytes differ from the actual write tool arguments")
        outputs.append(dict(agent_id=item["agent_id"], path=item["path"], bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest()))
    runner.save_json(context["evidence"] / "smoke-workspace.json", dict(workspace=str(workspace), outputs=outputs))
    return outputs


def png_dimensions(path):
    raw = path.read_bytes()
    if raw[:8] != b"\x89PNG\r\n\x1a\n":
        raise RuntimeError("native screenshot is not PNG")
    offset, header, data, ended = 8, None, bytearray(), False
    while offset + 12 <= len(raw):
        length = struct.unpack(">I", raw[offset:offset+4])[0]
        kind, payload = raw[offset+4:offset+8], raw[offset+8:offset+8+length]
        if offset+12+length > len(raw) or zlib.crc32(kind+payload) != struct.unpack(">I", raw[offset+8+length:offset+12+length])[0]:
            raise RuntimeError("native screenshot has corrupt PNG chunks")
        if kind == b"IHDR":
            if len(payload) != 13:
                raise RuntimeError("native screenshot has an invalid image header")
            header = struct.unpack(">IIBBBBB", payload)
        if kind == b"IDAT": data.extend(payload)
        if kind == b"IEND": ended = True
        offset += length+12
    if not header or not ended or not data or min(header[:2]) <= 0:
        raise RuntimeError("native screenshot lacks a complete image")
    try:
        decoded = zlib.decompress(data)
    except zlib.error as error:
        raise RuntimeError("native screenshot pixels cannot be decoded") from error
    width, height, depth, color, compression, filtering, interlace = header
    channels = {0: 1, 2: 3, 4: 2, 6: 4}.get(color)
    depths = (1, 2, 4, 8, 16) if color == 0 else (8, 16)
    if not channels or depth not in depths or compression or filtering or interlace:
        raise RuntimeError("native screenshot uses an unsupported PNG pixel encoding")
    stride = (width * channels * depth + 7) // 8
    if len(decoded) != (stride+1)*height or any(decoded[row*(stride+1)] > 4 for row in range(height)):
        raise RuntimeError("native screenshot has incomplete or invalid pixel scanlines")
    return list(header[:2])


def capture_window(context, deadline, window, name):
    """Observe fresh window pixels; the caller owns the unchanged deadline."""
    assert_process(context, context["app_pid"], "desktop")
    image = context["evidence"] / name
    descriptor = os.open(image, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    os.close(descriptor)
    command = ["/usr/sbin/screencapture", "-x", "-l", str(window["window_id"]), str(image)]
    result = observed_command(context, deadline, command, "window_pixels", 8)
    if result.returncode:
        raise readiness.Failure("native_pixels_failed", "pixels", "native window screenshot failed: " + result.stderr[:500])
    dimensions = png_dimensions(image)
    observation = readiness.pixel_observation(image)
    observation.update(path=str(image), sha256=hashlib.sha256(image.read_bytes()).hexdigest(),
                       window_id=window["window_id"], window_owner_pid=window["window_owner_pid"])
    if dimensions != observation["dimensions"] or min(dimensions) < 100:
        raise readiness.Failure("native_pixels_invalid", "pixels", "native screenshot is not a full window image")
    deadline.remaining("ordinary startup pixels")
    return observation


def wait_initial_ready(context, deadline):
    """Ordinary startup uses engine, AX and pixels, never a recovery journal.

    This says nothing about a replacement's automatic readiness or cold-start
    duration. Those assertions belong to the explicit recovery scenario.
    """
    context["readiness_stage"] = "engine"
    origin, lease = wait_engine(context, deadline)
    context["origin"] = origin
    timings = context.setdefault("readiness_timings", {})
    timings["engine_ready_seconds"] = deadline.clock()-deadline.started
    attempt = 0
    while True:
        context["readiness_stage"] = "window"
        window = wait_window(context, deadline, initial=True)
        timings["window_ready_seconds"] = deadline.clock()-deadline.started
        context["readiness_stage"] = "pixels"
        attempt += 1
        pixels = capture_window(context, deadline, window, "smoke-initial-%04d.png" % attempt)
        readiness.save_json(context["evidence"] / "smoke-initial.json",
                            dict(scope="ordinary_startup", window=window, pixels=pixels,
                                 shared_deadline_monotonic=deadline.ends, recovery_accepted=False))
        if pixels["content_visible"]:
            timings["pixels_ready_seconds"] = deadline.clock()-deadline.started
            context["readiness_stage"] = "scenario"
            return origin, lease, window
        pending(context, deadline, "pixels", "ordinary_window_pixels_pending")


def run_smoke(context):
    deadline = Deadline(context["timeout"])
    runner.require_guest()
    context["ax_identity"] = validate_helper(context["ax"])
    runner.save_json(context["evidence"] / "smoke-helper.json", context["ax_identity"])
    phases = {}
    executable = assert_process(context, context["app_pid"], "desktop")
    binary_hash = hashlib.sha256()
    with executable.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024*1024), b""):
            binary_hash.update(chunk)
            deadline.remaining("candidate binary provenance")
    binary_sha256 = binary_hash.hexdigest()
    with (executable.parent.parent / "Info.plist").open("rb") as stream:
        bundle = plistlib.load(stream)
    origin, lease, before = wait_initial_ready(context, deadline)
    phases.update(context["readiness_timings"])
    phases["initial_ready_seconds"] = deadline.clock() - deadline.started
    runner.save_json(context["evidence"] / "smoke-window-before.json", before)
    message = "Inspect this workspace for case " + context["case_id"] + " and report findings."
    if request_json(origin, "/api/threads", deadline).get("threads") != []:
        raise RuntimeError("native smoke case must start without existing conversations")
    submit_native(context, deadline, message)
    phases["native_submitted_seconds"] = deadline.clock() - deadline.started
    trace, fragment = wait_trace(origin, context, message, deadline)
    outputs = assert_workspace_outputs(origin, context, trace, deadline)
    phases["turn_done_seconds"] = deadline.clock() - deadline.started
    final = wait_window(context, deadline, expected=fragment)
    runner.save_json(context["evidence"] / "smoke-window-final.json", final)
    image = context["evidence"] / "smoke-window.png"
    result = subprocess.run(["/usr/sbin/screencapture", "-x", "-l", str(final["window_id"]), str(image)],
                            capture_output=True, text=True, timeout=deadline.remaining("native window screenshot"))
    if result.returncode:
        raise RuntimeError("native window screenshot failed: " + result.stderr.strip()[:500])
    dimensions = png_dimensions(image)
    deadline.remaining("final native evidence")
    phases["scenario_seconds"] = deadline.clock() - deadline.started
    return dict(passed=True, case_id=context["case_id"], app_pid=context["app_pid"], engine_pid=lease["pid"],
                executable=str(executable), executable_sha256=binary_sha256,
                bundle_version=bundle.get("CFBundleShortVersionString"), bundle_build=bundle.get("CFBundleVersion"),
                source_sha="unknown; requires external candidate provenance", thread_id=trace["turn"]["thread_id"],
                turn_id=trace["turn"]["id"], result_fragment=fragment, screenshot=str(image),
                screenshot_dimensions=dimensions, screenshot_window_id=final["window_id"],
                screenshot_owner_pid=final["window_owner_pid"], screenshot_rect_points=final["rect"],
                native_helper=context["ax_identity"], worker_outputs=outputs, phases=phases)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--prepare-ax", type=Path, help="prepare/reuse the tracked direct AX helper before starting a case")
    mode.add_argument("--ax", type=Path, help="prepared direct AX helper, with its adjacent .json identity receipt")
    parser.add_argument("--case", action="store_true", help="run the isolated smoke lifecycle with a fresh pre-copy readiness probe")
    parser.add_argument("--app", type=Path)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--timeout", type=float, default=50)
    parser.add_argument("--budget", type=float, default=60)
    args = parser.parse_args(argv)
    if args.case:
        if args.ax is None or args.app is None or args.evidence is None:
            parser.error("--case requires --ax, --app and --evidence")
        binary = args.ax.absolute()
        return runner.main(["--app", str(args.app), "--evidence", str(args.evidence),
                            "--timeout", str(args.timeout), "--budget", str(args.budget), "--",
                            sys.executable, str(Path(__file__).resolve()), "--ax", str(binary)],
                           preflight=lambda lock, receipt, evidence, deadline:
                           case_preflight(binary, receipt, evidence, deadline))
    if args.app is not None or args.evidence is not None:
        parser.error("--app and --evidence require --case")
    context = None
    try:
        if args.prepare_ax is not None:
            print(json.dumps(prepare_helper(args.prepare_ax)))
            return 0
        context = case_context(os.environ)
        context["ax"] = args.ax.resolve(strict=True)
        receipt = run_smoke(context)
        runner.save_json(context["evidence"] / "smoke.json", receipt)
        print(json.dumps(receipt))
        return 0
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        receipt = dict(passed=False, failure=str(error),
                       **readiness.failure_record(error, context.get("readiness_stage", "scenario") if context else "preparation"))
        if context is not None:
            receipt["evidence_errors"] = context.get("evidence_errors", [])
            receipt["phases"] = context.get("readiness_timings", {})
            try:
                readiness.save_json(context["evidence"] / "smoke.json", receipt)
            except (OSError, ValueError, TypeError) as write_error:
                receipt["terminal_evidence_error"] = str(write_error)
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
