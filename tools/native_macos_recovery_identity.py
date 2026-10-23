"""Offline actual-writer proof preparation, never native acceptance or App launch.

Build on an identified checkout, transfer that binary/binding, then prepare on
the target SDK/signing system. No expected hash is learned from a live target.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys


HERE = Path(__file__).resolve().parent
HEX = re.compile(r"[0-9a-f]{64}\Z")
SOURCE = re.compile(r"[0-9a-f]{40}\Z")
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+\Z")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def save(path, value):
    with Path(path).open("x") as stream:
        json.dump(value, stream, indent=2, allow_nan=False)
        stream.write("\n")


def run(argv, directory, label, *, env=None, cwd=None, timeout=120):
    argv = list(map(str, argv))
    primary, cause = None, None
    try:
        result = subprocess.run(argv, env=env, cwd=cwd,
                                capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        primary = error
        artifacts = [(name, "write_bytes", data) for name, data in
                     (("stdout", error.stdout), ("stderr", error.stderr)) if data is not None]
        artifacts.append(("timeout.json", "save_json", dict(timeout=timeout,
                         stdout_present=error.stdout is not None, stderr_present=error.stderr is not None)))
    else:
        artifacts = [("stdout", "write_bytes", result.stdout), ("stderr", "write_bytes", result.stderr),
                     ("execution.json", "save_json", dict(argv=argv, exit_code=result.returncode))]
        if result.returncode:
            primary = RuntimeError(label + " failed; exit " + str(result.returncode))
            cause = subprocess.CalledProcessError(result.returncode, argv, result.stdout, result.stderr)
    failure, errors = None, []
    for suffix, operation, data in artifacts:
        path = directory / (label + "." + suffix)
        try:
            if operation == "write_bytes":
                path.write_bytes(data)
            else:
                save(path, data)
        except OSError as error:
            if failure is None:
                failure = OSError(error.errno, "evidence write failed", path.name)
            errors.append(dict(artifact=path.name, operation=operation,
                               error_type=type(error).__name__, errno=error.errno))
    if errors:
        (primary or failure).evidence_errors = errors
        try:
            print(json.dumps(dict(evidence_errors=errors), sort_keys=True), file=sys.stderr)
        except (OSError, ValueError):
            pass
    if primary is not None:
        raise primary from (primary.__cause__ or cause or failure)
    if failure is not None:
        raise failure
    return result.stdout


def payload_fingerprint(data):
    """Hash every pre-signature byte, normalizing only three derived lengths.

    The actual writer/strict signature check remains mandatory. This is not a
    signature validator or a general Mach-O equivalence algorithm.
    """
    if len(data) < 32:
        raise ValueError("short Mach-O")
    magic, cpu, _, _, count, size, _, _ = struct.unpack_from("<8I", data)
    page = {0x100000C: 16384, 0x1000007: 4096}.get(cpu)
    if magic != 0xFEEDFACF or not page or not 0 < count < 4096 or size > len(data)-32:
        raise ValueError("unsupported Mach-O header")
    offset, signature, linkedit = 32, None, None
    for _ in range(count):
        if offset + 8 > 32 + size:
            raise ValueError("truncated load command")
        command, length = struct.unpack_from("<II", data, offset)
        if length < 8 or length % 8 or length > 32 + size - offset:
            raise ValueError("invalid load command size")
        if command == 0x1D:
            if signature is not None or length != 16:
                raise ValueError("ambiguous code signature command")
            signature = (offset, *struct.unpack_from("<II", data, offset+8))
        if command == 0x19 and data[offset+8:offset+24].rstrip(b"\0") == b"__LINKEDIT":
            if linkedit is not None or length != 72:
                raise ValueError("ambiguous linkedit segment")
            linkedit = (offset, *struct.unpack_from("<QQQ", data, offset+32))
        offset += length
    if offset != 32 + size or signature is None or linkedit is None:
        raise ValueError("incomplete load commands")
    command, start, length = signature
    segment, virtual, file_offset, file_size = linkedit
    if (start < offset or not length or start + length != len(data)
            or file_offset > start or file_size != len(data)-file_offset
            or virtual != (file_size+page-1)//page*page):
        raise ValueError("signature/linkedit lengths do not describe the actual tail")
    normalized = bytearray(data[:start])
    struct.pack_into("<I", normalized, command+12, 0)
    struct.pack_into("<Q", normalized, segment+32, 0)
    struct.pack_into("<Q", normalized, segment+48, 0)
    return dict(sha256=sha(normalized), signature_offset=start,
                normalized_fields=[command+12, segment+32, segment+48])


def build_writer(checkout, destination, source):
    checkout, destination = Path(checkout).resolve(), Path(destination).resolve()
    if not SOURCE.fullmatch(source) or destination.is_relative_to(checkout):
        raise ValueError("full source SHA and new output outside checkout required")
    destination.mkdir(mode=0o700)
    inputs = writer_inputs(checkout, destination, source, "before")
    template = HERE / "native_macos_recovery_writer_test.go.txt"
    template_hash = sha(template.read_bytes())
    overlay = destination / "overlay.json"
    save(overlay, {"Replace": {str(checkout / "internal/desktop/zz_recovery_identity_test.go"): str(template)}})
    binary = destination / "recovery-identity-writer.test"
    environment = dict(os.environ, GOENV="off", GOWORK="off", GOFLAGS="-mod=readonly", GOTOOLCHAIN="local")
    run(["go", "test", "-c", "-overlay", overlay, "-o", binary, "./internal/desktop"], destination, "compile", cwd=checkout, env=environment)
    if inputs != writer_inputs(checkout, destination, source, "after") or template_hash != sha(template.read_bytes()):
        raise ValueError("writer inputs changed during compilation")
    binding = dict(schema=1, source_sha=source, binary_sha256=sha(binary.read_bytes()),
                   writer_source_sha256=sha((checkout / "internal/desktop/bundle_darwin.go").read_bytes()),
                   icon_sha256=sha((checkout / "internal/desktop/AppIcon.icns").read_bytes()),
                   template_sha256=template_hash, inputs_sha256=sha(json.dumps(inputs, sort_keys=True).encode()))
    save(destination / "writer-binding.json", binding)
    return binary, binding


def writer_inputs(checkout, destination, source, label):
    """The desktop package has no other repository-package imports."""
    head = run(["git", "rev-parse", "HEAD"], destination, label+"-head", cwd=checkout).decode().strip()
    if head != source:
        raise ValueError("writer checkout source changed")
    paths = ["internal/desktop", "go.mod", "go.sum"]
    run(["git", "diff", "--exit-code", "HEAD", "--", *paths], destination, label+"-clean-writer", cwd=checkout)
    untracked = run(["git", "ls-files", "--others", "--", *paths], destination, label+"-untracked", cwd=checkout)
    if untracked:
        raise ValueError("untracked writer package/module inputs refused")
    tracked = run(["git", "ls-files", "-z", "--", *paths], destination, label+"-inputs", cwd=checkout)
    return {name: sha((checkout / name).read_bytes()) for name in tracked.decode().split("\0") if name}


def prepare(writer, writer_binding, original, version, source, destination, *, original_sha256):
    writer, original, destination = Path(writer).resolve(), Path(original).resolve(), Path(destination).resolve()
    binding = json.loads(Path(writer_binding).read_text())
    if (not isinstance(binding, dict) or set(binding) != {"schema", "source_sha", "binary_sha256", "writer_source_sha256", "icon_sha256", "template_sha256", "inputs_sha256"}
            or not SOURCE.fullmatch(source) or not VERSION.fullmatch(version) or not HEX.fullmatch(original_sha256)
            or type(binding.get("schema")) is not int or binding.get("schema") != 1 or binding.get("source_sha") != source
            or binding.get("binary_sha256") != sha(writer.read_bytes())
            or binding.get("template_sha256") != sha((HERE / "native_macos_recovery_writer_test.go.txt").read_bytes())
            or any(not isinstance(binding[key], str) or not HEX.fullmatch(binding[key])
                   for key in ("writer_source_sha256", "icon_sha256", "inputs_sha256"))):
        raise ValueError("writer source/binary/template identity refused")
    destination.mkdir(mode=0o700)
    before = original.read_bytes()
    if sha(before) != original_sha256:
        raise ValueError("original candidate differs from its frozen full SHA")
    run(["/usr/bin/codesign", "--verify", "--strict", "--all-architectures", original],
        destination, "original-strict-signature", timeout=10)
    environment = dict(os.environ, ZWAI_RECOVERY_ORIGINAL=str(original),
                       ZWAI_RECOVERY_ORIGINAL_SHA256=sha(before), ZWAI_RECOVERY_VERSION=version,
                       ZWAI_RECOVERY_EVIDENCE=str(destination / "actual-writer"))
    run([writer, "-test.run", "^TestNativeRecoveryIdentityWriter$", "-test.v", "-test.timeout", "30s"],
        destination, "actual-writer", env=environment, timeout=35)
    actual = destination / "actual-writer"
    result = json.loads((actual / "actual-Q-WriteMacApp-result.json").read_text())
    if result.get("original_sha256") != sha(before) or result.get("original_unchanged") is not True or len(result.get("rows", [])) != 2:
        raise ValueError("actual writer did not retain the original")
    expected_payload = payload_fingerprint(before)
    copies = []
    for index, row in enumerate(result["rows"], 1):
        core = (actual / ("copy" + str(index) + "-core")).read_bytes()
        if (row.get("version_input") != version or row.get("writer_error") != "<nil>"
                or row.get("codesign_exit") != 0 or row.get("verify_error") != "<nil>"
                or row.get("display_error") != "<nil>" or row.get("core_sha256") != sha(core)
                or (actual / ("copy" + str(index) + "-codesign.exit")).read_bytes().strip() != b"0"
                or payload_fingerprint(core) != expected_payload):
            raise ValueError("writer/signature/full non-signature payload proof refused")
        copies.append(core)
    if copies[0] != copies[1] or original.read_bytes() != before:
        raise ValueError("writer was nondeterministic or original changed")
    # Keep every raw command/result bound in the preparation digest, never the live target.
    preparation = dict(schema=1, source_sha=source, version=version, writer=binding,
                       original_core_sha256=sha(before), payload=expected_payload,
                       original_verification={p.name: sha(p.read_bytes()) for p in sorted(destination.glob("original-strict-signature.*"))},
                       actual_artifacts={p.name: sha(p.read_bytes()) for p in sorted(actual.iterdir()) if p.is_file()})
    build = run(["/usr/bin/sw_vers", "-buildVersion"], destination, "OS-build", timeout=2).decode().strip()
    preparation["OS_build"] = build
    save(destination / "preparation.json", preparation)
    proof = dict(schema=1, source_sha=source, version=version,
                 original_core_sha256=sha(before), derived_core_sha256=sha(copies[0]),
                 derived_info_sha256=sha((actual / "copy1-Info.plist").read_bytes()),
                 derived_icon_sha256=sha((actual / "copy1-Resources-AppIcon.icns").read_bytes()),
                 preparation_sha256=sha((destination / "preparation.json").read_bytes()))
    proof_path = destination / "recovery-identity.json"
    save(proof_path, proof)
    return dict(path=str(proof_path), sha256=sha(proof_path.read_bytes()), source_sha=source, version=version)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    build = sub.add_parser("build")
    build.add_argument("--checkout", type=Path, required=True)
    for command in (build, sub.add_parser("prepare")):
        command.add_argument("--source", required=True)
        command.add_argument("--output", type=Path, required=True)
    sub.choices["prepare"].add_argument("--writer", type=Path, required=True)
    sub.choices["prepare"].add_argument("--binding", type=Path, required=True)
    sub.choices["prepare"].add_argument("--original", type=Path, required=True)
    sub.choices["prepare"].add_argument("--original-sha256", required=True)
    sub.choices["prepare"].add_argument("--version", required=True)
    args = parser.parse_args()
    value = build_writer(args.checkout, args.output, args.source) if args.action == "build" else prepare(
        args.writer, args.binding, args.original, args.version, args.source, args.output, original_sha256=args.original_sha256)
    print(json.dumps(value, default=str))


if __name__ == "__main__":
    main()
