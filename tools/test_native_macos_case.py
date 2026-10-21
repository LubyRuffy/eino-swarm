"""Native cases own only their copied bundle, data and scenario processes."""
from contextlib import redirect_stderr
import ctypes
import errno
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import runpy
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name("native_macos_case.py")
spec = importlib.util.spec_from_file_location("native_macos_case", SCRIPT)
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class NativeCaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="native-case-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "Original.app"
        (self.app / "Contents/MacOS").mkdir(parents=True)
        with (self.app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump({"CFBundleExecutable": "fixture"}, stream)
        self.executable = self.app / "Contents/MacOS/fixture"
        self.executable.write_text("#!" + sys.executable + "\nimport time\ntime.sleep(120)\n")
        self.executable.chmod(0o755)
        self.lock = self.root / "guest.lock"
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(runner, "LOCK_PATH", self.lock).start()
        mock.patch.object(runner, "require_guest", return_value=None).start()

    def tearDown(self):
        result = self._outcome.result
        if any(test is self or getattr(test, "test_case", None) is self
               for test, _ in result.failures + result.errors):
            durable = Path(tempfile.mkdtemp(prefix="native-case-failure-evidence-"))
            for evidence in self.root.glob("*evidence*"):
                if evidence.is_dir():
                    shutil.copytree(evidence, durable / evidence.name)
            print("native case failure evidence: " + str(durable), file=sys.stderr)

    def run_case(self, script="", timeout=5):
        evidence = self.root / ("evidence-" + str(time.monotonic_ns()))
        code = runner.run_case(self.app, evidence, timeout, [sys.executable, "-c", script])
        receipt = json.loads((evidence / "cleanup.json").read_text())
        if "case_root" in receipt:
            self.addCleanup(lambda path=receipt["case_root"]: shutil.rmtree(path, ignore_errors=True))
        return code, evidence, receipt

    def test_success_has_unique_paths_preserved_logs_and_no_seed(self):
        self.executable.write_text("#!" + sys.executable + "\nimport os,time\nfrom pathlib import Path\n"
                                   "Path(os.environ['ZWAI_HOME'],'started').write_text('yes')\n"
                                   "print('app started',flush=True)\ntime.sleep(120)\n")
        scenario = """import os,time,json
from pathlib import Path
p=Path(os.environ['ZWAI_HOME'])
for _ in range(100):
 if (p/'started').exists(): break
 time.sleep(.01)
assert (p/'started').exists()
assert not any(k.startswith('OPENAI_') for k in os.environ)
assert Path(os.environ['ZWAI_NATIVE_WORKSPACE']).is_dir()
assert Path(os.environ['ZWAI_NATIVE_APP']).is_dir()
Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'paths.json').write_text(json.dumps(dict(data=str(p),app=os.environ['ZWAI_NATIVE_APP'])))
print('scenario output')
"""
        with mock.patch.dict(os.environ, {"OPENAI_API_KEY": "must-not-inherit"}):
            code, evidence, receipt = self.run_case(scenario)
        self.assertEqual(code, 0)
        self.assertTrue(receipt["virtualized"])
        self.assertTrue(receipt["cleanup_complete"])
        self.assertTrue(receipt["data_removed"])
        self.assertEqual(receipt["remaining_owned_pids"], [])
        paths = json.loads((evidence / "paths.json").read_text())
        self.assertFalse(Path(paths["data"]).exists())
        self.assertFalse(Path(paths["app"]).exists())
        self.assertIn("scenario output", (evidence / "scenario.stdout.log").read_text())
        self.assertIn("app started", (evidence / "app.stdout.log").read_text())
        self.assertTrue(self.executable.exists())

    def test_failure_timeout_and_interruption_cleanup(self):
        for script, timeout, expected in (("raise SystemExit(7)", 5, 7),
                                          ("import time; time.sleep(120)", .1, 124)):
            with self.subTest(expected=expected):
                code, _, receipt = self.run_case(script, timeout)
                self.assertEqual(code, expected)
                self.assertTrue(receipt["cleanup_complete"])
                self.assertTrue(receipt["data_removed"])
        with mock.patch.object(runner, "wait_scenario", side_effect=KeyboardInterrupt):
            code, _, receipt = self.run_case()
        self.assertEqual(code, 130)
        self.assertTrue(receipt["cleanup_complete"])

    def test_checkout_cache_uses_case_home_and_is_removed_without_touching_user_cache(self):
        user_home = self.root / "normal-home"
        user_cache = user_home / "Library/Caches/zwai/zwai.app"
        user_cache.mkdir(parents=True)
        sentinel = user_cache / "keep"
        sentinel.write_text("user bundle")
        self.executable.write_text("#!" + sys.executable + "\nfrom pathlib import Path\nimport time\n"
            "cache=Path.home()/'Library/Caches/zwai/zwai.app'\ncache.mkdir(parents=True,exist_ok=True)\n"
            "(cache/'case-bundle').write_text('temporary app')\ntime.sleep(120)\n")
        scenario = """import json,os,time
from pathlib import Path
cache=Path.home()/'Library/Caches/zwai/zwai.app'
for _ in range(100):
 if (cache/'case-bundle').exists(): break
 time.sleep(.01)
assert (cache/'case-bundle').read_text() == 'temporary app'
Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'home.json').write_text(json.dumps(dict(home=str(Path.home()),cache=str(cache))))
"""
        with mock.patch.dict(os.environ, {"HOME": str(user_home)}):
            code, evidence, receipt = self.run_case(scenario)
            self.assertEqual(os.environ["HOME"], str(user_home))
        self.assertEqual(code, 0)
        paths = json.loads((evidence / "home.json").read_text())
        self.assertNotEqual(Path(paths["home"]), user_home)
        self.assertTrue(Path(paths["home"]).is_relative_to(Path(receipt["case_root"])))
        self.assertFalse(Path(paths["home"]).exists())
        self.assertFalse(Path(paths["cache"]).exists())
        self.assertEqual(list(user_cache.iterdir()), [sentinel])
        self.assertEqual(sentinel.read_text(), "user bundle")

    def test_completed_scenario_cannot_mask_failed_cleanup_receipt(self):
        real_save = runner.save_lock
        writes = []
        def fail_final_receipt(stream, row):
            writes.append(row["cleanup_complete"])
            if row["cleanup_complete"]:
                raise OSError("cleanup receipt could not be persisted")
            return real_save(stream, row)
        with mock.patch.object(runner, "save_lock", side_effect=fail_final_receipt):
            code, _, receipt = self.run_case()
        self.assertEqual(writes, [False, True])
        self.assertNotEqual(code, 0)
        self.assertEqual(receipt["scenario_exit"], 0)
        self.assertIn("could not be persisted", receipt["failure"])
        self.assertFalse(json.loads(self.lock.read_text())["cleanup_complete"])
        code, _, blocked = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(blocked["launched"])

    def test_cancellation_before_launch_keeps_interrupted_result(self):
        with mock.patch.object(runner, "require_guest", side_effect=KeyboardInterrupt), \
                mock.patch.object(runner.subprocess, "Popen") as launch:
            code, _, receipt = self.run_case()
        self.assertEqual(code, 130)
        self.assertEqual(receipt["exit_code"], 130)
        self.assertEqual(receipt["failure"], "interrupted")
        self.assertFalse(receipt["launched"])
        launch.assert_not_called()

    def test_detached_engine_is_removed_but_unrelated_process_survives(self):
        unrelated_data = self.root / "unrelated-data"
        unrelated_data.mkdir()
        sentinel = unrelated_data / "keep"
        sentinel.write_text("untouched")
        unrelated = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(120)",
                                      "--data-dir", str(unrelated_data)], start_new_session=True)
        self.addCleanup(unrelated.wait)
        self.addCleanup(lambda: unrelated.poll() is None and unrelated.kill())
        self.executable.write_text("#!" + sys.executable + "\nimport subprocess,sys,os,time\n"
            "from pathlib import Path\n"
            "p=subprocess.Popen([sys.executable,'-c','import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(120)',"
            "'engine','--data-dir='+os.environ['ZWAI_HOME']],start_new_session=True)\n"
            "Path(os.environ['ZWAI_HOME'],'engine-pid').write_text(str(p.pid))\ntime.sleep(120)\n")
        scenario = """import os,time
from pathlib import Path
p=Path(os.environ['ZWAI_HOME'],'engine-pid')
for _ in range(100):
 if p.exists(): break
 time.sleep(.01)
assert p.exists()
Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'engine-pid').write_text(p.read_text())
"""
        code, evidence, receipt = self.run_case(scenario)
        self.assertEqual(code, 0)
        pid = int((evidence / "engine-pid").read_text())
        self.assertIsNone(runner.process_argv(pid))
        self.assertTrue(receipt["cleanup_complete"])
        self.assertIsNone(unrelated.poll())
        self.assertEqual(sentinel.read_text(), "untouched")

    def test_lock_conflict_refuses_before_launch_and_stale_cleanup_blocks_next(self):
        with runner.case_lock():
            code, _, receipt = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(receipt["launched"])
        self.lock.write_text(json.dumps({"cleanup_complete": False, "case_id": "old"}))
        code, _, receipt = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(receipt["launched"])

    def test_cleanup_failure_preserves_case_and_blocks_next_launch(self):
        captured = []
        def pretend_survivor(bundle, data, processes, **kwargs):
            captured.extend(processes)
            return [999999]
        real_save = runner.save_lock
        writes = []
        def save_once(stream, row):
            writes.append(row["cleanup_complete"])
            if len(writes) > 1:
                stream.seek(0)
                stream.truncate()
                raise OSError("failed lock rewrite")
            return real_save(stream, row)
        with mock.patch.object(runner, "stop_owned", side_effect=pretend_survivor), \
                mock.patch.object(runner, "save_lock", side_effect=save_once):
            code, _, receipt = self.run_case()
        self.assertEqual(writes, [False])
        self.assertFalse(json.loads(self.lock.read_text())["cleanup_complete"])
        self.assertNotEqual(code, 0)
        self.assertFalse(receipt["cleanup_complete"])
        self.assertFalse(receipt["data_removed"])
        self.assertEqual(receipt["remaining_owned_pids"], [999999])
        # The fixture subprocess must still be reaped, even when simulating a survivor.
        runner.stop_owned(Path(receipt["app_copy"]), Path(receipt["data_dir"]), captured, grace=.1)
        self.assertTrue(Path(receipt["data_dir"]).exists())
        code, _, next_receipt = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(next_receipt["launched"])

    def test_exact_identity_does_not_match_siblings_or_embedded_argument_text(self):
        bundle, data = Path("/tmp/one.app"), Path("/tmp/data")
        for argv in (["/tmp/one.app/Contents/MacOS/app"], ["x", "--data-dir", str(data)],
                     ["x", "--data-dir=" + str(data)]):
            self.assertTrue(runner.owns_argv(argv, bundle, data))
        for argv in (["/tmp/one.app-other/app"], ["x", "--data-dir", str(data) + "-other"],
                     ["x", "--data-dir=" + str(data) + "-other"], ["x", "echo --data-dir /tmp/data"]):
            self.assertFalse(runner.owns_argv(argv, bundle, data))

    def test_completed_case_cannot_share_data_with_next_case(self):
        first = self.run_case()[2]
        second = self.run_case()[2]
        self.assertNotEqual(first["case_id"], second["case_id"])
        self.assertNotEqual(first["data_dir"], second["data_dir"])
        self.assertNotEqual(first["workspace"], second["workspace"])
        self.assertTrue(first["cleanup_complete"] and second["cleanup_complete"])

    def test_engine_and_scenario_helpers_survive_parent_exit_but_not_case_cleanup(self):
        # The engine's tool has neither bundle nor data argv markers. Its private
        # process group remains ours after the engine leader has already exited.
        engine_code = ("import subprocess,sys,time,os; from pathlib import Path; "
                       "p=subprocess.Popen([sys.executable,'-c','import time; time.sleep(120)']); "
                       "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'tool-pid').write_text(str(p.pid)); "
                       "time.sleep(.3)")
        self.executable.write_text("#!" + sys.executable + "\nimport subprocess,sys,os,time\n"
            "subprocess.Popen([sys.executable,'-c'," + repr(engine_code) + ", 'engine','--data-dir',os.environ['ZWAI_HOME']],start_new_session=True)\n"
            "time.sleep(120)\n")
        scenario = """import subprocess,sys,os,time
from pathlib import Path
p=subprocess.Popen([sys.executable,'-c','import os,time; os.setpgrp(); time.sleep(120)'])
Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'scenario-helper-pid').write_text(str(p.pid))
time.sleep(.8)
"""
        code, evidence, receipt = self.run_case(scenario)
        self.assertEqual(code, 0)
        self.assertTrue(receipt["cleanup_complete"])
        for name in ("tool-pid", "scenario-helper-pid"):
            self.assertIsNone(runner.process_argv(int((evidence / name).read_text())))

    def test_immediate_detached_helper_is_owned_after_parent_disappears(self):
        code = ("import subprocess,sys,os; from pathlib import Path; "
                "p=subprocess.Popen([sys.executable,'-c','import time; time.sleep(120)'],start_new_session=True); "
                "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'escaped-pid').write_text(str(p.pid))")
        self.executable.write_text("#!" + sys.executable + "\n" + code + "\n")
        # Even without any lifecycle samples, inherited case identity attributes
        # the surviving detached helper after its root already disappeared.
        scenario = """import os,time
from pathlib import Path
p=Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'escaped-pid')
for _ in range(100):
 if p.exists(): break
 time.sleep(.01)
assert p.exists()
time.sleep(.2)
"""
        def wait_without_sampling(process, timeout):
            return process.wait(timeout=timeout)
        original_sample = runner.ProcessScope.sample
        def skip_first_snapshot(scope):
            if not getattr(scope, "initial_skipped", False):
                scope.initial_skipped = True
                return []
            return original_sample(scope)
        with mock.patch.object(runner, "wait_scenario", side_effect=wait_without_sampling), \
                mock.patch.object(runner.ProcessScope, "sample", skip_first_snapshot):
            status, evidence, receipt = self.run_case(scenario)
        self.assertEqual(status, 0)
        self.assertTrue(receipt["cleanup_complete"])
        self.assertIsNone(runner.process_argv(int((evidence / "escaped-pid").read_text())))
        self.assertEqual(runner.case_marker([b"SECRET=never-return", b"ZWAI_NATIVE_CASE_ID=ours"]), "ours")

    def test_real_cli_sigterm_cleans_and_cross_process_lock_refuses(self):
        evidence = self.root / "cli-evidence"
        wrapper = ("import importlib.util,sys; "
                   "s=importlib.util.spec_from_file_location('runner',sys.argv[1]); "
                   "r=importlib.util.module_from_spec(s); s.loader.exec_module(r); "
                   "r.require_guest=lambda:None; r.LOCK_PATH=r.Path(sys.argv[2]); "
                   "raise SystemExit(r.main(sys.argv[3:]))")
        command = [sys.executable, "-c", wrapper, str(SCRIPT), str(self.lock),
                   "--app", str(self.app), "--evidence", str(evidence), "--timeout", "30", "--",
                   sys.executable, "-c", "import time; print('ready',flush=True); time.sleep(120)"]
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(lambda: process.poll() is None and process.kill())
        for _ in range(200):
            log = evidence / "scenario.stdout.log"
            if log.exists() and "ready" in log.read_text():
                break
            if process.poll() is not None:
                _, errors = process.communicate()
                receipt_path = evidence / "cleanup.json"
                self.fail("headless CLI exited before readiness: " + errors.decode()
                          + (receipt_path.read_text() if receipt_path.exists() else "no cleanup receipt"))
            time.sleep(.02)
        else:
            self.fail("headless CLI scenario did not start")
        with mock.patch.dict(os.environ, {"TMPDIR": "/different/tmp"}):
            code, _, rejected = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(rejected["launched"])
        process.send_signal(signal.SIGTERM)
        _, errors = process.communicate(timeout=10)
        self.assertEqual(process.returncode, 130, errors.decode())
        receipt = json.loads((evidence / "cleanup.json").read_text())
        self.assertTrue(receipt["cleanup_complete"])
        self.assertFalse(Path(receipt["data_dir"]).exists())

    def test_invalid_app_inputs_and_cli_validation_leave_no_live_case(self):
        for invalid in ("missing", "../outside", ""):
            with self.subTest(executable=invalid):
                with (self.app / "Contents/Info.plist").open("wb") as stream:
                    plistlib.dump({"CFBundleExecutable": invalid}, stream)
                code, _, receipt = self.run_case()
                self.assertNotEqual(code, 0)
                self.assertTrue(receipt["cleanup_complete"])
                self.assertFalse(receipt["launched"])
        with self.assertRaises(ValueError):
            runner.run_case(self.app, self.app / "evidence", 1, ["true"])
        for timeout, command in ((0, ["true"]), (float("nan"), ["true"]), (1, [])):
            code = runner.run_case(self.app, self.root / str(time.monotonic_ns()), timeout, command)
            self.assertNotEqual(code, 0)
        with mock.patch.object(runner, "run_case", return_value=0) as run:
            self.assertEqual(runner.main(["--app", str(self.app), "--evidence", str(self.root / "main"),
                                          "--timeout", "1", "--", "true"]), 0)
            self.assertEqual(run.call_args.args[-1], ["true"])
        with mock.patch.object(runner, "run_case", side_effect=ValueError("invalid")), redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as outcome:
                runner.main(["--app", str(self.app), "--evidence", str(self.root / "main"), "--timeout", "1"])
            self.assertEqual(outcome.exception.code, 2)

    def test_non_dictionary_bundle_metadata_is_reported_without_launch(self):
        with (self.app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump(["not bundle metadata"], stream)
        with mock.patch.object(runner.subprocess, "Popen") as launch, \
                mock.patch.object(runner.ProcessScope, "sample", return_value=[]):
            code, _, receipt = self.run_case()
        self.assertEqual(code, 2)
        self.assertIn("dictionary", receipt["failure"])
        self.assertTrue(receipt["cleanup_complete"])
        self.assertTrue(receipt["data_removed"])
        launch.assert_not_called()

    def test_engine_log_persists_after_data_removal_and_io_failure_latches_lock(self):
        scenario = "import os; from pathlib import Path; Path(os.environ['ZWAI_HOME'],'engine.log').write_text('engine trace')"
        code, evidence, _ = self.run_case(scenario)
        self.assertEqual(code, 0)
        self.assertEqual((evidence / "engine.log").read_text(), "engine trace")
        with mock.patch.object(runner.shutil, "rmtree", side_effect=OSError("cannot remove")):
            code, _, receipt = self.run_case()
        self.assertEqual(code, 3)
        self.assertFalse(receipt["cleanup_complete"])
        self.assertIn("cannot remove", receipt["cleanup_error"])

    def test_default_lock_identity_is_independent_of_tmpdir(self):
        paths = []
        for directory in ("/tmp/first", "/tmp/second"):
            with mock.patch.dict(os.environ, {"TMPDIR": directory}), \
                    mock.patch.object(tempfile, "gettempdir", return_value=directory):
                paths.append(runpy.run_path(str(SCRIPT))["LOCK_PATH"])
        self.assertEqual(paths[0], paths[1])
        self.assertEqual(paths[0], Path("/tmp") / f"zwai-native-macos-{os.getuid()}.lock")

    def test_corrupt_lock_is_not_silently_reused(self):
        self.lock.write_text("invalid json")
        code, _, receipt = self.run_case()
        self.assertNotEqual(code, 0)
        self.assertFalse(receipt["launched"])

    def test_guest_gate_fails_closed_for_host_missing_sysctl_and_other_os(self):
        mock.patch.stopall()
        for system, result in (("Linux", b"1\n"), ("Darwin", b"0\n"), ("Darwin", b"")):
            with self.subTest(system=system, result=result), mock.patch.object(runner.platform, "system", return_value=system), \
                    mock.patch.object(runner.subprocess, "check_output", return_value=result):
                with self.assertRaises(RuntimeError):
                    runner.require_guest()
        with mock.patch.object(runner.platform, "system", return_value="Darwin"), \
                mock.patch.object(runner.subprocess, "check_output", return_value=b"1\n"):
            runner.require_guest()
        for error in (FileNotFoundError(), subprocess.TimeoutExpired("sysctl", 5)):
            with mock.patch.object(runner.platform, "system", return_value="Darwin"), \
                    mock.patch.object(runner.subprocess, "check_output", side_effect=error):
                with self.assertRaises(RuntimeError):
                    runner.require_guest()

    def test_host_refusal_never_launches_or_creates_case(self):
        with mock.patch.object(runner, "require_guest", side_effect=RuntimeError("physical host refused")), \
                mock.patch.object(runner.subprocess, "Popen") as launch:
            code, _, receipt = self.run_case()
        self.assertNotEqual(code, 0)
        launch.assert_not_called()
        self.assertFalse(receipt["virtualized"])
        self.assertFalse(receipt["launched"])

    def procargs_fixture(self, failures):
        raw = bytes(ctypes.c_int(2)) + b"/fixture\0\0fixture\0arg\0ZWAI_NATIVE_CASE_ID=fixture-id\0SECRET=never-export\0"
        phases = []
        def sysctl(mib, count, output, size, *unused):
            phase = "size" if output is None else "read"
            phases.append(phase)
            if failures and failures[0][0] == phase:
                _, error = failures.pop(0)
                ctypes.set_errno(error)
                return -1
            ctypes.cast(size, ctypes.POINTER(ctypes.c_size_t)).contents.value = len(raw)
            if output is not None:
                ctypes.memmove(output, raw, len(raw))
            return 0
        library = mock.Mock(sysctl=mock.Mock(side_effect=sysctl))
        return library, phases

    def test_process_arguments_retry_bounded_exec_and_exit_races(self):
        for phase in ("size", "read"):
            for error in (errno.EIO, errno.ENOMEM):
                with self.subTest(phase=phase, error=error):
                    library, phases = self.procargs_fixture([(phase, error)])
                    with mock.patch.object(runner.platform, "system", return_value="Darwin"), \
                            mock.patch.object(runner.ctypes, "CDLL", return_value=library), \
                            mock.patch.object(runner.time, "sleep") as pause:
                        self.assertEqual(runner.process_identity(123), (["fixture", "arg"], "fixture-id"))
                    self.assertEqual(pause.call_count, 1)
                    self.assertEqual(phases[-2:], ["size", "read"])
        library, phases = self.procargs_fixture([("read", errno.EIO), ("size", errno.ESRCH)])
        with mock.patch.object(runner.platform, "system", return_value="Darwin"), \
                mock.patch.object(runner.ctypes, "CDLL", return_value=library), \
                mock.patch.object(runner.time, "sleep"):
            self.assertEqual(runner.process_identity(123), (None, None))
        self.assertEqual(phases, ["size", "read", "size"])

    def test_unreadable_live_process_remains_failure_after_retry_limit(self):
        for phase in ("size", "read"):
            for error, attempts in ((errno.EIO, 3), (errno.ENOMEM, 3), (errno.ENOSYS, 1)):
                with self.subTest(phase=phase, error=error):
                    library, phases = self.procargs_fixture([(phase, error)] * attempts)
                    with mock.patch.object(runner.platform, "system", return_value="Darwin"), \
                            mock.patch.object(runner.ctypes, "CDLL", return_value=library), \
                            mock.patch.object(runner.time, "sleep") as pause:
                        with self.assertRaises(OSError) as raised:
                            runner.process_identity(123)
                    self.assertEqual(raised.exception.errno, error)
                    self.assertIn("123", str(raised.exception))
                    self.assertEqual(phases.count(phase), attempts)
                    self.assertEqual(pause.call_count, attempts - 1)

    def test_unreadable_owned_process_cannot_claim_cleanup_or_unlock_next_case(self):
        original_launch, original_identity = runner.subprocess.Popen, runner.process_identity
        processes = []
        def launch(command, *args, **kwargs):
            process = original_launch(command, *args, **kwargs)
            if len(command) > 1 and command[1] == "desktop":
                processes.append(process)
            return process
        def identity(pid):
            if any(process.pid == pid for process in processes):
                raise OSError(errno.EIO, f"cannot inspect process arguments for pid {pid}")
            return original_identity(pid)
        try:
            with mock.patch.object(runner.subprocess, "Popen", side_effect=launch), \
                    mock.patch.object(runner, "process_identity", side_effect=identity):
                code, _, receipt = self.run_case()
            self.assertEqual(code, 3)
            self.assertFalse(receipt["cleanup_complete"])
            self.assertFalse(receipt["data_removed"])
            self.assertIn(str(processes[0].pid), receipt["cleanup_error"])
            self.assertTrue(Path(receipt["data_dir"]).is_dir())
            code, _, blocked = self.run_case()
            self.assertNotEqual(code, 0)
            self.assertFalse(blocked["launched"])
        finally:
            for process in processes:
                if process.poll() is None:
                    process.kill()
                process.wait()


if __name__ == "__main__":
    unittest.main()
