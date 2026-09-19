package remote

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func testEngine(t *testing.T) *engine.Engine {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := engine.New(cfg, st, provider.NewMock(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e
}

func TestListDefaultsToFiveAndOmitsProjectSecrets(t *testing.T) {
	e := testEngine(t)
	secret := "prompt-material-must-not-leave-the-pc"
	p, err := e.CreateProject("alpha", secret, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		th, err := e.CreateThread("", "", p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Store().UpdateThread(th.ID, map[string]any{
			"last_active_at": time.Now().UTC().Add(time.Duration(i) * time.Second),
			"title":          "t",
		}); err != nil {
			t.Fatal(err)
		}
	}
	resp := Handle(e, config.RemoteConfig{ThreadLimit: 5, SummaryChars: 40, OpenTurns: 3}, Request{ID: "1", Op: OpList}, "relay", "sess")
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	if len(resp.Threads) != 5 {
		t.Fatalf("got %d threads", len(resp.Threads))
	}
	if !resp.More || resp.Next == "" {
		t.Fatal("expected another page")
	}
	if len(resp.Projects) != 1 || resp.Projects[0].Name != "alpha" {
		t.Fatalf("projects %+v", resp.Projects)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("system prompt leaked onto the phone")
	}
	more := Handle(e, config.RemoteConfig{ThreadLimit: 5}, Request{ID: "2", Op: OpMore, Cursor: resp.Next}, "relay", "sess")
	if !more.OK || len(more.Threads) != 2 {
		t.Fatalf("more %+v", more)
	}
}

func TestOpenTruncatesAssistantText(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("open-me", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone, Final: strings.Repeat("x", 80), UserText: "q"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{SummaryChars: 12, OpenTurns: 4}, Request{ID: "3", Op: OpOpen, ThreadID: th.ID}, "direct", "abc")
	if !resp.OK || resp.Detail == nil {
		t.Fatalf("%+v", resp)
	}
	if resp.Path != "direct" || resp.SessionID != "abc" {
		t.Fatalf("metadata %+v", resp)
	}
	if len(resp.Detail.Turns) != 1 {
		t.Fatalf("turns %+v", resp.Detail.Turns)
	}
	if got := resp.Detail.Turns[0].Text; got != strings.Repeat("x", 12)+"…" {
		t.Fatalf("text %q", got)
	}
}

func TestSendOnIdleStartsATurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "4", Op: OpSend, ThreadID: th.ID, Text: "go"}, "relay", "s")
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	st := e.Status(th.ID)
	if !st.Running {
		t.Fatal("expected a live turn")
	}
}

func TestUnknownOpFails(t *testing.T) {
	e := testEngine(t)
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "5", Op: "wipe"}, "relay", "s")
	if resp.OK || resp.Code != "unknown_op" {
		t.Fatalf("%+v", resp)
	}
}

func TestSlimListPayloadStaysBounded(t *testing.T) {
	e := testEngine(t)
	for i := 0; i < 8; i++ {
		th, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Store().UpdateThread(th.ID, map[string]any{
			"title":          "t",
			"last_active_at": time.Now().UTC().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	resp := Handle(e, config.RemoteConfig{ThreadLimit: 5, SummaryChars: 40, OpenTurns: 3}, Request{ID: "sz", Op: OpList}, "relay", "s")
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 16<<10 {
		t.Fatalf("list payload %d bytes", len(raw))
	}
	if strings.Contains(string(raw), "system_prompt") || strings.Contains(string(raw), "token_delta") {
		t.Fatal("fat fields leaked onto the phone")
	}
}
