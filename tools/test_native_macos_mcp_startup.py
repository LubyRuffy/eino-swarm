import copy
import datetime
import fcntl
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("native_macos_mcp_startup", Path(__file__).with_name("native_macos_mcp_startup.py"))
startup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(startup)
import native_macos_mcp as mcp


def write_json(path, value):
    startup.smoke.runner.save_json(path, value)


class MCPStartupFixture(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.app = self.root / "zwai.app"
        (self.app / "Contents/MacOS").mkdir(parents=True)
        self.executable = self.app / "Contents/MacOS/zwai"
        self.executable.write_bytes(b"identified native executable")
        self.executable.chmod(0o755)
        self.plist = self.app / "Contents/Info.plist"
        self.plist.write_bytes(plistlib.dumps({"CFBundleExecutable": "zwai", "CFBundleVersion": "0.0.0", "CFBundleShortVersionString": "0.0.0"}))
        self.archive = self.root / "private.zip"
        self.archive.write_bytes(b"private immutable candidate archive")
        production = {"internal/config/mcp.go": "1" * 64}
        self.source = self.root / "source.json"
        self.source_data = {"production_inputs": production,
            "production_inputs_sha256": hashlib.sha256(json.dumps(production, sort_keys=True).encode()).hexdigest(),
            "archive": str(self.archive), "archive_sha256": startup.sha(self.archive),
            "app": str(self.app), "version": "0.0.0", "baseline": "a" * 40,
            "executable_sha256": startup.sha(self.executable)}
        write_json(self.source, self.source_data)
        self.manifest = startup.freeze_candidate(self.source)
        self.candidate = self.root / "candidate.json"
        write_json(self.candidate, self.manifest)
        self.data = self.root / "data"
        self.home = self.root / "home"
        self.evidence = self.root / "evidence"
        for path in (self.data, self.home, self.evidence):
            path.mkdir(mode=0o700)
        self.context = dict(app=self.app, data=self.data, home=self.home,
                            evidence=self.evidence, app_pid=123, case_id="case-owned")
        self.launch_path = self.evidence / "mcp-launch.json"
        self.environment = {"ZWAI_NATIVE_APP": str(self.app), "ZWAI_NATIVE_DATA": str(self.data),
                            "ZWAI_NATIVE_EVIDENCE": str(self.evidence), "ZWAI_NATIVE_CASE_ID": "case-owned"}
        self.binding = dict(origin="http://127.0.0.1:3456", shell=dict(pid=123, birth=[11, 22], executable=str(self.executable)),
                            engine=dict(pid=456, birth=[33, 44], executable=str(self.executable)),
                            version="0.0.0", data=str(self.data))
        self.session = "0123456789abcdef"

    def product_files(self):
        cache = self.home / "Library/Caches/zwai"
        cache.mkdir(parents=True)
        recovery = cache / "desktop-recovery-first"
        recovery.mkdir()
        marker = recovery / "session"
        marker.write_bytes(self.session.encode())
        marker.chmod(0o600)
        diagnostics = self.data / "diagnostics"
        diagnostics.mkdir(mode=0o700)
        return cache, marker, diagnostics / ("desktop-" + self.session + ".jsonl")

    def record(self, **changes):
        now = datetime.datetime.now(datetime.timezone.utc)
        row = dict(session_id=self.session, generation=0, attempt=0, shell_pid=123, engine_pid=456,
                   phase="ready", at=now.strftime("%Y-%m-%dT%H:%M:%S.%fZ"))
        row.update(changes)
        return row

    def journal_bytes(self, *rows):
        return b"".join(json.dumps(row).encode() + b"\n" for row in rows)

    def write_journal(self, path, raw):
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "wb") as output:
            output.write(raw)

    def test_complete_candidate_inventory_rejects_changed_extra_and_wrong_mode_files(self):
        proof = startup.validate_candidate(self.manifest, self.app)
        self.assertEqual(proof["baseline"], "a" * 40)
        self.assertEqual(proof["archive_sha256"], startup.sha(self.archive))
        before = self.executable.read_bytes()
        self.executable.write_bytes(b"changed executable")
        with self.assertRaisesRegex(ValueError, "contents differ"):
            startup.validate_candidate(self.manifest, self.app)
        self.executable.write_bytes(before)
        extra = self.app / "unrecorded"
        extra.write_bytes(b"not admitted")
        with self.assertRaisesRegex(ValueError, "contents differ"):
            startup.validate_candidate(self.manifest, self.app)
        extra.unlink()
        self.executable.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "contents differ"):
            startup.validate_candidate(self.manifest, self.app)

    def test_candidate_receipt_requires_archive_and_production_digest_agreement(self):
        source = dict(self.source_data, production_inputs_sha256="f" * 64)
        write_json(self.source, source)
        with self.assertRaisesRegex(ValueError, "source/archive"):
            startup.freeze_candidate(self.source)
        write_json(self.source, self.source_data)
        self.archive.write_bytes(b"another archive")
        with self.assertRaisesRegex(ValueError, "source/archive"):
            startup.freeze_candidate(self.source)

    def test_candidate_version_and_executable_are_verified_after_full_inventory(self):
        plist = plistlib.loads(self.plist.read_bytes())
        plist["CFBundleVersion"] = "0.0.1"
        self.plist.write_bytes(plistlib.dumps(plist))
        altered = dict(self.manifest, app_files=startup.app_inventory(self.app))
        with self.assertRaisesRegex(ValueError, "bundle version"):
            startup.validate_candidate(altered, self.app)
        for key, value in (("expected_mock", False), ("baseline", "short"), ("archive_sha256", "x" * 64)):
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "source identity"):
                startup.validate_candidate(dict(self.manifest, **{key: value}), self.app)

    def test_inventory_refuses_root_links_escaping_links_and_special_files(self):
        link = self.root / "alias.app"
        link.symlink_to(self.app, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "real App"):
            startup.app_inventory(link)
        outside = self.root / "outside"
        outside.write_bytes(b"outside candidate")
        escape = self.app / "escape"
        escape.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "escapes"):
            startup.app_inventory(self.app)
        escape.unlink()
        internal = self.app / "same-executable"
        internal.symlink_to("Contents/MacOS/zwai")
        inventory = startup.app_inventory(self.app)
        self.assertEqual(inventory["same-executable"]["kind"], "link")
        internal.unlink()
        os.mkfifo(self.app / "not-a-resource")
        with self.assertRaisesRegex(ValueError, "special file"):
            startup.app_inventory(self.app)

    def test_receipt_read_refuses_duplicate_keys_nonobjects_and_symbolic_links(self):
        self.source.write_text('{"version":"first","version":"second"}')
        with self.assertRaisesRegex(ValueError, "duplicate"):
            startup.read_object(self.source)
        self.source.write_text('[]')
        with self.assertRaisesRegex(ValueError, "object"):
            startup.read_object(self.source)
        link = self.root / "receipt-link"
        link.symlink_to(self.candidate)
        with self.assertRaises(OSError):
            startup.read_object(link)

    def test_before_launch_callback_arms_one_private_timestamp_for_the_real_case(self):
        prepare = startup.case_preparation(self.manifest, self.candidate, self.launch_path)
        action = prepare(None, {"case_id": "case-owned"}, self.environment, [], None, 900)
        self.assertIsInstance(action, startup.smoke.runner.CasePreparation)
        self.assertFalse(self.launch_path.exists())
        self.assertIsNone(action.handoff(None, 900))
        action.before_launch()
        self.assertEqual(self.launch_path.stat().st_mode & 0o777, 0o600)
        launch = startup.load_launch(self.context, self.candidate, self.launch_path)
        self.assertEqual(launch["case_id"], "case-owned")
        self.assertEqual(launch["candidate_manifest_sha256"], startup.sha(self.candidate))
        self.assertGreater(launch["prelaunch_wall_ns"], 0)
        with self.assertRaisesRegex(ValueError, "already armed"):
            action.before_launch()
        self.assertEqual(startup.read_object(self.launch_path), launch)

    def test_before_launch_refuses_changed_candidate_and_wrong_case(self):
        prepare = startup.case_preparation(self.manifest, self.candidate, self.launch_path)
        with self.assertRaisesRegex(ValueError, "case ownership"):
            prepare(None, {"case_id": "another"}, self.environment, [], None, 900)
        action = prepare(None, {"case_id": "case-owned"}, self.environment, [], None, 900)
        self.candidate.write_text(self.candidate.read_text() + "\n")
        with self.assertRaisesRegex(ValueError, "manifest changed"):
            action.before_launch()
        self.assertFalse(self.launch_path.exists())

    def test_launch_readback_refuses_other_paths_contexts_and_freshness(self):
        prepare = startup.case_preparation(self.manifest, self.candidate, self.launch_path)
        prepare(None, {"case_id": "case-owned"}, self.environment, [], None, 900).before_launch()
        launch = startup.read_object(self.launch_path)
        alternate = self.evidence / "elsewhere.json"
        write_json(alternate, launch)
        with self.assertRaisesRegex(ValueError, "another evidence"):
            startup.load_launch(self.context, self.candidate, alternate)
        for key, value in (("case_id", "foreign"), ("app", "/another/app"), ("data", "/another/data"),
                           ("expected_mock", False), ("prelaunch_wall_ns", True), ("prelaunch_wall_ns", 10**22)):
            with self.subTest(key=key):
                write_json(self.launch_path, dict(launch, **{key: value}))
                with self.assertRaisesRegex(ValueError, "receipt identity"):
                    startup.load_launch(self.context, self.candidate, self.launch_path)

    def test_product_session_requires_one_case_owned_real_cache_marker(self):
        with self.assertRaises(FileNotFoundError):
            startup.product_session(self.context)
        cache, marker, _ = self.product_files()
        self.assertEqual(startup.product_session(self.context), self.session)
        marker.write_bytes(b"wrong")
        with self.assertRaisesRegex(ValueError, "marker identity"):
            startup.product_session(self.context)
        marker.write_bytes(b"G" * 16)
        with self.assertRaisesRegex(ValueError, "marker format"):
            startup.product_session(self.context)
        marker.write_bytes(self.session.encode())
        second = cache / "desktop-recovery-second"
        second.mkdir()
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            startup.product_session(self.context)
        second.rmdir()
        marker.unlink()
        marker.symlink_to(self.candidate)
        with self.assertRaises(OSError):
            startup.product_session(self.context)

    def test_journal_reader_refuses_links_oversize_and_respects_another_writers_flock(self):
        _, _, path = self.product_files()
        raw = self.journal_bytes(self.record())
        self.write_journal(path, raw)
        read, identity = startup.read_journal(path)
        self.assertEqual(read, raw)
        self.assertEqual(identity["bytes"], len(raw))
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        with path.open("rb") as locked:
            fcntl.flock(locked, fcntl.LOCK_EX | fcntl.LOCK_NB)
            script = "import sys,json;sys.path.insert(0,sys.argv[1]);import native_macos_mcp_startup as s;print(json.dumps(s.read_journal(sys.argv[2])))"
            result = subprocess.run([sys.executable, "-B", "-c", script, str(Path(__file__).parent), str(path)], capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(json.loads(result.stdout), None)
        self.write_journal(path, b"x" * 65537)
        with self.assertRaisesRegex(ValueError, "bounded"):
            startup.read_journal(path)
        path.unlink()
        path.symlink_to(self.candidate)
        with self.assertRaises(OSError):
            startup.read_journal(path)

    def test_product_record_needs_current_unique_ready_identity_not_elapsed_time(self):
        row = self.record()
        start = startup.utc_nanoseconds(row["at"]) - 1
        self.assertEqual(startup.ready_record(self.journal_bytes(row), self.session, self.binding, start), row)
        self.assertIsNone(startup.ready_record(self.journal_bytes(dict(row, phase="suspended")), self.session, self.binding, start))
        for change in ({"session_id": "another"}, {"shell_pid": 999}, {"engine_pid": 999}, {"generation": 1},
                       {"attempt": 1}, {"generation": True}, {"duration_ms": -1}, {"duration_ms": True},
                       {"reason": 4}, {"extra": "unknown"}, {"at": "2000-01-01T00:00:00Z"},
                       {"at": "2099-01-01T00:00:00Z"}, {"phase": "invented"}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                startup.ready_record(self.journal_bytes(dict(row, **change)), self.session, self.binding, start)
        for raw in (b"", self.journal_bytes(row).rstrip(b"\n"), self.journal_bytes(row, row),
                    self.journal_bytes(row).replace(b'"ready"', b'"ready", "phase":"ready"')):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                startup.ready_record(raw, self.session, self.binding, start)
        self.assertEqual(startup.utc_nanoseconds("1970-01-01T00:00:01.123456789Z"), 1123456789)
        with self.assertRaises(ValueError):
            startup.utc_nanoseconds("1970-01-01T00:00:01+00:00")

    def test_ready_admission_retains_raw_private_journal_and_rechecks_live_identity_before_AX(self):
        _, _, path = self.product_files()
        row = self.record()
        self.write_journal(path, self.journal_bytes(row))
        launch = {"prelaunch_wall_ns": startup.utc_nanoseconds(row["at"]) - 1}
        deadline = startup.smoke.Deadline(3)
        with patch.object(startup, "live_binding", return_value=self.binding) as live:
            observed = startup.wait_product_ready(self.context, self.manifest, launch, deadline)
        self.assertEqual(observed, self.binding)
        self.assertEqual(live.call_count, 2)
        self.assertEqual(live.call_args.args[2], deadline)
        folder = self.evidence / "product-readiness"
        receipt = startup.read_object(folder / "0001.json")
        self.assertTrue(receipt["ready"])
        self.assertTrue(receipt["live_binding_rechecked"])
        self.assertEqual(receipt["helper_calls"], 0)
        self.assertEqual(receipt["inputs"], 0)
        self.assertEqual(receipt["shared_deadline_monotonic"], deadline.ends)
        self.assertEqual((folder / "0001.jsonl").read_bytes(), self.journal_bytes(row))
        self.assertEqual({p.stat().st_mode & 0o777 for p in folder.iterdir()}, {0o600})

    def test_changed_live_birth_refuses_ready_and_keeps_the_refusal_receipt(self):
        _, _, path = self.product_files()
        row = self.record()
        self.write_journal(path, self.journal_bytes(row))
        altered = copy.deepcopy(self.binding)
        altered["shell"]["birth"] = [999, 22]
        with patch.object(startup, "live_binding", side_effect=[self.binding, altered]):
            with self.assertRaisesRegex(ValueError, "live process"):
                startup.wait_product_ready(self.context, self.manifest,
                    {"prelaunch_wall_ns": startup.utc_nanoseconds(row["at"]) - 1}, startup.smoke.Deadline(3))
        receipt = startup.read_object(self.evidence / "product-readiness/0001.json")
        self.assertTrue(receipt["refused"])
        self.assertFalse(receipt["ready"])

    def test_ready_admission_rejects_prior_native_calls_without_observing_or_activating(self):
        for guard in ({"native_call_count": 1}, {"native_input_attempted": True}):
            with self.subTest(guard=guard), patch.object(startup, "live_binding") as live:
                with self.assertRaisesRegex(ValueError, "precede"):
                    startup.wait_product_ready(dict(self.context, **guard), self.manifest,
                                               {"prelaunch_wall_ns": 1}, startup.smoke.Deadline(3))
                live.assert_not_called()

    def test_live_binding_requires_the_same_source_mock_engine_and_one_native_presence(self):
        deadline = startup.smoke.Deadline(3)
        lease = {"version": "0.0.0", "pid": 456}
        metadata = {"version": "0.0.0", "mode": "engine", "mock": True,
                    "clients": [{"surface": "desktop", "pid": 123}]}

        def observe(actual_lease, actual_metadata, identities=None):
            identities = identities if identities is not None else [self.binding["shell"], self.binding["engine"]]
            with patch.object(startup.smoke, "wait_engine", return_value=(self.binding["origin"], actual_lease)), \
                    patch.object(startup.smoke, "request_json", return_value=actual_metadata) as request, \
                    patch.object(startup.smoke, "assert_process", return_value=self.executable), \
                    patch.object(startup, "process_identity", side_effect=identities):
                result = startup.live_binding(self.context, self.manifest, deadline)
                request.assert_called_once_with(self.binding["origin"], "/api/meta", deadline)
                return result

        self.assertEqual(observe(lease, metadata), self.binding)
        self.assertEqual((self.evidence / "mcp-engine-binding-0001.json").stat().st_mode & 0o777, 0o600)
        for changes in ({"version": "0.0.1"}, {"mock": False}, {"mock": 1}, {"mode": "web"},
                        {"clients": []}, {"clients": [{"surface": "desktop", "pid": 999}]},
                        {"clients": [{"surface": "desktop", "pid": 123}] * 2}):
            with self.subTest(changes=changes), self.assertRaisesRegex(ValueError, "lease/meta"):
                observe(lease, dict(metadata, **changes))
        with self.assertRaisesRegex(ValueError, "lease/meta"):
            observe(dict(lease, version="0.0.1"), metadata)
        for identity in (None, dict(self.binding["shell"], pid=999), dict(self.binding["shell"], birth=[])):
            with self.subTest(identity=identity), self.assertRaisesRegex(ValueError, "birth/executable"):
                observe(lease, metadata, [identity, self.binding["engine"]])
        unrecorded = self.app / "extra-resource"
        unrecorded.write_bytes(b"not the full frozen App")
        with self.assertRaisesRegex(ValueError, "contents differ"):
            observe(lease, metadata)

    def test_candidate_preparation_cli_binds_the_current_controller_descriptor(self):
        controller_path = self.root / "controller.json"
        controller = dict(pid=101, bootstrap_pid=102, wake_pid=103, expected_uid=501,
                          expected_vm_id="private-vm", expires_at=1900000000,
                          binary_sha256="b" * 64, secret_nonce="never copy into manifest")
        write_json(controller_path, controller)
        destination = self.root / "prepared-candidate.json"
        arguments = ["--prepare-candidate", str(self.source), "--candidate", str(destination),
                     "--controller-descriptor", str(controller_path),
                     "--controller-sha256", startup.sha(controller_path)]
        with redirect_stdout(io.StringIO()):
            self.assertEqual(mcp.main(arguments), 0)
        frozen = startup.read_object(destination)
        self.assertEqual(frozen["controller"]["descriptor_sha256"], startup.sha(controller_path))
        self.assertEqual(frozen["controller"]["pid"], controller["pid"])
        self.assertNotIn("secret_nonce", frozen["controller"])
        self.assertEqual(destination.stat().st_mode & 0o777, 0o600)
        controller_path.write_text(controller_path.read_text() + "\n")
        with self.assertRaisesRegex(ValueError, "descriptor changed"):
            with redirect_stdout(io.StringIO()):
                mcp.main(arguments)

    def test_real_case_and_child_main_carry_the_before_launch_receipt_into_run(self):
        self.exercise_real_case_and_child_main(baseline=False)

    def test_real_case_and_child_main_carry_baseline_flag_and_before_launch_receipt(self):
        self.exercise_real_case_and_child_main(baseline=True)

    def exercise_real_case_and_child_main(self, *, baseline):
        # Run both CLI callers and their actual preparation callback. Only the
        # OS case launcher and final UI scenario are substituted; missing real
        # module methods or dropped child arguments must fail this test.
        manifest = dict(self.manifest, controller={"descriptor_sha256": "c" * 64, "pid": 101})
        write_json(self.candidate, manifest)
        helpers = {}
        for name in ("ax", "controls", "resilience"):
            path = self.root / name
            path.write_bytes((name + " prepared helper").encode())
            helpers[name] = path
        arguments = ["--case", "--app", str(self.app), "--evidence", str(self.evidence),
                     "--candidate", str(self.candidate), "--timeout", "7", "--budget", "11",
                     "--ax", str(helpers["ax"]), "--controls", str(helpers["controls"]),
                     "--control-sha256", startup.sha(helpers["controls"]),
                     "--resilience", str(helpers["resilience"]),
                     "--resilience-sha256", startup.sha(helpers["resilience"])]
        if baseline:
            arguments.append("--smoke-baseline")
        observed = []

        def case_driver(argv, *, prepare, preflight):
            self.assertEqual(argv[:8], ["--app", str(self.app), "--evidence", str(self.evidence),
                                       "--timeout", "7.0", "--budget", "11.0"])
            child = argv[argv.index("--") + 1:]
            self.assertEqual(child[0], sys.executable)
            self.assertEqual(Path(child[1]), Path(mcp.__file__).resolve())
            self.assertEqual(child.count("--smoke-baseline"), 1 if baseline else 0)
            self.assertFalse(self.launch_path.exists())
            receipt = {"case_id": self.context["case_id"]}
            action = prepare(None, receipt, self.environment, [], None, 12345)
            self.assertIsInstance(action, mcp.smoke.runner.CasePreparation)
            self.assertFalse(self.launch_path.exists())
            preflight(None, receipt, self.evidence, 12345)
            action.before_launch()
            self.assertIsNone(action.handoff(None, 12345))
            with patch.dict(os.environ, ZWAI_NATIVE_SCENARIO_TIMEOUT_SECONDS="7"), \
                    patch.object(mcp.smoke, "case_context", return_value=dict(self.context)) as context, \
                    patch.object(mcp, "run_baseline" if baseline else "run",
                                 side_effect=lambda value: observed.append(value) or {"passed": True}), \
                    patch.object(mcp, "run" if baseline else "run_baseline") as other_scenario:
                self.assertEqual(mcp.main(child[2:]), 0)
                context.assert_called_once_with(os.environ)
                other_scenario.assert_not_called()
            return 0

        with patch.object(mcp.smoke.runner, "main", side_effect=case_driver) as runner, \
                patch.object(mcp.smoke, "case_preflight") as preflight, redirect_stdout(io.StringIO()):
            self.assertEqual(mcp.main(arguments), 0)
        self.assertEqual(runner.call_count, 1)
        preflight.assert_called_once_with(helpers["ax"], {"case_id": "case-owned"}, self.evidence, 12345)
        self.assertEqual(len(observed), 1)
        context = observed[0]
        self.assertEqual(context["candidate"], manifest)
        self.assertEqual(context["launch"], startup.read_object(self.launch_path))
        self.assertEqual(context["launch"]["controller"], manifest["controller"])
        self.assertEqual(context["timeout"], 7)
        self.assertEqual(context["controls_sha256"], startup.sha(helpers["controls"]))
        self.assertEqual(context["resilience_sha256"], startup.sha(helpers["resilience"]))
        self.assertEqual(self.launch_path.stat().st_mode & 0o777, 0o600)

    def test_baseline_product_ready_and_smoke_share_one_deadline_and_source_receipt(self):
        context = dict(self.context, timeout=7, candidate=self.manifest,
                       launch={"prelaunch_wall_ns": 1})
        events = []
        deadline_type = mcp.smoke.Deadline

        def ready(actual_context, candidate, launch, deadline):
            events.append(("ready", deadline))
            self.assertIs(actual_context, context)
            self.assertIs(candidate, self.manifest)
            self.assertIs(launch, context["launch"])
            return self.binding

        def smoke(actual_context, *, deadline):
            events.append(("smoke", deadline))
            self.assertIs(actual_context, context)
            return {"passed": True, "case_id": context["case_id"]}

        with patch.object(mcp.smoke.runner, "require_guest"), \
                patch.object(mcp.smoke, "Deadline", wraps=deadline_type) as constructor, \
                patch.object(mcp.startup, "wait_product_ready", side_effect=ready), \
                patch.object(mcp.smoke, "run_smoke", side_effect=smoke):
            receipt = mcp.run_baseline(context)
        constructor.assert_called_once_with(7)
        self.assertEqual([name for name, _ in events], ["ready", "smoke"])
        self.assertIs(events[0][1], events[1][1])
        self.assertEqual(receipt["source_sha"], self.manifest["baseline"])
        self.assertEqual(receipt["production_inputs_sha256"], self.manifest["production_inputs_sha256"])
        self.assertEqual(receipt["archive_sha256"], self.manifest["archive_sha256"])
        destination = self.evidence / "smoke.json"
        self.assertEqual(startup.read_object(destination), receipt)
        self.assertEqual(destination.stat().st_mode & 0o777, 0o600)

    def test_real_smoke_uses_the_supplied_deadline_before_its_initial_window_wait(self):
        class ObservedInitialWait(Exception):
            pass

        deadline = mcp.smoke.Deadline(3)
        context = dict(self.context, timeout=700, ax=self.executable)
        observed = []

        def wait(actual_context, actual_deadline):
            observed.append(actual_deadline)
            self.assertIs(actual_context, context)
            raise ObservedInitialWait()

        with patch.object(mcp.smoke.runner, "require_guest"), \
                patch.object(mcp.smoke, "Deadline", side_effect=AssertionError("a supplied deadline cannot reset the budget")) as constructor, \
                patch.object(mcp.smoke, "validate_helper", return_value={"identified": True}), \
                patch.object(mcp.smoke, "assert_process", return_value=self.executable), \
                patch.object(mcp.smoke, "wait_initial_ready", side_effect=wait):
            with self.assertRaises(ObservedInitialWait):
                mcp.smoke.run_smoke(context, deadline=deadline)
        constructor.assert_not_called()
        self.assertEqual(observed, [deadline])

    def test_run_waits_for_product_ready_before_window_and_pixels_on_one_deadline(self):
        class ObservedPixels(Exception):
            pass

        context = dict(self.context, timeout=7, ax=self.executable,
                       candidate=self.manifest, launch={"prelaunch_wall_ns": 1})
        events = []
        window = {"window_id": 17}
        deadline_type = mcp.smoke.Deadline

        def ready(actual_context, candidate, launch, deadline):
            events.append(("ready", deadline))
            self.assertIs(actual_context, context)
            self.assertIs(candidate, self.manifest)
            self.assertIs(launch, context["launch"])
            self.assertNotIn("readiness_stage", actual_context)
            return self.binding

        def wait_window(actual_context, deadline, *, initial):
            events.append(("window", deadline))
            self.assertEqual(actual_context["readiness_stage"], "window")
            self.assertTrue(initial)
            return window

        def pixels(actual_context, deadline, name, actual_window):
            events.append(("pixels", deadline))
            self.assertEqual(name, "mcp-00-ready")
            self.assertIs(actual_window, window)
            raise ObservedPixels()

        with patch.object(mcp.smoke.runner, "require_guest"), \
                patch.object(mcp.smoke, "Deadline", wraps=deadline_type) as constructor, \
                patch.object(mcp.smoke, "validate_helper", return_value={"identified": True}), \
                patch.object(mcp.startup, "wait_product_ready", side_effect=ready), \
                patch.object(mcp.smoke, "wait_window", side_effect=wait_window), \
                patch.object(mcp, "save_window", side_effect=pixels), patch.object(mcp, "api") as api:
            with self.assertRaises(ObservedPixels):
                mcp.run(context)
        constructor.assert_called_once_with(7)
        self.assertEqual([name for name, _ in events], ["ready", "window", "pixels"])
        self.assertTrue(all(deadline is events[0][1] for _, deadline in events))
        api.assert_not_called()


if __name__ == "__main__":
    unittest.main()
