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

// A parked /goal wait used to look idle on the phone: list omitted waiting,
// open hid goal flags, and the roster only listed live turns. Then the
// human thought the swarm died.
func TestPhoneListAndOpenSurfaceGoalAndParkedWait(t *testing.T) {
	e := testEngine(t)
	old, err := e.CreateThread("old", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(old.ID, map[string]any{
		"last_active_at": time.Now().UTC().Add(-time.Hour),
		"title":          "old",
	}); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("parked", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
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
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"last_active_at": time.Now().UTC().Add(-30 * time.Minute),
		"title":          "parked",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		row, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Store().UpdateThread(row.ID, map[string]any{
			"title":          "fresh",
			"last_active_at": time.Now().UTC().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	listed := Handle(e, config.RemoteConfig{ThreadLimit: 5, SummaryChars: 40}, Request{ID: "l", Op: OpList}, "relay", "s")
	if !listed.OK {
		t.Fatalf("list %+v", listed)
	}
	if len(listed.Threads) != 5 {
		t.Fatalf("page %d", len(listed.Threads))
	}
	for _, row := range listed.Threads {
		if row.ID == th.ID {
			t.Fatalf("parked wait must not depend on the recent page: %+v", listed.Threads)
		}
	}
	if len(listed.Running) != 1 || listed.Running[0].ThreadID != th.ID || !listed.Running[0].Waiting {
		t.Fatalf("roster must carry the parked wait %+v", listed.Running)
	}
	if listed.Running[0].Action != "" {
		t.Fatalf("waiting action must stay off the wire for i18n, got %q", listed.Running[0].Action)
	}
	wide := Handle(e, config.RemoteConfig{ThreadLimit: 20, SummaryChars: 40}, Request{ID: "w", Op: OpList}, "relay", "s")
	found := false
	for _, row := range wide.Threads {
		if row.ID != th.ID {
			continue
		}
		found = true
		if !row.Waiting {
			t.Fatal("listing row must carry waiting")
		}
	}
	if !found {
		t.Fatal("parked thread missing from the wide list")
	}

	open := Handle(e, config.RemoteConfig{SummaryChars: 40}, Request{ID: "o", Op: OpOpen, ThreadID: th.ID}, "relay", "s")
	if !open.OK || open.Detail == nil {
		t.Fatalf("open %+v", open)
	}
	d := open.Detail
	if d.Goal != "keep going" || !d.GoalOn || d.GoalComplete {
		t.Fatalf("goal flags %+v", d)
	}
	if d.GoalStartedAt == "" {
		t.Fatal("goal clock missing")
	}
	if !d.Waiting || d.Wake == nil || d.Wake.ID != wake.ID {
		t.Fatalf("wake %+v waiting=%v", d.Wake, d.Waiting)
	}
	if d.Wake.Title != "wake" || d.Wake.Prompt != "Continue the wait." || d.Wake.NextRunAt == "" {
		t.Fatalf("wake payload %+v", d.Wake)
	}
	if d.Running != nil {
		t.Fatal("a parked wait is not a live turn")
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_complete": true}); err != nil {
		t.Fatal(err)
	}
	done := Handle(e, config.RemoteConfig{SummaryChars: 40}, Request{ID: "d", Op: OpOpen, ThreadID: th.ID}, "relay", "s")
	if !done.OK || done.Detail == nil || !done.Detail.GoalOn || !done.Detail.GoalComplete {
		t.Fatalf("completed goal must still paint %+v", done.Detail)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_blocked":      true,
		"goal_block_reason": "needs an external change",
		"goal_capped":       true,
		"goal_idle":         true,
	}); err != nil {
		t.Fatal(err)
	}
	held := Handle(e, config.RemoteConfig{SummaryChars: 40}, Request{ID: "h", Op: OpOpen, ThreadID: th.ID}, "relay", "s")
	if !held.OK || held.Detail == nil {
		t.Fatalf("held %+v", held)
	}
	if !held.Detail.GoalBlocked || held.Detail.GoalBlockReason != "needs an external change" {
		t.Fatalf("block %+v", held.Detail)
	}
	if !held.Detail.GoalCapped || !held.Detail.GoalIdle {
		t.Fatalf("pause flags %+v", held.Detail)
	}
}

func TestPhoneRosterMarksALiveTurnThatAlsoHasAWait(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("both", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(engine.ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.", DelayS: 3600,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	listed := Handle(e, config.RemoteConfig{}, Request{ID: "r", Op: OpList}, "relay", "s")
	if !listed.OK || len(listed.Running) != 1 {
		t.Fatalf("running %+v", listed.Running)
	}
	if !listed.Running[0].Waiting || listed.Running[0].TurnID == "" {
		t.Fatalf("live wait %+v", listed.Running[0])
	}
	gone, err := e.CreateThread("gone", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(engine.ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: gone.ID,
		Title: "wake", Prompt: "Continue the wait.", DelayS: 3600,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().DeleteThread(gone.ID); err != nil {
		t.Fatal(err)
	}
	again := Handle(e, config.RemoteConfig{}, Request{ID: "g", Op: OpList}, "relay", "s")
	if !again.OK {
		t.Fatalf("list after delete %+v", again)
	}
	for _, row := range again.Running {
		if row.ThreadID == gone.ID {
			t.Fatalf("deleted thread leaked onto the roster %+v", again.Running)
		}
	}
}
