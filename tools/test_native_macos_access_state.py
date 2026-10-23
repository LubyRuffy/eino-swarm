"""Network loss, transport health and restart ownership are separate evidence."""
import copy
from pathlib import Path
import runpy
import unittest


decide = runpy.run_path(str(Path(__file__).with_name("native_macos_access_state.py")))["decide"]


def observation():
    return {
        "target": "vm-fixture-id", "probe_id": "current-probe",
        "host": {"vm_network_present": False, "old_ip_route": "proxy"},
        "guest": {
            "probe_id": "current-probe", "identity_matches": True, "hypervisor": True,
            "link": "inactive", "ipv4_present": False, "default_route_present": False,
            "sshd_loaded": True, "sshd_disabled": False,
        },
        "channels": {
            "ssh": "timeout", "utm_exec": "unsupported",
            "shared_queue": {"probe_id": "current-probe", "result": "ok"},
        },
        "maintenance": {
            "probe_id": "current-probe", "lock": "peer", "cleanup_complete": False,
            "outstanding_claims": ["peer-case"],
        },
    }


def owned_maintenance(row):
    row["maintenance"].update(lock="owned", cleanup_complete=True, outstanding_claims=[])


class NativeAccessStateTests(unittest.TestCase):
    def test_missing_vm_network_is_not_a_broken_ssh_service(self):
        result = decide(observation())
        self.assertEqual(result["diagnosis"], "guest_network_unavailable")
        self.assertEqual(result["control_channel"], "shared_queue")
        self.assertEqual(result["next_action"], "acquire_guest_maintenance")
        self.assertFalse(result["restart_admitted"])
        self.assertIn("old_ip_routes_to_proxy", result["reason_codes"])
        self.assertIn("utm_exec_unsupported", result["reason_codes"])
        self.assertIn("sshd_on_demand_loaded", result["reason_codes"])

    def test_only_current_exclusive_ownership_admits_a_bounded_network_rebuild(self):
        row = observation()
        owned_maintenance(row)
        result = decide(row)
        self.assertTrue(result["restart_admitted"])
        self.assertEqual(result["next_action"], "bounded_rebuild_vm_network")

    def test_each_missing_maintenance_fact_refuses_restart(self):
        for field, value, reason in (
            ("probe_id", "previous-probe", "maintenance_probe_stale"),
            ("lock", "peer", "maintenance_lock_not_owned"),
            ("lock", "unknown", "maintenance_lock_not_owned"),
            ("cleanup_complete", False, "peer_cleanup_incomplete"),
            ("cleanup_complete", None, "peer_cleanup_incomplete"),
            ("outstanding_claims", ["unfinished-job"], "outstanding_claims_present"),
            ("outstanding_claims", None, "outstanding_claims_unknown"),
        ):
            with self.subTest(field=field, value=value):
                row = observation()
                owned_maintenance(row)
                row["maintenance"][field] = value
                result = decide(row)
                self.assertFalse(result["restart_admitted"])
                self.assertNotEqual(result["next_action"], "bounded_rebuild_vm_network")
                self.assertIn(reason, result["reason_codes"])

    def test_stale_or_mismatched_guest_cannot_admit_restart(self):
        for field, value in (("probe_id", "previous-probe"), ("probe_id", None),
                             ("identity_matches", False), ("identity_matches", None),
                             ("hypervisor", False), ("hypervisor", None)):
            with self.subTest(field=field, value=value):
                row = observation()
                owned_maintenance(row)
                row["guest"][field] = value
                result = decide(row)
                self.assertFalse(result["restart_admitted"])
                self.assertEqual(result["diagnosis"], "guest_identity_unverified")
                self.assertEqual(result["next_action"], "probe_guest_identity")

    def test_old_queue_ready_and_ssh_timeout_do_not_prove_the_queue_is_gone(self):
        row = observation()
        row["guest"]["probe_id"] = None
        row["channels"]["shared_queue"]["probe_id"] = "previous-probe"
        result = decide(row)
        self.assertIsNone(result["control_channel"])
        self.assertEqual(result["next_action"], "probe_shared_queue")
        self.assertIn("shared_queue_receipt_stale", result["reason_codes"])

    def test_unsupported_exec_uses_available_queue_and_never_retries_that_exec(self):
        row = observation()
        row["host"]["vm_network_present"] = True
        result = decide(row)
        self.assertEqual(result["control_channel"], "shared_queue")
        self.assertEqual(result["next_action"], "refresh_guest_network")
        row["channels"]["shared_queue"]["result"] = "timeout"
        result = decide(row)
        self.assertIsNone(result["control_channel"])
        self.assertEqual(result["next_action"], "probe_shared_queue")

    def test_active_nic_without_ip_uses_guest_dhcp_before_a_vm_rebuild(self):
        row = observation()
        owned_maintenance(row)
        row["guest"]["link"] = "active"
        result = decide(row)
        self.assertEqual(result["diagnosis"], "guest_ipv4_unavailable")
        self.assertEqual(result["next_action"], "refresh_guest_dhcp")

    def test_unknown_network_facts_and_missing_host_interfaces_do_not_require_restart(self):
        row = observation()
        owned_maintenance(row)
        for link, ipv4, route in (("unknown", None, None), ("active", True, True)):
            with self.subTest(link=link, ipv4=ipv4, route=route):
                row["guest"].update(link=link, ipv4_present=ipv4, default_route_present=route)
                result = decide(row)
                self.assertEqual(result["diagnosis"], "transport_unresolved")
                self.assertNotEqual(result["next_action"], "bounded_rebuild_vm_network")

    def test_dhcp_and_network_inspection_need_a_real_available_channel(self):
        row = observation()
        row["channels"]["shared_queue"]["result"] = "unknown"
        for link, ipv4 in (("active", False), ("unknown", None)):
            with self.subTest(link=link):
                row["guest"].update(link=link, ipv4_present=ipv4)
                self.assertEqual(decide(row)["next_action"], "probe_shared_queue")

    def test_loaded_on_demand_sshd_is_not_disabled_because_a_process_is_absent(self):
        row = observation()
        row["guest"].update(link="active", ipv4_present=True, default_route_present=True)
        result = decide(row)
        self.assertEqual(result["diagnosis"], "transport_unresolved")
        self.assertEqual(result["next_action"], "inspect_guest_network")
        self.assertNotIn("sshd_disabled", result["reason_codes"])

    def test_real_disabled_service_refusal_and_authentication_have_distinct_actions(self):
        row = observation()
        row["guest"].update(link="active", ipv4_present=True, default_route_present=True)
        for ssh, disabled, diagnosis, action in (
            ("timeout", True, "sshd_disabled", "inspect_guest_sshd"),
            ("refused", False, "ssh_connection_refused", "inspect_guest_sshd"),
            ("auth_failed", False, "ssh_authentication_failed", "inspect_ssh_authentication"),
        ):
            with self.subTest(ssh=ssh, disabled=disabled):
                row["channels"]["ssh"] = ssh
                row["guest"]["sshd_disabled"] = disabled
                result = decide(row)
                self.assertEqual(result["diagnosis"], diagnosis)
                self.assertEqual(result["next_action"], action)

    def test_ssh_success_is_access_only_and_does_not_make_restart_necessary(self):
        row = observation()
        owned_maintenance(row)
        row["channels"]["ssh"] = "ok"
        result = decide(row)
        self.assertEqual(result["diagnosis"], "access_available")
        self.assertEqual(result["control_channel"], "ssh")
        self.assertEqual(result["next_action"], "inspect_native_window")
        self.assertEqual(result["native_acceptance"], "pending")

    def test_supported_exec_is_a_fallback_without_assuming_native_window_success(self):
        row = observation()
        row["channels"]["shared_queue"]["result"] = "unknown"
        row["channels"]["utm_exec"] = "ok"
        self.assertEqual(decide(row)["control_channel"], "utm_exec")
        self.assertEqual(decide(row)["native_acceptance"], "pending")

    def test_cross_machine_timestamps_are_not_a_freshness_or_queue_ttl_input(self):
        row = observation()
        row["channels"]["shared_queue"].update(host_time=1000, guest_time=922, ttl_seconds=3600)
        with self.assertRaises(ValueError):
            decide(row)

    def test_observations_are_not_mutated_and_private_inputs_are_not_echoed(self):
        row = observation()
        before = copy.deepcopy(row)
        result = decide(row)
        self.assertEqual(row, before)
        self.assertNotIn(row["target"], str(result))
        self.assertNotIn(row["probe_id"], str(result))
        row["password"] = "private-value"
        with self.assertRaises(ValueError) as error:
            decide(row)
        self.assertNotIn("private-value", str(error.exception))

    def test_malformed_evidence_cannot_be_coerced_into_restart_permission(self):
        invalid = (
            ((), None),
            (("target",), ""),
            (("probe_id",), " "),
            (("host", "vm_network_present"), 1),
            (("host", "old_ip_route"), "claimed-good"),
            (("guest", "identity_matches"), "true"),
            (("guest", "probe_id"), 5),
            (("guest", "link"), "up"),
            (("channels", "shared_queue", "result"), "ready"),
            (("channels", "ssh"), "success"),
            (("maintenance", "cleanup_complete"), 1),
            (("maintenance", "lock"), "unlocked"),
            (("maintenance", "outstanding_claims"), ""),
            (("maintenance", "outstanding_claims"), [""]),
        )
        for path, value in invalid:
            with self.subTest(path=path):
                row = observation()
                owned_maintenance(row)
                if not path:
                    row = value
                else:
                    nested = row
                    for field in path[:-1]:
                        nested = nested[field]
                    nested[path[-1]] = value
                with self.assertRaises(ValueError):
                    decide(row)


if __name__ == "__main__":
    unittest.main()
