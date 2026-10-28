#!/usr/bin/env python3
"""Run one isolated native case inside a macOS VM; never launch on its host.

The scenario owns window/behavior assertions. A zero exit code proves only that
its command succeeded and case processes/data were cleaned up within the budget.
"""
import argparse
import base64
from contextlib import contextmanager
import ctypes
import errno
import fcntl
import json
import math
import os
from pathlib import Path
import platform
import plistlib
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
from typing import Callable, NamedTuple
import uuid


LOCK_PATH = Path("/tmp") / f"zwai-native-macos-{os.getuid()}.lock"


class CasePreparation(NamedTuple):
    before_launch: Callable[[], None]
    handoff: Callable[[subprocess.Popen, float], None]


def require_guest():
    """Fail closed before any GUI launch; the CLI has no bypass switch."""
    if platform.system() != "Darwin":
        raise RuntimeError("native cases require a macOS virtual machine")
    try:
        present = subprocess.check_output(
            ["/usr/sbin/sysctl", "-n", "kern.hv_vmm_present"],
            stderr=subprocess.DEVNULL, timeout=5)
    except (OSError, subprocess.SubprocessError) as error:
        raise RuntimeError("cannot establish macOS VM identity") from error
    if present.strip() != b"1":
        raise RuntimeError("physical host refused; execute inside ci-macOS")


@contextmanager
def case_lock():
    # Never unlink this inode: a new inode would permit two concurrent owners.
    fd = os.open(LOCK_PATH, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "r+", encoding="utf-8") as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError("another native case still owns the guest") from error
        saved = stream.read()
        if saved:
            try:
                previous = json.loads(saved)
            except ValueError as error:
                raise RuntimeError("native case lock receipt is unreadable; inspect before reuse") from error
            if not isinstance(previous, dict) or previous.get("cleanup_complete") is not True:
                raise RuntimeError("previous native case is not cleaned; inspect lock receipt before reuse")
        yield stream


def save_json(path, row):
    path.write_text(json.dumps(row, indent=2) + "\n", encoding="utf-8")


def save_lock(stream, row):
    stream.seek(0)
    stream.truncate()
    json.dump(row, stream)
    stream.flush()
    os.fsync(stream.fileno())


def process_identity(pid):
    """Read exact argv and only our case marker; discard all other environment."""
    if platform.system() == "Linux":
        # Headless process fixtures can run on Linux; require_guest still denies CLI.
        try:
            argv = [os.fsdecode(arg) for arg in Path(f"/proc/{pid}/cmdline").read_bytes().split(b"\0") if arg]
            environment = Path(f"/proc/{pid}/environ").read_bytes().split(b"\0")
            return argv, case_marker(environment)
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            return None, None
    libc = ctypes.CDLL(None, use_errno=True)
    mib = (ctypes.c_int * 3)(1, 49, pid)  # CTL_KERN, KERN_PROCARGS2.
    for attempt in range(3):
        size = ctypes.c_size_t()
        if libc.sysctl(mib, 3, None, ctypes.byref(size), None, 0):
            error = ctypes.get_errno()
        else:
            buffer = ctypes.create_string_buffer(size.value)
            if not libc.sysctl(mib, 3, buffer, ctypes.byref(size), None, 0):
                break
            error = ctypes.get_errno()
        if error in (errno.ESRCH, errno.EPERM, errno.EINVAL):
            return None, None
        # An exiting/execing process can invalidate the stack while sysctl
        # copies it. Re-read the size too; a live unreadable PID still fails.
        if error not in (errno.EIO, errno.ENOMEM) or attempt == 2:
            raise OSError(error, f"cannot inspect process arguments for pid {pid}")
        time.sleep(.01)
    raw = buffer.raw[:size.value]
    if len(raw) < ctypes.sizeof(ctypes.c_int):
        return None, None
    argc = ctypes.c_int.from_buffer_copy(raw[:4]).value
    # The executable path precedes argv, with an arbitrary amount of null padding.
    _, _, argv = raw[4:].partition(b"\0")
    entries = argv.lstrip(b"\0").split(b"\0")
    return [os.fsdecode(arg) for arg in entries[:argc]], case_marker(entries[argc:])


def case_marker(environment):
    prefix = b"ZWAI_NATIVE_CASE_ID="
    return next((os.fsdecode(entry[len(prefix):]) for entry in environment if entry.startswith(prefix)), None)


def process_argv(pid):
    return process_identity(pid)[0]


def owns_argv(argv, bundle, data):
    if not argv:
        return False
    bundle_prefix = str(bundle) + os.sep
    for index, arg in enumerate(argv):
        if arg.startswith(bundle_prefix):
            return True
        if arg == "--data-dir=" + str(data):
            return True
        if arg == "--data-dir" and index + 1 < len(argv) and argv[index + 1] == str(data):
            return True
    return False


class ProcessScope:
    """Attribute inherited case markers, private sessions and observed descendants."""
    def __init__(self, bundle, data, processes, case_id=None, inventory=None):
        self.bundle, self.data, self.processes = bundle, data, processes
        self.case_id = case_id
        self.inventory = inventory
        self.groups = {process.pid for process in processes}
        self.identities = {}

    def sample(self):
        listing = (self.inventory or subprocess.check_output)(
            ["/bin/ps", "-axo", "pid=,ppid=,pgid=,uid=,stat=,lstart="], timeout=5, text=True)
        rows = {}
        for line in listing.splitlines():
            parts = line.split(maxsplit=5)
            pid, parent, group, uid = map(int, parts[:4])
            if uid == os.getuid() and pid != os.getpid() and not parts[4].startswith("Z"):
                rows[pid] = (parent, group, parts[5])
        self.groups.update(process.pid for process in self.processes if process.poll() is None)
        owned = set()
        for pid, (_, group, born) in rows.items():
            argv, marker = process_identity(pid)
            if (group in self.groups or self.identities.get(pid) == born
                    or owns_argv(argv, self.bundle, self.data)
                    or (self.case_id is not None and marker == self.case_id)):
                owned.add(pid)
        while True:
            children = {pid for pid, (parent, _, _) in rows.items() if parent in owned}
            if children <= owned:
                break
            owned.update(children)
        self.identities = {pid: rows[pid][2] for pid in owned}
        self.groups = {rows[pid][1] for pid in owned
                       if rows[pid][1] in self.groups or rows[pid][1] == pid}
        return sorted(owned)


def send_signal(pids, sig):
    for pid in pids:
        try:
            os.kill(pid, sig)
        except ProcessLookupError:
            pass


def stop_owned(bundle, data, processes, grace=2, scope=None):
    """Rescan after TERM/KILL, retaining ownership after a parent exits."""
    scope = scope or ProcessScope(bundle, data, processes)
    for sig, duration in ((signal.SIGTERM, grace), (signal.SIGKILL, grace)):
        deadline = time.monotonic() + duration
        quiet = 0
        while True:
            remaining = scope.sample()
            if not remaining:
                quiet += 1
                if quiet == 2:
                    return []
            else:
                quiet = 0
                send_signal(remaining, sig)
            if time.monotonic() >= deadline:
                break
            time.sleep(.05)
    return scope.sample()


def stop_known_children(processes, grace=2):
    """Cancel only held Popen children before a potentially unavailable inventory."""
    records, pending, complete = [], [], True
    for process in reversed(processes):
        row = dict(pid=process.pid, signals=[], wait_errors=[], outcome="pending", returncode=None)
        records.append(row)
        try:
            status = process.poll()
            if status is not None:
                row.update(outcome="already_exited", returncode=process.wait(timeout=0))
                continue
        except (OSError, RuntimeError, ValueError) as error:
            row["wait_errors"].append(dict(operation="initial_wait", error_type=type(error).__name__,
                                           errno=getattr(error, "errno", None)))
            complete = False
        pending.append((process, row))
    for name, method in (("TERM", "terminate"), ("KILL", "kill")):
        if not pending:
            break
        # One phase deadline is shared by all direct children, not renewed per PID.
        deadline = time.monotonic() + grace
        for process, row in pending:
            try:
                if process.poll() is None:
                    getattr(process, method)()
                    row["signals"].append(name)
            except (OSError, RuntimeError, ValueError) as error:
                row["wait_errors"].append(dict(operation=name, error_type=type(error).__name__,
                                               errno=getattr(error, "errno", None)))
                complete = False
        remaining = []
        for process, row in pending:
            try:
                status = process.wait(timeout=max(0, deadline - time.monotonic()))
                outcome = "killed" if status == -signal.SIGKILL else "terminated" if status == -signal.SIGTERM else "exited"
                row.update(outcome=outcome, returncode=status)
            except subprocess.TimeoutExpired as error:
                row["wait_errors"].append(dict(operation=name + "_wait", error_type=type(error).__name__,
                                               timeout_seconds=error.timeout))
                remaining.append((process, row))
            except (OSError, RuntimeError, ValueError) as error:
                row["wait_errors"].append(dict(operation=name + "_wait", error_type=type(error).__name__,
                                               errno=getattr(error, "errno", None)))
                complete = False
                remaining.append((process, row))
        pending = remaining
    for _, row in pending:
        row["outcome"] = "wait_failed"
    return records, complete and not pending


def wait_scenario(process, timeout):
    deadline = time.monotonic() + timeout
    while True:
        process.native_scope.sample()
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ScenarioDeadlineExpired(process.args, timeout)
        try:
            return process.wait(timeout=min(.05, remaining))
        except subprocess.TimeoutExpired:
            pass


def copy_app(source, root):
    source = source.resolve(strict=True)
    if source.suffix != ".app" or not source.is_dir():
        raise ValueError("--app must identify an existing .app directory")
    with (source / "Contents/Info.plist").open("rb") as stream:
        metadata = plistlib.load(stream)
    if not isinstance(metadata, dict):
        raise ValueError("bundle Info.plist must contain a dictionary")
    name = metadata.get("CFBundleExecutable")
    if not isinstance(name, str) or not name or Path(name).name != name:
        raise ValueError("bundle executable must be one filename")
    original = source / "Contents/MacOS" / name
    if not original.resolve(strict=True).is_relative_to(source) or not os.access(original, os.X_OK):
        raise ValueError("bundle executable must be executable within the supplied app")
    target = root / source.name
    shutil.copytree(source, target, symlinks=True)
    executable = target / "Contents/MacOS" / name
    if not executable.resolve(strict=True).is_relative_to(target):
        raise ValueError("copied executable must remain inside the isolated bundle")
    return target, executable


class CaseBudgetExpired(TimeoutError):
    pass


class ScenarioDeadlineExpired(subprocess.TimeoutExpired):
    """Only the runner's own scenario deadline, never a leaf command timeout."""


def write_timeout_receipt(path, row):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(row, stream, ensure_ascii=True, allow_nan=False)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def record_timeout(evidence, receipt, phase, error):
    command = error.cmd
    if isinstance(command, (list, tuple)):
        command = [os.fsdecode(argument) for argument in command]
    elif command is not None:
        command = os.fsdecode(command)
    row = dict(case_id=receipt["case_id"], phase=phase, error_type=type(error).__name__,
               command=command, command_type=type(error.cmd).__name__,
               timeout_seconds=error.timeout, observed_at=time.time())
    for key in ("stdout", "stderr"):
        value = getattr(error, key, None)
        raw = value.encode("utf-8", "surrogatepass") if isinstance(value, str) else value
        row[key] = dict(type="none" if value is None else "text" if isinstance(value, str) else "bytes",
                        base64=base64.b64encode(raw if raw is not None else b"").decode("ascii"))
    name = "runner-timeout-" + uuid.uuid4().hex + ".json"
    retained = dict(row, receipt=name, saved=False)
    receipt.setdefault("timeouts", []).append(retained)
    try:
        write_timeout_receipt(evidence / name, row)
        retained["saved"] = True
    except (OSError, TypeError, ValueError) as write_error:
        failure = dict(phase=phase, error_type=type(write_error).__name__, errno=getattr(write_error, "errno", None))
        receipt.setdefault("timeout_evidence_errors", []).append(failure)
        try:
            print("WARN runner_timeout_evidence_failed " + json.dumps(failure), file=sys.stderr)
        except (OSError, ValueError):
            pass


@contextmanager
def measure_phase(receipt, name):
    started = time.monotonic()
    try:
        yield
    finally:
        receipt["timings_seconds"][name] += time.monotonic() - started


def work_remaining(started, receipt, phase):
    remaining = (receipt["budget_seconds"] - (time.monotonic() - started)
                 - receipt["cleanup_reserve_seconds"])
    if remaining <= 0:
        raise CaseBudgetExpired("native case work budget exhausted before " + phase)
    return remaining


def finish_receipt(evidence, receipt, started, code):
    def update():
        nonlocal code
        receipt["timings_seconds"]["total"] = time.monotonic() - started
        budget = receipt["budget_seconds"]
        if budget is not None and receipt["timings_seconds"]["total"] > budget:
            receipt["budget_exceeded"] = True
            if code == 0:
                code = 124
                receipt["failure"] = "native case budget exceeded"
        receipt["exit_code"] = code
    # Count the first evidence write, then retain that measured duration. The
    # final write cannot be measured inside its own payload; check its deadline
    # once more so a slow receipt can never turn an over-budget case green.
    update()
    save_json(evidence / "cleanup.json", receipt)
    update()
    save_json(evidence / "cleanup.json", receipt)
    budget = receipt["budget_seconds"]
    if budget is not None and time.monotonic() - started > budget and not receipt["budget_exceeded"]:
        update()
        save_json(evidence / "cleanup.json", receipt)
    return code


def remove_case_tree(root):
    # Go's module cache intentionally uses read-only directories. The whole
    # case is disposable, but links into another cache never grant ownership.
    def writable(directory):
        mode = os.lstat(directory).st_mode
        if not stat.S_ISDIR(mode):
            raise RuntimeError("private case directory identity changed")
        os.chmod(directory, stat.S_IMODE(mode) | 0o700, follow_symlinks=False)
    writable(root)
    def fail(error):
        raise error
    for directory, children, _ in os.walk(root, followlinks=False, onerror=fail):
        for child in children:
            path = Path(directory) / child
            if stat.S_ISDIR(os.lstat(path).st_mode):
                writable(path)
    shutil.rmtree(root)


def recover_renderer_faults(evidence, receipt, bundle, *, require_exit=False):
    if not any(os.path.lexists(evidence / name) for name in
               ("renderer-fault-targets.jsonl", "renderer-fault-copies.jsonl")):
        return True
    try:
        from native_macos_resilience_fault import cleanup_case
        result = cleanup_case(evidence, receipt["case_id"], bundle, require_exit=require_exit)
        if not isinstance(result, dict) or result.get("complete") is not True:
            raise RuntimeError("renderer fault cleanup was not confirmed")
        receipt.setdefault("renderer_fault_cleanup", []).append(result)
        return True
    except Exception as error:
        receipt["renderer_fault_cleanup_error"] = str(error)
        return False


def run_case(app, evidence, timeout, command, *, budget=60, prepare=None, preflight=None):
    started = time.monotonic()
    evidence = evidence.absolute()
    # Evidence is durable and must never be recursively copied with the source app.
    if evidence.resolve().is_relative_to(app.resolve()):
        raise ValueError("evidence must be outside the supplied app")
    evidence.mkdir(parents=True, exist_ok=False, mode=0o700)
    receipt = {"case_id": uuid.uuid4().hex, "virtualized": False, "launched": False,
               "cleanup_complete": True, "data_removed": False, "remaining_owned_pids": [],
               "app_source": str(app.absolute()), "scenario_exit": None,
               "budget_seconds": budget if math.isfinite(budget) and budget > 0 else None,
               "cleanup_reserve_seconds": min(5, budget / 10) if math.isfinite(budget) and budget > 0 else 0,
               "budget_exceeded": False,
               "timings_seconds": {name: 0 for name in
                                   ("guest_gate", "preflight", "prepare", "launch", "scenario", "cleanup", "total")},
               "native_behavior": "scenario assertions and window evidence required"}
    code = 2
    try:
        if receipt["budget_seconds"] is None:
            raise ValueError("provide a positive finite case budget")
        if prepare is not None and not callable(prepare):
            raise ValueError("case preparation must be callable")
        if preflight is not None and not callable(preflight):
            raise ValueError("case preflight must be callable")
        work_deadline = started + receipt["budget_seconds"] - receipt["cleanup_reserve_seconds"]
        with measure_phase(receipt, "guest_gate"):
            require_guest()
        receipt["virtualized"] = True
        if not command or not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("provide a scenario command and a positive finite timeout")
        work_remaining(started, receipt, "preparation")
        with case_lock() as lock:
            if preflight is not None:
                # No private process scope exists yet. This read-only hook must
                # reap its synchronous probes and leave the previous latch intact.
                with measure_phase(receipt, "preflight"):
                    preflight(lock, receipt, evidence, work_deadline)
                work_remaining(started, receipt, "preparation")
            root = Path(tempfile.mkdtemp(prefix="zwai-native-" + receipt["case_id"] + "-")).resolve()
            bundle, data = root / "app-not-copied", root / "data"
            processes, scenario, scope, preparation = [], None, None, None
            receipt.update(case_root=str(root), data_dir=str(data), cleanup_complete=False)
            phase = "prepare"
            try:
                with measure_phase(receipt, "prepare"):
                    save_lock(lock, receipt)
                    bundle, executable = copy_app(app, root)
                    workspace = root / "workspace"
                    case_home = root / "home"
                    data.mkdir()
                    workspace.mkdir()
                    case_home.mkdir(mode=0o700)
                    receipt.update(app_copy=str(bundle), workspace=str(workspace), home_dir=str(case_home))
                    environment = {key: value for key, value in os.environ.items() if not key.startswith("OPENAI_")}
                    environment.update(HOME=str(case_home), ZWAI_HOME=str(data), ZWAI_NATIVE_APP=str(bundle),
                                       ZWAI_NATIVE_DATA=str(data), ZWAI_NATIVE_WORKSPACE=str(workspace),
                                       ZWAI_NATIVE_EVIDENCE=str(evidence), ZWAI_NATIVE_CASE_ID=receipt["case_id"])
                    scope = ProcessScope(bundle, data, processes, receipt["case_id"])
                    if prepare is not None:
                        preparation = prepare(lock, receipt, environment, processes, scope, work_deadline)
                        if preparation is not None and (not isinstance(preparation, CasePreparation)
                                or not callable(preparation.before_launch) or not callable(preparation.handoff)):
                            raise ValueError("case preparation must return CasePreparation or None")
                work_remaining(started, receipt, "app launch")
                with (evidence / "app.stdout.log").open("wb") as out, \
                        (evidence / "app.stderr.log").open("wb") as err, \
                        (evidence / "scenario.stdout.log").open("wb") as scenario_out, \
                        (evidence / "scenario.stderr.log").open("wb") as scenario_err:
                    phase = "launch"
                    with measure_phase(receipt, "launch"):
                        if preparation is not None:
                            preparation.before_launch()
                            work_remaining(started, receipt, "app launch")
                        desktop = subprocess.Popen([str(executable), "desktop", "--mock", "--data-dir", str(data)],
                                                   cwd=workspace, env=environment, stdout=out, stderr=err,
                                                   start_new_session=True)
                        processes.append(desktop)
                        receipt["launched"] = True
                        receipt["app_pid"] = desktop.pid
                        scope.sample()
                    scenario_limit = min(timeout, work_remaining(started, receipt, "scenario launch"))
                    receipt["scenario_limit_seconds"] = scenario_limit
                    environment.update(ZWAI_NATIVE_APP_PID=str(desktop.pid),
                                       ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS=str(scenario_limit))
                    phase = "scenario"
                    with measure_phase(receipt, "scenario"):
                        scenario_deadline = time.monotonic() + scenario_limit
                        scenario = subprocess.Popen(command, cwd=workspace, env=environment,
                                                    stdout=scenario_out, stderr=scenario_err, start_new_session=True)
                        processes.append(scenario)
                        scope.groups.add(scenario.pid)
                        scenario.native_scope = scope
                        if preparation is not None:
                            preparation.handoff(scenario, min(scenario_deadline, work_deadline))
                        wait_limit = min(scenario_deadline - time.monotonic(),
                                         work_remaining(started, receipt, "scenario wait"))
                        if wait_limit <= 0:
                            raise ScenarioDeadlineExpired(command, scenario_limit)
                        code = wait_scenario(scenario, wait_limit)
                        receipt["scenario_exit"] = code
            except CaseBudgetExpired as error:
                code = 124
                receipt["failure"] = str(error)
            except ScenarioDeadlineExpired as error:
                code = 124
                receipt["failure"] = "scenario timeout"
                receipt["failure_kind"] = "scenario_deadline"
                record_timeout(evidence, receipt, phase, error)
            except subprocess.TimeoutExpired as error:
                code = 2
                receipt["failure"] = "subprocess timeout during " + phase
                receipt["failure_kind"] = "subprocess_timeout"
                record_timeout(evidence, receipt, phase, error)
            except KeyboardInterrupt:
                code = 130
                receipt["failure"] = "interrupted"
            except (OSError, ValueError, RuntimeError) as error:
                code = 2
                receipt["failure"] = str(error)
            finally:
                with measure_phase(receipt, "cleanup"):
                    handlers = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGINT, signal.SIGTERM)}
                    try:
                        try:
                            before_faults = recover_renderer_faults(evidence, receipt, bundle)
                            try:
                                direct, direct_complete = stop_known_children(processes)
                                receipt["direct_child_cleanup"] = direct
                                remaining = stop_owned(bundle, data, processes, scope=scope)
                            finally:
                                after_faults = recover_renderer_faults(evidence, receipt, bundle, require_exit=True)
                            receipt["remaining_owned_pids"] = remaining
                            if not remaining and before_faults and after_faults and direct_complete:
                                engine_log = data / "engine.log"
                                if engine_log.is_file() and not engine_log.is_symlink():
                                    shutil.copyfile(engine_log, evidence / "engine.log")
                                remove_case_tree(root)
                                receipt["data_removed"] = True
                                receipt["cleanup_complete"] = True
                            else:
                                code = 3
                                if not direct_complete:
                                    receipt["cleanup_error"] = "direct child cleanup had an unconfirmed wait"
                        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                            if isinstance(error, subprocess.TimeoutExpired):
                                record_timeout(evidence, receipt, "cleanup", error)
                            receipt["cleanup_error"] = str(error)
                            code = 3
                        # A failed cleanup keeps the original fsynced refusal latch.
                        # Truncating that latch could otherwise allow the next case.
                        if receipt["cleanup_complete"]:
                            save_lock(lock, receipt)
                    finally:
                        for sig, handler in handlers.items():
                            signal.signal(sig, handler)
    except CaseBudgetExpired as error:
        code = 124
        receipt["failure"] = str(error)
    except KeyboardInterrupt:
        code = 130
        receipt["failure"] = "interrupted"
    except subprocess.TimeoutExpired as error:
        code = 2
        receipt["failure"] = "subprocess timeout during preflight"
        receipt.setdefault("failure_kind", "subprocess_timeout")
        record_timeout(evidence, receipt, "preflight", error)
    except (OSError, ValueError, RuntimeError) as error:
        code = 2
        receipt["failure"] = str(error)
    finally:
        code = finish_receipt(evidence, receipt, started, code)
    return code


def main(argv=None, *, prepare=None, preflight=None):
    parser = argparse.ArgumentParser(description=__doc__, epilog=(
        "Scenario environment: HOME (isolated home/cache, parent unchanged), "
        "ZWAI_HOME / ZWAI_NATIVE_DATA (isolated data), "
        "ZWAI_NATIVE_APP (copied .app), ZWAI_NATIVE_WORKSPACE (cwd), "
        "ZWAI_NATIVE_APP_PID (launched desktop PID), "
        "ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS (remaining limit at scenario launch), "
        "ZWAI_NATIVE_EVIDENCE (persistent logs and cleanup.json). "
        "Preserve ZWAI_NATIVE_CASE_ID in descendants so detached helpers remain attributable. "
        "Perform multiple restart steps within one scenario; independent cases never share data."))
    parser.add_argument("--app", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path, help="new persistent directory outside the app")
    parser.add_argument("--timeout", required=True, type=float, help="scenario deadline in seconds")
    parser.add_argument("--budget", type=float, default=60,
                        help="whole-case budget in seconds (default: 60); cleanup always runs and may exceed it")
    parser.add_argument("command", nargs=argparse.REMAINDER, help="-- followed by a scenario command")
    args = parser.parse_args(argv)
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    def interrupt(signum, frame):
        # Once cancellation begins, a second signal must not bypass final cleanup.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise KeyboardInterrupt
    handlers = {sig: signal.signal(sig, interrupt) for sig in (signal.SIGTERM, signal.SIGINT)}
    try:
        return run_case(args.app, args.evidence, args.timeout, command, budget=args.budget,
                        prepare=prepare, preflight=preflight)
    except (OSError, ValueError) as error:
        parser.exit(2, str(error) + "\n")
    finally:
        for sig, handler in handlers.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    raise SystemExit(main())
