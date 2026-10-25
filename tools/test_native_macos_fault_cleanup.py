"""Cleanup owns private case directories and any recorded renderer stall."""
import os
import json
from pathlib import Path
import plistlib
import select
import signal
import stat
import subprocess
import sys
import tempfile
import types
import time
import unittest
from unittest import mock

import native_macos_case as runner


class ReadOnlyCaseCleanupTests(unittest.TestCase):
    def test_readonly_module_cache_is_removed_without_following_external_links(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            root, external = base / "case", base / "outside"
            cache = root / "home" / "pkg" / "mod" / "module@version"
            cache.mkdir(parents=True)
            external.mkdir()
            (external / "keep").write_text("unrelated")
            original_mode = stat.S_IMODE(external.stat().st_mode)
            (cache / "source.go").write_text("package fixture")
            (root / "outside-link").symlink_to(external, target_is_directory=True)
            for directory in (cache, cache.parent, cache.parent.parent, root / "home"):
                directory.chmod(0o500)
            runner.remove_case_tree(root)
            self.assertFalse(root.exists())
            self.assertEqual((external / "keep").read_text(), "unrelated")
            self.assertEqual(stat.S_IMODE(external.stat().st_mode), original_mode)

    def test_a_symlink_cannot_replace_the_private_case_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            external = base / "outside"
            external.mkdir()
            (external / "keep").write_text("unrelated")
            root = base / "case"
            root.symlink_to(external, target_is_directory=True)
            with self.assertRaises(RuntimeError):
                runner.remove_case_tree(root)
            self.assertEqual((external / "keep").read_text(), "unrelated")
            self.assertTrue(root.is_symlink())


class RendererCleanupHookTests(unittest.TestCase):
    def run_case(self, root, cleanup, filename="renderer-fault-targets.jsonl"):
        app = root / "Original.app"
        (app / "Contents/MacOS").mkdir(parents=True)
        (app / "Contents/Info.plist").write_bytes(plistlib.dumps(dict(CFBundleExecutable="fixture")))
        executable = app / "Contents/MacOS/fixture"
        executable.write_text("#!" + sys.executable + "\nimport time\ntime.sleep(120)\n")
        executable.chmod(0o755)
        evidence = root / "evidence"
        scenario = ("import os;from pathlib import Path;"
                    "Path(os.environ['ZWAI_NATIVE_EVIDENCE']," + repr(filename) + ").write_text('test')")
        module = types.SimpleNamespace(cleanup_case=cleanup)
        with mock.patch.object(runner, "LOCK_PATH", root / "lock"), \
                mock.patch.object(runner, "require_guest"), \
                mock.patch.dict(sys.modules, native_macos_resilience_fault=module):
            code = runner.run_case(app, evidence, 10, [sys.executable, "-c", scenario])
        receipt = json.loads((evidence / "cleanup.json").read_text())
        case = Path(receipt["case_root"])
        self.addCleanup(lambda: runner.remove_case_tree(case) if case.exists() else None)
        return code, receipt, root / "lock"

    def test_recorded_stalls_are_recovered_before_and_after_owned_process_exit(self):
        with tempfile.TemporaryDirectory() as directory:
            calls = []
            def cleanup(evidence, case_id, app, *, require_exit=False):
                self.assertEqual(case_id, json.loads((Path(directory) / "lock").read_text())["case_id"])
                self.assertIn(case_id, str(app))
                calls.append(require_exit)
                return dict(complete=True, targets=[])
            code, receipt, _ = self.run_case(Path(directory), cleanup)
            self.assertEqual(code, 0)
            self.assertEqual(len(calls), 2)
            self.assertEqual(calls, [False, True])
            self.assertEqual(len(receipt["renderer_fault_cleanup"]), 2)
            self.assertTrue(receipt["cleanup_complete"])
            self.assertTrue(receipt["data_removed"])


    def test_failed_stall_recovery_stops_processes_but_keeps_data_and_refusal_latch(self):
        with tempfile.TemporaryDirectory() as directory:
            def failed(*_, **__):
                raise RuntimeError("still stopped")
            code, receipt, lock = self.run_case(Path(directory), failed)
            self.assertEqual(code, 3)
            self.assertFalse(receipt["cleanup_complete"])
            self.assertFalse(receipt["data_removed"])
            self.assertEqual(receipt["remaining_owned_pids"], [])
            self.assertIn("still stopped", receipt["renderer_fault_cleanup_error"])
            self.assertFalse(json.loads(lock.read_text())["cleanup_complete"])
            runner.remove_case_tree(Path(receipt["case_root"]))

    def test_unconfirmed_cleanup_cannot_accept_a_case(self):
        for result in (None, {}, {"complete": False}):
            with self.subTest(result=result), tempfile.TemporaryDirectory() as directory:
                code, receipt, _ = self.run_case(Path(directory), lambda *_, **__: result)
                self.assertEqual(code, 3)
                self.assertFalse(receipt["cleanup_complete"])
                self.assertEqual(receipt["remaining_owned_pids"], [])
                runner.remove_case_tree(Path(receipt["case_root"]))

    def test_permission_only_receipts_also_require_confirmed_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            cleanup = mock.Mock(return_value=dict(complete=True, targets=[], copies=[]))
            code, receipt, _ = self.run_case(Path(directory), cleanup, "renderer-fault-copies.jsonl")
            self.assertEqual(code, 0)
            self.assertEqual(cleanup.call_count, 2)
            self.assertTrue(cleanup.call_args.kwargs["require_exit"])
            self.assertTrue(receipt["data_removed"])


class KilledScenarioCleanupTests(unittest.TestCase):
    def test_killed_scenario_cannot_leave_its_recorded_target_stopped(self):
        import native_macos_resilience_fault as fault
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            app = root / "zwai.app"
            executable = app / "Contents/MacOS/zwai"
            executable.parent.mkdir(parents=True)
            (app / "Contents/Info.plist").write_bytes(plistlib.dumps(dict(CFBundleExecutable="zwai")))
            source = ("#include <signal.h>\n#include <string.h>\n#include <unistd.h>\n"
                      "static void finish(int value){(void)value;_exit(0);}\n"
                      "int main(int n,char **v){if(n>1&&!strcmp(v[1],\"fault-target\"))"
                      "{signal(SIGCONT,finish);write(1,\"ready\\n\",6);}for(;;)pause();}\n")
            subprocess.run(["/usr/bin/cc", "-x", "c", "-o", str(executable), "-"],
                           input=source, text=True, capture_output=True, check=True, timeout=30)
            evidence = root / "evidence"
            scenario = ("import os,json,signal,time;from pathlib import Path;"
                        "signal.signal(signal.SIGTERM,signal.SIG_IGN);"
                        "signal.signal(signal.SIGINT,signal.SIG_IGN);"
                        "Path(os.environ['ZWAI_NATIVE_EVIDENCE'],'qa-ready.json').write_text(json.dumps("
                        "dict(case_id=os.environ['ZWAI_NATIVE_CASE_ID'],app=os.environ['ZWAI_NATIVE_APP'],"
                        "shell=int(os.environ['ZWAI_NATIVE_APP_PID']))));time.sleep(120)")
            targets, killed = [], []
            def kill_after_durable_stall(process, _timeout):
                until = time.monotonic() + 5
                while not (evidence / "qa-ready.json").is_file():
                    if time.monotonic() >= until:
                        self.fail("private QA child did not publish its identity")
                    time.sleep(.01)
                row = json.loads((evidence / "qa-ready.json").read_text())
                environment = {key: value for key, value in os.environ.items()
                               if not key.startswith("ZWAI_NATIVE_")}
                child = subprocess.Popen([str(executable), "fault-target"], env=environment,
                                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                         start_new_session=True)
                targets.append(child)
                if not select.select([child.stdout], [], [], 3)[0]:
                    self.fail("private target did not publish its startup receipt")
                self.assertEqual(child.stdout.read(6), b"ready\n", child.poll())
                child.stdout.close()
                target = fault.process_identity(child.pid)
                shell = fault.process_identity(row["shell"])
                record = dict(schema=1, case_id=row["case_id"], private_app=row["app"],
                              kind="renderer", target=target, shell=shell, recorded_at=time.time())
                with (evidence / fault.TARGETS).open("w") as stream:
                    stream.write(json.dumps(record) + "\n")
                    stream.flush()
                    os.fsync(stream.fileno())
                os.kill(child.pid, signal.SIGSTOP)
                until = time.monotonic() + 2
                while not (fault._state(child.pid) or "").startswith("T"):
                    if time.monotonic() >= until:
                        self.fail("private target never entered the actual stopped state")
                    time.sleep(.01)
                process.kill()
                killed.append(process.wait(timeout=3))
                raise subprocess.TimeoutExpired("killed private QA child", .1)
            try:
                with mock.patch.object(runner, "LOCK_PATH", root / "lock"), \
                        mock.patch.object(runner, "require_guest"), \
                        mock.patch.object(runner, "wait_scenario", side_effect=kill_after_durable_stall), \
                        mock.patch.object(fault, "require_guest"), \
                        mock.patch.object(fault, "_service_path", side_effect=lambda _kind, path: path == str(executable)):
                    code = runner.run_case(app, evidence, 10, [sys.executable, "-c", scenario])
                receipt = json.loads((evidence / "cleanup.json").read_text())
                self.assertEqual(killed, [-signal.SIGKILL])
                self.assertEqual(code, 2, receipt)  # The injected child failure is not a scenario deadline.
                self.assertEqual(receipt["failure_kind"], "subprocess_timeout")
                self.assertEqual(receipt["timeouts"][0]["command"], "killed private QA child")
                self.assertEqual(targets[0].wait(timeout=2), 0)
                self.assertTrue(receipt["cleanup_complete"], receipt)
                self.assertTrue(receipt["data_removed"])
                self.assertEqual(receipt["remaining_owned_pids"], [])
                self.assertEqual(len(receipt["renderer_fault_cleanup"]), 2)
                self.assertTrue(receipt["renderer_fault_cleanup"][-1]["require_exit"])
            finally:
                for child in targets:
                    if child.poll() is None:
                        os.kill(child.pid, signal.SIGCONT)
                        child.terminate()
                        child.wait(timeout=3)
                    if child.stdout:
                        child.stdout.close()
                if (evidence / "cleanup.json").is_file():
                    case = Path(json.loads((evidence / "cleanup.json").read_text())["case_root"])
                    if case.exists():
                        runner.remove_case_tree(case)


if __name__ == "__main__":
    unittest.main()
