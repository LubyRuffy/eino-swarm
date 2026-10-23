"""Offline proof preparation refuses untrusted origins and changed writer inputs."""
import contextlib
import errno
import io
import json
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import native_macos_recovery_identity as identity


def macho(signature=b"s"*16):
    data = bytearray(256+len(signature))
    struct.pack_into("<8I", data, 0, 0xFEEDFACF, 0x100000C, 0, 2, 2, 88, 0, 0)
    struct.pack_into("<II16sQQQQIIII", data, 32, 0x19, 72, b"__LINKEDIT", 4096,
                     16384, 128, len(data)-128, 7, 1, 0, 0)
    struct.pack_into("<4I", data, 104, 0x1D, 16, 256, len(signature))
    data[128:256] = b"p"*128
    data[256:] = signature
    return bytes(data)


class RecoveryIdentityPreparationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="recovery-proof-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()

    def repository(self):
        root = self.root / "checkout"; root.mkdir()
        (root / "internal/desktop").mkdir(parents=True)
        for name in ("go.mod", "go.sum", "internal/desktop/bundle_darwin.go", "internal/desktop/AppIcon.icns"):
            (root / name).write_bytes(name.encode())
        for args in (["init", "-q"], ["add", "."],
                     ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                      "-c", "commit.gpgsign=false", "commit", "-qm", "writer fixture"]):
            subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)
        return root, subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()

    def test_payload_normalizes_only_the_three_real_signature_length_fields(self):
        original, resigned = macho(), macho(b"different signature"*4)
        self.assertEqual(identity.payload_fingerprint(original), identity.payload_fingerprint(resigned))
        for offset in (8, 56, 128, 255):
            changed = bytearray(resigned); changed[offset] ^= 1
            self.assertNotEqual(identity.payload_fingerprint(original), identity.payload_fingerprint(changed))
        cases = [b"short", b"\0"*32]
        for offset, fmt, value in ((16, "I", 0), (20, "I", 99999), (16, "I", 3),
                                   (36, "I", 7), (36, "I", 96), (108, "I", 24),
                                   (104, "I", 0x19), (32, "I", 0x1D), (112, "I", 120),
                                   (116, "I", 0), (72, "Q", 300), (80, "Q", 5),
                                   (64, "Q", 0)):
            changed = bytearray(original); struct.pack_into("<"+fmt, changed, offset, value)
            cases.append(changed)
        for value in cases:
            with self.subTest(value=bytes(value[:40])), self.assertRaises(ValueError):
                identity.payload_fingerprint(value)
        # Both duplicate commands must fail, rather than choosing one signature.
        for command in (0x19, 0x1D):
            duplicate = bytearray(original)
            struct.pack_into("<II", duplicate, 16, 3, 104)
            struct.pack_into("<4I", duplicate, 120, command, 16, 256, 16)
            if command == 0x19:
                struct.pack_into("<II", duplicate, 20, 160, 0)
                struct.pack_into("<I", duplicate, 124, 72)
                duplicate[128:144] = b"__LINKEDIT"+b"\0"*6
            with self.assertRaises(ValueError):
                identity.payload_fingerprint(duplicate)

    def test_command_failure_and_timeout_retain_raw_streams_without_inventing_empty_output(self):
        self.assertEqual(identity.run([sys.executable, "-c", "print('verified')"], self.root, "success"), b"verified\n")
        with self.assertRaisesRegex(RuntimeError, "exit 3"):
            identity.run([sys.executable, "-c", "import sys;print('cause',file=sys.stderr);sys.exit(3)"], self.root, "failure")
        self.assertEqual((self.root / "failure.stderr").read_bytes(), b"cause\n")
        self.assertEqual(json.loads((self.root / "failure.execution.json").read_text())["exit_code"], 3)
        with self.assertRaises(subprocess.TimeoutExpired):
            identity.run([sys.executable, "-c", "import time;print('partial',flush=True);time.sleep(2)"],
                         self.root, "timeout", timeout=.2)
        self.assertEqual((self.root / "timeout.stdout").read_bytes(), b"partial\n")
        self.assertFalse((self.root / "timeout.stderr").exists())
        self.assertFalse(json.loads((self.root / "timeout.timeout.json").read_text())["stderr_present"])
        with self.assertRaises(FileExistsError):
            identity.save(self.root / "success.execution.json", {})

    @contextlib.contextmanager
    def failed_receipts(self, names):
        attempts = []
        write, save = Path.write_bytes, identity.save
        def bytes_receipt(path, data):
            attempts.append(path.name)
            if path.name in names:
                raise OSError(errno.EIO, "controlled receipt failure")
            return write(path, data)
        def json_receipt(path, value):
            attempts.append(path.name)
            if path.name in names:
                raise OSError(errno.EIO, "controlled receipt failure")
            return save(path, value)
        with mock.patch.object(Path, "write_bytes", bytes_receipt), \
                mock.patch.object(identity, "save", side_effect=json_receipt):
            yield attempts

    def assert_receipt_failures(self, error, names):
        errors = error.evidence_errors
        self.assertEqual([row["artifact"] for row in errors], names)
        for row in errors:
            self.assertEqual(row["error_type"], "OSError")
            self.assertEqual(row["errno"], errno.EIO)
            self.assertIn(row["operation"], ("write_bytes", "save_json"))
            self.assertNotIn("controlled receipt failure", json.dumps(row))

    def test_failed_command_keeps_its_actual_exit_and_streams_when_any_receipt_write_fails(self):
        artifacts = ["failure.stdout", "failure.stderr", "failure.execution.json"]
        argv = [sys.executable, "-c", "import sys;print('actual output');print('actual rejection',file=sys.stderr);sys.exit(7)"]
        for index, failed in enumerate(([name] for name in artifacts)):
            directory = self.root / str(index); directory.mkdir()
            with self.subTest(failed=failed), self.failed_receipts(failed) as attempts, \
                    self.assertRaisesRegex(RuntimeError, "exit 7") as caught:
                identity.run(argv, directory, "failure")
            self.assertEqual(attempts, artifacts)
            primary = caught.exception.__cause__
            self.assertIsInstance(primary, subprocess.CalledProcessError)
            self.assertEqual((primary.returncode, primary.cmd), (7, argv))
            self.assertEqual(primary.stdout, b"actual output\n")
            self.assertEqual(primary.stderr, b"actual rejection\n")
            self.assert_receipt_failures(caught.exception, failed)
            for name in set(artifacts) - set(failed):
                self.assertTrue((directory / name).exists())

    def test_actual_timeout_survives_all_secondary_receipt_failures_with_original_partial_bytes(self):
        artifacts = ["timeout.stdout", "timeout.stderr", "timeout.timeout.json"]
        argv = [sys.executable, "-c", "import sys,time;print('partial out',flush=True);print('partial err',file=sys.stderr,flush=True);time.sleep(5)"]
        observed, real_run = [], subprocess.run
        def observe_timeout(*args, **kwargs):
            try:
                return real_run(*args, **kwargs)
            except subprocess.TimeoutExpired as error:
                observed.append(error)
                raise
        with self.failed_receipts(artifacts) as attempts, \
                mock.patch.object(identity.subprocess, "run", side_effect=observe_timeout), \
                self.assertRaises(subprocess.TimeoutExpired) as caught:
            identity.run(argv, self.root, "timeout", timeout=.2)
        self.assertEqual(attempts, artifacts)
        self.assertIs(caught.exception, observed[0])
        self.assertEqual(caught.exception.stdout, b"partial out\n")
        self.assertEqual(caught.exception.stderr, b"partial err\n")
        self.assertEqual(caught.exception.timeout, .2)
        self.assertIsInstance(caught.exception.__cause__, OSError)
        self.assert_receipt_failures(caught.exception, artifacts)
        self.assertFalse((self.root / "timeout.execution.json").exists())

    def test_timeout_keeps_an_existing_cause_before_secondary_storage_errors(self):
        # An adapter may already have attributed this exact child timeout.
        real_run = subprocess.run
        for failed in ([], ["cause.stdout"]):
            directory = self.root / ("with-io" if failed else "without-io")
            directory.mkdir()
            original, observed = RuntimeError("existing attribution"), []
            def attributed_timeout(*args, **kwargs):
                try:
                    return real_run(*args, **kwargs)
                except subprocess.TimeoutExpired as error:
                    error.__cause__ = original
                    observed.append(error)
                    raise
            argv = [sys.executable, "-c", "import time;print('partial',flush=True);time.sleep(5)"]
            with self.subTest(failed=failed), self.failed_receipts(failed), \
                    mock.patch.object(identity.subprocess, "run", side_effect=attributed_timeout), \
                    self.assertRaises(subprocess.TimeoutExpired) as caught:
                identity.run(argv, directory, "cause", timeout=.2)
            self.assertIs(caught.exception, observed[0])
            self.assertIs(caught.exception.__cause__, original)
            self.assertEqual(caught.exception.stdout, b"partial\n")
            if failed:
                self.assert_receipt_failures(caught.exception, failed)

    def test_success_is_refused_when_any_mandatory_receipt_is_unavailable(self):
        artifacts = ["success.stdout", "success.stderr", "success.execution.json"]
        for index, failed in enumerate(([name] for name in artifacts)):
            directory = self.root / str(index); directory.mkdir()
            with self.subTest(failed=failed), self.failed_receipts(failed) as attempts, \
                    self.assertRaises(OSError) as caught:
                identity.run([sys.executable, "-c", "print('actual success')"], directory, "success")
            self.assertEqual(attempts, artifacts)
            self.assert_receipt_failures(caught.exception, failed)
            for name in set(artifacts) - set(failed):
                self.assertTrue((directory / name).exists())

    def test_actual_CLI_exposes_safe_secondary_errors_without_replacing_its_git_failure(self):
        checkout = self.root / "not-a-repository"; checkout.mkdir()
        output = self.root / "build"
        script = """
import errno,sys
from pathlib import Path
from unittest import mock
sys.path.insert(0, sys.argv.pop(1))
import native_macos_recovery_identity as identity
real_write = Path.write_bytes
def write(path, data):
    if path.name == 'before-head.stderr':
        raise OSError(errno.EIO, 'hidden-secondary-canary')
    return real_write(path, data)
with mock.patch.object(Path, 'write_bytes', write):
    identity.main()
"""
        result = subprocess.run([sys.executable, "-c", script, str(identity.HERE), "build",
                                 "--checkout", str(checkout), "--source", "a"*40, "--output", str(output)],
                                capture_output=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        secondary = json.loads(result.stderr.splitlines()[0])["evidence_errors"]
        self.assertEqual(secondary, [dict(artifact="before-head.stderr", operation="write_bytes",
                                          error_type="OSError", errno=errno.EIO)])
        self.assertIn(b"CalledProcessError", result.stderr)
        self.assertIn(b"before-head failed; exit 128", result.stderr)
        self.assertNotIn(b"hidden-secondary-canary", result.stderr)
        self.assertEqual(json.loads((output / "before-head.execution.json").read_text())["exit_code"], 128)
        self.assertFalse((output / "writer-binding.json").exists())

    def test_secondary_stderr_failure_cannot_replace_the_original_command_failure(self):
        class BrokenStderr:
            def write(self, text):
                raise OSError("controlled broken stderr")
        with self.failed_receipts(["failure.stderr"]), \
                mock.patch.object(sys, "stderr", BrokenStderr()), \
                self.assertRaisesRegex(RuntimeError, "exit 7") as caught:
            identity.run([sys.executable, "-c", "import sys;sys.exit(7)"], self.root, "failure")
        self.assertIsInstance(caught.exception.__cause__, subprocess.CalledProcessError)
        self.assertEqual(caught.exception.__cause__.returncode, 7)
        self.assert_receipt_failures(caught.exception, ["failure.stderr"])

    def test_writer_inputs_are_actual_clean_tracked_package_and_module_bytes(self):
        checkout, source = self.repository()
        real_run = identity.run
        def compile_only(argv, *args, **kwargs):
            if argv[0] != "go":
                return real_run(argv, *args, **kwargs)
            self.assertEqual(kwargs["env"]["GOWORK"], "off")
            self.assertEqual(kwargs["env"]["GOFLAGS"], "-mod=readonly")
            Path(argv[argv.index("-o")+1]).write_bytes(b"compiled fixture")
            return b""
        with mock.patch.object(identity, "run", side_effect=compile_only):
            binary, binding = identity.build_writer(checkout, self.root / "build", source)
        self.assertEqual(binding["binary_sha256"], identity.sha(binary.read_bytes()))
        self.assertRegex(binding["inputs_sha256"], r"^[0-9a-f]{64}$")
        for changed in ("go.mod", "go.sum", "internal/desktop/bundle_darwin.go"):
            file = checkout / changed; before = file.read_bytes(); file.write_bytes(before+b"changed")
            with self.subTest(changed=changed), self.assertRaises(RuntimeError):
                identity.build_writer(checkout, self.root / ("dirty-"+file.name), source)
            file.write_bytes(before)
        for name in ("untracked_test.go", "untracked.go"):
            file = checkout / "internal/desktop" / name; file.write_text("package desktop")
            with self.assertRaisesRegex(ValueError, "untracked"):
                identity.build_writer(checkout, self.root / name, source)
            file.unlink()
        for output, supplied in ((checkout / "output", source), (self.root / "bad", "x"),
                                  (self.root / "wrong", "0"*40)):
            with self.assertRaises(ValueError):
                identity.build_writer(checkout, output, supplied)
        def changed_during_build(argv, *args, **kwargs):
            result = compile_only(argv, *args, **kwargs)
            if argv[0] == "go":
                (checkout / "go.sum").write_text("changed while compiler ran")
            return result
        with mock.patch.object(identity, "run", side_effect=changed_during_build), self.assertRaises(RuntimeError):
            identity.build_writer(checkout, self.root / "raced", source)

    def prepared_fixture(self):
        writer = self.root / "writer"; writer.write_bytes(b"actual compiled writer seam")
        original = self.root / "core"; original.write_bytes(macho())
        binding = dict(schema=1, source_sha="a"*40, binary_sha256=identity.sha(writer.read_bytes()),
                       writer_source_sha256="b"*64, icon_sha256="c"*64, inputs_sha256="d"*64,
                       template_sha256=identity.sha((identity.HERE / "native_macos_recovery_writer_test.go.txt").read_bytes()))
        path = self.root / "binding.json"; path.write_text(json.dumps(binding))
        return writer, original, path, binding

    def test_preparation_refuses_each_failed_writer_or_payload_before_issuing_a_proof(self):
        writer, original, binding_path, binding = self.prepared_fixture()
        mode = None
        def actual_writer_seam(argv, directory, label, **kwargs):
            if label != "actual-writer":
                if label == "original-strict-signature":
                    self.assertEqual(argv[1:4], ["--verify", "--strict", "--all-architectures"])
                    (directory / (label+".stdout")).write_bytes(b"")
                return b"test OS\n"
            actual = directory / "actual-writer"; actual.mkdir()
            core = bytearray(macho(b"resigned"*4))
            if mode == "payload":
                core[128] ^= 1
            rows = []
            for index in (1, 2):
                label = "copy"+str(index)
                data = bytes(core)+(b"bad" if mode == "nondeterministic" and index == 2 else b"")
                # For the nondeterminism check retain a valid, different signature.
                if mode == "nondeterministic" and index == 2:
                    data = macho(b"resigned"*4+b"more")
                (actual / (label+"-core")).write_bytes(data)
                (actual / (label+"-codesign.exit")).write_bytes(b"1" if mode == "exit-file" else b"0")
                (actual / (label+"-Info.plist")).write_bytes(b"info")
                (actual / (label+"-Resources-AppIcon.icns")).write_bytes(b"icon")
                row = dict(version_input="1.2.3", writer_error="<nil>", codesign_exit=0,
                           verify_error="<nil>", display_error="<nil>", core_sha256=identity.sha(data))
                if mode in row:
                    row[mode] = "bad"
                rows.append(row)
            result = dict(original_sha256=identity.sha(original.read_bytes()), original_unchanged=True, rows=rows)
            if mode == "origin-result":
                result["original_unchanged"] = False
            if mode == "origin-changed":
                original.write_bytes(original.read_bytes()+b"changed")
            (actual / "actual-Q-WriteMacApp-result.json").write_text(json.dumps(result))
            return b""
        def prepare(label):
            return identity.prepare(writer, binding_path, original, "1.2.3", "a"*40,
                                    self.root / label, original_sha256=identity.sha(original.read_bytes()))
        with mock.patch.object(identity, "run", side_effect=actual_writer_seam):
            accepted = prepare("accepted")
            self.assertEqual(json.loads(Path(accepted["path"]).read_text())["original_core_sha256"], identity.sha(original.read_bytes()))
            for mode in ("origin-result", "version_input", "writer_error", "codesign_exit", "verify_error", "display_error",
                         "core_sha256", "exit-file", "payload", "nondeterministic", "origin-changed"):
                with self.subTest(mode=mode), self.assertRaises(ValueError):
                    prepare(mode)
                self.assertFalse((self.root / mode / "recovery-identity.json").exists())
        binding_path.write_text(json.dumps(dict(binding, schema=True)))
        with self.assertRaises(ValueError):
            prepare("bad-binding")
        binding_path.write_text(json.dumps(binding))
        with self.assertRaisesRegex(ValueError, "frozen full SHA"):
            identity.prepare(writer, binding_path, original, "1.2.3", "a"*40, self.root / "wrong-origin", original_sha256="0"*64)

    def test_CLI_requires_frozen_origin_and_exposes_only_the_binding(self):
        for args, function, value in ((["build", "--checkout", str(self.root)], "build_writer", ["writer", {}]),
                                     (["prepare", "--writer", "writer", "--binding", "binding", "--original", "core",
                                       "--original-sha256", "c"*64, "--version", "1.2.3"], "prepare", {"sha256": "b"*64})):
            arguments = ["identity", *args, "--source", "a"*40, "--output", str(self.root / "output")]
            with mock.patch.object(sys, "argv", arguments), mock.patch.object(identity, function, return_value=value), contextlib.redirect_stdout(io.StringIO()) as out:
                identity.main()
            self.assertEqual(json.loads(out.getvalue()), value)


if __name__ == "__main__":
    unittest.main(verbosity=2)
