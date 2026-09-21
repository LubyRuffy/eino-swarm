package remote

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestMapErrAndFmtErr(t *testing.T) {
	if mapErr("1", "relay", "s", engine.ErrBusy).Code != "busy" {
		t.Fatal("busy")
	}
	if mapErr("1", "relay", "s", engine.ErrIdle).Code != "idle" {
		t.Fatal("idle")
	}
	if mapErr("1", "relay", "s", engine.ErrSkippedBusy).Code != "skipped_busy" {
		t.Fatal("skipped_busy")
	}
	if mapErr("1", "relay", "s", store.ErrNotFound).Code != "not_found" {
		t.Fatal("not_found")
	}
	other := mapErr("1", "relay", "s", errors.New("boom"))
	if other.OK || other.Error != "boom" {
		t.Fatalf("%+v", other)
	}
	if fmtErr(nil) != "" || fmtErr(errors.New("x")) != "x" {
		t.Fatal("fmtErr")
	}
}

func TestAnswerWithObjectAndStartAfterClose(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, e.Config().Remote, Request{
		ID: "a", Op: OpAnswer, ThreadID: th.ID, Answers: json.RawMessage(`{"q":["x"]}`),
	}, "relay", "s")
	if resp.OK {
		t.Fatalf("idle answer %+v", resp)
	}
	_ = e.Store().Close()
	closed := Handle(e, e.Config().Remote, Request{ID: "l", Op: OpList}, "relay", "s")
	if closed.OK {
		t.Fatalf("list on closed store %+v", closed)
	}
	start := Handle(e, e.Config().Remote, Request{ID: "n", Op: OpStart, Text: "x"}, "relay", "s")
	if start.OK {
		t.Fatalf("start on closed store %+v", start)
	}
}

func TestPageThreadsBogusCursor(t *testing.T) {
	page, next, more := pageThreads(nil, "!!!!", 0)
	if len(page) != 0 || next != "" || more {
		t.Fatalf("%v %q %v", page, next, more)
	}
}

func TestExcludeLiveThreadsSkipsRosterIDs(t *testing.T) {
	all := []store.Thread{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := excludeLiveThreads(all, nil)
	if len(got) != 3 || &got[0] != &all[0] {
		t.Fatal("empty live must keep the same slice")
	}
	got = excludeLiveThreads(all, map[string]struct{}{"b": {}})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("%+v", got)
	}
	got = excludeLiveThreads(all, map[string]struct{}{"a": {}, "b": {}, "c": {}})
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	ids := runningIDs([]RunningView{{ThreadID: "a"}, {ThreadID: ""}, {ThreadID: "b"}})
	if len(ids) != 2 {
		t.Fatalf("%v", ids)
	}
	if _, ok := ids["a"]; !ok {
		t.Fatal("missing a")
	}
	if _, ok := ids[""]; ok {
		t.Fatal("empty id leaked")
	}
}

func TestHostTokenFileErrorSurfaces(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	path := cfg.RemoteDir()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := path + "/host_token"
	if err := os.Mkdir(tokenPath, 0o700); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	if _, err := h.ListBindings(t.Context()); err == nil {
		t.Fatal("list should fail")
	}
	if err := h.RevokeBinding(t.Context(), "x"); err == nil {
		t.Fatal("revoke should fail")
	}
}

func TestHostRegisterFailureStaysOffline(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = "http://127.0.0.1:1"
	if err := cfg.WriteHostToken("not-a-real-token"); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	h.Start()
	if h.Status().Online || h.Status().Error == "" {
		t.Fatalf("%+v", h.Status())
	}
	h.Start()
}

func TestDisabledHostDoesNotDial(t *testing.T) {
	e := testEngine(t)
	h := New(e, e.Config(), nil)
	h.Start()
	h.Start()
	if h.Status().Online {
		t.Fatal("disabled host came online")
	}
}

func TestIdentityPathIsDirectory(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.RemoteDir()+"/identity", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIdentity(cfg); err == nil {
		t.Fatal("identity path is a directory")
	}
}

func TestOpenWhileRunningAndEmptySummary(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().CreateTurn(&store.Turn{ThreadID: th.ID, Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "go"); err != nil {
		t.Fatal(err)
	}
	open := Handle(e, config.RemoteConfig{OpenTurns: 2, SummaryChars: 20}, Request{
		ID: "o", Op: OpOpen, ThreadID: th.ID,
	}, "relay", "s")
	if !open.OK || open.Detail == nil || open.Detail.Running == nil {
		t.Fatalf("running detail %+v", open.Detail)
	}
}
