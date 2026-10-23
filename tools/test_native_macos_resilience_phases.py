"""Fault-stage evidence must bind the actual private shell and recovery event."""
from contextlib import contextmanager
import copy
from datetime import datetime, timezone
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("phases", Path(__file__).with_name("native_macos_resilience_phases.py"))
phases = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(phases)


class PhaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "diagnostics").mkdir()
        self.context = dict(case_id="c" * 32, data=self.root, app=self.root / "zwai.app", app_pid=17,
                            activated_pid=17, engine_pid=9, origin="http://127.0.0.1:12345", expected_mock=False,
                            recovery_session_id="a" * 16)
        self.event = dict(phase="ready", session_id="a" * 16, shell_pid=18, engine_pid=9,
                          generation=1, attempt=1, at="2026-10-22T00:00:10Z")
        self.journal = self.root / "diagnostics/desktop-case.jsonl"
        self.journal.write_text(json.dumps(self.event) + "\n")
        (self.root / "engine.json").write_text(json.dumps(dict(pid=9, url=self.context["origin"])))
        self.harness = SimpleNamespace(api=mock.Mock(return_value=dict(data_dir=str(self.root), mock=False,
            clients=[dict(surface="desktop", pid=17), dict(surface="desktop", pid=18)])),
            read_json=lambda path:json.loads(path.read_text()), timestamp=lambda value:int(value[17:19]),
            runner=SimpleNamespace(process_identity=lambda pid:([str(self.root / "zwai.app/Contents/MacOS/zwai")], "c" * 32)),
            native=mock.Mock(return_value=dict(active=True, frontmost_pid=18)))

    def test_replacement_activates_only_the_new_private_shell_then_requires_its_fresh_ready_event(self):
        row = phases.replacement_ready(self.context, self.harness, original_pid=17, since=5)
        self.assertEqual(row, self.event)
        self.assertEqual(self.context["app_pid"], 18)
        self.assertEqual(self.harness.native.call_args.args[1], "activate")
        self.journal.write_text("")
        self.assertIsNone(phases.replacement_ready(self.context, self.harness, original_pid=17, since=5))

    def test_stale_ready_or_changed_engine_cannot_accept_a_replacement(self):
        self.assertIsNone(phases.replacement_ready(self.context, self.harness, original_pid=17, since=11))
        for field, value in [("generation", 0), ("attempt", 0), ("session_id", "unknown"), ("session_id", "b" * 16)]:
            self.journal.write_text(json.dumps(dict(self.event, **{field:value})) + "\n")
            with self.subTest(field=field):
                self.assertIsNone(phases.replacement_ready(self.context, self.harness, original_pid=17, since=5))
        (self.root / "engine.json").write_text(json.dumps(dict(pid=10, url=self.context["origin"])))
        with self.assertRaisesRegex(RuntimeError, "engine"):
            phases.replacement_ready(self.context, self.harness, original_pid=17, since=5)

    def test_offline_replacement_requires_current_events_commit_and_retired_original(self):
        old = dict(pid=17,birth=[1,2],executable="/private/zwai.app/Contents/MacOS/zwai")
        initial = dict(self.event,shell_pid=17,generation=0,attempt=0,at="2026-10-22T00:00:01Z")
        replacing = dict(initial,phase="replacing",attempt=1,at="2026-10-22T00:00:08Z")
        recovered = dict(replacing,phase="recovered",at="2026-10-22T00:00:11Z")
        verified = dict(since=5,session_id="a"*16,ready=self.event,replacing=replacing,recovered=recovered,
            original_exit=dict(identity=old,outcome="exited",observed_at=12),journal=[initial,replacing,self.event,recovered])
        self.assertTrue(phases.validate_replacement(verified,initial,old,9,self.harness))
        changes = [("generation",0),("engine_pid",999),("attempt",0),("at","2026-10-22T00:00:01Z"),
                   ("session_id","b"*16),("shell_pid",17)]
        for field,value in changes:
            bad=json.loads(json.dumps(verified));bad["ready"][field]=value
            # Even changing the retained row cannot override the protocol.
            bad["journal"][2]=bad["ready"]
            with self.subTest(field=field),self.assertRaisesRegex(RuntimeError,"replacement"):
                phases.validate_replacement(bad,initial,old,9,self.harness)
        for mutate in (lambda v:v.update(journal=v["journal"][:-1]),
                       lambda v:v["recovered"].update(at="2026-10-22T00:00:09Z"),
                       lambda v:v["recovered"].update(engine_pid=999),
                       lambda v:v["recovered"].update(attempt=2),
                       lambda v:v["original_exit"].update(outcome="running"),
                       lambda v:v["original_exit"].update(observed_at=39),
                       lambda v:v["original_exit"].update(identity=dict(old,birth=[2,2]))):
            bad=json.loads(json.dumps(verified));mutate(bad)
            with self.assertRaisesRegex(RuntimeError,"replacement"):
                phases.validate_replacement(bad,initial,old,9,self.harness)

    def test_exhaustion_requires_two_actual_permission_failures_and_no_third_attempt(self):
        path = "/private/case/unused/zwai"
        rows = []
        for attempt in (1, 2):
            for phase in ("replacing", "replacement_failed"):
                rows.append(dict(self.event, shell_pid=17, generation=0, attempt=attempt, phase=phase,
                    reason="start recovery shell: fork/exec " + path + ": permission denied"))
        rows.append(dict(self.event, shell_pid=17, generation=0, attempt=2, phase="exhausted"))
        result = phases.exhaustion_events(rows, self.harness, since=5, shell_pid=17, session_id="a"*16, path=path, engine_pid=9, generation=0)
        self.assertEqual(result["failed_attempts"], [1, 2])
        for changed in (rows[:-1], rows + [dict(rows[0], attempt=3)],
                        [dict(row,engine_pid=999) for row in rows],
                        [dict(row,generation=99) for row in rows],
                        [rows[0],rows[2],rows[1],rows[3],rows[4]],
                        [dict(row, reason="provider failed") if row["phase"]=="replacement_failed" else row for row in rows]):
            with self.assertRaisesRegex(RuntimeError, "exhaust|permission|attempt"):
                phases.exhaustion_events(changed, self.harness, since=5, shell_pid=17, session_id="a"*16, path=path, engine_pid=9, generation=0)


class FaultEvidenceTests(unittest.TestCase):
    def setUp(self):
        from test_native_macos_resilience_fault import SelectorTests, fault
        fixture=SelectorTests();fixture.setUp()
        self.fault,self.fixture=fault,fixture
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        (self.root/"diagnostics").mkdir()
        self.stamp=lambda value:datetime.fromtimestamp(value,timezone.utc).isoformat().replace("+00:00","Z")
        self.harness=SimpleNamespace(read_json=lambda p:json.loads(p.read_text()),
            atomic_json=lambda p,v:p.write_text(json.dumps(v)),timestamp=lambda v:datetime.fromisoformat(v.replace("Z","+00:00")).timestamp())
        self.target=fault.select_target(fixture.boundaries,fixture.current,"renderer",case_id=fixture.case,private_app=fixture.app,now=fixture.now)
        self.old=self.target["shell"]
        self.initial=dict(phase="ready",session_id="a"*16,shell_pid=self.old["pid"],engine_pid=9,generation=0,attempt=0,at=self.stamp(fixture.now-1))
        self.replacing=dict(self.initial,phase="replacing",attempt=1,at=self.stamp(fixture.now+1))
        self.ready=dict(self.initial,shell_pid=300,generation=1,attempt=1,at=self.stamp(fixture.now+2))
        self.recovered=dict(self.replacing,phase="recovered",at=self.stamp(fixture.now+3))
        self.rows=[self.initial,self.replacing,self.ready,self.recovered]
        self.state=dict(stopped=True,before_stat="S",stopped_stat="T",expired=False,
                       resume=dict(identity=self.target["target"],outcome="resumed"))
        self.verified=dict(case_id=fixture.case,kind="renderer",since=fixture.now,session_id="a"*16,passed=True,
            target=self.target,current=fixture.current,state=self.state,ready=self.ready,replacing=self.replacing,
            recovered=self.recovered,original_exit=dict(identity=self.old,outcome="exited",observed_at=fixture.now+4),journal=self.rows)
        self.proof=dict(boundaries=fixture.boundaries,ready=self.initial)
        self.result=dict(case_id=fixture.case,fault_verified=True,engine_pid_before=9)
        self.cleanup=dict(app_copy=str(fixture.app),renderer_fault_cleanup=[
            dict(case_id=fixture.case,private_app=str(fixture.app),complete=True,require_exit=final,
                 targets=[dict(identity=self.target["target"],outcome="exited" if final else "resumed")]) for final in (False,True)])
        self.save()

    def save(self):
        for name,row in (("fault-verification.json",self.verified),("fault-proof.json",self.proof)):
            (self.root/name).write_text(json.dumps(row))
        (self.root/self.fault.TARGETS).write_text(json.dumps(self.target)+"\n")
        (self.root/"diagnostics/desktop-test.jsonl").write_text("".join(json.dumps(row)+"\n" for row in self.rows))

    def accept(self):
        return phases.accept_evidence(self.root,self.result,self.cleanup,"renderer",self.harness)

    def test_offline_fault_gate_binds_real_stop_and_both_runner_cleanup_receipts(self):
        self.assertTrue(self.accept())
        originals=copy.deepcopy((self.verified,self.cleanup))
        for mutate in (lambda:self.verified["state"].update(expired=True),
                       lambda:self.verified["state"].update(stopped_stat="S"),
                       lambda:self.verified["state"]["resume"].update(outcome="unknown"),
                       lambda:self.cleanup["renderer_fault_cleanup"].pop(),
                       lambda:self.cleanup["renderer_fault_cleanup"][1]["targets"][0].update(outcome="resumed"),
                       lambda:self.cleanup["renderer_fault_cleanup"][0].update(complete=False)):
            self.verified,self.cleanup=copy.deepcopy(originals);mutate();self.save()
            with self.assertRaisesRegex(RuntimeError,"fault"):
                self.accept()

    def test_verified_events_cannot_replace_missing_or_changed_retained_diagnostics(self):
        for rows in ([],self.rows[:-1],[dict(row,engine_pid=999) for row in self.rows]):
            self.save();(self.root/"diagnostics/desktop-test.jsonl").write_text("".join(json.dumps(row)+"\n" for row in rows))
            with self.assertRaisesRegex(RuntimeError,"journal|diagnostic"):
                self.accept()

    def test_exhaustion_gate_requires_same_session_copy_native_error_and_continuous_no_respawn(self):
        fixture=self.fixture
        copy_row=dict(schema=1,case_id=fixture.case,private_app=str(fixture.app),
            path=str(fixture.app.parent/"home/Library/Caches/zwai/desktop-recovery-test/zwai.app/Contents/MacOS/zwai"),
            session_marker="a"*16,dev=1,ino=2,mode=0o755,sha256="f"*64)
        self.rows=[self.initial]
        for n,phase,attempt in ((1,"replacing",1),(2,"replacement_failed",1),(3,"replacing",2),(4,"replacement_failed",2),(5,"exhausted",2)):
            self.rows.append(dict(self.initial,phase=phase,attempt=attempt,at=self.stamp(fixture.now+n),
                reason="start recovery shell: fork/exec "+copy_row["path"]+": permission denied"))
        self.verified.update(kind="exhaustion",private_copy=copy_row,journal=self.rows,
            dialog=dict(error_dialog=True,pid=200,case_id=fixture.case,text="已停止自动重启",window_id=88))
        observations=[dict(monotonic=n,shell_pid=200,engine_pid=9) for n in (10,15,20,25)]
        (self.root/"fault-no-respawn.json").write_text(json.dumps(dict(observations=observations)))
        (self.root/self.fault.COPIES).write_text(json.dumps(copy_row)+"\n")
        for receipt in self.cleanup["renderer_fault_cleanup"]:
            receipt["copies"]=[dict(path=copy_row["path"],outcome="restored")]
        self.save()
        accept=lambda:phases.accept_evidence(self.root,self.result,self.cleanup,"exhaustion",self.harness)
        self.assertTrue(accept())
        original=copy.deepcopy(self.verified)
        for change in (dict(error_dialog=False),dict(pid=999),dict(text="unrelated error")):
            self.verified=copy.deepcopy(original);self.verified["dialog"].update(change);self.save()
            with self.assertRaisesRegex(RuntimeError,"exhaustion"):
                accept()
        self.verified=original;self.save()
        for changed in (observations[:-1],[dict(row,engine_pid=10) for row in observations],
                        [observations[0],dict(observations[1],monotonic=21),observations[-1]]):
            (self.root/"fault-no-respawn.json").write_text(json.dumps(dict(observations=changed)))
            with self.assertRaisesRegex(RuntimeError,"no-respawn"):
                accept()

    def test_runtime_exhaustion_observes_fifteen_seconds_after_resume_before_manual_reopen(self):
        import native_macos_resilience_fault as fault
        fixture=self.fixture;clock=[0.0];events=[]
        context=dict(case_id=fixture.case,app=fixture.app,app_pid=200,engine_pid=9,data=self.root,
            evidence=self.root,origin="http://127.0.0.1:12345",helper="/helper",expected_mock=False)
        copy_row=dict(path=str(fixture.app.parent/"copy/zwai"))
        error_rows=[self.initial]
        for n,phase,attempt in ((1,"replacing",1),(2,"replacement_failed",1),(3,"replacing",2),(4,"replacement_failed",2),(5,"exhausted",2)):
            error_rows.append(dict(self.initial,phase=phase,attempt=attempt,at=self.stamp(fixture.now+n),
                reason="start recovery shell: fork/exec "+copy_row["path"]+": permission denied"))
        self.rows=[self.initial];self.save()
        for name,row in fixture.boundaries.items():
            (self.root/("public-process-ownership-"+name+".json")).write_text(json.dumps(row))
        def resources(ctx,h,label):
            (self.root/("public-process-ownership-"+label+".json")).write_text(json.dumps(fixture.current))
        @contextmanager
        def denied(**kwargs):
            self.assertEqual(kwargs["session_id"],"a"*16)
            try:yield copy_row
            finally:events.append("restore")
        @contextmanager
        def stalled(*args,**kwargs):
            kwargs["snapshot"]();kwargs["snapshot"]()
            self.rows=error_rows;self.save()
            try:yield dict(target=self.target,state=self.state)
            finally:events.append("resume")
        def native(ctx,action,value=""):
            events.append(action)
            if action=="image":return dict(buckets=32,dominant_fraction=.5)
            return dict(error_dialog=True,case_id=fixture.case,pid=200,text="已停止自动重启",window_id=88)
        def monotonic():
            clock[0]+=5
            return clock[0]
        h=self.harness;h.background=SimpleNamespace(reopen=lambda *a:events.append("reopen"),resources=resources,
                                                  check=lambda *a:events.append("check"))
        h.process_identity=lambda *args:self.old;h.native=native;h.case_shells=lambda ctx:{200}
        h.wait_for=lambda check,deadline:check();h.Deadline=lambda s:s
        result=dict(snapshots=[])
        with mock.patch.object(fault,"stall",stalled),mock.patch.object(fault,"deny_successor_launch",denied), \
                mock.patch.object(phases.time,"time",return_value=fixture.now), \
                mock.patch.object(phases.time,"monotonic",side_effect=monotonic), \
                mock.patch.object(phases.time,"sleep"),mock.patch.object(phases.subprocess,"run"):
            phases.run(context,h,"exhaustion",180,result)
        rows=self.harness.read_json(self.root/"fault-no-respawn.json")["observations"]
        self.assertGreaterEqual(rows[-1]["monotonic"]-rows[0]["monotonic"],15)
        self.assertLess(events.index("resume"),events.index("restore"))
        self.assertLess(events.index("restore"),events.index("dismiss-error"))
        self.assertLess(events.index("dismiss-error"),events.index("check"))
        self.assertEqual(events[-1],"reopen");self.assertTrue(result["fault_verified"])

    def test_runtime_waits_past_pixel_ready_until_commit_and_original_exit(self):
        fixture=self.fixture;clock=[fixture.now];polls=[]
        context=dict(case_id=fixture.case,app=fixture.app,app_pid=200,engine_pid=9,data=self.root,
            evidence=self.root,origin="http://127.0.0.1:12345",helper="/helper",expected_mock=False)
        (self.root/"engine.json").write_text(json.dumps(dict(pid=9,url=context["origin"])))
        for name,row in fixture.boundaries.items():
            (self.root/("public-process-ownership-"+name+".json")).write_text(json.dumps(row))
        def resources(ctx,h,label):
            (self.root/("public-process-ownership-"+label+".json")).write_text(json.dumps(fixture.current))
        def wait(predicate,deadline):
            clock[0]=fixture.now+4
            for number in range(2):
                value=predicate();polls.append(value)
                if value:return value
            raise RuntimeError("did not commit")
        @contextmanager
        def stalled(*args,**kwargs):
            kwargs["snapshot"]();kwargs["snapshot"]()
            yield dict(target=self.target,state=self.state)
        h=self.harness
        h.background=SimpleNamespace(reopen=mock.Mock(),resources=resources)
        h.process_identity=lambda helper,pid:self.old
        h.runner=SimpleNamespace(process_identity=lambda pid:([str(fixture.app/"Contents/MacOS/zwai")],fixture.case))
        h.api=lambda origin,path:dict(data_dir=str(self.root),mock=False,clients=[dict(surface="desktop",pid=300)])
        h.native=mock.Mock(return_value=dict(active=True,frontmost_pid=300));h.wait_for=wait;h.Deadline=lambda seconds:seconds
        result=dict(snapshots=[])
        import native_macos_resilience_fault as real_fault
        with mock.patch.object(real_fault,"stall",stalled), \
                mock.patch.object(real_fault,"process_identity",side_effect=[self.old,None]), \
                mock.patch.object(phases.time,"time",side_effect=lambda:clock[0]):
            phases.run(context,h,"renderer",180,result)
        self.assertEqual(len(polls),2);self.assertIsNone(polls[0]);self.assertTrue(result["fault_verified"])
        self.assertEqual(self.harness.read_json(self.root/"fault-verification.json")["original_exit"]["outcome"],"exited")
        self.assertTrue((self.root/"public-process-ownership-fault-live-0.json").exists())
        self.assertTrue((self.root/"public-process-ownership-fault-live-1.json").exists())
        self.state["expired"]=True
        context.update(app_pid=200,activated_pid=200)
        with mock.patch.object(real_fault,"stall",stalled),mock.patch.object(real_fault,"process_identity",return_value=None), \
                mock.patch.object(phases.time,"time",side_effect=lambda:clock[0]),self.assertRaisesRegex(RuntimeError,"deadline"):
            clock[0]=fixture.now
            phases.run(context,h,"renderer",180,{"snapshots":[]})


if __name__ == "__main__":
    unittest.main()
