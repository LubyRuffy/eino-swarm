"""Keyless loopback worker fixture; both attempts use explicit test handshakes."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading


class Fixture:
    def __init__(self):
        self.workers = 0
        self.worker_id = ""
        self.calls = 0
        self.errors = []
        self.interrupt = threading.Event()
        self.finish = threading.Event()
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def chunk(self, delta, finish=None):
                body = {"id": "native-generation", "object": "chat.completion.chunk",
                        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
                self.wfile.write(("data: " + json.dumps(body) + "\n\n").encode())
                self.wfile.flush()

            def end(self, reason):
                self.chunk({}, reason)
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()

            def reply(self, content="", name=None, args=None):
                if name:
                    fixture.calls += 1
                    self.chunk({"role": "assistant", "tool_calls": [{"index": 0,
                        "id": "native-call-" + str(fixture.calls), "type": "function",
                        "function": {"name": name, "arguments": json.dumps(args)}}]})
                else:
                    self.chunk({"role": "assistant", "content": content})
                self.end("tool_calls" if name else "stop")

            def do_POST(self):
                try:
                    if self.headers.get("Authorization"):
                        raise ValueError("fixture must be keyless")
                    body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.end_headers()
                    manager = any(t["function"]["name"] == "spawn_agent" for t in body.get("tools", []))
                    if not manager:
                        fixture.workers += 1
                        if fixture.workers == 1:
                            self.chunk({"role": "assistant", "reasoning_content": "Inspecting the first assigned input before the interrupted output."})
                            if not fixture.interrupt.wait(100):
                                raise TimeoutError("test did not interrupt the first attempt")
                            self.end("length")
                        elif fixture.workers == 2:
                            self.chunk({"role": "assistant", "reasoning_content": "Inspecting the remaining input after recovery."})
                            if not fixture.finish.wait(100):
                                raise TimeoutError("test did not finish recovery")
                            self.chunk({"content": "The assigned inputs were verified after recovery."})
                            self.end("stop")
                        else:
                            raise ValueError("same worker must recover once")
                        return
                    messages = body["messages"]
                    calls = [call for message in messages for call in message.get("tool_calls", [])]
                    count = lambda name: sum(call["function"]["name"] == name for call in calls)
                    receipt = next((m for m in messages if m["role"] == "tool" and '"agent_id"' in m.get("content", "")), None)
                    if not receipt:
                        self.reply(name="spawn_agent", args={"role": "verifier", "task": "Inspect the assigned inputs and report a verified result."})
                    else:
                        fixture.worker_id = json.loads(receipt["content"])["agent_id"]
                        if count("wait_agents") == 0:
                            self.reply(name="wait_agents", args={"agent_ids": [fixture.worker_id], "timeout_s": 60})
                        elif count("resume_agent") == 0:
                            self.reply(name="resume_agent", args={"agent_id": fixture.worker_id, "task": "Continue the unfinished verification and report the result."})
                        elif count("wait_agents") == 1:
                            self.reply(name="wait_agents", args={"agent_ids": [fixture.worker_id], "timeout_s": 60})
                        else:
                            self.reply("The existing worker recovered and verified the assigned inputs.")
                except (OSError, ValueError, TimeoutError) as error:
                    fixture.errors.append(str(error))

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:" + str(self.server.server_port)

    def close(self):
        self.interrupt.set()
        self.finish.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
