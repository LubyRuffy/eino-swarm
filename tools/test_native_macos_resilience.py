"""Headless regressions for the guest-only resilience campaign's evidence gates."""
import importlib.util
import json
from pathlib import Path
import plistlib
import tempfile
import unittest
from unittest import mock
import zipfile
import signal
import zlib
import subprocess
import sys
import time
import argparse
from contextlib import ExitStack

SCRIPT = Path(__file__).with_name("native_macos_resilience.py")
SPEC = importlib.util.spec_from_file_location("resilience", SCRIPT)
resilience = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = resilience
SPEC.loader.exec_module(resilience)


class ResilienceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.manifest = dict(budgets=resilience.phase_budgets(resilience.phase_plan(64800, 3, 60), 180),
                             input_fingerprint="frozen-inputs")

    def cleanup_receipt(self):
        return dict(case_id="case-a", cleanup_complete=True, data_removed=True, remaining_owned_pids=[],
                    scenario_exit=0, exit_code=0, budget_exceeded=False, budget_seconds=240,
                    scenario_limit_seconds=180, timings_seconds=dict(guest_gate=.1, prepare=.1,
                    launch=.1, scenario=1, cleanup=.1, total=1.4))

    def package(self, extra=None):
        path = self.root / "zwai-0.1.36-darwin-arm64.zip"
        with zipfile.ZipFile(path, "w") as archive:
            archive.writestr("zwai.app/Contents/Info.plist", plistlib.dumps({
                "CFBundleExecutable": "zwai", "CFBundleShortVersionString": "0.1.36",
                "CFBundleVersion": "0.1.36"}))
            info = zipfile.ZipInfo("zwai.app/Contents/MacOS/zwai")
            info.external_attr = 0o100755 << 16
            archive.writestr(info, b"identified candidate")
            if extra:
                archive.writestr(*extra)
        return path

    def test_campaign_requires_full_long_run_and_three_actual_sleep_cycles(self):
        plan = resilience.phase_plan(64800, 3, 60)
        self.assertEqual([row["name"] for row in plan], ["baseline", "renderer", "gpu", "exhaustion", "sleepwake", "soak"])
        self.assertEqual(plan[-1]["duration_seconds"], 64800)
        self.assertEqual(plan[-2]["cycles"], 3)
        for args in [(0, 3, 60), (64800, 0, 60), (64800, 3, 0), (float("nan"), 3, 60)]:
            with self.subTest(args=args), self.assertRaises(ValueError):
                resilience.phase_plan(*args)

    def test_phase_selection_is_explicit_unique_and_in_full_plan_order(self):
        self.assertEqual(resilience.normalize_selection(["soak", "baseline", "sleepwake"]), ["baseline", "sleepwake", "soak"])
        self.assertEqual(resilience.normalize_selection(["soak", "gpu", "renderer", "baseline", "exhaustion", "sleepwake"]), ["baseline", "renderer", "gpu", "exhaustion", "sleepwake", "soak"])
        for phases in ([], ["baseline", "baseline"], ["unknown"]):
            with self.subTest(phases=phases), self.assertRaises(ValueError):
                resilience.normalize_selection(phases)

    def test_normal_driver_cannot_compile_or_execute_the_retired_injection_probe(self):
        with mock.patch.object(resilience.subprocess, "run") as execute:
            with self.assertRaisesRegex(ValueError, "unavailable"):
                resilience.compile_helpers(self.root, include_probe=True)
            execute.assert_not_called()
        sources = [SCRIPT.read_text(), SCRIPT.with_name("native_macos_resilience_ax.m").read_text()]
        for source in sources:
            for forbidden in ("SIGSTOP", "SIGCONT", "RESILIENCE_PROBE", "objc_msgSend", "-dynamiclib"):
                self.assertNotIn(forbidden, source)

    def test_long_campaign_explicitly_overrides_the_runner_minute_budget_without_shortening_soak(self):
        plan = resilience.phase_plan(64800, 3, 60)
        budgets = resilience.phase_budgets(plan, 180)
        self.assertEqual(budgets["soak"], dict(scenario_seconds=64980, cleanup_seconds=60, whole_case_seconds=65040))
        self.assertGreater(budgets["sleepwake"]["scenario_seconds"], 3 * 180)
        manifest = dict(package=dict(app="/private/app"), budgets=budgets)
        args = argparse.Namespace(campaign=self.root / "campaign.json", stage_timeout=180)
        command, whole = resilience.native_command(manifest, plan[-1], args, self.root / "soak")
        self.assertEqual(float(command[command.index("--budget") + 1]), whole)
        self.assertEqual(whole, 65040)
        self.assertEqual(float(command[command.index("--timeout") + 1]), 64980)

    def test_selected_normal_success_never_reports_the_whole_campaign_passed(self):
        phases = resilience.phase_plan(64800, 3, 60)
        selected = ["baseline", "sleepwake", "soak"]
        resilience.atomic_json(self.root / "campaign.json", dict(phases=phases, selection=selected, package={}))
        for name in selected:
            (self.root / name).mkdir()
            resilience.atomic_json(self.root / name / "cleanup.json", {})
        with mock.patch.object(resilience, "accepted_stage", return_value=True):
            result = resilience.campaign_status(self.root)
        self.assertFalse(result["passed"])
        self.assertTrue(result["selection_complete"])
        self.assertEqual([row["name"] for row in result["phases"] if row["status"] == "pending"], ["renderer", "gpu", "exhaustion"])

    def test_manifest_cannot_remove_pending_fault_stages_to_fabricate_a_full_pass(self):
        resilience.atomic_json(self.root / "campaign.json", dict(phases=[dict(name="baseline")], selection=["baseline"], package={}))
        (self.root / "baseline").mkdir()
        resilience.atomic_json(self.root / "baseline" / "cleanup.json", {})
        with mock.patch.object(resilience, "accepted_stage", return_value=True):
            with self.assertRaisesRegex(RuntimeError, "six-stage"):
                resilience.campaign_status(self.root)

    def test_resume_cannot_change_selection_or_replace_an_existing_stage_claim(self):
        resilience.atomic_json(self.root / "campaign.json", dict(selection=["baseline", "soak"], phases=resilience.phase_plan(64800, 3, 60)))
        claim = self.root / "baseline-claim"
        resilience.claim_stage(claim, dict(identity="original"))
        args = argparse.Namespace(evidence=self.root, phases=["baseline"], stage_timeout=180,
                                  soak_seconds=64800, sleep_cycles=3)
        with mock.patch.object(resilience, "require_guest"), mock.patch.object(resilience.subprocess, "Popen") as start:
            with self.assertRaisesRegex(RuntimeError, "selection"):
                resilience.campaign(args)
            start.assert_not_called()
        self.assertEqual(resilience.read_json(claim / "claim.json"), dict(identity="original"))

    def test_background_and_worker_sources_are_frozen_and_missing_or_mutated_dependencies_refuse_resume(self):
        for name in ("native_macos_resilience_background.py", "native_macos_resilience_worker.py", "native_macos_resilience_fault.py", "native_macos_resilience_phases.py", "native_macos_resilience_soak.py", "native_macos_input_evidence.py"):
            (self.root / name).write_text("identified helper " + name)
        with mock.patch.object(resilience, "HERE", self.root):
            dependencies = resilience.dependency_hashes()
            self.assertEqual(set(dependencies), {"native_macos_resilience_background.py", "native_macos_resilience_worker.py", "native_macos_resilience_fault.py", "native_macos_resilience_phases.py", "native_macos_resilience_soak.py", "native_macos_input_evidence.py"})
            resilience.validate_dependencies(dict(dependencies=dependencies))
            with self.assertRaisesRegex(RuntimeError, "dependencies"):
                resilience.validate_dependencies({})
            (self.root / "native_macos_resilience_worker.py").write_text("changed worker behavior")
            with self.assertRaisesRegex(RuntimeError, "dependencies"):
                resilience.validate_dependencies(dict(dependencies=dependencies))
            (self.root / "native_macos_resilience_worker.py").unlink()
            with self.assertRaisesRegex(RuntimeError, "dependencies"):
                resilience.validate_dependencies(dict(dependencies=dependencies))

    def test_changed_inputs_are_refused_before_a_scenario_touches_the_native_case(self):
        manifest = self.root / "campaign.json"
        resilience.atomic_json(manifest, dict(phases=resilience.phase_plan(64800, 3, 60)))
        args = argparse.Namespace(campaign=manifest, phase="baseline", timeout=180)
        with mock.patch.object(resilience, "require_guest"), \
                mock.patch.object(resilience, "verify_inputs", side_effect=RuntimeError("inputs changed")), \
                mock.patch.object(resilience, "context_for") as native_case:
            with self.assertRaisesRegex(RuntimeError, "inputs changed"):
                resilience.scenario(args)
            native_case.assert_not_called()

    def test_actual_scenario_routes_faults_and_completes_a_final_real_soak_tick(self):
        plan=resilience.phase_plan(64800,3,60)
        manifest=dict(phases=plan,budgets=resilience.phase_budgets(plan,180),helper="/helper")
        campaign=self.root/"campaign.json";resilience.atomic_json(campaign,manifest)
        expected=dict(text="unsent",files=[],quotes=[],state="editing")
        for phase in ("renderer","gpu","exhaustion","soak"):
            directory=self.root/phase;directory.mkdir()
            env=dict(ZWAI_NATIVE_CASE_ID="a"*32,ZWAI_NATIVE_DATA=str(directory),ZWAI_NATIVE_EVIDENCE=str(directory))
            context=dict(engine_pid=9,origin="http://127.0.0.1:1",workspace=directory,thread_id="thread-a")
            def submit(ctx,text,timeout,result,**kwargs):result["turn_ids"].append("turn-"+str(len(result["turn_ids"])))
            def start(ctx,*args):ctx["background"]={"running":dict(turn_id="running-a"),"parked":dict(turn_id="parked-a")}
            with self.subTest(phase=phase),ExitStack() as stack:
                stack.enter_context(mock.patch.dict(resilience.os.environ,env))
                for name,value in {"require_guest":None,"verify_inputs":"fingerprint","context_for":context,
                        "snapshot":"real.png","native":dict(textareas=1),"collect_draft":expected,
                        "verify_draft":expected,"draft_snapshot":expected,"turn_rows":[dict(id="first")],
                        "read_json":None}.items():
                    if name=="read_json":continue
                    stack.enter_context(mock.patch.object(resilience,name,return_value=value))
                stack.enter_context(mock.patch.object(resilience,"submit",side_effect=submit))
                for name in ("start_awake","prepare","resources","check","finish","retain","stop_awake","reopen"):
                    stack.enter_context(mock.patch.object(resilience.background,name))
                stack.enter_context(mock.patch.object(resilience.background,"start",side_effect=start))
                fault_run=stack.enter_context(mock.patch.object(resilience.fault_phases,"run"))
                soak_start=stack.enter_context(mock.patch.object(resilience.soak,"start",return_value=dict(started_monotonic=0)))
                stack.enter_context(mock.patch.object(resilience.soak,"begin_tick",return_value={}))
                ticks=stack.enter_context(mock.patch.object(resilience.soak,"record_tick",side_effect=[dict(seq=1),dict(seq=2)]))
                finish=stack.enter_context(mock.patch.object(resilience.soak,"finish"))
                stack.enter_context(mock.patch.object(resilience.time,"monotonic",side_effect=[0,1,64801]))
                sleep=stack.enter_context(mock.patch.object(resilience.time,"sleep"))
                (directory/"engine.json").write_text(json.dumps(dict(pid=9)))
                self.assertEqual(resilience.scenario(argparse.Namespace(campaign=campaign,phase=phase,timeout=180)),0)
                if phase=="soak":
                    fault_run.assert_not_called();soak_start.assert_called_once();finish.assert_called_once()
                    self.assertEqual(ticks.call_count,2);sleep.assert_called_once_with(60)
                else:
                    self.assertEqual(fault_run.call_args.args[2],phase);soak_start.assert_not_called()
                self.assertTrue(resilience.read_json(directory/"resilience.json")["passed"])

    def test_package_identity_binds_source_zip_and_executable_bytes(self):
        package = self.package()
        with mock.patch.object(resilience.subprocess, "check_output", return_value="\tbuild\tvcs.revision=" + "a" * 40 + "\n\tbuild\tvcs.modified=false\n"):
            identity = resilience.prepare_package(package, "a" * 40, self.root / "prepared")
        self.assertEqual(identity["source_sha"], "a" * 40)
        self.assertEqual(identity["zip_sha256"], resilience.sha256(package))
        self.assertEqual(identity["executable_sha256"], resilience.sha256(Path(identity["executable"])))
        self.assertEqual(identity["version"], "0.1.36")

    def test_declared_source_cannot_override_missing_dirty_or_different_embedded_build_identity(self):
        for output in ["binary: go1.26\n", "\tbuild\tvcs.revision=" + "b" * 40 + "\n\tbuild\tvcs.modified=false\n",
                       "\tbuild\tvcs.revision=" + "a" * 40 + "\n\tbuild\tvcs.modified=true\n"]:
            with self.subTest(output=output), mock.patch.object(resilience.subprocess, "check_output", return_value=output):
                with self.assertRaisesRegex(RuntimeError, "provenance pending"):
                    resilience.build_identity(Path("/package/zwai"), "a" * 40)

    def test_extract_rejects_paths_and_symlinks_before_writing(self):
        link = zipfile.ZipInfo("zwai.app/Contents/link")
        link.external_attr = 0o120777 << 16
        for entry in [("../outside", b"bad"), ("/absolute", b"bad"), (link, b"/outside")]:
            with self.subTest(entry=entry), self.assertRaises(ValueError):
                resilience.prepare_package(self.package(entry), "a" * 40, self.root / "prepared")
        self.assertFalse((self.root / "outside").exists())

    def test_source_claim_must_be_a_full_commit(self):
        with self.assertRaises(ValueError):
            resilience.prepare_package(self.package(), "latest-main", self.root / "prepared")

    def test_go_timestamp_fraction_lengths_work_on_the_guest_python(self):
        base = resilience.timestamp("2026-10-22T04:50:58Z")
        for digits in ("7", "75", "754", "7544", "75446", "754460", "7544601", "75446012", "754460123"):
            with self.subTest(digits=digits):
                self.assertAlmostEqual(resilience.timestamp("2026-10-22T04:50:58." + digits + "Z") - base,
                                       float("0." + digits[:6]), places=5)
        with self.assertRaises(ValueError):
            resilience.timestamp("2026-10-22T04:50:58")

    def test_native_gate_refuses_physical_host_before_any_process(self):
        with mock.patch.object(resilience.runner, "require_guest", side_effect=RuntimeError("physical host")), \
                mock.patch.object(resilience.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "physical host"):
                resilience.require_guest()
            run.assert_not_called()

    def test_context_accepts_only_the_declared_keyless_fixture_engine_after_public_pixel_ready(self):
        import os
        resilience.atomic_json(self.root / "engine.json", dict(pid=9, url="http://127.0.0.1:12345"))
        diagnostic = self.root / "diagnostics"
        diagnostic.mkdir()
        (diagnostic / "desktop-fixture.jsonl").write_text(json.dumps(dict(phase="ready", shell_pid=5)) + "\n")
        env = {key:str(self.root) for key in ("ZWAI_NATIVE_APP", "ZWAI_NATIVE_DATA", "ZWAI_NATIVE_WORKSPACE", "ZWAI_NATIVE_EVIDENCE")}
        env["ZWAI_NATIVE_CASE_ID"] = "case-a"
        meta = dict(mock=False, data_dir=str(self.root.resolve()), version="0.0.0", clients=[dict(surface="desktop", pid=5)])
        with mock.patch.dict(os.environ, env), mock.patch.object(resilience, "api", return_value=meta), \
                mock.patch.object(resilience, "native", return_value={}):
            context = resilience.context_for(dict(helper="/helper", package=dict(version="0.0.0"), expected_mock=False), 1)
        self.assertEqual(context["engine_pid"], 9)
        self.assertFalse(context["expected_mock"])

    def test_initial_ready_failure_preserves_private_diagnostics_before_case_cleanup(self):
        import os
        env = {key:str(self.root) for key in ("ZWAI_NATIVE_APP", "ZWAI_NATIVE_DATA", "ZWAI_NATIVE_WORKSPACE", "ZWAI_NATIVE_EVIDENCE")}
        env["ZWAI_NATIVE_CASE_ID"] = "case-a"
        with mock.patch.dict(os.environ, env), \
                mock.patch.object(resilience, "wait_for", side_effect=TimeoutError("public ready missing")), \
                mock.patch.object(resilience.background, "retain") as retain:
            with self.assertRaisesRegex(TimeoutError, "public ready missing"):
                resilience.context_for(dict(helper="/helper", package=dict(version="0.0.0")), 1)
        retain.assert_called_once()
        failure = resilience.read_json(self.root / "readiness-failure.json")
        self.assertEqual(failure["case_id"], "case-a")
        self.assertIn("public ready missing", failure["error"])

    def test_explicit_send_checks_the_existing_trimmed_transport_body_after_exact_draft_input(self):
        text = "  native draft 中文  "
        context = dict(evidence=self.root, origin="http://127.0.0.1:12345")
        turn = dict(id="turn-new", thread_id="thread-a", status="done", user_text=text.strip(), final="Completed", error="")
        result = dict(turn_ids=[])
        with mock.patch.object(resilience, "native", return_value=dict(values=[text], text="Completed")), \
                mock.patch.object(resilience, "turn_rows", side_effect=[[], [turn]]), \
                mock.patch.object(resilience, "api", return_value=dict(llm_calls=[dict(model="mock")])):
            self.assertEqual(resilience.submit(context, text, 1, result), turn)
        self.assertEqual(result["turn_ids"], ["turn-new"])

    def test_attachment_selection_waits_for_the_real_sheet_before_one_choose_action(self):
        import hashlib
        context=dict(case_id="a"*32,app_pid=7,workspace=self.root,evidence=self.root)
        state=dict(text="",files=[],dialogs=0,choices=0,panel_ready=False)
        def draft(ctx):return dict(text=state["text"],files=list(state["files"]),quotes=[],state="editing")
        def native(ctx,action="snapshot",value=""):
            if action=="fill":state["text"]=value;return dict(filled=True)
            if action=="dialogs":
                state["dialogs"]+=1;state["panel_ready"]=state["dialogs"]>=2
                return dict(case_id=context["case_id"],pid=7,rows=[dict(AXRole="AXSheet")] if state["panel_ready"] else [])
            if action=="choose-file":
                if not state["panel_ready"]:raise OSError("attachment sheet is not present yet")
                state["choices"]+=1
                path=Path(value);state["files"].append(dict(id="binary",name=path.name,kind="file",sha256=hashlib.sha256(path.read_bytes()).hexdigest()))
                return dict(chosen=True)
            if action=="paste-file":
                path=Path(value);state["files"].append(dict(id="image",name=path.name,kind="image",sha256=hashlib.sha256(path.read_bytes()).hexdigest()))
            return dict(values=[state["text"]],text="Draft saved")
        with mock.patch.object(resilience,"native",side_effect=native),mock.patch.object(resilience,"draft_snapshot",side_effect=draft), \
                mock.patch.object(resilience.time,"sleep"):
            row=resilience.collect_draft(context,5)
        self.assertEqual(state["dialogs"],2);self.assertEqual(state["choices"],1)
        self.assertEqual(len(row["files"]),2)
        self.assertEqual(resilience.read_json(self.root/"attachment-panel-ready.json")["rows"][0]["AXRole"],"AXSheet")

    def test_collection_retains_last_existing_observation_without_extra_input_or_reads(self):
        import copy
        for mode in ("success", "ack_transient", "text", "state", "count", "hash", "kind", "native", "api_error", "native_error", "ack", "ack_error", "interrupt", "paste_error"):
            with self.subTest(mode=mode):
                root=self.root/mode;root.mkdir()
                context=dict(case_id="a"*32,app_pid=7,workspace=root,evidence=root)
                state=dict(text="",files=[],pasted=False,reads=0,snapshots=0)
                actions=[];problem=OSError("isolated "+mode+" observation failed")
                def native(ctx,action="snapshot",value=""):
                    actions.append(action)
                    if action=="fill":state["text"]=value
                    if action=="dialogs":return dict(case_id=ctx["case_id"],pid=7,rows=[dict(AXRole="AXSheet")])
                    if action=="choose-file":
                        state["files"].append(dict(id="binary",name=Path(value).name,kind="file",mime="application/octet-stream",size=275,sha256=resilience.sha256(Path(value))))
                        return dict(chosen=True)
                    if action=="paste-file":
                        if mode=="paste_error":raise problem
                        state["pasted"]=True
                        state["files"].append(dict(id="image",name=Path(value).name,kind="image",mime="image/png",size=78,sha256=resilience.sha256(Path(value))))
                        return dict(pasted=True,pid=7,case_id=ctx["case_id"])
                    if action=="snapshot":
                        state["snapshots"]+=1
                        if mode=="native_error" or (mode=="ack_error" and state["snapshots"]>1) or (mode=="ack_transient" and state["snapshots"]==2):raise problem
                    return dict(values=["changed" if mode=="native" else state["text"]],text="" if mode=="ack" else "Draft saved")
                def draft(ctx):
                    row=dict(text=state["text"],quotes=[],files=copy.deepcopy(state["files"]),state="editing")
                    if state["pasted"]:
                        state["reads"]+=1
                        if mode=="api_error":raise problem
                        if mode=="text":row["text"]="changed"
                        if mode=="state":row["state"]="sending"
                        if mode in ("count","interrupt"):row["files"]=row["files"][:1]
                        if mode=="hash":row["files"][1]["sha256"]="different"
                        if mode=="kind":row["files"][1]["kind"]="file"
                    return row
                clock=[0.0];interrupted=KeyboardInterrupt("private interruption")
                def sleep(seconds):
                    if mode=="interrupt":raise interrupted
                    clock[0]+=seconds
                with mock.patch.object(resilience,"native",side_effect=native),mock.patch.object(resilience,"draft_snapshot",side_effect=draft), \
                        mock.patch.object(resilience.time,"monotonic",side_effect=lambda:clock[0]), \
                        mock.patch.object(resilience.time,"sleep",side_effect=sleep), \
                        mock.patch.object(resilience,"atomic_json",wraps=resilience.atomic_json) as writes:
                    if mode in ("success","ack_transient"):
                        row=resilience.collect_draft(context,1)
                        self.assertEqual(len(row["files"]),2)
                    elif mode=="interrupt":
                        with self.assertRaises(KeyboardInterrupt) as caught:resilience.collect_draft(context,1)
                        self.assertIs(caught.exception,interrupted)
                    elif mode=="paste_error":
                        with self.assertRaises(OSError) as caught:resilience.collect_draft(context,1)
                        self.assertIs(caught.exception,problem)
                    else:
                        with self.assertRaises(TimeoutError) as caught:resilience.collect_draft(context,1)
                        if mode in ("api_error","native_error","ack_error"):self.assertIs(caught.exception.__cause__,problem)
                self.assertEqual(actions[:5],["fill","press","dialogs","choose-file","paste-file"])
                self.assertEqual(actions.count("paste-file"),1)
                self.assertEqual(actions[5:],["snapshot"]*state["snapshots"])
                if mode in ("text","state","count","hash","kind","api_error","interrupt","paste_error"):
                    self.assertEqual(state["snapshots"],0)
                elif mode in ("success","ack_transient"):self.assertEqual(state["snapshots"],3 if mode=="ack_transient" else 2)
                elif mode in ("ack","ack_error"):self.assertGreater(state["snapshots"],1);self.assertEqual(state["reads"],1)
                else:self.assertEqual(state["snapshots"],state["reads"])
                observed=resilience.read_json(root/"draft-collection-last.json")
                self.assertEqual(observed["case_id"],context["case_id"])
                self.assertEqual(observed["api"]["text"]["sha256"],resilience.hashlib.sha256(("changed" if mode=="text" else state["text"]).encode()).hexdigest())
                if mode in ("success","ack_transient"):
                    self.assertNotIn("observation_error",observed)
                    self.assertTrue(all(observed["predicates"].values()))
                    self.assertNotIn("error",observed)
                else:
                    self.assertEqual(observed["error"]["type"],"OSError" if mode=="paste_error" else "KeyboardInterrupt" if mode=="interrupt" else "TimeoutError")
                    self.assertEqual(observed["error"]["stage"],"paste" if mode=="paste_error" else "acknowledgement" if mode in ("ack","ack_error") else "saved")
                if mode in ("api_error","native_error","ack_error"):
                    self.assertEqual(observed["observation_error"],dict(type="OSError"))
                if mode in ("text","state","count","hash","kind"):
                    key={"text":"text","state":"state","count":"file_count","hash":"file_hash_kinds","kind":"file_hash_kinds"}[mode]
                    self.assertFalse(observed["predicates"][key]);self.assertIsNone(observed["native"])
                if mode=="native":self.assertFalse(observed["predicates"]["native_values"])
                if mode=="ack":self.assertFalse(observed["predicates"]["saved_acknowledged"])
                if mode in ("api_error","interrupt"):self.assertEqual(observed["api"]["file_count"],1)
                if mode!="paste_error":
                    self.assertTrue(resilience.read_json(root/"draft-image-paste.json")["pasted"])
                self.assertEqual(sorted(path.name for path in root.glob("draft-collection*")),["draft-collection-last.json"])
                self.assertLess((root/"draft-collection-last.json").stat().st_size,4096)
                self.assertNotIn("Unsent recovery input",(root/"draft-collection-last.json").read_text())
                self.assertNotIn("changed",(root/"draft-collection-last.json").read_text())
                self.assertEqual(sum(Path(call.args[0]).name=="draft-collection-last.json" for call in writes.call_args_list),1)

    def test_collection_write_failure_keeps_original_exception_and_success_fails_closed(self):
        import io
        from itertools import product
        class UnwritableStderr(io.StringIO):
            def write(self,value):raise OSError("private stderr detail")
        original_write=resilience.atomic_json
        for kind,target,broken_warning in product(("success", "timeout", "api", "interrupt"),
                ("draft-image-paste.json", "draft-collection-last.json"),(False,True)):
            with self.subTest(kind=kind,target=target,broken_warning=broken_warning):
                root=self.root/(kind+target+str(broken_warning));root.mkdir()
                context=dict(case_id="a"*32,app_pid=7,workspace=root,evidence=root)
                state=dict(text="",files=[],waits=0);failed=[];stderr=UnwritableStderr() if broken_warning else io.StringIO()
                problem={"timeout":TimeoutError,"api":OSError,"interrupt":KeyboardInterrupt}.get(kind,RuntimeError)("private business detail")
                cause=ValueError("original upstream cause");problem.__cause__=cause
                write_error=OSError("private write detail")
                def native(ctx,action="snapshot",value=""):
                    if action=="fill":state["text"]=value
                    if action=="dialogs":return dict(case_id=ctx["case_id"],pid=7,rows=[dict(AXRole="AXSheet")])
                    if action in ("choose-file","paste-file"):
                        state["files"].append(dict(kind="file" if action=="choose-file" else "image",sha256=resilience.sha256(Path(value))))
                        return dict(chosen=True,pasted=True)
                    return dict(values=[state["text"]],text="Draft saved")
                def draft(ctx):return dict(text=state["text"],state="editing",files=list(state["files"]))
                def wait(check,deadline):
                    state["waits"]+=1;value=check()
                    if state["waits"]==3 and kind!="success":raise problem
                    return value
                def write(path,value):
                    if Path(path).name==target:failed.append(path);raise write_error
                    return original_write(path,value)
                with mock.patch.object(resilience,"native",side_effect=native),mock.patch.object(resilience,"draft_snapshot",side_effect=draft), \
                        mock.patch.object(resilience,"wait_for",side_effect=wait),mock.patch.object(resilience,"atomic_json",side_effect=write), \
                        mock.patch.object(resilience.sys,"stderr",stderr):
                    if kind=="success":
                        with self.assertRaisesRegex(RuntimeError,"evidence") as caught:resilience.collect_draft(context,5)
                        self.assertIs(caught.exception.__cause__,write_error)
                    else:
                        with self.assertRaises(type(problem)) as caught:resilience.collect_draft(context,5)
                        self.assertIs(caught.exception,problem);self.assertIs(caught.exception.__cause__,cause)
                        if not broken_warning:self.assertIn("WARN draft_collection_evidence_write_failed",stderr.getvalue())
                self.assertEqual(len(failed),1)
                self.assertNotIn("private business detail",stderr.getvalue());self.assertNotIn("private write detail",stderr.getvalue())

    def test_native_attachment_send_matches_uploaded_bytes_notice_and_actual_vision_input(self):
        import hashlib
        binary, image = b"unchanged attachment", resilience.fixture_png()
        files = [dict(id="file-a", name="input.bin", size=len(binary), kind="file", mime="application/octet-stream", sha256=hashlib.sha256(binary).hexdigest()),
                 dict(id="image-a", name="image.png", size=len(image), kind="image", mime="image/png", sha256=hashlib.sha256(image).hexdigest())]
        context = dict(origin="http://127.0.0.1:1", evidence=self.root)
        text = "  unsent 中文  "
        wire = text.strip() + "\n\nAttached files:\n- uploads/input.bin"
        row = dict(id="turn-a", thread_id="thread-a", user_text=wire)
        trace = dict(events=[dict(kind="user_message", text=wire, images=[dict(id="img_a", mime="image/png", name="image.png")])])
        listing = dict(files=[dict(path="uploads/input.bin", name="input.bin", size=len(binary), uploaded=True, dir=False)])
        with mock.patch.object(resilience, "api", return_value=listing), \
                mock.patch.object(resilience.input_evidence, "retain"), \
                mock.patch.object(resilience, "download_input", side_effect=[binary, image]) as download:
            resilience.validate_sent_input(context, text, dict(files=files), row, trace)
        self.assertEqual(download.call_args_list[0].args[1], "/api/threads/thread-a/download/uploads/input.bin")
        self.assertEqual(download.call_args_list[1].args[1], "/api/threads/thread-a/input-images/img_a")
        for mutation in ("bytes", "notice", "image", "duplicate"):
            changed = json.loads(json.dumps(trace))
            if mutation == "image": changed["events"][0]["images"] = []
            if mutation == "duplicate": changed["events"] *= 2
            bad_row = dict(row, user_text="wrong") if mutation == "notice" else row
            with self.subTest(mutation=mutation), mock.patch.object(resilience, "api", return_value=listing), \
                    mock.patch.object(resilience.input_evidence, "retain"), \
                    mock.patch.object(resilience, "download_input", side_effect=[b"changed" if mutation == "bytes" else binary, image]):
                with self.assertRaisesRegex(RuntimeError, "input|attachment|image"):
                    resilience.validate_sent_input(context, text, dict(files=files), bad_row, changed)

    def test_owned_process_term_adapter_covers_display_and_actual_offline_prepare_calls(self):
        import native_macos_resilience_worker as worker
        case="a"*32;app=self.root/("zwai-native-"+case+"-owned")/"zwai.app"
        case_env=dict(ZWAI_NATIVE_CASE_ID=case,ZWAI_NATIVE_APP=str(app))
        identity=dict(pid=123,birth=[10,20],executable="/usr/bin/caffeinate")
        process=mock.Mock(pid=123);process.poll.side_effect=[None,0]
        context=dict(awake_process=process,awake_identity=identity,helper="/helper",evidence=self.root)
        with mock.patch.dict(resilience.os.environ,case_env), \
                mock.patch.object(resilience.runner,"process_identity",return_value=(None,None)) as arguments, \
                mock.patch.object(resilience,"process_identity",return_value=identity),mock.patch.object(resilience.os,"kill") as kill:
            resilience.background.stop_awake(context,resilience)
        kill.assert_not_called();arguments.assert_not_called();process.terminate.assert_called_once_with();process.wait.assert_called_once_with(timeout=5)
        self.assertNotIn("awake_process",context)
        self.assertTrue(resilience.read_json(self.root/"display-assertion-cleanup.json")["exited"])
        context=dict(case_id=case,app=app,app_pid=333,engine_pid=444,helper="/helper",evidence=self.root,workspace=self.root,
                     origin="http://127.0.0.1:1",data=self.root/"data")
        identities={pid:dict(pid=pid,birth=[10,pid],executable=str(app/"Contents/MacOS/zwai")) for pid in (333,444)}
        counts={}
        def process_args(pid):
            counts[pid]=counts.get(pid,0)+1
            return ([str(app/"Contents/MacOS/zwai"),"desktop" if pid==333 else "engine"],case) if counts[pid]<=2 else (None,None)
        with mock.patch.dict(resilience.os.environ,case_env), \
                mock.patch.object(resilience.runner,"process_identity",side_effect=process_args), \
                mock.patch.object(resilience,"process_identity",side_effect=lambda helper,pid:identities[pid]), \
                mock.patch.object(resilience.os,"kill") as kill, \
                mock.patch.object(resilience,"api",return_value=dict(settings=dict(swarm={},memory={}))), \
                mock.patch.object(resilience,"wait_for",side_effect=lambda check,deadline:check()), \
                mock.patch.object(resilience,"context_for",return_value=dict(app_pid=555,engine_pid=666)), \
                mock.patch.object(resilience.subprocess,"Popen"),mock.patch.object(worker,"WorkerFixture") as fixture:
            fixture.return_value.models=dict(checkpoint="offline");fixture.return_value.url="http://127.0.0.1:1/v1"
            resilience.background.prepare(context,resilience,{},10)
        self.assertEqual(kill.call_args_list,[mock.call(333,signal.SIGTERM),mock.call(444,signal.SIGTERM)])
        self.assertEqual(context["engine_pid"],666)
        for changed,signum,args,marker in ((dict(identity,birth=[11,20]),signal.SIGTERM,["/usr/bin/caffeinate"],case),
                (identity,signal.SIGSTOP,["/usr/bin/caffeinate"],case),
                (identity,signal.SIGTERM,["/usr/bin/caffeinate"],"b"*32),
                (identity,signal.SIGTERM,["/bin/other"],case)):
            with mock.patch.dict(resilience.os.environ,case_env), \
                    mock.patch.object(resilience.runner,"process_identity",return_value=(args,marker)), \
                    mock.patch.object(resilience,"process_identity",return_value=changed),mock.patch.object(resilience.os,"kill") as kill:
                with self.assertRaisesRegex(RuntimeError,"identity|owned"):
                    resilience.signal_identified(Path("/helper"),identity,signum)
                kill.assert_not_called()

    def test_display_child_exit_during_actual_identity_probe_is_reaped_but_a_live_probe_failure_is_retained(self):
        identity=dict(pid=123,birth=[10,20],executable="/usr/bin/caffeinate")
        for gone in (True,False):
            process=mock.Mock(pid=123);process.poll.side_effect=[None,0,0] if gone else [None,None]
            context=dict(awake_process=process,awake_identity=identity,helper="/helper",evidence=self.root)
            failure=subprocess.CalledProcessError(2,["/helper","identity","123"],stderr="process identity unavailable")
            with self.subTest(gone=gone),mock.patch.object(resilience.subprocess,"run",side_effect=failure):
                if gone:
                    resilience.background.stop_awake(context,resilience)
                    process.wait.assert_called_once_with(timeout=5)
                    self.assertTrue(resilience.read_json(self.root/"display-assertion-cleanup.json")["exited"])
                    self.assertNotIn("awake_process",context)
                else:
                    with self.assertRaises(subprocess.CalledProcessError):resilience.background.stop_awake(context,resilience)
                    process.wait.assert_not_called();self.assertIn("awake_process",context)
                process.terminate.assert_not_called()

    def test_accepted_stage_needs_matching_cleanup_and_actual_scenario_evidence(self):
        stage = self.root / "baseline"
        stage.mkdir()
        cleanup = self.cleanup_receipt()
        result = dict(case_id="case-a", passed=True, phase="baseline", engine_pid_before=9,
                      engine_pid_after=9, snapshots=["before.png", "after.png"],
                      turn_ids=["turn-a", "turn-b"], draft_verified=True, input_fingerprint="frozen-inputs")
        for name in result["snapshots"]:
            (stage / name).write_bytes(b"PNG is validated by the guest helper")
        for name, row in [("cleanup.json", cleanup), ("resilience.json", result)]:
            resilience.atomic_json(stage / name, row)
        with mock.patch.object(resilience.background, "accept_evidence") as background_gate, \
                mock.patch.object(resilience.input_evidence, "accept_stage") as input_gate:
            self.assertTrue(resilience.accepted_stage(stage, "baseline", self.manifest))
            background_gate.assert_called_once_with(stage, result)
            input_gate.assert_called_once_with(stage, result, resilience.fixture_png())
        for patch in [dict(case_id="other-case"), dict(engine_pid_after=10), dict(draft_verified=False),
                      dict(turn_ids=["turn-a"]), dict(snapshots=["missing.png"])]:
            resilience.atomic_json(stage / "resilience.json", dict(result, **patch))
            with self.subTest(patch=patch), self.assertRaises(RuntimeError):
                resilience.accepted_stage(stage, "baseline", self.manifest)
        resilience.atomic_json(stage / "resilience.json", result)
        resilience.atomic_json(stage / "cleanup.json", dict(cleanup, remaining_owned_pids=[8]))
        with self.assertRaises(RuntimeError):
            resilience.accepted_stage(stage, "baseline", self.manifest)

    def test_done_turns_and_screenshots_without_running_and_parked_evidence_cannot_pass(self):
        cleanup = self.cleanup_receipt()
        result = dict(case_id="case-a", passed=True, phase="baseline", engine_pid_before=9, engine_pid_after=9,
                      snapshots=["before.png", "after.png"], turn_ids=["turn-a", "turn-b"], draft_verified=True,
                      input_fingerprint="frozen-inputs")
        for name in result["snapshots"]: (self.root / name).write_bytes(b"image")
        resilience.atomic_json(self.root / "cleanup.json", cleanup)
        resilience.atomic_json(self.root / "resilience.json", result)
        with self.assertRaisesRegex(RuntimeError, "background"):
            resilience.accepted_stage(self.root, "baseline", self.manifest)

    def test_successful_scenario_with_runner_budget_failure_or_old_receipt_cannot_pass(self):
        result = dict(case_id="case-a", passed=True, phase="baseline", engine_pid_before=9, engine_pid_after=9,
                      snapshots=["before.png", "after.png"], turn_ids=["turn-a", "turn-b"], draft_verified=True,
                      input_fingerprint="frozen-inputs")
        for name in result["snapshots"]: (self.root / name).write_bytes(b"image")
        resilience.atomic_json(self.root / "resilience.json", result)
        for patch in [dict(exit_code=124, budget_exceeded=True), dict(exit_code=None), dict(budget_exceeded=None),
                      dict(budget_seconds=60), dict(timings_seconds=dict(total=241)),
                      dict(timings_seconds=dict(total=float("inf")))]:
            receipt = dict(self.cleanup_receipt(), **patch)
            (self.root / "cleanup.json").write_text(json.dumps(receipt))
            with self.subTest(patch=patch), mock.patch.object(resilience.background, "accept_evidence"), self.assertRaisesRegex(RuntimeError, "budget"):
                resilience.accepted_stage(self.root, "baseline", self.manifest)

    def test_unknown_or_failed_attempt_is_never_implicitly_repeated(self):
        phase = self.root / "gpu"
        phase.mkdir()
        resilience.atomic_json(phase / "claim.json", {"phase": "gpu"})
        with self.assertRaisesRegex(RuntimeError, "already claimed"):
            resilience.claim_stage(phase, {"phase": "gpu"})
        self.assertEqual(json.loads((phase / "claim.json").read_text()), {"phase": "gpu"})

    def test_fault_and_soak_claims_must_reach_their_raw_evidence_gates(self):
        for name in ("renderer", "gpu", "exhaustion", "soak"):
            stage = self.root / name; stage.mkdir()
            cleanup = self.cleanup_receipt()
            budget = self.manifest["budgets"][name]
            cleanup.update(budget_seconds=budget["whole_case_seconds"], scenario_limit_seconds=budget["scenario_seconds"])
            result = dict(case_id="case-a", passed=True, phase=name, engine_pid_before=9, engine_pid_after=9,
                snapshots=["before.png", "after.png"], turn_ids=["a", "b"], draft_verified=True,
                soak_elapsed_seconds=64800, input_fingerprint="frozen-inputs")
            for filename in result["snapshots"]: (stage / filename).write_bytes(b"image")
            resilience.atomic_json(stage / "cleanup.json", cleanup)
            resilience.atomic_json(stage / "resilience.json", result)
            manifest = dict(self.manifest, phases=resilience.phase_plan(64800,3,60))
            gate = resilience.soak if name == "soak" else resilience.fault_phases
            with mock.patch.object(resilience.background, "accept_evidence"), \
                    mock.patch.object(resilience.input_evidence, "accept_stage"), \
                    mock.patch.object(gate, "accept_evidence") as evidence:
                self.assertTrue(resilience.accepted_stage(stage,name,manifest))
                evidence.assert_called_once()
                evidence.side_effect = RuntimeError("raw evidence incomplete")
                with self.assertRaisesRegex(RuntimeError,"raw evidence"):
                    resilience.accepted_stage(stage,name,manifest)

    def test_campaign_cancellation_waits_for_actual_child_cleanup(self):
        script = self.root / "owned-runner.py"
        script.write_text("import signal,time,pathlib,sys\n"
                          "root=pathlib.Path(sys.argv[1])\n"
                          "def stop(*args):\n (root/'cleanup.finished').write_text('closed engine and data')\n sys.exit(130)\n"
                          "signal.signal(signal.SIGTERM,stop)\n(root/'ready').touch()\nwhile True: time.sleep(.01)\n")
        process = subprocess.Popen([sys.executable, str(script), str(self.root)], start_new_session=True)
        try:
            deadline = time.monotonic() + 5
            while not (self.root / "ready").exists() and time.monotonic() < deadline:
                time.sleep(.01)
            self.assertTrue((self.root / "ready").exists())
            resilience.atomic_json(self.root / "cancel.request.json", dict(launch_id="mine"))
            with self.assertRaises(KeyboardInterrupt):
                resilience.wait_native_case(process, 64800, self.root, "mine", self.root, "/unused-helper")
            self.assertEqual(process.poll(), 130)
            self.assertTrue((self.root / "cleanup.finished").exists())
        finally:
            if process.poll() is None:
                process.terminate()
            process.wait(timeout=5)

    def test_deadlines_reject_nan_and_infinite_waits(self):
        for seconds in [0, -1, float("nan"), float("inf")]:
            with self.subTest(seconds=seconds), self.assertRaises(ValueError):
                resilience.Deadline(seconds)
        with mock.patch.object(resilience.time, "monotonic", side_effect=[0, 2]):
            deadline = resilience.Deadline(1)
            with self.assertRaises(TimeoutError):
                deadline.remaining()

    def test_image_fixture_is_a_decodable_three_by_two_rgba_png(self):
        import struct
        data = resilience.fixture_png()
        self.assertEqual(data[:8], b"\x89PNG\r\n\x1a\n")
        self.assertEqual(struct.unpack(">II", data[16:24]), (3, 2))
        offset, compressed = 8, b""
        while offset < len(data):
            length = struct.unpack(">I", data[offset:offset+4])[0]
            kind, value = data[offset+4:offset+8], data[offset+8:offset+8+length]
            crc = struct.unpack(">I", data[offset+8+length:offset+12+length])[0]
            self.assertEqual(crc, zlib.crc32(kind + value))
            if kind == b"IDAT":
                compressed += value
            offset += length + 12
        pixels = zlib.decompress(compressed)
        self.assertEqual(len(pixels), 26)
        self.assertEqual(pixels[:5], b"\0\xdc((\xff")



    def test_blank_snapshot_is_rejected_even_with_a_live_web_area(self):
        context = dict(evidence=self.root, case_id="case-a", app_pid=5)
        import struct
        image = self.root / "test.png"
        image.write_bytes(b"\x89PNG\r\n\x1a\n" + b"\0" * 8 + struct.pack(">II", 1280, 860))
        with mock.patch.object(resilience, "native", side_effect=[
                dict(case_id="case-a", pid=5, has_web_area=True, window_id=99),
                dict(buckets=3, dominant_fraction=.999)]), mock.patch.object(resilience.subprocess, "run"):
            with self.assertRaisesRegex(RuntimeError, "blank"):
                resilience.snapshot(context, "test")


if __name__ == "__main__":
    unittest.main()
