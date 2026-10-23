"""Keyless loopback model fixture for actual long-lived engine workers.

Models select scenarios; human prompt text never selects behavior. The fixture
returns a real exec call, requires its side effect, then retains one HTTP stream
until the acceptance driver explicitly releases it. It never fabricates engine
worker IDs or lifecycle events.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import shlex
import threading
import time
import uuid


class WorkerFixture:
    def __init__(self, workspace, evidence, heartbeat=15):
        self.workspace, self.evidence = Path(workspace).resolve(), Path(evidence).resolve()
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.evidence.mkdir(parents=True, exist_ok=True)
        self.models = {kind: "resilience-" + kind for kind in ("checkpoint", "running", "parked")}
        self.heartbeat = heartbeat
        self._lock = threading.RLock()
        self._stop = threading.Event()
        self._release = {kind: threading.Event() for kind in ("running", "parked")}
        self._wake = {kind: threading.Event() for kind in self._release}
        self._active = {kind: threading.Event() for kind in self._release}
        self._rows = {kind: dict(worker_id=None, worker_requests=0, spawn_calls=0,
            stream_active=False, cancelled_before_release=False, released=False,
            marker=str(self.workspace / ("operation-" + kind + "-" + uuid.uuid4().hex)), errors=[])
            for kind in self._release}
        self._requests = []
        self._call_sequences = {}
        self._wait_ids = {kind: set() for kind in self._release}
        fixture = self
        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"
            def log_message(self, *args):
                pass

            def do_POST(self):
                if self.headers.get("Authorization"):
                    self.send_error(400, "keyless fixture refuses authorization headers")
                    return
                if self.path not in ("/v1/chat/completions", "/chat/completions"):
                    self.send_error(404)
                    return
                try:
                    count = int(self.headers.get("Content-Length", "0"))
                    if not 0 < count <= 8 << 20:
                        raise ValueError("invalid bounded request size")
                    request = json.loads(self.rfile.read(count))
                    fixture._handle(self, request)
                except (ValueError, KeyError, TypeError) as error:
                    self.send_error(400, str(error))
                except (BrokenPipeError, ConnectionResetError):
                    pass
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.server.block_on_close = False
        self.url = "http://127.0.0.1:" + str(self.server.server_port) + "/v1"
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": .05}, daemon=True)
        self.thread.start()

    def _persist(self):
        path = self.evidence / "worker-fixture-status.json"
        temporary = path.with_suffix("." + uuid.uuid4().hex + ".tmp")
        temporary.write_text(json.dumps(dict(self._rows, requests=self._requests), indent=2))
        temporary.replace(path)

    def status(self):
        with self._lock:
            return json.loads(json.dumps(dict(self._rows, requests=self._requests)))

    def wait_active(self, kind, timeout):
        return self._active[kind].wait(timeout)

    def release(self, kind):
        with self._lock:
            self._rows[kind]["released"] = True
            self._persist()
        self._release[kind].set()
        self._wake[kind].set()

    def stop(self):
        self._stop.set()
        for wake in self._wake.values():
            wake.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(2)

    def _reply(self, handler, request, message, finish="stop"):
        if request.get("stream"):
            handler.send_response(200)
            handler.send_header("Content-Type", "text/event-stream")
            handler.send_header("Connection", "close")
            handler.end_headers()
            self._chunk(handler, request, message)
            self._chunk(handler, request, {}, finish)
            handler.wfile.write(b"data: [DONE]\n\n")
            handler.wfile.flush()
            handler.close_connection = True
        else:
            body = json.dumps(dict(id="offline-reply", object="chat.completion", created=int(time.time()),
                model=request["model"], choices=[dict(index=0, message=dict(role="assistant", **message), finish_reason=finish)])).encode()
            handler.send_response(200)
            handler.send_header("Content-Type", "application/json")
            handler.send_header("Content-Length", str(len(body)))
            handler.end_headers()
            handler.wfile.write(body)

    def _chunk(self, handler, request, delta, finish=None):
        row = dict(id="offline-stream", object="chat.completion.chunk", created=int(time.time()),
            model=request["model"], choices=[dict(index=0, delta=delta, finish_reason=finish)])
        handler.wfile.write(b"data: " + json.dumps(row).encode() + b"\n\n")
        handler.wfile.flush()

    def _call(self, kind, name, arguments):
        identity = "resilience-" + kind + "-" + name
        if name == "wait_agents":
            sequence = self._call_sequences.get(kind, 0) + 1
            self._call_sequences[kind] = sequence
            identity += "-" + str(sequence)
            self._wait_ids[kind].add(identity)
        return dict(content="", tool_calls=[dict(index=0, id=identity,
            type="function", function=dict(name=name, arguments=json.dumps(arguments)))])

    def _observed_done(self, messages, kind, worker_id):
        for message in reversed(messages):
            if message.get("role") != "tool" or message.get("tool_call_id") not in self._wait_ids[kind]:
                continue
            receipt = json.loads(message["content"])
            for agent in receipt.get("agents", []):
                if agent.get("agent_id") == worker_id:
                    return agent.get("status") == "done" and not agent.get("error") and not agent.get("err")
            return False
        return False

    def _handle(self, handler, request):
        model = request["model"]
        kinds = [kind for kind, value in self.models.items() if value == model]
        if not kinds:
            raise ValueError("unexpected offline fixture model")
        kind = kinds[0]
        names = {tool.get("function", {}).get("name") for tool in request.get("tools", [])}
        manager = "spawn_agent" in names
        messages = request.get("messages", [])
        # Rolling session memory uses the thread's model, independently of the
        # pinned compact model. Recognize its fixed system protocol, never a
        # phrase inside human/worker input, and leave the held worker untouched.
        briefing_header = "Write a dense briefing of the conversation so a later turn can continue the work, and so a later memory review can see what was learned."
        briefing = (not names and len(messages) == 2
            and messages[0].get("role") == "system" and messages[1].get("role") == "user"
            and isinstance(messages[0].get("content"), str)
            and messages[0]["content"].startswith(briefing_header + "\n"))
        with self._lock:
            self._requests.append(dict(seq=len(self._requests) + 1, at=time.time(), kind=kind,
                role="briefing" if briefing else "manager" if manager else "worker", tools=sorted(name for name in names if name)))
            self._persist()
        if briefing:
            return self._reply(handler, request, dict(content="Continue verifying the assigned operation against the current engine lifecycle evidence."))
        if kind == "checkpoint":
            return self._reply(handler, request, dict(content="The offline checkpoint completed."))
        if manager:
            return self._manager(handler, request, kind)
        return self._worker(handler, request, kind)

    def _manager(self, handler, request, kind):
        messages = request.get("messages", [])
        last_user = next((index for index in range(len(messages) - 1, -1, -1)
                          if messages[index].get("role") == "user"), -1)
        # This is the engine's fixed transport wrapper, as in mock_schedule.go;
        # the operation instruction following it never selects fixture behavior.
        content = messages[last_user].get("content", "") if last_user >= 0 else ""
        if isinstance(content, list):
            content = "".join(part.get("text", "") for part in content
                               if isinstance(part, dict) and part.get("type") == "text"
                               and isinstance(part.get("text"), str))
        scheduled = isinstance(content, str) and content.startswith("This turn is a scheduled check.")
        current = messages[last_user + 1:] if last_user >= 0 else messages
        with self._lock:
            row = self._rows[kind]
            for message in messages:
                if message.get("role") != "tool" or message.get("tool_call_id") != "resilience-" + kind + "-spawn_agent":
                    continue
                receipt = json.loads(message["content"])
                if not isinstance(receipt.get("agent_id"), str) or not receipt["agent_id"]:
                    raise ValueError("spawn did not return an actual worker identity")
                if row["worker_id"] and row["worker_id"] != receipt["agent_id"]:
                    raise ValueError("worker identity changed")
                row["worker_id"] = receipt["agent_id"]
            if not row["worker_id"]:
                row["spawn_calls"] += 1
                if row["spawn_calls"] != 1:
                    row["errors"].append("manager attempted another spawn without a valid receipt")
                    raise ValueError("duplicate spawn refused")
                reply = self._call(kind, "spawn_agent", dict(role="verifier", task="Inspect the assigned operation and verify its persisted result."))
            elif row["released"]:
                done = self._observed_done(current, kind, row["worker_id"])
                reported = any(message.get("role") == "tool" and message.get("tool_call_id") == "resilience-" + kind + "-report_schedule" for message in current)
                if not done:
                    reply = self._call(kind, "wait_agents", dict(agent_ids=[row["worker_id"]], timeout_s=1))
                elif kind == "parked" and scheduled and not reported:
                    reply = self._call(kind, "report_schedule", dict(findings="The existing operation completed.", keep=False))
                else:
                    reply = dict(content="The existing operation completed.")
            elif kind == "parked":
                reported = any(message.get("role") == "tool" and message.get("tool_call_id") == "resilience-" + kind + "-report_schedule" for message in current)
                reply = self._call(kind, "report_schedule", dict(findings="The existing operation remains active.", keep=True)) if scheduled and not reported else dict(content="The independently owned operation remains active.")
            else:
                reply = self._call(kind, "wait_agents", dict(agent_ids=[row["worker_id"]], timeout_s=30))
            self._persist()
        return self._reply(handler, request, reply, "tool_calls" if "tool_calls" in reply else "stop")

    def _worker(self, handler, request, kind):
        messages = request.get("messages", [])
        executed = any(message.get("role") == "tool" and message.get("tool_call_id") == "resilience-" + kind + "-exec" for message in messages)
        with self._lock:
            row = self._rows[kind]
            row["worker_requests"] += 1
            if not executed:
                reply = self._call(kind, "exec", dict(command="/usr/bin/touch " + shlex.quote(row["marker"]), timeout_ms=1000))
            else:
                if not Path(row["marker"]).is_file() or not request.get("stream"):
                    raise ValueError("worker stream requires the actual tool side effect and streaming adapter")
                if row["stream_active"] or row["worker_requests"] != 2:
                    row["errors"].append("worker model request restarted")
                    raise ValueError("worker model request restarted")
                row["stream_active"] = True
                reply = None
            self._persist()
        if reply is not None:
            return self._reply(handler, request, reply, "tool_calls")
        handler.send_response(200)
        handler.send_header("Content-Type", "text/event-stream")
        handler.send_header("Connection", "close")
        handler.end_headers()
        try:
            self._chunk(handler, request, dict(role="assistant", reasoning_content="Operation pending. "))
            self._active[kind].set()
            while not self._stop.is_set() and not self._release[kind].is_set():
                self._chunk(handler, request, dict(reasoning_content="Checking. "))
                self._wake[kind].wait(self.heartbeat)
            if self._release[kind].is_set():
                self._chunk(handler, request, dict(content="The assigned operation finished."))
                self._chunk(handler, request, {}, "stop")
                handler.wfile.write(b"data: [DONE]\n\n")
                handler.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            with self._lock:
                row["cancelled_before_release"] = not row["released"]
        finally:
            handler.close_connection = True
            with self._lock:
                row["stream_active"] = False
                if not row["released"]:
                    row["cancelled_before_release"] = True
                self._persist()
