package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
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
		ThreadID: th.ID, TurnID: st.TurnID, Kind: "tool_call", Text: "read",
	}); err != nil {
		t.Fatal(err)
	}
	listed := Handle(e, config.RemoteConfig{}, Request{ID: "r", Op: OpList}, "relay", "s")
	if !listed.OK || len(listed.Running) == 0 || listed.Running[0].Action != "read" {
		t.Fatalf("running %+v", listed.Running)
	}
}
