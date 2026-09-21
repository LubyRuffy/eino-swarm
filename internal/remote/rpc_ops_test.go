package remote

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestStartSteerStopAndAnswerMapOntoTheEngine(t *testing.T) {
	e := testEngine(t)
	p, err := e.CreateProject("p", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	started := Handle(e, config.RemoteConfig{}, Request{
		ID: "s", Op: OpStart, ProjectID: p.ID, Text: "begin",
	}, "relay", "sess")
	if !started.OK || len(started.Threads) != 1 {
		t.Fatalf("start %+v", started)
	}
	tid := started.Threads[0].ID
	if !e.Status(tid).Running {
		t.Fatal("start did not run a turn")
	}

	listed := Handle(e, config.RemoteConfig{}, Request{ID: "l", Op: OpList}, "relay", "sess")
	if !listed.OK || len(listed.Running) == 0 {
		t.Fatalf("running %+v", listed.Running)
	}

	steered := Handle(e, config.RemoteConfig{}, Request{
		ID: "st", Op: OpSteer, ThreadID: tid, Text: "nudge",
	}, "relay", "sess")
	if !steered.OK {
		t.Fatalf("steer %+v", steered)
	}

	follow := Handle(e, config.RemoteConfig{}, Request{
		ID: "fu", Op: OpSend, ThreadID: tid, Text: "later",
	}, "relay", "sess")
	if !follow.OK {
		t.Fatalf("follow-up %+v", follow)
	}

	stopped := Handle(e, config.RemoteConfig{}, Request{
		ID: "x", Op: OpStop, ThreadID: tid,
	}, "relay", "sess")
	if !stopped.OK {
		t.Fatalf("stop %+v", stopped)
	}
}

func TestOpenMissingThreadAndIdleOpsSurfaceCodes(t *testing.T) {
	e := testEngine(t)
	missing := Handle(e, config.RemoteConfig{}, Request{ID: "m", Op: OpOpen, ThreadID: "nope"}, "relay", "s")
	if missing.OK || missing.Code != "not_found" {
		t.Fatalf("open %+v", missing)
	}
	idle := Handle(e, config.RemoteConfig{}, Request{ID: "i", Op: OpSteer, ThreadID: "nope", Text: "x"}, "relay", "s")
	if idle.OK || (idle.Code != "not_found" && idle.Code != "idle") {
		t.Fatalf("steer idle %+v", idle)
	}
	th, err := e.CreateThread("idle", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ans := Handle(e, config.RemoteConfig{}, Request{ID: "a", Op: OpAnswer, ThreadID: th.ID, Text: "nope"}, "relay", "s")
	if ans.OK {
		t.Fatalf("answer on idle should fail %+v", ans)
	}
	bad := Handle(e, config.RemoteConfig{}, Request{
		ID: "b", Op: OpAnswer, ThreadID: th.ID, Answers: json.RawMessage(`{`),
	}, "relay", "s")
	if bad.OK || bad.Code != "bad_request" {
		t.Fatalf("bad answers %+v", bad)
	}
	ver := Handle(e, config.RemoteConfig{}, Request{V: 9, ID: "v", Op: OpList}, "relay", "s")
	if ver.OK || ver.Code != "bad_version" {
		t.Fatalf("version %+v", ver)
	}
}

func TestSummaryFallsBackToUserTextAndRunningAction(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("sum", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().CreateTurn(&store.Turn{
		ThreadID: th.ID, Status: store.TurnDone, UserText: strings.Repeat("u", 40),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal": "keep going", "plan_mode": true,
	}); err != nil {
		t.Fatal(err)
	}
	open := Handle(e, config.RemoteConfig{SummaryChars: 8, OpenTurns: 0}, Request{
		ID: "o", Op: OpOpen, ThreadID: th.ID,
	}, "relay", "s")
	if !open.OK || open.Detail == nil || !open.Detail.GoalOn || !open.Detail.PlanOn {
		t.Fatalf("detail %+v", open.Detail)
	}
	if got := open.Detail.Turns[0].Text; got != strings.Repeat("u", 8)+"…" {
		t.Fatalf("summary %q", got)
	}

	if _, err := e.StartTurn(th.ID, "run"); err != nil {
		t.Fatal(err)
	}
	st := e.Status(th.ID)
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: st.TurnID, Kind: "tool_call",
		Text: `exec({"command":"echo hi","cwd":"."})`,
	}); err != nil {
		t.Fatal(err)
	}
	listed := Handle(e, config.RemoteConfig{}, Request{ID: "r", Op: OpList}, "relay", "s")
	if !listed.OK || len(listed.Running) == 0 || listed.Running[0].Action != "echo hi" {
		t.Fatalf("running %+v", listed.Running)
	}
	if strings.Contains(listed.Running[0].Action, "exec") || strings.Contains(listed.Running[0].Action, "{") {
		t.Fatalf("tool envelope leaked onto the inbox: %q", listed.Running[0].Action)
	}
}

func TestRunNowCancelWaitAndResumeGoalMapOntoTheEngine(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("parked", "", "")
	if err != nil {
		t.Fatal(err)
	}
	idle := Handle(e, config.RemoteConfig{}, Request{ID: "n", Op: OpRunNow, ThreadID: th.ID}, "relay", "s")
	if idle.OK || idle.Code != "idle" {
		t.Fatalf("run now without a wake %+v", idle)
	}
	cancelIdle := Handle(e, config.RemoteConfig{}, Request{ID: "c", Op: OpCancelWait, ThreadID: th.ID}, "relay", "s")
	if cancelIdle.OK || cancelIdle.Code != "idle" {
		t.Fatalf("cancel without a wake %+v", cancelIdle)
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_complete": true,
	}); err != nil {
		t.Fatal(err)
	}
	wake, err := e.CreateSchedule(engine.ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.", DelayS: 3600,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel := Handle(e, config.RemoteConfig{}, Request{ID: "x", Op: OpCancelWait, ThreadID: th.ID}, "relay", "s")
	if !cancel.OK {
		t.Fatalf("cancel %+v", cancel)
	}
	got, err := e.Store().GetSchedule(wake.ID)
	if err != nil || got.Status != store.ScheduleCancelled {
		t.Fatalf("cancelled row %+v err=%v", got, err)
	}
	if _, err := e.CreateSchedule(engine.ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.", DelayS: 3600,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	run := Handle(e, config.RemoteConfig{}, Request{ID: "go", Op: OpRunNow, ThreadID: th.ID}, "relay", "s")
	if !run.OK {
		t.Fatalf("run now %+v", run)
	}
	if !e.Status(th.ID).Running {
		t.Fatal("run now must start the parked turn")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.Status(th.ID).Running && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.Status(th.ID).Running {
		t.Fatal("interrupt did not settle")
	}
	resume := Handle(e, config.RemoteConfig{}, Request{ID: "re", Op: OpResumeGoal, ThreadID: th.ID}, "relay", "s")
	if !resume.OK {
		t.Fatalf("resume %+v", resume)
	}
	if !e.Status(th.ID).Running {
		t.Fatal("resume goal must start a turn")
	}
	loaded, err := e.Store().GetThread(th.ID)
	if err != nil || loaded.GoalComplete {
		t.Fatalf("goal still complete %+v err=%v", loaded, err)
	}
	missing := Handle(e, config.RemoteConfig{}, Request{ID: "ng", Op: OpResumeGoal, ThreadID: "nope"}, "relay", "s")
	if missing.OK {
		t.Fatalf("resume missing %+v", missing)
	}
	_ = e.Store().Close()
	closed := Handle(e, config.RemoteConfig{}, Request{ID: "cl", Op: OpRunNow, ThreadID: th.ID}, "relay", "s")
	if closed.OK {
		t.Fatalf("run now on closed store %+v", closed)
	}
}
