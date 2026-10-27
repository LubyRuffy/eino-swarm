"""One host/UID/project admission lease for cooperating heavy verification.

The mutex is authoritative; old receipts are only history. A read-only guard
also observes already-running manual checks. It cannot stop a new command that
bypasses this protocol after admission, and never signals an observed process.
"""
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import threading
import time
import uuid

from native_macos_case import process_identity as process_arguments
from native_macos_resilience_fault import process_identity


class HostLeaseBlocked(RuntimeError):
    pass


def canonical_module(root):
    text = (Path(root) / "go.mod").read_text()
    match = re.search(r"^module\s+([^\s]+)\s*$", text, re.M)
    if not match:
        raise ValueError("host verification lease requires an identified Go module")
    value = json.loads(match[1]) if match[1].startswith('"') else match[1]
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9._~/-]+", value):
        raise ValueError("host verification lease requires a canonical module path")
    return value


def project_root(cwd):
    current = Path(cwd).resolve(strict=True)
    if not current.is_dir():
        raise ValueError("process working directory is not a directory")
    for directory in (current, *current.parents):
        if (directory / "go.mod").is_file():
            return directory, canonical_module(directory)
    return None, None


def validation_kind(argv):
    """Classify actual tokens in memory, never retain argv or environment."""
    if not argv:
        return None
    executable = Path(argv[0]).name
    tokens = argv[1:]
    if executable.endswith(".test") and any(token.startswith("-test.") for token in tokens):
        return "go_test_binary"
    if executable == "go":
        if tokens and tokens[0].startswith("-C="):
            tokens = tokens[1:]
        elif tokens[:1] == ["-C"]:
            tokens = tokens[2:]
        return "go_test" if tokens[:1] == ["test"] else None
    if executable in ("make", "gmake"):
        heavy = {"test", "test-go", "test-web", "test-mobile", "check", "e2e"}
        return "make_validation" if any(token in heavy for token in tokens) else None
    if executable.startswith("python") or executable in ("Python", "pypy", "pypy3"):
        # A release controller can be waiting on this mutex without doing work.
        # Actual Go/Node/unittest workers are observed independently; classifying
        # the controller would make the owner wait for its own queued contender.
        if "-m" in tokens:
            index = tokens.index("-m")
            if tokens[index+1:index+2] == ["unittest"] and "discover" in tokens[index+2:]:
                return "python_suite"
    if executable in ("node", "nodejs", "npm", "npx"):
        if any(token in ("--list", "--version", "--help") for token in tokens):
            return None
        names = [Path(token).name for token in tokens]
        if any(name.startswith("vitest") for name in names):
            return "vitest"
        if (any("playwright" in token for token in tokens) and "test" in tokens):
            return "playwright"
        if executable in ("npm", "npx") or "npm-cli.js" in names:
            return "npm_tests" if any(token in ("test", "e2e") for token in tokens) else None
    return None


def effective_directory(argv, cwd):
    """Bind known directory switches as well as the kernel working directory."""
    executable, tokens = Path(argv[0]).name, argv[1:]
    switches = set()
    file_switches = set()
    if executable == "go":
        switches = {"-C"}
    elif executable in ("make", "gmake"):
        switches = {"-C", "--directory"}
    elif executable in ("node", "nodejs", "npm", "npx"):
        switches = {"--prefix", "--cwd", "--root"}
        file_switches = {"--config", "-c"}
    elif "-m" in tokens and "unittest" in tokens:
        switches = {"-s", "--start-directory"}
    selected = Path(cwd)
    for index, token in enumerate(tokens):
        option, separator, value = token.partition("=")
        if option not in switches | file_switches:
            short = next((flag for flag in ("-C", "-s") if flag in switches and token.startswith(flag) and len(token) > len(flag)), None)
            if short:
                option, separator, value = short, True, token[len(short):]
        if option not in switches | file_switches:
            continue
        if not separator:
            if index + 1 >= len(tokens):
                raise ValueError("validation working directory switch has no value")
            value = tokens[index+1]
        base = selected if executable in ("make", "gmake") and option in ("-C", "--directory") else Path(cwd)
        target = (base / value).resolve(strict=True)
        selected = target.parent if option in file_switches and target.is_file() else target
    return selected


def process_cwd(pid, *, timeout=2):
    if sys.platform.startswith("linux"):
        return Path(os.readlink(f"/proc/{pid}/cwd"))
    result = subprocess.run(["/usr/sbin/lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"],
                            capture_output=True, text=True, timeout=timeout)
    paths = [line[1:] for line in result.stdout.splitlines() if line.startswith("n")]
    if result.returncode or len(paths) != 1 or not paths[0].startswith("/"):
        raise RuntimeError("process working directory could not be verified")
    return Path(paths[0])


def active_validations(root, module, *, deadline=None):
    """Observe a fresh inventory under the lease, binding PID birth twice."""
    del root  # Copies are matched by canonical module, never one checkout path.
    deadline = time.monotonic() + 3 if deadline is None else deadline
    def remaining():
        left = deadline - time.monotonic()
        if left <= 0:
            raise TimeoutError("host inventory deadline exhausted")
        return left
    try:
        # Let the OS select the effective UID. A host-wide ps table can contain
        # unresolvable foreign UIDs; they are not evidence about this user's work.
        listing = subprocess.run(["/bin/ps", "-u", str(os.getuid()), "-o", "pid=,comm="],
                                 capture_output=True, text=True, timeout=min(3, remaining()))
        if listing.returncode:
            raise RuntimeError("process inventory failed")
    except (OSError, RuntimeError, subprocess.SubprocessError):
        return {"status": "unknown", "processes": [], "reason": "process_inventory_unavailable"}
    observations, unknown = [], False
    for line in listing.stdout.splitlines():
        parts = line.strip().split(None, 1)
        if len(parts) != 2 or not parts[0].isdigit():
            return {"status": "unknown", "processes": [], "reason": "process_inventory_malformed"}
        pid, executable = int(parts[0]), Path(parts[1]).name
        if pid == os.getpid() or not (
                executable in ("go", "node", "nodejs", "npm", "npx", "make", "gmake", "Python", "pypy", "pypy3")
                or executable.startswith("python") or executable.endswith(".test")):
            continue
        identity = None
        try:
            remaining()
            identity = process_identity(pid)
            if identity is None:
                continue
            argv, _ = process_arguments(pid)
            if argv is None:
                raise RuntimeError("process arguments are unavailable")
            kind = validation_kind(argv)
            if kind is None:
                if process_identity(pid) != identity:
                    raise RuntimeError("process identity changed during classification")
                continue
            cwd = process_cwd(pid, timeout=min(2, remaining()))
            effective = effective_directory(argv, cwd)
            directory, observed_module = project_root(effective)
            if process_identity(pid) != identity:
                raise RuntimeError("process identity changed during observation")
            # A config can set an arbitrary root; its directory is only a hint.
            # It cannot erase a same-project kernel cwd, and foreign hints cannot
            # prove exclusion without evaluating arbitrary configuration code.
            config_selected = Path(argv[0]).name in ("node", "nodejs", "npm", "npx") and any(
                token in ("--config", "-c") or token.startswith("--config=") for token in argv[1:])
            if config_selected:
                kernel_directory, kernel_module = project_root(cwd)
                if kernel_module == module:
                    directory, observed_module = kernel_directory, kernel_module
                elif observed_module != module:
                    raise RuntimeError("configured validation root could not be verified")
            if observed_module is None:
                raise RuntimeError("heavy process project could not be verified")
            if observed_module != module:
                continue
            observations.append(dict(identity, cwd=str(cwd), effective_cwd=str(effective), project_root=str(directory), kind=kind))
        except (OSError, RuntimeError, ValueError, subprocess.SubprocessError):
            # Departure is known only from a fresh kernel observation, not a
            # stale row or a ps/lsof error. A live unknown remains a blocker.
            try:
                current = process_identity(pid)
                absent = current is None
            except (OSError, RuntimeError, ValueError):
                current = identity
                absent = False
            if absent:
                continue
            unknown = True
            observations.append(dict(pid=pid, birth=(identity or {}).get("birth"),
                                     kind="unknown", reason="process_identity_or_project_unverified"))
            if time.monotonic() >= deadline:
                return {"status": "unknown", "processes": observations, "reason": "process_inventory_deadline"}
    return {"status": "unknown" if unknown else "blocked" if observations else "clear", "processes": observations}


def _private_json(path, row):
    temporary = path.with_name("." + path.name + "-" + uuid.uuid4().hex)
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "w") as stream:
            json.dump(row, stream, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


class HostValidationLease:
    def __init__(self, root, evidence, *, timeout=30, cancel=None, lock_root=Path("/tmp"), inventory=None):
        if not math.isfinite(timeout) or not 0 <= timeout <= 3600:
            raise ValueError("host lease wait must be finite and bounded")
        self.root, self.evidence = Path(root).resolve(strict=True), Path(evidence)
        self.module = canonical_module(self.root)
        self.timeout, self.cancel = timeout, cancel or threading.Event()
        key = hashlib.sha256(self.module.encode()).hexdigest()[:24]
        self.lock_path = Path(lock_root) / f"zwai-heavy-validation-{os.getuid()}-{key}.lock"
        self.inventory = inventory
        self.stream = None
        self.row = dict(schema_version=1, module=self.module, checkout=str(self.root),
                        status="queued", queued_wall=time.time(), queued_monotonic=time.monotonic(), observations=[])
        self.receipt = self.evidence / ("host-lease-" + uuid.uuid4().hex + ".json")

    def _save(self):
        self.row.update(updated_wall=time.time(), updated_monotonic=time.monotonic())
        _private_json(self.receipt, self.row)

    def _owner(self, status):
        self.stream.seek(0)
        self.stream.truncate()
        json.dump(dict(schema_version=1, module=self.module, owner=self.row["owner"],
                       status=status, receipt=str(self.receipt)), self.stream)
        self.stream.flush()
        os.fsync(self.stream.fileno())

    def _wait(self, deadline, reason):
        self.row["wait_reason"] = reason
        self._save()
        if self.cancel.is_set():
            raise InterruptedError("host validation admission cancelled")
        left = deadline - time.monotonic()
        if left <= 0:
            raise HostLeaseBlocked("host validation admission blocked; inspect this attempt's host-lease receipt")
        self.cancel.wait(min(.25, left))

    def __enter__(self):
        if self.stream is not None:
            raise RuntimeError("host validation lease is already held")
        self.evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
        if self.evidence.is_symlink():
            raise ValueError("host lease evidence must be a real directory")
        self._save()
        deadline = time.monotonic() + self.timeout
        stream = None
        try:
            fd = os.open(self.lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1:
                os.close(fd)
                raise RuntimeError("host validation lock inode is unsafe")
            os.fchmod(fd, 0o600)
            stream = os.fdopen(fd, "r+")
            while True:
                try:
                    fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    self._wait(deadline, "cooperating_verification_active")
            self.stream = stream
            if canonical_module(self.root) != self.module:
                raise HostLeaseBlocked("host lease project identity changed while waiting")
            self.row["owner"] = process_identity(os.getpid())
            if self.row["owner"] is None:
                raise RuntimeError("host lease owner identity is unavailable")
            self._owner("checking_existing_processes")
            while True:
                if self.cancel.is_set():
                    raise InterruptedError("host validation admission cancelled")
                observed_wall, observed_monotonic = time.time(), time.monotonic()
                observation = (self.inventory(self.root, self.module) if self.inventory else
                               active_validations(self.root, self.module, deadline=deadline))
                status = observation.get("status")
                if status not in ("clear", "blocked", "unknown"):
                    raise RuntimeError("host admission inventory is malformed")
                safe = {key: observation[key] for key in ("status", "reason") if key in observation}
                fields = {"pid", "birth", "executable", "cwd", "effective_cwd", "project_root", "kind", "reason"}
                safe["processes"] = [{key: value for key, value in row.items() if key in fields}
                                     for row in observation.get("processes", [])]
                safe.update(observed_wall=observed_wall, observed_monotonic=observed_monotonic,
                            finished_monotonic=time.monotonic())
                self.row.setdefault("first_observation", safe)
                self.row["observations"] = [*self.row["observations"][-15:], safe]
                if status == "clear":
                    self.row.update(status="admitted", admitted_wall=time.time(), admitted_monotonic=time.monotonic())
                    self.row["queue_seconds"] = self.row["admitted_monotonic"] - self.row["queued_monotonic"]
                    self._owner("active")
                    self._save()
                    return self
                self._wait(deadline, "existing_manual_validation" if status == "blocked" else "existing_process_state_unknown")
        except BaseException:
            self.row["status"] = "cancelled" if self.cancel.is_set() else "blocked"
            self.row["queue_seconds"] = time.monotonic() - self.row["queued_monotonic"]
            if self.stream is not None and "owner" in self.row:
                try:
                    self._owner("released_after_refusal")
                except (OSError, ValueError) as error:
                    self.row["owner_evidence_error"] = type(error).__name__
            self.stream = None
            if stream is not None:
                stream.close()
            try:
                self._save()
            except (OSError, ValueError):
                pass  # The original admission/cancellation failure remains primary.
            raise

    def __exit__(self, exception_type, _error, _traceback):
        self.row.update(status="released", released_wall=time.time(), released_monotonic=time.monotonic(),
                        outcome="failed" if exception_type else "returned")
        self.row["hold_seconds"] = self.row["released_monotonic"] - self.row["admitted_monotonic"]
        try:
            self._owner("released")
            self._save()
        except (OSError, ValueError):
            if exception_type is None:
                raise
        finally:
            self.stream.close()
            self.stream = None
