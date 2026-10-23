import fcntl
from contextlib import redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("native_macos_access_guest.py")
spec = importlib.util.spec_from_file_location("native_macos_access_guest", SCRIPT)
guest = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guest)


class GuestMaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.lock = self.root / "native.lock"
        self.lock.write_text(json.dumps({"cleanup_complete": True, "remaining_owned_pids": [], "data_removed": True}))
        self.queue = self.root / "queue"
        for name in ("claims", "responses"):
            (self.queue / name).mkdir(parents=True)
        self.claim("current")

    def claim(self, request, response=None):
        (self.queue / "claims" / (request + ".json")).write_text(json.dumps({"id": request, "nonce": "private"}))
        if response is not None:
            (self.queue / "responses" / (request + ".json")).write_text(json.dumps(response))

    def test_only_one_matching_enabled_service_and_mac_can_be_selected(self):
        services = "(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en1)\n(2) Ethernet\n(Hardware Port: Ethernet, Device: en0)\n"
        self.assertEqual(guest.service_for_interface(services, "en0"), "Ethernet")
        guest.verify_mac("en0: flags=1\n\tether 8e:70:a5:c0:d7:c8\n", "8E:70:A5:C0:D7:C8")
        for bad in (services.replace("(2) Ethernet", "(2) *Ethernet"), services + services, ""):
            with self.subTest(bad=bad), self.assertRaises(RuntimeError):
                guest.service_for_interface(bad, "en0")
        with self.assertRaises(RuntimeError):
            guest.verify_mac("ether 00:00:00:00:00:01", "8e:70:a5:c0:d7:c8")

    def test_case_guard_requires_complete_receipt_and_does_not_rewrite_it(self):
        original = self.lock.read_bytes()
        with guest.case_guard(self.lock):
            self.assertEqual(self.lock.read_bytes(), original)
        for receipt in ({}, {"cleanup_complete": True}, {"cleanup_complete": False, "remaining_owned_pids": []},
                        {"cleanup_complete": True, "remaining_owned_pids": [123]}):
            self.lock.write_text(json.dumps(receipt))
            with self.subTest(receipt=receipt), self.assertRaises(RuntimeError):
                with guest.case_guard(self.lock):
                    self.fail("unsafe case was accepted")

    def test_peer_claims_exempt_only_exact_current_queue_and_request(self):
        self.claim("current")
        self.assertEqual(guest.unfinished_claims([self.queue], self.queue, "current"), [])
        self.claim("other")
        self.assertEqual(guest.unfinished_claims([self.queue], self.queue, "current"), [str((self.queue / "claims/other.json").resolve())])
        self.claim("other", {"id": "other", "nonce": "private", "status": "exited", "exit_code": 1})
        self.assertEqual(guest.unfinished_claims([self.queue], self.queue, "current"), [])
        with self.assertRaises(RuntimeError):
            guest.unfinished_claims([self.queue], self.root, "current")
        self.claim("other", {"id": "wrong", "status": "exited", "exit_code": 0})
        self.assertEqual(len(guest.unfinished_claims([self.queue], self.queue, "current")), 1)

    def test_terminal_peer_response_must_match_current_nonce_and_claim(self):
        for response in ({"id": "other", "nonce": "old-session", "status": "exited", "exit_code": 0},
                         {"id": "other", "nonce": "private", "status": "running", "exit_code": 0},
                         {"id": "other", "nonce": "private", "status": "exited", "exit_code": False}):
            self.claim("other", response)
            self.assertEqual(len(guest.unfinished_claims([self.queue], self.queue, "current")), 1)
        with self.assertRaises(RuntimeError):
            guest.unfinished_claims([self.queue], self.queue, "unclaimed")

    def test_guest_identity_refuses_host_wrong_user_and_wrong_mac(self):
        with mock.patch.object(guest.platform, "system", return_value="Linux"), self.assertRaises(RuntimeError):
            guest.require_identity(501, "en0", "8e:70:a5:c0:d7:c8")
        with mock.patch.object(guest.platform, "system", return_value="Darwin"), \
                mock.patch.object(guest.platform, "node", return_value="ci-macOS.local"), \
                mock.patch.object(guest.os, "getuid", return_value=501):
            with self.assertRaises(RuntimeError):
                guest.require_identity(502, "en0", "8e:70:a5:c0:d7:c8")
            with mock.patch.object(guest, "command", side_effect=[{"exit_code": 0, "stdout": "1"}, {"exit_code": 0, "stdout": "ether 00:00:00:00:00:01"}]), self.assertRaises(RuntimeError):
                guest.require_identity(501, "en0", "8e:70:a5:c0:d7:c8")

    def test_existing_evidence_is_never_modified_on_refusal(self):
        evidence = self.root / "existing"
        evidence.mkdir()
        marker = evidence / "failure.json"
        marker.write_text("preserve another operation")
        with mock.patch.object(guest, "require_identity", return_value={}):
            code = guest.main(["inspect", "--evidence", str(evidence), "--expected-uid", "501",
                               "--expected-mac", "8e:70:a5:c0:d7:c8", "--interface", "en0"])
        self.assertEqual(code, 2)
        self.assertEqual(marker.read_text(), "preserve another operation")

    def test_sshd_loaded_and_disabled_are_independent_of_listener_presence(self):
        with mock.patch.object(guest, "command", side_effect=[
            {"exit_code": 0, "stdout": "state = waiting", "stderr": ""},
            {"exit_code": 0, "stdout": '"com.openssh.sshd" => false', "stderr": ""},
        ]):
            self.assertEqual(guest.sshd_state(), {"loaded": True, "disabled": False})
        with mock.patch.object(guest, "command", side_effect=[
            {"exit_code": 113, "stdout": "", "stderr": "Could not find service"},
            {"exit_code": 0, "stdout": '"com.openssh.sshd" => true', "stderr": ""},
        ]):
            self.assertEqual(guest.sshd_state(), {"loaded": False, "disabled": True})
        with mock.patch.object(guest, "command", return_value={"exit_code": 1, "stdout": "", "stderr": "unavailable"}):
            self.assertEqual(guest.sshd_state(), {"loaded": None, "disabled": None})

    def controller(self, name, pid):
        root = self.root / name
        root.mkdir(parents=True)
        for directory in ("claims", "responses"):
            (root / directory).mkdir()
        (root / "requests").mkdir()
        row = {"protocol": 1, "root": str(root), "nonce": "inventory-secret"}
        (root / "session.json").write_text(json.dumps(row))
        (root / "ready.json").write_text(json.dumps({**row, "pid": pid, "uid": 501}))
        return root, f"{pid} 1 501 {root}/ci-queue-agent --root {root} --nonce inventory-secret --ttl 3600"

    def inventory(self, roots, lines, exit_code=0):
        with mock.patch.object(guest, "command", return_value={"exit_code": exit_code, "stdout": "\n".join(lines)}):
            return guest.current_peers(roots, 501)

    def test_current_inventory_excludes_dead_history_but_keeps_every_live_nested_controller(self):
        active, first = self.controller("active", 100)
        nested, second = self.controller("parent/nested", 101)
        stopped, _ = self.controller("stopped", 90)
        (stopped / "claims/stop.json").write_text(json.dumps({"id": "stop", "nonce": "inventory-secret"}))
        (stopped / "requests/stop.json").write_text(json.dumps({"id": "stop", "nonce": "inventory-secret", "stop": True}))
        (stopped / "responses/stop.json").write_text(json.dumps({"id": "stop", "nonce": "inventory-secret", "status": "stopped"}))
        result = self.inventory([active, nested, stopped], [first, second, "102 101 501 /usr/bin/python3 inspect.py"])
        self.assertEqual(result["current_peer_roots"], [str(active), str(nested)])
        self.assertEqual(result["current_peer_controllers"], [{"root": str(active), "pid": 100, "child_pids": []},
                          {"root": str(nested), "pid": 101, "child_pids": [102]}])
        self.assertNotIn("inventory-secret", json.dumps(result))
        self.assertNotIn("inspect.py", json.dumps(result))

    def test_inventory_refuses_unknown_controllers_or_mismatched_owner_pid_and_nonce(self):
        active, line = self.controller("active", 100)
        unknown, other = self.controller("unknown", 101)
        for lines in ([line, other], [line.replace("100 1 501", "100 1 502")],
                      [line.replace("100 1 501", "101 1 501")], [line.replace("inventory-secret", "wrong")]):
            with self.subTest(lines=lines):
                self.assertIsNone(self.inventory([active], lines)["current_peer_roots"])

    def test_dead_controller_with_an_unfinished_live_child_or_unknown_effect_cannot_be_excluded(self):
        dead, _ = self.controller("dead", 90)
        (dead / "claims/work.json").write_text(json.dumps({"id": "work", "nonce": "inventory-secret"}))
        response = {"id": "work", "nonce": "inventory-secret", "status": "interrupted", "pid": 91}
        (dead / "responses/work.json").write_text(json.dumps(response))
        self.assertIsNone(self.inventory([dead], ["91 1 501 /usr/bin/python3 work.py"])["current_peer_roots"])
        self.assertEqual(self.inventory([dead], [])["current_peer_roots"], [])
        (dead / "responses/work.json").unlink()
        self.assertIsNone(self.inventory([dead], [])["current_peer_roots"])

    def test_process_inventory_is_unknown_when_ps_or_queue_identity_cannot_be_verified(self):
        active, line = self.controller("active", 100)
        self.assertIsNone(self.inventory([active], [line], exit_code=1)["current_peer_roots"])
        (active / "ready.json").write_text("{}")
        self.assertIsNone(self.inventory([active], [line])["current_peer_roots"])

    def test_dead_stop_receipt_cannot_hide_an_ordinary_execution_or_mismatched_claim(self):
        dead, _ = self.controller("dead-stop", 90)
        claim = {"id": "work", "nonce": "inventory-secret"}
        (dead / "claims/work.json").write_text(json.dumps(claim))
        response = {**claim, "status": "stopped"}
        (dead / "responses/work.json").write_text(json.dumps(response))
        request_path = dead / "requests/work.json"
        request_path.write_text(json.dumps({**claim, "argv": ["/usr/bin/true"]}))
        self.assertIsNone(self.inventory([dead], [])["current_peer_roots"])
        request_path.write_text(json.dumps({**claim, "stop": True, "nonce": "wrong"}))
        self.assertIsNone(self.inventory([dead], [])["current_peer_roots"])
        (dead / "responses/work.json").write_text(json.dumps({**response, "nonce": "wrong"}))
        self.assertIsNone(self.inventory([dead], [])["current_peer_roots"])

    def test_finished_rejected_and_never_started_history_are_excluded_without_rewriting_it(self):
        dead, _ = self.controller("dead-finished", 90)
        for request, response in (("done", {"status": "exited", "exit_code": 0}), ("invalid", {"status": "rejected"})):
            claim = {"id": request, "nonce": "inventory-secret"}
            (dead / "claims" / (request + ".json")).write_text(json.dumps(claim))
            (dead / "responses" / (request + ".json")).write_text(json.dumps({**claim, **response}))
        original = {path: path.read_bytes() for path in dead.rglob("*.json")}
        self.assertEqual(self.inventory([dead], [])["current_peer_roots"], [])
        self.assertEqual({path: path.read_bytes() for path in original}, original)
        (dead / "ready.json").unlink()
        self.assertEqual(self.inventory([dead], [])["current_peer_roots"], [])
        (dead / "session.json").write_text("{}")
        self.assertIsNone(self.inventory([dead], [])["current_peer_roots"])

    def test_inspect_returns_inventory_for_the_explicit_peer_roots(self):
        evidence = self.root / "inspect"
        output = io.StringIO()
        with mock.patch.object(guest, "require_identity", return_value={"uid": 501}), \
                mock.patch.object(guest, "command", return_value={"exit_code": 0, "stdout": "(1) Ethernet\n(Device: en0)"}), \
                mock.patch.object(guest, "network_snapshot", return_value={"active": False}), \
                mock.patch.object(guest, "sshd_state", return_value={"loaded": True, "disabled": False}), \
                mock.patch.object(guest, "current_peers", return_value={"current_peer_roots": [], "current_peer_controllers": []}) as peers, \
                redirect_stdout(output):
            code = guest.main(["inspect", "--evidence", str(evidence), "--expected-uid", "501", "--expected-mac",
                               "8e:70:a5:c0:d7:c8", "--interface", "en0", "--peer-queue-root", str(self.queue)])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(output.getvalue())["current_peer_roots"], [])
        peers.assert_called_once_with([self.queue], 501)

    def test_dhcp_renewal_is_once_and_never_changes_another_interface(self):
        before = {"interface": "en0", "dhcp": True, "ready": False}
        after = {"interface": "en0", "dhcp": True, "ready": True}
        evidence = self.root / "evidence"
        evidence.mkdir()
        with mock.patch.object(guest, "network_snapshot", side_effect=[before, after]), \
                mock.patch.object(guest, "command", return_value={"exit_code": 0, "stdout": "", "stderr": ""}) as run:
            result = guest.renew_dhcp(evidence, "en0", "Ethernet", "mac", self.lock, [self.queue], self.queue, "current")
        self.assertTrue(result["guest_network_ready"])
        self.assertEqual(run.call_args.args[0], ["/usr/bin/sudo", "-n", "/usr/sbin/ipconfig", "set", "en0", "DHCP"])
        self.assertEqual(run.call_count, 1)
        self.assertEqual(json.loads((evidence / "network-before.json").read_text()), before)

    def test_static_configuration_or_unfinished_peer_prevents_renewal(self):
        for reason in ("static", "peer"):
            evidence = self.root / reason
            evidence.mkdir()
            if reason == "peer":
                self.claim("another-task")
            with mock.patch.object(guest, "network_snapshot", return_value={"dhcp": reason != "static"}), \
                    mock.patch.object(guest, "command") as run, self.assertRaises(RuntimeError):
                guest.renew_dhcp(evidence, "en0", "Ethernet", "mac", self.lock, [self.queue], self.queue, "current")
            run.assert_not_called()

    def test_failed_renewal_is_not_retried_and_unlocks(self):
        evidence = self.root / "failed"
        evidence.mkdir()
        with mock.patch.object(guest, "network_snapshot", return_value={"dhcp": True}), \
                mock.patch.object(guest, "command", return_value={"exit_code": 1, "stdout": "", "stderr": "sudo refused"}) as run, \
                self.assertRaises(RuntimeError):
            guest.renew_dhcp(evidence, "en0", "Ethernet", "mac", self.lock, [self.queue], self.queue, "current")
        self.assertEqual(run.call_count, 1)
        with guest.case_guard(self.lock):
            pass

    def spawn_hold(self, ttl):
        evidence = self.root / ("hold-" + str(time.monotonic_ns()))
        evidence.mkdir()
        code = ("import importlib.util,pathlib,sys; s=importlib.util.spec_from_file_location('guest',sys.argv[1]);"
                "m=importlib.util.module_from_spec(s);s.loader.exec_module(m);"
                "r=m.hold_maintenance(pathlib.Path(sys.argv[2]),'owned-token',float(sys.argv[3]),"
                "pathlib.Path(sys.argv[4]),[pathlib.Path(sys.argv[5])],pathlib.Path(sys.argv[5]),'current');"
                "sys.exit(0 if r['status']=='released' else 124)")
        process = subprocess.Popen([sys.executable, "-c", code, str(SCRIPT), str(evidence), str(ttl), str(self.lock), str(self.queue)],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(lambda: process.kill() if process.poll() is None else None)
        return process, evidence

    def wait_receipt(self, path, predicate=lambda row: True):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            try:
                value = json.loads(path.read_text())
                if predicate(value):
                    return value
            except (FileNotFoundError, json.JSONDecodeError):
                pass
            time.sleep(.02)
        self.fail("maintenance receipt did not reach the expected state")

    def contender(self, native_runner=False):
        module = SCRIPT.with_name("native_macos_case.py") if native_runner else SCRIPT
        context = "m.case_lock()" if native_runner else "m.case_guard(pathlib.Path(sys.argv[2]))"
        code = ("import importlib.util,pathlib,sys; s=importlib.util.spec_from_file_location('guest',sys.argv[1]);"
                "m=importlib.util.module_from_spec(s);s.loader.exec_module(m);m.LOCK_PATH=pathlib.Path(sys.argv[2]);"
                "\ntry:\n with " + context + ": print('acquired')"
                "\nexcept RuntimeError: sys.exit(2)")
        return subprocess.run([sys.executable, "-c", code, str(module), str(self.lock)], capture_output=True, text=True, timeout=5)

    def test_real_process_hold_excludes_another_process_and_requires_own_release_token(self):
        process, evidence = self.spawn_hold(5)
        first = self.wait_receipt(evidence / "maintenance-ready.json")
        self.assertEqual(self.contender(native_runner=True).returncode, 2)
        (evidence / "maintenance-release.json").write_text(json.dumps({"token": "wrong-token"}))
        second = self.wait_receipt(evidence / "maintenance-ready.json", lambda row: row["heartbeat"] > first["heartbeat"])
        self.assertEqual(second["token"], "owned-token")
        self.assertIsNone(process.poll())
        (evidence / "maintenance-release.json").write_text(json.dumps({"token": "owned-token"}))
        stdout, stderr = process.communicate(timeout=5)
        self.assertEqual(process.returncode, 0, stdout + stderr)
        self.assertEqual(self.contender(native_runner=True).returncode, 0)
        receipt = json.loads((evidence / "maintenance-finished.json").read_text())
        self.assertEqual(receipt["status"], "released")
        self.assertTrue(receipt["lock_released"])

    def test_real_process_hold_expires_and_releases_the_actual_flock(self):
        process, evidence = self.spawn_hold(.6)
        self.wait_receipt(evidence / "maintenance-ready.json")
        self.assertEqual(self.contender().returncode, 2)
        stdout, stderr = process.communicate(timeout=5)
        self.assertEqual(process.returncode, 124, stdout + stderr)
        self.assertEqual(self.contender().returncode, 0)
        receipt = json.loads((evidence / "maintenance-finished.json").read_text())
        self.assertEqual(receipt["status"], "expired")
        self.assertTrue(receipt["lock_released"])


if __name__ == "__main__":
    unittest.main()
