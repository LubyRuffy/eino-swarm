"""One bounded login preparation inside an already-owned native case.

Root observes each published frame, then invokes the pinned client. Missing SDK
lock flags remain unknown; only a fresh desktop approval can admit the App.
No credential is written, hashed, logged, or placed in argv/environment.
"""
from contextlib import contextmanager
import hashlib
import json
import math
import os
from pathlib import Path
import select
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time

from native_macos_case import process_identity as process_arguments
from native_macos_case_power import validate_case_context
from native_macos_resilience_fault import process_identity


FIXED_BINDINGS = ("execution_id", "launch_id", "source_sha", "package_sha256", "phase", "session")
SESSION_FIELDS = ("user", "uid", "on_console", "login_done", "audit_id", "session_uuid")


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def raw_clock():
    return time.clock_gettime(time.CLOCK_MONOTONIC_RAW)


def remaining(deadline):
    if type(deadline) not in (int, float) or not math.isfinite(deadline):
        raise RuntimeError("preparation deadline is invalid")
    left = deadline - time.monotonic()
    if left <= 0:
        raise RuntimeError("preparation original deadline expired")
    return left


def session_lock(value):
    """Do not normalize a missing or malformed SDK lock flag to false."""
    if not isinstance(value, dict) or type(value.get("lock_key_present")) is not bool:
        raise RuntimeError("SDK lock presence is malformed")
    locked = value.get("locked")
    if value["lock_key_present"]:
        if type(locked) is not bool:
            raise RuntimeError("SDK lock flag is malformed")
    elif locked is not None:
        raise RuntimeError("missing SDK lock flag must remain unknown")
    return locked


def check_session(value, expected):
    session_lock(value)
    if (not isinstance(expected, dict) or set(expected) != set(SESSION_FIELDS)
            or any(value.get(key) != expected[key] or type(value.get(key)) is not type(expected[key])
                   for key in SESSION_FIELDS)
            or type(value.get("uid")) is not int or value["uid"] != os.getuid()
            or value.get("on_console") is not True or value.get("login_done") is not True
            or type(value.get("audit_id")) is not int or value["audit_id"] < 0
            or not isinstance(value.get("session_uuid"), str) or not value["session_uuid"]):
        raise RuntimeError("preparation session identity differs")
    return session_lock(value)


@contextmanager
def rooted(root, expected_identity=None):
    """Relative AF_UNIX names stay inside even a >104-byte private case path."""
    current = os.open(".", os.O_RDONLY)
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        info = os.fstat(directory)
        if expected_identity is not None and expected_identity != dict(device=info.st_dev, inode=info.st_ino):
            raise RuntimeError("preparation root changed before relative socket access")
        os.fchdir(directory)
        yield
    finally:
        os.fchdir(current)
        os.close(directory)
        os.close(current)


def private_directory(path):
    info = Path(path).lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid()
            or stat.S_IMODE(info.st_mode) != 0o700):
        raise RuntimeError("preparation private root is unsafe")
    return dict(device=info.st_dev, inode=info.st_ino)


def write_new(path, value):
    path = Path(path)
    # Pollers must never observe a truncated readiness document. link publishes
    # the complete inode atomically and refuses an existing target.
    descriptor, temporary = tempfile.mkstemp(prefix="." + path.name + "-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def spawn_owned(argv, context, **kwargs):
    """Register Popen before a pending cancellation may leave this boundary."""
    pending = []
    previous = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
    def defer(signum, frame):
        pending.append((signum, frame))
    try:
        for sig in previous:
            signal.signal(sig, defer)
        process = subprocess.Popen(argv, env=context["environment"], start_new_session=True, **kwargs)
        context["processes"].append(process)
        context["scope"].groups.add(process.pid)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    if pending:
        signum, frame = pending[0]
        if callable(previous[signum]):
            previous[signum](signum, frame)
        raise KeyboardInterrupt
    return process


class NativeOperations:
    """OS leaves; headless tests replace these without inventing harness APIs."""
    def __init__(self, helper):
        self.helper = Path(helper)

    def session(self, context, deadline):
        child = spawn_owned(["/usr/bin/swift", str(self.helper), "session"], context,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        out, _ = child.communicate(timeout=min(5, remaining(deadline)))
        if child.returncode != 0 or len(out) > 16384:
            raise RuntimeError("preparation SDK session probe failed")
        return json.loads(out)

    def capture(self, context, path, deadline):
        child = spawn_owned(["/usr/sbin/screencapture", "-x", "-D1", str(path)], context,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        child.wait(timeout=min(5, remaining(deadline)))
        if child.returncode != 0:
            raise RuntimeError("preparation current frame failed")

    def input(self, context, target, credential, deadline):
        child = spawn_owned(["/usr/bin/swift", str(self.helper), "input", json.dumps(target, sort_keys=True)],
                            context, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        out, _ = child.communicate(input=credential.encode("utf-8"), timeout=min(30, remaining(deadline)))
        # The pinned helper reports only these safe booleans; unknown post-lock is
        # intentionally allowed here and must still pass a fresh desktop frame.
        result = json.loads(out)
        if (child.returncode not in (0, 1) or result.get("input_attempted") is not True
                or result.get("credential_recorded") is not False
                or set(result) != {"input_attempted", "credential_recorded", "unlocked"}
                or type(result["unlocked"]) is not bool):
            raise RuntimeError("preparation input helper refused")
        return dict(input_attempted=True, helper_exit=child.returncode)


def validate_sources(pins):
    for name in ("actor", "client", "helper"):
        entry = pins[name]
        path = Path(entry["path"])
        if (not path.is_absolute() or path.is_symlink() or not path.is_file()
                or sha(path) != entry["sha256"]):
            raise RuntimeError("preparation " + name + " source differs")


def validate_context(context, binding, pins, app_source, package_path):
    receipt, env, bound = context["receipt"], context["environment"], context["power_binding"]
    case_id = validate_case_context(context["lock"], receipt, env)
    if (any(bound.get(key) != binding[key] for key in FIXED_BINDINGS)
            or bound.get("case_id") != case_id or process_identity(os.getpid()) != bound.get("runner")
            or receipt.get("launched") is not False
            or Path(receipt["app_source"]).resolve() != Path(app_source).resolve()
            or sha(package_path) != binding["package_sha256"]):
        raise RuntimeError("preparation case/source/package identity differs")
    root = Path(receipt["case_root"])
    private_directory(root)
    scope = context["scope"]
    if (scope.processes is not context["processes"] or scope.case_id != case_id
            or Path(scope.bundle) != Path(env["ZWAI_NATIVE_APP"])
            or Path(scope.data) != Path(env["ZWAI_NATIVE_DATA"])):
        raise RuntimeError("preparation original process scope differs")
    for name in ("HOME", "ZWAI_NATIVE_DATA", "ZWAI_NATIVE_WORKSPACE", "ZWAI_NATIVE_APP"):
        if not Path(env[name]).resolve().is_relative_to(root.resolve()):
            raise RuntimeError("preparation environment escapes private case")
    if env.get("ZWAI_NATIVE_POWER_BINDING") != json.dumps(bound, sort_keys=True):
        # Producers may use compact separators; compare objects, preserving types.
        if json.loads(env.get("ZWAI_NATIVE_POWER_BINDING", "null")) != bound:
            raise RuntimeError("preparation inherited binding differs")
    validate_sources(pins)
    return root


def frame_record(path, began, wall, raw_began=None):
    raw = path.read_bytes()
    if len(raw) < 33 or raw[:8] != b"\x89PNG\r\n\x1a\n" or raw[12:16] != b"IHDR":
        raise RuntimeError("preparation frame is not PNG")
    width, height = struct.unpack(">II", raw[16:24])
    if not width or not height:
        raise RuntimeError("preparation frame has empty dimensions")
    captured_raw = raw_clock() if raw_began is None else raw_began
    return dict(path=str(path), sha256=hashlib.sha256(raw).hexdigest(), width=width, height=height,
                captured_monotonic=began, captured_wall=wall,
                captured_raw=captured_raw, expires_raw=captured_raw+60,
                expires_monotonic=began + 60, expires_wall=wall + 60)


def validate_frame(frame, deadline):
    remaining(deadline)
    mono = time.monotonic() - frame["captured_monotonic"]
    if not 0 <= mono < 60 or time.monotonic() >= frame["expires_monotonic"]:
        raise RuntimeError("preparation original local capture expired")
    validate_remote_frame(frame, frame["expires_raw"])


def validate_remote_frame(frame, deadline_raw):
    """Shared clocks must have a system origin, including on macOS Python 3.9.

    Python 3.9's macOS time.monotonic origin is process-local. The producer keeps
    its original local work deadline; only its conservative RAW projection and
    the original capture's RAW/wall deadlines may cross a process boundary.
    """
    if (type(deadline_raw) not in (int, float) or not math.isfinite(deadline_raw)
            or raw_clock() >= deadline_raw):
        raise RuntimeError("preparation original shared deadline expired")
    wall = time.time() - frame["captured_wall"]
    raw = raw_clock() - frame["captured_raw"]
    if (not 0 <= wall < 60 or not 0 <= raw < 60 or abs(raw - wall) > 1
            or raw_clock() >= frame["expires_raw"] or time.time() >= frame["expires_wall"]
            or sha(frame["path"]) != frame["sha256"]):
        raise RuntimeError("preparation original capture expired or changed")


def peer_pid(connection):
    if sys.platform == "darwin":
        return struct.unpack("i", connection.getsockopt(0, 2, 4))[0]  # LOCAL_PEERPID.
    return struct.unpack("3i", connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))[0]


def validate_peer(connection, ready, ready_path, ready_sha, pins):
    pid = peer_pid(connection)
    sampled = process_identity(pid)
    argv, marker = process_arguments(pid)
    expected = [ready["client_kernel"], "-B", pins["client"]["path"],
                str(ready_path), ready_sha]
    if (pid <= 1 or pid == os.getpid() or sampled is None
            or sampled.get("executable") != ready["client_kernel"]
            or argv != expected or marker != ready["binding"]["case_id"]
            or process_identity(pid) != sampled):
        raise RuntimeError("preparation sender PID/birth/CLI/source differs")
    validate_sources(pins)
    return sampled


def receive(server, ready, ready_path, pins, deadline, revalidate):
    """One admission and one envelope. No retry after a partial/invalid sender."""
    frame = ready["frame"]
    end = min(deadline, frame["expires_monotonic"])
    while True:
        readable, _, _ = select.select([server], [], [], min(.1, remaining(end)))
        if readable:
            break
    connection, _ = server.accept()
    with connection:
        delivery_end = min(end, time.monotonic() + 8)
        peer = validate_peer(connection, ready, ready_path, sha(ready_path), pins)
        revalidate()
        validate_frame(frame, end)
        write_new(ready_path.parent / ("peer-" + ready["stage"] + ".json"),
                  dict(binding=ready["binding"], identity=peer, ready_sha256=sha(ready_path),
                       client_sha256=pins["client"]["sha256"],
                       argv=[ready["client_kernel"], "-B", pins["client"]["path"], str(ready_path), sha(ready_path)]))
        connection.settimeout(remaining(delivery_end))
        connection.sendall(b"READY\n")
        # Eight seconds begins before READY, not on every recv.
        raw = bytearray()
        while True:
            connection.settimeout(remaining(delivery_end))
            part = connection.recv(min(4097 - len(raw), 1024))
            if not part:
                break
            raw.extend(part)
            if len(raw) > 4096:
                raise RuntimeError("preparation envelope exceeds bound")
        try:
            payload = json.loads(raw)
        finally:
            raw.clear()
        keys = {"frame_sha256", "approval"} | ({"credential"} if ready["stage"] == "before" else set())
        expected = "ci-us-empty-password" if ready["stage"] == "before" else "ci-desktop"
        if (not isinstance(payload, dict) or set(payload) != keys
                or payload.get("frame_sha256") != frame["sha256"] or payload.get("approval") != expected
                or ("credential" in keys and (not isinstance(payload["credential"], str)
                    or not payload["credential"] or not payload["credential"].isascii()))):
            raise RuntimeError("preparation Root frame approval differs")
        revalidate()
        validate_frame(frame, end)
        if process_identity(peer["pid"]) != peer:
            raise RuntimeError("preparation sender changed before admission")
        connection.sendall(b"ACCEPTED\n")
        return payload


def make_before_app(binding, *, helper_path, helper_sha256, client_path, client_sha256,
                    source_sha256, app_source, package_path, client_kernel, login_target, ops=None):
    """Construct an inert callback; only the actual runner supplies its case FD."""
    if (not isinstance(binding, dict) or set(binding) != set(FIXED_BINDINGS)
            or any(binding[key] in (None, "", {}) for key in FIXED_BINDINGS)):
        raise ValueError("preparation fixed binding is incomplete")
    pins = dict(actor=dict(path=str(Path(__file__).resolve()), sha256=source_sha256),
                client=dict(path=str(Path(client_path).absolute()), sha256=client_sha256),
                helper=dict(path=str(Path(helper_path).absolute()), sha256=helper_sha256))
    native = ops if ops is not None else NativeOperations(helper_path)
    def before_app(context, prepare_deadline):
        deadline = min(prepare_deadline, time.monotonic() + 120)
        remaining(deadline)
        # Sample RAW first, so this one projection cannot extend the original
        # local deadline by the sampling interval. Never rebuild it per stage.
        deadline_raw = raw_clock() + (deadline - time.monotonic())
        root = validate_context(context, binding, pins, app_source, package_path)
        evidence = Path(context["environment"]["ZWAI_NATIVE_EVIDENCE"]) / "prepare-login"
        evidence.mkdir(mode=0o700)
        root_identity = private_directory(root)
        def current():
            if validate_context(context, binding, pins, app_source, package_path) != root:
                raise RuntimeError("preparation case root changed")
            if private_directory(root) != root_identity:
                raise RuntimeError("preparation case root inode changed")
            value = native.session(context, deadline)
            check_session(value, binding["session"])
            remaining(deadline)
            return value
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        endpoint = root / "prepare-login.sock"
        endpoint_inode = None
        input_attempted = False
        try:
            with rooted(root, root_identity):
                server.bind(endpoint.name)
            endpoint_inode = endpoint.lstat().st_ino
            os.chmod(endpoint, 0o600)
            server.listen(1)
            first = current()
            stages = ("before", "after") if session_lock(first) is True else ("after",)
            for stage in stages:
                observed = current()
                if stage == "before" and session_lock(observed) is not True:
                    raise RuntimeError("preparation login state changed before input frame")
                if stage == "after" and session_lock(observed) is True:
                    raise RuntimeError("SDK locked flag vetoes App admission")
                began, wall, raw_began = time.monotonic(), time.time(), raw_clock()
                image = evidence / (stage + ".png")
                native.capture(context, image, deadline)
                frame = frame_record(image, began, wall, raw_began)
                if (frame["width"], frame["height"]) != (login_target["width"], login_target["height"]):
                    raise RuntimeError("preparation display geometry differs")
                ready = dict(schema=1, stage=stage, binding=context["power_binding"],
                             actor=process_identity(os.getpid()), root=str(root), root_identity=root_identity,
                             socket=endpoint.name, socket_inode=endpoint.lstat().st_ino,
                             pins=pins, client_kernel=client_kernel, frame=frame,
                             prepare_deadline=deadline, prepare_deadline_raw=deadline_raw, session=observed)
                ready_path = evidence / ("ready-" + stage + ".json")
                write_new(ready_path, ready)
                def revalidate():
                    value = current()
                    state = session_lock(value)
                    if (stage == "before" and state is not True) or (stage == "after" and state is True):
                        raise RuntimeError("preparation session lock changed")
                payload = receive(server, ready, ready_path, pins, deadline, revalidate)
                if stage == "before":
                    try:
                        # Re-check the same capture immediately before this single input.
                        revalidate()
                        validate_frame(frame, deadline)
                        target = dict(login_target, session=binding["session"],
                                      captured_raw=frame["captured_raw"], captured_wall=frame["captured_wall"])
                        write_new(evidence / "input-launch-attempt.json",
                                  dict(binding=context["power_binding"], frame_sha256=frame["sha256"],
                                       helper=pins["helper"], credential_recorded=False,
                                       native_input_confirmed=None))
                        result = native.input(context, target, payload["credential"], deadline)
                        input_attempted = True
                        write_new(evidence / "input-result.json", result)
                    finally:
                        payload.clear()
                else:
                    payload.clear()
                    revalidate()
                    final = current()
                    if session_lock(final) is True:
                        raise RuntimeError("SDK locked flag vetoes final App publication")
                    validate_frame(frame, deadline)
                    write_new(evidence / "approval-after.json", dict(binding=context["power_binding"],
                              frame_sha256=frame["sha256"], sdk_locked=session_lock(final),
                              input_attempted=input_attempted, approval="ci-desktop"))
                    consumed = False
                    def final_admission():
                        nonlocal consumed
                        if consumed:
                            raise RuntimeError("preparation final admission was already consumed")
                        consumed = True
                        # Power takeover verification may have consumed time since
                        # approval. Keep this exact frame's absolute expiry and run
                        # the SDK read before the last clock/byte check.
                        latest = current()
                        if session_lock(latest) is True:
                            raise RuntimeError("SDK locked flag vetoes App publication")
                        validate_frame(frame, deadline)
                        write_new(evidence / "admitted.json", dict(binding=context["power_binding"],
                                  frame_sha256=frame["sha256"], sdk_locked=session_lock(latest),
                                  input_attempted=input_attempted, approval="ci-desktop"))
                        validate_frame(frame, deadline)
                    return final_admission
        finally:
            primary = sys.exc_info()[0] is not None
            try:
                server.close()
                if endpoint_inode is not None:
                    info = endpoint.lstat()
                    if info.st_ino != endpoint_inode or not stat.S_ISSOCK(info.st_mode):
                        raise RuntimeError("preparation own endpoint was replaced before cleanup")
                    endpoint.unlink()
            except (OSError, RuntimeError):
                context["receipt"]["prepare_login_endpoint_cleanup_failed"] = True
                if not primary:
                    raise
    return before_app
