"""Small real lock/process fixtures; never occupy the actual project's lease."""
import fcntl
import json
import os
from pathlib import Path
import select
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock
import uuid

sys.path.insert(0, str(Path(__file__).parent))
import release_host_lease as lease_module


def clear_inventory(*_):
    return {"status": "clear", "processes": []}


class HostValidationLeaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="host-lease-fixture-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name).resolve()
        self.module = "example.invalid/lease-fixture-" + uuid.uuid4().hex
        self.first = self.project("copy-one")
        self.second = self.project("copy-two")
        self.locks = self.directory / "locks"
        self.locks.mkdir(mode=0o700)
        self.tools = Path(__file__).resolve().parent

    def project(self, name, module=None):
        root = self.directory / name
        root.mkdir()
        (root / "go.mod").write_text("module " + (module or self.module) + "\n\ngo 1.26.0\n")
        return root

    def lease(self, root=None, evidence=None, **kwargs):
        return lease_module.HostValidationLease(
            root or self.first, evidence or self.directory / ("evidence-" + uuid.uuid4().hex),
            lock_root=self.locks, inventory=kwargs.pop("inventory", clear_inventory), **kwargs)

    def retained(self, evidence):
        return [json.loads(path.read_text()) for path in evidence.glob("host-lease-*.json")]

    def finish(self, process, *, input=None):
        try:
            stdout, stderr = process.communicate(input=input, timeout=3)
        except subprocess.TimeoutExpired:
            # This Popen is a fixture we directly own, never an observed process.
            process.terminate()
            stdout, stderr = process.communicate(timeout=2)
            self.fail("owned fixture did not finish: " + stderr[-500:])
        return stdout, stderr

    def start_holder(self):
        code = (
            "import sys; from pathlib import Path; sys.path.insert(0,sys.argv[1]); "
            "from release_host_lease import HostValidationLease; "
            "scope=HostValidationLease(Path(sys.argv[2]),Path(sys.argv[3]),lock_root=Path(sys.argv[4]),"
            "inventory=lambda *_:{'status':'clear','processes':[]}); "
            "scope.__enter__(); print('held',flush=True); sys.stdin.read(1); scope.__exit__(None,None,None)"
        )
        process = subprocess.Popen([sys.executable, "-B", "-c", code, str(self.tools),
                                    str(self.first), str(self.directory / "holder-evidence"), str(self.locks)],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.stop_fixture, process)
        ready, _, _ = select.select([process.stdout], [], [], 3)
        self.assertTrue(ready, "owned holder did not publish readiness")
        line = process.stdout.readline().strip()
        if line != "held":
            self.fail("owned holder refused: " + process.stderr.read()[-500:])
        return process

    @staticmethod
    def stop_fixture(process):
        if process.poll() is None:
            process.terminate()
        try:
            process.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate(timeout=2)

    def test_copies_share_uid_module_lock_and_normal_release_preserves_inode(self):
        first, second = self.lease(), self.lease(self.second)
        self.assertEqual(lease_module.canonical_module(self.first), self.module)
        self.assertEqual(first.lock_path, second.lock_path)
        with first:
            original = first.lock_path.stat().st_ino
            self.assertEqual(stat.S_IMODE(first.lock_path.stat().st_mode), 0o600)
            with first.lock_path.open("r+") as competing:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(competing, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with second:
            self.assertEqual(second.lock_path.stat().st_ino, original)
        self.assertEqual(first.lock_path.stat().st_ino, original)
        different = self.project("other-project", self.module + "-other")
        self.assertNotEqual(first.lock_path, self.lease(different).lock_path)

    def test_real_process_holder_blocks_another_copy_until_owner_exits(self):
        holder = self.start_holder()
        evidence = self.directory / "waiter-evidence"
        with self.assertRaises(lease_module.HostLeaseBlocked):
            with self.lease(self.second, evidence, timeout=.12):
                self.fail("cross-copy validation overlapped a real holder")
        self.assertIsNone(holder.poll())
        self.assertTrue(self.retained(evidence))
        self.finish(holder, input="x")
        self.assertEqual(holder.returncode, 0)
        with self.lease(self.second, timeout=.3):
            pass

    def test_cancelled_wait_does_not_signal_owner_and_allows_later_admission(self):
        holder = self.start_holder()
        event = threading.Event()
        timer = threading.Timer(.08, event.set)
        timer.start()
        self.addCleanup(timer.cancel)
        evidence = self.directory / "cancelled-wait"
        with self.assertRaises(InterruptedError):
            with self.lease(self.second, evidence, timeout=2, cancel=event):
                self.fail("cancelled waiter entered")
        self.assertIsNone(holder.poll())
        rows = self.retained(evidence)
        self.assertTrue(any(row.get("status") == "cancelled" for row in rows), rows)
        self.finish(holder, input="x")
        self.assertEqual(holder.returncode, 0)
        with self.lease(self.second, timeout=.3):
            pass

    def test_stale_released_receipt_never_overrides_current_active_or_unknown_observation(self):
        for status in ("blocked", "unknown"):
            with self.subTest(status=status):
                evidence = self.directory / ("guard-" + status)
                scope = self.lease(evidence=evidence, timeout=.08,
                                   inventory=lambda *_: {"status": status, "processes": [{"pid": 123, "kind": "go_test"}]})
                scope.lock_path.write_text(json.dumps({"status": "released", "pid": 999999999}))
                with self.assertRaises(lease_module.HostLeaseBlocked):
                    with scope:
                        self.fail("historical receipt replaced a current process observation")
                rows = self.retained(evidence)
                self.assertTrue(rows)
                self.assertFalse(any(row.get("status") == "admitted" for row in rows))

    def test_evidence_is_private_and_does_not_serialize_argv_or_environment(self):
        evidence = self.directory / "private-evidence"
        secret = "fixture-credential-not-an-argument"
        with mock.patch.dict(os.environ, PRIVATE_LEASE_FIXTURE_SECRET=secret):
            with self.lease(evidence=evidence):
                pass
        for path in evidence.glob("host-lease-*.json"):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            text = path.read_text()
            self.assertNotIn(secret, text)
            self.assertNotIn("argv", text)
            self.assertNotIn("PRIVATE_LEASE_FIXTURE_SECRET", text)

    def test_same_module_real_unittest_discovery_is_detected_from_another_copy(self):
        tests = self.first / "owned_tests"
        tests.mkdir()
        (tests / "test_owned.py").write_text(
            "import time, unittest\nfrom pathlib import Path\n"
            "class OwnedFixture(unittest.TestCase):\n"
            " def test_wait(self):\n"
            "  Path('fixture-ready').touch()\n"
            "  end=time.monotonic()+15\n"
            "  while not Path('fixture-release').exists() and time.monotonic()<end: time.sleep(.01)\n"
        )
        process = subprocess.Popen([sys.executable, "-B", "-m", "unittest", "discover", "-s", "owned_tests"],
                                   cwd=self.first, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.stop_fixture, process)
        deadline = time.monotonic() + 3
        while not (self.first / "fixture-ready").exists() and time.monotonic() < deadline:
            if process.poll() is not None:
                self.fail("owned unittest fixture exited before observation")
            time.sleep(.01)
        self.assertTrue((self.first / "fixture-ready").exists())
        observed = lease_module.active_validations(self.second, self.module)
        self.assertIn(observed["status"], ("blocked", "unknown"))
        self.assertIn(process.pid, {row.get("pid") for row in observed["processes"]}, observed)
        serialized = json.dumps(observed)
        self.assertNotIn("argv", serialized)
        self.assertNotIn("-m unittest", serialized)
        evidence = self.directory / "real-active-stale-receipt"
        guarded = self.lease(self.second, evidence, timeout=2, inventory=None)
        # A released historical owner row cannot accept the current process.
        guarded.lock_path.write_text(json.dumps({"status": "released", "pid": 999999999}))
        with self.assertRaises(lease_module.HostLeaseBlocked):
            with guarded:
                self.fail("stale lease history admitted a live same-project suite")
        rows = self.retained(evidence)
        self.assertTrue(any(process.pid == item.get("pid")
                            for row in rows for observation in row.get("observations", [])
                            for item in observation.get("processes", [])), rows)
        self.assertIsNone(process.poll(), "the guard must not signal the observed validation")
        (self.first / "fixture-release").touch()
        self.finish(process)
        self.assertEqual(process.returncode, 0)

    def test_sampler_binds_actual_pid_birth_cwd_and_module_without_retaining_tokens(self):
        (self.first / "tools").mkdir()
        identity = {"pid": 77777, "birth": [123, 4], "executable": "/private/fixture/python3"}
        listing = subprocess.CompletedProcess([], 0, "77777 " + str(os.getuid()) + " /private/fixture/python3\n", "")
        with mock.patch.object(lease_module.subprocess, "run", return_value=listing), \
             mock.patch.object(lease_module, "process_identity", return_value=identity), \
             mock.patch.object(lease_module, "process_arguments", return_value=([
                 "/private/fixture/python3", "-m", "unittest", "discover", "-s", "tools"], None)), \
             mock.patch.object(lease_module, "process_cwd", return_value=self.first):
            observed = lease_module.active_validations(self.second, self.module)
        self.assertEqual(observed["status"], "blocked")
        row, = observed["processes"]
        self.assertEqual(row["pid"], identity["pid"])
        self.assertEqual(row["birth"], identity["birth"])
        self.assertEqual(Path(row["cwd"]), self.first)
        self.assertEqual(Path(row["project_root"]), self.first)
        self.assertNotIn("argv", json.dumps(observed))

    def test_known_directory_switches_bind_the_effective_project_copy(self):
        cases = [
            (["/usr/bin/go", "-C", str(self.first), "test", "./..."], self.first),
            (["/usr/bin/go", "-C=" + str(self.first), "test", "./..."], self.first),
            (["/usr/bin/make", "--directory=" + str(self.first), "check"], self.first),
            (["/usr/bin/make", "-C" + str(self.first.parent), "-Ccopy-one", "check"], self.first),
            (["/usr/bin/node", "/private/npm-cli.js", "--prefix", str(self.first), "test"], self.first),
        ]
        for argv, expected in cases:
            with self.subTest(tool=Path(argv[0]).name):
                self.assertIsNotNone(lease_module.validation_kind(argv))
                self.assertEqual(lease_module.effective_directory(argv, self.second), expected)
        (self.second / "go.mod").write_text('module "' + self.module + '"\n')
        self.assertEqual(lease_module.canonical_module(self.second), self.module)

    def test_process_reuse_cannot_exclude_a_foreign_cwd_as_current_evidence(self):
        foreign = self.project("foreign-cwd", self.module + "-other")
        before = {"pid": 77777, "birth": [123, 4], "executable": "/private/fixture/python3"}
        after = dict(before, birth=[124, 5])
        listing = subprocess.CompletedProcess([], 0, "77777 " + str(os.getuid()) + " /private/fixture/python3\n", "")
        with mock.patch.object(lease_module.subprocess, "run", return_value=listing), \
             mock.patch.object(lease_module, "process_identity", side_effect=[before, after, after]), \
             mock.patch.object(lease_module, "process_arguments", return_value=([
                 "/private/fixture/python3", "-m", "unittest", "discover"], None)), \
             mock.patch.object(lease_module, "process_cwd", return_value=foreign):
            observed = lease_module.active_validations(self.second, self.module)
        self.assertEqual(observed["status"], "unknown")

    def test_known_foreign_module_can_be_excluded_without_claiming_it_belongs_here(self):
        foreign = self.project("known-foreign", self.module + "-other")
        identity = {"pid": 77777, "birth": [123, 4], "executable": "/private/fixture/go"}
        listing = subprocess.CompletedProcess([], 0, "77777 " + str(os.getuid()) + " /private/fixture/go\n", "")
        with mock.patch.object(lease_module.subprocess, "run", return_value=listing), \
             mock.patch.object(lease_module, "process_identity", return_value=identity), \
             mock.patch.object(lease_module, "process_arguments", return_value=(["/private/fixture/go", "test", "-race", "./..."], None)), \
             mock.patch.object(lease_module, "process_cwd", return_value=foreign):
            observed = lease_module.active_validations(self.second, self.module)
        self.assertEqual(observed, {"status": "clear", "processes": []})

    def test_external_config_cannot_erase_a_same_project_kernel_working_directory(self):
        foreign = self.project("external-config", self.module + "-other")
        config = foreign / "vitest.config.js"
        config.write_text("export default {}")
        identity = {"pid": 77777, "birth": [123, 4], "executable": "/private/fixture/node"}
        listing = subprocess.CompletedProcess([], 0, "77777 /private/fixture/node\n", "")
        with mock.patch.object(lease_module.subprocess, "run", return_value=listing), \
             mock.patch.object(lease_module, "process_identity", return_value=identity), \
             mock.patch.object(lease_module, "process_arguments", return_value=([
                 "/private/fixture/node", "/private/fixture/vitest.mjs", "run", "--config", str(config)], None)), \
             mock.patch.object(lease_module, "process_cwd", return_value=self.first):
            observed = lease_module.active_validations(self.second, self.module)
        self.assertEqual(observed["status"], "blocked")
        self.assertEqual(Path(observed["processes"][0]["project_root"]), self.first)


if __name__ == "__main__":
    unittest.main()
