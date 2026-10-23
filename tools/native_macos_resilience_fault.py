"""Case-scoped public-OS fault QA, never an arbitrary PID signal interface.

Four lifecycle observations support case attribution; they cannot prove the
absence of arbitrary IPC clients. A runner cleanup hook must survive scenario
SIGKILL and call cleanup_case before removing the private case directory.
"""
from contextlib import contextmanager
import copy
import ctypes
import errno
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import platform
import re
import signal
import stat
import subprocess
import threading
import time

TARGETS = "renderer-fault-targets.jsonl"
CLEANUP = "renderer-fault-cleanup.jsonl"
COPIES = "renderer-fault-copies.jsonl"
MAX_SECONDS = 90
MAX_TARGETS = 3
NAMES = {"gpu": "com.apple.WebKit.GPU", "renderer": "com.apple.WebKit.WebContent"}
BOUNDARIES = ("before-quit", "after-quit", "before-reopen", "after-reopen")


def require_guest():
    if platform.system() != "Darwin":
        raise RuntimeError("fault QA requires a macOS virtual machine")
    present = subprocess.check_output(["/usr/sbin/sysctl", "-n", "kern.hv_vmm_present"], timeout=1)
    if present.strip() != b"1":
        raise RuntimeError("physical host refused for native faults")


def _identity(value):
    if (not isinstance(value, dict) or set(value) != {"pid", "birth", "executable"}
            or type(value["pid"]) is not int or not 1 < value["pid"] <= 2147483647
            or not isinstance(value["birth"], (tuple, list)) or len(value["birth"]) != 2
            or any(type(number) is not int for number in value["birth"])
            or value["birth"][0] <= 0 or not 0 <= value["birth"][1] < 1000000
            or not isinstance(value["executable"], str)
            or "\x00" in value["executable"]
            or not value["executable"].startswith("/")
            or str(PurePosixPath(value["executable"])) != value["executable"]
            or ".." in PurePosixPath(value["executable"]).parts):
        raise RuntimeError("process identity is missing or malformed")
    return dict(pid=value["pid"], birth=list(value["birth"]), executable=value["executable"])


def _case_app(case_id, private_app):
    app = PurePosixPath(str(private_app))
    if (not re.fullmatch(r"[0-9a-f]{32}", case_id) or not app.is_absolute() or app.name != "zwai.app"
            or ".." in app.parts or not app.parent.name.startswith("zwai-native-" + case_id + "-")):
        raise RuntimeError("fault proof does not identify its private case app")
    return app


def _service_path(kind, path):
    name = NAMES[kind]
    suffix = "/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/" + name + ".xpc/Contents/MacOS/" + name
    return path in (suffix, "/System/Volumes/Preboot/Cryptexes/OS" + suffix)


def _signature(observation, name):
    rows = observation.get("signature", [])
    fields = dict(line.split("=", 1) for line in rows if isinstance(line, str) and "=" in line)
    if (observation.get("signature_exit") != 0 or len(fields) != len(rows)
            or fields.get("Identifier") != name or not fields.get("TeamIdentifier")
            or set(fields) - {"Identifier", "TeamIdentifier", "CDHash"}):
        raise RuntimeError("service code-signature evidence is missing or changed")
    return fields


def _cohort(row, case_id, app):
    if row.get("case_id") != case_id or not isinstance(row.get("guest_os"), str) or not row["guest_os"]:
        raise RuntimeError("process snapshot belongs to another or unknown guest case")
    if type(row.get("at")) not in (int, float) or not math.isfinite(row["at"]):
        raise RuntimeError("process observation time is missing")
    observations, services, seen = {}, {}, set()
    for observed in row.get("observations", []):
        ident = _identity(observed.get("identity"))
        if ident["pid"] in observations or observed.get("procinfo_exit") != 0:
            raise RuntimeError("process observation is duplicated or unavailable")
        fields = observed.get("procinfo_fields", [])
        if "program path = " + ident["executable"] not in fields or "pid = " + str(ident["pid"]) not in fields:
            raise RuntimeError("public process diagnostics disagree with the process identity")
        observations[ident["pid"]] = observed
    for process in row.get("process_rows", []):
        pid, name = process.get("pid"), process.get("executable_name", "")
        if pid in seen:
            raise RuntimeError("process inventory contains a duplicate PID")
        seen.add(pid)
        if name.startswith("com.apple.WebKit.WebContent") or name.startswith("com.apple.WebKit.GPU"):
            if name not in NAMES.values() or name in services or pid not in observations:
                raise RuntimeError("WebKit cohort is incomplete or has additional clients")
            services[name] = observations[pid]
        elif process.get("ownership") != "case" or name != "zwai":
            raise RuntimeError("process inventory includes another application client")
    service_pids = {value["identity"]["pid"] for value in services.values()}
    shell = row.get("shell")
    if shell is not None:
        shell = _identity(shell)
        path = PurePosixPath(shell["executable"])
        if (not path.is_relative_to(app.parent) or path.parts[-3:] != ("Contents", "MacOS", "zwai")
                or path.parent.parent.parent.suffix != ".app" or shell["pid"] not in observations or shell["pid"] not in seen
                or observations[shell["pid"]]["identity"] != shell):
            raise RuntimeError("shell identity does not belong to the private case")
        _signature(observations[shell["pid"]], "app.zwai.desktop")
    if set(observations) != service_pids | ({shell["pid"]} if shell else set()):
        raise RuntimeError("unexplained process observations are present")
    for name, observation in services.items():
        kind = next(kind for kind, value in NAMES.items() if value == name)
        ident = _identity(observation["identity"])
        if not shell or not _service_path(kind, ident["executable"]) or ident["birth"] < shell["birth"]:
            raise RuntimeError("service path or process birth cannot belong to this shell")
        if ident["birth"][0] + ident["birth"][1] / 1000000 > row["at"]:
            raise RuntimeError("service was not alive at its observation")
        _signature(observation, name)
        version = observation.get("framework_version", {})
        if version.get("CFBundleIdentifier") != "com.apple.WebKit" or not version.get("CFBundleVersion"):
            raise RuntimeError("system WebKit build identity is missing")
    return shell, services


def select_target(boundaries, current, kind, *, case_id, private_app, now=None):
    """Select from this fresh case's controlled empty-cohort lifecycle only."""
    app = _case_app(case_id, private_app)
    now = time.time() if now is None else now
    if kind not in NAMES or set(boundaries) != set(BOUNDARIES):
        raise RuntimeError("all four lifecycle boundaries and one supported fault kind are required")
    rows = [boundaries[name] for name in BOUNDARIES] + [current]
    times = [row.get("at", float("nan")) for row in rows]
    if (any(type(value) not in (int, float) or not math.isfinite(value) for value in times)
            or any(first >= second for first, second in zip(times, times[1:]))
            or not 0 <= now - times[-1] <= 5 or now - times[0] > 120):
        raise RuntimeError("lifecycle observations are stale or out of order")
    if len({row.get("guest_os") for row in rows}) != 1:
        raise RuntimeError("guest OS identity changed during the proof")
    cohorts = [_cohort(row, case_id, app) for row in rows]
    old_shell, old = cohorts[0]
    new_shell, new = cohorts[3]
    if (not old_shell or not new_shell or old_shell["pid"] == new_shell["pid"]
            or any(shell or services for shell, services in cohorts[1:3])
            or any(len(services) != 2 for _, services in (cohorts[0], cohorts[3], cohorts[4]))
            or cohorts[4] != cohorts[3]):
        raise RuntimeError("case shutdown/reopen did not isolate one new WebKit cohort")
    if new_shell["birth"][0] + new_shell["birth"][1] / 1000000 <= times[2]:
        raise RuntimeError("successor shell predates the observed empty cohort")
    old_pids = {old_shell["pid"]} | {value["identity"]["pid"] for value in old.values()}
    new_pids = {new_shell["pid"]} | {value["identity"]["pid"] for value in new.values()}
    if old_pids & new_pids or any(process.get("pid") in old_pids for process in current["process_rows"]):
        raise RuntimeError("old process remains or a PID was reused")
    for name in NAMES.values():
        if old[name]["signature"] != new[name]["signature"] or old[name]["framework_version"] != new[name]["framework_version"]:
            raise RuntimeError("system service code identity changed during the proof")
    shell_signatures = [next(observation["signature"] for observation in row["observations"]
                             if observation["identity"]["pid"] == shell["pid"])
                        for row, (shell, _) in zip(rows, cohorts) if shell]
    if any(value != shell_signatures[0] for value in shell_signatures):
        raise RuntimeError("private shell code signature changed during the proof")
    return dict(schema=1, case_id=case_id, private_app=str(app), kind=kind,
                target=copy.deepcopy(new[NAMES[kind]]["identity"]), shell=copy.deepcopy(new_shell), recorded_at=now)


def process_identity(pid):
    """Public libproc PID+microsecond birth+path; Linux is only for owned-child tests."""
    if platform.system() == "Linux":
        try:
            text = Path("/proc", str(pid), "stat").read_text().rsplit(") ", 1)[1].split()
            return dict(pid=pid, birth=[int(text[19]), 0], executable=os.readlink("/proc/" + str(pid) + "/exe"))
        except FileNotFoundError:
            return None
    class BSDInfo(ctypes.Structure):
        _fields_ = [("ids", ctypes.c_uint32 * 12), ("comm", ctypes.c_char * 16), ("name", ctypes.c_char * 32),
                    ("group", ctypes.c_uint32 * 5), ("nice", ctypes.c_int32),
                    ("seconds", ctypes.c_uint64), ("microseconds", ctypes.c_uint64)]
    lib = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
    lib.proc_pidinfo.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_uint64, ctypes.c_void_p, ctypes.c_int]
    lib.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
    info, path = BSDInfo(), ctypes.create_string_buffer(4096)
    if lib.proc_pidinfo(pid, 3, 0, ctypes.byref(info), ctypes.sizeof(info)) != ctypes.sizeof(info) or lib.proc_pidpath(pid, path, len(path)) <= 0:
        if info.ids[1] == 5:
            return None
        if ctypes.get_errno() == errno.ESRCH:
            return None
        raise RuntimeError("public OS process identity is unavailable")
    return dict(pid=pid, birth=[info.seconds, info.microseconds], executable=os.fsdecode(path.value))


def _state(pid):
    result = subprocess.run(["/bin/ps", "-p", str(pid), "-o", "stat="], capture_output=True, text=True, timeout=1)
    return result.stdout.strip() if result.returncode == 0 else None


def _resume_identity(expected, identity):
    expected = _identity(expected)
    current = identity(expected["pid"])
    if current is None:
        return dict(identity=expected, outcome="exited")
    if _identity(current) != expected:
        raise RuntimeError("process birth or path changed; resume refused")
    try:
        os.kill(expected["pid"], signal.SIGCONT)
    except ProcessLookupError:
        return dict(identity=expected, outcome="exited")
    deadline = time.monotonic() + 1
    while True:
        current = identity(expected["pid"])
        if current is None:
            return dict(identity=expected, outcome="exited")
        if _identity(current) != expected:
            raise RuntimeError("process identity changed during resume verification")
        state = _state(expected["pid"])
        if state and not state.startswith("T"):
            return dict(identity=expected, outcome="exited" if state.startswith("Z") else "resumed")
        if time.monotonic() >= deadline:
            raise RuntimeError("target still stopped or its state is unavailable after resume")
        time.sleep(.01)


def _append(path, row):
    data = (json.dumps(row, sort_keys=True, allow_nan=False) + "\n").encode()
    descriptor = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "ab") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


def _validated_target(row, case_id, private_app):
    app = _case_app(case_id, private_app)
    if (set(row) != {"schema", "case_id", "private_app", "kind", "target", "shell", "recorded_at"}
            or row["schema"] != 1 or row["case_id"] != case_id or row["private_app"] != str(app)
            or row["kind"] not in NAMES or type(row["recorded_at"]) not in (int, float) or not math.isfinite(row["recorded_at"])):
        raise RuntimeError("fault receipt is not bound to this case and app")
    target, shell = _identity(row["target"]), _identity(row["shell"])
    if (not _service_path(row["kind"], target["executable"]) or target["birth"] < shell["birth"]
            or not PurePosixPath(shell["executable"]).is_relative_to(app.parent)
            or PurePosixPath(shell["executable"]).parts[-3:] != ("Contents", "MacOS", "zwai")):
        raise RuntimeError("fault receipt target path or lineage is invalid")
    return row


def _append_target(evidence, target):
    evidence = Path(evidence)
    _validated_target(target, target["case_id"], target["private_app"])
    path = evidence / TARGETS
    existing = [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []
    if len(existing) >= MAX_TARGETS or any(row["target"] == target["target"] for row in existing):
        raise RuntimeError("fault target already claimed or the case fault budget is exhausted")
    for row in existing:
        _validated_target(row, target["case_id"], target["private_app"])
    _append(path, target)


def cleanup_resume(target, identity=process_identity):
    require_guest()
    _validated_target(target, target["case_id"], target["private_app"])
    return _resume_identity(target["target"], identity)


def _read_rows(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 16384:
        raise RuntimeError("fault cleanup receipt is missing or invalid")
    rows = [json.loads(line) for line in path.read_text().splitlines()]
    if not 0 < len(rows) <= MAX_TARGETS:
        raise RuntimeError("fault cleanup record count is invalid")
    return rows


def _copy_path(path, case_id, app):
    app = _case_app(case_id, app)
    path = Path(path)
    cache = Path(str(app.parent)) / "home/Library/Caches/zwai"
    if (not path.is_absolute() or ".." in path.parts or not path.is_relative_to(cache)
            or path.parts[-4:] != ("zwai.app", "Contents", "MacOS", "zwai")
            or path.parents[3].parent != cache
            or not path.parents[3].name.startswith("desktop-recovery-")):
        raise RuntimeError("permission fault target is not a case-owned recovery copy")
    return path


def _hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def _copy_matches(row):
    path = Path(row["path"])
    if any(parent.is_symlink() for parent in (path, *path.parents)):
        raise RuntimeError("recovery copy path gained a symlink")
    if not path.exists():
        return False
    info = path.lstat()
    marker = path.parents[3] / "session"
    if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or (info.st_dev, info.st_ino) != (row["dev"], row["ino"])
            or marker.is_symlink() or not marker.is_file() or marker.stat().st_size != 16
            or marker.read_text() != row["session_marker"] or _hash(path) != row["sha256"]):
        raise RuntimeError("recovery copy ownership or bytes changed")
    return True


def _validated_copy(row, case_id, app):
    if (set(row) != {"schema", "case_id", "private_app", "path", "session_marker", "dev", "ino", "mode", "sha256"}
            or row["schema"] != 1 or row["case_id"] != case_id or row["private_app"] != str(app)
            or not re.fullmatch(r"[0-9a-f]{16}", row["session_marker"])
            or not re.fullmatch(r"[0-9a-f]{64}", row["sha256"])
            or any(type(row[key]) is not int or row[key] < 0 for key in ("dev", "ino", "mode"))
            or row["mode"] > 0o7777 or not row["mode"] & 0o111):
        raise RuntimeError("permission fault receipt does not belong to this case")
    _copy_path(row["path"], case_id, app)
    return row


def _restore_copy(row):
    if not _copy_matches(row):
        return dict(path=row["path"], outcome="removed")
    os.chmod(row["path"], row["mode"], follow_symlinks=False)
    if not _copy_matches(row) or stat.S_IMODE(os.lstat(row["path"]).st_mode) != row["mode"]:
        raise RuntimeError("recovery copy mode restoration was not verified")
    return dict(path=row["path"], outcome="restored")


def cleanup_case(evidence, case_id, app, *, require_exit=False):
    require_guest()
    _case_app(case_id, app)
    path = Path(evidence) / TARGETS
    rows = _read_rows(path) if os.path.lexists(path) else []
    copies_path = Path(evidence) / COPIES
    copies = _read_rows(copies_path) if os.path.lexists(copies_path) else []
    if not rows and not copies:
        raise RuntimeError("fault cleanup has no owned targets")
    for row in rows:
        _validated_target(row, case_id, app)
    for row in copies:
        _validated_copy(row, case_id, app)
    result = dict(case_id=case_id, private_app=str(app), complete=False, targets=[], copies=[], require_exit=require_exit, at=time.time())
    try:
        for row in rows:
            if require_exit:
                deadline = time.monotonic() + 2
                while True:
                    identity = process_identity(row["target"]["pid"])
                    if identity is None or identity != row["target"]:
                        result["targets"].append(dict(identity=row["target"], outcome="exited"))
                        break
                    cleanup_resume(row, process_identity)
                    if time.monotonic() >= deadline:
                        raise RuntimeError("case-owned fault target did not exit; private data retained")
                    time.sleep(.02)
            else:
                result["targets"].append(cleanup_resume(row, process_identity))
        for row in copies:
            result["copies"].append(_restore_copy(row))
        result["complete"] = True
        return result
    finally:
        _append(Path(evidence) / CLEANUP, result)


@contextmanager
def _pause(expected, identity, seconds):
    if not math.isfinite(seconds) or not 0 < seconds <= MAX_SECONDS:
        raise ValueError("fault duration must be positive and at most ninety seconds")
    expected = _identity(expected)
    before = _state(expected["pid"])
    if identity(expected["pid"]) != expected or not before or before.startswith("T"):
        raise RuntimeError("target changed or was already stopped; fault refused")
    result, lock = dict(stopped=False, expired=False, resume=None, before_stat=before, stopped_stat=None), threading.Lock()
    def resume(expired=False):
        with lock:
            if result["resume"] is None:
                result["expired"] |= expired
                result["resume"] = _resume_identity(expected, identity)
    def expire():
        try:
            resume(True)
        except (RuntimeError, OSError):
            result["resume_refused"] = True
    timer = threading.Timer(seconds, expire)
    timer.daemon = True
    armed = False
    try:
        armed = True
        os.kill(expected["pid"], signal.SIGSTOP)
        timer.start()
        deadline = time.monotonic() + min(seconds, 1)
        while True:
            if identity(expected["pid"]) != expected:
                raise RuntimeError("target identity changed while verifying its stopped state")
            observed = _state(expected["pid"])
            if observed and observed.startswith("T") and not result["expired"]:
                result.update(stopped=True, stopped_stat=observed)
                break
            if time.monotonic() >= deadline or result["expired"]:
                raise RuntimeError("target did not enter a verified stopped state within the fault bound")
            time.sleep(.01)
        yield result
    finally:
        timer.cancel()
        if armed:
            resume()


@contextmanager
def stall(boundaries, kind, *, case_id, private_app, evidence, snapshot, seconds):
    require_guest()
    selected = select_target(boundaries, snapshot(), kind, case_id=case_id, private_app=private_app)
    subprocess.run(["/usr/bin/codesign", "--verify", "--strict", "-R", "anchor apple", selected["target"]["executable"]],
                   check=True, capture_output=True, timeout=2)
    current = select_target(boundaries, snapshot(), kind, case_id=case_id, private_app=private_app)
    if any(current[key] != selected[key] for key in ("target", "shell", "private_app", "case_id", "kind")):
        raise RuntimeError("fault target changed during signature verification")
    for row in boundaries["before-quit"]["observations"]:
        if process_identity(row["identity"]["pid"]) is not None:
            raise RuntimeError("an old process is still alive or its PID was reused")
    if process_identity(selected["shell"]["pid"]) != selected["shell"]:
        raise RuntimeError("case shell changed before fault admission")
    _append_target(evidence, current)
    with _pause(current["target"], process_identity, seconds) as receipt:
        yield dict(target=current, state=receipt)


def _live_executables():
    # A 0700 case root and nlink=1 copy are inaccessible through another UID.
    # Same-UID unreadable identities remain a refusal, never a silent skip.
    output = subprocess.check_output(["/bin/ps", "-axo", "pid=,uid="], text=True, timeout=2)
    for line in output.splitlines():
        pid, uid = map(int, line.split())
        if pid > 1 and uid == os.getuid():
            identity = process_identity(pid)
            if identity is not None:
                yield identity


@contextmanager
def deny_successor_launch(*, case_id, private_app, evidence, session_id):
    """Remove execute bits only from the unused private recovery copy, then restore."""
    require_guest()
    app = _case_app(case_id, private_app)
    root = Path(str(app.parent))
    root_info = root.lstat()
    if (root.is_symlink() or not stat.S_ISDIR(root_info.st_mode)
            or stat.S_IMODE(root_info.st_mode) != 0o700 or root_info.st_uid != os.getuid()
            or not re.fullmatch(r"[0-9a-f]{16}", session_id)):
        raise RuntimeError("permission fault requires its private case and current ready session")
    cache = Path(str(app.parent)) / "home/Library/Caches/zwai"
    candidates = []
    for candidate in cache.glob("desktop-recovery-*/zwai.app/Contents/MacOS/zwai"):
        marker = candidate.parents[3] / "session"
        if not marker.is_symlink() and marker.is_file() and marker.stat().st_size == 16 and marker.read_text() == session_id:
            candidates.append(candidate)
    if len(candidates) != 1:
        raise RuntimeError("one unused private recovery executable is required")
    path = _copy_path(str(candidates[0]), case_id, private_app)
    info, marker = path.lstat(), path.parents[3] / "session"
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or marker.is_symlink() or not marker.is_file():
        raise RuntimeError("recovery executable or owner marker is not a regular private file")
    row = dict(schema=1, case_id=case_id, private_app=str(private_app), path=str(path),
               session_marker=session_id, dev=info.st_dev, ino=info.st_ino,
               mode=stat.S_IMODE(info.st_mode), sha256=_hash(path))
    _validated_copy(row, case_id, private_app)
    if not _copy_matches(row):
        raise RuntimeError("recovery copy disappeared before the permission fault")
    for identity in _live_executables():
        live = Path(identity["executable"])
        try:
            used = live.stat()
        except FileNotFoundError:
            if process_identity(identity["pid"]) is None:
                continue
            raise RuntimeError("live executable identity cannot be verified")
        if (used.st_dev, used.st_ino) == (info.st_dev, info.st_ino) or live == path:
            raise RuntimeError("permission fault would modify a live executable")
    copies = Path(evidence) / COPIES
    if copies.exists():
        raise RuntimeError("permission fault was already claimed")
    _append(copies, row)
    try:
        if not _copy_matches(row):
            raise RuntimeError("recovery executable changed before chmod")
        os.chmod(path, row["mode"] & ~0o111, follow_symlinks=False)
        if not _copy_matches(row) or os.access(path, os.X_OK):
            raise RuntimeError("private successor launch failure was not established")
        yield row
    finally:
        _restore_copy(row)
