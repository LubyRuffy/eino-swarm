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
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib


_spec = importlib.util.spec_from_file_location("native_macos_case", Path(__file__).with_name("native_macos_case.py"))
runner = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(runner)


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
        raise RuntimeError(kind + " process identity does not belong to this private native case")
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
            origin = engine_origin(record["url"])
            if record.get("mock") is not True or type(record.get("pid")) is not int:
                raise RuntimeError("engine lease identity is not the case's offline engine")
            assert_process(context, record["pid"], "engine")
            metadata = request_json(origin, "/api/meta", deadline)
            if metadata.get("mock") is not True or metadata.get("data_dir") != str(context["data"]):
                raise RuntimeError("engine HTTP identity differs from the private offline lease")
            if not any(row.get("surface") == "desktop" and row.get("pid") == context["app_pid"]
                       for row in metadata.get("clients", [])):
                deadline.pause("desktop presence")
                continue
            return origin, record
        except (FileNotFoundError, json.JSONDecodeError, urllib.error.URLError, ConnectionError, TimeoutError):
            deadline.pause("engine readiness")


# System Events accesses only the exact case-owned PID, after require_guest.
# A rendered WKWebView and composer are required before sending real key events.
NATIVE_SCRIPT = r'''
function run(argv) {
  const pid=Number(argv[0]), action=argv[1], message=argv[2], expected=argv[3];
  const se=Application('System Events');
  const matches=se.applicationProcesses.whose({unixId:pid})();
  if (matches.length===0) return JSON.stringify({ready:false});
  const p=matches[0];
  p.frontmost=true;
  const windows=p.windows();
  if (windows.length===0) return JSON.stringify({ready:false});
  const w=windows[0];
  try { w.actions.byName('AXRaise').perform(); } catch (_) {}
  if (w.sheets().length!==0) throw Error('case window has a modal sheet');
  const pos=w.position(), size=w.size(), texts=[], roles={}, all=w.entireContents();
  let area=null, expectedVisible=false;
  for (let i=0;i<all.length;i++) {
    const e=all[i]; let role='';
    try {role=e.role();} catch (_) {continue;}
    roles[role]=(roles[role]||0)+1;
    if (role==='AXTextArea' && !area) area=e;
    if (!['AXStaticText','AXHeading','AXTextArea','AXButton','AXLink'].includes(role)) continue;
    let text='';
    try {text=String(e.value()||'');} catch (_) {}
    if (!text) try {text=String(e.name()||'');} catch (_) {}
    if (text) texts.push(text);
    if (expected && text.replace(/\s+/g,' ').includes(expected)) {
      try { e.actions.byName('AXScrollToVisible').perform(); } catch (_) {}
      try {
        const ep=e.position(), es=e.size();
        expectedVisible=expectedVisible || (es[0]>0 && es[1]>0 && ep[0]<pos[0]+size[0] &&
          ep[1]<pos[1]+size[1] && ep[0]+es[0]>pos[0] && ep[1]+es[1]>pos[1]);
      } catch (_) {}
    }
  }
  const ready=!!roles.AXWebArea && !!area;
  if (action==='submit' && ready) {
    if (!p.frontmost()) throw Error('case window did not take keyboard focus');
    area.attributes.byName('AXFocused').value=true;
    se.keystroke(message);
    se.keyCode(36);
  }
  return JSON.stringify({ready:ready,pid:p.unixId(),title:w.name(),frontmost:p.frontmost(),
    rect:[pos[0],pos[1],size[0],size[1]],has_web_area:!!roles.AXWebArea,
    textareas:roles.AXTextArea||0,expected_visible:expectedVisible,texts:texts,roles:roles,
    submitted:action==='submit'&&ready});
}
'''


def native_snapshot(context, deadline, action="snapshot", message="", expected=""):
    assert_process(context, context["app_pid"], "desktop")
    result = subprocess.run(["/usr/bin/osascript", "-l", "JavaScript", "-e", NATIVE_SCRIPT,
                             str(context["app_pid"]), action, message, expected],
                            capture_output=True, text=True,
                            timeout=min(4, deadline.remaining("native accessibility")))
    if result.returncode:
        raise RuntimeError("guest native Accessibility/window check failed: " + result.stderr.strip()[:500])
    return json.loads(result.stdout)


def assert_window(row, pid, require_result=False):
    rect = row.get("rect", [])
    if (row.get("pid") != pid or not row.get("title") or row.get("frontmost") is not True
            or row.get("has_web_area") is not True or row.get("textareas", 0) < 1
            or len(rect) != 4 or not all(isinstance(n, (int, float)) and math.isfinite(n) for n in rect)
            or rect[2] <= 0 or rect[3] <= 0
            or (require_result and row.get("expected_visible") is not True)):
        raise RuntimeError("native window lacks this case's rendered page or visible final result")


def wait_window(context, deadline, expected=""):
    while True:
        deadline.remaining("native rendered result" if expected else "native composer")
        row = native_snapshot(context, deadline, expected=expected)
        if row.get("ready") and (not expected or row.get("expected_visible")):
            assert_window(row, context["app_pid"], require_result=bool(expected))
            return row
        deadline.pause("native rendered result" if expected else "native composer")


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


def run_smoke(context):
    deadline = Deadline(context["timeout"])
    runner.require_guest()
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
    origin, lease = wait_engine(context, deadline)
    phases["engine_ready_seconds"] = deadline.clock() - deadline.started
    before = wait_window(context, deadline)
    runner.save_json(context["evidence"] / "smoke-window-before.json", before)
    message = "Inspect this workspace and report findings. native-smoke-" + context["case_id"]
    submitted = native_snapshot(context, deadline, action="submit", message=message)
    if not submitted.get("submitted"):
        raise RuntimeError("native composer did not accept the smoke submission")
    phases["native_submitted_seconds"] = deadline.clock() - deadline.started
    trace, fragment = wait_trace(origin, context, message, deadline)
    outputs = assert_workspace_outputs(origin, context, trace, deadline)
    phases["turn_done_seconds"] = deadline.clock() - deadline.started
    final = wait_window(context, deadline, expected=fragment)
    runner.save_json(context["evidence"] / "smoke-window-final.json", final)
    image = context["evidence"] / "smoke-window.png"
    result = subprocess.run(["/usr/sbin/screencapture", "-x", "-R" + ",".join(str(round(n)) for n in final["rect"]), str(image)],
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
                screenshot_dimensions=dimensions, screenshot_rect_points=final["rect"], worker_outputs=outputs, phases=phases)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args(argv)
    context = None
    try:
        context = case_context(os.environ)
        receipt = run_smoke(context)
        runner.save_json(context["evidence"] / "smoke.json", receipt)
        print(json.dumps(receipt))
        return 0
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        receipt = dict(passed=False, failure=str(error))
        if context is not None:
            runner.save_json(context["evidence"] / "smoke.json", receipt)
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
