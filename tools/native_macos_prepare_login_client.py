"""Pinned one-shot UDS client. Only safe protocol words reach its output."""
import hashlib
import json
import os
from pathlib import Path
import socket
import stat
import sys
import time
import select

from native_macos_prepare_login import (peer_pid, private_directory, process_identity, raw_clock,
                                      rooted, validate_remote_frame, validate_sources)


def read_line(connection, end):
    result = bytearray()
    while not result.endswith(b"\n"):
        left = end - time.monotonic()
        if left <= 0:
            raise RuntimeError("client original READY deadline expired")
        connection.settimeout(left)
        part = connection.recv(1)
        if not part or len(result) >= 16:
            raise RuntimeError("client protocol refused")
        result.extend(part)
    return bytes(result)


def validate_ready(path, expected_sha):
    path = Path(path)
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected_sha:
        raise RuntimeError("client ready bytes differ")
    ready = json.loads(raw)
    if (ready.get("schema") != 1 or ready.get("stage") not in ("before", "after")
            or ready["actor"] != ready["binding"]["runner"]
            or process_identity(ready["actor"]["pid"]) != ready["actor"]
            or os.environ.get("ZWAI_NATIVE_CASE_ID") != ready["binding"]["case_id"]
            or Path(__file__).resolve() != Path(ready["pins"]["client"]["path"])):
        raise RuntimeError("client actor/case/source differs")
    root = Path(ready["root"])
    if private_directory(root) != ready["root_identity"] or ready["socket"] != "prepare-login.sock":
        raise RuntimeError("client private root differs")
    endpoint = root / ready["socket"]
    info = endpoint.lstat()
    if (not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.getuid()
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_ino != ready["socket_inode"]):
        raise RuntimeError("client socket identity differs")
    validate_sources(ready["pins"])
    validate_remote_frame(ready["frame"], ready["prepare_deadline_raw"])
    return ready


def main(argv=None):
    args = sys.argv[1:] if argv is None else argv
    if len(args) != 2:
        raise RuntimeError("client requires ready path and exact bytes SHA")
    path, expected_sha = args
    ready = validate_ready(path, expected_sha)
    # Translate the original system RAW remainder once into this process's local
    # monotonic domain. A later source/frame recheck never creates a new budget.
    local_started = time.monotonic()
    shared_left = min(ready["prepare_deadline_raw"], ready["frame"]["expires_raw"]) - raw_clock()
    if shared_left <= 0:
        raise RuntimeError("client original shared deadline expired")
    end = local_started + min(8, shared_left)
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(max(.001, end - time.monotonic()))
        with rooted(ready["root"], ready["root_identity"]):
            connection.connect(ready["socket"])
        if (peer_pid(connection) != ready["actor"]["pid"]
                or process_identity(ready["actor"]["pid"]) != ready["actor"]):
            raise RuntimeError("client actual server identity differs")
        if read_line(connection, end) != b"READY\n":
            raise RuntimeError("client actor refused READY")
        validate_ready(path, expected_sha)
        # The RAM sender must not load its credential until it reads this READY.
        print("READY", flush=True)
        envelope = bytearray()
        try:
            # Buffered read(4097) can wait forever for EOF after a partial write.
            # Every real stdin read shares READY's original absolute deadline.
            while True:
                left = end - time.monotonic()
                if left <= 0 or not select.select([sys.stdin.fileno()], [], [], left)[0]:
                    raise RuntimeError("client RAM delivery expired")
                part = os.read(sys.stdin.fileno(), min(1024, 4097-len(envelope)))
                if not part:
                    break
                envelope.extend(part)
                if len(envelope) > 4096:
                    raise RuntimeError("client RAM envelope refused")
            if len(envelope) > 4096 or time.monotonic() >= end:
                raise RuntimeError("client RAM envelope refused")
            validate_ready(path, expected_sha)
            connection.sendall(envelope)
            connection.shutdown(socket.SHUT_WR)
            if read_line(connection, end) != b"ACCEPTED\n":
                raise RuntimeError("client actor refused delivery")
        finally:
            envelope.clear()
        print("DELIVERED", flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception:
        print("FAIL", flush=True)
        raise SystemExit(2)
