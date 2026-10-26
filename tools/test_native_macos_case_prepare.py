"""Preparation and power takeover share the original runner's case ownership."""
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("native_macos_case.py")
sys.path.insert(0, str(SCRIPT.parent))
import native_macos_case as runner

BRIDGE = """import json,os,signal,subprocess,sys,time
from pathlib import Path
child=subprocess.Popen([sys.executable,'-c','import time;time.sleep(120)'])
def stop(signum,frame):
    child.terminate();child.wait(timeout=2);raise SystemExit(0)
signal.signal(signal.SIGTERM,stop)
Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'bridge-ready.json').write_text(json.dumps(dict(pid=os.getpid(),child=child.pid)))
while True:time.sleep(.1)
"""

POWER_NATIVE_LEAF = """def install_native_leaf(power):
    import os,plistlib,subprocess,sys
    from pathlib import Path
    actual_spawn,actual_run=subprocess.Popen,subprocess.run
    executable=power.process_identity(os.getpid())['executable']
    def spawn(command,*args,**kwargs):
        if command[0]==executable and '-d' in command[1:]:
            types=power.SETUP_TYPES if '-u' in command else power.PASSIVE_TYPES
            child=actual_spawn([sys.executable,'-c','import time;time.sleep(120)'],*args,**kwargs)
            evidence=Path(kwargs['env']['ZWAI_NATIVE_EVIDENCE'])
            (evidence/('inert-IOPM-'+str(child.pid)+'.plist')).write_bytes(plistlib.dumps([
                dict(AssertType=value,AssertLevel=255) for value in sorted(types)]))
            return child
        return actual_spawn(command,*args,**kwargs)
    def snapshot(command,*args,**kwargs):
        if '--snapshot' in command:
            evidence=Path(kwargs['env']['ZWAI_NATIVE_EVIDENCE'])
            raw=(evidence/('inert-IOPM-'+command[-1]+'.plist')).read_bytes()
            return subprocess.CompletedProcess(command,0,raw,b'')
        return actual_run(command,*args,**kwargs)
    return executable,spawn,snapshot
"""


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="native-prepare-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "ScriptFixture.app"
        executable = self.app / "Contents/MacOS/fixture"
        executable.parent.mkdir(parents=True)
        with (self.app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump({"CFBundleExecutable": "fixture"}, stream)
        executable.write_text("#!" + sys.executable + "\nimport time\ntime.sleep(120)\n")
        executable.chmod(0o755)
        self.processes, self.scope, self.paths = [], None, []
        self.lock = self.root / "private.lock"
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(runner, "LOCK_PATH", self.lock).start()
        mock.patch.object(runner, "require_guest", return_value=None).start()

    def tearDown(self):
        result = self._outcome.result
        if any(test is self or getattr(test, "test_case", None) is self
               for test, _ in result.failures + result.errors):
            evidence = Path(tempfile.mkdtemp(prefix="native-prepare-failure-evidence-"))
            for source in self.root.iterdir():
                if source.is_dir() and "evidence" in source.name:
                    shutil.copytree(source, evidence / source.name)
            print("native preparation failure evidence: " + str(evidence), file=sys.stderr)
        # A failing regression still owns its inert children and disposable tree.
        for process in self.processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)
        for path in self.paths:
            if path.exists():
                shutil.rmtree(path)

    def main(self, prepare, scenario="pass", *, timeout=3, budget=8, entry=None):
        evidence = self.root / ("evidence-" + str(time.monotonic_ns()))
        def observed(*args):
            self.processes, self.scope = args[3:5]
            return prepare(*args)
        code = (entry or runner.main)(["--app", str(self.app), "--evidence", str(evidence),
                                       "--timeout", str(timeout), "--budget", str(budget), "--",
                                       sys.executable, "-c", scenario], prepare=observed)
        receipt = json.loads((evidence / "cleanup.json").read_text())
        if "case_root" in receipt:
            self.paths.append(Path(receipt["case_root"]))
        return code, evidence, receipt

    def bridge(self, lock, receipt, environment, processes, scope, deadline):
        self.assertIsInstance(scope, runner.ProcessScope)
        self.assertFalse(receipt["cleanup_complete"])
        self.assertEqual(json.loads(self.lock.read_text())["case_id"], receipt["case_id"])
        with self.lock.open() as another:
            with self.assertRaises(BlockingIOError):
                fcntl.flock(another, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.assertEqual(environment["ZWAI_NATIVE_CASE_ID"], receipt["case_id"])
        self.assertEqual(environment["HOME"], receipt["home_dir"])
        self.assertLess(time.monotonic(), deadline)
        child = subprocess.Popen([sys.executable, "-c", BRIDGE], env=environment,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                 start_new_session=True)
        processes.append(child)  # Register before observing/binding the bridge.
        path = Path(environment["ZWAI_NATIVE_EVIDENCE"]) / "bridge-ready.json"
        end = time.monotonic() + 3
        while not path.exists():
            if child.poll() is not None or time.monotonic() >= end:
                self.fail("inert bridge failed to publish its child")
            time.sleep(.01)
        return child, json.loads(path.read_text())

    def assert_clean(self, receipt):
        self.assertTrue(receipt["cleanup_complete"])
        self.assertTrue(receipt["data_removed"])
        self.assertEqual(receipt["remaining_owned_pids"], [])
        self.assertFalse(Path(receipt["case_root"]).exists())
        self.assertTrue(all(process.poll() is not None for process in self.processes))

    def test_prepare_refusal_reaps_registered_bridge_and_child_before_app_launch(self):
        owned = []
        def prepare(*args):
            bridge, row = self.bridge(*args)
            owned.append(row)
            raise RuntimeError("preparation refused")
        code, _, receipt = self.main(prepare)
        self.assertEqual(code, 2)
        self.assertFalse(receipt["launched"])
        self.assert_clean(receipt)
        with self.assertRaises(ProcessLookupError):
            os.kill(owned[0]["child"], 0)

    def test_copy_failure_never_prepares_or_allocates_a_fallback_case(self):
        prepare = mock.Mock(side_effect=AssertionError("prepare must not run"))
        original = runner.tempfile.mkdtemp
        with mock.patch.object(runner, "copy_app", side_effect=OSError("copy refused")), \
                mock.patch.object(runner.tempfile, "mkdtemp", wraps=original) as allocate:
            code, _, receipt = self.main(prepare)
        self.assertEqual(code, 2)
        self.assertEqual(allocate.call_count, 1)
        prepare.assert_not_called()
        self.assertFalse(receipt["launched"])
        self.assert_clean(receipt)

    def test_prepare_accepts_typed_hooks_or_none_and_refuses_unknown_fields(self):
        evidence = self.root / "unknown-preparation"
        code = runner.main(["--app", str(self.app), "--evidence", str(evidence),
                            "--timeout", "3", "--", sys.executable, "-c", "pass"], prepare=False)
        self.assertEqual(code, 2)
        self.assertNotIn("case_root", json.loads((evidence / "cleanup.json").read_text()))
        for value, expected in ((None, 0), (False, 2), (object(), 2), (lambda: None, 2),
                                (runner.CasePreparation(lambda: None, lambda *args: None), 0),
                                (runner.CasePreparation(None, lambda *args: None), 2),
                                (runner.CasePreparation(lambda: None, None), 2)):
            with self.subTest(value=type(value).__name__):
                code, _, receipt = self.main(lambda *args: value)
                self.assertEqual(code, expected)
                self.assertEqual(receipt["launched"], expected == 0)
                self.assert_clean(receipt)

    def test_handoff_refusal_cleans_the_real_scenario_app_and_bridge(self):
        for variant in ("malformed", "partial", "wrong-owner", "cancel", "valid"):
            with self.subTest(variant=variant):
                def prepare(*args):
                    bridge, _ = self.bridge(*args)
                    def handoff(scenario, deadline):
                        self.assertIn(scenario, self.processes)
                        self.assertIs(scenario.native_scope, self.scope)
                        self.assertIsNone(bridge.poll())
                        if variant == "cancel":
                            raise KeyboardInterrupt
                        path = Path(args[2]["ZWAI_NATIVE_EVIDENCE"]) / "ack.json"
                        while not path.exists():
                            if time.monotonic() >= deadline:
                                raise TimeoutError("ACK expired")
                            time.sleep(.01)
                        row = json.loads(path.read_text())
                        if row.get("pid") != scenario.pid or row.get("assertions") != [True] * 3:
                            raise ValueError("ACK does not bind this scenario and all assertions")
                        bridge.terminate()
                        bridge.wait(timeout=3)
                        self.assertTrue(any(process not in (bridge, scenario) and process.poll() is None
                                            for process in self.processes))
                    return runner.CasePreparation(lambda: None, handoff)
                value = "'{'" if variant == "malformed" else (
                    "json.dumps(dict(pid=os.getpid(),assertions=[True]))" if variant == "partial"
                    else "json.dumps(dict(pid=os.getpid(),assertions=[True]*3))" if variant == "valid"
                    else "json.dumps(dict(pid=os.getpid()+1,assertions=[True]*3))")
                scenario = ("import json,os,time;from pathlib import Path;"
                            "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'ack.json').write_text(" + value + ");"
                            "time.sleep(" + (".2" if variant == "valid" else "120") + ")")
                code, _, receipt = self.main(prepare, scenario)
                self.assertEqual(code, 130 if variant == "cancel" else 0 if variant == "valid" else 2)
                self.assertTrue(receipt["launched"])
                self.assert_clean(receipt)

    def test_late_prepare_cannot_open_app_or_start_a_new_wait_budget(self):
        real_clock, offset = time.monotonic, [0]
        def prepare(*args):
            self.bridge(*args)
            offset[0] += 10
        with mock.patch.object(runner.time, "monotonic", side_effect=lambda: real_clock() + offset[0]), \
                mock.patch.object(runner, "wait_scenario", wraps=runner.wait_scenario) as wait:
            code, _, receipt = self.main(prepare)
        self.assertEqual(code, 124)
        self.assertFalse(receipt["launched"])
        wait.assert_not_called()
        self.assert_clean(receipt)

    def test_late_handoff_consumes_the_original_scenario_deadline(self):
        real_clock, offset = time.monotonic, [0]
        def prepare(*args):
            self.bridge(*args)
            def handoff(scenario, deadline):
                self.assertLessEqual(deadline - time.monotonic(), .08)
                self.assertLessEqual(deadline, args[-1])
                offset[0] += max(0, deadline - time.monotonic()) + .1
            return runner.CasePreparation(lambda: None, handoff)
        with mock.patch.object(runner.time, "monotonic", side_effect=lambda: real_clock() + offset[0]), \
                mock.patch.object(runner, "wait_scenario", wraps=runner.wait_scenario) as wait:
            code, _, receipt = self.main(prepare, "import time;time.sleep(120)", timeout=.08)
        self.assertEqual(code, 124)
        self.assertEqual(receipt["scenario_limit_seconds"], .08)
        wait.assert_not_called()
        self.assert_clean(receipt)

    def test_final_admission_expiry_after_initial_check_cannot_open_app(self):
        real_clock, offset, checks = time.monotonic, [0], []
        def prepare(*args):
            self.bridge(*args)
            expiry = time.monotonic() + .5
            def admit():
                checks.append(time.monotonic())
                if time.monotonic() >= expiry:
                    raise RuntimeError("original frame expired")
            admit()
            offset[0] += 1  # The remaining native snapshot consumed the frame.
            return runner.CasePreparation(admit, lambda *args: None)
        with mock.patch.object(runner.time, "monotonic", side_effect=lambda: real_clock() + offset[0]):
            code, _, receipt = self.main(prepare)
        self.assertEqual(code, 2)
        self.assertEqual(len(checks), 2)
        self.assertFalse(receipt["launched"])
        self.assertIn("original frame expired", receipt["failure"])
        self.assert_clean(receipt)

    def test_actual_harness_shares_the_power_type_and_refuses_another_qa_cache(self):
        import native_macos_case_power as power
        harness = SCRIPT.with_name("native_macos_resilience.py")
        def load():
            spec = importlib.util.spec_from_file_location("actual_resilience_prepare_test", harness)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            return module
        self.assertIs(load().runner, runner)
        wrong_path = self.root / "another-qa/native_macos_case.py"
        wrong_path.parent.mkdir()
        shutil.copy2(SCRIPT, wrong_path)
        spec = importlib.util.spec_from_file_location("native_macos_case", wrong_path)
        wrong = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrong)
        with mock.patch.dict(sys.modules, {"native_macos_case": wrong}), \
                self.assertRaisesRegex(RuntimeError, "another QA directory"):
            load()

    def test_actual_harness_power_prepare_and_scenario_finish_the_two_way_handoff(self):
        import native_macos_case_power as power
        spec = importlib.util.spec_from_file_location("actual_resilience_power_test",
                                                    SCRIPT.with_name("native_macos_resilience.py"))
        harness = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(harness)
        self.assertIs(harness.runner, runner)
        native = {}
        exec(compile(POWER_NATIVE_LEAF, "<inert-native-power-leaf>", "exec"), native)
        executable, spawn, snapshot = native["install_native_leaf"](power)
        binding = dict(execution_id="e" * 32, launch_id="f" * 32, source_sha="b" * 40,
                       package_sha256="c" * 64, phase="idle", session={"uid": os.getuid()})
        def before_app(context, deadline):
            evidence = Path(context["environment"]["ZWAI_NATIVE_EVIDENCE"])
            self.assertIs(context["scope"], self.scope)
            return lambda: (evidence / "final-admission.txt").write_text("allowed")
        scenario = ("import os,sys,time,json,subprocess;from pathlib import Path\n"
                    "sys.path.insert(0," + repr(str(SCRIPT.parent)) + ")\n"
                    "import native_macos_case_power as power\n" + POWER_NATIVE_LEAF +
                    "executable,spawn,snapshot=install_native_leaf(power)\n"
                    "power.CAFFEINATE=executable;power.subprocess.Popen=spawn;power.subprocess.run=snapshot\n"
                    "evidence=Path(os.environ['ZWAI_NATIVE_EVIDENCE']);end=time.monotonic()+5\n"
                    "hold=power.start_setup_hold(evidence,dict(os.environ),end)\n"
                    "passive=subprocess.Popen([executable,'-d','-i','-t','5'],env=dict(os.environ))\n"
                    "power.await_identity(passive,executable,end)\n"
                    "power.release_setup_hold(hold,passive,end)\n"
                    "assert hold['process'].poll() is not None and passive.poll() is None\n"
                    "passive.terminate();passive.wait(timeout=2)\n"
                    "(evidence/'scenario-power-complete.json').write_text(json.dumps(dict(setup_reaped=True,passive_reaped=True)))\n")
        with mock.patch.object(power, "CAFFEINATE", executable), \
                mock.patch.object(power.subprocess, "Popen", side_effect=spawn), \
                mock.patch.object(power.subprocess, "run", side_effect=snapshot):
            prepare = power.make_prepare(binding, before_app, time.monotonic() + 7)
            code, evidence, receipt = self.main(prepare, scenario, timeout=5, entry=harness.runner.main)
        self.assertEqual(code, 0, (evidence / "scenario.stderr.log").read_text())
        self.assertEqual((evidence / "final-admission.txt").read_text(), "allowed")
        ack = json.loads((evidence / "power-takeover.json").read_text())
        committed = json.loads((evidence / "power-handoff.json").read_text())
        self.assertEqual(ack["scenario"], committed["scenario"])
        self.assertTrue(committed["bridge_reaped"])
        self.assertEqual(ack["types"], sorted(power.SETUP_TYPES))
        self.assertEqual(json.loads((evidence / "scenario-power-complete.json").read_text()),
                         dict(setup_reaped=True, passive_reaped=True))
        self.assert_clean(receipt)

    def test_failed_cleanup_retains_the_original_false_latch(self):
        def prepare(*args):
            self.bridge(*args)
            raise RuntimeError("prepare refused")
        with mock.patch.object(runner, "recover_renderer_faults", return_value=False):
            code, _, receipt = self.main(prepare)
        self.assertEqual(code, 3)
        self.assertFalse(receipt["cleanup_complete"])
        self.assertFalse(json.loads(self.lock.read_text())["cleanup_complete"])
        prepare = mock.Mock(side_effect=AssertionError("no second preparation"))
        code, _, second = self.main(prepare)
        self.assertEqual(code, 2)
        self.assertFalse(second["launched"])
        prepare.assert_not_called()

    def test_actual_main_sigterm_during_prepare_reaps_bridge_without_app(self):
        evidence = self.root / "signal-evidence"
        source = ("import importlib.util,json,signal,subprocess,sys;from pathlib import Path;"
                  "s=importlib.util.spec_from_file_location('actual_runner',sys.argv[1]);"
                  "r=importlib.util.module_from_spec(s);s.loader.exec_module(r);"
                  "r.require_guest=lambda:None;r.LOCK_PATH=Path(sys.argv[2])\n"
                  "def prepare(lock,receipt,env,processes,scope,deadline):\n"
                  " p=subprocess.Popen([sys.executable,'-c'," + repr(BRIDGE) + "],env=env,start_new_session=True);processes.append(p)\n"
                  " Path(env['ZWAI_NATIVE_EVIDENCE'],'prepared.json').write_text(json.dumps(dict(pid=p.pid)))\n"
                  " signal.pause()\n"
                  "raise SystemExit(r.main(sys.argv[3:],prepare=prepare))")
        process = subprocess.Popen([sys.executable, "-c", source, str(SCRIPT), str(self.lock),
                                    "--app", str(self.app), "--evidence", str(evidence),
                                    "--timeout", "3", "--budget", "8", "--", sys.executable, "-c", "pass"],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        try:
            end = time.monotonic() + 5
            while not (evidence / "bridge-ready.json").exists():
                if process.poll() is not None or time.monotonic() >= end:
                    self.fail("actual main never reached owned preparation")
                time.sleep(.01)
            process.send_signal(signal.SIGTERM)
            out, err = process.communicate(timeout=8)
            self.assertEqual(process.returncode, 130, (out, err))
            receipt = json.loads((evidence / "cleanup.json").read_text())
            self.assertFalse(receipt["launched"])
            self.assert_clean(receipt)
            owned = json.loads((evidence / "bridge-ready.json").read_text())
            for pid in owned.values():
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
        finally:
            if process.poll() is None:
                process.terminate()
                process.communicate(timeout=8)


if __name__ == "__main__":
    unittest.main()
