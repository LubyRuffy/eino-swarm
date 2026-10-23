"""Background task identity gates, independent of any model summary."""
import importlib.util
from pathlib import Path
import unittest
import json
import tempfile
import signal
import copy
import plistlib
from types import SimpleNamespace
from unittest import mock

SPEC = importlib.util.spec_from_file_location("background", Path(__file__).with_name("native_macos_resilience_background.py"))
background = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(background)


class BackgroundTests(unittest.TestCase):
    def test_actual_resource_collector_keeps_additional_webkit_clients_for_the_strict_selector(self):
        from test_native_macos_resilience_fault import SelectorTests, fault
        fixture = SelectorTests()
        fixture.setUp()
        variants = [("com.apple.WebKit.WebContent.CaptivePortal", False), ("com.apple.WebKit.GPU.Capture", False),
                    ("com.apple.WebKit.WebContent.CaptivePortal", True)]
        for index, (extra, departed) in enumerate(variants):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                observations = {row["identity"]["pid"]:copy.deepcopy(row) for row in fixture.current["observations"]}
                observed = copy.deepcopy(observations[202])
                observed["identity"] = dict(pid=999, birth=[1999996, 123], executable="/System/" + extra)
                observed["signature"][0] = "Identifier=" + extra
                observations[999] = observed
                inventory = "\n".join(str(pid) + " 1 100 " + row["identity"]["executable"]
                                       for pid, row in observations.items())
                context = dict(evidence=root, case_id=fixture.case, app_pid=200, engine_pid=300, helper="helper")
                def identity(helper, pid):
                    if departed and pid == 999:
                        raise ProcessLookupError("candidate departed during collection")
                    return observations[pid]["identity"]
                harness = SimpleNamespace(process_identity=identity,
                    atomic_json=lambda path,value:path.write_text(json.dumps(value)))
                def output(args, **kwargs):
                    return inventory if args[0] == "/bin/ps" else fixture.current["guest_os"]
                def command(args, **kwargs):
                    if args[0] == "/usr/bin/sudo":
                        fields = observations[int(args[-1])]["procinfo_fields"]
                        if int(args[-1]) == 999:
                            fields = ["program path = /System/" + extra, "pid = 999"]
                        return SimpleNamespace(returncode=0, stdout="\n".join(fields), stderr="")
                    row = next(row for row in observations.values() if row["identity"]["executable"] == args[-1])
                    return SimpleNamespace(returncode=0, stdout="", stderr="\n".join(row["signature"]))
                version = plistlib.dumps(dict(CFBundleIdentifier="com.apple.WebKit", CFBundleVersion="identified"))
                with mock.patch.object(background.subprocess, "check_output", side_effect=output), \
                        mock.patch.object(background.subprocess, "run", side_effect=command), \
                        mock.patch.object(background.time, "time", return_value=fixture.current["at"]), \
                        mock.patch.object(Path, "is_file", return_value=True), \
                        mock.patch.object(Path, "read_bytes", return_value=version):
                    background.resources(context, harness, "extra-client-" + str(index))
                snapshot = json.loads((root / ("public-process-ownership-extra-client-" + str(index) + ".json")).read_text())
                reopened = copy.deepcopy(snapshot)
                reopened["at"] = fixture.boundaries["after-reopen"]["at"]
                reopened["process_rows"] = [row for row in reopened["process_rows"] if row["pid"] != 999]
                reopened["observations"] = [row for row in reopened["observations"]
                                            if row.get("identity", {}).get("pid", row.get("pid")) != 999]
                fixture.boundaries["after-reopen"] = reopened
                with self.assertRaisesRegex(RuntimeError, "additional|incomplete|malformed"):
                    fault.select_target(fixture.boundaries, snapshot, "gpu", case_id=fixture.case,
                                        private_app=fixture.app, now=fixture.now)
                self.assertIn(999, [row["pid"] for row in snapshot["process_rows"]])
                if departed:
                    self.assertTrue(any(row.get("pid") == 999 and "observation_error" in row for row in snapshot["observations"]))

    def test_case_display_assertion_has_a_finite_deadline_and_exact_owned_cleanup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            context = dict(evidence=root, case_id="case-a", helper="helper", awake_until=110)
            identity = dict(pid=42, birth=[1, 2], executable="/usr/bin/caffeinate")
            process = mock.Mock(pid=42)
            process.poll.side_effect = [None, None, 0]
            harness = SimpleNamespace(process_identity=mock.Mock(return_value=identity), signal_identified=mock.Mock(),
                                      atomic_json=lambda path, value:path.write_text(json.dumps(value)))
            with mock.patch.object(background.time, "monotonic", return_value=10), \
                    mock.patch.object(background.subprocess, "Popen", return_value=process) as start:
                background.start_awake(context, harness)
                background.stop_awake(context, harness)
            command = start.call_args.args[0]
            self.assertEqual(command[:4], ["/usr/bin/caffeinate", "-d", "-i", "-u"])
            self.assertEqual(command[command.index("-t") + 1], "100")
            self.assertIn("-w", command)
            harness.signal_identified.assert_not_called()
            process.terminate.assert_called_once_with()
            self.assertEqual(harness.process_identity.call_count,2)
            process.wait.assert_called_once_with(timeout=5)
            self.assertTrue(json.loads((root / "display-assertion-cleanup.json").read_text())["exited"])
            self.assertNotIn("awake_process", context)

    def test_display_cleanup_rejects_changed_public_identity_without_terminating_any_process(self):
        identity=dict(pid=42,birth=[1,2],executable="/usr/bin/caffeinate")
        for observed,pid in ((dict(identity,birth=[2,2]),42),(identity,43)):
            with tempfile.TemporaryDirectory() as temporary:
                process=mock.Mock(pid=pid);process.poll.return_value=None
                context=dict(awake_process=process,awake_identity=identity,helper="/helper",evidence=Path(temporary))
                harness=SimpleNamespace(process_identity=mock.Mock(return_value=observed),signal_identified=mock.Mock(),atomic_json=mock.Mock())
                with self.assertRaisesRegex(RuntimeError,"identity"):
                    background.stop_awake(context,harness)
                process.terminate.assert_not_called();harness.signal_identified.assert_not_called()

    def setUp(self):
        self.expected = dict(worker_id="worker-a", turn_id="turn-a")
        self.current = dict(workers=1, running=True, turn_id="turn-a")
        self.trace = dict(events=[dict(kind="spawned", agent_id="worker-a", seq=8)])
        self.state = dict(worker_id="worker-a", worker_requests=2, stream_active=True,
                          cancelled_before_release=False, released=False, spawn_calls=1, errors=[])

    def test_summary_and_a_restarted_request_cannot_accept_a_live_worker(self):
        background.validate_worker("running", self.expected, self.current, self.trace, self.state)
        for patch in [dict(worker_id="replacement"), dict(worker_requests=3), dict(stream_active=False),
                      dict(cancelled_before_release=True), dict(released=True), dict(errors=["stream changed"])]:
            with self.subTest(patch=patch), self.assertRaises(RuntimeError):
                background.validate_worker("running", self.expected, self.current, self.trace, dict(self.state, **patch))

    def test_natural_finish_or_cleanup_cannot_be_mislabeled_still_running(self):
        for kind in ("finished", "cleanup"):
            trace = dict(events=self.trace["events"] + [dict(kind=kind, agent_id="worker-a")])
            with self.subTest(kind=kind), self.assertRaises(RuntimeError):
                background.validate_worker("running", self.expected, self.current, trace, self.state)
        with self.assertRaises(RuntimeError):
            background.validate_worker("running", self.expected, dict(self.current, turn_id="new-turn"), self.trace, self.state)

    def test_parked_worker_requires_actual_wait_ownership_and_no_running_manager(self):
        current = dict(workers=1, running=False, waiting=True)
        background.validate_worker("parked", self.expected, current, self.trace, self.state)
        for patch in [dict(waiting=False), dict(running=True), dict(workers=0)]:
            with self.subTest(patch=patch), self.assertRaises(RuntimeError):
                background.validate_worker("parked", self.expected, dict(current, **patch), self.trace, self.state)

    def test_schedule_identity_cadence_and_next_fire_are_independently_frozen(self):
        row = dict(id="wake-a", kind="thread", thread_id="thread-a", status="active", every_s=86400,
                   next_run_at="2026-10-23T05:00:00Z", created_by="human", cron="", max_runs=0)
        expected = dict(thread_id="thread-a", wake_id="wake-a")
        frozen = background.validate_schedule(expected, row)
        self.assertEqual(frozen, row)
        for patch in [dict(id="wake-b"), dict(thread_id="thread-b"), dict(status="paused"),
                      dict(every_s=1), dict(next_run_at="2026-10-23T05:00:01Z"),
                      dict(origin_thread_id="other"), dict(prompt="changed operation"),
                      dict(provider_id="external"), dict(until_at="2026-10-23T03:00:00Z")]:
            with self.subTest(patch=patch), self.assertRaises(RuntimeError):
                background.validate_schedule(dict(expected, schedule=frozen), dict(row, **patch))

    def test_completion_requires_actual_wait_receipt_and_original_worker_finish(self):
        expected = dict(worker_id="worker-a", thread_id="thread-a", turn_id="turn-a")
        trace = dict(turn=dict(id="turn-a", thread_id="thread-a", status="done"), events=[
            dict(kind="finished", agent_id="worker-a"),
            dict(kind="tool_call", tool_call_id="wait-a", text='wait_agents({"agent_ids":["worker-a"]})'),
            dict(kind="tool_result", tool_call_id="wait-a", text=json.dumps(dict(agents=[dict(agent_id="worker-a", status="done")]))),
        ])
        background.validate_completion("running", expected, trace, trace)
        for bad in [dict(trace, events=trace["events"][:1]),
                    dict(trace, events=[dict(trace["events"][0], err="cancelled")] + trace["events"][1:]),
                    dict(trace, turn=dict(id="other", thread_id="thread-a", status="done"))]:
            with self.subTest(bad=bad), self.assertRaises(RuntimeError):
                background.validate_completion("running", expected, bad, bad)
        latest = dict(trace, events=trace["events"] + [dict(kind="tool_result", tool_call_id="wait-a",
                      text=json.dumps(dict(agents=[dict(agent_id="worker-a", status="working")])))] )
        with self.assertRaisesRegex(RuntimeError, "wait_agents"):
            background.validate_completion("running", expected, latest, latest)

    def test_acceptance_cannot_use_summary_or_missing_background_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, "background"):
                background.accept_evidence(Path(directory), dict(background_verified=True))

    def test_long_run_uses_the_explicit_keyless_checkpoint_model_for_actual_compaction(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            context = dict(evidence=root, workspace=root, data=root, app=root / "private.app", case_id="case-a",
                           app_pid=5, engine_pid=6, helper="/helper", origin="http://127.0.0.1:12345")
            fixture = SimpleNamespace(url="http://127.0.0.1:12346/v1", models=dict(checkpoint="checkpoint", running="running", parked="parked"), stop=mock.Mock())
            seen = set()
            def process(pid):
                if pid in seen: return [], None
                seen.add(pid); return [str(root / "private.app/Contents/MacOS/zwai")], "case-a"
            harness = SimpleNamespace(api=mock.Mock(return_value=dict(settings=dict(swarm={}, memory={}))),
                runner=SimpleNamespace(process_identity=process), process_identity=lambda *args: {},
                signal_identified=mock.Mock(), wait_for=lambda check, _: check(), Deadline=lambda value: value,
                context_for=lambda *args: {})
            module = SimpleNamespace(WorkerFixture=lambda *args: fixture)
            with mock.patch.object(background.importlib, "import_module", return_value=module), mock.patch.object(background.subprocess, "Popen"):
                background.prepare(context, harness, {}, 10)
            put = next(call.args[3] for call in harness.api.call_args_list if len(call.args) > 2 and call.args[2] == "PUT")
            self.assertEqual(put["swarm"]["compact_provider"], "resilience-checkpoint")
            self.assertEqual(put["swarm"]["compact_model"], fixture.models["checkpoint"])

    def test_normal_reopen_requests_native_quit_and_selects_the_original_conversation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            context = dict(evidence=root, app=root / "private.app", data=root, app_pid=5, engine_pid=9,
                           helper="helper", package_version="0.1.36", origin="http://127.0.0.1:12345", thread_title="Original")
            def native(_, action="snapshot", value=""):
                return dict(quit_requested=True) if action == "quit" else dict(pressed=value, text="Original")
            harness = SimpleNamespace(native=mock.Mock(side_effect=native), signal_identified=mock.Mock(),
                process_identity=mock.Mock(), runner=SimpleNamespace(process_identity=lambda _: ([], None)),
                wait_for=lambda check, _: check(), Deadline=lambda value: value, case_shells=lambda _: set(),
                context_for=mock.Mock(return_value=dict(app_pid=6, engine_pid=9, origin=context["origin"])))
            with mock.patch.object(background.subprocess, "Popen") as launch, \
                    mock.patch.object(background.time, "monotonic", side_effect=[0, 3]), \
                    mock.patch.object(background, "resources") as observe:
                background.reopen(context, harness, 10)
            self.assertEqual(harness.native.call_args_list[0].args[1], "quit")
            harness.signal_identified.assert_not_called()
            self.assertNotIn("--mock", launch.call_args.args[0])
            self.assertFalse(harness.context_for.call_args.args[0]["expected_mock"])
            self.assertEqual(harness.native.call_args_list[-1].args[2], "Original")
            self.assertEqual([call.args[2] for call in observe.call_args_list],
                             ["before-quit", "after-quit", "before-reopen", "after-reopen"])

    def test_public_procinfo_diagnostics_exclude_argv_environment_and_credentials(self):
        output = "program path = /private/case/app\nresponsible pid = 123\ncoalition members = 123,456\n"
        output += "\t[2] = responsible pid = unrelated-command-data\nenvironment = {\nTOKEN = do-not-retain\n}\n"
        self.assertEqual(background.procinfo_fields(output), ["program path = /private/case/app",
            "responsible pid = 123", "coalition members = 123,456"])

    def test_complete_background_evidence_rejects_restarted_stream_or_changed_snapshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            before, fixture = {}, {}
            for kind in ("running", "parked"):
                expected = dict(thread_id="thread-" + kind, turn_id="turn-" + kind, worker_id="worker-a",
                                spawn_seq=1, marker_sha256="a" * 64, worker_requests=2)
                if kind == "parked":
                    expected.update(wake_id="wake-a", schedule=dict(id="wake-a", kind="thread", thread_id=expected["thread_id"],
                        status="active", every_s=86400, next_run_at="2026-10-23T05:00:00Z", created_by="human", cron="", max_runs=0))
                before[kind] = expected
                fixture[kind] = dict(self.state, spawn_calls=1, released=True, stream_active=False)
                original = dict(turn=dict(id=expected["turn_id"], thread_id=expected["thread_id"], status="done"), events=[
                    dict(kind="finished", agent_id="worker-a"),
                    dict(kind="tool_call", tool_call_id="wait-a", text="wait_agents({})"),
                    dict(kind="tool_result", tool_call_id="wait-a", text=json.dumps(dict(agents=[dict(agent_id="worker-a", status="done")]))),
                ])
                for suffix in ("original", "finished"):
                    (directory / ("background-" + kind + "-" + suffix + ".json")).write_text(json.dumps(original))
                live = dict(detail=dict(status=dict(workers=1, running=kind == "running", turn_id=expected["turn_id"],
                                                     waiting=kind == "parked")), fixture=self.state,
                            trace=dict(turn=original["turn"], events=[dict(kind="spawned", agent_id="worker-a", seq=1)]),
                            schedule=expected.get("schedule"))
                for suffix in ("before", "live"):
                    (directory / ("background-" + kind + "-" + suffix + ".json")).write_text(json.dumps(live))
            (directory / "background-before.json").write_text(json.dumps(before))
            after = dict(identities=before, fixture=fixture)
            (directory / "background-after.json").write_text(json.dumps(after))
            result = dict(background_verified=True, background=before)
            background.accept_evidence(directory, result)
            fixture["running"]["worker_requests"] = 3
            (directory / "background-after.json").write_text(json.dumps(after))
            with self.assertRaisesRegex(RuntimeError, "continuity"):
                background.accept_evidence(directory, result)
            fixture["running"]["worker_requests"] = 2
            (directory / "background-after.json").write_text(json.dumps(after))
            (directory / "background-running-live.json").unlink()
            with self.assertRaisesRegex(RuntimeError, "background"):
                background.accept_evidence(directory, result)


if __name__ == "__main__":
    unittest.main()
