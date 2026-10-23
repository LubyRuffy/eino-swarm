#!/usr/bin/env python3
"""Collect current ci-macOS access evidence without rebooting or opening a GUI."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import runpy
import shlex
import subprocess
import time
import uuid


decide = runpy.run_path(str(Path(__file__).with_name("native_macos_access_state.py")))["decide"]
GUEST_SOURCE = Path(__file__).with_name("native_macos_access_guest.py")
LEASES = Path("/var/db/dhcpd_leases")
BOOTSTRAP = """import hashlib,json,pathlib,subprocess,sys
source=pathlib.Path(sys.argv[1]); digest=hashlib.sha256(source.read_bytes()).hexdigest()
if digest != sys.argv[2]: raise RuntimeError('guest source hash differs')
child=subprocess.run([sys.executable,str(source)]+sys.argv[5:],capture_output=True,text=True,
                     timeout=90 if sys.argv[4]=='renew-dhcp' else 20)
print(json.dumps(dict(probe_id=sys.argv[3],source_sha256=digest,guest_path=str(source),
                     action=sys.argv[4],exit_code=child.returncode,result=json.loads(child.stdout))))
sys.exit(child.returncode)
"""


def save_json(path, row):
    with open(path, "x", encoding="utf-8") as stream:
        os.chmod(path, 0o600)
        json.dump(row, stream, indent=2)
        stream.write("\n")


def load_profile(path):
    path = Path(path)
    if path.stat().st_uid != os.getuid() or path.stat().st_mode & 0o077:
        raise ValueError("profile must be private to its owner")
    row = json.loads(path.read_text())
    fields = {"vm_id", "vm_name", "mac", "user", "uid", "interface", "identity_file",
              "known_hosts", "host_key_alias", "shared_host_root", "shared_guest_root"}
    if not isinstance(row, dict) or row.keys() != fields:
        raise ValueError("profile fields differ from the supported schema")
    if any(not isinstance(row[key], str) or not row[key].strip() for key in fields - {"uid"}):
        raise ValueError("profile references must be nonempty strings")
    uuid.UUID(row["vm_id"])
    if row["vm_name"].lower() != "ci-macos" or row["user"] != "ci" or type(row["uid"]) is not int or row["uid"] <= 0:
        raise ValueError("profile must identify the authorized ci-macOS account")
    if not re.fullmatch(r"(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}", row["mac"]) or not re.fullmatch(r"en[0-9]+", row["interface"]):
        raise ValueError("profile needs an explicit interface and MAC")
    for key in ("identity_file", "known_hosts", "shared_host_root", "shared_guest_root"):
        if not isinstance(row[key], str) or not Path(row[key]).is_absolute():
            raise ValueError("profile paths must be absolute")
    if not isinstance(row["host_key_alias"], str) or not row["host_key_alias"].strip():
        raise ValueError("profile needs a pinned SSH host alias")
    return row


def run(argv, timeout=5):
    try:
        result = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        return {"exit_code": result.returncode, "stdout": result.stdout, "stderr": result.stderr, "timed_out": False}
    except subprocess.TimeoutExpired:
        return {"exit_code": None, "stdout": "", "stderr": "", "timed_out": True}


def candidate_ips(leases, mac):
    found = []
    for block in reversed(re.findall(r"\{([^{}]*)\}", leases)):
        address = re.search(r"\bip_address=([^\s]+)", block)
        hardware = re.search(r"\bhw_address=1,([^\s]+)", block)
        if hardware and address and hardware[1].lower() == mac.lower():
            try:
                ip = ipaddress.IPv4Address(address[1])
                if not (ip.is_loopback or ip.is_multicast or ip.is_unspecified) and str(ip) not in found:
                    found.append(str(ip))
            except ipaddress.AddressValueError:
                pass
    return found


def route_kind(output):
    match = re.search(r"\binterface:\s*(\S+)", output)
    return ("unknown" if not match else "proxy" if match[1].startswith("utun") else
            "vm" if match[1].startswith(("bridge", "vmenet")) else "other")


def queues(profile, include_stopped=False):
    root = Path(profile["shared_host_root"]) / "dist"
    found = []
    for session in sorted(root.rglob("session.json")):
        try:
            directory = session.parent
            directory.resolve().relative_to(root.resolve())
            row = json.loads(session.read_text())
            guest_root = str(Path(profile["shared_guest_root"]) / "dist" / directory.relative_to(root))
            if row.get("protocol") != 1 or row.get("root") != guest_root or not isinstance(row.get("nonce"), str) or not row["nonce"]:
                continue
            if not include_stopped:
                ready = json.loads((directory / "ready.json").read_text())
                if (directory / "stopped.json").exists() or any(ready.get(key) != row[key] for key in ("protocol", "root", "nonce")):
                    continue
            found.append({"host": directory, "guest": guest_root, "nonce": row["nonce"]})
        except (OSError, ValueError):
            continue
    return found


def queue_call(queue, argv_factory, timeout, evidence=None):
    request_id = uuid.uuid4().hex
    request = {"id": request_id, "nonce": queue["nonce"], "argv": argv_factory(queue["guest"], request_id),
               "cwd": queue["guest"], "timeout_seconds": int(max(1, min(90, timeout)))}
    if (not request["argv"] or len(request["argv"]) > 64 or not Path(request["argv"][0]).is_absolute() or
            not Path(request["cwd"]).is_absolute() or len(json.dumps(request).encode()) > 65536):
        raise ValueError("request exceeds the existing queue protocol")
    if evidence:
        save_json(evidence / ("request-" + request_id + ".json"), {"request_id": request_id, "queue_root": queue["guest"], "timeout_seconds": request["timeout_seconds"]})
    pending = queue["host"] / "requests" / (request_id + ".json")
    temporary = pending.with_suffix(".tmp")
    save_json(temporary, request)
    os.replace(temporary, pending)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            response = json.loads((queue["host"] / "responses" / pending.name).read_text())
            if not isinstance(response, dict) or response.get("id") != request_id or response.get("nonce") != queue["nonce"] or response.get("protocol") != 1:
                raise ValueError("queue reply identity differs")
            if response.get("status") != "exited" or type(response.get("exit_code")) is not int or response.get("forced_kill"):
                raise ValueError("queue execution did not exit normally")
            for field in ("stdout", "stderr"):
                expected = "logs/" + request_id + "." + field + ".log"
                if response.get(field) != expected or response.get(field + "_truncated") is not False:
                    raise ValueError("queue log identity differs or is incomplete")
                log = queue["host"] / expected
                log.resolve().relative_to(queue["host"].resolve())
                request[field] = log.read_text()
            return {"exit_code": response["exit_code"], "stdout": request["stdout"], "timed_out": False, "request_id": request_id}
        except FileNotFoundError:
            time.sleep(.05)
    # Removing our unclaimed request cannot prove that a reader did not take it.
    pending.unlink(missing_ok=True)
    return {"exit_code": None, "stdout": "", "timed_out": True, "request_id": request_id, "cancellation": "unconfirmed"}


def freeze(profile, evidence):
    name = "native-access-" + uuid.uuid4().hex
    directory = Path(profile["shared_host_root"]) / "dist" / name
    directory.mkdir(mode=0o700)
    content = GUEST_SOURCE.read_bytes()
    (directory / "guest.py").write_bytes(content)
    (directory / "guest.py").chmod(0o444)
    frozen = {"guest_path": str(Path(profile["shared_guest_root"]) / "dist" / name / "guest.py"),
              "source_sha256": hashlib.sha256(content).hexdigest()}
    save_json(evidence / "source.json", frozen)
    return frozen


def stage_argv(profile, frozen, probe_id, action, queue_root, request_id, peer_roots):
    stage = str(Path(frozen["guest_path"]).parent / (action + "-" + request_id))
    args = ["/usr/bin/python3", "-c", BOOTSTRAP, frozen["guest_path"], frozen["source_sha256"], probe_id, action,
            action, "--evidence", stage, "--expected-uid", str(profile["uid"]),
            "--expected-mac", profile["mac"], "--interface", profile["interface"]]
    if action != "inspect":
        args += ["--own-queue-root", queue_root, "--request-id", request_id]
    for root in peer_roots:
        args += ["--peer-queue-root", root]
    return args


def validate_stage(raw, frozen, probe_id, action, profile):
    row = json.loads(raw["stdout"])
    if not isinstance(row, dict) or any(row.get(key) != value for key, value in {**frozen, "probe_id": probe_id, "action": action}.items()):
        raise ValueError("guest stage does not match frozen source and current probe")
    if type(row.get("exit_code")) is not int or row["exit_code"] != raw["exit_code"]:
        raise ValueError("guest stage exit status differs")
    if action == "inspect":
        identity = row["result"]["identity"]
        if (identity.get("virtualized") is not True or identity.get("uid") != profile["uid"] or
                identity.get("mac", "").lower() != profile["mac"].lower() or identity.get("interface") != profile["interface"] or
                identity.get("hostname", "").lower().removesuffix(".local") != profile["vm_name"].lower()):
            raise ValueError("guest stage identity differs from the profile")
    return row


def collect(profile, evidence, require_queue=False):
    probe_id, frozen = uuid.uuid4().hex, freeze(profile, evidence)
    row = {"target": profile["vm_id"], "probe_id": probe_id,
           "host": {"vm_network_present": None, "old_ip_route": "unknown"},
           "guest": dict(probe_id=None, identity_matches=None, hypervisor=None, link="unknown", ipv4_present=None,
                         default_route_present=None, sshd_loaded=None, sshd_disabled=None),
           "channels": {"ssh": "unknown", "utm_exec": "unknown", "shared_queue": {"probe_id": None, "result": "unknown"}},
           "maintenance": dict(probe_id=None, lock="unknown", cleanup_complete=None, outstanding_claims=None)}
    interfaces = run(["/sbin/ifconfig", "-l"])
    if interfaces["exit_code"] == 0:
        row["host"]["vm_network_present"] = any(name == "bridge100" or name.startswith("vmenet") for name in interfaces["stdout"].split())
    candidates = candidate_ips(LEASES.read_text() if LEASES.exists() else "", profile["mac"])
    all_queues = queues(profile)
    peer_roots = [queue["guest"] for queue in queues(profile, include_stopped=True)]
    deadline, accepted, selected = time.monotonic() + 20, None, None
    host_routes, attempts = [], []
    for ip in candidates:
        if time.monotonic() >= deadline:
            break
        kind = route_kind(run(["/sbin/route", "-n", "get", ip], min(2, max(.1, deadline - time.monotonic())))["stdout"])
        host_routes.append({"ip": ip, "route": kind})
        row["host"]["old_ip_route"] = kind
        if kind == "proxy":
            continue
        command = ["ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "IdentitiesOnly=yes",
                   "-o", "UserKnownHostsFile=" + profile["known_hosts"], "-o", "HostKeyAlias=" + profile["host_key_alias"],
                   "-i", profile["identity_file"], "-l", profile["user"], ip,
                   shlex.join(stage_argv(profile, frozen, probe_id, "inspect", "", uuid.uuid4().hex, peer_roots))]
        raw = run(command, min(5, max(.1, deadline - time.monotonic())))
        row["channels"]["ssh"] = "timeout" if raw["timed_out"] else "auth_failed" if "Permission denied" in raw["stderr"] else "refused" if "Connection refused" in raw["stderr"] else "unknown"
        if raw["exit_code"] == 0:
            try:
                accepted = validate_stage(raw, frozen, probe_id, "inspect", profile)
                row["channels"]["ssh"] = "ok"
                break
            except (KeyError, TypeError, ValueError):
                pass
    save_json(evidence / "host.json", {**row["host"], "routes": host_routes, "ssh": row["channels"]["ssh"]})
    for queue in all_queues if accepted is None or require_queue else []:
        if time.monotonic() >= deadline:
            break
        try:
            raw = queue_call(queue, lambda root, request: stage_argv(profile, frozen, probe_id, "inspect", root, request, peer_roots),
                             min(3, deadline - time.monotonic()), evidence)
            attempts.append({key: value for key, value in raw.items() if key != "stdout"})
            row["channels"]["shared_queue"]["result"] = "timeout" if raw["timed_out"] else "unknown"
            if raw["exit_code"] == 0:
                accepted = validate_stage(raw, frozen, probe_id, "inspect", profile)
                selected = queue
                row["channels"]["shared_queue"] = {"probe_id": probe_id, "result": "ok"}
                break
        except (OSError, KeyError, TypeError, ValueError) as error:
            attempts.append({"error_type": type(error).__name__})
            continue
    save_json(evidence / "queue.json", {"candidates": len(all_queues), "attempts": attempts, "result": row["channels"]["shared_queue"]["result"]})
    if accepted:
        save_json(evidence / "guest.json", accepted)
        result, network = accepted["result"], accepted["result"]["network"]
        row["guest"].update(probe_id=probe_id, identity_matches=True, hypervisor=True,
                            link="active" if network["active"] else "inactive", ipv4_present=bool(network["ip"]),
                            default_route_present=network["default_route_on_interface"],
                            sshd_loaded=result.get("sshd", {}).get("loaded"), sshd_disabled=result.get("sshd", {}).get("disabled"))
    save_json(evidence / "observation.json", row)
    return row, frozen, selected, accepted


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--repair-network", action="store_true")
    args = parser.parse_args(argv)
    try:
        profile = load_profile(args.profile)
        args.evidence.mkdir(mode=0o700, parents=True, exist_ok=False)
        row, frozen, selected, guest = collect(profile, args.evidence, args.repair_network)
        decision = decide(row)
        save_json(args.evidence / "decision.json", decision)
        code = 0 if decision["control_channel"] else 3
        repair = {"status": "not_requested"}
        if args.repair_network:
            peers = (guest or {}).get("result", {}).get("current_peer_roots")
            if selected is None or not isinstance(peers, list) or selected["guest"] not in peers:
                repair = {"status": "refused", "reason": "needs_fresh_queue_for_guarded_repair" if selected is None else "current_peer_inventory_unverified"}
                code = 3
            else:
                raw = queue_call(selected, lambda root, request: stage_argv(profile, frozen, row["probe_id"], "renew-dhcp", root, request, peers), 100, args.evidence)
                repair = validate_stage(raw, frozen, row["probe_id"], "renew-dhcp", profile) if raw["exit_code"] is not None else {"status": "outcome_unknown", "reason": "own_request_deadline"}
                repair.update({**{key: value for key, value in raw.items() if key != "stdout"}, "queue_root": selected["guest"]})
                code = 0 if repair.get("exit_code") == 0 and repair.get("result", {}).get("status") == "renewed" else 3
                if code == 0:
                    after = args.evidence / "after"
                    after.mkdir(mode=0o700)
                    post, _, _, _ = collect(profile, after)
                    decision = decide(post)
                    save_json(after / "decision.json", decision)
                    repair["host_ssh_verified"] = post["channels"]["ssh"] == "ok"
                    code = 0 if repair["host_ssh_verified"] else 3
            save_json(args.evidence / "repair.json", repair)
        repair_status = repair.get("result", repair).get("status")
        if repair_status == "renewed":
            repair_status = "host_ssh_verified" if repair.get("host_ssh_verified") else "guest_renewed_host_unverified"
        print(json.dumps({**decision, "repair_requested": args.repair_network, "repair_status": repair_status,
                          "rebuild": "requires_owned_maintenance", "evidence": str(args.evidence)}))
        return code
    except (OSError, KeyError, TypeError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(json.dumps({"status": "failed", "error_type": type(error).__name__, "native_acceptance": "pending", "evidence": str(args.evidence)}))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
