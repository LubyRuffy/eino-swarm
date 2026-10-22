#!/usr/bin/env python3
"""Observe interrupted and resumed reasoning in one isolated ci-macOS window."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import time
import urllib.request

from native_generation_interruption_fixture import Fixture


def eventually(probe, seconds=35):
    deadline, last = time.monotonic() + seconds, None
    while time.monotonic() < deadline:
        try:
            value = probe()
            if value:
                return value
        except (OSError, ValueError, AssertionError, KeyError, subprocess.SubprocessError) as error:
            last = str(error)
        time.sleep(.15)
    raise RuntimeError("native observation deadline: " + str(last))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", required=True, type=Path)
    parser.add_argument("--ax", required=True, type=Path)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location("native_case_runtime", args.runner)
    runtime = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(runtime)
    runtime.require_guest()
    evidence = Path(os.environ["ZWAI_NATIVE_EVIDENCE"])
    app, data = Path(os.environ["ZWAI_NATIVE_APP"]), Path(os.environ["ZWAI_NATIVE_DATA"])
    case_id = os.environ["ZWAI_NATIVE_CASE_ID"]
    fixture = Fixture()
    display_hold = subprocess.Popen(["/usr/bin/caffeinate", "-d", "-u", "-t", "180"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def save(name, row):
        (evidence / name).write_text(json.dumps(row, ensure_ascii=False, indent=2) + "\n")

    def lease_ready():
        row = json.loads((data / "engine.json").read_text())
        assert row["url"].startswith("http://127.0.0.1:")
        return row

    def api(origin, path, body=None, method=None):
        raw = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(origin + path, data=raw,
            headers={"Content-Type": "application/json"}, method=method)
        with urllib.request.urlopen(request, timeout=3) as response:
            return json.load(response)

    def desktop_pid():
        rows = subprocess.check_output(["/bin/ps", "-axo", "pid=,uid="], text=True)
        found = []
        for row in rows.splitlines():
            pid, uid = map(int, row.split())
            if uid == os.getuid() and pid != os.getpid():
                argv, marker = runtime.process_identity(pid)
                if marker == case_id and argv and "desktop" in argv and runtime.owns_argv(argv, app, data):
                    found.append(pid)
        assert len(found) == 1
        return found[0]

    def ax(pid, action, value=None):
        command = [str(args.ax), str(pid), action] + ([value] if value is not None else [])
        result = subprocess.run(command, capture_output=True, text=True, timeout=12)
        if result.returncode:
            raise AssertionError("native AX refused: " + result.stdout + result.stderr)
        return json.loads(result.stdout)

    def capture(pid, name, required):
        row = ax(pid, "inspect")
        assert len(row["windows"]) == 1
        body = json.dumps(row, ensure_ascii=False)
        for value in required:
            assert value in body, "native screen missing " + value
        assert all(label not in body for label in ("View failure reason", "Failure reason", "失败原因"))
        save(name + ".ax.json", row)
        image = evidence / (name + ".png")
        subprocess.run(["/usr/sbin/screencapture", "-x", "-l", str(row["windows"][0]["id"]), str(image)], check=True, capture_output=True, timeout=10)
        dimensions = subprocess.check_output(["/usr/bin/sips", "-g", "pixelWidth", "-g", "pixelHeight", str(image)], text=True)
        assert image.stat().st_size > 1000 and "pixelWidth:" in dimensions
        save(name + ".image.json", {"dimensions": dimensions, "sha256": hashlib.sha256(image.read_bytes()).hexdigest(), "window_id": row["windows"][0]["id"], "pid": pid})
        return row

    try:
        initial = eventually(lease_ready)
        origin = initial["url"].rstrip("/")
        meta = eventually(lambda: api(origin, "/api/meta"))
        assert Path(meta["data_dir"]).resolve() == data.resolve() and meta["mock"]
        initial_pid = eventually(desktop_pid)
        # Verify the control path before stopping this case's mock process.
        def initial_window():
            row = ax(initial_pid, "inspect")
            save("initial-window.latest.ax.json", row)
            assert len(row["windows"]) == 1
            assert any(node.get("AXRole") == "AXWebArea" for node in row["tree"])
            return row
        initial_window_row = eventually(initial_window)
        save("initial-window.ax.json", initial_window_row)
        chinese = any(node.get("AXTitle") == "对话" for node in initial_window_row["tree"])
        labels = ({"conversations": "对话", "new": "新对话", "send": "发送", "agents": "代理",
                   "thinking": "思考中", "thought": "思考", "cause": "模型输出额度耗尽", "recovered": "已恢复执行"}
                  if chinese else {"conversations": "Conversations", "new": "New conversation", "send": "Send", "agents": "Agents",
                   "thinking": "Thinking", "thought": "Thought", "cause": "model output budget exhausted", "recovered": "Execution resumed"})
        settings = api(origin, "/api/settings")["settings"]
        api(origin, "/api/settings", {
            "models": {"default": "fixture-provider", "providers": [{"id": "fixture-provider", "label": "Offline native generation regression", "base_url": fixture.url, "api_key": "", "model": "fixture-model", "context_window": 128000}]},
            "swarm": {**settings["swarm"], "auto_title": False},
            "memory": {**settings["memory"], "auto_review": False},
        }, "PUT")
        ax(initial_pid, "close")
        eventually(lambda: runtime.process_argv(initial_pid) is None, 10)
        argv, marker = runtime.process_identity(initial["pid"])
        assert marker == case_id and runtime.owns_argv(argv, app, data)
        os.kill(initial["pid"], signal.SIGTERM)
        eventually(lambda: runtime.process_argv(initial["pid"]) is None, 15)
        executable = app / "Contents/MacOS/zwai"
        with (evidence / "fixture-desktop.stdout.log").open("wb") as out, (evidence / "fixture-desktop.stderr.log").open("wb") as err:
            client = subprocess.Popen([str(executable), "desktop", "--data-dir", str(data)], stdout=out, stderr=err, start_new_session=True)
        current = eventually(lambda: (row if (row := lease_ready())["pid"] != initial["pid"] else None))
        origin = current["url"].rstrip("/")
        meta = eventually(lambda: api(origin, "/api/meta"))
        assert not meta["mock"] and Path(meta["data_dir"]).resolve() == data.resolve()
        pid = eventually(desktop_pid)
        eventually(lambda: ax(pid, "press", labels["conversations"]))
        ax(pid, "press", labels["new"])
        thread = eventually(lambda: api(origin, "/api/threads")["threads"])[0]
        prompt = "Inspect the assigned inputs."
        ax(pid, "fill", prompt)
        ax(pid, "press", labels["send"])
        turn = eventually(lambda: api(origin, "/api/threads/" + thread["id"] + "/turns")["turns"])[0]
        eventually(lambda: fixture.workers == 1 and fixture.worker_id)
        ax(pid, "press", labels["agents"])
        eventually(lambda: ax(pid, "press", "verifier"))
        eventually(lambda: capture(pid, "01-thinking", [labels["thinking"]]))
        fixture.interrupt.set()
        eventually(lambda: fixture.workers == 2)
        def recovered_trace():
            trace = api(origin, "/api/trace/" + turn["id"])
            save("trace.latest.json", trace)
            events = trace["events"]
            interrupted = [e for e in events if e["kind"] == "generation_interrupted"]
            recovered = [e for e in events if e["kind"] == "generation_recovered"]
            assert len(interrupted) == 1 and len(recovered) == 1
            assert interrupted[0]["agent_id"] == recovered[0]["agent_id"] == fixture.worker_id
            assert interrupted[0]["message_id"] == recovered[0]["message_id"]
            assert json.loads(interrupted[0]["text"])["reason"] == "output_limit"
            return trace
        eventually(recovered_trace)
        row = eventually(lambda: capture(pid, "02-recovered-running", [labels["cause"], labels["recovered"], labels["thinking"]]))
        assert any(node.get("AXRole") == "AXButton" and node.get("AXTitle") == labels["thought"] for node in row["tree"])
        fixture.finish.set()
        def done():
            trace = recovered_trace()
            return trace if trace["turn"]["status"] == "done" else None
        trace = eventually(done)
        save("trace.json", trace)
        eventually(lambda: capture(pid, "03-recovered-result", [labels["cause"], labels["recovered"], "verified after recovery"]))
        ax(pid, "reload")
        eventually(lambda: ax(pid, "press", prompt))
        ax(pid, "press", labels["agents"])
        eventually(lambda: ax(pid, "press", "verifier"))
        eventually(lambda: capture(pid, "04-replayed-result", [labels["cause"], labels["recovered"], "verified after recovery"]))
        assert not fixture.errors and fixture.workers == 2
        save("native.json", {"passed": True, "case_id": case_id, "app_pid": pid, "engine_pid": current["pid"], "thread_id": thread["id"], "turn_id": turn["id"], "worker_id": fixture.worker_id, "worker_requests": fixture.workers, "app_sha256": hashlib.sha256(executable.read_bytes()).hexdigest(), "native_window_evidence": True, "native_reload_verified": True, "scope": "keyless output-limit interruption, same-worker resume and native Reload replay; runner accepts cleanup separately"})
        ax(pid, "close")
        eventually(lambda: runtime.process_argv(pid) is None, 10)
        return 0
    finally:
        save("fixture.json", {"worker_id": fixture.worker_id, "workers": fixture.workers, "errors": fixture.errors})
        fixture.close()
        if display_hold.poll() is None:
            display_hold.terminate()
        display_hold.wait(timeout=5)


if __name__ == "__main__":
    raise SystemExit(main())
