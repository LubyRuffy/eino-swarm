package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An empty catalog must not spend a reviewer call. There is nothing to curate.
func TestFoldProjectSkillsSkipsTheModelWhenTheCatalogIsEmpty(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)

	rep, err := e.FoldProjectSkills(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reviewed || rep.Folded() || rep.Scanned != 0 {
		t.Fatalf("empty catalog=%+v", rep)
	}
}

// The Memory panel's tidy is a model call, attributed to the latest finished
// turn so `zwai trace <id>` still reaches it.
func TestFoldProjectSkillsCallsTheReviewerAndHangsItOnTheLatestTurn(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "alpha-prep", "when preparing", "1. prepare")

	turn, err := e.StartTurn(th.ID, "a request for the swarm")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	waitForReview(t, e, turn.ID)
	before := reviewerCallCount(t, e, turn.ID)
	if before == 0 {
		t.Fatal("the automatic reviewer must have run first")
	}

	rep, err := e.FoldProjectSkills(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Reviewed {
		t.Fatal("a catalog with skills must call the reviewer")
	}
	after := reviewerCallCount(t, e, turn.ID)
	if after <= before {
		t.Fatalf("tidy reviewer calls=%d before=%d", after, before)
	}

	reviews := 0
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			reviews++
		}
	}
	if reviews < 2 {
		t.Fatalf("catalog tidy must record a memory_review on the turn, got %d", reviews)
	}
}

// A click used to sit on a full bar with nothing moving. The watcher has to
// see the scan and the reviewer's line before the report comes back.
func TestFoldProjectSkillsWatchShowsTheReviewersLine(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "alpha-prep", "when preparing", "1. prepare")

	var scanned int
	var lines []string
	rep, err := e.FoldProjectSkillsWatch(p.ID, func(ev TidyEvent) {
		switch ev.Phase {
		case "scan":
			scanned = ev.Scanned
		case "text":
			lines = append(lines, ev.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Reviewed || scanned != 1 {
		t.Fatalf("scanned=%d report=%+v", scanned, rep)
	}
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "curated") {
		t.Fatalf("lines=%q", lines)
	}
}

func TestFoldProjectSkillsDoesNotWriteNotes(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "alpha-prep", "when preparing", "1. prepare")

	if _, err := e.FoldProjectSkills(p.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := e.ProjectMemory(p.ID).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 0 {
		t.Fatalf("a catalog tidy must not invent notes: %+v", snap.Entries)
	}
}

func reviewerCallCount(t *testing.T, e *Engine, turnID string) int {
	t.Helper()
	calls, err := e.Store().ListLLMCalls(turnID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range calls {
		if c.AgentID == ReviewAgentID {
			n++
		}
	}
	return n
}

func plantDoneTurn(t *testing.T, e *Engine, threadID string, ended time.Time) *store.Turn {
	t.Helper()
	turn := &store.Turn{ThreadID: threadID, UserText: "a request"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(turn.ID, store.TurnDone, "done", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateTurn(turn.ID, map[string]any{"ended_at": ended}); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Sidebar order is not recency. A just-opened archived conversation and a
// pinned older one both sit above the thread that actually finished last;
// tidy still has to hang on that latest turn or `zwai trace` lies.
func TestTidyReviewAnchorPicksTheLatestFinishedTurnNotSidebarOrder(t *testing.T) {
	e := newTestEngine(t)
	p, opened := projectThread(t, e)
	pinned, err := e.CreateThread("pinned", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := e.CreateThread("latest", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	plantDoneTurn(t, e, opened.ID, now.Add(-2*time.Hour))
	plantDoneTurn(t, e, pinned.ID, now.Add(-time.Hour))
	want := plantDoneTurn(t, e, latest.ID, now)

	if err := e.Store().UpdateThread(opened.ID, map[string]any{
		"archived":       true,
		"sort_rank":      0,
		"last_active_at": now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(pinned.ID, map[string]any{
		"sort_rank":      1,
		"last_active_at": now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(latest.ID, map[string]any{
		"sort_rank":      0,
		"last_active_at": now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	threadID, turnID, _, _, _ := e.tidyReviewAnchor(p.ID)
	if threadID != latest.ID || turnID != want.ID {
		t.Fatalf("anchor=%s/%s want %s/%s", threadID, turnID, latest.ID, want.ID)
	}
}

func TestTidyReviewAnchorFallsBackToTheMostRecentlyActiveThread(t *testing.T) {
	e := newTestEngine(t)
	p, stale := projectThread(t, e)
	fresh, err := e.CreateThread("fresh", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := e.Store().UpdateThread(stale.ID, map[string]any{
		"sort_rank":      1,
		"last_active_at": now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(fresh.ID, map[string]any{
		"sort_rank":      2,
		"last_active_at": now,
	}); err != nil {
		t.Fatal(err)
	}

	threadID, turnID, providerID, _, _ := e.tidyReviewAnchor(p.ID)
	if turnID != "" {
		t.Fatalf("no finished turn, got %s", turnID)
	}
	if threadID != fresh.ID {
		t.Fatalf("anchor thread=%s want %s", threadID, fresh.ID)
	}
	if providerID != fresh.ProviderID {
		t.Fatalf("provider=%s want %s", providerID, fresh.ProviderID)
	}
}

func TestTurnFinishedAtFallsBackToStartedAt(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	bare := store.Turn{StartedAt: started}
	if !turnFinishedAt(bare).Equal(started) {
		t.Fatal("a turn with no ended_at must use started_at")
	}
	ended := started.Add(time.Second)
	done := store.Turn{StartedAt: started, EndedAt: &ended}
	if !turnFinishedAt(done).Equal(ended) {
		t.Fatal("ended_at must win over started_at")
	}
	if !turnFinishedAfter(done, bare) {
		t.Fatal("the later finished turn must win")
	}
	tied := store.Turn{StartedAt: started.Add(time.Second), EndedAt: &ended}
	if !turnFinishedAfter(tied, done) {
		t.Fatal("equal ended_at must fall back to started_at")
	}
}
