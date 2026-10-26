"""HOST-only preparation contract tests; OS leaves never call a GUI."""
import base64
import fcntl
import json
import os
from pathlib import Path
import plistlib
import select
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

import native_macos_prepare_login as login
import native_macos_case as runner


PNG = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l4sAAAAASUVORK5CYII=")
HERE = Path(__file__).resolve().parent
SESSION = dict(user="ci", uid=os.getuid(), on_console=True, login_done=True,
               audit_id=9, session_uuid="session")


class InertOS:
    def __init__(self, locked=None):
        self.locked, self.inputs, self.frames = locked, [], []

    def session(self, context, deadline):
        login.remaining(deadline)
        return dict(SESSION, lock_key_present=self.locked is not None, locked=self.locked)

    def capture(self, context, path, deadline):
        login.remaining(deadline)
        path.write_bytes(PNG)
        self.frames.append(path)

    def input(self, context, target, credential, deadline):
        login.remaining(deadline)
        self.inputs.append(credential)
        self.locked = None
        return dict(input_attempted=True, helper_exit=1)


class PrepareLoginTests(unittest.TestCase):
    def test_real_kernel_child_validates_shared_raw_clock_not_parent_monotonic_origin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image, ready = root / "frame.png", root / "ready.json"
            image.write_bytes(PNG)
            kernel = login.process_identity(os.getpid())["executable"]
            producer = ("import json,sys,time;from pathlib import Path;sys.path.insert(0,sys.argv[1]);"
                        "import native_macos_prepare_login as m;time.sleep(.1);"
                        "f=m.frame_record(Path(sys.argv[2]),time.monotonic(),time.time());"
                        "Path(sys.argv[3]).write_text(json.dumps(dict(frame=f,deadline_raw=m.raw_clock()+3)))")
            subprocess.run([kernel, "-B", "-c", producer, str(HERE), str(image), str(ready)],
                           check=True, timeout=2, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            consumer = ("import json,sys;from pathlib import Path;sys.path.insert(0,sys.argv[1]);"
                        "import native_macos_prepare_login as m;r=json.loads(Path(sys.argv[2]).read_text());"
                        "m.validate_remote_frame(r['frame'],r['deadline_raw']);print('SHARED_CLOCK_VALID')")
            result = subprocess.run([kernel, "-B", "-c", consumer, str(HERE), str(ready)],
                                    timeout=2, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(result.stdout, b"SHARED_CLOCK_VALID\n")

    def test_missing_lock_flag_stays_unknown(self):
        value = dict(user="ci", uid=501, on_console=True, login_done=True,
                     audit_id=9, session_uuid="session", lock_key_present=False,
                     locked=None)
        self.assertIsNone(login.session_lock(value))
        with self.assertRaises(RuntimeError):
            login.session_lock(dict(value, locked=False))

    def test_wrong_sdk_types_never_become_unlocked(self):
        for present, locked in ((1, False), (True, 0), (False, False), (True, None)):
            with self.subTest(present=present, locked=locked), self.assertRaises(RuntimeError):
                login.session_lock(dict(lock_key_present=present, locked=locked))
        self.assertFalse(login.session_lock(dict(lock_key_present=True, locked=False)))

    def test_remote_frame_refuses_original_raw_expiry_and_changed_wall(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "frame.png"
            path.write_bytes(PNG)
            frame = login.frame_record(path, time.monotonic(), time.time())
            login.validate_remote_frame(frame, login.raw_clock()+1)
            with self.assertRaises(RuntimeError):
                login.validate_remote_frame(frame, login.raw_clock()-1)
            with self.assertRaises(RuntimeError):
                login.validate_remote_frame(dict(frame, expires_raw=login.raw_clock()-1), login.raw_clock()+1)
            with self.assertRaises(RuntimeError):
                login.validate_remote_frame(dict(frame, captured_wall=time.time()-61), login.raw_clock()+1)

    def test_frame_original_expiry_and_bytes_cannot_be_renewed(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "frame.png"
            path.write_bytes(PNG)
            frame = login.frame_record(path, time.monotonic(), time.time())
            login.validate_frame(frame, time.monotonic()+1)
            for changed in (dict(frame, captured_monotonic=frame["captured_monotonic"]-61),
                            dict(frame, expires_wall=time.time()-1)):
                with self.assertRaises(RuntimeError):
                    login.validate_frame(changed, time.monotonic()+1)
            path.write_bytes(PNG+b"changed")
            with self.assertRaises(RuntimeError):
                login.validate_frame(frame, time.monotonic()+1)

    def test_ready_is_absent_until_complete_and_existing_file_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ready.json"
            dump = login.json.dump
            def observed(value, stream, **kwargs):
                stream.write('{"partially_written":')
                stream.flush()
                self.assertFalse(path.exists())
                stream.seek(0)
                stream.truncate()
                dump(value, stream, **kwargs)
            with mock.patch.object(login.json, "dump", side_effect=observed):
                login.write_new(path, dict(complete=True))
            self.assertEqual(json.loads(path.read_text()), dict(complete=True))
            original = path.read_bytes()
            with self.assertRaises(FileExistsError):
                login.write_new(path, dict(replaced=True))
            self.assertEqual(path.read_bytes(), original)
            self.assertEqual(list(Path(directory).iterdir()), [path])


class LoginCaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="case-login-headless-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "Inert.app"
        executable = self.app / "Contents/MacOS/inert"
        executable.parent.mkdir(parents=True)
        executable.write_text("#!"+sys.executable+"\nimport time\ntime.sleep(120)\n")
        executable.chmod(0o755)
        with (self.app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump(dict(CFBundleExecutable="inert"), stream)
        self.package = self.root / "fixture.zip"
        self.package.write_bytes(b"synthetic-package")
        self.evidence = self.root / "evidence"
        self.lock_path = self.root / "case.lock"
        self.binding = dict(execution_id="1"*32, launch_id="2"*32, source_sha="3"*40,
                            package_sha256=login.sha(self.package), phase="idle", session=SESSION)
        self.kernel = login.process_identity(os.getpid())["executable"]
        self.client = HERE / "native_macos_prepare_login_client.py"
        self.helper = HERE / "native_macos_prepare_login.swift"
        self.senders, self.sender_errors = [], []
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(runner, "LOCK_PATH", self.lock_path).start()
        mock.patch.object(runner, "require_guest", return_value=None).start()

    def tearDown(self):
        for child in self.senders:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=2)
            for stream in (child.stdin, child.stdout, child.stderr):
                if stream is not None and not stream.closed:
                    stream.close()

    def callback(self, ops, **changes):
        values = dict(helper_path=self.helper, helper_sha256=login.sha(self.helper),
                      client_path=self.client, client_sha256=login.sha(self.client),
                      source_sha256=login.sha(Path(login.__file__)), app_source=self.app,
                      package_path=self.package, client_kernel=self.kernel,
                      login_target=dict(width=1, height=1, password_x=0.0, password_y=0.0), ops=ops)
        values.update(changes)
        return login.make_before_app(self.binding, **values)

    def run_case(self, callback, *, change_context=None, before_publication=None, budget=5, cleanup=True):
        captured = {}
        def prepare(lock, receipt, environment, processes, scope, deadline):
            bound = dict(self.binding, case_id=receipt["case_id"], runner=login.process_identity(os.getpid()))
            environment["ZWAI_NATIVE_POWER_BINDING"] = json.dumps(bound, sort_keys=True)
            context = dict(lock=lock, receipt=receipt, environment=environment,
                           processes=processes, scope=scope, power_binding=bound)
            captured.update(context)
            if change_context:
                change_context(context)
            final = callback(context, deadline)
            if final is not None:
                def guarded():
                    if before_publication:
                        before_publication(context)
                    final()
                return runner.CasePreparation(guarded, lambda scenario, end: None)
        code = runner.main(["--app", str(self.app), "--evidence", str(self.evidence),
                            "--timeout", "1", "--budget", str(budget), "--",
                            self.kernel, "-c", "pass"], prepare=prepare)
        row = json.loads((self.evidence / "cleanup.json").read_text())
        if cleanup:
            self.assertTrue(row["cleanup_complete"])
            self.assertTrue(row["data_removed"])
            self.assertEqual(row["remaining_owned_pids"], [])
            self.assertFalse(Path(row["case_root"]).exists())
            self.assertTrue(all(child.poll() is not None for child in captured["processes"]))
        return code, row

    def start_sender(self, stages, *, transform=None, argv_extra=(), partial=False):
        def send():
            try:
                for stage in stages:
                    path = self.evidence / "prepare-login" / ("ready-"+stage+".json")
                    end = time.monotonic()+4
                    while not path.exists():
                        if time.monotonic() >= end:
                            raise RuntimeError("test actor did not publish frame")
                        time.sleep(.005)
                    ready = json.loads(path.read_text())
                    environment = dict(os.environ, ZWAI_NATIVE_CASE_ID=ready["binding"]["case_id"])
                    child = subprocess.Popen([self.kernel, "-B", str(self.client), str(path), login.sha(path), *argv_extra],
                                             env=environment, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                             stderr=subprocess.PIPE, start_new_session=True)
                    self.senders.append(child)
                    if not select.select([child.stdout], [], [], 2)[0] or child.stdout.readline() != b"READY\n":
                        raise RuntimeError("test client refused before RAM load")
                    if partial:
                        child.stdin.write(b'{"frame')
                        child.stdin.flush()
                        # Leave stdin open: both actual client and actor must obey
                        # the original deadline rather than waiting for EOF.
                        if not select.select([child.stdout], [], [], 2)[0]:
                            raise AssertionError("partial stdin ignored original deadline")
                        child.stdout.readline()
                        if child.wait(timeout=2) == 0:
                            raise AssertionError("partial stdin was accepted")
                        return
                    payload = dict(frame_sha256=ready["frame"]["sha256"],
                                   approval="ci-us-empty-password" if stage == "before" else "ci-desktop")
                    if stage == "before":
                        payload["credential"] = "syntheticsecret"
                    if transform:
                        transform(payload)
                    child.stdin.write(json.dumps(payload).encode())
                    child.stdin.close()
                    if child.stdout.readline() != b"DELIVERED\n" or child.wait(timeout=2) != 0:
                        raise RuntimeError("test client delivery refused")
            except BaseException as error:
                self.sender_errors.append(type(error).__name__)
        thread = threading.Thread(target=send, daemon=True)
        thread.start()
        return thread

    def test_unknown_uses_only_postframe_approval_and_actual_main_cleanup(self):
        operations = InertOS()
        thread = self.start_sender(["after"])
        code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(self.sender_errors, [])
        self.assertEqual(code, 0)
        self.assertTrue(row["launched"])
        self.assertEqual(operations.inputs, [])
        admitted = json.loads((self.evidence / "prepare-login/admitted.json").read_text())
        self.assertIsNone(admitted["sdk_locked"])
        self.assertFalse(admitted["input_attempted"])

    def test_locked_one_ram_input_then_missing_flag_needs_new_postframe(self):
        operations = InertOS(True)
        thread = self.start_sender(["before", "after"])
        code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(self.sender_errors, [])
        self.assertEqual(code, 0)
        self.assertTrue(row["launched"])
        self.assertEqual(operations.inputs, ["syntheticsecret"])
        self.assertEqual(len(operations.frames), 2)
        for path in (self.evidence / "prepare-login").iterdir():
            self.assertNotIn(b"syntheticsecret", path.read_bytes())

    def test_real_headless_helper_receives_single_stdin_and_is_registered_for_cleanup(self):
        native = login.NativeOperations(self.helper)
        class HeadlessInput(InertOS):
            def input(self, context, target, credential, deadline):
                result = native.input(context, target, credential, deadline)
                self.locked = None
                return result
        operations = HeadlessInput(True)
        original = login.spawn_owned
        captured = []
        def command_leaf(argv, context, **kwargs):
            self.assertEqual(argv[:3], ["/usr/bin/swift", str(self.helper), "input"])
            script = ("import json,sys;good=sys.stdin.buffer.read()==b'syntheticsecret';"
                      "print(json.dumps(dict(input_attempted=good,credential_recorded=False,unlocked=False)));"
                      "raise SystemExit(1 if good else 3)")
            child = original([self.kernel, "-c", script], context, **kwargs)
            captured.append(child)
            return child
        thread = self.start_sender(["before", "after"])
        with mock.patch.object(login, "spawn_owned", side_effect=command_leaf):
            code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(self.sender_errors, [])
        self.assertEqual(code, 0)
        self.assertTrue(row["launched"])
        self.assertEqual(len(captured), 1)
        self.assertEqual(captured[0].returncode, 1)
        result = json.loads((self.evidence / "prepare-login/input-result.json").read_text())
        self.assertEqual(result, dict(input_attempted=True, helper_exit=1))

    def test_bad_frame_approval_never_inputs_or_launches(self):
        operations = InertOS(True)
        thread = self.start_sender(["before"], transform=lambda row: row.update(frame_sha256="0"*64))
        code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.inputs, [])
        self.assertEqual(self.sender_errors, ["RuntimeError"])

    def test_wrong_phase_refuses_before_any_frame(self):
        operations = InertOS()
        code, row = self.run_case(self.callback(operations),
                                  change_context=lambda context: context["power_binding"].update(phase="sleepwake"))
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_expired_prepare_deadline_never_captures_or_launches(self):
        operations = InertOS()
        callback = self.callback(operations)
        code, row = self.run_case(lambda context, deadline: callback(context, time.monotonic()-1))
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_bad_session_refuses_before_any_frame(self):
        class WrongSession(InertOS):
            def session(self, context, deadline):
                return dict(super().session(context, deadline), session_uuid="other")
        operations = WrongSession()
        code, row = self.run_case(self.callback(operations))
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_bad_source_refuses_before_any_frame(self):
        operations = InertOS()
        code, row = self.run_case(self.callback(operations, helper_sha256="0"*64))
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_reused_runner_birth_refuses_before_any_frame(self):
        operations = InertOS()
        def changed(context):
            context["power_binding"]["runner"]["birth"][1] += 1
        code, row = self.run_case(self.callback(operations), change_context=changed)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_unheld_original_fd_refuses_in_actual_main(self):
        operations = InertOS()
        code, row = self.run_case(self.callback(operations),
                                  change_context=lambda context: fcntl.flock(context["lock"], fcntl.LOCK_UN))
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.frames, [])

    def test_wrong_sender_cli_never_gets_READY_or_loads_secret(self):
        operations = InertOS()
        thread = self.start_sender(["after"], argv_extra=("unexpected",))
        code, row = self.run_case(self.callback(operations), budget=1)
        thread.join(timeout=3)
        self.assertNotEqual(code, 0)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.inputs, [])
        self.assertEqual(self.sender_errors, ["RuntimeError"])

    def test_malformed_envelope_fails_without_retry(self):
        operations = InertOS(True)
        thread = self.start_sender(["before"], transform=lambda row: row.update(unapproved=True))
        code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.inputs, [])
        self.assertEqual(len(operations.frames), 1)

    def test_partial_stdin_is_bounded_without_loading_a_credential(self):
        operations = InertOS(True)
        thread = self.start_sender(["before"], partial=True)
        started = time.monotonic()
        code, row = self.run_case(self.callback(operations), budget=1)
        thread.join(timeout=3)
        self.assertLess(time.monotonic()-started, 3)
        self.assertFalse(thread.is_alive())
        self.assertEqual(self.sender_errors, [])
        self.assertNotEqual(code, 0)
        self.assertFalse(row["launched"])
        self.assertEqual(operations.inputs, [])

    def test_actual_peer_birth_change_refuses_before_READY(self):
        operations = InertOS()
        original = login.process_identity
        samples = {}
        def changed(pid):
            value = original(pid)
            if pid != os.getpid() and value is not None:
                samples[pid] = samples.get(pid, 0)+1
                if samples[pid] > 1:
                    value["birth"][1] += 1
            return value
        thread = self.start_sender(["after"])
        with mock.patch.object(login, "process_identity", side_effect=changed):
            code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertTrue(any(count > 1 for count in samples.values()))
        self.assertEqual(self.sender_errors, ["RuntimeError"])

    def test_cancellation_keeps_actual_owned_child_for_runner_cleanup(self):
        operations = InertOS()
        callback = self.callback(operations)
        captured = []
        def cancelling(context, deadline):
            child = login.spawn_owned([self.kernel, "-c", "import time;time.sleep(120)"], context,
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            captured.append(child)
            raise KeyboardInterrupt
        code, row = self.run_case(cancelling)
        self.assertEqual(code, 130)
        self.assertFalse(row["launched"])
        self.assertIsNotNone(captured[0].poll())

    def test_cleanup_failure_keeps_false_latch(self):
        operations = InertOS()
        with mock.patch.object(runner, "remove_case_tree", side_effect=OSError("synthetic cleanup refusal")):
            code, row = self.run_case(self.callback(operations),
                                     change_context=lambda context: context["power_binding"].update(phase="wrong"),
                                     cleanup=False)
        self.assertEqual(code, 3)
        self.assertFalse(row["launched"])
        self.assertFalse(row["cleanup_complete"])
        self.assertFalse(json.loads(self.lock_path.read_text())["cleanup_complete"])
        # This disposable fixture remains ours after the deliberate refusal.
        __import__("shutil").rmtree(row["case_root"])

    def test_locked_flag_during_final_gate_vetoes_approved_frame(self):
        class Relocking(InertOS):
            def __init__(self):
                super().__init__()
                self.calls = 0
            def session(self, context, deadline):
                self.calls += 1
                if self.calls >= 6:
                    self.locked = True
                return super().session(context, deadline)
        operations = Relocking()
        thread = self.start_sender(["after"])
        code, row = self.run_case(self.callback(operations))
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertFalse((self.evidence / "prepare-login/admitted.json").exists())

    def test_after_power_handoff_final_rawtrue_vetoes_actual_App_Popen(self):
        operations = InertOS()
        thread = self.start_sender(["after"])
        code, row = self.run_case(self.callback(operations),
                                  before_publication=lambda context: setattr(operations, "locked", True))
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertTrue((self.evidence / "prepare-login/approval-after.json").exists())
        self.assertFalse((self.evidence / "prepare-login/admitted.json").exists())

    def test_after_power_handoff_birth_change_vetoes_actual_App_Popen(self):
        operations = InertOS()
        thread = self.start_sender(["after"])
        def mutate(context):
            context["power_binding"]["runner"]["birth"][1] += 1
        code, row = self.run_case(self.callback(operations), before_publication=mutate)
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertFalse((self.evidence / "prepare-login/admitted.json").exists())

    def test_after_power_handoff_original_capture_expiry_vetoes_actual_App_Popen(self):
        operations = InertOS()
        callback = self.callback(operations)
        original = login.raw_clock
        def prepare(context, deadline):
            final = callback(context, deadline)
            def expired_final():
                with mock.patch.object(login, "raw_clock", return_value=original()+61):
                    final()
            return expired_final
        thread = self.start_sender(["after"])
        code, row = self.run_case(prepare)
        thread.join(timeout=3)
        self.assertEqual(code, 2)
        self.assertFalse(row["launched"])
        self.assertTrue((self.evidence / "prepare-login/approval-after.json").exists())
        self.assertFalse((self.evidence / "prepare-login/admitted.json").exists())


if __name__ == "__main__":
    unittest.main()
