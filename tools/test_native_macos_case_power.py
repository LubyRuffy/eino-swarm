"""Power ownership belongs to the original case, never to a detached login job."""
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import signal
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock


sys.path.insert(0, str(Path(__file__).parent))
import native_macos_case_power as power
import native_macos_case as runner


class PowerCaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.processes = []
        self.addCleanup(self.temp.cleanup)
        self.addCleanup(self.reap)
        self.binding = dict(execution_id="e" * 32, launch_id="a" * 32,
                            source_sha="b" * 40, package_sha256="c" * 64,
                            phase="idle", session={"uid": os.getuid(), "session_id": 8})
        self.environment = dict(os.environ, ZWAI_NATIVE_CASE_ID="d" * 32,
                                ZWAI_NATIVE_EVIDENCE=str(self.root))
        self.receipt = dict(case_id="d" * 32, cleanup_complete=False)
        self.lock = (self.root / "case.lock").open("w+")
        patcher = mock.patch.object(runner, "LOCK_PATH", self.root / "case.lock")
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(self.lock.close)
        self.lock.write(json.dumps(self.receipt))
        self.lock.flush()
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def reap(self):
        for process in self.processes:
            if process.poll() is None:
                if os.getpgid(process.pid) == process.pid:
                    os.killpg(process.pid, signal.SIGTERM)
                else:
                    process.terminate()
            process.wait(timeout=3)

    def child(self):
        process = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"],
                                   start_new_session=True)
        self.processes.append(process)
        return process

    def scenario_with_hold(self):
        destination = self.root / ("child-" + str(len(self.processes)))
        script = ("import os,pathlib,subprocess,sys,time; "
                  "p=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)']); "
                  "pathlib.Path(sys.argv[1]).write_text(str(p.pid)); time.sleep(30)")
        scenario = subprocess.Popen([sys.executable, "-c", script, str(destination)],
                                    env=self.environment, start_new_session=True)
        self.processes.append(scenario)
        end = time.monotonic() + 1
        while not destination.exists():
            self.assertLess(time.monotonic(), end)
            time.sleep(.005)
        child = int(destination.read_text())
        expected = power.await_identity(scenario, power.process_identity(os.getpid())["executable"], end)
        self.assertEqual(expected["pid"], scenario.pid)
        while power.process_identity(child)["executable"] != expected["executable"]:
            self.assertLess(time.monotonic(), end)
            time.sleep(.005)
        return scenario, power.process_identity(child)

    def spawn_hold(self, environment, processes, deadline, name, **kwargs):
        process = self.child()
        if processes is not self.processes:
            processes.append(process)
        identity = power.await_identity(process, power.process_identity(os.getpid())["executable"], deadline)
        return dict(process=process, identity=identity)

    def sampled(self, expected, evidence, deadline, environment, required):
        if power.process_identity(expected["pid"]) != expected:
            raise RuntimeError("identity changed")
        return dict(identity=expected, types=sorted(required))

    def prepare(self, before=None):
        if before is None:
            before = lambda context, deadline: lambda: None
        with mock.patch.object(power, "start_hold", self.spawn_hold), \
                mock.patch.object(power, "capture_assertions", self.sampled):
            callback = power.make_prepare(self.binding, before, time.monotonic() + 2)
            handoff = callback(self.lock, self.receipt, self.environment,
                               self.processes, object(), time.monotonic() + 3)
        return handoff.handoff

    def ack(self, scenario_process, hold, **changes):
        row = dict(schema=1, binding=json.loads(self.environment["ZWAI_NATIVE_POWER_BINDING"]),
                   scenario=power.process_identity(scenario_process.pid), assertion=hold["identity"],
                   types=sorted(power.SETUP_TYPES))
        row.update(changes)
        Path(self.environment["ZWAI_NATIVE_POWER_ACK"]).write_text(json.dumps(row))

    def test_bridge_is_registered_before_actor_and_failure_stays_owned(self):
        def actor(context, deadline):
            self.assertIs(context["processes"], self.processes)
            self.assertIs(context["lock"], self.lock)
            self.assertFalse(context["receipt"]["cleanup_complete"])
            self.assertEqual(len(self.processes), 1)
            self.assertIsNone(self.processes[0].poll())
            raise RuntimeError("actor refused unknown session")
        with self.assertRaisesRegex(RuntimeError, "unknown session"):
            self.prepare(actor)
        self.assertIsNone(self.processes[0].poll())

    def test_three_fresh_assertions_and_same_birth_are_required_before_release(self):
        handoff = self.prepare()
        bridge = self.processes[0]
        scenario, assertion = self.scenario_with_hold()
        self.ack(scenario, dict(identity=assertion))
        with mock.patch.object(power, "capture_assertions", self.sampled):
            handoff(scenario, time.monotonic() + 2)
        self.assertIsNotNone(bridge.poll())
        self.assertEqual(power.process_identity(assertion["pid"]), assertion)

    def test_partial_wrong_owner_and_wrong_birth_ack_never_release_bridge(self):
        for change in ({"types": list(power.PASSIVE_TYPES)}, {"binding": {}},
                       {"scenario": dict(pid=1, birth=[1, 0], executable="/wrong")},
                       {"assertion": dict(pid=1, birth=[1, 0], executable="/wrong")}):
            with self.subTest(change=change):
                handoff = self.prepare()
                bridge = self.processes[-1]
                scenario, assertion = self.child(), self.child()
                self.ack(scenario, dict(identity=power.process_identity(assertion.pid)), **change)
                with mock.patch.object(power, "capture_assertions", self.sampled), \
                        self.assertRaises(RuntimeError):
                    handoff(scenario, time.monotonic() + 1)
                self.assertIsNone(bridge.poll())
                Path(self.environment["ZWAI_NATIVE_POWER_ACK"]).unlink()

    def test_ack_claim_cannot_replace_actual_assertion_sampling(self):
        handoff = self.prepare()
        bridge = self.processes[0]
        scenario, assertion = self.scenario_with_hold()
        self.ack(scenario, dict(identity=assertion))
        with mock.patch.object(power, "capture_assertions", side_effect=RuntimeError("level off")), \
                self.assertRaisesRegex(RuntimeError, "level off"):
            handoff(scenario, time.monotonic() + 1)
        self.assertIsNone(bridge.poll())

    def test_missing_ack_uses_original_deadline_and_keeps_bridge_owned(self):
        handoff = self.prepare()
        scenario = self.child()
        start = time.monotonic()
        with self.assertRaisesRegex(RuntimeError, "deadline"):
            handoff(scenario, start + .04)
        self.assertLess(time.monotonic() - start, .4)
        self.assertIsNone(self.processes[0].poll())

    def test_complete_latch_and_unheld_fd_refuse_before_any_power_spawn(self):
        callback = power.make_prepare(self.binding, lambda *args: lambda: None, time.monotonic() + 1)
        for complete in (True, False):
            self.receipt["cleanup_complete"] = complete
            self.lock.seek(0)
            self.lock.truncate()
            self.lock.write(json.dumps(self.receipt))
            self.lock.flush()
            if not complete:
                fcntl.flock(self.lock, fcntl.LOCK_UN)
            with mock.patch.object(power, "start_hold") as spawn, self.assertRaises(RuntimeError):
                callback(self.lock, self.receipt, self.environment, self.processes,
                         object(), time.monotonic() + 1)
            spawn.assert_not_called()

    def test_actor_cannot_complete_latch_while_bridge_is_still_owned(self):
        def actor(context, deadline):
            context["receipt"]["cleanup_complete"] = True
        with self.assertRaisesRegex(RuntimeError, "incomplete"):
            self.prepare(actor)
        self.assertIsNone(self.processes[0].poll())

    def test_actual_runner_fdopen_lock_has_numeric_name(self):
        descriptor = os.open(self.root / "fdopen.lock", os.O_CREAT | os.O_RDWR, 0o600)
        with os.fdopen(descriptor, "r+") as stream:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            json.dump(self.receipt, stream)
            stream.flush()
            with mock.patch.object(runner, "LOCK_PATH", self.root / "fdopen.lock"):
                self.assertEqual(power.validate_case_context(stream, self.receipt, self.environment),
                                 self.receipt["case_id"])

    def test_final_frame_guard_is_retained_until_immediate_before_launch(self):
        observed = []
        def actor(context, deadline):
            def final():
                observed.append("final")
                raise RuntimeError("original Root frame expired")
            return final
        with mock.patch.object(power, "start_hold", self.spawn_hold), \
                mock.patch.object(power, "capture_assertions", self.sampled):
            callback = power.make_prepare(self.binding, actor, time.monotonic() + 1)
            preparation = callback(self.lock, self.receipt, self.environment,
                                   self.processes, object(), time.monotonic() + 2)
        self.assertEqual(observed, [])
        with self.assertRaisesRegex(RuntimeError, "Root frame expired"):
            preparation.before_launch()
        self.assertEqual(observed, ["final"])
        self.assertIsNone(self.processes[0].poll())

    def test_bridge_exit_before_receiver_ack_cannot_claim_continuous_handoff(self):
        handoff = self.prepare()
        self.processes[0].terminate()
        self.processes[0].wait(timeout=1)
        scenario, assertion = self.scenario_with_hold()
        self.ack(scenario, dict(identity=assertion))
        with mock.patch.object(power, "capture_assertions", self.sampled), \
                self.assertRaisesRegex(RuntimeError, "bridge"):
            handoff(scenario, time.monotonic() + 1)
        self.assertFalse((self.root / "power-handoff.json").exists())

    def test_di_is_proved_before_setup_user_activity_is_released(self):
        setup, passive = self.child(), self.child()
        hold = dict(process=setup, identity=power.process_identity(setup.pid),
                    environment=self.environment, evidence=self.root)
        with mock.patch.object(power, "capture_assertions", side_effect=RuntimeError("no di")), \
                self.assertRaisesRegex(RuntimeError, "no di"):
            power.release_setup_hold(hold, passive, time.monotonic() + 1)
        self.assertIsNone(setup.poll())
        with mock.patch.object(power, "capture_assertions", self.sampled):
            power.release_setup_hold(hold, passive, time.monotonic() + 1)
        self.assertIsNotNone(setup.poll())
        self.assertIsNone(passive.poll())

    def test_passive_takeover_cannot_hide_user_activity_at_measurement_start(self):
        setup, passive = self.child(), self.child()
        hold = dict(process=setup, identity=power.process_identity(setup.pid),
                    environment=self.environment, evidence=self.root)
        with mock.patch.object(power, "capture_assertions", return_value=dict(types=sorted(power.SETUP_TYPES))), \
                self.assertRaisesRegex(RuntimeError, "UserIsActive"):
            power.release_setup_hold(hold, passive, time.monotonic() + 1)
        self.assertIsNone(setup.poll())

    def test_assertion_parser_rejects_off_bool_partial_and_malformed_rows(self):
        rows = [{"AssertType": value, "AssertLevel": 255} for value in power.SETUP_TYPES]
        self.assertEqual(power.assertion_types(rows), power.SETUP_TYPES)
        for invalid in ([dict(rows[0], AssertLevel=True)], [dict(rows[0], AssertLevel=0)],
                        [dict(rows[0], AssertLevel=1)], [None], {}, [dict(AssertLevel=255)]):
            with self.subTest(invalid=invalid), self.assertRaises(RuntimeError):
                power.assertion_types(invalid)

    def test_real_headless_exec_transition_is_bound_before_identity_freeze(self):
        shell = subprocess.Popen(["/bin/sh", "-c", 'sleep .08; exec "$1" -c "import time; time.sleep(30)"',
                                  "fixture", sys.executable], start_new_session=True)
        self.processes.append(shell)
        expected_path = power.process_identity(os.getpid())["executable"]
        actual = power.await_identity(shell, expected_path, time.monotonic() + 1)
        self.assertEqual(actual["pid"], shell.pid)
        self.assertEqual(actual["executable"], expected_path)

    def test_actual_assertion_child_needs_same_scenario_parent_and_case_marker(self):
        scenario, assertion = self.scenario_with_hold()
        expected = power.process_identity(scenario.pid)
        power.assertion_child(assertion, expected, self.environment, time.monotonic() + 1)
        for parent, environment in ((power.process_identity(os.getpid()), self.environment),
                                    (expected, dict(self.environment, ZWAI_NATIVE_CASE_ID="a" * 32))):
            with self.subTest(parent=parent), self.assertRaises(RuntimeError):
                power.assertion_child(assertion, parent, environment, time.monotonic() + 1)

    def test_system_utility_marker_missing_stays_unknown_under_real_parent_proof(self):
        scenario, assertion = self.scenario_with_hold()
        original = runner.process_identity
        def without_child_marker(pid):
            return (None, None) if pid == assertion["pid"] else original(pid)
        with mock.patch.object(runner, "process_identity", without_child_marker):
            proof = power.assertion_child(assertion, power.process_identity(scenario.pid),
                                          self.environment, time.monotonic() + 1)
        self.assertEqual(proof["assertion_marker"], "unknown")
        self.assertEqual(proof["scenario_marker"], "matching")

    def test_scenario_setup_waits_for_original_driver_commit_and_keeps_deadline(self):
        end = time.monotonic() + 1
        bound = dict(self.binding, case_id=self.receipt["case_id"],
                     runner=power.process_identity(os.getppid()))
        self.environment.update(ZWAI_NATIVE_POWER_BINDING=json.dumps(bound),
                                ZWAI_NATIVE_PREPARE_DEADLINE_RAW=str(power.portable_prepare_deadline(end)),
                                ZWAI_NATIVE_POWER_ACK=str(self.root / "power-takeover.json"))
        errors = []
        def driver():
            try:
                ack = Path(self.environment["ZWAI_NATIVE_POWER_ACK"])
                while not ack.exists():
                    if time.monotonic() >= end:
                        raise RuntimeError("no scenario ACK")
                    time.sleep(.005)
                row = json.loads(ack.read_text())
                power._save(self.root / "power-handoff.json", dict(
                    binding=bound, scenario=row["scenario"], assertion=row["assertion"],
                    ownership={}, bridge_reaped=True))
            except Exception as error:
                errors.append(error)
        thread = threading.Thread(target=driver)
        thread.start()
        try:
            with mock.patch.object(power, "start_hold", wraps=self.spawn_hold) as spawn, \
                    mock.patch.object(power, "capture_assertions", self.sampled):
                hold = power.start_setup_hold(self.root, self.environment, end + 10)
            self.assertFalse(spawn.call_args.kwargs["new_session"])
            self.assertLessEqual(spawn.call_args.args[2], end)
            self.assertLess(end - spawn.call_args.args[2], .01)
            self.assertEqual(hold["prepare_deadline"], spawn.call_args.args[2])
            self.assertIsNone(hold["process"].poll())
            self.assertTrue((self.root / "power-handoff.json").is_file())
        finally:
            thread.join(timeout=2)
        self.assertEqual(errors, [])

    def test_scenario_setup_never_acknowledges_missing_three_assertions(self):
        end = time.monotonic() + 1
        self.environment.update(ZWAI_NATIVE_POWER_BINDING=json.dumps(dict(
            self.binding, case_id=self.receipt["case_id"], runner=power.process_identity(os.getppid()))),
            ZWAI_NATIVE_PREPARE_DEADLINE_RAW=str(power.portable_prepare_deadline(end)),
            ZWAI_NATIVE_POWER_ACK=str(self.root / "ack.json"))
        with mock.patch.object(power, "start_hold", self.spawn_hold), \
                mock.patch.object(power, "capture_assertions", side_effect=RuntimeError("no three flags")), \
                self.assertRaisesRegex(RuntimeError, "no three"):
            power.start_setup_hold(self.root, self.environment, end)
        self.assertFalse((self.root / "ack.json").exists())
        self.assertIsNone(self.processes[0].poll())

    def test_cancel_during_spawn_registers_real_child_before_delivery(self):
        original = subprocess.Popen
        def interrupted(*args, **kwargs):
            child = original(*args, **kwargs)
            os.kill(os.getpid(), signal.SIGTERM)
            return child
        def cancel(signum, frame):
            raise KeyboardInterrupt()
        prior = signal.signal(signal.SIGTERM, cancel)
        try:
            with mock.patch.object(power.subprocess, "Popen", interrupted), self.assertRaises(KeyboardInterrupt):
                power._spawn_registered([sys.executable, "-c", "import time;time.sleep(30)"],
                                        self.environment, self.processes, subprocess.DEVNULL, subprocess.DEVNULL)
            self.assertEqual(len(self.processes), 1)
            self.assertIsNone(self.processes[0].poll())
        finally:
            signal.signal(signal.SIGTERM, prior)

    def test_scenario_assertion_inherits_original_cleanup_group(self):
        child = power._spawn_registered([sys.executable, "-c", "import time;time.sleep(30)"],
                                        self.environment, self.processes, subprocess.DEVNULL,
                                        subprocess.DEVNULL, new_session=False)
        self.assertEqual(os.getpgid(child.pid), os.getpgrp())

    def test_native_snapshot_streams_survive_timeout_and_malformed_output(self):
        expected = dict(pid=77, birth=[1, 2], executable=power.CAFFEINATE)
        cases = (subprocess.TimeoutExpired(["snapshot"], .1, output=b"partial", stderr=b"original timeout"),
                 subprocess.CompletedProcess(["snapshot"], 0, stdout=b"bad plist", stderr=b"diagnostic"))
        for index, outcome in enumerate(cases):
            directory = self.root / str(index)
            directory.mkdir()
            with mock.patch.object(power, "process_identity", return_value=expected), \
                    mock.patch.object(power.subprocess, "run", side_effect=outcome if isinstance(outcome, Exception) else None,
                                      return_value=outcome), self.assertRaises(RuntimeError):
                power.capture_assertions(expected, directory, time.monotonic() + 1,
                                         self.environment, power.SETUP_TYPES)
            raw = next(directory.glob("*.stdout.plist")).read_bytes()
            self.assertEqual(raw, outcome.output if isinstance(outcome, Exception) else outcome.stdout)
            receipt = json.loads(next(directory.glob("*.json")).read_text())
            self.assertEqual(receipt["timed_out"], isinstance(outcome, Exception))

    def test_native_snapshot_requires_fresh_same_pid_three_level_255_flags(self):
        expected = dict(pid=77, birth=[1, 2], executable=power.CAFFEINATE)
        rows = [{"AssertType": kind, "AssertLevel": 255} for kind in power.SETUP_TYPES]
        result = subprocess.CompletedProcess(["snapshot"], 0, stdout=plistlib.dumps(rows), stderr=b"")
        with mock.patch.object(power, "process_identity", return_value=expected), \
                mock.patch.object(power.subprocess, "run", return_value=result):
            proof = power.capture_assertions(expected, self.root, time.monotonic() + 1,
                                              self.environment, power.SETUP_TYPES)
        self.assertEqual(proof["types"], sorted(power.SETUP_TYPES))
        changed = dict(expected, birth=[1, 3])
        with mock.patch.object(power, "process_identity", side_effect=[expected, changed]), \
                mock.patch.object(power.subprocess, "run", return_value=result), \
                self.assertRaisesRegex(RuntimeError, "changed during"):
            power.capture_assertions(expected, self.root, time.monotonic() + 1,
                                     self.environment, power.SETUP_TYPES)

    def test_readonly_native_cli_refuses_missing_case_marker_before_os_api(self):
        environment = dict(os.environ)
        environment.pop("ZWAI_NATIVE_CASE_ID", None)
        result = subprocess.run([sys.executable, "-B", str(Path(power.__file__)), "--snapshot", str(os.getpid())],
                                env=environment, capture_output=True, timeout=2)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"native case marker required", result.stderr)
        self.assertEqual(result.stdout, b"")

    def test_ack_fifo_symlink_partial_and_oversize_refuse_without_blocking(self):
        fifo, link = self.root / "fifo", self.root / "link"
        os.mkfifo(fifo)
        link.symlink_to(self.root / "missing")
        partial, oversized = self.root / "partial", self.root / "oversized"
        partial.write_text('{"schema":')
        oversized.write_text(" " * 65537)
        for path in (fifo, link, partial, oversized):
            started = time.monotonic()
            with self.subTest(path=path), self.assertRaises(RuntimeError):
                power.read_ack(path)
            self.assertLess(time.monotonic() - started, .2)

    def clt_clock_transfer(self, expired):
        if sys.platform != "darwin" or not Path("/Library/Developer/CommandLineTools/usr/bin/python3").is_file():
            self.skipTest("real macOS CLT Python 3.9 is unavailable; no substitute runtime")
        child = self.root / "clock-child.py"
        child.write_text('''import json,os,time
import native_macos_case_power as power
sample={}
def seen(environment,processes,deadline,name,**kwargs):
 sample.update(admitted=True,remaining=deadline-time.monotonic(),
  original_raw_remaining=float(environment['ZWAI_NATIVE_PREPARE_DEADLINE_RAW'])-time.clock_gettime(time.CLOCK_MONOTONIC_RAW))
 raise RuntimeError('observed before native spawn')
power.start_hold=seen
try: power.start_setup_hold(os.environ['ZWAI_NATIVE_EVIDENCE'],os.environ,time.monotonic()+10)
except RuntimeError as error:
 if not sample: sample.update(admitted=False,error=str(error))
print(json.dumps(sample))
''')
        parent = self.root / "clock-parent.py"
        parent.write_text('''import json,os,subprocess,sys,time
import native_macos_case_power as power
time.sleep(.35)
# Real CLT startup belongs to the live preparation budget. Keep the short
# expired case separate from the positive clock-domain preservation check.
deadline=time.monotonic()+(.30 if sys.argv[2]=='expired' else 2)
raw=time.clock_gettime(time.CLOCK_MONOTONIC_RAW)
raw_deadline=raw+(deadline-time.monotonic())
environment=dict(os.environ,ZWAI_NATIVE_PREPARE_DEADLINE=str(deadline),
 ZWAI_NATIVE_PREPARE_DEADLINE_RAW=str(raw_deadline))
environment['ZWAI_NATIVE_POWER_BINDING']=json.dumps(dict(case_id=environment['ZWAI_NATIVE_CASE_ID'],runner=power.process_identity(os.getpid())))
time.sleep(.40 if sys.argv[2]=='expired' else .04)
out=subprocess.check_output([sys.executable,'-B',sys.argv[1]],env=environment,timeout=2)
print(out.decode().strip())
''')
        environment = dict(self.environment, PYTHONPATH=str(Path(power.__file__).parent))
        output = subprocess.check_output(["/Library/Developer/CommandLineTools/usr/bin/python3", "-B",
                                          str(parent), str(child), "expired" if expired else "live"],
                                         env=environment, timeout=3)
        return json.loads(output)

    def test_real_clt39_parent_prefix_and_new_exec_cannot_extend_original_raw_budget(self):
        row = self.clt_clock_transfer(False)
        self.assertTrue(row["admitted"], row)
        self.assertGreater(row["remaining"], 0, row)
        self.assertLessEqual(row["remaining"], 2, row)
        self.assertLessEqual(row["remaining"], row["original_raw_remaining"] + .02, row)

    def test_real_clt39_expired_parent_budget_refuses_before_native_spawn(self):
        row = self.clt_clock_transfer(True)
        self.assertFalse(row["admitted"], row)
        self.assertIn("deadline", row["error"], row)

    def test_clt_clock_regression_explicitly_skips_when_the_runtime_is_unavailable(self):
        with mock.patch.object(sys, "platform", "linux"), mock.patch.object(subprocess, "check_output") as spawn:
            with self.assertRaisesRegex(unittest.SkipTest, "no substitute runtime"):
                self.clt_clock_transfer(False)
            spawn.assert_not_called()

    def test_parent_publishes_raw_once_and_removes_foreign_monotonic_field(self):
        self.environment["ZWAI_NATIVE_PREPARE_DEADLINE"] = "99999999"
        self.prepare()
        self.assertNotIn("ZWAI_NATIVE_PREPARE_DEADLINE", self.environment)
        left = float(self.environment["ZWAI_NATIVE_PREPARE_DEADLINE_RAW"]) - power.raw_clock()
        self.assertGreater(left, 0)
        self.assertLessEqual(left, 2)

    def test_raw_projection_and_reverse_conversion_use_conservative_sampling_order(self):
        with mock.patch.object(power, "raw_clock", return_value=100), \
                mock.patch.object(power.time, "monotonic", return_value=30):
            self.assertEqual(power.portable_prepare_deadline(40), 110)
        with mock.patch.object(power, "raw_clock", return_value=100), \
                mock.patch.object(power.time, "monotonic", return_value=300):
            self.assertEqual(power.inherited_prepare_deadline({"ZWAI_NATIVE_PREPARE_DEADLINE_RAW": "110"}), 310)
        for environment in ({"ZWAI_NATIVE_PREPARE_DEADLINE": "99999"}, {},
                            {"ZWAI_NATIVE_PREPARE_DEADLINE_RAW": "nan"}):
            with self.subTest(environment=environment), self.assertRaises(RuntimeError):
                power.inherited_prepare_deadline(environment)


if __name__ == "__main__":
    unittest.main()
