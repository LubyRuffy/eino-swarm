"""MCP case admission from this launch's product paint receipt, before any AX.

The product ready journal is a prerequisite, not native behavior acceptance.
The unchanged strict window/pixel checks still run afterwards on one deadline.
"""
import calendar
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import stat
import time

import native_macos_smoke as smoke
from native_macos_resilience_fault import process_identity


def sha(path):
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest() if hasattr(hashlib, "file_digest") else hashlib.sha256(source.read()).hexdigest()


def strict_record(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("MCP startup receipt has duplicate keys")
        value[key] = item
    return value


def read_object(path):
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "r") as source:
        value = json.load(source, object_pairs_hook=strict_record)
    if not isinstance(value, dict):
        raise ValueError("MCP startup receipt must be an object")
    return value


def app_inventory(app):
    app = Path(app)
    if app.is_symlink() or not app.is_dir():
        raise ValueError("candidate must be a real App directory")
    files = {}
    for path in sorted(app.rglob("*")):
        info = path.lstat()
        row = dict(mode=stat.S_IMODE(info.st_mode))
        if stat.S_ISREG(info.st_mode):
            row.update(kind="file", sha256=sha(path), bytes=info.st_size)
        elif stat.S_ISDIR(info.st_mode):
            row.update(kind="directory")
        elif stat.S_ISLNK(info.st_mode):
            target = path.resolve(strict=True)
            if not target.is_relative_to(app.resolve(strict=True)):
                raise ValueError("candidate link escapes the App")
            row.update(kind="link", target=os.readlink(path))
        else:
            raise ValueError("candidate contains a special file")
        files[path.relative_to(app).as_posix()] = row
    return files


def freeze_candidate(source_receipt, controller_receipt=None, controller_sha256=None):
    source = read_object(source_receipt)
    production = source["production_inputs"]
    digest = hashlib.sha256(json.dumps(production, sort_keys=True).encode()).hexdigest()
    if digest != source["production_inputs_sha256"] or sha(source["archive"]) != source["archive_sha256"]:
        raise ValueError("candidate source/archive receipt differs")
    manifest = dict(schema=1, version=source["version"], baseline=source["baseline"],
        production_inputs_sha256=digest, source_receipt_sha256=sha(source_receipt),
        archive_sha256=source["archive_sha256"], executable_sha256=source["executable_sha256"],
        expected_mock=True, app_files=app_inventory(source["app"]))
    if controller_receipt is not None:
        if sha(controller_receipt) != controller_sha256:
            raise ValueError("trusted controller descriptor changed")
        controller = read_object(controller_receipt)
        manifest["controller"] = {key: controller[key] for key in
            ("pid", "bootstrap_pid", "wake_pid", "expected_uid", "expected_vm_id", "expires_at", "binary_sha256")}
        manifest["controller"]["descriptor_sha256"] = controller_sha256
    validate_candidate(manifest, Path(source["app"]))
    return manifest


def validate_candidate(manifest, app):
    if (manifest.get("schema") != 1 or manifest.get("expected_mock") is not True
            or not isinstance(manifest.get("version"), str) or not manifest["version"]
            or not isinstance(manifest.get("baseline"), str) or not re.fullmatch(r"[0-9a-f]{40}", manifest["baseline"])
            or any(not isinstance(manifest.get(key), str) or not re.fullmatch(r"[0-9a-f]{64}", manifest[key]) for key in
                   ("production_inputs_sha256", "source_receipt_sha256", "archive_sha256", "executable_sha256"))):
        raise ValueError("candidate source identity is incomplete")
    actual = app_inventory(app)
    if not actual or actual != manifest.get("app_files"):
        raise ValueError("complete candidate App contents differ")
    with (Path(app) / "Contents/Info.plist").open("rb") as source:
        metadata = plistlib.load(source)
    if not isinstance(metadata, dict):
        raise ValueError("candidate bundle metadata must be an object")
    executable = metadata.get("CFBundleExecutable")
    if (not isinstance(executable, str) or Path(executable).name != executable
            or metadata.get("CFBundleShortVersionString") != manifest["version"]
            or metadata.get("CFBundleVersion") != manifest["version"]
            or sha(Path(app) / "Contents/MacOS" / executable) != manifest["executable_sha256"]):
        raise ValueError("candidate executable or bundle version differs")
    return dict(version=manifest["version"], baseline=manifest["baseline"],
                production_inputs_sha256=manifest["production_inputs_sha256"],
                archive_sha256=manifest["archive_sha256"], app_entries=len(actual))


def case_preparation(manifest, candidate_path, launch_path):
    candidate_sha = sha(candidate_path)
    def prepare(lock, receipt, environment, processes, scope, work_deadline):
        del lock, processes, scope, work_deadline
        app = Path(environment["ZWAI_NATIVE_APP"]).resolve(strict=True)
        data = Path(environment["ZWAI_NATIVE_DATA"]).resolve(strict=True)
        destination = Path(environment["ZWAI_NATIVE_EVIDENCE"]).resolve(strict=True) / "mcp-launch.json"
        if (app.parent != data.parent or receipt["case_id"] != environment["ZWAI_NATIVE_CASE_ID"]
                or destination != Path(launch_path).parent.resolve(strict=True) / Path(launch_path).name):
            raise ValueError("MCP preparation lost private case ownership")
        validate_candidate(manifest, app)
        state = dict(case_id=receipt["case_id"], app=str(app), data=str(data),
                     candidate_manifest_sha256=candidate_sha, expected_mock=True,
                     controller=manifest.get("controller"))
        def before_launch():
            if "prelaunch_wall_ns" in state:
                raise ValueError("MCP launch timestamp already armed")
            if sha(candidate_path) != candidate_sha:
                raise ValueError("MCP candidate manifest changed before launch")
            validate_candidate(manifest, app)
            state["prelaunch_wall_ns"] = time.time_ns()
            smoke.runner.save_json(destination, state)
        return smoke.runner.CasePreparation(before_launch, lambda process, deadline: None)
    return prepare


def load_launch(context, candidate_path, launch_path):
    expected = context["evidence"] / "mcp-launch.json"
    if Path(launch_path).resolve(strict=True) != expected:
        raise ValueError("MCP launch receipt belongs to another evidence directory")
    launch = read_object(expected)
    if (launch.get("case_id") != context["case_id"] or launch.get("app") != str(context["app"])
            or launch.get("data") != str(context["data"]) or launch.get("expected_mock") is not True
            or launch.get("candidate_manifest_sha256") != sha(candidate_path)
            or launch.get("controller") != read_object(candidate_path).get("controller")
            or type(launch.get("prelaunch_wall_ns")) is not int
            or not 0 < launch["prelaunch_wall_ns"] <= time.time_ns()):
        raise ValueError("MCP before-launch receipt identity differs")
    return launch


def live_binding(context, manifest, deadline):
    origin, lease = smoke.wait_engine(context, deadline)
    metadata = smoke.request_json(origin, "/api/meta", deadline)
    index = context.get("mcp_binding_count", 0) + 1
    context["mcp_binding_count"] = index
    smoke.runner.save_json(context["evidence"] / ("mcp-engine-binding-%04d.json" % index),
                           dict(origin=origin, lease=lease, metadata=metadata,
                                shared_deadline_monotonic=deadline.ends))
    desktop = [row for row in metadata.get("clients", []) if row.get("surface") == "desktop"]
    if (lease.get("version") != manifest["version"] or metadata.get("version") != manifest["version"]
            or metadata.get("mode") != "engine" or metadata.get("mock") is not True
            or len(desktop) != 1 or type(desktop[0].get("pid")) is not int
            or desktop[0]["pid"] != context["app_pid"]):
        raise ValueError("MCP engine lease/meta/version/presence differs")
    shell = process_identity(context["app_pid"])
    engine = process_identity(lease["pid"])
    for pid, kind, identity in ((context["app_pid"], "desktop", shell), (lease["pid"], "engine", engine)):
        executable = smoke.assert_process(context, pid, kind)
        if (identity is None or identity.get("pid") != pid or not isinstance(identity.get("birth"), list)
                or len(identity["birth"]) != 2 or any(type(n) is not int for n in identity["birth"])
                or identity["birth"][0] <= 0 or not 0 <= identity["birth"][1] < 1000000
                or Path(identity["executable"]).resolve(strict=True) != executable.resolve(strict=True)
                or sha(executable) != manifest["executable_sha256"]):
            raise ValueError("MCP process birth/executable source differs")
    validate_candidate(manifest, context["app"])
    return dict(origin=origin, shell=shell, engine=engine, version=manifest["version"], data=str(context["data"]))


def utc_nanoseconds(value):
    match = re.fullmatch(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z", value) if isinstance(value, str) else None
    if not match:
        raise ValueError("product timestamp is not UTC RFC3339Nano")
    date = datetime.datetime.strptime(match[1], "%Y-%m-%dT%H:%M:%S")
    return calendar.timegm(date.utctimetuple()) * 10**9 + int((match[2] or "").ljust(9, "0"))


def product_session(context):
    cache = context["home"]
    for part in ("Library", "Caches", "zwai"):
        cache = cache / part
        info = cache.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid():
            raise ValueError("product cache is not a case-owned directory")
    copies = sorted(cache.glob("desktop-recovery-*"))
    if not copies:
        raise FileNotFoundError("product session is not present yet")
    if len(copies) != 1 or not stat.S_ISDIR(copies[0].lstat().st_mode):
        raise ValueError("product session is ambiguous or a link")
    with os.fdopen(os.open(copies[0] / "session", os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_size != 16:
            raise ValueError("product session marker identity differs")
        raw = source.read(17)
    if not re.fullmatch(rb"[0-9a-f]{16}", raw):
        raise ValueError("product session marker format differs")
    return raw.decode("ascii")


def read_journal(path):
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_uid != os.getuid() or before.st_size > 65536:
            raise ValueError("product journal is not a bounded case-owned file")
        try:
            fcntl.flock(source, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            return None
        before = os.fstat(source.fileno())
        raw = source.read(65537)
        after = os.fstat(source.fileno())
        identity = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_mtime_ns, item.st_ctime_ns)
        if identity(before) != identity(after) or len(raw) != after.st_size or len(raw) > 65536:
            raise ValueError("product journal changed while read-locked")
        return raw, dict(device=after.st_dev, inode=after.st_ino, bytes=len(raw))


def ready_record(raw, session, binding, start):
    if not raw or not raw.endswith(b"\n"):
        raise ValueError("product journal is empty or incomplete")
    allowed = {"session_id", "generation", "attempt", "shell_pid", "engine_pid", "thread_id", "turn_id", "phase", "reason", "at", "duration_ms"}
    phases = {"ready", "resumed", "suspended", "exhausted", "probe_failed", "reloading", "replacing", "recovered", "replacement_failed"}
    records = [json.loads(line, object_pairs_hook=strict_record) for line in raw.decode().splitlines()]
    for row in records:
        if (not isinstance(row, dict) or not set(row) <= allowed
                or any(type(row.get(key)) is not int for key in ("generation", "attempt", "shell_pid", "engine_pid"))
                or row.get("session_id") != session or row["generation"] != 0 or row["attempt"] != 0
                or row["shell_pid"] != binding["shell"]["pid"] or row["engine_pid"] != binding["engine"]["pid"]
                or not isinstance(row.get("phase"), str) or row["phase"] not in phases
                or any(key in row and not isinstance(row[key], str) for key in ("reason", "thread_id", "turn_id"))
                or ("duration_ms" in row and (type(row["duration_ms"]) is not int or row["duration_ms"] < 0))
                or not start <= utc_nanoseconds(row.get("at")) <= time.time_ns()):
            raise ValueError("product ready identity, generation or freshness differs")
    ready = [row for row in records if row["phase"] == "ready"]
    if len(ready) > 1:
        raise ValueError("product ready identity is not unique")
    return ready[0] if ready else None


def wait_product_ready(context, manifest, launch, deadline):
    if context.get("native_call_count", 0) or context.get("native_input_attempted"):
        raise ValueError("product readiness must precede every QA native call")
    initial = live_binding(context, manifest, deadline)
    context.update(origin=initial["origin"], engine_pid=initial["engine"]["pid"], readiness_stage="product_ready")
    destination = context["evidence"] / "product-readiness"
    destination.mkdir(mode=0o700, exist_ok=False)
    while True:
        deadline.remaining("MCP product pixel readiness")
        try:
            session = product_session(context)
            diagnostics = context["data"] / "diagnostics"
            info = diagnostics.lstat()
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid():
                raise ValueError("product diagnostics directory identity differs")
            expected = diagnostics / ("desktop-" + session + ".jsonl")
            journals = sorted(diagnostics.glob("desktop-*.jsonl"))
            if journals and journals != [expected]:
                raise ValueError("product journal session is not unique")
            sample = read_journal(expected)
        except FileNotFoundError:
            smoke.pending(context, deadline, "product_ready", "product_journal_missing")
            continue
        if sample is None:
            smoke.pending(context, deadline, "product_ready", "product_journal_writer_busy")
            continue
        raw, identity = sample
        index = context.get("product_journal_count", 0) + 1
        context["product_journal_count"] = index
        path = destination / ("%04d.jsonl" % index)
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as out:
            out.write(raw)
        receipt = dict(ready=False, journal=str(expected), file_identity=identity, raw_sha256=sha(path),
            before_launch_wall_ns=launch["prelaunch_wall_ns"], shared_deadline_monotonic=deadline.ends,
            binding=initial, source=manifest, helper_calls=0, inputs=0)
        try:
            record = ready_record(raw, session, initial, launch["prelaunch_wall_ns"])
            if record is not None:
                if live_binding(context, manifest, deadline) != initial or product_session(context) != session:
                    raise ValueError("live process/engine/session changed before QA admission")
                receipt.update(ready=True, ready_record=record, live_binding_rechecked=True)
        except BaseException as error:
            receipt.update(refused=True, error_type=type(error).__name__, error=str(error))
            try:
                smoke.runner.save_json(destination / ("%04d.json" % index), receipt)
            except BaseException as secondary:
                error.__notes__ = [*getattr(error, "__notes__", []), "MCP readiness evidence failed: " + type(secondary).__name__]
            raise
        smoke.runner.save_json(destination / ("%04d.json" % index), receipt)
        if receipt["ready"]:
            return initial
        smoke.pending(context, deadline, "product_ready", "product_ready_missing")
