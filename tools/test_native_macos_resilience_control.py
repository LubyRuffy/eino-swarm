"""Ownership and cancellation of a detached long-running native campaign."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import os
import sys
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("control", Path(__file__).with_name("native_macos_resilience_control.py"))
control = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(control)


class ControlTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.process = dict(pid=123, birth_time="Thu Oct 22 12:00:00 2026", command_sha256="unique launch argv")

    def test_fingerprint_binds_actual_helper_modules_package_and_phase_budgets(self):
        fields = {"native_macos_resilience.py": "harness_sha256", "native_macos_resilience_ax.m": "probe_source_sha256",
                  "native_macos_case.py": "runner_sha256", "native_macos_resilience_control.py": "control_sha256"}
        digest = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
        manifest = dict(dependencies={}, phases=[dict(name="baseline")], selection=["baseline"], budgets={"baseline": 240}, probe=None)
        for name, field in fields.items():
            path = self.root / name; path.write_text(name); manifest[field] = digest(path)
        self.assertIn("native_macos_input_evidence.py", control.DEPENDENCIES)
        for name in control.DEPENDENCIES:
            path = self.root / name; path.write_text(name); manifest["dependencies"][name] = digest(path)
        for name in ("helper", "package.zip", "executable"):
            (self.root / name).write_text(name)
        manifest.update(helper=str(self.root / "helper"), helper_sha256=digest(self.root / "helper"),
            package=dict(zip=str(self.root / "package.zip"), zip_sha256=digest(self.root / "package.zip"),
                         executable=str(self.root / "executable"), executable_sha256=digest(self.root / "executable"),
                         source_sha="a" * 40, version="0.1.36"))
        before = control.input_fingerprint(manifest, self.root, digest)
        self.assertEqual(len(before), 64)
        for name in manifest["dependencies"]:
            with self.subTest(missing=name), self.assertRaisesRegex(RuntimeError, "inputs"):
                control.input_fingerprint(dict(manifest, dependencies={key:value for key,value in
                    manifest["dependencies"].items() if key != name}), self.root, digest)
        changed = dict(manifest, budgets={"baseline": 60})
        self.assertNotEqual(before, control.input_fingerprint(changed, self.root, digest))
        evidence = self.root / "native_macos_input_evidence.py"
        evidence.write_text("changed accepted-input evidence validation")
        with self.assertRaisesRegex(RuntimeError, "inputs"):
            control.input_fingerprint(manifest, self.root, digest)
        evidence.write_text(evidence.name)
        (self.root / "native_macos_resilience_worker.py").write_text("changed HTTP behavior")
        with self.assertRaisesRegex(RuntimeError, "inputs"):
            control.input_fingerprint(manifest, self.root, digest)

    def test_launch_detaches_and_never_repeats_an_uncertain_attempt(self):
        args = argparse.Namespace(evidence=self.root, zip=self.root / "candidate.zip", source="a" * 40,
                                  soak_seconds=64800, sleep_cycles=3, interval=60, stage_timeout=180)
        with mock.patch.object(control.subprocess, "Popen") as start, mock.patch.object(control, "identity", return_value=self.process):
            start.return_value.pid = 123
            row = control.launch(args, Path("/guest/resilience.py"))
            self.assertTrue(start.call_args.kwargs["start_new_session"])
            self.assertEqual(start.call_args.args[0][2], "run")
            self.assertIn(row["launch_id"], start.call_args.args[0])
            self.assertEqual(row["process"], self.process)
            argv = start.call_args.args[0]
            self.assertEqual(argv[argv.index("--phases") + 1:argv.index("--launch-id")],
                             ["baseline", "renderer", "gpu", "exhaustion", "sleepwake", "soak"])
            with self.assertRaises(FileExistsError):
                control.launch(args, Path("/guest/resilience.py"))
            self.assertEqual(start.call_count, 1)

    def test_cancel_signals_only_same_birth_and_command_then_requires_cleanup(self):
        control.write_json(self.root / "owner.json", dict(launch_id="mine", process=self.process))
        control.write_json(self.root / "driver-exit.json", dict(launch_id="mine", cleanup_complete=True, exit_code=130))
        with mock.patch.object(control, "identity", return_value=self.process), mock.patch.object(control.os, "kill") as kill:
            receipt = control.cancel(self.root)
            kill.assert_called_once_with(123, signal.SIGTERM)
            self.assertTrue(receipt["cleanup_complete"])
        control.write_json(self.root / "driver-exit.json", dict(launch_id="mine", cleanup_complete=False))
        with mock.patch.object(control, "identity", return_value=None):
            with self.assertRaisesRegex(RuntimeError, "cleanup is pending"):
                control.cancel(self.root)

    def test_reused_pid_never_receives_campaign_term(self):
        control.write_json(self.root / "owner.json", dict(launch_id="mine", process=self.process))
        with mock.patch.object(control, "identity", return_value=dict(self.process, birth_time="later")), \
                mock.patch.object(control.os, "kill") as kill:
            with self.assertRaisesRegex(RuntimeError, "identity changed"):
                control.cancel(self.root)
            kill.assert_not_called()

    def test_cancel_file_is_session_bound_and_interrupts_long_wait(self):
        control.write_json(self.root / "cancel.request.json", dict(launch_id="old"))
        process = mock.Mock()
        process.wait.return_value = 0
        self.assertEqual(control.wait_process(process, 64800, self.root, "new"), 0)
        control.write_json(self.root / "cancel.request.json", dict(launch_id="new"))
        process.reset_mock()
        with self.assertRaises(KeyboardInterrupt):
            control.wait_process(process, 64800, self.root, "new")
        process.wait.assert_not_called()

    def test_wait_is_bounded_even_if_runner_never_exits(self):
        process = mock.Mock()
        process.wait.side_effect = subprocess.TimeoutExpired("runner", 1)
        with mock.patch.object(control.time, "monotonic", side_effect=[0, .5, 2]):
            with self.assertRaises(subprocess.TimeoutExpired):
                control.wait_process(process, 1, self.root, "mine")

    def test_driver_exit_does_not_lie_about_missing_case_cleanup(self):
        claim = self.root / "renderer-claim"
        claim.mkdir()
        control.write_json(claim / "claim.json", dict(phase="renderer"))
        args = argparse.Namespace(evidence=self.root, launch_id="mine")
        with mock.patch.object(control, "identity", return_value=self.process):
            with self.assertRaises(KeyboardInterrupt):
                control.execute(args, mock.Mock(side_effect=KeyboardInterrupt))
        row = json.loads((self.root / "driver-exit.json").read_text())
        self.assertEqual(row["exit_code"], 130)
        self.assertFalse(row["cleanup_complete"])

    def test_retired_fault_receipts_cannot_confirm_current_campaign_cleanup(self):
        claim = self.root / "gpu-claim"
        claim.mkdir()
        control.write_json(claim / "claim.json", {})
        case = self.root / "gpu"
        case.mkdir()
        control.write_json(case / "cleanup.json", dict(cleanup_complete=True, data_removed=True, remaining_owned_pids=[]))
        control.write_json(case / "fault-target.json", dict(target=self.process))
        self.assertFalse(control.cleanup_complete(self.root))
        control.write_json(case / "fault-target-outer-cleanup.json", dict(target=self.process, exited=True))
        self.assertFalse(control.cleanup_complete(self.root))

    def test_all_six_current_cases_can_confirm_cleanup_without_fabricating_behavior_success(self):
        for name in ("baseline", "renderer", "gpu", "exhaustion", "sleepwake", "soak"):
            claim = self.root / (name + "-claim")
            claim.mkdir()
            control.write_json(claim / "claim.json", dict(phase=name))
            case = self.root / name
            case.mkdir()
            control.write_json(case / "cleanup.json", dict(case_id="current-case", cleanup_complete=True,
                data_removed=True, remaining_owned_pids=[], scenario_exit=124, exit_code=124))
        self.assertTrue(control.cleanup_complete(self.root))

    def test_recorded_faults_need_both_confirmed_cleanup_boundaries_from_the_same_case(self):
        claim = self.root / "renderer-claim"
        claim.mkdir()
        control.write_json(claim / "claim.json", {})
        case = self.root / "renderer"
        case.mkdir()
        (case / "renderer-fault-targets.jsonl").write_text("recorded target\n")
        row = dict(case_id="current-case", app_copy="/private/owned/zwai.app", cleanup_complete=True,
                   data_removed=True, remaining_owned_pids=[])
        control.write_json(case / "cleanup.json", row)
        self.assertFalse(control.cleanup_complete(self.root))
        checks = [dict(case_id=row["case_id"], private_app=row["app_copy"], complete=True, require_exit=flag)
                  for flag in (False, True)]
        for changed in (checks[:1], [checks[0], dict(checks[1], complete=False)],
                        [checks[0], dict(checks[1], case_id="another-case")],
                        [checks[0], dict(checks[1], require_exit=False)]):
            control.write_json(case / "cleanup.json", dict(row, renderer_fault_cleanup=changed))
            self.assertFalse(control.cleanup_complete(self.root))
        control.write_json(case / "cleanup.json", dict(row, renderer_fault_cleanup=checks))
        self.assertTrue(control.cleanup_complete(self.root))

    def test_real_detached_child_survives_launcher_and_cancel_waits_for_receipt(self):
        script = self.root / "detached-fixture.py"
        script.write_text("import json,pathlib,signal,sys,time\n"
                          "root=pathlib.Path(sys.argv[sys.argv.index('--evidence')+1])\n"
                          "launch=sys.argv[sys.argv.index('--launch-id')+1]\n"
                          "def stop(*args):\n"
                          " (root/'driver-exit.json').write_text(json.dumps(dict(launch_id=launch,cleanup_complete=True,exit_code=130)))\n"
                          " sys.exit(130)\n"
                          "signal.signal(signal.SIGTERM,stop)\n(root/'fixture.ready').touch()\nwhile True: time.sleep(.01)\n")
        args = argparse.Namespace(evidence=self.root, zip=self.root / "candidate.zip", source="a" * 40,
                                  soak_seconds=64800, sleep_cycles=3, interval=60, stage_timeout=180)
        code = "import sys,argparse;from pathlib import Path;sys.path.insert(0," + repr(str(SPEC.origin and Path(SPEC.origin).parent)) + ")\n"
        code += "import native_macos_resilience_control as c\n"
        code += "c.launch(argparse.Namespace(evidence=Path(" + repr(str(self.root)) + "),zip=Path('/unused.zip'),source='" + "a" * 40 + "',soak_seconds=64800,sleep_cycles=3,interval=60,stage_timeout=180),Path(" + repr(str(script)) + "))\n"
        subprocess.run([sys.executable, "-Wignore::ResourceWarning", "-c", code], check=True, capture_output=True, timeout=10)
        row = json.loads((self.root / "launch.json").read_text())
        pid = row["process"]["pid"]
        try:
            deadline = time.monotonic() + 5
            while not (self.root / "fixture.ready").exists() and time.monotonic() < deadline:
                time.sleep(.01)
            self.assertTrue((self.root / "fixture.ready").exists())
            self.assertEqual(os.getsid(pid), pid)
            self.assertEqual(control.identity(pid), row["process"])
            self.assertTrue(control.cancel(self.root, timeout=5)["cleanup_complete"])
        finally:
            if control.identity(pid) == row["process"]:
                os.kill(pid, signal.SIGTERM)


if __name__ == "__main__":
    unittest.main()
