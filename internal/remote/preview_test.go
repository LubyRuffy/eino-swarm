package remote

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestHumanLineDropsScheduleAndMemoryJargon(t *testing.T) {
	wake := `schedule_wake({"every_s":900,"id":"sch_x","prompt":"Continue the wait."})`
	if got := humanLine(wake); got != "" {
		t.Fatalf("armed wait is chrome, not a subtitle: %q", got)
	}
	quiet := `report_schedule({"findings":"","keep":true})`
	if got := humanLine(quiet); got != "" {
		t.Fatalf("quiet report must stay off the inbox: %q", got)
	}
	got := humanLine(`report_schedule({"findings":"one thing changed","keep":true})`)
	if got != "one thing changed" {
		t.Fatalf("findings=%q", got)
	}
	assertNoToolJargon(t, got)

	if got := humanLine(`memory({"action":"add","content":"a durable fact"})`); got != "" {
		t.Fatalf("memory is bookkeeping: %q", got)
	}
	if got := humanLine("This turn is a scheduled check.\n\nContinue the wait."); got != "" {
		t.Fatalf("protocol wrapper leaked: %q", got)
	}
}

func TestHumanLinePrefersToolPayloadNotTheEnvelope(t *testing.T) {
	got := humanLine(`exec({"command":"echo hi","cwd":"."})`)
	if got != "echo hi" {
		t.Fatalf("exec=%q", got)
	}
	assertNoToolJargon(t, got)
	if got := humanLine("Hello (aside)"); got != "Hello (aside)" {
		t.Fatalf("prose with parens=%q", got)
	}
}

func TestRunningPreviewSkipsBookkeepingToReachProse(t *testing.T) {
	evts := []store.Event{
		{TurnID: "tu", Kind: "agent_message", Text: "working on it"},
		{TurnID: "tu", Kind: "tool_call", Text: `memory({"action":"add","content":"a durable fact"})`},
		{TurnID: "other", Kind: "agent_message", Text: "wrong turn"},
	}
	if got := runningPreview(evts, "tu"); got != "working on it" {
		t.Fatalf("got %q", got)
	}
	evts = append(evts, store.Event{
		TurnID: "tu", Kind: "tool_call",
		Text: `report_schedule({"findings":"one thing changed"})`,
	})
	if got := runningPreview(evts, "tu"); got != "one thing changed" {
		t.Fatalf("later findings=%q", got)
	}
}

func TestSummaryFromTurnsSkipsQuietAndProtocolUserText(t *testing.T) {
	turns := []store.Turn{
		{Final: "kept this", UserText: "ask"},
		{Quiet: true, UserText: "This turn is a scheduled check.\n\nContinue the wait."},
		{UserText: "This turn is a scheduled check.\n\nContinue the wait."},
	}
	if got := summaryFromTurns(turns, 80); got != "kept this" {
		t.Fatalf("summary=%q", got)
	}
	if got := summaryFromTurns(nil, 8); got != "" {
		t.Fatalf("empty=%q", got)
	}
	long := summaryFromTurns([]store.Turn{{Final: strings.Repeat("x", 40)}}, 8)
	if got, want := long, strings.Repeat("x", 8)+"…"; got != want {
		t.Fatalf("clip=%q want %q", got, want)
	}
}

func TestJsonFieldPreviewDoesNotUseAPromptBlob(t *testing.T) {
	got := jsonFieldPreview(`{"id":"sch_x","kind":"thread","prompt":"do the long check","title":"periodic"}`)
	if got != "periodic" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "long check") {
		t.Fatalf("prompt leaked: %q", got)
	}
}

func assertNoToolJargon(t *testing.T, s string) {
	t.Helper()
	for _, w := range []string{
		"schedule_wake", "report_schedule", "cancel_schedule", "schedule_task",
		"memory(", "skill_manage",
	} {
		if strings.Contains(s, w) {
			t.Fatalf("leaked %q in %q", w, s)
		}
	}
}

func TestClipPreviewAndPackedFallbacks(t *testing.T) {
	if got := clipPreview("  a\nb  ", 0); got != "a b" {
		t.Fatalf("default clip=%q", got)
	}
	if got := jsonFieldPreview("{"); got != "" {
		t.Fatalf("junk JSON=%q", got)
	}
	if got := jsonFieldPreview(`{"keep":true,"note":"x"}`); got != "x" {
		t.Fatalf("fallback field=%q", got)
	}
	if got := jsonFieldPreview(`{"n":1}`); got != "" {
		t.Fatalf("non-text=%q", got)
	}
	if got := humanLine("[1]"); got != "" {
		t.Fatalf("array=%q", got)
	}
	if got := humanLine(`read(notes.md)`); got != "notes.md" {
		t.Fatalf("legacy args=%q", got)
	}
	if got := humanLine(`read()`); got != "" {
		t.Fatalf("empty args=%q", got)
	}
	if got := humanLine(`exec({"ok":true})`); got != "" {
		t.Fatalf("packed with no payload=%q", got)
	}
	if got := humanLine(`_x(y)`); got != "y" {
		t.Fatalf("underscore ident=%q", got)
	}
	if got := humanLine(`bad-name(x)`); got != "bad-name(x)" {
		t.Fatalf("hyphenated is prose=%q", got)
	}
	if got := runningPreview([]store.Event{{TurnID: "tu", Kind: "progress", Text: "x"}}, "tu"); got != "" {
		t.Fatalf("progress is not an inbox line: %q", got)
	}
	if isToolIdent("") || isToolIdent("1x") || isToolIdent("Hello") {
		t.Fatal("ident must be snake_case")
	}
	if !isToolIdent("a_b1") || !isToolIdent("_x") {
		t.Fatal("snake_case ident")
	}
}

func TestListActionOmitsAWakeEnvelope(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "run"); err != nil {
		t.Fatal(err)
	}
	st := e.Status(th.ID)
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: st.TurnID, Kind: "tool_call",
		Text: `schedule_wake({"every_s":900,"id":"sch_x","prompt":"Continue the wait."})`,
	}); err != nil {
		t.Fatal(err)
	}
	listed := Handle(e, config.RemoteConfig{}, Request{ID: "w", Op: OpList}, "relay", "s")
	if !listed.OK || len(listed.Running) == 0 {
		t.Fatalf("running %+v", listed.Running)
	}
	if listed.Running[0].Action != "" {
		t.Fatalf("schedule_wake leaked onto action: %q", listed.Running[0].Action)
	}
}
