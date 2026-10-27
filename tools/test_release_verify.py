"""The release entry can prepare evidence without publishing anything."""
import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock
import subprocess
import os
import stat
import signal

sys.path.insert(0, str(Path(__file__).parent))
import release
import release_verify


class VerificationEntryTests(unittest.TestCase):
    def setUp(self):
        # Entry fixtures isolate commands/preflight below. They must not acquire
        # the actual project's host lease while another task validates it.
        lease = mock.patch.object(release_verify, "HostValidationLease", side_effect=lambda *_args, **_kwargs: contextlib.nullcontext())
        self.addCleanup(lease.stop)
        self.lease = lease.start()

    def test_host_admission_refusal_never_starts_preflight_or_heavy_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            root.mkdir()
            self.lease.side_effect = release_verify.HostLeaseBlocked("fixture existing validation")
            with mock.patch.object(release_verify, "_verify") as execute:
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 2)
            execute.assert_not_called()
            row = json.loads((evidence / "verification.json").read_text())
            self.assertEqual((row["status"], row["failure"]), ("blocked", "host_validation_admission"))

    def test_sigterm_during_preflight_is_recorded_and_releases_evidence_lock(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            root.mkdir()
            previous = signal.getsignal(signal.SIGTERM)
            def interrupted(*args, **kwargs):
                signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
            with mock.patch.object(release_verify, "_verify", side_effect=interrupted), \
                 self.assertRaises(release_verify.Cancelled):
                release_verify.verify(root, "1.2.3", evidence)
            self.assertEqual(signal.getsignal(signal.SIGTERM), previous)
            row = json.loads((evidence / "verification.json").read_text())
            self.assertEqual((row["status"], row["exit_code"]), ("cancelled", 143))
            with mock.patch.object(release_verify, "_verify", return_value=2):
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 2)

    def test_stage_cancelled_by_sigterm_keeps_cancellation_instead_of_build_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            (root / ".git").mkdir(parents=True)
            report = {"status": "passed", "inputs": {}, "toolchain": {}}
            def stopped_stage(*_args, **_kwargs):
                # The real stage runner records/reaps on KeyboardInterrupt before
                # returning its cancelled row. The pipeline must retain the cause.
                try:
                    signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
                except release_verify.Cancelled:
                    return {"status": "cancelled"}
            with mock.patch("release_preflight.collect_preflight", return_value=report), \
                 mock.patch("release_preflight.stable_inputs", return_value={"toolchain": {}}), \
                 mock.patch.object(release_verify, "source_revision", return_value="a" * 40), \
                 mock.patch("release_stages.StageRunner.run", side_effect=stopped_stage), \
                 mock.patch.object(release_verify.platform, "system", return_value="Darwin"), \
                 self.assertRaises(release_verify.Cancelled):
                release_verify.verify(root, "1.2.3", evidence, e2e_port=12345)
            row = json.loads((evidence / "verification.json").read_text())
            self.assertEqual((row["status"], row["exit_code"]), ("cancelled", 143))

    def test_verification_plan_keeps_full_race_ui_and_native_gates_without_execution(self):
        output = io.StringIO()
        with mock.patch.object(release, "execute") as publish, \
             mock.patch.object(release, "home", side_effect=AssertionError("publication state touched")), \
             mock.patch.object(sys, "argv", ["release.py", "--version", "1.2.3", "--platform", "macos",
                                            "--verify", "--plan"]), contextlib.redirect_stdout(output):
            self.assertEqual(release.main(), 0)
        publish.assert_not_called()
        plan = json.loads(output.getvalue())
        self.assertEqual(plan["status"], "plan_only")
        self.assertEqual(plan["native_status"], "required_separate_guest_acceptance")
        by_name = {stage["name"]: stage for stage in plan["stages"]}
        self.assertIn("-race", by_name["go-race"]["argv"])
        self.assertIn("./...", by_name["go-race"]["argv"])
        self.assertIn("--retries=0", by_name["pc-e2e"]["argv"])
        self.assertEqual(plan["jobs"], 2)

    def test_verification_does_not_fall_through_to_publication(self):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch("release_verify.verify", return_value=3) as verify, \
                 mock.patch.object(release, "execute") as publish, \
                 mock.patch.object(release, "home", side_effect=AssertionError("publication state touched")), \
                 mock.patch.object(sys, "argv", ["release.py", "--version", "1.2.3", "--platform", "macos",
                                                "--verify", "--evidence", directory]):
                self.assertEqual(release.main(), 3)
            verify.assert_called_once()
            publish.assert_not_called()

    def test_dependency_or_source_refusal_stops_before_every_expensive_stage(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            root.mkdir()
            with mock.patch("release_preflight.collect_preflight", return_value={"status": "blocked"}), \
                 mock.patch.object(release_verify, "source_revision", return_value="a" * 40), \
                 mock.patch("release_stages.StageRunner") as stages:
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 2)
            stages.assert_not_called()
            self.assertEqual(json.loads((evidence / "preflight.json").read_text())["status"], "blocked")
            self.assertEqual(json.loads((evidence / "verification.json").read_text())["status"], "blocked")
            self.assertEqual(stat.S_IMODE((evidence / "preflight.json").stat().st_mode), 0o600)

    def test_actual_stage_orchestration_resumes_only_same_inputs_and_keeps_gui_pending(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            (root / ".git").mkdir(parents=True)
            (root / "frontend/dist").mkdir(parents=True)
            output = root / "package.zip"
            report = {"status": "passed", "inputs": {"lock": "same"}, "toolchain": {}, "source": {}}
            binding = {"toolchain": {}, "lockfiles": {"lock": "same"}}
            def fixture_commands(*_):
                return [
                    ("frontend-build", (sys.executable, "-c", "print('frontend')"), root, ()),
                    ("go-race", (sys.executable, "-c", "print('race fixture')"), root, ()),
                    ("pc-e2e", (sys.executable, "-c", "print('ui fixture')"), root, ()),
                    ("package", (sys.executable, "-c", "from pathlib import Path; Path('package.zip').write_bytes(b'fixture')"), root, (output,)),
                ]
            with mock.patch("release_preflight.collect_preflight", return_value=report), \
                 mock.patch("release_preflight.stable_inputs", return_value=binding), \
                 mock.patch.object(release_verify, "source_revision", return_value="a" * 40), \
                 mock.patch.object(release_verify, "commands", side_effect=fixture_commands), \
                 mock.patch.object(release_verify, "package_provenance", return_value={"sha256": "fixture"}), \
                 mock.patch.object(release_verify.platform, "system", return_value="Darwin"):
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence, e2e_port=12345), 3,
                                 (evidence / "verification.json").read_text())
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 3)
                resumed = json.loads((evidence / "verification.json").read_text())
                self.assertTrue(all(row["status"] == "reused" for row in resumed["stages"].values()))
                self.assertEqual(resumed["native_status"], release_verify.NATIVE_PENDING)
                self.assertEqual(resumed["execution_union_seconds"], 0)
                binding["lockfiles"]["lock"] = "changed"
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 3)
                rerun = json.loads((evidence / "verification.json").read_text())
                self.assertTrue(all(row["status"] == "passed" for row in rerun["stages"].values()))

    def test_queued_source_change_cannot_use_earlier_preflight(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            (root / ".git").mkdir(parents=True)
            report = {"status": "passed", "inputs": {}, "toolchain": {}}
            with mock.patch("release_preflight.collect_preflight", side_effect=[report, {"status": "blocked"}]), \
                 mock.patch("release_preflight.stable_inputs", return_value={"toolchain": {}}), \
                 mock.patch.object(release_verify, "source_revision", return_value="a" * 40), \
                 mock.patch.object(release_verify.platform, "system", return_value="Darwin"), \
                 mock.patch("release_stages.StageRunner.run") as run:
                with self.assertRaisesRegex(ValueError, "while waiting"):
                    release_verify.verify(root, "1.2.3", evidence, e2e_port=12345)
            run.assert_not_called()
            self.assertEqual(json.loads((evidence / "verification.json").read_text())["status"], "failed")

    def test_unsafe_evidence_path_refuses_before_any_write_or_command(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "source"
            root.mkdir()
            for name in ("space data", "x;touch-pwned", "x$(pwned)", "x\nnext"):
                target = Path(directory) / name
                with mock.patch("release_preflight.collect_preflight") as probe, \
                     self.assertRaisesRegex(ValueError, "E2E evidence path"):
                    release_verify.verify(root, "1.2.3", target)
                probe.assert_not_called()
                self.assertFalse(target.exists())

    def test_new_refusal_replaces_current_success_but_retains_history(self):
        with tempfile.TemporaryDirectory() as directory:
            root, evidence = Path(directory) / "source", Path(directory) / "evidence"
            root.mkdir()
            release_verify.private_json(evidence / "verification.json", {"status": "headless_passed_native_pending"})
            with mock.patch.object(release_verify, "_verify", return_value=2):
                self.assertEqual(release_verify.verify(root, "1.2.3", evidence), 2)
            current = json.loads((evidence / "verification.json").read_text())
            self.assertEqual(current["status"], "blocked")
            old = [json.loads(path.read_text()) for path in (evidence / "history").glob("previous-*.json")]
            self.assertEqual(old[0]["status"], "headless_passed_native_pending")

    def test_final_package_requires_matching_plist_and_clean_embedded_source(self):
        import plistlib
        import zipfile
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "candidate.zip"
            with zipfile.ZipFile(path, "w") as archive:
                archive.writestr("zwai.app/Contents/Info.plist", plistlib.dumps({
                    "CFBundleExecutable": "zwai", "CFBundleShortVersionString": "1.2.3"}))
                archive.writestr("zwai.app/Contents/MacOS/zwai", b"fixture")
            for revision, modified, valid in [("a" * 40, "false", True), ("b" * 40, "false", False),
                                               ("a" * 40, "true", False)]:
                result = subprocess.CompletedProcess([], 0, f"\tbuild\tvcs.revision={revision}\n\tbuild\tvcs.modified={modified}\n", "")
                with mock.patch.object(release, "validate_macos_zip"), \
                     mock.patch.object(release_verify.subprocess, "run", return_value=result):
                    if valid:
                        row = release_verify.package_provenance(path, "1.2.3", "a" * 40, root)
                        self.assertEqual(row["source_sha"], "a" * 40)
                        self.assertEqual(row["boundary"], "final_package_bytes_after_build_signing")
                    else:
                        with self.assertRaisesRegex(ValueError, "VCS provenance"):
                            release_verify.package_provenance(path, "1.2.3", "a" * 40, root)
            with mock.patch.object(release, "validate_macos_zip"), self.assertRaisesRegex(ValueError, "version differs"):
                release_verify.package_provenance(path, "1.2.4", "a" * 40, root)


if __name__ == "__main__":
    unittest.main()
