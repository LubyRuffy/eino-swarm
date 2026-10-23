"""Access preflight uses fresh process/queue outcomes and pinned SSH identity."""
from contextlib import redirect_stdout
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("native_macos_access.py")
spec = importlib.util.spec_from_file_location("native_macos_access", SCRIPT)
access = importlib.util.module_from_spec(spec)
spec.loader.exec_module(access)


class NativeAccessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.shared = self.root / "shared files"
        self.shared.joinpath("dist").mkdir(parents=True)
        self.profile = dict(vm_id="12345678-1234-1234-1234-123456789abc", vm_name="ci-macOS", user="ci", uid=501,
                            mac="00:00:00:00:00:01", interface="en0", identity_file=str(self.root / "key"),
                            known_hosts=str(self.root / "known_hosts"), host_key_alias="selected-guest",
                            shared_host_root=str(self.shared), shared_guest_root=str(self.shared))
        self.profile_path = self.root / "access.json"
        access.save_json(self.profile_path, self.profile)
        self.leases = self.root / "leases"
        self.leases.write_text("")
        self.fixture = self.root / "guest-fixture.py"
        self.fixture.write_text("""import json,pathlib,sys
action=sys.argv[1]
arg=lambda name: sys.argv[sys.argv.index(name)+1]
evidence=pathlib.Path(arg('--evidence')); evidence.mkdir()
if action=='inspect':
 result=dict(identity=dict(hostname='ci-macOS.local',uid=int(arg('--expected-uid')),virtualized=True,
                          mac=arg('--expected-mac'),interface=arg('--interface')),
             network=dict(active=True,ip='192.168.64.4',default_route_on_interface=True),
             sshd=dict(loaded=True,disabled=False),
             current_peer_roots=[sys.argv[i+1] for i,arg in enumerate(sys.argv) if arg=='--peer-queue-root' and
                                 pathlib.Path(sys.argv[i+1],'fixture-live').exists()])
else:
 root=pathlib.Path(arg('--own-queue-root')); request=arg('--request-id')
 claim=json.loads((root/'claims'/(request+'.json')).read_text())
 assert claim['id']==request and claim['nonce']
 result=dict(status='renewed',renewals=1,guest_network_ready=True,host_connectivity='unverified')
print(json.dumps(result))
""")
        self.workers = []
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(access, "LEASES", self.leases).start()
        mock.patch.object(access, "GUEST_SOURCE", self.fixture).start()

    def queue(self, name="queue", stopped=False):
        root = self.shared / "dist" / name
        root.mkdir(parents=True)
        for directory in ("requests", "responses", "claims", "logs"):
            (root / directory).mkdir()
        row = {"protocol": 1, "root": str(root), "nonce": "fixture-session-secret"}
        access.save_json(root / "session.json", row)
        access.save_json(root / "ready.json", {**row, "started_at": "2000-01-01T00:00:00Z", "ttl_seconds": 1})
        if stopped:
            access.save_json(root / "stopped.json", {"status": "stopped"})
        return {"host": root, "guest": str(root), "nonce": row["nonce"]}

    def worker(self, queue, override=None):
        stop, seen, errors = threading.Event(), [], []
        queue["host"].joinpath("fixture-live").write_text("fixture running controller")
        def serve():
            while not stop.wait(.005):
                for path in queue["host"].joinpath("requests").glob("*.json"):
                    if path.stem in [request["id"] for request in seen]:
                        continue
                    try:
                        request = json.loads(path.read_text())
                        seen.append(request)
                        self.assertEqual(request["nonce"], queue["nonce"])
                        self.assertTrue(Path(request["argv"][0]).is_absolute(), "real queue executable validation")
                        self.assertIs(type(request["timeout_seconds"]), int, "real queue integer decoding")
                        self.assertTrue(Path(request["cwd"]).is_absolute())
                        self.assertLessEqual(len(request["argv"]), 64)
                        self.assertLessEqual(path.stat().st_size, 65536)
                        access.save_json(queue["host"] / "claims" / path.name, {"id": request["id"], "nonce": request["nonce"]})
                        child = subprocess.run(request["argv"], cwd=request["cwd"], capture_output=True, text=True, timeout=3)
                        row = dict(protocol=1, id=request["id"], nonce=request["nonce"], status="exited",
                                   exit_code=child.returncode, forced_kill=False, stdout_truncated=False, stderr_truncated=False)
                        for field in ("stdout", "stderr"):
                            row[field] = "logs/" + path.stem + "." + field + ".log"
                            queue["host"].joinpath(row[field]).write_text(getattr(child, field))
                        row.update(override or {})
                        target = queue["host"] / "responses" / path.name
                        temporary = target.with_suffix(".tmp")
                        access.save_json(temporary, row)
                        os.replace(temporary, target)
                    except BaseException as error:
                        errors.append(error)
                        stop.set()
        thread = threading.Thread(target=serve)
        thread.start()
        def finish():
            stop.set()
            thread.join(5)
            queue["host"].joinpath("fixture-live").unlink(missing_ok=True)
            self.assertFalse(thread.is_alive())
            self.assertEqual(errors, [])
        self.addCleanup(finish)
        return seen

    def host_run(self, ssh="timeout", route="vm", interfaces="en0 bridge100"):
        calls = []
        def run(argv, timeout=5):
            calls.append(argv)
            if argv[0] == "ssh" and ssh == "ok":
                return access_run(shlex.split(argv[-1]), timeout)
            if argv[0] == "ssh":
                return dict(exit_code=None, stdout="", stderr="", timed_out=True)
            output = interfaces if argv[0] == "/sbin/ifconfig" else "interface: " + ("utun2" if route == "proxy" else "bridge100")
            return dict(exit_code=0, stdout=output, stderr="", timed_out=False)
        access_run = access.run
        mock.patch.object(access, "run", side_effect=run).start()
        return calls

    def cli(self, repair=False):
        evidence = self.root / ("evidence-" + str(time.monotonic_ns()))
        output = io.StringIO()
        args = ["--profile", str(self.profile_path), "--evidence", str(evidence)]
        with redirect_stdout(output):
            code = access.main(args + (["--repair-network"] if repair else []))
        return code, json.loads(output.getvalue()), evidence

    def test_only_the_selected_mac_supplies_dhcp_candidate_addresses(self):
        leases = """{\n ip_address=192.168.64.2\n hw_address=1,00:00:00:00:00:01\n}
{\n ip_address=192.168.64.3\n hw_address=1,00:00:00:00:00:02\n}
{\n ip_address=192.168.64.4\n hw_address=1,00:00:00:00:00:01\n}"""
        self.assertEqual(access.candidate_ips(leases, "00:00:00:00:00:01"), ["192.168.64.4", "192.168.64.2"])

    def test_invalid_duplicate_and_loopback_leases_do_not_add_targets(self):
        blocks = ["{ip_address=" + ip + "\nhw_address=1," + self.profile["mac"] + "}" for ip in ("bad", "127.0.0.1", "192.168.64.4", "192.168.64.4")]
        self.assertEqual(access.candidate_ips("\n".join(blocks), self.profile["mac"]), ["192.168.64.4"])
        self.assertEqual(access.route_kind("interface: utun4"), "proxy")
        self.assertEqual(access.route_kind("interface: en0"), "other")
        self.assertEqual(access.route_kind("no route"), "unknown")

    def test_real_subprocess_success_failure_and_deadline_are_separate(self):
        success = access.run([sys.executable, "-c", "print('observed')"])
        self.assertEqual((success["exit_code"], success["stdout"].strip()), (0, "observed"))
        failure = access.run([sys.executable, "-c", "import sys;sys.exit(7)"])
        self.assertEqual(failure["exit_code"], 7)
        timeout = access.run([sys.executable, "-c", "import time;time.sleep(10)"], .05)
        self.assertTrue(timeout["timed_out"])
        self.assertIsNone(timeout["exit_code"])

    def test_guest_bootstrap_uses_the_absolute_executable_required_by_the_real_queue(self):
        frozen = {"guest_path": "/shared/guest.py", "source_sha256": "a" * 64}
        argv = access.stage_argv(self.profile, frozen, "probe", "inspect", "", "request", [])
        self.assertTrue(Path(argv[0]).is_absolute(), "the actual queue rejects relative executable paths")

    def test_fresh_queue_executes_new_request_despite_old_ready_wall_clock(self):
        queue = self.queue()
        seen = self.worker(queue)
        raw = access.queue_call(queue, lambda root, request: [sys.executable, "-c", "print('fresh')"], 2.5)
        self.assertEqual(raw["stdout"].strip(), "fresh")
        self.assertEqual(len(seen), 1)
        self.assertEqual(len(seen[0]["id"]), 32)

    def test_timeout_removes_only_own_pending_request_without_claiming_it_never_ran(self):
        queue = self.queue()
        peer = queue["host"] / "requests/peer.json"
        peer.write_text("peer evidence")
        raw = access.queue_call(queue, lambda root, request: ["/unused"], .05)
        self.assertTrue(raw["timed_out"])
        self.assertIsNone(raw["exit_code"])
        self.assertEqual(raw["cancellation"], "unconfirmed")
        self.assertEqual(peer.read_text(), "peer evidence")
        self.assertEqual(list(queue["host"].joinpath("requests").glob("*.json")), [peer])

    def test_queue_nonce_log_escape_truncation_and_nonterminal_results_are_rejected(self):
        for index, override in enumerate((
            {"nonce": "wrong"}, {"id": "wrong"}, {"stdout": "../outside"},
            {"stdout_truncated": True}, {"status": "running"}, {"exit_code": True}, {"forced_kill": True},
        )):
            with self.subTest(override=override):
                queue = self.queue("invalid-" + str(index))
                self.worker(queue, override)
                with self.assertRaises(ValueError):
                    access.queue_call(queue, lambda root, request: [sys.executable, "-c", "print('{}')"], 2)

    def test_stopped_queues_remain_observation_candidates_for_the_guest_to_classify(self):
        live = self.queue("live")
        stopped = self.queue("stopped", stopped=True)
        self.assertEqual(access.queues(self.profile), [live])
        self.assertEqual(access.queues(self.profile, include_stopped=True), [live, stopped])

    def test_guard_inventory_uses_fresh_inspection_instead_of_old_stopped_claims(self):
        queue = self.queue()
        seen = self.worker(queue)
        stopped = self.queue("old-stopped", stopped=True)
        access.save_json(stopped["host"] / "claims/old-stop.json", {"id": "old-stop", "nonce": stopped["nonce"]})
        access.save_json(stopped["host"] / "responses/old-stop.json", {"id": "old-stop", "nonce": stopped["nonce"], "status": "stopped"})
        self.host_run()
        self.cli(repair=True)
        renewal = next(request for request in seen if request["argv"][7] == "renew-dhcp")
        self.assertIn(queue["guest"], renewal["argv"])
        self.assertNotIn(stopped["guest"], renewal["argv"])
        self.assertTrue((stopped["host"] / "claims/old-stop.json").exists())

    def test_unknown_current_peer_inventory_cannot_emit_a_dhcp_request(self):
        queue = self.queue()
        seen = self.worker(queue)
        self.fixture.write_text(self.fixture.read_text().replace(
            "print(json.dumps(result))", "if action=='inspect': result['current_peer_roots']=None\nprint(json.dumps(result))"))
        self.host_run()
        code, output, evidence = self.cli(repair=True)
        self.assertEqual(code, 3)
        self.assertEqual(json.loads((evidence / "repair.json").read_text())["reason"], "current_peer_inventory_unverified")
        self.assertEqual([request["argv"][7] for request in seen], ["inspect"])

    def test_nested_queue_keeps_its_full_guest_relative_path(self):
        nested = self.queue("session/queue3")
        self.assertEqual(access.queues(self.profile), [nested])

    def test_thunderbolt_bridge_does_not_prove_a_virtualization_network(self):
        self.host_run(interfaces="lo0 en0 bridge0")
        code, output, evidence = self.cli()
        self.assertFalse(json.loads((evidence / "host.json").read_text())["vm_network_present"])

    def test_preflight_runs_frozen_real_inspect_and_keeps_credentials_out_of_evidence(self):
        queue = self.queue()
        seen = self.worker(queue)
        self.host_run()
        code, output, evidence = self.cli()
        self.assertEqual(code, 0)
        self.assertEqual(output["control_channel"], "shared_queue")
        self.assertEqual(output["native_acceptance"], "pending")
        self.assertFalse(output["restart_admitted"])
        self.assertEqual([request["argv"][7] for request in seen], ["inspect"])
        source = json.loads((evidence / "source.json").read_text())
        self.assertEqual(source["source_sha256"], hashlib.sha256(self.fixture.read_bytes()).hexdigest())
        guest = json.loads((evidence / "guest.json").read_text())
        self.assertEqual(guest["source_sha256"], source["source_sha256"])
        self.assertEqual(guest["guest_path"], source["guest_path"])
        self.assertTrue(json.loads((evidence / "observation.json").read_text())["guest"]["sshd_loaded"])
        for path in evidence.glob("*.json"):
            self.assertNotIn(queue["nonce"], path.read_text())
        self.assertNotIn(queue["nonce"], json.dumps(output))

    def test_proxy_route_does_not_send_ssh_to_the_old_ip(self):
        self.leases.write_text("{ip_address=192.168.64.4\nhw_address=1," + self.profile["mac"] + "}")
        calls = self.host_run(route="proxy")
        code, output, evidence = self.cli()
        self.assertEqual(code, 3)
        self.assertFalse(any(argv[0] == "ssh" for argv in calls))
        self.assertIn("old_ip_routes_to_proxy", output["reason_codes"])

    def test_ssh_requires_pinned_host_and_real_stage_then_repair_refuses_without_queue(self):
        self.leases.write_text("{ip_address=192.168.64.4\nhw_address=1," + self.profile["mac"] + "}")
        calls = self.host_run(ssh="ok")
        code, output, evidence = self.cli()
        self.assertEqual((code, output["control_channel"]), (0, "ssh"))
        ssh = next(argv for argv in calls if argv[0] == "ssh")
        for option in ("BatchMode=yes", "StrictHostKeyChecking=yes", "IdentitiesOnly=yes", "HostKeyAlias=selected-guest",
                       "UserKnownHostsFile=" + self.profile["known_hosts"]):
            self.assertIn(option, ssh)
        code, output, evidence = self.cli(repair=True)
        self.assertEqual(code, 3)
        self.assertEqual(json.loads((evidence / "repair.json").read_text())["reason"], "needs_fresh_queue_for_guarded_repair")

    def test_guarded_repair_is_an_actual_child_request_and_host_reachability_stays_separate(self):
        queue = self.queue()
        seen = self.worker(queue)
        self.host_run()
        code, output, evidence = self.cli(repair=True)
        self.assertEqual([request["argv"][7] for request in seen], ["inspect", "renew-dhcp", "inspect"])
        repair = json.loads((evidence / "repair.json").read_text())
        self.assertEqual(repair["result"]["renewals"], 1)
        self.assertEqual(repair["queue_root"], queue["guest"])
        self.assertEqual(repair["request_id"], seen[1]["id"])
        request_receipt = json.loads((evidence / ("request-" + repair["request_id"] + ".json")).read_text())
        self.assertEqual(request_receipt["queue_root"], queue["guest"])
        self.assertEqual(repair["result"]["host_connectivity"], "unverified")
        self.assertEqual(code, 3, "a guest DHCP receipt cannot prove that pinned host SSH recovered")
        self.assertEqual(output["repair_status"], "guest_renewed_host_unverified")

    def test_repair_success_requires_an_actual_followup_pinned_ssh_stage(self):
        queue = self.queue()
        seen = self.worker(queue)
        self.leases.write_text("{ip_address=192.168.64.4\nhw_address=1," + self.profile["mac"] + "}")
        self.host_run(ssh="ok")
        code, output, evidence = self.cli(repair=True)
        self.assertEqual(code, 0)
        self.assertEqual(output["control_channel"], "ssh")
        self.assertEqual(output["repair_status"], "host_ssh_verified")
        self.assertEqual(output["native_acceptance"], "pending")
        self.assertTrue(json.loads((evidence / "repair.json").read_text())["host_ssh_verified"])
        self.assertEqual([request["argv"][7] for request in seen], ["inspect", "renew-dhcp"])

    def test_profile_and_evidence_identity_errors_fail_closed(self):
        self.profile_path.chmod(0o644)
        self.assertEqual(self.cli()[0], 2)
        self.profile_path.chmod(0o600)
        row = dict(self.profile, password="private-value")
        self.profile_path.write_text(json.dumps(row))
        code, output, evidence = self.cli()
        self.assertEqual(code, 2)
        self.assertNotIn("private-value", json.dumps(output))

    def test_invalid_profile_types_aliases_paths_and_accounts_are_rejected(self):
        for field, value in (("vm_id", "wrong"), ("vm_name", "other"), ("user", "root"), ("uid", True),
                             ("mac", "00:wrong"), ("interface", "en0;false"), ("known_hosts", "relative"),
                             ("host_key_alias", " "), ("vm_name", None)):
            with self.subTest(field=field):
                self.profile_path.write_text(json.dumps(dict(self.profile, **{field: value})))
                with self.assertRaises(ValueError):
                    access.load_profile(self.profile_path)

    def test_real_bootstrap_refuses_changed_source_before_executing_the_child(self):
        evidence = self.root / "frozen-evidence"
        evidence.mkdir()
        frozen = access.freeze(self.profile, evidence)
        Path(frozen["guest_path"]).chmod(0o600)
        Path(frozen["guest_path"]).write_text("raise RuntimeError('must not execute')")
        raw = access.run(access.stage_argv(self.profile, frozen, "probe", "inspect", "", "request", []))
        self.assertNotEqual(raw["exit_code"], 0)
        self.assertIn("guest source hash differs", raw["stderr"])
        self.assertNotIn("must not execute", raw["stderr"])

    def test_real_stage_receipt_requires_exact_source_path_probe_status_and_guest_identity(self):
        evidence = self.root / "stage-evidence"
        evidence.mkdir()
        frozen = access.freeze(self.profile, evidence)
        raw = access.run(access.stage_argv(self.profile, frozen, "probe", "inspect", "", "request", []))
        row = access.validate_stage(raw, frozen, "probe", "inspect", self.profile)
        for path, value in (
            (("probe_id",), "old"), (("source_sha256",), "wrong"), (("guest_path",), "/other/guest.py"),
            (("exit_code",), 9), (("result", "identity", "virtualized"), False),
            (("result", "identity", "uid"), 0), (("result", "identity", "mac"), "00:00:00:00:00:02"),
            (("result", "identity", "hostname"), "another-guest"), (("result", "identity", "interface"), "en1"),
        ):
            with self.subTest(path=path):
                changed = json.loads(json.dumps(row))
                nested = changed
                for key in path[:-1]:
                    nested = nested[key]
                nested[path[-1]] = value
                with self.assertRaises(ValueError):
                    access.validate_stage(dict(raw, stdout=json.dumps(changed)), frozen, "probe", "inspect", self.profile)


if __name__ == "__main__":
    unittest.main()
