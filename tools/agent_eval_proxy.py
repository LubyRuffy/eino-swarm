#!/usr/bin/env python3
"""Private model gateway for byte-preserving transport and usage observation.

This separate loopback utility forwards fixed model endpoints in memory and
records model identities, timings and usage without storing prompt/output bodies.
The ordinary evaluation runner does not automatically route traffic through it.
It does not provide container isolation or protect an external grader.
"""

from __future__ import annotations

import argparse
import http.server
import json
import os
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


def token_usage(value) -> dict | None:
    if not isinstance(value, dict):
        return None
    fields = ("prompt_tokens", "completion_tokens", "total_tokens", "input_tokens", "output_tokens",
              "cached_tokens", "reasoning_tokens")
    result = {k: v for k, v in value.items() if k in fields and isinstance(v, int) and not isinstance(v, bool)}
    for key in ("prompt_tokens_details", "completion_tokens_details", "input_tokens_details", "output_tokens_details"):
        if isinstance(value.get(key), dict):
            result[key] = {k: v for k, v in value[key].items()
                           if k in fields and isinstance(v, int) and not isinstance(v, bool)}
    return result


class UsageStream:
    def __init__(self):
        self.buffer = b""
        self.usage = None
        self.done = False

    def add(self, data: bytes):
        self.buffer += data
        while b"\n" in self.buffer:
            line, self.buffer = self.buffer.split(b"\n", 1)
            if not line.startswith(b"data:"):
                continue
            body = line[5:].strip()
            if body == b"[DONE]":
                self.done = True
                continue
            try:
                event = json.loads(body)
            except (ValueError, UnicodeError):
                continue
            usage = token_usage(event.get("usage"))
            response = event.get("response")
            if usage is None and isinstance(response, dict):
                usage = token_usage(response.get("usage"))
            if usage is not None:
                self.usage = usage
            if event.get("type") in ("response.completed", "response.failed", "response.incomplete"):
                self.done = True


def make_server(config: dict, address: str, port: int):
    lock = threading.Lock()
    log = Path(config["observations"])
    upstream = urllib.request.build_opener(NoRedirect())

    def observe(record):
        with lock:
            fd = os.open(log, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
            with os.fdopen(fd, "w") as out:
                out.write(json.dumps(record, separators=(",", ":")) + "\n")

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.0"

        def log_message(self, *_):
            pass  # Default access/error logging can include user-controlled URLs.

        def do_GET(self):
            self.forward()

        def do_POST(self):
            self.forward()

        def forward(self):
            if self.path == "/health" and self.command == "GET":
                self.send_response(200); self.end_headers(); self.wfile.write(b"ok"); return
            raw_length = self.headers.get("Content-Length", "0")
            if not raw_length.isdigit() or int(raw_length) > 32 * 1024 * 1024:
                self.send_error(413); return
            body = self.rfile.read(int(raw_length))
            if self.command == "POST" and not body:
                self.send_error(400); return
            headers = {"Content-Type": self.headers.get("Content-Type", "application/json"), "Accept": self.headers.get("Accept", "application/json")}
            provider_id, model_name = None, None
            segments = urllib.parse.urlsplit(self.path).path.split("/")
            if len(segments) < 4 or segments[1] != "model":
                self.send_error(404); return
            provider_id = urllib.parse.unquote(segments[2])
            provider = config["providers"].get(provider_id)
            suffix = "/" + "/".join(segments[3:])
            if not provider or (self.command, suffix) not in (("POST", "/chat/completions"), ("POST", "/responses"), ("GET", "/models")):
                self.send_error(404); return
            if self.headers.get("Authorization") != "Bearer " + config["agent_token"]:
                self.send_error(403); return
            if body:
                try:
                    model_name = json.loads(body).get("model")
                except (ValueError, AttributeError):
                    self.send_error(400); return
                if model_name not in provider["catalog"]:
                    self.send_error(400); return
            if provider.get("api_key"):
                headers["Authorization"] = "Bearer " + provider["api_key"]
            url = provider["base_url"].rstrip("/") + suffix
            request_id = uuid.uuid4().hex
            start = time.monotonic()
            metered = self.command == "POST" and provider_id is not None
            identity = {"request_id": request_id, "provider_id": provider_id, "model": model_name}
            if metered:
                observe({**identity, "phase": "start", "wall_ns": time.time_ns(), "monotonic_ns": time.monotonic_ns()})
            usage, status, failure = None, None, None
            stream = UsageStream()
            try:
                request = urllib.request.Request(url, data=body if self.command == "POST" else None, headers=headers, method=self.command)
                try:
                    response = upstream.open(request, timeout=float(config.get("upstream_timeout_s", 300)))
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    status = response.status
                    self.send_response(status)
                    content_type = response.headers.get("Content-Type", "application/json")
                    self.send_header("Content-Type", content_type)
                    self.end_headers()
                    ordinary = bytearray()
                    while chunk := response.read1(16 * 1024):
                        if "text/event-stream" in content_type:
                            stream.add(chunk)
                        elif len(ordinary) + len(chunk) <= 32 * 1024 * 1024:
                            ordinary.extend(chunk)
                        self.wfile.write(chunk)
                        self.wfile.flush()
                    usage = stream.usage
                    if "text/event-stream" not in content_type:
                        try:
                            usage = token_usage(json.loads(ordinary).get("usage"))
                        except (ValueError, AttributeError):
                            pass
            except Exception as error:
                failure = type(error).__name__
                if status is None:
                    self.send_error(502)
            finally:
                if metered:
                    observe({**identity, "phase": "end", "http_status": status, "error_class": failure,
                             "raw_usage": usage, "stream_completed": stream.done, "duration_ms": round((time.monotonic() - start) * 1000),
                             "wall_ns": time.time_ns(), "monotonic_ns": time.monotonic_ns()})

    return http.server.ThreadingHTTPServer((address, port), Handler)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--bind", default="127.0.0.1", help="listen interface; default is loopback")
    parser.add_argument("--port", type=int, default=8080)
    args = parser.parse_args()
    make_server(json.loads(Path(args.config).read_text()), args.bind, args.port).serve_forever()
