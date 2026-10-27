"""Small subprocess fixtures for release evidence, never a real release."""
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from release_stages import StageRunner, StageSpec, interval_union_seconds, mark_queued


class StageEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'input'
        self.source.write_text('source')
        self.output = self.root / 'output'
        self.identity = dict(source_sha='a' * 40, version='1.2.3',
                             lockfiles={'npm': 'digest'}, toolchain={'python': sys.version},
                             config={'platform': 'fixture'})

    def stage(self, code=None, **kwargs):
        code = code or "from pathlib import Path; Path('output').write_text('built')"
        return StageSpec('build', (sys.executable, '-c', code), self.root,
                         inputs=kwargs.pop('inputs', (self.source,)),
                         outputs=kwargs.pop('outputs', (self.output,)), **kwargs)

    def runner(self, stage=None, **kwargs):
        return StageRunner(self.root / 'evidence', self.identity,
                           [stage or self.stage()], **kwargs)

    def test_passed_stage_reuses_only_intact_exact_evidence(self):
        with self.runner() as runner:
            first = runner.run('build')
            second = runner.run('build')
        self.assertEqual(first['status'], 'passed')
        self.assertEqual(second['status'], 'reused')
        self.assertEqual(second['reused_from'], first['evidence'])
        self.assertEqual(second['stdout'], first['stdout'])
        self.assertGreaterEqual(first['execute_seconds'], 0)
        for path in (self.root / 'evidence').rglob('*'):
            if path.is_file():
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, path)

    def test_changed_input_output_or_log_requires_new_execution(self):
        with self.runner() as runner:
            first = runner.run('build')
            for label in ['input', 'output', 'stdout']:
                path = {'input': self.source, 'output': self.output,
                        'stdout': Path(first['stdout'])}[label]
                path.write_text('changed')
                result = runner.run('build', reason='rework')
                self.assertEqual(result['status'], 'passed')
                self.assertEqual(result['reason'], 'rework')
                first = result
            Path(first['stderr']).unlink()
            self.assertEqual(runner.run('build')['status'], 'passed')

    def test_failed_attempt_keeps_streams_and_retry_never_reuses_failure(self):
        stage = self.stage("import sys; print('before failure'); print('failure',file=sys.stderr); sys.exit(7)")
        with self.runner(stage) as runner:
            first = runner.run('build')
            second = runner.run('build', reason='retry')
        self.assertEqual(first['status'], 'failed')
        self.assertEqual(first['exit_code'], 7)
        self.assertEqual(second['status'], 'failed')
        self.assertNotEqual(first['evidence'], second['evidence'])
        self.assertIn('before failure', Path(first['stdout']).read_text())
        self.assertIn('failure', Path(first['stderr']).read_text())

    def test_identity_and_command_changes_do_not_reuse_prior_success(self):
        with self.runner() as runner:
            runner.run('build')
        for field in self.identity:
            identity = dict(self.identity, **{field: {'changed': field}})
            with StageRunner(self.root / 'evidence', identity, [self.stage()]) as runner:
                self.assertEqual(runner.run('build')['status'], 'passed')
        with self.runner(self.stage("from pathlib import Path; Path('output').write_text('new')")) as runner:
            self.assertEqual(runner.run('build')['status'], 'passed')

    def test_missing_output_cannot_accept_success_or_be_reused(self):
        with self.runner(self.stage('pass')) as runner:
            result = runner.run('build')
        self.assertEqual(result['status'], 'failed')
        self.assertEqual(result['failure'], 'output_evidence_unavailable')
        self.assertEqual(result['exit_code'], 0)

    def test_gui_and_nonreusable_stages_always_execute(self):
        for options in [dict(gui=True), dict(reusable=False)]:
            with self.runner(self.stage(**options)) as runner:
                self.assertEqual(runner.run('build')['status'], 'passed')
                self.assertEqual(runner.run('build')['status'], 'passed')

    def test_input_mutation_during_command_invalidates_result(self):
        stage = self.stage("from pathlib import Path; Path('input').write_text('changed'); Path('output').write_text('built')")
        with self.runner(stage) as runner:
            result = runner.run('build')
        self.assertEqual(result['status'], 'failed')
        self.assertEqual(result['failure'], 'inputs_changed_during_execution')

    def test_cancellation_stops_owned_descendant_before_releasing_pipeline(self):
        marker = self.root / 'late-write'
        code = ("import subprocess,sys,time; subprocess.Popen([sys.executable,'-c',"
                "\"import time; from pathlib import Path; time.sleep(.6); Path('late-write').write_text('bad')\"]); "
                "print('started',flush=True); time.sleep(10)")
        event = threading.Event()
        timer = threading.Timer(.15, event.set)
        timer.start()
        self.addCleanup(timer.cancel)
        with self.runner(self.stage(code), cancel=event) as runner:
            result = runner.run('build')
        self.assertEqual(result['status'], 'cancelled')
        time.sleep(.65)
        self.assertFalse(marker.exists())
        with self.runner() as runner:
            self.assertEqual(runner.run('build')['status'], 'passed')

    def test_timeout_preserves_failed_command_evidence(self):
        # Join the producer's first flushed write before this fixture admits
        # Popen. A busy interpreter may miss a short deadline before printing;
        # that says nothing about preserving bytes which were actually produced.
        ready = self.root / 'stdout-produced'
        launch = subprocess.Popen
        def after_first_write(*args, **kwargs):
            process = launch(*args, **kwargs)
            end = time.monotonic() + 5
            while not ready.exists():
                if process.poll() is not None or time.monotonic() >= end:
                    process.kill()
                    process.wait(timeout=2)
                    self.fail('fixture did not produce its first stream bytes')
                time.sleep(.01)
            return process
        code = "import time; from pathlib import Path; print('before timeout',flush=True); Path('stdout-produced').touch(); time.sleep(10)"
        with patch('release_stages.subprocess.Popen', side_effect=after_first_write), \
             self.runner(self.stage(code, timeout=.5)) as runner:
            result = runner.run('build')
        self.assertEqual(result['status'], 'timed_out')
        self.assertIn('before timeout', Path(result['stdout']).read_text())
        self.assertGreater(result['execute_seconds'], .49)

    def test_lock_competition_times_out_and_cancellation_releases_wait(self):
        with self.runner() as first:
            with self.assertRaises(TimeoutError):
                with self.runner(lock_timeout=.08):
                    self.fail('contending pipeline acquired')
            event = threading.Event()
            event.set()
            with self.assertRaises(InterruptedError):
                with self.runner(cancel=event):
                    self.fail('cancelled pipeline acquired')
            self.assertEqual(first.run('build')['status'], 'passed')
        queues = list((self.root / 'evidence').glob('queue-*.json'))
        states = {json.loads(p.read_text())['status'] for p in queues}
        self.assertTrue({'acquired', 'timed_out', 'cancelled'} <= states)

    def test_receipt_corruption_or_symlink_evidence_refuses_reuse(self):
        with self.runner() as runner:
            first = runner.run('build')
            Path(first['evidence']).write_text('{broken')
            second = runner.run('build')
            self.assertEqual(second['status'], 'passed')
            self.output.unlink()
            self.output.symlink_to(self.source)
            third = runner.run('build')
            self.assertEqual(third['status'], 'failed')

    def test_unregistered_stage_and_invalid_identity_are_refused(self):
        with self.runner() as runner:
            with self.assertRaises(KeyError):
                runner.run('arbitrary command')
            with self.assertRaises(ValueError):
                runner.run('build', reason='secret text')
        with self.assertRaises(ValueError):
            StageRunner(self.root / 'bad', {'version': '1'}, [self.stage()])

    def test_parallel_stage_durations_use_interval_union(self):
        self.assertEqual(interval_union_seconds([(1, 4), (2, 5), (7, 9)]), 6)
        self.assertEqual(interval_union_seconds([]), 0)
        with self.assertRaises(ValueError):
            interval_union_seconds([(3, 1)])

    def test_executor_queue_time_and_shared_checkout_lock_are_recorded(self):
        queued = mark_queued()
        time.sleep(.03)
        lock = self.root / 'checkout.lock'
        with self.runner(lock_path=lock) as runner:
            with self.assertRaises(TimeoutError):
                with StageRunner(self.root / 'other-evidence', self.identity,
                                 [self.stage()], lock_path=lock, lock_timeout=.03):
                    self.fail('different evidence bypassed the checkout lock')
            result = runner.run('build', queued_at=queued)
        self.assertGreaterEqual(result['queue_seconds'], .03)
        self.assertEqual(result['queued'], queued)

    def test_output_directory_reuse_checks_every_asset_and_refuses_symlinks(self):
        assets = self.root / 'assets'
        code = ("from pathlib import Path; p=Path('assets'); p.mkdir(exist_ok=True); "
                "(p/'index.html').write_text('index'); (p/'bundle.js').write_text('script')")
        stage = StageSpec('build', (sys.executable, '-c', code), self.root,
                          inputs=(self.source,), outputs=(assets,))
        with self.runner(stage) as runner:
            self.assertEqual(runner.run('build')['status'], 'passed')
            self.assertEqual(runner.run('build')['status'], 'reused')
            (assets / 'bundle.js').write_text('changed')
            self.assertEqual(runner.run('build')['status'], 'passed')
            (assets / 'extra').write_text('added')
            self.assertEqual(runner.run('build')['status'], 'passed')
            (assets / 'extra').unlink()
            self.assertEqual(runner.run('build')['status'], 'passed')
            (assets / 'link').symlink_to(self.source)
            self.assertEqual(runner.run('build')['status'], 'failed')

    def test_allowlisted_env_is_effective_but_values_never_enter_receipts(self):
        value = 'fixture-private-path'
        stage = self.stage("import os; from pathlib import Path; Path('output').write_text(os.environ['ZWAI_E2E_DATA_DIR'])",
                           env={'ZWAI_E2E_DATA_DIR': value})
        with self.runner(stage) as runner:
            result = runner.run('build')
        self.assertEqual(self.output.read_text(), value)
        self.assertNotIn(value, Path(result['evidence']).read_text())
        with self.assertRaises(ValueError):
            self.stage(env={'SECRET_TOKEN': 'never-allowed'})

    def test_concurrent_different_stages_overlap_but_same_stage_serializes(self):
        specs = []
        for name, other in [('one', 'two'), ('two', 'one')]:
            code = ("import time; from pathlib import Path; "
                    f"Path('{name}-ready').touch(); deadline=time.monotonic()+2\n"
                    f"while not Path('{other}-ready').exists() and time.monotonic()<deadline: time.sleep(.01)\n"
                    "time.sleep(.08)")
            specs.append(StageSpec(name, (sys.executable, '-c', code), self.root,
                                   reusable=False))
        with StageRunner(self.root / 'evidence', self.identity, specs) as runner:
            with ThreadPoolExecutor(max_workers=3) as pool:
                futures = [pool.submit(runner.run, name) for name in ('one', 'two', 'one')]
                results = [future.result() for future in futures]
            self.assertEqual(len(runner.results), 3)
        one, two, again = results
        earlier, later = sorted([one, again], key=lambda row: row['started']['monotonic'])
        self.assertLess(two['started']['monotonic'], earlier['ended']['monotonic'])
        self.assertLess(earlier['started']['monotonic'], two['ended']['monotonic'])
        self.assertGreaterEqual(later['started']['monotonic'], earlier['ended']['monotonic'])
        self.assertTrue(all(row['status'] == 'passed' for row in results))

    def test_waiting_stage_has_its_own_bounded_failure_receipt(self):
        spec = self.stage("import time; time.sleep(.25)", outputs=())
        with self.runner(spec, lock_timeout=.03) as runner:
            with ThreadPoolExecutor(max_workers=2) as pool:
                first = pool.submit(runner.run, 'build')
                time.sleep(.02)
                result = pool.submit(runner.run, 'build').result()
                first.result()
        self.assertEqual(result['status'], 'timed_out')
        self.assertEqual(result['failure'], 'stage_queue')
        self.assertEqual(result['execute_seconds'], 0)
        self.assertIsNone(result['started'])
        self.assertTrue(Path(result['evidence']).is_file())

    def test_missing_input_or_executable_records_failure_without_leaking_argv(self):
        self.source.unlink()
        with self.runner() as runner:
            result = runner.run('build')
        self.assertEqual(result['failure'], 'input_evidence_unavailable')
        spec = StageSpec('missing', (str(self.root / 'absent-secret-command'),), self.root)
        with self.runner(spec) as runner:
            result = runner.run('missing')
        self.assertEqual(result['failure'], 'command_launch')
        self.assertNotIn('absent-secret-command', Path(result['evidence']).read_text())

    def test_evidence_io_does_not_replace_original_command_failure(self):
        from release_stages import _save
        def fail_result(path, row):
            if path.name == 'result.json':
                raise PermissionError('fixture')
            return _save(path, row)
        with self.runner(self.stage('raise SystemExit(3)')) as runner:
            with patch('release_stages._save', side_effect=fail_result):
                result = runner.run('build')
            self.assertEqual(runner.results[0]['exit_code'], 3)
        self.assertEqual(result['status'], 'failed')
        self.assertEqual(result['failure'], 'command_exit')
        self.assertEqual(result['evidence_error_type'], 'PermissionError')

    def test_unreadable_directory_tree_is_never_a_partial_pass(self):
        from release_stages import _files
        directory = self.root / 'assets'
        directory.mkdir()
        def refuse(*args, **kwargs):
            kwargs['onerror'](PermissionError('fixture unreadable child'))
            return iter(())
        with patch('release_stages.os.walk', side_effect=refuse):
            with self.assertRaises(PermissionError):
                _files([directory])

    def test_real_process_lock_owner_blocks_then_releases_another_pipeline(self):
        lock = self.root / 'checkout.lock'
        holder = subprocess.Popen([sys.executable, '-c',
            "import fcntl,sys,time; f=open(sys.argv[1],'a'); "
            "fcntl.flock(f,fcntl.LOCK_EX); print('ready',flush=True); time.sleep(10)", str(lock)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            self.assertEqual(holder.stdout.readline().strip(), 'ready')
            with self.assertRaises(TimeoutError):
                with self.runner(lock_path=lock, lock_timeout=.03):
                    self.fail('another process still owns the pipeline')
        finally:
            holder.terminate()
            holder.communicate(timeout=2)
        with self.runner(lock_path=lock) as runner:
            self.assertEqual(runner.run('build')['status'], 'passed')

    def test_invalid_specs_and_runner_lifecycle_are_refused_before_commands(self):
        for kwargs in [dict(name='../escape'), dict(argv=()), dict(timeout=0),
                       dict(timeout=float('inf')), dict(env={'CI': 1})]:
            values = dict(name='one', argv=(sys.executable, '-c', 'pass'), cwd=self.root)
            values.update(kwargs)
            with self.assertRaises(ValueError):
                StageSpec(**values)
        for kwargs in [dict(lock_timeout=-1), dict(lock_timeout=float('nan'))]:
            with self.assertRaises(ValueError):
                self.runner(**kwargs)
        with self.assertRaises(ValueError):
            StageRunner(self.root / 'bad', self.identity, [self.stage(), self.stage()])
        runner = self.runner()
        with self.assertRaises(RuntimeError):
            runner.run('build')
        with runner:
            with self.assertRaises(RuntimeError):
                runner.__enter__()
            with self.assertRaises(ValueError):
                runner.run('build', queued_at={'wall': 'now', 'monotonic': float('nan')})

    def test_actual_build_environment_changes_invalidate_a_cached_pass(self):
        code = ("import json,os; from pathlib import Path; "
                "Path('output').write_text(json.dumps({key:os.environ.get(key) "
                "for key in ['GOARCH','CGO_ENABLED','CC','CGO_CFLAGS','NODE_OPTIONS','PRIVATE_TEST_TOKEN']}))")
        secret = 'fixture-token-never-forwarded'
        with self.runner(self.stage(code)) as runner:
            with patch.dict(os.environ, GOARCH='fixture-arch-one', NODE_OPTIONS='injected-one',
                            PRIVATE_TEST_TOKEN=secret):
                first = runner.run('build')
                self.assertEqual(first['status'], 'passed')
            for key in ['GOARCH', 'CGO_ENABLED', 'CC', 'CGO_CFLAGS']:
                with patch.dict(os.environ, {key: 'changed-' + key, 'NODE_OPTIONS': 'injected-two'}):
                    result = runner.run('build')
                    self.assertEqual(result['status'], 'passed', key)
                    self.assertEqual(json.loads(self.output.read_text())[key], 'changed-' + key)
                    self.assertNotIn('changed-' + key, Path(result['evidence']).read_text())
        data = json.loads(self.output.read_text())
        self.assertEqual(data['NODE_OPTIONS'], '')
        self.assertIsNone(data['PRIVATE_TEST_TOKEN'])

    def test_node_option_injection_is_cleared_before_the_actual_node_subprocess(self):
        import shutil
        node = shutil.which('node')
        if not node:
            self.skipTest('node unavailable')
        injection = self.root / 'injection.js'
        injection.write_text("require('fs').writeFileSync('injected', 'bad')")
        code = "require('fs').writeFileSync('output', process.env.NODE_OPTIONS + '|' + process.env.NODE_PATH)"
        stage = StageSpec('build', (node, '-e', code), self.root, outputs=(self.output,))
        with self.runner(stage) as runner:
            with patch.dict(os.environ, NODE_OPTIONS='--require=' + str(injection), NODE_PATH='fixture-one'):
                first = runner.run('build')
            with patch.dict(os.environ, NODE_OPTIONS='--invalid-injection', NODE_PATH='fixture-two'):
                second = runner.run('build')
        self.assertEqual(first['status'], 'passed')
        self.assertEqual(second['status'], 'reused')
        self.assertEqual(self.output.read_text(), '|')
        self.assertFalse((self.root / 'injected').exists())

    def test_ambient_auto_toolchain_cannot_replace_the_bound_local_toolchain(self):
        stage = self.stage("import os; from pathlib import Path; Path('output').write_text(os.environ['GOTOOLCHAIN'])")
        with patch.dict(os.environ, GOTOOLCHAIN='auto'):
            with self.runner(stage) as runner:
                result = runner.run('build')
        self.assertEqual(result['status'], 'passed')
        self.assertEqual(self.output.read_text(), 'local')

    def test_detached_owned_writer_is_joined_before_stage_acceptance(self):
        data = self.root / 'evidence' / 'private-data'
        data.mkdir(parents=True)
        child = ("import time; from pathlib import Path; deadline=time.monotonic()+10\n"
                 "while not Path('release-write').exists() and time.monotonic()<deadline: time.sleep(.01)\n"
                 "Path('late-write').write_text('bad'); time.sleep(10)")
        code = ("import subprocess,sys,time; from pathlib import Path; "
                f"p=subprocess.Popen([sys.executable,'-c',{child!r},'--data-dir',{str(data)!r}],start_new_session=True); "
                "Path('owned-child-pid').write_text(str(p.pid)); "
                "Path('output').write_text('done'); time.sleep(.12)")
        stage = self.stage(code, owned_data=data)
        try:
            with self.runner(stage) as runner:
                result = runner.run('build')
            self.assertEqual(result['status'], 'passed', result)
            self.assertTrue(result['cleanup']['cleanup_complete'])
            self.assertEqual(result['cleanup']['remaining_owned_pids'], [])
            self.assertGreater(result['cleanup']['inventory_samples'], 0)
            (self.root / 'release-write').touch()
            time.sleep(.15)
            self.assertFalse((self.root / 'late-write').exists())
        finally:
            # The same private data/marker ownership rule also bounds fixture
            # teardown after an assertion failure; never signal a stale file PID.
            from native_macos_case import stop_owned
            stop_owned(data / 'no-native-app', data, [], grace=.25)

    def test_inventory_failure_joins_direct_child_and_keeps_durable_refusal_latch(self):
        data = self.root / 'evidence' / 'private-data'
        stage = self.stage("import time; print('started',flush=True); time.sleep(10)", owned_data=data)
        ordinary = StageSpec('ordinary', (sys.executable, '-c', 'raise SystemExit(99)'), self.root)
        lock = self.root / 'checkout.lock'
        with StageRunner(self.root / 'evidence', self.identity, [stage, ordinary], lock_path=lock) as runner:
            with patch('native_macos_case.ProcessScope.sample',
                       side_effect=subprocess.TimeoutExpired(['/bin/ps'], .01)):
                result = runner.run('build')
            self.assertEqual(result['status'], 'failed')
            self.assertEqual(result['failure'], 'process_inventory')
            self.assertFalse(result['cleanup']['cleanup_complete'])
            self.assertIsNotNone(result['exit_code'])
            blocked = runner.run('build')
            self.assertEqual(blocked['failure'], 'previous_cleanup_pending')
            with patch('release_stages.subprocess.Popen') as launch:
                self.assertEqual(runner.run('ordinary')['failure'], 'previous_cleanup_pending')
            launch.assert_not_called()
        self.assertTrue(data.exists())
        saved = json.loads(lock.read_text())
        self.assertFalse(saved['cleanup_complete'])
        self.assertEqual(saved['evidence'], result['evidence'])
        for evidence, lock_path in [(self.root / 'different-evidence', lock),
                                    (self.root / 'evidence', self.root / 'different.lock')]:
            next_stage = self.stage(owned_data=evidence / 'private-data')
            with self.assertRaisesRegex(RuntimeError, 'cleanup'):
                with StageRunner(evidence, self.identity, [next_stage], lock_path=lock_path):
                    self.fail('pending cleanup was bypassed')

    def test_normal_pending_latch_write_does_not_refuse_a_parallel_stage(self):
        import native_macos_case
        owned = StageSpec('owned', (sys.executable, '-c', "print('owned')"), self.root,
                          owned_data=self.root / 'evidence/private-data')
        ordinary = self.stage()
        entered, release = threading.Event(), threading.Event()
        # The actual lifecycle, Popen, durable latch and cache still run. Only
        # this fixture's OS inventory leaf is empty; no detached child is made.
        def scope_factory(bundle, data, processes, case_id, *, inventory):
            return native_macos_case.ProcessScope(bundle, data, processes, case_id,
                                                  inventory=lambda *_args, **_kwargs: '')
        with StageRunner(self.root / 'evidence', self.identity, [owned, ordinary]) as runner:
            original = runner._scope_latch
            def pause_pending(spec, row, complete):
                if not complete:
                    entered.set()
                    if not release.wait(3):
                        raise RuntimeError('fixture pending barrier expired')
                return original(spec, row, complete)
            with patch('release_stages.ProcessScope', side_effect=scope_factory), \
                 patch.object(runner, '_scope_latch', side_effect=pause_pending), \
                 ThreadPoolExecutor(max_workers=1) as executor:
                running = executor.submit(runner.run, 'owned')
                self.assertTrue(entered.wait(3), 'owned stage never reached pending publication')
                try:
                    result = runner.run('build')
                finally:
                    release.set()
                owned_result = running.result(timeout=5)
            self.assertEqual(result['status'], 'passed', result)
            self.assertEqual(owned_result['status'], 'passed', owned_result)
            self.assertTrue(owned_result['cleanup']['cleanup_complete'])

    def test_pending_latch_write_failure_refuses_subsequent_ordinary_stage(self):
        owned = StageSpec('owned', (sys.executable, '-c', "print('must not launch')"), self.root,
                          owned_data=self.root / 'evidence/private-data')
        with StageRunner(self.root / 'evidence', self.identity, [owned, self.stage()]) as runner:
            with patch.object(runner, '_scope_latch', side_effect=OSError('fixture durable write failed')), \
                 patch('release_stages.subprocess.Popen') as launch:
                failed = runner.run('owned')
                ordinary = runner.run('build')
            self.assertEqual(failed['failure'], 'scope_evidence_write')
            self.assertEqual(ordinary['failure'], 'previous_cleanup_pending')
            launch.assert_not_called()

    def test_cancelled_pending_publication_retains_the_original_cause_and_refusal(self):
        from release_verify import Cancelled
        for index, cause in enumerate((KeyboardInterrupt('original fixture interruption'), Cancelled(15))):
            evidence = self.root / ('cancel-evidence-' + str(index))
            owned = StageSpec('owned', (sys.executable, '-c', 'pass'), self.root,
                              owned_data=evidence / 'private-data')
            with StageRunner(evidence, self.identity, [owned, self.stage()]) as runner:
                original = runner._scope_latch
                def cancelled(spec, row, complete):
                    original(spec, row, complete)
                    if not complete:
                        raise cause
                with patch.object(runner, '_scope_latch', side_effect=cancelled), \
                     patch('release_stages.subprocess.Popen') as launch:
                    with self.assertRaises(KeyboardInterrupt) as caught:
                        runner.run('owned')
                    self.assertIs(caught.exception, cause)
                    self.assertEqual(runner.run('build')['failure'], 'previous_cleanup_pending')
                launch.assert_not_called()
            self.assertFalse(json.loads((evidence / 'cleanup-state.json').read_text())['cleanup_complete'])
            with self.assertRaisesRegex(RuntimeError, 'cleanup'):
                with StageRunner(evidence, self.identity, [owned]):
                    self.fail('cancelled setup was admitted as cleaned')

    def test_cleanup_failure_during_hashing_cannot_reuse_an_earlier_pass(self):
        import release_stages
        owned = StageSpec('owned', (sys.executable, '-c', 'pass'), self.root,
                          owned_data=self.root / 'evidence/private-data')
        entered, release = threading.Event(), threading.Event()
        with StageRunner(self.root / 'evidence', self.identity, [owned, self.stage()]) as runner:
            self.assertEqual(runner.run('build')['status'], 'passed')
            original = release_stages._files
            def pause_hash(paths):
                if tuple(paths) == (self.source,):
                    entered.set()
                    if not release.wait(3):
                        raise RuntimeError('fixture hashing barrier expired')
                return original(paths)
            with patch('release_stages._files', side_effect=pause_hash), \
                 ThreadPoolExecutor(max_workers=1) as executor:
                waiting = executor.submit(runner.run, 'build')
                self.assertTrue(entered.wait(3), 'ordinary stage never reached input hashing')
                try:
                    with patch.object(runner, '_scope_latch', side_effect=OSError('fixture setup failure')):
                        self.assertEqual(runner.run('owned')['failure'], 'scope_evidence_write')
                finally:
                    release.set()
                result = waiting.result(timeout=5)
            self.assertEqual(result['failure'], 'previous_cleanup_pending')
            self.assertEqual(result['status'], 'failed')
            self.assertNotIn('command_started', result)

    def test_signal_cancellation_marks_the_waiting_pipeline_cancelled(self):
        # Exercise the CLI's real signal handler while StageRunner is blocked in
        # its actual flock/Event wait; a programmatic Event alone misses this path.
        import fcntl
        import signal
        lock = self.root / 'checkout.lock'
        ready = self.root / 'waiting'
        evidence = self.root / 'signal-evidence'
        code = '''import json,sys,threading
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from release_stages import StageRunner
from release_verify import cancellation, Cancelled
class QueueEvent(threading.Event):
    def wait(self, timeout=None):
        Path(sys.argv[4]).touch()
        return super().wait(timeout)
event = QueueEvent()
try:
    with cancellation(event):
        with StageRunner(Path(sys.argv[2]), json.loads(sys.argv[5]), [],
                         lock_path=Path(sys.argv[3]), lock_timeout=5, cancel=event):
            raise AssertionError('held checkout lock was acquired')
except Cancelled as error:
    raise SystemExit(128 + error.signum)
'''
        with lock.open('a') as holder:
            fcntl.flock(holder, fcntl.LOCK_EX)
            child = subprocess.Popen([sys.executable, '-B', '-c', code,
                str(Path(__file__).parent), str(evidence), str(lock), str(ready),
                json.dumps(self.identity)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                deadline = time.monotonic() + 5
                while not ready.exists() and child.poll() is None and time.monotonic() < deadline:
                    time.sleep(.01)
                self.assertTrue(ready.exists(), 'child never reached the real lock wait')
                child.send_signal(signal.SIGTERM)
                stdout, stderr = child.communicate(timeout=5)
                self.assertEqual(child.returncode, 143, (stdout, stderr))
                queues = list(evidence.glob('queue-*.json'))
                self.assertEqual(len(queues), 1)
                receipt = json.loads(queues[0].read_text())
                self.assertEqual(receipt['status'], 'cancelled', receipt)
                self.assertGreaterEqual(receipt['queue_seconds'], 0)
                self.assertEqual(list(evidence.glob('*/*/result.json')), [])
            finally:
                if child.poll() is None:
                    child.terminate()
                child.communicate(timeout=5)
        with StageRunner(evidence, self.identity, [], lock_path=lock):
            pass

    def test_delayed_detached_shutdown_keeps_native_grace_and_terminal_inventory(self):
        import native_macos_case
        data = self.root / 'evidence' / 'private-data'
        child = '''import signal,sys,time
from pathlib import Path
def stop(*_):
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    time.sleep(.6)
    Path('graceful-cleanup').write_text('finished')
    raise SystemExit(0)
signal.signal(signal.SIGTERM, stop)
Path('delayed-ready').touch()
time.sleep(10)
'''
        code = ("import subprocess,sys,time; from pathlib import Path; "
                f"subprocess.Popen([sys.executable,'-c',{child!r},'--data-dir',{str(data)!r}],start_new_session=True); "
                "deadline=time.monotonic()+3\n"
                "while not Path('delayed-ready').exists() and time.monotonic()<deadline: time.sleep(.01)\n"
                "Path('output').write_text('done'); time.sleep(.8)")
        try:
            with self.runner(self.stage(code, owned_data=data, timeout=5)) as runner:
                with patch('release_stages.stop_owned', wraps=native_macos_case.stop_owned) as cleanup:
                    result = runner.run('build')
            self.assertEqual(result['status'], 'passed', result)
            self.assertEqual((self.root / 'graceful-cleanup').read_text(), 'finished')
            self.assertNotIn('grace', cleanup.call_args.kwargs)
            phases = [call['phase'] for call in result['inventory_calls']]
            self.assertEqual(phases.count('first'), 1)
            self.assertEqual(phases.count('terminal'), 1)
            self.assertNotIn('periodic', phases)
            for call in result['inventory_calls']:
                self.assertEqual(call['timeout_seconds'], 5)
                self.assertEqual(Path(call['evidence']).is_file(), True)
        finally:
            native_macos_case.stop_owned(data / 'no-native-app', data, [])

    def test_repeated_actual_inventory_timeouts_keep_raw_bytes_before_cleanup(self):
        import native_macos_case
        data = self.root / 'evidence' / 'private-data'
        native_scope = native_macos_case.ProcessScope
        native_direct_cleanup = __import__('release_stages').stop_known_children
        sequence = []
        payload = b'\x00\xff' + bytes(range(256)) * 8
        def scope_factory(bundle, owned_data, processes, case_id, *, inventory):
            def timed_inventory(command, **kwargs):
                self.assertEqual(kwargs['timeout'], 5)
                tag = str(len(sequence))
                fixture = [sys.executable, '-B', '-c',
                    "import os,sys,time; p=bytes.fromhex(sys.argv[2]); "
                    "os.write(1,p+sys.argv[1].encode()); os.write(2,p[::-1]+sys.argv[1].encode()); time.sleep(10)",
                    tag, payload.hex()]
                sequence.append(fixture)
                return inventory(fixture, timeout=1, text=True)
            return native_scope(bundle, owned_data, processes, case_id, inventory=timed_inventory)
        def joined_after_evidence(processes):
            files = list((self.root / 'evidence').glob('build/*/inventory-0001.json'))
            self.assertEqual(len(files), 1, 'raw failure must be durable before cleanup')
            receipt = json.loads(files[0].read_text())
            self.assertEqual(Path(receipt['stdout']).read_bytes(), payload + b'0')
            self.assertEqual(Path(receipt['stderr']).read_bytes(), payload[::-1] + b'0')
            return native_direct_cleanup(processes)
        with self.runner(self.stage('import time; time.sleep(10)', owned_data=data)) as runner:
            with patch('release_stages.ProcessScope', side_effect=scope_factory), \
                    patch('release_stages.stop_known_children', side_effect=joined_after_evidence):
                result = runner.run('build')
        self.assertEqual(result['failure'], 'process_inventory')
        self.assertEqual(result['inventory_error_type'], 'TimeoutExpired')
        self.assertEqual(result['inventory_timeout_seconds'], 1)
        self.assertFalse(result['cleanup']['cleanup_complete'])
        self.assertIsNotNone(result['exit_code'])
        self.assertGreaterEqual(len(sequence), 3)
        first = result['inventory_failures'][0]
        self.assertEqual(first['command'], sequence[0])
        self.assertLessEqual(json.loads(Path(first['raw_evidence']).read_text())['ended']['monotonic'],
                             result['cleanup']['started']['monotonic'])
        for index, call in enumerate(result['inventory_calls']):
            self.assertEqual(call['original_command'], sequence[index])
            self.assertEqual(call['original_timeout_seconds'], 1)
            self.assertEqual(Path(call['stdout']).read_bytes(), payload + str(index).encode())
            self.assertEqual(Path(call['stderr']).read_bytes(), payload[::-1] + str(index).encode())
        self.assertEqual(json.loads((self.root / 'evidence' / 'cleanup-state.json').read_text())['cleanup_complete'], False)

    def test_inventory_evidence_error_preserves_original_timeout_and_unavailable_streams(self):
        from release_stages import _InventoryRecorder
        attempt = self.root / 'inventory'
        attempt.mkdir()
        recorder = _InventoryRecorder(attempt)
        command = [sys.executable, '-B', '-c',
                   "import os,time; os.write(1,b'partial\\x00\\xff'); os.write(2,b'error\\x00'); time.sleep(10)"]
        with patch('release_stages._save', side_effect=PermissionError('fixture evidence I/O')):
            with self.assertRaises(subprocess.TimeoutExpired) as raised:
                recorder(command, timeout=1, text=True)
        self.assertEqual(raised.exception.cmd, command)
        self.assertEqual(raised.exception.output, b'partial\x00\xff')
        self.assertEqual(raised.exception.stderr, b'error\x00')
        self.assertEqual(recorder.calls[0]['evidence_error_type'], 'PermissionError')
        with self.assertRaises(FileNotFoundError):
            recorder([str(self.root / 'missing-inventory-program')], timeout=5, text=True)
        unavailable = recorder.calls[-1]
        self.assertFalse(unavailable['stdout_present'])
        self.assertFalse(unavailable['stderr_present'])
        self.assertIsNone(unavailable['stdout'])
        self.assertIsNone(unavailable['stderr'])

    def test_owned_inventory_does_not_renew_the_scenario_deadline(self):
        data = self.root / 'evidence' / 'private-data'
        with self.runner(self.stage(owned_data=data, timeout=.01)) as runner:
            result = runner.run('build')
        self.assertEqual(result['status'], 'timed_out', result)
        self.assertTrue(result['cleanup']['cleanup_complete'])
        self.assertEqual([call['phase'] for call in result['inventory_calls']].count('terminal'), 1)


if __name__ == '__main__':
    unittest.main()
