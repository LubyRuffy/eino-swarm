#!/usr/bin/env python3
"""Accept MCP configuration/save/catalog controls in one private ci-macOS case.

API setup seeds a disabled, harmless stdio fixture. Native actions navigate,
save the existing form, inspect the connection and expand its schema. This does
not accept model tool execution, which has separate real-adapter Go tests.
"""
import argparse
import hashlib
import json
import os
import math
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

import native_goal_resume_notice as evidence
import native_macos_smoke as smoke
import native_macos_mcp_startup as startup


CONTROL_ROLES = ("AXButton", "AXRadioButton", "AXCheckBox")


def visible_text(row):
    return "\n".join(str(node.get(key, "")) for node in row.get("tree", [])
                     for key in ("AXTitle", "AXDescription", "AXValue"))


def assert_settings_window(row, pid):
    # The Settings page legitimately has zero or several textareas. Its window
    # and origin still need the same positive identity and visibility proof.
    rect = row.get("rect", [])
    window_id = row.get("window_id")
    if (type(pid) is not int or type(row.get("pid")) is not int or row["pid"] != pid
            or row.get("case_verified") is not True or row.get("frontmost") is not True
            or row.get("has_web_area") is not True or not row.get("title")
            or row.get("window_count") != 1 or type(window_id) is not int or window_id <= 0
            or row.get("window_owner_pid") != pid or row.get("screen_capture_allowed") is not True
            or row.get("windows") != [{"id": window_id, "owner_pid": pid}]
            or len(rect) != 4 or any(type(n) not in (int, float) or not math.isfinite(n) for n in rect)
            or rect[2] <= 0 or rect[3] <= 0):
        raise RuntimeError("native MCP Settings lacks an identified, visible case window")
    smoke.assert_web_origin(row, pid)


def exact_control(row, label):
    matches = [node for node in row.get("tree", [])
               if node.get("AXRole") in CONTROL_ROLES
               and label in (node.get("AXTitle"), node.get("AXDescription"))]
    if len(matches) > 1:
        raise RuntimeError("native MCP control is ambiguous: " + label)
    if matches and matches[0].get("AXEnabled") is True:
        return matches[0]
    return None


def catalog_control(row):
    node = exact_control(row, "echo")
    if node and node["AXRole"] != "AXButton":
        raise RuntimeError("native MCP catalog disclosure is not an actionable button")
    return "echo" if node else None


def assert_schema(row):
    text = visible_text(row)
    if not all(value in text for value in ("Input schema", '"properties"', '"required"', '"text"')):
        raise RuntimeError("native MCP schema has not been expanded and rendered")


def snapshot(context, deadline, expected=""):
    row = smoke.native_snapshot(context, deadline, expected=expected)
    if row.get("window_count") == 0 and row.get("registration_pending"):
        return None
    if row.get("has_web_area") is not True:
        return None
    assert_settings_window(row, context["app_pid"])
    return row


def wait_view(context, deadline, predicate, stage, expected=""):
    while True:
        deadline.remaining(stage)
        row = snapshot(context, deadline, expected)
        if row is not None and predicate(row):
            return row
        deadline.pause(stage)


def helper(context, deadline, which, action, value=""):
    smoke.assert_process(context, context["app_pid"], "desktop")
    binary = context[which]
    if hashlib.sha256(binary.read_bytes()).hexdigest() != context[which + "_sha256"]:
        raise RuntimeError("prepared native MCP helper changed")
    if action != "image":
        guard = snapshot(context, deadline)
        if guard is None:
            raise RuntimeError("native MCP action has no current private window")
        if action == "press":
            node = exact_control(guard, value)
            if node is None or (which == "resilience" and node["AXRole"] != "AXButton"):
                raise RuntimeError("native MCP action lost its exact page control")
    if which == "controls":
        command = [str(binary), str(context["app_pid"]), action]
        if value:
            command.append(value)
    else:
        command = [str(binary), action, str(context["app_pid"]), value]
    result = smoke.observed_command(context, deadline, command, "mcp-" + action, 8)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command, result.stdout, result.stderr)
    row = json.loads(result.stdout)
    if action != "image" and row.get("pid") != context["app_pid"]:
        raise RuntimeError("native MCP helper returned another PID")
    if which == "controls" and row.get("ax_error") != 0:
        raise RuntimeError("native MCP navigation was not acknowledged")
    return row


def press(context, deadline, label):
    row = wait_view(context, deadline, lambda row: exact_control(row, label), "MCP control " + label)
    node = exact_control(row, label)
    which = "resilience" if node["AXRole"] == "AXButton" else "controls"
    return helper(context, deadline, which, "press", label)


def api(context, deadline, path, body=None, method=None):
    request = urllib.request.Request(context["origin"] + path,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Accept": "application/json", "Content-Type": "application/json"}, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), smoke.NoRedirect())
    with opener.open(request, timeout=min(8, deadline.remaining("MCP API"))) as response:
        return json.load(response)


def save_window(context, deadline, name, row):
    evidence.save_window(context, deadline, name, row)
    image = context["evidence"] / (name + ".png")
    pixels = helper(context, deadline, "resilience", "image", str(image))
    if pixels.get("buckets", 0) < 16 or pixels.get("dominant_fraction", 1) > .985:
        raise RuntimeError("native MCP window screenshot is blank")
    smoke.runner.save_json(context["evidence"] / (name + ".pixels.json"), pixels)


def run(context):
    smoke.runner.require_guest()
    deadline = smoke.Deadline(context["timeout"])
    context["ax_identity"] = smoke.validate_helper(context["ax"])
    binding = startup.wait_product_ready(context, context["candidate"], context["launch"], deadline)
    context["readiness_stage"] = "window"
    window = smoke.wait_window(context, deadline, initial=True)
    save_window(context, deadline, "mcp-00-ready", window)
    settings = api(context, deadline, "/api/settings")["settings"]
    api(context, deadline, "/api/settings", {"ui": {**settings["ui"], "locale": "en"}}, "PUT")
    server_id = "native-mcp"
    name = "Native MCP " + context["case_id"][:8]
    fixture = Path(__file__).with_name("mcp_fixture.py").resolve(strict=True)
    server = {"id": server_id, "name": name, "transport": "stdio", "command": sys.executable,
              "args": [str(fixture)], "env": {"MCP_ACCEPTANCE": "private-fixture-marker"},
              "headers": {}, "url": "", "enabled": False, "timeout_seconds": 10}
    saved = api(context, deadline, "/api/mcp/" + server_id, server, "PUT")["server"]
    config_path = context["data"] / "config.yaml"
    config_before = config_path.read_bytes()
    helper(context, deadline, "controls", "reload")
    press(context, deadline, "Settings")
    press(context, deadline, "MCP")
    row = wait_view(context, deadline, lambda row: name in visible_text(row), "native MCP server list")
    save_window(context, deadline, "mcp-01-connections", row)
    press(context, deadline, "Edit " + name)
    row = wait_view(context, deadline, lambda row: "Arguments (JSON array)" in visible_text(row), "native MCP editor")
    if "private-fixture-marker" in visible_text(row):
        raise RuntimeError("native MCP editor exposed a private environment value")
    save_window(context, deadline, "mcp-02-editor", row)
    press(context, deadline, "Save connection")
    wait_view(context, deadline, lambda row: exact_control(row, "Edit " + name), "saved MCP editor closed")
    after = api(context, deadline, "/api/mcp")["servers"]
    if after != [saved]:
        raise RuntimeError("native save changed the configured MCP server or dropped private keys")
    if config_path.read_bytes() != config_before:
        raise RuntimeError("native MCP save changed the persisted configuration bytes")
    if config_path.stat().st_mode & 0o777 != 0o600:
        raise RuntimeError("native MCP configuration file is not private")
    press(context, deadline, "Test " + name + " and list tools")
    row = wait_view(context, deadline, lambda row: catalog_control(row), "native MCP tool catalog")
    if "Connected · 1 tools" not in visible_text(row):
        raise RuntimeError("native MCP catalog lacks the successful connection result")
    tool_name = catalog_control(row)
    press(context, deadline, tool_name)
    row = wait_view(context, deadline, lambda row: '"properties"' in visible_text(row),
                    "expanded native MCP schema", expected='"properties"')
    assert_schema(row)
    if row.get("expected_visible") is not True:
        raise RuntimeError("native MCP schema did not intersect the real window")
    save_window(context, deadline, "mcp-03-expanded-schema", row)
    receipt = {"passed": True, "case_id": context["case_id"], "app_pid": context["app_pid"],
               "engine_pid": binding["engine"]["pid"], "server_id": server_id, "tool_name": tool_name,
               "fixture_sha256": hashlib.sha256(fixture.read_bytes()).hexdigest(),
               "config_saved_unchanged": True, "private_keys_retained": True,
               "disabled_server_inspected": True, "schema_visible": True,
               "scope": "native MCP configuration save and tool catalog; model invocation accepted separately",
               "elapsed_seconds": time.monotonic() - deadline.started}
    smoke.runner.save_json(context["evidence"] / "native-mcp.json", receipt)
    return receipt


def run_baseline(context):
    """Observe this launch's paint first, then reuse the ordinary smoke deadline."""
    smoke.runner.require_guest()
    deadline = smoke.Deadline(context["timeout"])
    startup.wait_product_ready(context, context["candidate"], context["launch"], deadline)
    receipt = smoke.run_smoke(context, deadline=deadline)
    receipt.update(source_sha=context["candidate"]["baseline"],
                   production_inputs_sha256=context["candidate"]["production_inputs_sha256"],
                   archive_sha256=context["candidate"]["archive_sha256"],
                   scope="ordinary native offline-turn baseline for the identified private development candidate")
    smoke.runner.save_json(context["evidence"] / "smoke.json", receipt)
    return receipt


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prepare-candidate", type=Path, help="freeze a private development candidate source receipt")
    parser.add_argument("--candidate", type=Path, help="frozen candidate manifest (output for --prepare-candidate)")
    parser.add_argument("--controller-descriptor", type=Path)
    parser.add_argument("--controller-sha256")
    parser.add_argument("--launch-manifest", type=Path)
    parser.add_argument("--ax", type=Path)
    parser.add_argument("--controls", type=Path)
    parser.add_argument("--control-sha256")
    parser.add_argument("--resilience", type=Path)
    parser.add_argument("--resilience-sha256")
    parser.add_argument("--case", action="store_true")
    parser.add_argument("--smoke-baseline", action="store_true", help="run the ordinary offline-turn smoke after the same product-ready admission")
    parser.add_argument("--app", type=Path)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--timeout", type=float, default=90)
    parser.add_argument("--budget", type=float, default=120)
    args = parser.parse_args(argv)
    if args.prepare_candidate:
        if args.candidate is None or args.controller_descriptor is None or not args.controller_sha256:
            parser.error("candidate preparation needs --candidate and the frozen trusted controller descriptor/hash")
        manifest = startup.freeze_candidate(args.prepare_candidate, args.controller_descriptor, args.controller_sha256)
        smoke.runner.save_json(args.candidate, manifest)
        print(json.dumps({"candidate": str(args.candidate), "archive_sha256": manifest["archive_sha256"]}))
        return 0
    if not all((args.candidate, args.ax, args.controls, args.control_sha256, args.resilience, args.resilience_sha256)):
        parser.error("a case needs --candidate, --ax and both prepared control helpers/hashes")
    candidate_path = args.candidate.resolve(strict=True)
    manifest = startup.read_object(candidate_path)
    if not isinstance(manifest.get("controller"), dict):
        parser.error("candidate manifest must bind the current trusted controller descriptor")
    if args.case:
        if args.app is None or args.evidence is None:
            parser.error("--case requires --app and --evidence")
        startup.validate_candidate(manifest, args.app)
        launch_path = args.evidence.absolute() / "mcp-launch.json"
        command = [sys.executable, str(Path(__file__).resolve()), "--candidate", str(candidate_path),
                   "--launch-manifest", str(launch_path), "--ax", str(args.ax.absolute()),
                   "--controls", str(args.controls.absolute()), "--control-sha256", args.control_sha256,
                   "--resilience", str(args.resilience.absolute()), "--resilience-sha256", args.resilience_sha256]
        if args.smoke_baseline:
            command.append("--smoke-baseline")
        return smoke.runner.main(["--app", str(args.app), "--evidence", str(args.evidence),
            "--timeout", str(args.timeout), "--budget", str(args.budget), "--", *command],
            prepare=startup.case_preparation(manifest, candidate_path, launch_path),
            preflight=lambda lock, receipt, destination, deadline:
                smoke.case_preflight(args.ax.absolute(), receipt, destination, deadline))
    if args.launch_manifest is None:
        parser.error("scenario requires its runner before-launch receipt")
    context = smoke.case_context(os.environ)
    startup.validate_candidate(manifest, context["app"])
    context.update(candidate=manifest, launch=startup.load_launch(context, candidate_path, args.launch_manifest))
    context.update(timeout=float(os.environ["ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS"]), ax=args.ax.resolve(strict=True),
                   controls=args.controls.resolve(strict=True), controls_sha256=args.control_sha256,
                   resilience=args.resilience.resolve(strict=True), resilience_sha256=args.resilience_sha256)
    try:
        print(json.dumps(run_baseline(context) if args.smoke_baseline else run(context)))
        return 0
    except (OSError, ValueError, RuntimeError, AssertionError, subprocess.SubprocessError) as error:
        receipt = {"passed": False, "case_id": context["case_id"], "failure": str(error)}
        try:
            smoke.runner.save_json(context["evidence"] / ("smoke.json" if args.smoke_baseline else "native-mcp.json"), receipt)
        except OSError as storage_error:
            receipt["terminal_evidence_error"] = type(storage_error).__name__
        print(json.dumps(receipt))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
