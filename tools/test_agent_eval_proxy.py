"""Real HTTP gateway tests: unchanged stream bytes and private usage evidence."""

import http.server
import json
import sys
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import agent_eval_proxy as proxy
import agent_eval_usage as usage


class GatewayTest(unittest.TestCase):
    def test_upstream_redirect_cannot_forward_a_key_to_another_service(self):
        leaked = []

        class OtherService(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_GET(self):
                leaked.append(self.headers.get("Authorization"))
                self.send_response(200); self.end_headers(); self.wfile.write(b"{}")

        other = http.server.ThreadingHTTPServer(("127.0.0.1", 0), OtherService)
        threading.Thread(target=other.serve_forever, daemon=True).start()
        self.addCleanup(other.server_close); self.addCleanup(other.shutdown)

        class Redirect(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_POST(self):
                self.rfile.read(int(self.headers["Content-Length"]))
                self.send_response(302)
                self.send_header("Location", f"http://127.0.0.1:{other.server_port}/unrelated")
                self.end_headers()

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Redirect)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        self.addCleanup(upstream.server_close); self.addCleanup(upstream.shutdown)
        with tempfile.TemporaryDirectory() as tmp:
            gateway = proxy.make_server({"observations": str(Path(tmp) / "usage.jsonl"), "agent_token": "ephemeral",
                                         "providers": {"p": {"base_url": f"http://127.0.0.1:{upstream.server_port}/v1",
                                                               "api_key": "private-test-key", "catalog": ["m"]}}}, "127.0.0.1", 0)
            thread = threading.Thread(target=gateway.serve_forever, daemon=True); thread.start()
            try:
                request = urllib.request.Request(f"http://127.0.0.1:{gateway.server_port}/model/p/chat/completions",
                                                 data=b'{"model":"m"}', headers={"Authorization": "Bearer ephemeral"})
                with self.assertRaises(urllib.error.HTTPError) as caught:
                    urllib.request.urlopen(request, timeout=5)
                self.assertEqual(caught.exception.code, 302)
                caught.exception.close()
                self.assertEqual(leaked, [])
            finally:
                gateway.shutdown(); gateway.server_close(); thread.join(timeout=2)

    def test_actual_stream_keeps_zero_cache_without_recording_prompts_or_keys(self):
        payload = b'data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0}}}\n\ndata: [DONE]\n\n'
        upstream_calls = []

        class Upstream(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_POST(self):
                upstream_calls.append((self.path, self.headers.get("Authorization"), self.rfile.read(int(self.headers["Content-Length"]))))
                self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.end_headers()
                for offset in range(0, len(payload), 11):
                    self.wfile.write(payload[offset:offset + 11]); self.wfile.flush()

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        upstream_thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        upstream_thread.start()
        self.addCleanup(upstream.server_close); self.addCleanup(upstream.shutdown)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "usage.jsonl"
            gateway = proxy.make_server({"observations": str(path), "agent_token": "temporary-token",
                                         "providers": {"p": {"base_url": f"http://127.0.0.1:{upstream.server_port}/v1",
                                                               "api_key": "upstream-private-key", "catalog": ["m"]}}}, "127.0.0.1", 0)
            gateway_thread = threading.Thread(target=gateway.serve_forever, daemon=True)
            gateway_thread.start()
            try:
                req = urllib.request.Request(f"http://127.0.0.1:{gateway.server_port}/model/p/chat/completions",
                                             data=json.dumps({"model": "m", "messages": [{"role": "user", "content": "private-task-text"}]}).encode(),
                                             headers={"Authorization": "Bearer temporary-token", "Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=5) as response:
                    self.assertEqual(response.read(), payload)
                self.assertEqual(upstream_calls[0][0], "/v1/chat/completions")
                self.assertEqual(upstream_calls[0][1], "Bearer upstream-private-key")
            finally:
                gateway.shutdown(); gateway.server_close(); gateway_thread.join(timeout=2)
            text = path.read_text()
            self.assertNotIn("private-task-text", text)
            self.assertNotIn("upstream-private-key", text)
            self.assertNotIn("temporary-token", text)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            end = next(r for r in map(json.loads, text.splitlines()) if r["phase"] == "end")
            facts = usage.extract([end])
            self.assertEqual(facts["total_tokens"], 12)
            self.assertEqual(facts["calls"][0]["cached_tokens"]["state"], "zero")
            self.assertTrue(end["stream_completed"])

    def test_responses_usage_survives_fragmented_events(self):
        value = proxy.UsageStream()
        data = b'data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7,"input_tokens_details":{"cached_tokens":0}}}}\n\n'
        for byte in data:
            value.add(bytes([byte]))
        self.assertTrue(value.done)
        self.assertEqual(value.usage["total_tokens"], 7)


if __name__ == "__main__":
    unittest.main()
