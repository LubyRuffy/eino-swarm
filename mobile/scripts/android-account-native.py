#!/usr/bin/env python3
"""Run the packaged account flow in one owned headless Android emulator.

Build the APK/test APK and account-fixture first. This runner never patches
shipping assets, installs on an existing device, or opens host GUI windows.
"""
import argparse
import contextlib
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import time
import zipfile


def private_json(path, value):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
        json.dump(value, output, indent=2)


def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def closed(number):
    with socket.socket() as listener:
        listener.settimeout(.25)
        return listener.connect_ex(("127.0.0.1", number)) != 0


def emulator_port():
    for number in range(5600, 5682, 2):
        if closed(number) and closed(number + 1):
            return number
    raise RuntimeError("No unoccupied emulator port pair")


def native_log_metadata(raw):
    """Keep only allowlisted bridge identities and fixed exception categories."""
    rows = []
    identity = re.compile(r"callbackId:\s*([0-9-]{1,32}),\s*pluginId:\s*(App|Browser|AppLauncher|NativeStreamBody|StreamBody),\s*methodName:\s*([A-Za-z_]{1,48})")
    console = re.compile(r"\b(native|result)\s+(App|Browser|AppLauncher|NativeStreamBody|StreamBody)\.([A-Za-z_]{1,48})\s*\(#([0-9-]{1,32})\)")
    categories = {"ActivityNotFoundException": "activity_not_found", "FATAL EXCEPTION": "native_fatal",
                  "Error binding to custom tabs service": "custom_tabs_binding", "NullPointerException": "null_pointer"}
    for line in raw.decode(errors="replace").splitlines():
        match = identity.search(line)
        if match:
            rows.append({"event": "native_call", "callback_id": match[1], "plugin": match[2], "method": match[3]})
        match = console.search(line)
        if match:
            rows.append({"event": match[1], "plugin": match[2], "method": match[3], "callback_id": match[4]})
        if "BrowserControllerActivity" in line:
            rows.append({"event": "browser_controller_activity"})
        for needle, category in categories.items():
            if needle in line:
                rows.append({"event": "exception", "category": category})
    return json.dumps(rows, indent=2).encode()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--apk", type=Path, required=True)
    parser.add_argument("--test-apk", type=Path, required=True)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--sdk", type=Path, required=True)
    parser.add_argument("--jdk", type=Path, required=True)
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--development", action="store_true", help="Record dirty-source development evidence; never release acceptance")
    parser.add_argument("--version", help="Expected release version; required for formal acceptance")
    parser.add_argument("--previous-apk", type=Path, help="Previous public APK for a real signature-compatible upgrade")
    args = parser.parse_args()
    if not 180 <= args.timeout <= 900:
        raise ValueError("A bounded 180–900 second window is required")
    source = args.source.resolve(strict=True)
    for item in (args.apk, args.test_apk, args.fixture):
        if not item.is_absolute() or item.is_symlink() or not item.is_file():
            raise ValueError("Exact absolute regular APK/fixture paths are required")
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True, timeout=5).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain=v1", "--untracked-files=all"], cwd=source, text=True, timeout=5).strip())
    if head != args.source_sha or (dirty and not args.development):
        raise ValueError("Native acceptance requires the exact clean candidate; use --development only for harness debugging")
    if not args.development and (not args.version or not args.previous_apk or not args.previous_apk.is_file()):
        raise ValueError("Formal acceptance requires the expected version and previous public APK")
    if args.evidence.exists() or not args.evidence.is_absolute():
        raise ValueError("A fresh absolute private evidence directory is required")
    args.evidence.mkdir(mode=0o700)
    out = args.evidence
    runtime = out / "owned-runtime"
    runtime.mkdir(mode=0o700)
    for name in ("avd", "android-user", "android-emulator", "fixture-data"):
        (runtime / name).mkdir(mode=0o700)
    sdk = args.sdk.resolve(strict=True)
    adb, emulator = sdk / "platform-tools/adb", sdk / "emulator/emulator"
    manager = sdk / "cmdline-tools/latest/bin/avdmanager"
    image = "system-images;android-33;google_apis;arm64-v8a"
    if not (sdk / "system-images/android-33/google_apis/arm64-v8a").is_dir():
        raise ValueError("Installed API33 arm64 Google APIs image is required; no SDK download is performed")
    adb_port, emu_port = port(), emulator_port()
    serial = f"emulator-{emu_port}"
    avd = "zwai_account_" + hashlib.sha256(str(out).encode()).hexdigest()[:12]
    env = dict(os.environ, ANDROID_HOME=str(sdk), ANDROID_SDK_ROOT=str(sdk), JAVA_HOME=str(args.jdk),
               ANDROID_AVD_HOME=str(runtime / "avd"), ANDROID_USER_HOME=str(runtime / "android-user"),
               ANDROID_EMULATOR_HOME=str(runtime / "android-emulator"),
               ANDROID_ADB_SERVER_PORT=str(adb_port), ADB_SERVER_SOCKET=f"tcp:127.0.0.1:{adb_port}",
               ADB_LOCAL_TRANSPORT_MAX_PORT="5553", ADB_MDNS_AUTO_CONNECT="",
               OPENAI_BASE_URL="", OPENAI_API_KEY="", OPENAI_MODEL="")
    env["PATH"] = str(args.jdk / "bin") + os.pathsep + env.get("PATH", "")
    processes, streams, commands = [], [], []
    started = time.monotonic()
    deadline = started + args.timeout
    hashes = {name: sha(path) for name, path in (("apk", args.apk), ("test_apk", args.test_apk), ("fixture", args.fixture))}
    result = {"source_sha": head, "dirty_development": dirty, "formal_acceptance": False,
              "fixture_scope": "synthetic CAS; actual native Browser, HTTP, PKCE and encrypted Pairlink/Eino RPC",
              "artifacts": hashes, "serial": serial, "private_adb_port": adb_port,
              "normal_child_umask": "0022", "production_NOSEC_acceptance": False}
    if args.previous_apk:
        result["previous_public_apk_sha256"] = sha(args.previous_apk)
    fixture_ports = []

    def start(label, argv):
        fds = []
        for suffix in ("stdout", "stderr"):
            handle = os.fdopen(os.open(out / f"{label}.{suffix}.log", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb")
            streams.append(handle); fds.append(handle)
        child = subprocess.Popen(list(map(str, argv)), cwd=source, env=env, stdin=subprocess.DEVNULL,
                                 stdout=fds[0], stderr=fds[1], start_new_session=True, umask=0o022)
        processes.append(child)
        commands.append({"label": label, "argv": list(map(str, argv)), "pid": child.pid})
        return child

    def run(label, argv, timeout=15, check=True, input=None):
        left = deadline - time.monotonic() - 30
        if left <= 0:
            raise TimeoutError("The original work window expired; cleanup reserve retained")
        child = subprocess.Popen(list(map(str, argv)), cwd=source, env=env, stdin=subprocess.PIPE if input else subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True, umask=0o077 if label == "pull-native-evidence" else 0o022)
        processes.append(child)
        row = {"label": label, "argv": list(map(str, argv)), "pid": child.pid}
        commands.append(row)
        try:
            stdout, stderr = child.communicate(input.encode() if input else None, timeout=min(timeout, left))
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGTERM)
            try: stdout, stderr = child.communicate(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL); stdout, stderr = child.communicate(timeout=3)
            row["timeout"] = True
            row["exit_code"] = child.returncode
            if label == "failure-native-logcat": stdout, stderr = native_log_metadata(stdout), b""
            for suffix, value in (("stdout", stdout), ("stderr", stderr)):
                with os.fdopen(os.open(out / f"{label}.{suffix}.log", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as handle: handle.write(value)
            raise
        row["exit_code"] = child.returncode
        if label == "failure-native-logcat": stdout, stderr = native_log_metadata(stdout), b""
        for suffix, value in (("stdout", stdout), ("stderr", stderr)):
            with os.fdopen(os.open(out / f"{label}.{suffix}.log", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as handle: handle.write(value)
        if check and child.returncode:
            raise RuntimeError(f"{label} failed; private original streams retained")
        return stdout.decode(errors="replace")

    def ad(label, command, **options):
        return run(label, [adb, "-P", str(adb_port), "-s", serial, *command], **options)

    primary = None
    server = fixture = None
    lease = None
    lease_entered = False
    try:
        sys.path.insert(0, str(source / "tools"))
        from release_host_lease import HostValidationLease
        lease = HostValidationLease(source, out, timeout=30)
        lease.__enter__()
        lease_entered = True
        build_tools = sorted((sdk / "build-tools").glob("*/apksigner"))[-1].parent
        signers = {}
        for label, apk in (("candidate", args.apk), ("test", args.test_apk), ("previous", args.previous_apk)):
            if apk is None:
                continue
            signature = run("signature-" + label, [build_tools / "apksigner", "verify", "--print-certs", apk])
            fingerprint = re.search(r"Signer #1 certificate SHA-256 digest: ([0-9a-f]+)", signature)
            if not fingerprint:
                raise ValueError("Verified APK signer fingerprint is required")
            signers[label] = fingerprint.group(1)
        if len(set(signers.values())) != 1:
            raise ValueError("Candidate, instrumentation and prior installer must have the same signer")
        result["signer_sha256"] = signers
        badging = run("candidate-badging", [build_tools / "aapt", "dump", "badging", args.apk])
        package = re.search(r"package: name='([^']+)' versionCode='([0-9]+)' versionName='([^']+)'", badging)
        if not package or package.group(1) != "com.lubyruffy.zwai":
            raise ValueError("The exact zwai package is required")
        result["apk_package"] = package.group(1)
        result["apk_version_code"] = int(package.group(2))
        result["apk_version_name"] = package.group(3)
        if not args.development:
            if package.group(3) != args.version or "application-debuggable" in badging:
                raise ValueError("Formal acceptance requires the requested non-debuggable release variant")
            dist = source / "mobile/dist"
            expected = {str(p.relative_to(dist)): sha(p) for p in dist.rglob("*") if p.is_file()}
            with zipfile.ZipFile(args.apk) as archive:
                actual = {name.removeprefix("assets/public/"): hashlib.sha256(archive.read(name)).hexdigest()
                          for name in archive.namelist() if name.startswith("assets/public/") and not name.endswith("/")}
            # Capacitor adds these two bridge files during its copy operation.
            actual = {name: value for name, value in actual.items() if name not in ("cordova.js", "cordova_plugins.js")}
            if not expected or actual != expected:
                raise ValueError("APK web assets differ from the exact release build's mobile/dist")
            result["packaged_web_assets"] = expected
        ready = runtime / "fixture-ready.json"
        fixture = start("fixture", [args.fixture, "--listen", "127.0.0.1:0", "--data-dir", runtime / "fixture-data", "--ready-file", ready])
        until = time.monotonic() + 20
        while not ready.exists():
            if fixture.poll() is not None or time.monotonic() >= until: raise RuntimeError("Owned fixture did not become ready")
            time.sleep(.1)
        fixture_state = json.loads(ready.read_bytes())
        for key in ("origin", "anonymous_origin"):
            value = fixture_state[key]
            if not re.fullmatch(r"http://127\.0\.0\.1:[0-9]+", value): raise ValueError("Fixture must bind loopback only")
            fixture_ports.append(int(value.rsplit(":", 1)[1]))
        private_json(out / "fixture-ready.json", fixture_state)
        run("create-private-avd", [manager, "create", "avd", "-n", avd, "-k", image, "-p", runtime / "avd" / (avd + ".avd")], timeout=30, input="no\n")
        server = start("private-adb", [adb, "-L", f"tcp:{adb_port}", "--one-device", serial, "server", "nodaemon"])
        until = time.monotonic() + 10
        while closed(adb_port):
            if server.poll() is not None or time.monotonic() >= until: raise RuntimeError("Private ADB server unavailable")
            time.sleep(.1)
        emu = start("headless-emulator", [emulator, "-avd", avd, "-port", str(emu_port), "-no-window", "-no-audio", "-no-snapshot", "-no-boot-anim", "-gpu", "swiftshader", "-memory", "2048", "-cores", "2"])
        boot_deadline = time.monotonic() + 100
        index = 0
        while time.monotonic() < boot_deadline:
            if emu.poll() is not None: raise RuntimeError("Owned headless emulator exited")
            boot = ad(f"boot-{index:03d}", ["shell", "getprop", "sys.boot_completed"], timeout=5, check=False)
            index += 1
            if boot.strip() == "1": break
            time.sleep(.5)
        else: raise TimeoutError("Owned emulator did not boot in 100 seconds")
        for index, number in enumerate(fixture_ports): ad(f"reverse-{index}", ["reverse", f"tcp:{number}", f"tcp:{number}"])
        ad("private-airplane-mode", ["shell", "cmd", "connectivity", "airplane-mode", "enable"])
        ad("private-wifi-off", ["shell", "svc", "wifi", "disable"])
        ad("private-mobile-data-off", ["shell", "svc", "data", "disable"])
        ad("private-data-setting-off", ["shell", "settings", "put", "global", "mobile_data", "0"])
        isolation = {name: ad("verify-private-" + name, ["shell", "settings", "get", "global", setting]).strip()
                     for name, setting in (("airplane", "airplane_mode_on"), ("wifi", "wifi_on"), ("mobile_data", "mobile_data"))}
        if isolation != {"airplane": "1", "wifi": "0", "mobile_data": "0"}:
            raise ValueError("Owned emulator must have no WiFi or mobile-data egress")
        network_deadline = time.monotonic() + 5
        network_attempt = 0
        while True:
            connectivity = ad(f"private-connectivity-{network_attempt}", ["shell", "dumpsys", "connectivity"])
            if re.search(r"Active default network:\s*none", connectivity, re.IGNORECASE):
                break
            if time.monotonic() >= network_deadline:
                raise ValueError("Owned emulator still has a default network; loopback-only isolation is unconfirmed")
            network_attempt += 1
            time.sleep(.1)
        result["owned_device_network_isolation"] = isolation
        result["owned_device_default_network"] = "none"
        if args.previous_apk:
            ad("install-previous-public", ["install", str(args.previous_apk)], timeout=40)
        ad("install-formal", ["install", "-r", str(args.apk)], timeout=40)
        ad("install-instrumentation", ["install", "-t", "-r", str(args.test_apk)], timeout=40)
        package = ad("installed-package", ["shell", "dumpsys", "package", "com.lubyruffy.zwai"])
        result["installed_version_name"] = re.search(r"versionName=([^\s]+)", package).group(1)
        result["installed_version_code"] = int(re.search(r"versionCode=([0-9]+)", package).group(1))
        if (result["installed_version_name"], result["installed_version_code"]) != (result["apk_version_name"], result["apk_version_code"]):
            raise ValueError("Installed APK identity differs from the accepted package")
        result["public_upgrade_verified"] = bool(args.previous_apk)
        installed_path = ad("installed-apk-path", ["shell", "pm", "path", "com.lubyruffy.zwai"]).strip()
        if not re.fullmatch(r"package:/data/app/[^\r\n]+/base\.apk", installed_path):
            raise ValueError("One installed base APK is required")
        installed_digest = ad("installed-apk-sha256", ["shell", "sha256sum", installed_path.removeprefix("package:")]).split()[0]
        if installed_digest != hashes["apk"]:
            raise ValueError("Installed APK bytes differ from the supplied candidate")
        result["installed_apk_sha256"] = installed_digest
        output = ad("native-account", ["shell", "am", "instrument", "-w", "-r", "-e", "fixture_origin", fixture_state["origin"], "-e", "class", "com.lubyruffy.zwai.AccountLoginTest#accountLoginReviewModelsPCAndAnonymousPairing", "com.lubyruffy.zwai.test/androidx.test.runner.AndroidJUnitRunner"], timeout=150, check=False)
        directories = re.findall(r"INSTRUMENTATION_STATUS: evidence_directory=([^\r\n]+)", output)
        if len(directories) == 1:
            ad("pull-native-evidence", ["pull", directories[0], str(out / "native-evidence")], timeout=20)
        if "OK (1 test)" not in output or any(x in output for x in ("FAILURES", "INSTRUMENTATION_FAILED", "INSTRUMENTATION_ABORTED")):
            raise AssertionError("Native account case failed; original streams and reached screen retained")
        if len(directories) != 1: raise AssertionError("Native case did not retain its unique evidence directory")
        result["native_case_passed"] = True
    except BaseException as error:
        primary = error
        result["failure"] = {"type": type(error).__name__, "message": str(error)}
        if fixture is not None and fixture.poll() is None and fixture_ports:
            try:
                connection = http.client.HTTPConnection("127.0.0.1", fixture_ports[0], timeout=2)
                try:
                    connection.request("GET", "/fixture/state")
                    response = connection.getresponse()
                    if response.status == 200:
                        private_json(out / "failure-fixture-state.json", json.loads(response.read(65536)))
                finally:
                    connection.close()
            except BaseException as diagnostic_error:
                result["fixture_diagnostic_error"] = type(diagnostic_error).__name__
        if server is not None and server.poll() is None:
            try:
                target_pid = ad("failure-target-pid", ["shell", "pidof", "com.lubyruffy.zwai"], check=False).strip()
                if re.fullmatch(r"[0-9]+", target_pid):
                    ad("failure-native-logcat", ["logcat", "--pid", target_pid, "-d", "-v", "threadtime", "-t", "2000"], timeout=10, check=False)
            except BaseException as diagnostic_error:
                result["native_diagnostic_error"] = type(diagnostic_error).__name__
    finally:
        stopped = []
        for child in reversed(processes):
            if child.poll() is None:
                with contextlib.suppress(ProcessLookupError): os.killpg(child.pid, signal.SIGTERM)
                try: child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    with contextlib.suppress(ProcessLookupError): os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=5)
            stopped.append({"pid": child.pid, "returncode": child.returncode})
        for stream in streams: stream.close()
        untouched = hashes == {name: sha(path) for name, path in (("apk", args.apk), ("test_apk", args.test_apk), ("fixture", args.fixture))}
        cleanup_deadline = time.monotonic() + 5
        while True:
            all_ports_closed = all(closed(number) for number in (adb_port, emu_port, emu_port + 1, *fixture_ports))
            groups_gone = True
            for child in processes:
                try: os.killpg(child.pid, 0); groups_gone = False
                except ProcessLookupError: pass
            if all_ports_closed and groups_gone or time.monotonic() >= cleanup_deadline:
                break
            time.sleep(.1)
        cleanup = {"owned_processes": stopped, "ports_closed": all_ports_closed, "owned_process_groups_gone": groups_gone, "artifact_bytes_unchanged": untouched}
        if all_ports_closed and groups_gone and all(row["returncode"] is not None for row in stopped):
            shutil.rmtree(runtime)
        cleanup["owned_runtime_removed"] = not runtime.exists()
        private_json(out / "commands.json", commands)
        private_json(out / "cleanup.json", cleanup)
        result["cleanup_complete"] = cleanup["owned_runtime_removed"] and all_ports_closed and untouched
        result["formal_acceptance"] = bool(result.get("native_case_passed") and result["cleanup_complete"] and not args.development and not dirty)
        result["elapsed_seconds"] = time.monotonic() - started
        private_json(out / "result.json", result)
        if lease_entered:
            try:
                lease.__exit__(None, None, None)
            except BaseException:
                if primary is None:
                    raise
    if primary is not None: raise primary
    if not result["cleanup_complete"]: raise RuntimeError("Owned cleanup was incomplete")
    print(json.dumps({"native_case_passed": True, "formal_acceptance": result["formal_acceptance"], "evidence": str(out)}))


if __name__ == "__main__":
    main()
