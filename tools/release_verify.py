"""Local verification stages for the existing release entry; never publish or drive a VM."""
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import hashlib
import fcntl
import json
import os
from pathlib import Path
import platform
import re
import socket
import signal
import subprocess
import sys
import tempfile
import threading
import uuid


NATIVE_PENDING = "required_separate_guest_acceptance"
E2E_PROFILES = ("REASONING_FIELDS", "MODEL_API_ERROR", "WORKER_ROSTER", "SCHEDULE_ANSWER",
                "WORKER_COMPLETION", "GOAL_SCHEDULE", "GOAL_PAUSE", "HUMAN_WAKE_POLICY",
                "MEMORY_REVIEW", "BUILTIN_TOOLS", "SCHEDULE_WORKER_OWNERSHIP", "GENERATION_INTERRUPTION")


@contextmanager
def cancellation(cancel):
    """Signal only our owned commands; let the stage runner retain and reap them."""
    previous = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        for sig in previous:
            signal.signal(sig, lambda *_: cancel.set())
        yield
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def commands(root, version, evidence):
    """Keep one complete default suite; E2E remains serial with its own server/data."""
    node = root / "frontend/node_modules/@playwright/test/cli.js"
    return [
        ("frontend-build", ("make", "frontend"), root, (root / "frontend/dist",)),
        ("format", ("make", "fmt-check"), root, ()),
        ("vet", ("go", "vet", "./..."), root, ()),
        ("go-race", ("go", "test", "-race", "-cover", "-timeout", "25m", "./..."), root, ()),
        ("frontend-lint", ("npm", "run", "lint"), root / "frontend", ()),
        ("frontend-unit", ("npm", "test", "--", "--maxWorkers=2", "--minWorkers=1"), root / "frontend", ()),
        ("mobile-lint", ("npm", "run", "lint"), root / "mobile", ()),
        ("mobile-unit", ("npm", "test", "--", "--maxWorkers=2", "--minWorkers=1"), root / "mobile", ()),
        ("tools", (sys.executable, "-B", "-m", "unittest", "discover", "-s", "tools", "-p", "test_*.py"), root, ()),
        ("docs", (sys.executable, "-B", "tools/docs_check.py"), root, ()),
        ("pc-e2e", ("node", str(node), "test", "--workers=1", "--retries=0",
                    "--output=" + str(evidence / "playwright")), root / "frontend", ()),
        ("package", ("make", "desktop-release", "VERSION=" + version), root,
         (root / "bin" / ("zwai-" + version + "-darwin-" + architecture() + ".zip"),)),
    ]


def architecture():
    return {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "AMD64": "amd64"}.get(platform.machine(), "unsupported")


def plan(root, version, evidence, jobs=2):
    return dict(status="plan_only", version=version, jobs=jobs, native_status=NATIVE_PENDING,
                stages=[dict(name=name, argv=list(argv)) for name, argv, _, _ in commands(root, version, evidence)])


def private_json(path, value):
    """Atomic replace only of our own regular report; never follow a linked target."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        raise ValueError("verification evidence cannot be a symbolic link")
    descriptor, temporary = tempfile.mkstemp(prefix="." + path.name, dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def package_provenance(path, version, source_sha, evidence):
    """Read final signed bytes. Later signing/notarization/stapling requires new evidence."""
    import plistlib
    import zipfile
    from release import validate_macos_zip
    validate_macos_zip(path)
    with zipfile.ZipFile(path) as archive:
        info = plistlib.loads(archive.read("zwai.app/Contents/Info.plist"))
        executable = info.get("CFBundleExecutable", "")
        if (not isinstance(executable, str) or not executable or Path(executable).name != executable
                or info.get("CFBundleShortVersionString") != version):
            raise ValueError("package executable/version differs from the frozen candidate")
        raw = archive.read("zwai.app/Contents/MacOS/" + executable)
    with tempfile.NamedTemporaryFile(prefix="build-info-", dir=evidence) as core:
        core.write(raw)
        core.flush()
        result = subprocess.run(["go", "version", "-m", core.name], text=True, capture_output=True, timeout=15)
    settings = dict(re.findall(r"^\s*build\s+([^=\s]+)=(.*)$", result.stdout, re.M))
    if result.returncode or settings.get("vcs.revision") != source_sha or settings.get("vcs.modified") != "false":
        raise ValueError("final package lacks exact clean VCS provenance")
    return dict(version=version, source_sha=source_sha, asset_name=path.name,
                asset_sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                boundary="final_package_bytes_after_build_signing", native_status=NATIVE_PENDING)


def evidence_path(root, evidence):
    root, evidence = Path(root).resolve(strict=True), Path(evidence).absolute()
    # Playwright's existing webServer interpolates dataDir into a shell command.
    # Refuse unsafe paths before any writes; never trim or redirect a selection.
    if not re.fullmatch(r"[A-Za-z0-9_./-]+", str(evidence)):
        raise ValueError("E2E evidence path must contain only letters, digits, dot, slash, underscore and hyphen")
    if evidence.resolve().is_relative_to(root):
        raise ValueError("verification evidence must be outside the source checkout")
    return evidence


def verify(root, version, evidence, *, jobs=2, e2e_port=None, lock_timeout=30):
    evidence = evidence_path(root, evidence)
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    if evidence.is_symlink():
        raise ValueError("verification evidence directory cannot be a symbolic link")
    descriptor = os.open(evidence / "verification.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "r+") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("another verification owns this evidence directory") from None
        return verification_attempt(root, version, evidence, jobs=jobs, e2e_port=e2e_port, lock_timeout=lock_timeout)


def verification_attempt(root, version, evidence, *, jobs=2, e2e_port=None, lock_timeout=30):
    attempt = uuid.uuid4().hex
    current = evidence / "verification.json"
    if current.is_symlink():
        raise ValueError("verification report cannot be a symbolic link")
    if current.is_file():
        private_json(evidence / "history" / ("previous-" + attempt + ".json"), json.loads(current.read_text()))
    private_json(current, dict(status="running", attempt=attempt, version=version, native_status=NATIVE_PENDING))
    code = 1
    try:
        code = _verify(root, version, evidence, jobs=jobs, e2e_port=e2e_port, lock_timeout=lock_timeout)
        row = json.loads(current.read_text())
        if row.get("status") == "running":
            row["status"] = "blocked" if code == 2 else "failed"
        row.update(attempt=attempt, exit_code=code)
        private_json(current, row)
        private_json(evidence / "history" / (attempt + ".json"), row)
        return code
    except BaseException as error:
        failure = dict(status="failed", attempt=attempt, version=version, exit_code=1,
                       error_type=type(error).__name__, native_status=NATIVE_PENDING)
        try:
            private_json(current, failure)
            private_json(evidence / "history" / (attempt + ".json"), failure)
        except (OSError, ValueError):
            pass  # Keep the original action/validation failure.
        raise


def source_revision(root):
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()


def harness_fingerprint():
    tools = Path(__file__).parent
    return {name: hashlib.sha256((tools / name).read_bytes()).hexdigest()
            for name in ("release.py", "release_verify.py", "release_stages.py", "release_preflight.py")}


def _verify(root, version, evidence, *, jobs=2, e2e_port=None, lock_timeout=30):
    from release_preflight import collect_preflight, stable_inputs
    from release_stages import StageRunner, StageSpec, mark_queued, interval_union_seconds
    root, evidence = Path(root).resolve(strict=True), Path(evidence).absolute()
    if not 1 <= jobs <= 2:
        raise ValueError("verification jobs must be 1 or 2")
    source = source_revision(root)
    report = collect_preflight(root, source, projects=("frontend", "mobile"), collect_e2e=True)
    private_json(evidence / "preflight.json", report)
    if report["status"] != "passed":
        return 2
    if platform.system() != "Darwin" or architecture() == "unsupported":
        raise ValueError("macOS package verification requires a supported macOS build host")
    previous = evidence / "source-inputs.json"
    if e2e_port is None and previous.is_file() and not previous.is_symlink():
        saved = json.loads(previous.read_text())
        if saved.get("source_sha") == source and saved.get("version") == version:
            e2e_port = saved.get("config", {}).get("e2e_port")
    if e2e_port is None:
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            e2e_port = reservation.getsockname()[1]
    if type(e2e_port) is not int or not 1 <= e2e_port <= 65535:
        raise ValueError("invalid isolated E2E port")
    configuration = dict(jobs=jobs, e2e_port=e2e_port, goflags="-buildvcs=true", retries=0,
                         e2e_workers=1, native_status=NATIVE_PENDING)
    binding = stable_inputs(report)
    identity = dict(source_sha=source, version=version, lockfiles=report["inputs"],
                    toolchain=binding["toolchain"], dependencies=binding, config=configuration,
                    harness=harness_fingerprint())
    # Persist source proof before the first build. No release/tag/batch state is touched.
    private_json(evidence / "source-inputs.json", identity)
    env = {"GOFLAGS": "-buildvcs=true", "GOWORK": "off", "PYTHONDONTWRITEBYTECODE": "1"}
    specs = []
    for name, argv, cwd, outputs in commands(root, version, evidence):
        overrides = dict(env)
        if name == "pc-e2e":
            overrides.update(ZWAI_E2E_PORT=str(e2e_port), ZWAI_E2E_DATA_DIR=str(evidence / "e2e-data"))
            # Explicitly select the complete default suite, independent of a shell's opt-in flags.
            overrides.update({"ZWAI_E2E_" + key: "0" for key in E2E_PROFILES})
        inputs = (root / "frontend/dist",) if name in ("pc-e2e", "package") else ()
        specs.append(StageSpec(name=name, argv=argv, cwd=cwd, inputs=inputs, outputs=outputs,
                               timeout=1800, env=overrides,
                               owned_data=evidence / "e2e-data" if name == "pc-e2e" else None))
    results = {}
    cancel = threading.Event()
    with cancellation(cancel), StageRunner(evidence, identity=identity, stages=specs, lock_timeout=lock_timeout,
                     lock_path=root / ".git/zwai-release-verification.lock", cancel=cancel) as runner:
        # Check again after queueing: another owner may have changed source or dependencies.
        latest = collect_preflight(root, source, projects=("frontend", "mobile"), collect_e2e=True)
        if latest["status"] != "passed" or stable_inputs(latest) != binding:
            raise ValueError("verification inputs changed while waiting for the checkout lock")
        results["frontend-build"] = runner.run("frontend-build")
        if results["frontend-build"]["status"] not in ("passed", "reused"):
            private_json(evidence / "verification.json", dict(status="failed", identity=identity, stages=results))
            return 1
        with ThreadPoolExecutor(max_workers=jobs) as executor:
            futures = {name: executor.submit(runner.run, name, queued_at=mark_queued()) for name, *_ in commands(root, version, evidence)
                       if name not in ("frontend-build", "package")}
            results.update({name: future.result() for name, future in futures.items()})
        if any(result["status"] not in ("passed", "reused") for result in results.values()):
            private_json(evidence / "verification.json", dict(status="failed", identity=identity, stages=results))
            return 1
        results["package"] = runner.run("package")
        if results["package"]["status"] not in ("passed", "reused"):
            private_json(evidence / "verification.json", dict(status="failed", identity=identity, stages=results))
            return 1
        final = collect_preflight(root, source, projects=("frontend", "mobile"), collect_e2e=False)
        if final["status"] != "passed" or stable_inputs(final) != binding:
            raise ValueError("verification inputs changed during execution")
        if harness_fingerprint() != identity["harness"]:
            raise ValueError("verification implementation changed during execution")
        asset = next(spec.outputs[0] for spec in specs if spec.name == "package")
        try:
            package = package_provenance(asset, version, source, evidence)
        except (OSError, ValueError, subprocess.SubprocessError):
            private_json(evidence / "verification.json", dict(status="failed", failure="package_provenance_refused",
                         identity=identity, stages=results))
            raise
        intervals = [(row["command_started"]["monotonic"], row["command_ended"]["monotonic"])
                     for row in results.values() if "command_started" in row and "command_ended" in row]
        private_json(evidence / "verification.json", dict(status="headless_passed_native_pending", identity=identity,
                     stages=results, package=package, native_status=NATIVE_PENDING,
                     execution_union_seconds=interval_union_seconds(intervals), pipeline_queue=runner.pipeline_queue))
    print("Headless verification passed; exact final-package guest acceptance remains required.")
    return 3
