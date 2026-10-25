"""Finite setup power transfer within native_macos_case's existing ownership.

Power prevents an avoidable handoff gap; it is never evidence of an unlocked
session. Native reads run only in the guest, under the caller's original budget.
"""
import ctypes
import fcntl
import json
import math
import os
from pathlib import Path
import plistlib
import re
import signal
import stat
import subprocess
import sys
import time
import uuid

from native_macos_resilience_fault import process_identity


CAFFEINATE = "/usr/bin/caffeinate"
PASSIVE_TYPES = frozenset(("PreventUserIdleDisplaySleep", "PreventUserIdleSystemSleep"))
SETUP_TYPES = PASSIVE_TYPES | {"UserIsActive"}


def remaining(deadline):
    if not isinstance(deadline, (int, float)) or not math.isfinite(deadline):
        raise RuntimeError("power handoff deadline is invalid")
    left = deadline - time.monotonic()
    if left <= 0:
        raise RuntimeError("power handoff original deadline expired")
    return left


def raw_clock():
    return time.clock_gettime(time.CLOCK_MONOTONIC_RAW)


def portable_prepare_deadline(deadline):
    # Python 3.9 macOS monotonic origins differ between processes. Sample RAW
    # first and local time second so this one projection cannot add time.
    raw = raw_clock()
    return raw + remaining(deadline)


def inherited_prepare_deadline(environment):
    # Convert only at the scenario entrance; subsequent phases retain this
    # local result. Local first/RAW second makes the reverse mapping conservative.
    local = time.monotonic()
    try:
        raw = float(environment["ZWAI_NATIVE_PREPARE_DEADLINE_RAW"])
    except (KeyError, TypeError, ValueError) as error:
        raise RuntimeError("original portable preparation deadline required") from error
    left = raw - raw_clock()
    if not math.isfinite(left) or left <= 0:
        raise RuntimeError("original portable preparation deadline expired or invalid")
    return local + left


def identity(value):
    if (not isinstance(value, dict) or set(value) != {"pid", "birth", "executable"}
            or type(value["pid"]) is not int or value["pid"] <= 0
            or not isinstance(value["birth"], list) or len(value["birth"]) != 2
            or any(type(part) is not int or part < 0 for part in value["birth"])
            or not isinstance(value["executable"], str) or not value["executable"].startswith("/")):
        raise RuntimeError("power handoff process identity is malformed")
    return value


def await_identity(process, executable, deadline):
    """A direct child's birth can precede exec; never freeze that transient path."""
    birth = None
    while True:
        remaining(deadline)
        if process.poll() is not None:
            raise RuntimeError("power handoff child exited before executable binding")
        current = process_identity(process.pid)
        if current is not None:
            identity(current)
            if birth is not None and current["birth"] != birth:
                raise RuntimeError("power handoff child birth changed during exec")
            birth = current["birth"]
            if current["executable"] == executable:
                return current
        time.sleep(min(.01, remaining(deadline)))


def assertion_types(rows):
    if not isinstance(rows, list):
        raise RuntimeError("power assertion result is not an array")
    types = set()
    for row in rows:
        if (not isinstance(row, dict) or type(row.get("AssertLevel")) is not int
                or row["AssertLevel"] != 255 or not isinstance(row.get("AssertType"), str)):
            raise RuntimeError("power assertion has an unknown type or inactive level")
        types.add(row["AssertType"])
    return frozenset(types)


def _snapshot(pid):
    # Public IOPM API: PID CFNumber keys map to arrays of active assertions.
    from native_macos_case import require_guest
    require_guest()
    cf = ctypes.CDLL("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
    io = ctypes.CDLL("/System/Library/Frameworks/IOKit.framework/IOKit")
    ptr = ctypes.c_void_p
    io.IOPMCopyAssertionsByProcess.argtypes = [ctypes.POINTER(ptr)]
    io.IOPMCopyAssertionsByProcess.restype = ctypes.c_int
    cf.CFNumberCreate.argtypes, cf.CFNumberCreate.restype = [ptr, ctypes.c_int, ptr], ptr
    cf.CFDictionaryGetValue.argtypes, cf.CFDictionaryGetValue.restype = [ptr, ptr], ptr
    cf.CFGetTypeID.argtypes, cf.CFGetTypeID.restype = [ptr], ctypes.c_ulong
    cf.CFArrayGetTypeID.restype = cf.CFDictionaryGetTypeID.restype = ctypes.c_ulong
    cf.CFPropertyListCreateData.argtypes = [ptr, ptr, ctypes.c_long, ctypes.c_ulong, ptr]
    cf.CFPropertyListCreateData.restype = ptr
    cf.CFDataGetLength.argtypes, cf.CFDataGetLength.restype = [ptr], ctypes.c_long
    cf.CFDataGetBytePtr.argtypes, cf.CFDataGetBytePtr.restype = [ptr], ptr
    cf.CFRelease.argtypes = [ptr]
    objects, key, data = ptr(), None, None
    try:
        if io.IOPMCopyAssertionsByProcess(ctypes.byref(objects)) or not objects.value:
            raise RuntimeError("IOPMCopyAssertionsByProcess failed")
        if cf.CFGetTypeID(objects) != cf.CFDictionaryGetTypeID():
            raise RuntimeError("IOPM assertion dictionary has wrong type")
        number = ctypes.c_int(pid)
        key = cf.CFNumberCreate(None, 9, ctypes.byref(number))  # kCFNumberIntType.
        if not key:
            raise RuntimeError("IOPM PID key allocation failed")
        rows = cf.CFDictionaryGetValue(objects, key)
        if not rows:
            return plistlib.dumps([])
        if cf.CFGetTypeID(rows) != cf.CFArrayGetTypeID():
            raise RuntimeError("IOPM PID assertions have wrong type")
        data = cf.CFPropertyListCreateData(None, rows, 200, 0, None)
        if not data:
            raise RuntimeError("IOPM assertion serialization failed")
        length = cf.CFDataGetLength(data)
        if not 0 < length <= 1024 * 1024:
            raise RuntimeError("IOPM assertion evidence exceeds bound")
        return ctypes.string_at(cf.CFDataGetBytePtr(data), length)
    finally:
        for value in (data, key, objects):
            if value:
                cf.CFRelease(value)


def _save(path, row):
    path = Path(path)
    temporary = path.with_name(path.name + ".pending-" + uuid.uuid4().hex)
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w") as stream:
            json.dump(row, stream, sort_keys=True, allow_nan=False)
            stream.flush()
            os.fsync(stream.fileno())
        # The ACK becomes visible only after all of its bytes are durable. A
        # stale/symlink destination is never replaced or accepted as a retry.
        os.link(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def read_ack(path):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 65536:
                raise RuntimeError("power acknowledgement is not bounded regular data")
            raw = stream.read(65537)
            if len(raw) > 65536:
                raise RuntimeError("power acknowledgement exceeds bound")
        return json.loads(raw)
    except (OSError, ValueError, UnicodeError) as error:
        raise RuntimeError("power acknowledgement is unreadable or malformed") from error


def assertion_child(expected, parent, environment, deadline):
    from native_macos_case import process_identity as arguments
    expected, parent = identity(expected), identity(parent)
    if process_identity(expected["pid"]) != expected or process_identity(parent["pid"]) != parent:
        raise RuntimeError("power assertion child/parent birth changed")
    command = ["/bin/ps", "-p", str(expected["pid"]), "-o", "ppid="]
    prefix = Path(environment["ZWAI_NATIVE_EVIDENCE"]) / ("power-owner-" + uuid.uuid4().hex)
    try:
        result = subprocess.run(command, capture_output=True, timeout=min(1, remaining(deadline)))
        out, err, code = result.stdout, result.stderr, result.returncode
    except subprocess.TimeoutExpired as error:
        out, err, code = error.stdout or b"", error.stderr or b"", None
    prefix.with_suffix(".stdout.log").write_bytes(out)
    prefix.with_suffix(".stderr.log").write_bytes(err)
    _, marker = arguments(expected["pid"])
    parent_argv, parent_marker = arguments(parent["pid"])
    proof = dict(assertion=expected, scenario=parent, parent_pid_raw=out.decode(errors="replace"),
                 exit_code=code, command=command,
                 assertion_marker="unknown" if marker is None else "matching" if marker == environment.get("ZWAI_NATIVE_CASE_ID") else "wrong",
                 scenario_marker="matching" if parent_marker == environment.get("ZWAI_NATIVE_CASE_ID") else "wrong")
    _save(prefix.with_suffix(".json"), proof)
    remaining(deadline)
    if (code != 0 or out.strip() != str(parent["pid"]).encode() or not parent_argv
            or parent_marker != environment.get("ZWAI_NATIVE_CASE_ID")
            or (marker is not None and marker != environment.get("ZWAI_NATIVE_CASE_ID"))
            or process_identity(expected["pid"]) != expected or process_identity(parent["pid"]) != parent):
        raise RuntimeError("power assertion is not this same-birth scenario's case-owned child")
    return proof


def capture_assertions(expected, evidence, deadline, environment, required):
    expected = identity(expected)
    if expected["executable"] != CAFFEINATE or process_identity(expected["pid"]) != expected:
        raise RuntimeError("power assertion owner identity changed")
    prefix = Path(evidence) / ("power-snapshot-" + uuid.uuid4().hex)
    command = [sys.executable, "-B", str(Path(__file__).resolve()), "--snapshot", str(expected["pid"])]
    row = dict(owner=expected, command=command, exit_code=None, timed_out=False)
    try:
        result = subprocess.run(command, env=environment, capture_output=True,
                                timeout=min(2, remaining(deadline)))
        out, err = result.stdout, result.stderr
        row["exit_code"] = result.returncode
    except subprocess.TimeoutExpired as error:
        out, err = error.stdout or b"", error.stderr or b""
        row["timed_out"] = True
    # Preserve streams before decoding, including native or evidence failures.
    prefix.with_suffix(".stdout.plist").write_bytes(out)
    prefix.with_suffix(".stderr.log").write_bytes(err)
    _save(prefix.with_suffix(".json"), row)
    if row["timed_out"] or row["exit_code"] != 0:
        raise RuntimeError("power assertion native snapshot failed")
    remaining(deadline)
    if process_identity(expected["pid"]) != expected:
        raise RuntimeError("power assertion owner changed during snapshot")
    try:
        types = assertion_types(plistlib.loads(out))
    except (ValueError, TypeError, plistlib.InvalidFileException) as error:
        raise RuntimeError("power assertion native snapshot is malformed") from error
    if not set(required) <= types:
        raise RuntimeError("power assertion takeover is incomplete")
    return dict(identity=expected, types=sorted(types), evidence=str(prefix))


def _spawn_registered(command, environment, processes, output, error, *, new_session=True):
    # Defer delivery only until the original case owns the new Popen. Do not
    # inherit a blocked signal mask into the bridge or scenario process.
    pending = []
    handlers = {sig: signal.signal(sig, lambda received, frame: pending.append(received))
                for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        process = subprocess.Popen(command, env=environment, stdout=output, stderr=error,
                                   stdin=subprocess.DEVNULL, start_new_session=new_session)
        processes.append(process)
    finally:
        for sig, handler in handlers.items():
            signal.signal(sig, handler)
    if pending:
        signal.raise_signal(pending[0])
    return process


def start_hold(environment, processes, deadline, name, *, new_session=True):
    left = remaining(deadline)
    evidence = Path(environment["ZWAI_NATIVE_EVIDENCE"])
    with (evidence / (name + ".stdout.log")).open("xb") as out, \
            (evidence / (name + ".stderr.log")).open("xb") as err:
        process = _spawn_registered([CAFFEINATE, "-d", "-i", "-u", "-t", str(math.ceil(left)),
                                     "-w", str(os.getpid())], environment, processes, out, err,
                                    new_session=new_session)
    expected = await_identity(process, CAFFEINATE, deadline)
    return dict(process=process, identity=expected)


def _stop(hold, deadline):
    process, expected = hold["process"], identity(hold["identity"])
    remaining(deadline)
    if process.pid != expected["pid"]:
        raise RuntimeError("power hold Popen does not own this identity")
    if process.poll() is None:
        if process_identity(process.pid) != expected:
            raise RuntimeError("power hold identity changed before release")
        process.terminate()
        process.wait(timeout=min(2, remaining(deadline)))
    if process.poll() is None:
        raise RuntimeError("power hold was not reaped")


def _live_bridge(bridge):
    if bridge["process"].poll() is not None or process_identity(bridge["process"].pid) != bridge["identity"]:
        raise RuntimeError("power bridge exited or changed before receiver acknowledgement")


def validate_case_context(lock, receipt, environment):
    from native_macos_case import LOCK_PATH
    case_id = environment.get("ZWAI_NATIVE_CASE_ID", "")
    if not re.fullmatch(r"[a-f0-9]{32}", case_id) or receipt.get("case_id") != case_id:
        raise RuntimeError("power preparation case identity does not match")
    if receipt.get("cleanup_complete") is not False:
        raise RuntimeError("power preparation needs an incomplete cleanup latch")
    # The real runner uses os.fdopen(), whose .name is the integer FD. Bind
    # that FD to the runner's existing global inode, never an invented filename.
    info, named = os.fstat(lock.fileno()), os.lstat(LOCK_PATH)
    if not stat.S_ISREG(named.st_mode) or (info.st_dev, info.st_ino) != (named.st_dev, named.st_ino):
        raise RuntimeError("power preparation lock identity changed")
    saved = json.loads(os.pread(lock.fileno(), info.st_size, 0))
    if saved.get("case_id") != case_id or saved.get("cleanup_complete") is not False:
        raise RuntimeError("power preparation durable cleanup latch disagrees")
    probe = os.open(LOCK_PATH, os.O_RDWR | os.O_NOFOLLOW)
    try:
        try:
            fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return case_id
        raise RuntimeError("power preparation original exclusion FD is not held")
    finally:
        os.close(probe)


def _binding(value):
    if not isinstance(value, dict) or set(value) != {
            "execution_id", "launch_id", "source_sha", "package_sha256", "phase", "session"}:
        raise RuntimeError("power preparation binding is incomplete")
    for key, length in (("execution_id", 32), ("launch_id", 32), ("source_sha", 40), ("package_sha256", 64)):
        if not isinstance(value[key], str) or not re.fullmatch(r"[a-f0-9]{" + str(length) + "}", value[key]):
            raise RuntimeError("power preparation binding identity is malformed")
    if not isinstance(value["phase"], str) or not value["phase"] or not isinstance(value["session"], dict) or not value["session"]:
        raise RuntimeError("power preparation phase/session binding is missing")
    return json.loads(json.dumps(value, allow_nan=False))


def make_prepare(binding, before_app, prepare_deadline):
    """Adapt the original runner's private prepare/handoff callbacks; no new lock."""
    frozen = _binding(binding)
    if not callable(before_app):
        raise RuntimeError("power preparation actor/admission callback is required")

    def prepare(lock, receipt, environment, processes, scope, work_deadline):
        deadline = min(prepare_deadline, work_deadline)
        remaining(deadline)
        case_id = validate_case_context(lock, receipt, environment)
        bound = dict(frozen, case_id=case_id, runner=identity(process_identity(os.getpid())))
        evidence = Path(environment["ZWAI_NATIVE_EVIDENCE"])
        ack = evidence / "power-takeover.json"
        if ack.exists() or ack.is_symlink():
            raise RuntimeError("power takeover acknowledgement already exists")
        environment.pop("ZWAI_NATIVE_PREPARE_DEADLINE", None)
        environment.update(ZWAI_NATIVE_POWER_BINDING=json.dumps(bound, sort_keys=True, allow_nan=False),
                           ZWAI_NATIVE_PREPARE_DEADLINE_RAW=str(portable_prepare_deadline(deadline)),
                           ZWAI_NATIVE_POWER_ACK=str(ack))
        bridge = start_hold(environment, processes, deadline, "power-bridge")
        capture_assertions(bridge["identity"], evidence, deadline, environment, SETUP_TYPES)
        final_admission = before_app(dict(lock=lock, receipt=receipt, environment=environment,
                                         processes=processes, scope=scope, power_binding=bound), deadline)
        validate_case_context(lock, receipt, environment)
        if not callable(final_admission):
            raise RuntimeError("power preparation requires a final App admission guard")
        _live_bridge(bridge)
        capture_assertions(bridge["identity"], evidence, deadline, environment, SETUP_TYPES)
        remaining(deadline)

        def before_launch():
            validate_case_context(lock, receipt, environment)
            _live_bridge(bridge)
            remaining(deadline)
            final_admission()
            remaining(deadline)

        def handoff(scenario, scenario_deadline):
            end = min(deadline, scenario_deadline)
            validate_case_context(lock, receipt, environment)
            _live_bridge(bridge)
            while not (ack.exists() or ack.is_symlink()):
                remaining(end)
                _live_bridge(bridge)
                if scenario.poll() is not None:
                    raise RuntimeError("scenario exited before power takeover")
                time.sleep(min(.01, remaining(end)))
            try:
                row = read_ack(ack)
                if (set(row) != {"schema", "binding", "scenario", "assertion", "types"}
                        or type(row["schema"]) is not int or row["schema"] != 1 or row["binding"] != bound
                        or row["types"] != sorted(SETUP_TYPES) or scenario.poll() is not None
                        or identity(row["scenario"])["pid"] != scenario.pid
                        or process_identity(scenario.pid) != row["scenario"]):
                    raise RuntimeError("power takeover acknowledgement owner/binding is wrong")
                expected = identity(row["assertion"])
            except (ValueError, TypeError, KeyError) as error:
                raise RuntimeError("power takeover acknowledgement is malformed") from error
            owner_proof = assertion_child(expected, row["scenario"], environment, end)
            capture_assertions(expected, evidence, end, environment, SETUP_TYPES)
            validate_case_context(lock, receipt, environment)
            _live_bridge(bridge)
            _stop(bridge, end)
            remaining(end)
            _save(evidence / "power-handoff.json", dict(binding=bound, scenario=row["scenario"],
                                                       assertion=expected, ownership=owner_proof, bridge_reaped=True))
        from native_macos_case import CasePreparation
        return CasePreparation(before_launch, handoff)
    return prepare


def start_setup_hold(evidence, environment, deadline=None):
    """Scenario startup: retain all three flags until its measured di takes over."""
    inherited = inherited_prepare_deadline(environment)
    deadline = inherited if deadline is None else min(deadline, inherited)
    remaining(deadline)
    bound = json.loads(environment["ZWAI_NATIVE_POWER_BINDING"])
    if bound.get("case_id") != environment.get("ZWAI_NATIVE_CASE_ID") or bound["runner"] != process_identity(os.getppid()):
        raise RuntimeError("scenario power binding does not name its original runner")
    processes = []
    # Keep this child in the original scenario's known process group. System
    # utilities may hide their marker after exec; SIGKILL of the scenario must
    # not make that child disappear from the original runner's cleanup scope.
    hold = start_hold(environment, processes, deadline, "power-scenario-setup", new_session=False)
    hold.update(environment=environment, evidence=Path(evidence), prepare_deadline=deadline)
    capture_assertions(hold["identity"], evidence, deadline, environment, SETUP_TYPES)
    _save(Path(environment["ZWAI_NATIVE_POWER_ACK"]), dict(schema=1, binding=bound,
          scenario=identity(process_identity(os.getpid())), assertion=hold["identity"], types=sorted(SETUP_TYPES)))
    committed = Path(environment["ZWAI_NATIVE_POWER_ACK"]).parent / "power-handoff.json"
    while not (committed.exists() or committed.is_symlink()):
        remaining(deadline)
        if hold["process"].poll() is not None or process_identity(os.getppid()) != bound["runner"]:
            raise RuntimeError("scenario power owner exited before handoff commit")
        time.sleep(min(.01, remaining(deadline)))
    try:
        commit = read_ack(committed)
        if (set(commit) != {"binding", "scenario", "assertion", "ownership", "bridge_reaped"}
                or commit["binding"] != bound or commit["scenario"] != process_identity(os.getpid())
                or commit["assertion"] != hold["identity"] or commit["bridge_reaped"] is not True):
            raise RuntimeError("power handoff commit binding is wrong")
    except (ValueError, KeyError, TypeError) as error:
        raise RuntimeError("power handoff commit is malformed") from error
    remaining(deadline)
    return hold


def release_setup_hold(hold, passive_di, deadline):
    """Never release setup -u until the actual measured -d/-i owner is proven."""
    deadline = min(deadline, hold.get("prepare_deadline", deadline))
    if passive_di.poll() is not None:
        raise RuntimeError("passive di child exited before takeover")
    expected = identity(process_identity(passive_di.pid))
    proof = capture_assertions(expected, hold["evidence"], deadline, hold["environment"], PASSIVE_TYPES)
    if "UserIsActive" in proof["types"]:
        raise RuntimeError("passive di takeover unexpectedly retains UserIsActive")
    _stop(hold, deadline)


if __name__ == "__main__":
    if len(sys.argv) != 3 or sys.argv[1] != "--snapshot" or not sys.argv[2].isdigit():
        raise SystemExit("readonly --snapshot PID expected")
    if not re.fullmatch(r"[a-f0-9]{32}", os.environ.get("ZWAI_NATIVE_CASE_ID", "")):
        raise SystemExit("native case marker required")
    sys.stdout.buffer.write(_snapshot(int(sys.argv[2])))
