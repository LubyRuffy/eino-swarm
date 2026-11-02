"""Keyless model for the native cancel/Continue authorization acceptance case.

Only fixed human inputs and known assistant candidates are classified. The
manager always attempts the real scheduling tool; successful prose cannot
substitute for a tool receipt or a stored wait.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import re
import threading

from native_macos_readiness import save_json


CONTINUE_TEXT = "Continue the current work."
ALLOW_TEXT = "You may now suspend this conversation and arm its next timed wake."
ARMED_TEXT = "I have armed the next check and will wait for it."
REFUSED_TEXT = "The next check was not armed; the current waiting prohibition remains in effect."


def engine_prompt(filename, constant):
    source = (Path(__file__).resolve().parents[1] / "internal/engine" / filename).read_text()
    match = re.search(r"const " + re.escape(constant) + r" = `([^`]+)`", source)
    if match is None:
        raise ValueError("native fixture cannot identify the actual engine classifier protocol")
    return match[1]


class Fixture:
    def __init__(self, evidence):
        self.evidence = Path(evidence)
        self.wake_prompt = engine_prompt("thread_wake_policy.go", "wakePolicyClassificationPrompt")
        self.answer_prompt = engine_prompt("schedule_answer.go", "scheduleAnswerPrompt")
        self._lock = threading.RLock()
        self._state = dict(errors=[], sources=[], manager_sources=[], tool_attempts=0, requests=0)
        self._sequence = 0
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args):
                pass

            def do_POST(self):
                try:
                    self.connection.settimeout(3)
                    if self.path not in ("/chat/completions", "/v1/chat/completions") or self.headers.get("Authorization"):
                        raise ValueError("keyless fixture endpoint or authorization refused")
                    length = int(self.headers.get("Content-Length", "0"))
                    if not 0 < length <= 8 << 20:
                        raise ValueError("invalid bounded model request size")
                    request = json.loads(self.rfile.read(length))
                    message, finish = fixture.reply(request)
                    if request.get("stream") is True:
                        delta = dict(message)
                        if delta.get("tool_calls"):
                            delta["tool_calls"] = [dict(index=0, **call) for call in delta["tool_calls"]]
                        chunk = lambda value, reason: dict(id="native-cancel", object="chat.completion.chunk", choices=[dict(index=0, delta=value, finish_reason=reason)])
                        payload = ("data: " + json.dumps(chunk(delta, None)) + "\n\ndata: " + json.dumps(chunk({}, finish)) + "\n\ndata: [DONE]\n\n").encode()
                        mime = "text/event-stream"
                    else:
                        payload = json.dumps(dict(id="native-cancel", object="chat.completion", choices=[dict(index=0, message=message, finish_reason=finish)])).encode()
                        mime = "application/json"
                    self.send_response(200)
                    self.send_header("Content-Type", mime)
                    self.send_header("Content-Length", str(len(payload)))
                    self.send_header("Connection", "close")
                    self.end_headers()
                    self.wfile.write(payload)
                    self.close_connection = True
                except (ValueError, KeyError, TypeError) as error:
                    with fixture._lock:
                        fixture._state["errors"].append(str(error))
                        fixture.persist()
                    self.send_error(400, str(error))
                except (BrokenPipeError, ConnectionResetError, TimeoutError):
                    pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.server.block_on_close = False
        self.url = "http://127.0.0.1:" + str(self.server.server_port) + "/v1"
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": .05}, daemon=True)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(3)
        if self.thread.is_alive():
            raise RuntimeError("native cancellation fixture did not stop")

    def status(self):
        with self._lock:
            return json.loads(json.dumps(self._state))

    def persist(self):
        save_json(self.evidence / "schedule-cancel-fixture.json", self.status())

    def _policy(self, request, messages, *, force_none=False):
        if (request.get("stream", False) is not False or request.get("tools", []) != []
                or request.get("tool_choice", "none") != "none"
                or (force_none and request.get("tool_choice") != "none") or len(messages) != 2
                or any(not isinstance(row, dict) or set(row) != {"role", "content"} for row in messages)
                or messages[1]["role"] != "user" or not isinstance(messages[1]["content"], str)):
            raise ValueError("classifier must be a separate tool-free Generate request")
        return json.loads(messages[1]["content"])

    def reply(self, request):
        with self._lock:
            if not isinstance(request, dict) or request.get("model") != "native-cancel-model":
                raise ValueError("unknown native cancellation model")
            messages = request.get("messages")
            if not isinstance(messages, list) or not messages or any(not isinstance(row, dict) for row in messages):
                raise ValueError("invalid model messages")
            first = messages[0]
            self._state["requests"] += 1
            if first == dict(role="system", content=self.wake_prompt):
                source = self._policy(request, messages)
                if (not isinstance(source, dict) or set(source) != {"seq", "kind", "text"}
                        or type(source["seq"]) is not int or source["seq"] <= 0
                        or source["kind"] != "user_message" or source["text"] not in (CONTINUE_TEXT, ALLOW_TEXT)):
                    raise ValueError("classifier did not receive one known original human input")
                self._state["sources"].append(source)
                answer = dict(decision="allow" if source["text"] == ALLOW_TEXT else "unchanged",
                              quote=ALLOW_TEXT if source["text"] == ALLOW_TEXT else "")
                message = dict(role="assistant", content=json.dumps(answer))
            elif first == dict(role="system", content=self.answer_prompt):
                candidate = self._policy(request, messages, force_none=True)
                if (not isinstance(candidate, dict) or set(candidate) != {"messages", "now"}
                        or not isinstance(candidate["messages"], list) or not candidate["messages"]
                        or any(text not in (ARMED_TEXT, REFUSED_TEXT) for text in candidate["messages"])
                        or not isinstance(candidate["now"], str)):
                    raise ValueError("unknown schedule-answer candidate")
                claims = [dict(stance="affirmed", temporal="current", attribution="assistant", quote=ARMED_TEXT,
                               scope="unspecified", new=True)] if ARMED_TEXT in candidate["messages"] else []
                message = dict(role="assistant", content=json.dumps(dict(claims=claims)))
            else:
                if any(isinstance(row.get("content"), str) and row["content"].startswith(("## Thread wake authorization classification", "## Schedule answer claims")) for row in messages if row.get("role") == "system"):
                    raise ValueError("classifier protocol differs from the actual engine source")
                names = {row.get("function", {}).get("name") for row in request.get("tools", [])}
                if "schedule_wake" not in names:
                    raise ValueError("unexpected background model request")
                last_user = max((index for index, row in enumerate(messages) if row.get("role") == "user"), default=-1)
                if last_user < 0 or messages[last_user].get("content") not in (CONTINUE_TEXT, ALLOW_TEXT):
                    raise ValueError("manager did not receive the current native human input")
                for row in messages:
                    if row.get("role") == "system" and isinstance(row.get("content"), str) and row["content"].startswith("## Thread wake authorization\n"):
                        source = json.loads(row["content"].splitlines()[-1])
                        self._state["manager_sources"].append({key: source.get(key) for key in ("source_seq", "source_kind", "schedule_id")})
                tail = messages[last_user + 1:]
                called = any(call.get("function", {}).get("name") == "schedule_wake" for row in tail for call in row.get("tool_calls", []))
                if not called:
                    self._sequence += 1
                    self._state["tool_attempts"] += 1
                    message = dict(role="assistant", tool_calls=[dict(id="native-cancel-wake-" + str(self._sequence), type="function",
                        function=dict(name="schedule_wake", arguments=json.dumps(dict(prompt="Inspect the current work state.", delay_s=3600))))])
                else:
                    results = [row for row in tail if row.get("role") == "tool"]
                    if not results:
                        raise ValueError("manager tool receipt is missing")
                    accepted = json.loads(results[-1]["content"]).get("ok") is True
                    message = dict(role="assistant", content=ARMED_TEXT if accepted else REFUSED_TEXT)
            self.persist()
            return message, "tool_calls" if message.get("tool_calls") else "stop"
