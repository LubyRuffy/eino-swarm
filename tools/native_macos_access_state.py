"""Classify current VM access observations without operating a VM or native UI."""


def _object(value, fields):
    if not isinstance(value, dict) or value.keys() != set(fields):
        # Unknown keys may contain credentials; never include them in an error.
        raise ValueError("Observation has missing or unsupported fields")
    return value


def _text(value, optional=False):
    if optional and value is None:
        return
    if not isinstance(value, str) or not value.strip():
        raise ValueError("Observation references must be nonempty strings")


def _flag(value):
    if value is not None and type(value) is not bool:
        raise ValueError("Observation flags must be boolean or null")


def _choice(value, choices):
    if value not in choices:
        raise ValueError("Observation contains an unsupported state")


def _validate(row):
    _object(row, ("target", "probe_id", "host", "guest", "channels", "maintenance"))
    _text(row["target"])
    _text(row["probe_id"])
    host = _object(row["host"], ("vm_network_present", "old_ip_route"))
    _flag(host["vm_network_present"])
    _choice(host["old_ip_route"], ("vm", "proxy", "other", "unknown"))
    guest = _object(row["guest"], ("probe_id", "identity_matches", "hypervisor", "link",
                                   "ipv4_present", "default_route_present", "sshd_loaded", "sshd_disabled"))
    _text(guest["probe_id"], optional=True)
    _choice(guest["link"], ("active", "inactive", "unknown"))
    for field in ("identity_matches", "hypervisor", "ipv4_present", "default_route_present",
                  "sshd_loaded", "sshd_disabled"):
        _flag(guest[field])
    channels = _object(row["channels"], ("ssh", "utm_exec", "shared_queue"))
    _choice(channels["ssh"], ("ok", "timeout", "refused", "auth_failed", "unknown"))
    _choice(channels["utm_exec"], ("ok", "unsupported", "failed", "unknown"))
    queue = _object(channels["shared_queue"], ("probe_id", "result"))
    _text(queue["probe_id"], optional=True)
    _choice(queue["result"], ("ok", "timeout", "unknown"))
    maintenance = _object(row["maintenance"], ("probe_id", "lock", "cleanup_complete", "outstanding_claims"))
    _text(maintenance["probe_id"], optional=True)
    _choice(maintenance["lock"], ("owned", "peer", "unknown"))
    _flag(maintenance["cleanup_complete"])
    claims = maintenance["outstanding_claims"]
    if claims is not None:
        if not isinstance(claims, list):
            raise ValueError("Outstanding claims must be a list or null")
        for claim in claims:
            _text(claim)


def decide(observation):
    """Classify one probe; the caller owns identity checks and local deadlines.

    Matching probe IDs mean a new challenge was answered. Guest/host wall-clock
    timestamps and old ready files cannot establish current queue availability.
    Restart admission is a necessary safety gate, never a request to restart.
    """
    _validate(observation)
    row = observation
    host, guest, channels, maintenance = (row[key] for key in ("host", "guest", "channels", "maintenance"))
    reasons = []
    if host["old_ip_route"] == "proxy":
        reasons.append("old_ip_routes_to_proxy")
    if host["vm_network_present"] is False:
        reasons.append("host_vm_network_missing")
    if channels["utm_exec"] == "unsupported":
        reasons.append("utm_exec_unsupported")
    queue = channels["shared_queue"]
    queue_current = queue["result"] == "ok" and queue["probe_id"] == row["probe_id"]
    if queue["result"] == "ok" and not queue_current:
        reasons.append("shared_queue_receipt_stale")
    channel = ("ssh" if channels["ssh"] == "ok" else "shared_queue" if queue_current else
               "utm_exec" if channels["utm_exec"] == "ok" else None)

    guest_current = (guest["probe_id"] == row["probe_id"] and
                     guest["identity_matches"] is True and guest["hypervisor"] is True)
    if not guest_current:
        reasons.append("guest_identity_unverified")
    if maintenance["probe_id"] != row["probe_id"]:
        reasons.append("maintenance_probe_stale")
    if maintenance["lock"] != "owned":
        reasons.append("maintenance_lock_not_owned")
    if maintenance["cleanup_complete"] is not True:
        reasons.append("peer_cleanup_incomplete")
    claims = maintenance["outstanding_claims"]
    if claims is None:
        reasons.append("outstanding_claims_unknown")
    elif claims:
        reasons.append("outstanding_claims_present")
    admitted = (guest_current and maintenance["probe_id"] == row["probe_id"] and
                maintenance["lock"] == "owned" and maintenance["cleanup_complete"] is True and claims == [])

    def result(diagnosis, action):
        return {"diagnosis": diagnosis, "next_action": action, "control_channel": channel,
                "restart_admitted": admitted, "native_acceptance": "pending", "reason_codes": reasons}

    if not guest_current:
        return result("guest_identity_unverified", "probe_guest_identity" if channel else "probe_shared_queue")
    if guest["sshd_loaded"] is True and guest["sshd_disabled"] is False:
        reasons.append("sshd_on_demand_loaded")
    if channels["ssh"] == "ok":
        return result("access_available", "inspect_native_window")
    network_down = (guest["link"] == "inactive" and guest["ipv4_present"] is False and
                    guest["default_route_present"] is False)
    if network_down:
        reasons.extend(("guest_network_link_inactive", "guest_ipv4_missing", "guest_default_route_missing"))
        if host["vm_network_present"] is False:
            action = "bounded_rebuild_vm_network" if admitted else "acquire_guest_maintenance"
        else:
            action = "refresh_guest_network" if channel else "probe_shared_queue"
        return result("guest_network_unavailable", action)
    if guest["link"] == "active" and guest["ipv4_present"] is False:
        return result("guest_ipv4_unavailable", "refresh_guest_dhcp" if channel else "probe_shared_queue")
    if guest["sshd_disabled"] is True:
        return result("sshd_disabled", "inspect_guest_sshd")
    if channels["ssh"] == "auth_failed":
        return result("ssh_authentication_failed", "inspect_ssh_authentication")
    if channels["ssh"] == "refused":
        return result("ssh_connection_refused", "inspect_guest_sshd")
    return result("transport_unresolved", "inspect_guest_network" if channel else "probe_shared_queue")
