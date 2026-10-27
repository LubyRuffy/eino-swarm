"""Private timing/evidence around fixed release commands, not a release publisher.

Callers register non-secret argv from the existing build/test entry points. Native
acceptance still belongs to native_macos_case.py; its receipts are never reused
here. A cached headless pass requires the same complete identity and file bytes.
"""
from contextlib import contextmanager
from dataclasses import dataclass, field
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import threading
import time
from types import MappingProxyType
from uuid import uuid4

from native_macos_case import ProcessScope, save_lock, stop_known_children, stop_owned


E2E_SWITCHES = frozenset({
    'ZWAI_E2E_BUILTIN_TOOLS', 'ZWAI_E2E_GENERATION_INTERRUPTION',
    'ZWAI_E2E_GOAL_PAUSE', 'ZWAI_E2E_GOAL_SCHEDULE', 'ZWAI_E2E_HUMAN_WAKE_POLICY',
    'ZWAI_E2E_MEMORY_REVIEW', 'ZWAI_E2E_MODEL_API_ERROR', 'ZWAI_E2E_REASONING_FIELDS',
    'ZWAI_E2E_SCHEDULE_ANSWER', 'ZWAI_E2E_SCHEDULE_WORKER_OWNERSHIP',
    'ZWAI_E2E_WORKER_COMPLETION', 'ZWAI_E2E_WORKER_ROSTER',
})
ENV_OVERRIDES = E2E_SWITCHES | {'ZWAI_E2E_PORT', 'ZWAI_E2E_DATA_DIR',
                                'CI', 'PYTHONDONTWRITEBYTECODE', 'GOFLAGS', 'GOWORK'}
# Build-affecting values may vary, but every value actually passed to a command
# belongs to its cache key. Authentication and runtime injection variables do not
# cross this boundary. In particular, a parent NODE_OPTIONS must never load code.
INHERITED_ENVIRONMENT = frozenset({
    'PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'SYSTEMROOT', 'COMSPEC', 'LANG', 'LC_ALL',
    'CI', 'GOROOT', 'GOPATH', 'GOCACHE', 'GOMODCACHE', 'GOTOOLCHAIN', 'GOFLAGS',
    'GOWORK', 'GOOS', 'GOARCH', 'GOAMD64', 'GOARM', 'GOARM64', 'GO386', 'GOMIPS',
    'GOMIPS64', 'GOWASM', 'CGO_ENABLED', 'CC', 'CXX', 'FC', 'AR', 'PKG_CONFIG',
    'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_CXXFLAGS', 'CGO_FFLAGS', 'CGO_LDFLAGS',
    'CFLAGS', 'CPPFLAGS', 'CXXFLAGS', 'FFLAGS', 'LDFLAGS', 'SDKROOT',
    'MACOSX_DEPLOYMENT_TARGET', 'DEVELOPER_DIR',
})


def _execution_environment(spec):
    """Snapshot once for both keying and Popen; never serialize these raw values."""
    environment = {key: value for key, value in os.environ.items()
                   if key in INHERITED_ENVIRONMENT}
    environment.setdefault('PATH', os.defpath)
    environment.update(spec.env)
    # Ignore ambient Go env files and Node/Python module injection. Fixed argv
    # still selects the repository's normal tools and already-installed modules.
    environment.update(GOENV='off', GOTOOLCHAIN='local', NODE_OPTIONS='', NODE_PATH='',
                       PYTHONPATH='', PYTHONHOME='')
    return environment


@dataclass(frozen=True)
class StageSpec:
    name: str
    argv: tuple[str, ...]
    cwd: Path
    inputs: tuple[Path, ...] = ()
    outputs: tuple[Path, ...] = ()
    timeout: float = 1800
    reusable: bool = True
    gui: bool = False
    env: dict[str, str] = field(default_factory=dict)
    owned_data: Path | None = None

    def __post_init__(self):
        if not re.fullmatch(r'[a-zA-Z0-9_-]+', self.name):
            raise ValueError('Stage name must be a simple identifier')
        if not self.argv or any(not isinstance(a, str) or '\0' in a for a in self.argv):
            raise ValueError('Stage requires fixed non-secret argv')
        if not math.isfinite(self.timeout) or self.timeout <= 0:
            raise ValueError('Stage timeout must be positive and finite')
        if set(self.env) - ENV_OVERRIDES or any(not isinstance(v, str) for v in self.env.values()):
            raise ValueError('Stage environment override is not allowlisted')
        object.__setattr__(self, 'argv', tuple(self.argv))
        object.__setattr__(self, 'cwd', Path(self.cwd).absolute())
        for key in ('inputs', 'outputs'):
            object.__setattr__(self, key, tuple(Path(p).absolute() for p in getattr(self, key)))
        object.__setattr__(self, 'env', MappingProxyType(dict(self.env)))
        if self.owned_data is not None:
            object.__setattr__(self, 'owned_data', Path(self.owned_data).absolute())


def _digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'),
                                     allow_nan=False).encode()).hexdigest()


def _stamp():
    return {'wall': datetime.now(timezone.utc).isoformat(), 'monotonic': time.monotonic()}


def mark_queued():
    """Call before executor.submit; pass this stamp to run(queued_at=...)."""
    return _stamp()


def _private_directory(path):
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or not path.is_dir():
        raise ValueError('Evidence directory must be a real directory')
    os.chmod(path, 0o700)


def _open_private(path, *, exclusive=True):
    flags = os.O_WRONLY | os.O_CREAT | getattr(os, 'O_NOFOLLOW', 0)
    flags |= os.O_EXCL if exclusive else os.O_TRUNC
    return os.fdopen(os.open(path, flags, 0o600), 'w')


def _save(path, row):
    # Unique files retain every failed attempt; replacements never follow links.
    temp = path.with_name('.' + path.name + '-' + uuid4().hex)
    try:
        with _open_private(temp) as stream:
            json.dump(row, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        temp.replace(path)
    finally:
        temp.unlink(missing_ok=True)


def _files(paths):
    result = {}
    for path in paths:
        path = Path(path)
        if path.is_symlink():
            raise ValueError('Stage evidence cannot be a symlink')
        if path.is_dir():
            # Include directory names and empty directories; add/delete/rename and
            # any asset byte change invalidate the whole selected output tree.
            tree = []
            def refuse_unreadable(error):
                raise error
            for directory, dirs, files in os.walk(path, followlinks=False,
                                                  onerror=refuse_unreadable):
                for name in sorted(dirs + files):
                    child = Path(directory) / name
                    if child.is_symlink():
                        raise ValueError('Stage evidence tree contains a symlink')
                    rel = child.relative_to(path).as_posix()
                    tree.append((rel, 'dir' if child.is_dir() else _files([child])[str(child)]))
            result[str(path)] = _digest(sorted(tree))
            continue
        fd = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
        with os.fdopen(fd, 'rb') as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                raise ValueError('Stage evidence must be a regular file')
            result[str(path)] = hashlib.file_digest(stream, 'sha256').hexdigest()
    return result


def interval_union_seconds(intervals):
    """Use monotonic seconds from one host/boot; overlapping work counts once."""
    merged = []
    for start, end in sorted(intervals):
        if not all(math.isfinite(v) for v in (start, end)) or end < start:
            raise ValueError('Invalid monotonic interval')
        if merged and start <= merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], end)
        else:
            merged.append([start, end])
    return sum(end - start for start, end in merged)


class StageRunner:
    """One bounded pipeline lock, with concurrent independent named stages.

    Identity must include source_sha, version, lockfiles, toolchain and config.
    Only its digest is stored: inherited environment and argv are never logged.
    Each run keeps independent stdout/stderr and a result, including failures.
    A GUI stage executes anew even when an identical previous result exists.
    """

    def __init__(self, evidence_dir, identity, stages, *, lock_timeout=30, cancel=None,
                 lock_path=None):
        required = {'source_sha', 'version', 'lockfiles', 'toolchain', 'config'}
        if not isinstance(identity, dict) or not required <= set(identity):
            raise ValueError('Complete release identity is required')
        if any(identity[key] is None for key in required):
            raise ValueError('Complete release identity is required')
        if not math.isfinite(lock_timeout) or lock_timeout < 0:
            raise ValueError('Lock timeout must be finite and nonnegative')
        self.identity_hash = _digest(identity)
        self.evidence = Path(evidence_dir).absolute()
        self.lock_path = Path(lock_path).absolute() if lock_path else self.evidence / 'pipeline.lock'
        self.stages = {stage.name: stage for stage in stages}
        if len(self.stages) != len(stages):
            raise ValueError('Stage names must be unique')
        self.lock_timeout = lock_timeout
        self.cancel = cancel or threading.Event()
        self._gates = {name: threading.Lock() for name in self.stages}
        self._results_lock = threading.Lock()
        self._results = []
        self._lock_stream = None
        self.pipeline_queue = None
        self._scope_lock = threading.Lock()
        self._cleanup_pending = False
        for spec in self.stages.values():
            if spec.owned_data is not None and (not spec.owned_data.is_relative_to(self.evidence)
                                                or spec.owned_data == self.evidence):
                raise ValueError('Owned process data must be private to this evidence directory')

    @property
    def results(self):
        with self._results_lock:
            return json.loads(json.dumps(self._results))

    def __enter__(self):
        if self._lock_stream is not None:
            raise RuntimeError('Pipeline already entered')
        _private_directory(self.evidence)
        queued = _stamp()
        row = {'queued': queued, 'status': 'waiting'}
        receipt = self.evidence / ('queue-' + uuid4().hex + '.json')
        flags = os.O_RDWR | os.O_CREAT | getattr(os, 'O_NOFOLLOW', 0)
        stream = os.fdopen(os.open(self.lock_path, flags, 0o600), 'r+')
        os.fchmod(stream.fileno(), 0o600)
        try:
            while True:
                if self.cancel.is_set():
                    row['status'] = 'cancelled'
                    raise InterruptedError('Pipeline lock wait cancelled')
                try:
                    fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    row['status'] = 'acquired'
                    self._check_cleanup_latches(stream)
                    break
                except BlockingIOError:
                    if time.monotonic() - queued['monotonic'] >= self.lock_timeout:
                        row['status'] = 'timed_out'
                        raise TimeoutError('Pipeline lock wait timed out')
                    self.cancel.wait(min(.05, self.lock_timeout))
        except BaseException:
            stream.close()
            if row['status'] == 'waiting':
                row['status'] = 'interrupted'
            raise
        finally:
            row['ended'] = _stamp()
            row['queue_seconds'] = row['ended']['monotonic'] - queued['monotonic']
            self.pipeline_queue = row
            try:
                _save(receipt, row)
            except BaseException:
                stream.close()
                raise
        self._lock_stream = stream
        return self

    def _check_cleanup_latches(self, stream):
        stream.seek(0)
        contents = [stream.read()]
        evidence_latch = self.evidence / 'cleanup-state.json'
        if evidence_latch.is_symlink():
            raise RuntimeError('Previous cleanup evidence is linked; inspect before reuse')
        if evidence_latch.exists():
            contents.append(evidence_latch.read_text())
        for saved in contents:
            if not saved:
                continue
            try:
                previous = json.loads(saved)
            except ValueError as error:
                raise RuntimeError('Previous cleanup receipt is unreadable; inspect before reuse') from error
            if not isinstance(previous, dict) or previous.get('cleanup_complete') is not True:
                raise RuntimeError('Previous process cleanup is pending; inspect before reuse')

    def _scope_latch(self, spec, row, complete):
        value = {'cleanup_complete': complete, 'stage': spec.name,
                 'owned_data': str(spec.owned_data), 'evidence': row['evidence'],
                 'updated': _stamp()}
        # The checkout lock retains refusal across another evidence directory;
        # the evidence latch retains refusal if a caller chooses a new lock file.
        save_lock(self._lock_stream, value)
        _save(self.evidence / 'cleanup-state.json', value)

    def __exit__(self, *_):
        stream, self._lock_stream = self._lock_stream, None
        if stream is not None:
            stream.close()

    def _key(self, spec, inputs, environment):
        return _digest({'identity': self.identity_hash, 'inputs': inputs,
                        'argv': spec.argv, 'cwd': str(spec.cwd),
                        'outputs': [str(p) for p in spec.outputs],
                        'timeout': spec.timeout, 'environment': environment,
                        'owned_data': str(spec.owned_data) if spec.owned_data else None,
                        'gui': spec.gui, 'reusable': spec.reusable})

    def _reuse(self, directory, key, spec):
        if spec.gui or not spec.reusable:
            return None
        try:
            latest = json.loads((directory / 'latest.json').read_text())
            if not re.fullmatch(r'[0-9a-f]{32}', latest['attempt']):
                return None
            path = directory / latest['attempt'] / 'result.json'
            if path.parent.parent != directory or path.is_symlink():
                return None
            if _files([path])[str(path)] != latest['sha256']:
                return None
            row = json.loads(path.read_text())
            if (row['status'] != 'passed' or row['identity'] != key
                    or row['exit_code'] != 0 or row['gui']
                    or row['stdout'] != str(path.parent / 'stdout.log')
                    or row['stderr'] != str(path.parent / 'stderr.log')):
                return None
            if _files([*spec.outputs, Path(row['stdout']), Path(row['stderr'])]) != row['files']:
                return None
            return row
        except (OSError, ValueError, KeyError, TypeError):
            return None

    @contextmanager
    def _stage_gate(self, name):
        start = time.monotonic()
        while not self._gates[name].acquire(timeout=.05):
            if self.cancel.is_set():
                raise InterruptedError('Stage queue cancelled')
            if time.monotonic() - start >= self.lock_timeout:
                raise TimeoutError('Stage queue timed out')
        try:
            yield
        finally:
            self._gates[name].release()

    def run(self, name, *, reason='execute', queued_at=None):
        if self._lock_stream is None:
            raise RuntimeError('Enter the pipeline before running stages')
        if reason not in {'execute', 'retry', 'rework'}:
            raise ValueError('Stage reason must be execute, retry or rework')
        spec = self.stages[name]
        queued = dict(queued_at) if queued_at is not None else _stamp()
        if (not isinstance(queued.get('wall'), str)
                or not isinstance(queued.get('monotonic'), (int, float))
                or not math.isfinite(queued['monotonic'])
                or queued['monotonic'] > time.monotonic()):
            raise ValueError('Queue stamp must come from this host monotonic clock')
        directory = self.evidence / name
        _private_directory(directory)
        attempt = directory / uuid4().hex
        _private_directory(attempt)
        row = {'schema': 1, 'stage': name, 'reason': reason, 'gui': spec.gui,
               'queued': queued, 'started': None, 'exit_code': None,
               'evidence': str(attempt / 'result.json'), 'status': 'failed',
               'stdout': str(attempt / 'stdout.log'), 'stderr': str(attempt / 'stderr.log'),
               'pipeline_queue': self.pipeline_queue, 'files': {}}
        for path in (row['stdout'], row['stderr']):
            with _open_private(Path(path)):
                pass
        try:
            with self._stage_gate(name):
                row['started'] = _stamp()
                row['queue_seconds'] = row['started']['monotonic'] - queued['monotonic']
                self._run_stage(spec, directory, row)
                self._finish(directory, attempt, row)
        except (InterruptedError, TimeoutError) as error:
            if row['started'] is not None:
                raise
            row.update(status='cancelled' if isinstance(error, InterruptedError) else 'timed_out',
                       failure='stage_queue', queue_seconds=time.monotonic() - queued['monotonic'])
            # A waiter cannot overwrite the running stage's latest receipt.
            self._finish(directory, attempt, row, latest=False)
        return row

    def _run_stage(self, spec, directory, row):
        if self._cleanup_pending:
            row['failure'] = 'previous_cleanup_pending'
            return
        try:
            inputs = _files(spec.inputs)
            environment = _execution_environment(spec)
            if spec.owned_data is not None:
                environment['ZWAI_NATIVE_CASE_ID'] = _digest(
                    {'evidence': str(self.evidence), 'stage': spec.name, 'data': str(spec.owned_data)})
            row['environment_sha256'] = _digest(environment)
            row['identity'] = self._key(spec, inputs, environment)
        except (OSError, ValueError):
            row['failure'] = 'input_evidence_unavailable'
            return
        reused = self._reuse(directory, row['identity'], spec)
        if reused and not self.cancel.is_set():
            row.update(status='reused', exit_code=0, reused_from=reused['evidence'],
                       stdout=reused['stdout'], stderr=reused['stderr'], files=reused['files'])
            return
        self._execute(spec, row, environment)
        if row['status'] == 'passed':
            try:
                if _files(spec.inputs) != inputs:
                    row.update(status='failed', failure='inputs_changed_during_execution')
                else:
                    row['files'] = _files([*spec.outputs, Path(row['stdout']), Path(row['stderr'])])
            except (OSError, ValueError):
                row.update(status='failed', failure='output_evidence_unavailable')

    def _finish(self, directory, attempt, row, *, latest=True):
        row['ended'] = _stamp()
        row['execute_seconds'] = (row['ended']['monotonic'] - row['started']['monotonic']
                                  if row['started'] and row['status'] != 'reused' else 0)
        try:
            _save(Path(row['evidence']), row)
            if latest and row['status'] != 'reused':
                digest = _files([Path(row['evidence'])])[row['evidence']]
                _save(directory / 'latest.json', {'attempt': attempt.name, 'sha256': digest})
        except (OSError, ValueError) as error:
            # Retain the command's original failure in memory even if its evidence
            # cannot be written. Such a result must never become a reusable pass.
            row['evidence_error_type'] = type(error).__name__
            if row['status'] in {'passed', 'reused'}:
                row.update(status='failed', failure='evidence_write')
        with self._results_lock:
            self._results.append(row)

    def _execute(self, spec, row, environment):
        if spec.owned_data is not None:
            with self._scope_lock:
                if self._cleanup_pending:
                    row['failure'] = 'previous_cleanup_pending'
                    return
                self._execute_owned(spec, row, environment)
            return
        process = None
        with _open_private(Path(row['stdout']), exclusive=False) as stdout, \
                _open_private(Path(row['stderr']), exclusive=False) as stderr:
            try:
                if self.cancel.is_set():
                    row['status'] = 'cancelled'
                    return
                process = subprocess.Popen(spec.argv, cwd=spec.cwd,
                                           env=environment,
                                           stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
                                           start_new_session=True)
                row['command_started'] = _stamp()
                deadline = time.monotonic() + spec.timeout
                while process.poll() is None:
                    if self.cancel.is_set():
                        row['status'] = 'cancelled'
                        break
                    if time.monotonic() >= deadline:
                        row['status'] = 'timed_out'
                        break
                    self.cancel.wait(min(.05, max(0, deadline - time.monotonic())))
                else:
                    row['status'] = 'passed' if process.returncode == 0 else 'failed'
                    if process.returncode:
                        row['failure'] = 'command_exit'
            except KeyboardInterrupt:
                row['status'] = 'cancelled'
                self.cancel.set()
            except OSError as error:
                row.update(status='failed', failure='command_launch', error_type=type(error).__name__)
            finally:
                if process is not None:
                    # Cancel the command's own group, including writers left after
                    # its leader exits; never signal by executable/process name.
                    row['cleanup'] = {'owned_pid': process.pid, 'group_signals': []}
                    try:
                        for sig in (signal.SIGTERM, signal.SIGKILL):
                            try:
                                os.killpg(process.pid, sig)
                                row['cleanup']['group_signals'].append(sig.name)
                            except ProcessLookupError:
                                break
                            if sig == signal.SIGTERM:
                                try:
                                    process.wait(timeout=.25)
                                except subprocess.TimeoutExpired:
                                    pass
                        process.wait(timeout=1)
                    except (OSError, subprocess.TimeoutExpired) as error:
                        row['cleanup']['error_type'] = type(error).__name__
                        if row['status'] == 'passed':
                            row.update(status='failed', failure='owned_process_cleanup')
                    row['exit_code'] = process.returncode
                    row['command_ended'] = _stamp()

    def _execute_owned(self, spec, row, environment):
        """Reuse the existing runner's ownership and direct-child-first cleanup.

        There is no native/guest launch here. The private marker/data also bind
        detached engine sessions; an unknown inventory leaves a refusal latch.
        """
        processes, process = [], None
        bundle = spec.owned_data / 'no-native-app'
        scope = ProcessScope(bundle, spec.owned_data, processes,
                             environment['ZWAI_NATIVE_CASE_ID'])
        inventory_ok, samples = True, 0
        self._cleanup_pending = True
        try:
            _private_directory(spec.owned_data)
            self._scope_latch(spec, row, False)
        except (OSError, ValueError) as error:
            row.update(failure='scope_evidence_write', error_type=type(error).__name__)
            return
        self._cleanup_pending = False
        with _open_private(Path(row['stdout']), exclusive=False) as stdout, \
                _open_private(Path(row['stderr']), exclusive=False) as stderr:
            try:
                if self.cancel.is_set():
                    row['status'] = 'cancelled'
                else:
                    process = subprocess.Popen(spec.argv, cwd=spec.cwd, env=environment,
                                               stdin=subprocess.DEVNULL, stdout=stdout,
                                               stderr=stderr, start_new_session=True)
                    processes.append(process)
                    row['command_started'] = _stamp()
                    deadline = time.monotonic() + spec.timeout
                    while True:
                        try:
                            scope.sample()
                            samples += 1
                        except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
                            inventory_ok = False
                            row.update(status='failed', failure='process_inventory',
                                       inventory_error_type=type(error).__name__)
                            if isinstance(error, subprocess.TimeoutExpired):
                                row['inventory_timeout_seconds'] = error.timeout
                            break
                        if process.poll() is not None:
                            row['status'] = 'passed' if process.returncode == 0 else 'failed'
                            if process.returncode:
                                row['failure'] = 'command_exit'
                            break
                        if self.cancel.is_set():
                            row['status'] = 'cancelled'
                            break
                        if time.monotonic() >= deadline:
                            row['status'] = 'timed_out'
                            break
                        self.cancel.wait(min(.05, max(0, deadline - time.monotonic())))
            except KeyboardInterrupt:
                row['status'] = 'cancelled'
                self.cancel.set()
            except OSError as error:
                row.update(status='failed', failure='command_launch', error_type=type(error).__name__)
            finally:
                cleanup = {'cleanup_complete': False, 'inventory_samples': samples,
                           'remaining_owned_pids': None, 'observed_identities': dict(scope.identities),
                           'data_removed': False}
                row['cleanup'] = cleanup
                try:
                    direct, direct_complete = stop_known_children(processes, grace=.25)
                    cleanup['direct_child_cleanup'] = direct
                    remaining = stop_owned(bundle, spec.owned_data, processes, grace=.25, scope=scope)
                    cleanup['remaining_owned_pids'] = remaining
                    cleanup['cleanup_complete'] = inventory_ok and direct_complete and not remaining
                except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
                    cleanup['error_type'] = type(error).__name__
                    if isinstance(error, subprocess.TimeoutExpired):
                        cleanup['timeout_seconds'] = error.timeout
                self._cleanup_pending = not cleanup['cleanup_complete']
                if cleanup['cleanup_complete']:
                    try:
                        self._scope_latch(spec, row, True)
                    except (OSError, ValueError) as error:
                        cleanup.update(cleanup_complete=False, evidence_error_type=type(error).__name__)
                        self._cleanup_pending = True
                if self._cleanup_pending and row['status'] == 'passed':
                    row.update(status='failed', failure='owned_process_cleanup')
                if process is not None:
                    row['exit_code'] = process.returncode
                    row['command_ended'] = _stamp()
