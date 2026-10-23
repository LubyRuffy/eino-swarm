#!/usr/bin/env python3
"""Inspect or maintain ci-macOS access under the native-case exclusion lock.

Run only in the identified guest. This tool never reboots a VM or changes a
host, another interface, peer request, or the native case's cleanup receipt.
"""
import argparse
from contextlib import contextmanager
import fcntl
import json
import math
import os
from pathlib import Path
import platform
import re
import signal
import subprocess
import tempfile
import time


def command(argv, timeout=5):
    result = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
    return {"exit_code": result.returncode, "stdout": result.stdout, "stderr": result.stderr}


def require_identity(expected_uid, interface, expected_mac):
    if platform.system() != "Darwin" or platform.node().lower().rstrip(".") not in ("ci-macos", "ci-macos.local"):
        raise RuntimeError("only the ci-macOS guest may perform access maintenance")
    if os.getuid() != expected_uid:
        raise RuntimeError("guest user identity does not match the selected account")
    if not re.fullmatch(r"en[0-9]+", interface) or not re.fullmatch(r"(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}", expected_mac):
        raise RuntimeError("explicit Ethernet interface and MAC identity required")
    present = command(["/usr/sbin/sysctl", "-n", "kern.hv_vmm_present"])
    if present["exit_code"] or present["stdout"].strip() != "1":
        raise RuntimeError("physical host refused; macOS VM identity is required")
    network = command(["/sbin/ifconfig", interface])
    if network["exit_code"]:
        raise RuntimeError("selected guest interface does not exist")
    verify_mac(network["stdout"], expected_mac)
    return {"hostname": platform.node(), "uid": os.getuid(), "virtualized": True,
            "interface": interface, "mac": expected_mac.lower()}


def verify_mac(ifconfig, expected):
    found = re.findall(r"\bether\s+((?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2})", ifconfig)
    if len(found) != 1 or found[0].lower() != expected.lower():
        raise RuntimeError("selected interface MAC differs from the identified VM")


def service_for_interface(output, interface):
    service, matches = None, []
    for line in output.splitlines():
        match = re.match(r"^\((\d+|\*)\)\s+(.+)$", line.strip())
        if match:
            service = ("*" if match[1] == "*" else "") + match[2]
        if re.search(r"\bDevice:\s*" + re.escape(interface) + r"\)", line):
            matches.append(service)
    if len(matches) != 1 or not matches[0] or matches[0].startswith("*"):
        raise RuntimeError("selected interface needs one enabled network service")
    return matches[0]


def save_json(path, row):
    path = Path(path)
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, prefix=".maintenance-", delete=False) as stream:
        temporary = Path(stream.name)
        try:
            json.dump(row, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        except BaseException:
            temporary.unlink(missing_ok=True)
            raise
    try:
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


@contextmanager
def case_guard(path):
    # Share the native runner's inode. Never create, truncate, or unlink it.
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except OSError as error:
        raise RuntimeError("existing native-case cleanup receipt is required") from error
    with os.fdopen(descriptor, "r", encoding="utf-8") as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError("another native case or maintenance owns the guest") from error
        try:
            receipt = json.loads(stream.read(65537))
            if (not isinstance(receipt, dict) or receipt.get("cleanup_complete") is not True
                    or receipt.get("remaining_owned_pids") != []):
                raise RuntimeError("previous native case is not completely cleaned")
        except ValueError as error:
            raise RuntimeError("native-case cleanup receipt is unreadable") from error
        yield receipt


def unfinished_claims(queue_roots, own_root, own_request):
    own_root = Path(own_root).resolve()
    roots = list(dict.fromkeys(Path(root).resolve(strict=True) for root in queue_roots))
    if own_root not in roots or not re.fullmatch(r"[A-Za-z0-9_-]+", own_request or ""):
        raise RuntimeError("current request must belong to one of the explicitly inspected peer queues")
    if not (own_root / "claims" / (own_request + ".json")).is_file():
        raise RuntimeError("current maintenance request has not been claimed by its queue")
    unfinished = []
    for root in roots:
        if not (root / "claims").is_dir() or not (root / "responses").is_dir():
            raise RuntimeError("peer queue lacks its claims or responses directory")
        for claim in sorted((root / "claims").glob("*.json")):
            row = json.loads(claim.read_text())
            if row.get("id") != claim.stem or not isinstance(row.get("nonce"), str) or not row["nonce"]:
                raise RuntimeError("peer claim identity differs from its filename")
            if root == own_root and claim.stem == own_request:
                continue
            response = root / "responses" / claim.name
            try:
                result = json.loads(response.read_text())
            except FileNotFoundError:
                result = {}
            if (result.get("id") != claim.stem or result.get("nonce") != row["nonce"] or result.get("status") != "exited"
                    or type(result.get("exit_code")) is not int):
                unfinished.append(str(claim))
    return unfinished


def require_quiet_peers(queue_roots, own_root, own_request):
    busy = unfinished_claims(queue_roots, own_root, own_request)
    if busy:
        raise RuntimeError("peer queue still has unfinished claims: " + ", ".join(busy))


def network_snapshot(interface, service, mac):
    info = command(["/usr/sbin/networksetup", "-getinfo", service])
    adapter = command(["/sbin/ifconfig", interface])
    if adapter["exit_code"]:
        raise RuntimeError("selected interface disappeared during maintenance")
    verify_mac(adapter["stdout"], mac)
    address = command(["/usr/sbin/ipconfig", "getifaddr", interface])
    route = command(["/sbin/route", "-n", "get", "default"])
    sockets = command(["/usr/sbin/netstat", "-an", "-p", "tcp"])
    listeners = [line for line in sockets["stdout"].splitlines() if "LISTEN" in line and re.search(r"\.22\s", line)]
    active = bool(re.search(r"^\s*status:\s+active\s*$", adapter["stdout"], re.MULTILINE))
    ip = address["stdout"].strip() if address["exit_code"] == 0 else ""
    default_route = route["exit_code"] == 0 and bool(re.search(r"interface:\s*" + re.escape(interface) + r"\s*$", route["stdout"], re.MULTILINE))
    return {"interface": interface, "service": service,
            "dhcp": info["exit_code"] == 0 and bool(re.search(r"^DHCP Configuration\b", info["stdout"], re.MULTILINE)),
            "active": active, "ip": ip, "default_route_on_interface": default_route,
            "ssh_listening": bool(listeners), "ready": active and bool(ip) and default_route and bool(listeners),
            "networksetup": info, "ifconfig": adapter, "address": address, "default_route": route,
            "ssh_listeners": listeners}


def sshd_state():
    job = command(["/bin/launchctl", "print", "system/com.openssh.sshd"])
    disabled = command(["/bin/launchctl", "print-disabled", "system"])
    loaded = True if job["exit_code"] == 0 else False if "could not find service" in job["stderr"].lower() else None
    match = re.search(r'"com\.openssh\.sshd"\s*=>\s*(true|false)', disabled["stdout"])
    return {"loaded": loaded, "disabled": match[1] == "true" if disabled["exit_code"] == 0 and match else None}


def descendant_pids(processes, parent):
    children, pending = set(), {parent}
    while pending:
        following = {pid for pid, row in processes.items() if row["ppid"] in pending} - children - {parent}
        children.update(following)
        pending = following
    return sorted(children)


def dead_peer_is_quiet(root, processes, controller_pid):
    if controller_pid and descendant_pids(processes, controller_pid):
        return False
    for path in (root / "claims").glob("*.json"):
        claim = json.loads(path.read_text())
        response = json.loads((root / "responses" / path.name).read_text())
        if (claim.get("id") != path.stem or not claim.get("nonce") or
                response.get("id") != path.stem or response.get("nonce") != claim["nonce"]):
            return False
        status = response.get("status")
        if status == "exited" and type(response.get("exit_code")) is int:
            continue
        if status == "interrupted" and type(response.get("pid")) is int and response["pid"] not in processes:
            continue
        if status == "rejected" and response.get("pid") is None:
            continue
        if status == "stopped":
            request = json.loads((root / "requests" / path.name).read_text())
            if request.get("stop") is True and request.get("id") == path.stem and request.get("nonce") == claim["nonce"]:
                continue
        return False
    return True


def current_peers(queue_roots, expected_uid):
    # A ready/stopped file is history. Process identity decides which roots can
    # still execute work; unknown effects in a dead queue cannot be discarded.
    result = {"current_peer_roots": None, "current_peer_controllers": []}
    try:
        snapshot = command(["/bin/ps", "-wwaxo", "pid=,ppid=,uid=,args="])
        if snapshot["exit_code"]:
            raise RuntimeError("process snapshot unavailable")
        processes = {}
        for line in snapshot["stdout"].splitlines():
            pid, ppid, uid, argv = line.strip().split(None, 3)
            processes[int(pid)] = {"ppid": int(ppid), "uid": int(uid), "argv": argv}
        roots, controllers = [], []
        for root in dict.fromkeys(Path(path) for path in queue_roots):
            session = json.loads((root / "session.json").read_text())
            if (session.get("protocol") != 1 or session.get("root") != str(root) or
                    not isinstance(session.get("nonce"), str) or not session["nonce"]):
                raise RuntimeError("peer session identity differs")
            try:
                ready = json.loads((root / "ready.json").read_text())
            except FileNotFoundError:
                ready = {}
            pid = ready.get("pid")
            process = processes.get(pid)
            prefix = str(root / "ci-queue-agent") + " --root " + str(root) + " --nonce " + session["nonce"] + " --ttl "
            if process and process["argv"].startswith(prefix):
                if (type(pid) is not int or process["uid"] != expected_uid or ready.get("uid") != expected_uid or
                        any(ready.get(key) != session[key] for key in ("protocol", "root", "nonce"))):
                    raise RuntimeError("peer controller identity differs")
                roots.append(str(root))
                controllers.append({"root": str(root), "pid": pid, "child_pids": descendant_pids(processes, pid)})
            elif not dead_peer_is_quiet(root, processes, pid):
                raise RuntimeError("historical peer effects remain unverified")
        known_pids = {row["pid"] for row in controllers}
        if any(pid not in known_pids and row["argv"].split(" --root ", 1)[0].endswith("/ci-queue-agent")
               for pid, row in processes.items()):
            raise RuntimeError("unidentified peer controller is still running")
        result.update(current_peer_roots=roots, current_peer_controllers=controllers)
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError):
        result["peer_inventory_error"] = "current_process_inventory_unverified"
    return result


def renew_dhcp(evidence, interface, service, mac, lock_path, queue_roots, own_root, own_request):
    with case_guard(lock_path):
        require_quiet_peers(queue_roots, own_root, own_request)
        before = network_snapshot(interface, service, mac)
        save_json(evidence / "network-before.json", before)
        if not before.get("dhcp"):
            raise RuntimeError("selected network service is not already configured for DHCP")
        require_quiet_peers(queue_roots, own_root, own_request)
        result = command(["/usr/bin/sudo", "-n", "/usr/sbin/ipconfig", "set", interface, "DHCP"])
        save_json(evidence / "dhcp-command.json", result)
        if result["exit_code"]:
            raise RuntimeError("one DHCP renewal failed; inspect dhcp-command.json before another action")
        deadline = time.monotonic() + 10
        while True:
            after = network_snapshot(interface, service, mac)
            save_json(evidence / "network-after.json", after)
            if after["ready"] or time.monotonic() >= deadline:
                break
            time.sleep(.25)
        return {"status": "renewed" if after["ready"] else "needs_host_recovery", "renewals": 1,
                "guest_network_ready": after["ready"], "host_connectivity": "unverified",
                "next_action": "verify host SSH" if after["ready"] else "inspect host VZ NAT and consider coordinated VM restart"}


def hold_maintenance(evidence, token, ttl, lock_path, queue_roots, own_root, own_request, identity=None):
    if not token or not math.isfinite(ttl) or ttl <= 0 or ttl > 180:
        raise ValueError("maintenance needs a nonempty own token and a TTL in (0, 180] seconds")
    receipt = {"token": token, "pid": os.getpid(), "identity": identity, "heartbeat": 0,
               "started_monotonic": time.monotonic(), "ttl_seconds": ttl, "status": "blocked", "lock_held": False}
    try:
        with case_guard(lock_path):
            deadline = time.monotonic() + ttl
            while True:
                require_quiet_peers(queue_roots, own_root, own_request)
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    receipt["status"] = "expired"
                    break
                release = evidence / "maintenance-release.json"
                try:
                    requested = json.loads(release.read_text())
                except (FileNotFoundError, ValueError):
                    requested = {}
                if requested.get("token") == token:
                    receipt["status"] = "released"
                    break
                receipt.update(status="holding", lock_held=True, heartbeat=receipt["heartbeat"] + 1,
                               observed_monotonic=time.monotonic(), remaining_seconds=remaining)
                save_json(evidence / "maintenance-ready.json", receipt)
                time.sleep(min(.2, remaining))
    except BaseException:
        receipt["status"] = "interrupted"
        raise
    finally:
        receipt.update(lock_held=False, lock_released=True, finished_monotonic=time.monotonic())
        save_json(evidence / "maintenance-ready.json", receipt)
        save_json(evidence / "maintenance-finished.json", receipt)
    return receipt


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("inspect", "renew-dhcp", "hold-maintenance"))
    parser.add_argument("--evidence", type=Path, required=True, help="new persistent evidence directory")
    parser.add_argument("--expected-uid", type=int, required=True)
    parser.add_argument("--expected-mac", required=True)
    parser.add_argument("--interface", required=True)
    parser.add_argument("--own-queue-root", type=Path)
    parser.add_argument("--request-id")
    parser.add_argument("--peer-queue-root", action="append", type=Path, default=[])
    parser.add_argument("--token")
    parser.add_argument("--ttl", type=float, default=90)
    args = parser.parse_args(argv)
    if args.action != "inspect" and (not args.own_queue_root or not args.request_id or not args.peer_queue_root):
        parser.error("maintenance requires the own queue root/request ID and every current peer queue root")
    if args.action == "hold-maintenance" and not args.token:
        parser.error("hold-maintenance requires an own token")
    evidence_created = False
    try:
        identity = require_identity(args.expected_uid, args.interface, args.expected_mac)
        args.evidence.mkdir(parents=True, exist_ok=False)
        evidence_created = True
        save_json(args.evidence / "identity.json", identity)
        lock = Path("/tmp") / ("zwai-native-macos-" + str(args.expected_uid) + ".lock")
        if args.action == "hold-maintenance":
            result = hold_maintenance(args.evidence, args.token, args.ttl, lock,
                                      args.peer_queue_root, args.own_queue_root, args.request_id, identity)
        else:
            listing = command(["/usr/sbin/networksetup", "-listnetworkserviceorder"])
            if listing["exit_code"]:
                raise RuntimeError("could not read guest network services")
            service = service_for_interface(listing["stdout"], args.interface)
            if args.action == "inspect":
                result = {"identity": identity, "network": network_snapshot(args.interface, service, args.expected_mac),
                          "sshd": sshd_state(), "host_connectivity": "unverified",
                          **current_peers(args.peer_queue_root, args.expected_uid)}
            else:
                result = renew_dhcp(args.evidence, args.interface, service, args.expected_mac, lock,
                                    args.peer_queue_root, args.own_queue_root, args.request_id)
        save_json(args.evidence / "result.json", result)
        print(json.dumps(result))
        return {"expired": 124, "needs_host_recovery": 3}.get(result.get("status"), 0)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        result = {"status": "refused", "failure": str(error), "error_type": type(error).__name__}
        if evidence_created:
            save_json(args.evidence / "failure.json", result)
        print(json.dumps(result))
        return 130 if isinstance(error, KeyboardInterrupt) else 2


if __name__ == "__main__":
    def interrupt(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    raise SystemExit(main())
