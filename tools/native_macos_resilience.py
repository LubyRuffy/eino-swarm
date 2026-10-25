#!/usr/bin/env python3
"""Serial, resumable CI-only acceptance of a byte-identified macOS ZIP.

Each stage runs through native_macos_case.py. Claims never imply success and an
interrupted/unknown attempt is never replayed automatically. Normal stages use
public native UI and operating-system diagnostics. Each fault requires fresh
private-app ownership proof and a verified bounded cleanup receipt.
"""
import argparse
import base64
from datetime import datetime, timedelta
from contextlib import contextmanager
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path, PurePosixPath
import plistlib
import re
import signal
import stat
import struct
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid
import zipfile
import zlib

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("native_case", HERE / "native_macos_case.py")
runner = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(runner)
_control_spec = importlib.util.spec_from_file_location("resilience_control", HERE / "native_macos_resilience_control.py")
control = importlib.util.module_from_spec(_control_spec)
_control_spec.loader.exec_module(control)
_background_spec = importlib.util.spec_from_file_location("resilience_background", HERE / "native_macos_resilience_background.py")
background = importlib.util.module_from_spec(_background_spec)
_background_spec.loader.exec_module(background)

_phases_spec = importlib.util.spec_from_file_location("resilience_phases", HERE / "native_macos_resilience_phases.py")
fault_phases = importlib.util.module_from_spec(_phases_spec)
_phases_spec.loader.exec_module(fault_phases)
_soak_spec = importlib.util.spec_from_file_location("resilience_soak", HERE / "native_macos_resilience_soak.py")
soak = importlib.util.module_from_spec(_soak_spec)
_soak_spec.loader.exec_module(soak)
_input_spec = importlib.util.spec_from_file_location("resilience_input_evidence", HERE / "native_macos_input_evidence.py")
input_evidence = importlib.util.module_from_spec(_input_spec)
_input_spec.loader.exec_module(input_evidence)


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def dependency_hashes():
    return {name: sha256(HERE / name) for name in control.DEPENDENCIES}


def validate_dependencies(manifest):
    try:
        if manifest.get("dependencies") == dependency_hashes():
            return
    except OSError as error:
        raise RuntimeError("campaign harness dependencies are unavailable") from error
    raise RuntimeError("campaign harness dependencies changed or were not frozen")


def verify_inputs(manifest):
    fingerprint = control.input_fingerprint(manifest, HERE, sha256)
    if manifest.get("input_fingerprint") != fingerprint:
        raise RuntimeError("campaign input fingerprint differs from its frozen manifest")
    return fingerprint


def atomic_json(path, value):
    path = Path(path)
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    with temporary.open("x", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2, ensure_ascii=False, allow_nan=False)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


def read_json(path):
    value = json.loads(Path(path).read_text())
    if not isinstance(value, dict):
        raise ValueError("expected an object: " + str(path))
    return value


def timestamp(value):
    # The guest's Python 3.9 accepts only three or six fraction digits; Go's
    # RFC3339Nano omits trailing zeroes and can produce any length from 1 to 9.
    normalized = re.sub(r"\.\d+", lambda match: (match[0] + "000000")[:7], value.replace("Z", "+00:00"))
    parsed = datetime.fromisoformat(normalized)
    if parsed.tzinfo is None:
        raise ValueError("an explicit timestamp timezone is required")
    return parsed.timestamp()


def require_guest():
    runner.require_guest()
    name = subprocess.check_output(["/bin/hostname", "-s"], text=True, timeout=5).strip().lower()
    if name != "ci-macos":
        raise RuntimeError("resilience acceptance requires ci-macOS")


def phase_plan(soak_seconds, cycles, interval):
    if (not all(math.isfinite(value) and value > 0 for value in (soak_seconds, interval))
            or type(cycles) is not int or cycles < 1):
        raise ValueError("positive durations and at least one sleep cycle are required")
    return [dict(name="baseline", duration_seconds=0), dict(name="renderer", duration_seconds=0),
            dict(name="gpu", duration_seconds=0), dict(name="exhaustion", duration_seconds=0),
            dict(name="sleepwake", duration_seconds=0, cycles=cycles),
            dict(name="soak", duration_seconds=soak_seconds, interval_seconds=interval)]


def normalize_selection(phases):
    if not phases or len(phases) != len(set(phases)) or any(name not in control.PHASES for name in phases):
        raise ValueError("select unique stages from the complete six-stage plan")
    return [name for name in control.PHASES if name in phases]


def phase_budgets(phases, action_timeout):
    if not math.isfinite(action_timeout) or action_timeout <= 0:
        raise ValueError("the stage action timeout must be finite and positive")
    budgets = {}
    for phase in phases:
        seconds = max(action_timeout, 360 if phase["name"] == "exhaustion" else 0)
        seconds += phase["duration_seconds"] + phase.get("cycles", 0) * (30 + action_timeout)
        budgets[phase["name"]] = dict(scenario_seconds=seconds, cleanup_seconds=60, whole_case_seconds=seconds + 60)
    return budgets


def native_command(manifest, phase, args, directory):
    budget = manifest["budgets"][phase["name"]]
    command = [sys.executable, str(HERE / "native_macos_case.py"), "--app", manifest["package"]["app"],
               "--evidence", str(directory), "--timeout", str(budget["scenario_seconds"]),
               "--budget", str(budget["whole_case_seconds"]), "--", sys.executable,
               str(Path(__file__).resolve()), "scenario", "--campaign", str(args.campaign),
               "--phase", phase["name"], "--timeout", str(args.stage_timeout)]
    return command, budget["whole_case_seconds"]


def build_identity(executable, source):
    output = subprocess.check_output(["go", "version", "-m", str(executable)], text=True, timeout=15)
    settings = dict(re.findall(r"^\s*build\s+([^=\s]+)=(.*)$", output, re.MULTILINE))
    if settings.get("vcs.revision") != source or settings.get("vcs.modified") != "false":
        raise RuntimeError("package provenance pending: Go build must embed the exact clean source revision")
    return {key: settings[key] for key in ("vcs.revision", "vcs.modified")}


def prepare_package(package, source, destination):
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("source must identify a full Git commit")
    package, destination = Path(package).resolve(strict=True), Path(destination)
    with zipfile.ZipFile(package) as archive:
        entries = archive.infolist()
        seen = set()
        for entry in entries:
            path = PurePosixPath(entry.filename)
            normalized = path.as_posix().casefold()
            if (path.is_absolute() or ".." in path.parts or not path.parts or path.parts[0] != "zwai.app"
                    or stat.S_ISLNK(entry.external_attr >> 16) or normalized in seen):
                raise ValueError("unsafe or duplicate package entry")
            seen.add(normalized)
        info = plistlib.loads(archive.read("zwai.app/Contents/Info.plist"))
        name, version = info.get("CFBundleExecutable"), info.get("CFBundleShortVersionString")
        if not isinstance(name, str) or Path(name).name != name or not re.fullmatch(r"\d+\.\d+\.\d+", version or ""):
            raise ValueError("a standard versioned release package is required")
        executable_name = "zwai.app/Contents/MacOS/" + name
        binary_hash = hashlib.sha256(archive.read(executable_name)).hexdigest()
        destination.mkdir(parents=True, exist_ok=False)
        for entry in entries:
            target = destination / entry.filename
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.read(entry))
                target.chmod((entry.external_attr >> 16) & 0o777 or 0o644)
    build = build_identity(destination / executable_name, source)
    filename = "zwai-" + version + "-darwin-"
    if not package.name.startswith(filename) or not re.fullmatch(re.escape(filename) + r"(?:arm64|amd64)\.zip", package.name):
        raise RuntimeError("release ZIP filename and bundle version differ")
    return dict(source_sha=source, embedded_vcs=build, zip=str(package), zip_sha256=sha256(package), version=version,
                build=info.get("CFBundleVersion"), app=str(destination / "zwai.app"),
                executable=str(destination / executable_name), executable_sha256=binary_hash)


def claim_stage(directory, row):
    directory.mkdir(parents=True, exist_ok=True)
    try:
        with (directory / "claim.json").open("x") as stream:
            json.dump(row, stream)
            stream.flush()
            os.fsync(stream.fileno())
    except FileExistsError as error:
        raise RuntimeError("stage already claimed; inspect its result and cleanup before any new attempt") from error


def accepted_stage(directory, name, manifest=None):
    if name not in control.PHASES:
        raise RuntimeError("unknown native acceptance stage: " + name)
    cleanup, result = read_json(directory / "cleanup.json"), read_json(directory / "resilience.json")
    if (cleanup.get("cleanup_complete") is not True or cleanup.get("data_removed") is not True
            or cleanup.get("remaining_owned_pids") != [] or cleanup.get("scenario_exit") != 0
            or result.get("passed") is not True or result.get("phase") != name
            or result.get("case_id") != cleanup.get("case_id")
            or result.get("engine_pid_before") != result.get("engine_pid_after")
            or type(result.get("engine_pid_before")) is not int or result["engine_pid_before"] <= 0
            or result.get("draft_verified") is not True or len(set(result.get("turn_ids", []))) < 2
            or len(result.get("snapshots", [])) < 2):
        raise RuntimeError("stage lacks its complete behavior and cleanup evidence")
    if name == "soak" and result.get("soak_elapsed_seconds", 0) < 64800:
        raise RuntimeError("long-running acceptance did not observe eighteen hours")
    if name == "sleepwake" and result.get("sleep_cycles", 0) < 3:
        raise RuntimeError("three real sleep/wake cycles are required")
    for filename in result["snapshots"]:
        path = Path(filename)
        if path.is_absolute() or ".." in path.parts or not (directory / path).is_file():
            raise RuntimeError("stage snapshot evidence is missing or escapes its directory")
    if not manifest or name not in manifest.get("budgets", {}):
        raise RuntimeError("native stage has no frozen whole-case budget")
    control.validate_budget(cleanup, manifest["budgets"][name])
    if not result.get("input_fingerprint") or result["input_fingerprint"] != manifest.get("input_fingerprint"):
        raise RuntimeError("native stage input fingerprint differs from its campaign")
    background.accept_evidence(directory, result)
    input_evidence.accept_stage(directory, result, fixture_png())
    if name in ("renderer", "gpu", "exhaustion"):
        fault_phases.accept_evidence(directory, result, cleanup, name, sys.modules[__name__])
    elif name == "soak":
        phase = next(row for row in manifest["phases"] if row["name"] == name)
        soak.accept_evidence(directory, result, phase, manifest["budgets"][name])
    return True


def campaign_status(root):
    if not (root / "campaign.json").exists():
        return dict(passed=False, selection_complete=False, phases=[], driver=control.status(root))
    manifest = read_json(root / "campaign.json")
    selection = normalize_selection(manifest.get("selection"))
    if [row.get("name") for row in manifest.get("phases", [])] != [row["name"] for row in phase_plan(64800, 3, 60)]:
        raise RuntimeError("campaign must retain its complete ordered six-stage plan")
    phases = []
    for phase in manifest["phases"]:
        name = phase["name"]
        row = dict(name=name, status="pending")
        directory, claim = root / name, root / (name + "-claim")
        if (directory / "cleanup.json").exists():
            try:
                accepted_stage(directory, name, manifest)
                row["status"] = "passed"
            except (OSError, ValueError, RuntimeError) as error:
                row.update(status="failed", reason=str(error))
        elif (claim / "claim.json").exists():
            row["status"] = "claimed_outcome_unknown"
        if (directory / "progress.json").exists():
            row["progress"] = read_json(directory / "progress.json")
        phases.append(row)
    selection_complete = all(row["status"] == "passed" for row in phases if row["name"] in selection)
    return dict(package=manifest["package"], passed=all(row["status"] == "passed" for row in phases), phases=phases,
                selection_complete=selection_complete,
                driver=control.status(root))


def process_identity(helper, pid):
    result = subprocess.run([str(helper), "identity", str(pid)], check=True, capture_output=True, text=True, timeout=5)
    return json.loads(result.stdout)


def signal_identified(helper, expected, sig):
    # Setup discovers its private shell/engine by case marker. Held display
    # children and WebKit faults have separate ownership and cleanup paths.
    case, app = os.environ.get("ZWAI_NATIVE_CASE_ID", ""), Path(os.environ.get("ZWAI_NATIVE_APP", "."))
    argv, marker = runner.process_identity(expected["pid"])
    allowed = str(app / "Contents/MacOS/zwai")
    if (sig != signal.SIGTERM or not re.fullmatch(r"[0-9a-f]{32}", case)
            or not app.is_absolute() or not app.parent.name.startswith("zwai-native-" + case + "-")
            or marker != case or not argv or argv[0] != expected.get("executable") or argv[0] != allowed):
        raise RuntimeError("TERM target is not an owned private case process")
    if process_identity(helper, expected["pid"]) != expected:
        raise RuntimeError("process identity changed; owned process termination refused")
    os.kill(expected["pid"], sig)


def compile_helpers(destination, include_probe=False):
    if include_probe:
        raise ValueError("the retired injection probe is unavailable; only the public native UI helper is compiled")
    source = HERE / "native_macos_resilience_ax.m"
    common = ["/usr/bin/xcrun", "clang", "-fobjc-arc", "-Wall", "-Wextra", "-Werror", "-mmacosx-version-min=13.0",
              "-framework", "AppKit",
              "-framework", "ApplicationServices", "-framework", "CoreGraphics", str(source)]
    destination.mkdir(exist_ok=True)
    helper = destination / "native-resilience-ax"
    subprocess.run([*common, "-o", str(helper)], check=True, capture_output=True, timeout=120)
    return helper, None


@contextmanager
def campaign_lock(root):
    with (root / "campaign.lock").open("a+") as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError("this campaign already has a running owner") from error
        yield


def campaign(args):
    require_guest()
    selection = normalize_selection(args.phases)
    if (not math.isfinite(args.stage_timeout) or args.stage_timeout <= 0
            or args.soak_seconds < 64800 or args.sleep_cycles < 3):
        raise ValueError("acceptance requires at least 18 hours, three sleep cycles and a finite action timeout")
    root = args.evidence.resolve()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    with campaign_lock(root):
        manifest_path = root / "campaign.json"
        if manifest_path.exists():
            manifest = read_json(manifest_path)
            if manifest.get("selection") != selection:
                raise RuntimeError("campaign phase selection changed; existing claims cannot be overwritten")
            if manifest.get("phases") != phase_plan(args.soak_seconds, args.sleep_cycles, args.interval):
                raise RuntimeError("campaign six-stage manifest plan changed; existing claims cannot be overwritten")
            if manifest.get("budgets") != phase_budgets(manifest["phases"], args.stage_timeout):
                raise RuntimeError("campaign whole-case budgets changed; existing claims cannot be overwritten")
            validate_dependencies(manifest)
            if (manifest["package"]["zip_sha256"] != sha256(args.zip)
                    or manifest["package"]["source_sha"] != args.source):
                raise RuntimeError("campaign package, source or harness changed; use a new evidence directory")
            verify_inputs(manifest)
        else:
            package = prepare_package(args.zip, args.source, root / "prepared")
            subprocess.run(["/usr/bin/codesign", "--verify", "--deep", "--strict", package["app"]], check=True, timeout=20)
            helper, probe = compile_helpers(root / "helpers", include_probe=False)
            manifest = dict(package=package, harness_sha256=sha256(Path(__file__)),
                            probe_source_sha256=sha256(HERE / "native_macos_resilience_ax.m"),
                            runner_sha256=sha256(HERE / "native_macos_case.py"), helper=str(helper), probe=None,
                            control_sha256=sha256(HERE / "native_macos_resilience_control.py"),
                            dependencies=dependency_hashes(),
                            helper_sha256=sha256(helper), probe_sha256=None, selection=selection,
                            phases=phase_plan(args.soak_seconds, args.sleep_cycles, args.interval),
                            created_at=time.time(), passed=False)
            manifest["budgets"] = phase_budgets(manifest["phases"], args.stage_timeout)
            manifest["input_fingerprint"] = control.input_fingerprint(manifest, HERE, sha256)
            atomic_json(manifest_path, manifest)
        for phase in manifest["phases"]:
            if phase["name"] not in selection:
                continue
            control.interrupted(root, args.launch_id)
            verify_inputs(manifest)
            directory = root / phase["name"]
            if (directory / "cleanup.json").exists():
                accepted_stage(directory, phase["name"], manifest)
                continue
            # Claim is outside runner evidence: the runner deliberately refuses an existing directory.
            claim = root / (phase["name"] + "-claim")
            claim_stage(claim, dict(phase=phase["name"], claimed_at=time.time(), driver_pid=os.getpid()))
            environment = dict(os.environ, ZWAI_RESILIENCE_PHASE=phase["name"])
            environment.pop("DYLD_INSERT_LIBRARIES", None)
            command, seconds = native_command(manifest, phase, argparse.Namespace(campaign=manifest_path,
                                                stage_timeout=args.stage_timeout), directory)
            with (claim / "runner.stdout.log").open("wb") as out, (claim / "runner.stderr.log").open("wb") as err:
                process = subprocess.Popen(command, env=environment, stdout=out, stderr=err, start_new_session=True)
                atomic_json(claim / "process.json", dict(pid=process.pid, started_at=time.time(), command=command))
                code = wait_native_case(process, seconds, root, args.launch_id, directory, manifest["helper"])
            atomic_json(claim / "exit.json", dict(exit_code=code, finished_at=time.time()))
            if code != 0:
                raise RuntimeError("native stage failed; evidence retained at " + str(directory))
            verify_inputs(manifest)
            accepted_stage(directory, phase["name"], manifest)
        status = campaign_status(root)
        manifest["passed"] = status["passed"]
        manifest["selection_complete"] = status["selection_complete"]
        manifest["selection_finished_at"] = time.time()
        atomic_json(manifest_path, manifest)
    return 0


def wait_native_case(process, seconds, root, launch_id, directory, helper):
    try:
        return control.wait_process(process, seconds, root, launch_id)
    except (subprocess.TimeoutExpired, KeyboardInterrupt) as cause:
        try:
            process.send_signal(signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=30)
        except subprocess.TimeoutExpired as error:
            raise RuntimeError("runner cleanup is still pending; next stage refused") from error
        if isinstance(cause, KeyboardInterrupt):
            raise KeyboardInterrupt
        raise RuntimeError("stage timed out; inspect retained behavior and cleanup receipts") from cause




class Deadline:
    def __init__(self, seconds):
        if not math.isfinite(seconds) or seconds <= 0:
            raise ValueError("action deadline must be positive and finite")
        self.ends = time.monotonic() + seconds

    def remaining(self):
        value = self.ends - time.monotonic()
        if value <= 0:
            raise TimeoutError("native phase action timed out")
        return value


def api(origin, path, method="GET", body=None):
    parsed = urllib.parse.urlsplit(origin)
    if parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or not parsed.port:
        raise RuntimeError("non-loopback case origin refused")
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(origin + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, request, response, code, message, headers, new_url):
            raise RuntimeError("case API redirect refused")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(request, timeout=3) as response:
        if response.url != origin + path:
            raise RuntimeError("case API redirect refused")
        return json.load(response)


def _run_native_helper(argv, **options):
    return subprocess.run(argv, **options)


def _native_action_receipt(context, action, argv, started_at, elapsed_seconds, result=None, error=None):
    source = result if result is not None else error
    streams = {}
    for key in ("stdout", "stderr"):
        value = getattr(source, key, None)
        raw = value.encode("utf-8", "surrogatepass") if isinstance(value, str) else value
        streams[key] = dict(type="none" if value is None else type(value).__name__,
                            base64=base64.b64encode(raw if raw is not None else b"").decode("ascii"))
    # Each invocation owns a new directory; later snapshots cannot replace it.
    directory = context["evidence"] / "native-actions"
    directory.mkdir(mode=0o700, exist_ok=True)
    invocation = directory / uuid.uuid4().hex
    invocation.mkdir(mode=0o700)
    atomic_json(invocation / "receipt.json", dict(streams, schema=1, action=action,
        case_id=context["case_id"], pid=context["app_pid"], requested_argv=argv, timeout_seconds=8,
        started_at=started_at, elapsed_seconds=elapsed_seconds,
        returncode=result.returncode if result is not None else None,
        exception_type=type(error).__name__ if error is not None else None,
        exception_timeout_seconds=getattr(error, "timeout", None),
        exception_argv=getattr(error, "cmd", None)))
    if action == "choose-file" and isinstance(error, subprocess.TimeoutExpired):
        atomic_json(context["evidence"] / "native-timeout.json", dict(
            stdout_base64=streams["stdout"]["base64"], stderr_base64=streams["stderr"]["base64"],
            type=type(error).__name__, action=action, case_id=context["case_id"], requested_argv=argv,
            exception_argv=error.cmd, timeout_seconds=error.timeout,
            stdout_is_none=error.stdout is None, stderr_is_none=error.stderr is None))
    elif action == "choose-file" and result is not None:
        text = {key: value.decode("utf-8", "replace") if isinstance(value, bytes) else value
                for key, value in (("stdout", result.stdout), ("stderr", result.stderr))}
        atomic_json(context["evidence"] / "native-result.json", dict(text, case_id=context["case_id"],
                    action=action, argv=result.args, returncode=result.returncode))


def _warn_native_evidence(action, error):
    try:
        print("WARN native_evidence_write_failed " + json.dumps(dict(action=action,
              error_type=type(error).__name__)), file=sys.stderr)
    except (OSError, ValueError):
        pass  # A closed diagnostic stream cannot replace the helper's failure.


def native(context, action="snapshot", value=""):
    argv, marker = runner.process_identity(context["app_pid"])
    if marker != context["case_id"] or not argv or not Path(argv[0]).is_relative_to(context["app"].parent):
        raise RuntimeError("native action lost its private case ownership")
    if action == "press" and context.get("native_language") == "zh":
        value = {"Send": "发送", "Attach files": "附加文件", "Conversations": "对话", "New conversation": "新对话"}.get(value, value)
    argv = [context["helper"], action, str(context["app_pid"]), value]
    started_at, started = time.time(), time.monotonic()
    try:
        result = _run_native_helper(argv, capture_output=True, timeout=8)
    except (OSError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        elapsed = time.monotonic() - started
        try:
            _native_action_receipt(context, action, argv, started_at, elapsed, error=error)
        except (OSError, TypeError, ValueError) as write_error:
            _warn_native_evidence(action, write_error)
        if isinstance(error, subprocess.TimeoutExpired):
            fault_phases.observe_restore_native(context, action, error=error)
        raise
    elapsed = time.monotonic() - started
    try:
        _native_action_receipt(context, action, argv, started_at, elapsed, result=result)
    except (OSError, TypeError, ValueError) as write_error:
        if not result.returncode:
            raise
        _warn_native_evidence(action, write_error)
    fault_phases.observe_restore_native(context, action, result=result)
    if result.returncode:
        stderr = result.stderr.decode("utf-8", "replace") if isinstance(result.stderr, bytes) else result.stderr
        raise OSError("native " + action + ": " + (stderr or "").strip()[:1000])
    row = json.loads(result.stdout)
    if "发送" in row.get("text", ""):
        context["native_language"] = "zh"
    return row


def wait_for(check, deadline):
    last = None
    while True:
        try:
            remaining = deadline.remaining()
        except TimeoutError as error:
            if last is not None:
                raise TimeoutError("native phase action timed out; last observation: " + str(last)) from last
            raise error
        try:
            value = check()
            if value:
                return value
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            last = error
        time.sleep(min(.2, remaining))


def snapshot(context, name):
    row = native(context)
    if row.get("case_id") != context["case_id"] or row.get("pid") != context["app_pid"] or not row.get("has_web_area"):
        raise RuntimeError("native snapshot does not show this case's web content")
    path = context["evidence"] / (name + ".png")
    subprocess.run(["/usr/sbin/screencapture", "-x", "-l", str(row["window_id"]), str(path)],
                   check=True, capture_output=True, timeout=8)
    data = path.read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n" or min(struct.unpack(">II", data[16:24])) < 100:
        raise RuntimeError("native snapshot is not a full window image")
    image = native(context, "image", str(path))
    if image.get("buckets", 0) < 16 or image.get("dominant_fraction", 1) > .985:
        raise RuntimeError("native screenshot is blank or almost uniformly painted")
    row["image"] = image
    atomic_json(context["evidence"] / (name + ".json"), row)
    return path.name


def context_for(manifest, timeout):
    context = {key: Path(os.environ[env]).resolve(strict=True) for key, env in [
        ("app", "ZWAI_NATIVE_APP"), ("data", "ZWAI_NATIVE_DATA"),
        ("workspace", "ZWAI_NATIVE_WORKSPACE"), ("evidence", "ZWAI_NATIVE_EVIDENCE")]}
    context.update(case_id=os.environ["ZWAI_NATIVE_CASE_ID"], helper=manifest["helper"])
    context["package_version"] = manifest["package"]["version"]
    context["expected_mock"] = manifest.get("expected_mock", True)
    deadline = Deadline(timeout)
    def ready():
        lease = read_json(context["data"] / "engine.json")
        metadata = api(lease["url"], "/api/meta")
        if metadata.get("mock") is not context["expected_mock"] or metadata.get("data_dir") != str(context["data"]):
            raise RuntimeError("case engine identity differs")
        if metadata.get("version") != manifest["package"]["version"]:
            raise RuntimeError("API version differs from the byte-identified release ZIP and Info.plist")
        desktop = [row for row in metadata.get("clients", []) if row.get("surface") == "desktop"]
        if len(desktop) != 1:
            return None
        context.update(origin=lease["url"], engine_pid=lease["pid"], app_pid=desktop[0]["pid"])
        context["readiness_metadata"] = metadata
        if context.get("activated_pid") != context["app_pid"]:
            context["readiness_activation"] = native(context, "activate")
            context["activated_pid"] = context["app_pid"]
        journals = context["data"] / "diagnostics"
        ready = [json.loads(line) for path in journals.glob("desktop-*.jsonl") for line in path.read_text().splitlines() if line.strip()]
        if not any(row.get("phase") == "ready" and row.get("shell_pid") == context["app_pid"] for row in ready):
            return None
        native(context)
        return context
    try:
        return wait_for(ready, deadline)
    except BaseException as error:
        # Initial readiness precedes the scenario's stateful lifecycle. Retain
        # that evidence here while the generic runner still owns private data.
        atomic_json(context["evidence"] / "readiness-failure.json", dict(case_id=context["case_id"],
                    error=type(error).__name__ + ": " + str(error),
                    metadata=context.get("readiness_metadata"), activation=context.get("readiness_activation")))
        background.retain(context)
        raise


def current_shell(context):
    return fault_phases.current_shell(context, sys.modules[__name__])


def turn_rows(context):
    threads = api(context["origin"], "/api/threads")["threads"]
    rows = []
    for thread in threads:
        rows.extend(api(context["origin"], "/api/threads/" + thread["id"] + "/turns")["turns"])
    return rows


def download_input(context, path, mime=None, *, capture=None):
    return input_evidence.download(context, path, mime, capture)


def validate_sent_input(context, text, draft, row, trace):
    return input_evidence.validate_sent_input(context, text, draft, row, trace, sys.modules[__name__])


def submit(context, text, timeout, result, expected_draft=None):
    native(context, "fill", text)
    wait_for(lambda: native(context).get("values") == [text], Deadline(timeout))
    prior = {row["id"] for row in turn_rows(context)}
    native(context, "press", "Send")
    def finished():
        rows = [row for row in turn_rows(context) if row["id"] not in prior]
        if len(rows) > 1:
            raise RuntimeError("one native Send created multiple turns")
        if not rows or rows[0].get("status") not in ("done", "error", "interrupted"):
            return None
        row = rows[0]
        trace = api(context["origin"], "/api/trace/" + row["id"])
        atomic_json(context["evidence"] / ("turn-" + row["id"] + ".json"), row)
        atomic_json(context["evidence"] / ("trace-" + row["id"] + ".json"), trace)
        # Explicit Send uses formatQuotedMessage's existing trim contract;
        # recovery itself must preserve the unsent snapshot byte-for-byte.
        if row.get("status") != "done" or not row.get("final") or row.get("error"):
            raise RuntimeError("offline native turn did not complete with the exact input")
        if expected_draft is not None:
            validate_sent_input(context, text, expected_draft, row, trace)
        elif row.get("user_text") != text.strip():
            raise RuntimeError("offline native turn input differs")
        allowed = context.get("offline_models", ["mock"])
        if not trace.get("llm_calls") or any(call.get("model") not in allowed for call in trace["llm_calls"]):
            raise RuntimeError("native acceptance called a non-offline model")
        fragment = re.sub(r"[*_`#]", "", row["final"].splitlines()[0]).strip()
        if not fragment or " ".join(fragment.split()) not in " ".join(native(context).get("text", "").split()):
            return None
        return row
    row = wait_for(finished, Deadline(timeout))
    context["thread_id"] = row["thread_id"]
    result["turn_ids"].append(row["id"])
    return row


def draft_snapshot(context, *, evidence_label=None):
    return input_evidence.draft_snapshot(context, sys.modules[__name__], evidence_label)


def fixture_png():
    def chunk(kind, value):
        return struct.pack(">I", len(value)) + kind + value + struct.pack(">I", zlib.crc32(kind + value))
    pixels = b"\0" + bytes([220, 40, 40, 255]) * 3 + b"\0" + bytes([40, 80, 220, 255]) * 3
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 3, 2, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b""))


def collect_draft(context, timeout):
    binary = context["workspace"] / "resilience-input.bin"
    image = context["workspace"] / "resilience-image.png"
    binary.write_bytes(bytes(range(256)) + b"\0native attachment\xff")
    image.write_bytes(fixture_png())
    text = "Unsent recovery input " + context["case_id"] + " 中文  "
    native(context, "fill", text)
    native(context, "press", "Attach files")
    def panel_ready():
        row = native(context, "dialogs")
        if row.get("case_id") != context["case_id"] or row.get("pid") != context["app_pid"]:
            raise RuntimeError("attachment panel observation belongs to another case")
        return row if any(item.get("AXRole") == "AXSheet" or item.get("AXSubrole") == "AXDialog"
                          for item in row.get("rows", [])) else None
    panel = wait_for(panel_ready, Deadline(timeout))
    atomic_json(context["evidence"] / "attachment-panel-ready.json", panel)
    if native(context, "choose-file", str(binary)).get("chosen") is not True:
        raise RuntimeError("native file selection was not confirmed")
    wait_for(lambda: any(item["sha256"] == sha256(binary) for item in draft_snapshot(context)["files"]), Deadline(timeout))
    selected = draft_snapshot(context)
    atomic_json(context["evidence"] / "draft-file-selection.json", selected)
    def text_identity(value):
        if not isinstance(value, str):
            return dict(type=type(value).__name__)
        return dict(length=len(value),
                    sha256=hashlib.sha256(value.encode("utf-8", "surrogatepass")).hexdigest())
    def api_observation(row):
        files = [dict(kind=item.get("kind"), mime=str(item.get("mime", ""))[:128], size=item.get("size"),
                      sha256=item.get("sha256")) for item in row["files"][:2]]
        return dict(text=text_identity(row["text"]), state=row["state"], file_count=len(row["files"]),
                    quote_count=len(row.get("quotes", [])), files=files)
    def native_observation(row):
        values = row.get("values", [])
        return dict(case_id=row.get("case_id"), pid=row.get("pid"), value_count=len(values),
                    values=[text_identity(value) for value in values[:2]], pasted=row.get("pasted"),
                    saved_label=any(label in row.get("text", "") for label in ("Draft saved", "草稿已保存")))
    observation = dict(case_id=context["case_id"], stage="paste", api=api_observation(selected), native=None,
                       api_refreshed=False, native_for_api=False, predicates={})
    pasted = None
    def saved():
        observation.update(stage="saved", api_refreshed=False, native_for_api=False)
        try:
            row = draft_snapshot(context)
            observation.update(api=api_observation(row), api_refreshed=True)
            checks = dict(text=row["text"] == text, state=row["state"] == "editing",
                          file_count=len(row["files"]) == 2,
                          file_hash_kinds={item["sha256"]: item["kind"] for item in row["files"]} == expected,
                          native_values=None, saved_acknowledged=None)
            observation["predicates"] = checks
            observation.pop("observation_error", None)
            if not all(checks[name] for name in ("text", "state", "file_count", "file_hash_kinds")):
                return None
            actual = native(context)
            observation.update(native=native_observation(actual), native_for_api=True)
            checks["native_values"] = actual.get("values") == [text]
            return row if checks["native_values"] else None
        except Exception as error:
            observation["observation_error"] = dict(type=type(error).__name__)
            raise
    def acknowledged():
        observation["stage"] = "acknowledgement"
        try:
            actual = native(context)
            observation.update(native=native_observation(actual), native_for_api=True)
            observation.pop("observation_error", None)
            matched = any(label in actual.get("text", "") for label in ("Draft saved", "草稿已保存"))
            observation["predicates"]["saved_acknowledged"] = matched
            return matched
        except Exception as error:
            observation["observation_error"] = dict(type=type(error).__name__)
            raise
    def flush():
        if pasted is not None:
            atomic_json(context["evidence"] / "draft-image-paste.json", pasted)
        atomic_json(context["evidence"] / "draft-collection-last.json", observation)
    try:
        pasted = native_observation(native(context, "paste-file", str(image)))
        expected = {sha256(binary): "file", sha256(image): "image"}
        observation["expected_hash_kinds"] = expected
        row = wait_for(saved, Deadline(timeout))
        # Retain existing reads in memory and flush after the check exits;
        # add no native input/reads, polling I/O or sleeps.
        wait_for(acknowledged, Deadline(timeout))
    except BaseException as error:
        observation["error"] = dict(type=type(error).__name__, stage=observation["stage"])
        try:
            flush()
        except OSError as write_error:
            try:
                print("WARN draft_collection_evidence_write_failed " + json.dumps(dict(case_id=context["case_id"],
                      stage=observation["stage"], error_type=type(write_error).__name__)), file=sys.stderr)
            except OSError:
                pass
        raise
    try:
        flush()
    except OSError as write_error:
        raise RuntimeError("native draft collection evidence could not be retained") from write_error
    atomic_json(context["evidence"] / "draft-before.json", row)
    return row


def verify_draft(context, expected, timeout):
    return fault_phases.verify_draft(context, expected, timeout, sys.modules[__name__])









def case_shells(context):
    rows = subprocess.check_output(["/bin/ps", "-axo", "pid="], text=True, timeout=5)
    owned = set()
    for value in rows.split():
        pid = int(value)
        argv, marker = runner.process_identity(pid)
        if (marker == context["case_id"] and argv and ".app/Contents/MacOS/" in argv[0]
                and Path(argv[0]).is_relative_to(context["app"].parent) and "engine" not in argv[1:]):
            owned.add(pid)
    return owned




def sleepwake(context, expected, cycles, timeout, result, *, after_wake=None):
    for number in range(cycles):
        held = "awake_process" in context
        background.stop_awake(context, sys.modules[__name__])
        owner = "zwai-case-" + context["case_id"] + "-" + str(number)
        wake = (datetime.now() + timedelta(seconds=30)).strftime("%m/%d/%y %H:%M:%S")
        before = subprocess.check_output(["/usr/bin/pmset", "-g", "log"], timeout=10, text=True)
        command = ["/usr/bin/sudo", "-n", "/usr/bin/pmset"]
        subprocess.run([*command, "schedule", "wake", wake, owner], check=True, capture_output=True, timeout=5)
        try:
            subprocess.run([*command, "sleepnow"], check=True, capture_output=True, timeout=5)
            deadline = Deadline(timeout)
            def woke():
                log = subprocess.check_output(["/usr/bin/pmset", "-g", "log"], timeout=min(10, deadline.remaining()), text=True)
                delta = log[len(before):] if log.startswith(before) else ""
                if "Entering Sleep" not in delta or not re.search(r"Wake from|Wake\s+", delta):
                    return None
                return delta
            log = wait_for(woke, deadline)
            deadline.remaining()
            (context["evidence"] / ("sleepwake-" + str(number) + ".log")).write_text(log)
            deadline.remaining()
            if after_wake is not None:
                after_wake(context, number, deadline)
            verify_draft(context, expected, deadline.remaining())
            deadline.remaining()
            image = snapshot(context, "wake-" + str(number))
            deadline.remaining()
            atomic_json(context["evidence"] / "progress.json", dict(phase="sleepwake", sleep_cycles=number + 1,
                        case_id=context["case_id"]))
            deadline.remaining()
            result["snapshots"].append(image)
            result["sleep_cycles"] = number + 1
        finally:
            removed = subprocess.run([*command, "schedule", "cancel", "wake", wake, owner], capture_output=True, timeout=5)
            scheduled = subprocess.check_output(["/usr/bin/pmset", "-g", "sched"], text=True, timeout=5)
            atomic_json(context["evidence"] / ("wake-cleanup-" + str(number) + ".json"),
                        dict(owner=owner, wake=wake, cancel_exit=removed.returncode, own_event_remaining=owner in scheduled))
            if owner in scheduled:
                raise RuntimeError("case-owned wake schedule remains; next case refused")
            if held:
                background.start_awake(context, sys.modules[__name__])


def scenario(args):
    require_guest()
    if args.phase not in control.PHASES:
        raise RuntimeError("unknown native acceptance stage")
    manifest = read_json(args.campaign)
    fingerprint = verify_inputs(manifest)
    phase = next(row for row in manifest["phases"] if row["name"] == args.phase)
    context = dict(case_id=os.environ["ZWAI_NATIVE_CASE_ID"], helper=manifest["helper"],
                   data=Path(os.environ["ZWAI_NATIVE_DATA"]), evidence=Path(os.environ["ZWAI_NATIVE_EVIDENCE"]),
                   awake_until=time.monotonic() + manifest["budgets"][args.phase]["whole_case_seconds"])
    result = dict(case_id=context["case_id"], phase=args.phase, passed=False,
                  engine_pid_before=None, engine_pid_after=None,
                  snapshots=[], turn_ids=[], draft_verified=False)
    result["input_fingerprint"] = fingerprint
    receipt = context["evidence"] / "resilience.json"
    try:
        background.start_awake(context, sys.modules[__name__])
        context.update(context_for(manifest, args.timeout))
        background.prepare(context, sys.modules[__name__], manifest, args.timeout)
        result["engine_pid_before"] = context["engine_pid"]
        result["snapshots"].append(snapshot(context, "initial"))
        if native(context).get("textareas") != 1:
            native(context, "press", "Conversations")
            native(context, "press", "New conversation")
        submit(context, "Perform an offline checkpoint " + context["case_id"], args.timeout, result)
        expected = collect_draft(context, args.timeout)
        if draft_snapshot(context, evidence_label="before") != expected:
            raise RuntimeError("input evidence differs from the acknowledged unsent draft")
        background.start(context, sys.modules[__name__], args.timeout)
        background.resources(context, sys.modules[__name__])
        result["turn_ids"].extend(row["turn_id"] for row in context["background"].values())
        result["snapshots"].append(snapshot(context, "before"))
        prior_turns = {row["id"] for row in turn_rows(context)}
        if args.phase == "sleepwake":
            sleepwake(context, expected, phase["cycles"], args.timeout, result)
        elif args.phase == "soak":
            verify_draft(context, expected, args.timeout)
            state = soak.start(context, sys.modules[__name__], phase, manifest["budgets"][args.phase])
            while True:
                started = soak.begin_tick(state)
                tick = soak.record_tick(context, sys.modules[__name__], state, started)
                elapsed = time.monotonic() - state["started_monotonic"]
                if tick["seq"] % 10 == 1:
                    result["snapshots"].append(snapshot(context, "soak-" + str(tick["seq"])))
                atomic_json(context["evidence"] / "progress.json", dict(phase=args.phase, tick=tick["seq"],
                            elapsed_seconds=elapsed, case_id=context["case_id"]))
                if elapsed >= phase["duration_seconds"]:
                    break
                time.sleep(min(phase["interval_seconds"], max(0, phase["duration_seconds"] - elapsed)))
            soak.finish(context, sys.modules[__name__], state, result)
        elif args.phase in ("renderer", "gpu", "exhaustion"):
            fault_phases.run(context, sys.modules[__name__], args.phase, args.timeout, result)
        else:
            background.reopen(context, sys.modules[__name__], args.timeout)
        background.check(context, sys.modules[__name__])
        background.resources(context, sys.modules[__name__])
        verify_draft(context, expected, args.timeout)
        if {row["id"] for row in turn_rows(context)} != prior_turns:
            raise RuntimeError("UI recovery automatically sent an unsent draft")
        result["draft_verified"] = True
        atomic_json(context["evidence"] / "draft-after.json", draft_snapshot(context, evidence_label="after"))
        result["snapshots"].append(snapshot(context, "after"))
        background.finish(context, sys.modules[__name__], args.timeout, result)
        verify_draft(context, expected, args.timeout)
        # Send through the actual composer only after the unchanged unsent
        # snapshot was independently verified above.
        submit(context, expected["text"], args.timeout, result, expected_draft=expected)
        result["engine_pid_after"] = read_json(context["data"] / "engine.json")["pid"]
        verify_inputs(manifest)
        result["passed"] = True
        return 0
    except BaseException as error:
        result["failure"] = type(error).__name__ + ": " + str(error)
        raise
    finally:
        try:
            background.retain(context, sys.modules[__name__])
        finally:
            try:
                background.stop_awake(context, sys.modules[__name__])
            finally:
                atomic_json(receipt, result)


def main(*, launch_admission=None):
    def cancel(signum, frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, cancel)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("run", "launch"):
        run = commands.add_parser(name)
        run.add_argument("--zip", required=True, type=Path)
        run.add_argument("--source", required=True)
        run.add_argument("--evidence", required=True, type=Path)
        run.add_argument("--soak-seconds", type=float, default=18 * 3600)
        run.add_argument("--sleep-cycles", type=int, default=3)
        run.add_argument("--interval", type=float, default=60)
        run.add_argument("--stage-timeout", type=float, default=180)
        run.add_argument("--phases", nargs="+", default=list(control.PHASES), help="ordered stages to execute; unselected stages remain pending")
        run.add_argument("--launch-id", default=uuid.uuid4().hex, help=argparse.SUPPRESS)
    status = commands.add_parser("status")
    status.add_argument("--evidence", required=True, type=Path)
    cancel_case = commands.add_parser("cancel")
    cancel_case.add_argument("--evidence", required=True, type=Path)
    case = commands.add_parser("scenario")
    case.add_argument("--campaign", required=True, type=Path)
    case.add_argument("--phase", required=True, choices=control.PHASES)
    case.add_argument("--timeout", required=True, type=float)
    args = parser.parse_args()
    if args.command == "run":
        require_guest()
        return control.execute(args, campaign)
    if args.command in ("launch", "cancel"):
        require_guest()
        row = control.launch(args, Path(__file__).resolve(), admission=launch_admission) if args.command == "launch" else control.cancel(args.evidence)
        print(json.dumps(row, indent=2))
        return 0
    if args.command == "scenario":
        return scenario(args)
    print(json.dumps(campaign_status(args.evidence), indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(2)
