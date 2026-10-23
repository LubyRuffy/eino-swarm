"""Detached guest campaign ownership, cancellation and cleanup receipts.

The launcher returns before the queue's request deadline. A campaign owns its
own session and forwards cancellation to the native case before recording exit.
"""
import hashlib
import fcntl
import json
import math
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import uuid

PHASES = ("baseline", "renderer", "gpu", "exhaustion", "sleepwake", "soak")
DEPENDENCIES = ("native_macos_resilience_background.py", "native_macos_resilience_worker.py",
                "native_macos_resilience_fault.py", "native_macos_resilience_phases.py", "native_macos_resilience_soak.py")


def validate_budget(cleanup, budget):
    try:
        timings = cleanup["timings_seconds"]
        durations = [timings[key] for key in ("guest_gate", "prepare", "launch", "scenario", "cleanup", "total")]
        if (cleanup.get("exit_code") != 0 or cleanup.get("budget_exceeded") is not False
                or cleanup["budget_seconds"] != budget["whole_case_seconds"]
                or not 0 < cleanup["scenario_limit_seconds"] <= budget["scenario_seconds"]
                or not all(isinstance(value, (float, int)) and math.isfinite(value) and value >= 0 for value in durations)
                or timings["total"] > budget["whole_case_seconds"]
                or timings["scenario"] > cleanup["scenario_limit_seconds"]):
            raise RuntimeError("native runner exit or whole-case budget receipt failed")
    except (KeyError, TypeError) as error:
        raise RuntimeError("native runner budget receipt is missing or malformed") from error


def input_fingerprint(manifest, here, digest):
    try:
        fields = {"native_macos_resilience.py": "harness_sha256", "native_macos_resilience_ax.m": "probe_source_sha256",
                  "native_macos_case.py": "runner_sha256", "native_macos_resilience_control.py": "control_sha256"}
        frozen = {name: manifest[field] for name, field in fields.items()}
        dependencies = manifest["dependencies"]
        if set(dependencies) != set(DEPENDENCIES):
            raise RuntimeError("campaign inputs omit a scenario module")
        frozen.update(dependencies)
        paths = {name: here / name for name in frozen}
        for name, path, expected in [("helper", manifest["helper"], manifest["helper_sha256"]),
                ("package", manifest["package"]["zip"], manifest["package"]["zip_sha256"]),
                ("executable", manifest["package"]["executable"], manifest["package"]["executable_sha256"])]:
            frozen[name], paths[name] = expected, Path(path)
        if manifest.get("probe") is not None:
            raise RuntimeError("campaign inputs request an unavailable fault probe")
        if any(digest(paths[name]) != expected for name, expected in frozen.items()):
            raise RuntimeError("campaign inputs changed during the run")
        payload = dict(files=frozen, source_sha=manifest["package"]["source_sha"], version=manifest["package"]["version"],
                       phases=manifest["phases"], selection=manifest["selection"], budgets=manifest["budgets"])
        return hashlib.sha256(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    except (KeyError, OSError, TypeError) as error:
        raise RuntimeError("campaign inputs are unavailable or were not frozen") from error


def write_json(path, value):
    path = Path(path)
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    with temporary.open("x") as stream:
        json.dump(value, stream, indent=2, allow_nan=False)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


def identity(pid):
    """Birth time plus a unique launch token in argv prevents PID reuse signals."""
    try:
        born = subprocess.check_output(["/bin/ps", "-p", str(pid), "-o", "lstart="], text=True, timeout=5).strip()
        command = subprocess.check_output(["/bin/ps", "-ww", "-p", str(pid), "-o", "command="], text=True, timeout=5).strip()
    except subprocess.CalledProcessError:
        return None
    return dict(pid=pid, birth_time=born, command_sha256=hashlib.sha256(command.encode()).hexdigest()) if born and command else None


def interrupted(root, launch_id):
    path = Path(root) / "cancel.request.json"
    if path.exists() and json.loads(path.read_text()).get("launch_id") == launch_id:
        raise KeyboardInterrupt


def wait_process(process, timeout, root, launch_id):
    deadline = time.monotonic() + timeout
    while True:
        interrupted(root, launch_id)
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise subprocess.TimeoutExpired("owned native case", timeout)
        try:
            return process.wait(timeout=min(1, remaining))
        except subprocess.TimeoutExpired:
            continue


def fault_cleanup_confirmed(case, receipt):
    if any(os.path.lexists(case / name) for name in ("fault-target.json", "fault-target-outer-cleanup.json")):
        return False
    if not any(os.path.lexists(case / name) for name in
               ("renderer-fault-targets.jsonl", "renderer-fault-copies.jsonl")):
        return True
    checks = receipt.get("renderer_fault_cleanup")
    if (not isinstance(checks, list) or len(checks) != 2 or not receipt.get("case_id")
            or not receipt.get("app_copy")):
        return False
    return all(isinstance(row, dict) and row.get("complete") is True
               and row.get("case_id") == receipt["case_id"] and row.get("private_app") == receipt["app_copy"]
               and row.get("require_exit") is flag for row, flag in zip(checks, (False, True)))


def cleanup_complete(root):
    """An unknown case outcome cannot be presented as cleaned up."""
    for claim in Path(root).glob("*-claim/claim.json"):
        name = claim.parent.name.removesuffix("-claim")
        if name not in PHASES:
            return False
        path = Path(root) / name / "cleanup.json"
        if not path.exists():
            return False
        row = json.loads(path.read_text())
        if row.get("cleanup_complete") is not True or row.get("data_removed") is not True or row.get("remaining_owned_pids") != []:
            return False
        if not fault_cleanup_confirmed(path.parent, row):
            return False
    return True


def execute(args, action):
    root = args.evidence.resolve()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock = (root / "driver.lock").open("a+")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError as error:
        lock.close()
        raise RuntimeError("a campaign driver already owns this evidence directory") from error
    write_json(root / "owner.json", dict(launch_id=args.launch_id, process=identity(os.getpid()), started_at=time.time()))
    code = 2
    try:
        interrupted(root, args.launch_id)
        code = action(args)
        return code
    except KeyboardInterrupt:
        code = 130
        raise
    finally:
        try:
            write_json(root / "driver-exit.json", dict(launch_id=args.launch_id, exit_code=code,
                       cleanup_complete=cleanup_complete(root), finished_at=time.time()))
        finally:
            lock.close()


def launch(args, script):
    root = args.evidence.resolve()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    launch_id = uuid.uuid4().hex
    # Never overwrite an uncertain launch with another eighteen-hour campaign.
    with (root / "launch-claim.json").open("x") as stream:
        json.dump(dict(launch_id=launch_id, created_at=time.time()), stream)
    argv = [sys.executable, str(script), "run", "--zip", str(args.zip.resolve()), "--source", args.source,
            "--evidence", str(root), "--soak-seconds", str(args.soak_seconds), "--sleep-cycles", str(args.sleep_cycles),
            "--interval", str(args.interval), "--stage-timeout", str(args.stage_timeout),
            "--phases", *getattr(args, "phases", PHASES), "--launch-id", launch_id]
    with (root / "driver.stdout.log").open("xb") as out, (root / "driver.stderr.log").open("xb") as err:
        process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=out, stderr=err, start_new_session=True)
    row = dict(launch_id=launch_id, process=identity(process.pid), detached=True, launched_at=time.time())
    write_json(root / "launch.json", row)
    if row["process"] is None:
        raise RuntimeError("campaign exited during launch; inspect its retained logs and exit receipt")
    return row


def cancel(root, timeout=90):
    root = Path(root).resolve()
    row = json.loads((root / "owner.json" if (root / "owner.json").exists() else root / "launch.json").read_text())
    write_json(root / "cancel.request.json", dict(launch_id=row["launch_id"], requested_at=time.time()))
    expected = row["process"]
    if expected is None:
        raise RuntimeError("campaign launch identity is unknown; no signal sent")
    current = identity(expected["pid"])
    if current == expected:
        os.kill(expected["pid"], signal.SIGTERM)
    elif current is not None:
        raise RuntimeError("campaign PID identity changed; signal refused")
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        receipt = root / "driver-exit.json"
        if receipt.exists():
            done = json.loads(receipt.read_text())
            if done.get("launch_id") == row["launch_id"]:
                if done.get("cleanup_complete") is not True:
                    raise RuntimeError("campaign exited but native cleanup is pending; next case refused")
                return done
        time.sleep(.25)
    raise RuntimeError("cancellation requested; native cleanup receipt is still pending")


def status(root):
    path = Path(root) / "owner.json"
    if not path.exists():
        path = Path(root) / "launch.json"
    if not path.exists():
        return dict(state="not_launched")
    row = json.loads(path.read_text())
    process = row.get("process")
    row["state"] = "running" if process and identity(process["pid"]) == process else "exited_or_reused"
    receipt = Path(root) / "driver-exit.json"
    if receipt.exists():
        row["exit"] = json.loads(receipt.read_text())
    return row
