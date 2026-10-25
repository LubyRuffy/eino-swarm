package remote

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// A small phone response must not deserialize every historical turn just to
// locate the most recent answer. Keep checking the actual adapter queries.
func TestPhoneSnapshotsBoundTurnReads(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("history", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 90; i++ {
		turn := store.Turn{ThreadID: th.ID, Status: store.TurnDone, UserText: fmt.Sprint(i), Final: fmt.Sprintf("answer %d", i)}
		if err := e.Store().CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
	}
	running := store.Turn{ThreadID: th.ID, Status: store.TurnRunning, UserText: "latest request"}
	if err := e.Store().CreateTurn(&running); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary", "detail", "first watch"} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var bad []string
			callback := "test:bounded_turns_" + name
			if err := e.Store().DB().Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table != "turns" {
					return
				}
				query := tx.Statement.SQL.String()
				if tx.RowsAffected > 24 || !strings.Contains(query, "LIMIT") || strings.Contains(query, "SELECT *") {
					mu.Lock()
					bad = append(bad, fmt.Sprintf("rows=%d SQL=%s", tx.RowsAffected, query))
					mu.Unlock()
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := e.Store().DB().Callback().Query().Remove(callback); err != nil {
					t.Error(err)
				}
			}()
			switch name {
			case "summary":
				if got := threadSummary(e, th.ID, 80); got != "latest request" {
					t.Fatalf("summary=%q", got)
				}
			case "detail":
				d, err := openDetail(e, th.ID, config.RemoteConfig{OpenTurns: 2})
				if err != nil || len(d.Turns) != 2 || d.Turns[0].Text != "answer 88" || d.Turns[1].Text != "answer 89" {
					t.Fatalf("detail=%+v err=%v", d, err)
				}
			case "first watch":
				if _, _, err := lastTurnWindow(e.Store(), th.ID, 24); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(bad) > 0 {
				t.Fatalf("unbounded phone history reads: %v", bad)
			}
		})
	}
}

func TestPhoneSummaryPagesPastHiddenTurnsWithoutLosingOlderProse(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("history", "", "")
	if err != nil {
		t.Fatal(err)
	}
	base := store.Turn{ThreadID: th.ID, Status: store.TurnDone, Final: "still the visible answer"}
	if err := e.Store().CreateTurn(&base); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		turn := store.Turn{ThreadID: th.ID, Status: store.TurnDone, UserText: scheduledCheckPrefix, Final: `memory({"action":"add"})`}
		if err := e.Store().CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
	}
	quiet := store.Turn{ThreadID: th.ID, Status: store.TurnDone, Quiet: true, Final: "quiet must stay hidden"}
	if err := e.Store().CreateTurn(&quiet); err != nil {
		t.Fatal(err)
	}
	if got := threadSummary(e, th.ID, 80); got != "still the visible answer" {
		t.Fatalf("summary=%q", got)
	}
	if err := e.Store().UpdateTurn(base.ID, map[string]any{"final": ""}); err != nil {
		t.Fatal(err)
	}
	if got := threadSummary(e, th.ID, 80); got != "" {
		t.Fatalf("hidden-only summary=%q", got)
	}
	if got := threadSummary(e, "missing", 80); got != "" {
		t.Fatalf("missing summary=%q", got)
	}
	if err := e.Store().DB().Callback().Query().Before("gorm:query").Register("test:summary_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "turns" {
			_ = tx.AddError(errors.New("fixture read unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e.Store().DB().Callback().Query().Remove("test:summary_failure"); err != nil {
			t.Error(err)
		}
	}()
	if got := threadSummary(e, th.ID, 80); got != "" {
		t.Fatalf("failed summary=%q", got)
	}
	if d, err := openDetail(e, th.ID, config.RemoteConfig{}); err == nil || d != nil {
		t.Fatalf("failed detail=%+v %v", d, err)
	}
}

func TestPhoneRunningPreviewPagesPastBookkeepingAndReadsOnlyItsTurn(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(endpoint.Close)
	e := catalogEngine(t, config.Provider{ID: "fixture", BaseURL: endpoint.URL, Model: "fixture"})
	t.Cleanup(func() { close(release) })
	e.Config().Swarm.AutoTitle = false
	e.Config().Memory.AutoReview = false
	th, err := e.CreateThread("history", "", "")
	if err != nil {
		t.Fatal(err)
	}
	old := store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(&old); err != nil {
		t.Fatal(err)
	}
	history := make([]store.Event, 300)
	for i := range history {
		history[i] = store.Event{ThreadID: th.ID, TurnID: old.ID, Seq: int64(i + 1), Kind: "tool_result", Text: strings.Repeat("x", 4096)}
	}
	if err := e.Store().DB().CreateInBatches(&history, 100).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "Inspect the current state."); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model request not started")
	}
	turnID := e.Status(th.ID).TurnID
	visible := store.Event{ThreadID: th.ID, TurnID: turnID, Kind: "agent_message", Text: "current readable progress"}
	if err := e.Store().AppendEvent(&visible); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turnID, Kind: "tool_call", Text: `memory({"action":"add"})`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, TurnID: old.ID, Kind: "delta", Text: "wrong older turn"}); err != nil {
		t.Fatal(err)
	}
	var reads int
	if err := e.Store().DB().Callback().Query().After("gorm:query").Register("test:bounded_actions", func(tx *gorm.DB) {
		query := tx.Statement.SQL.String()
		if !strings.Contains(query, "FROM `events`") && !strings.Contains(query, "FROM events ") {
			return
		}
		reads++
		if tx.RowsAffected > 24 || !strings.Contains(query, "turn_id = ?") || !strings.Contains(query, "kind = ?") || !strings.Contains(query, "LIMIT") || strings.Contains(query, "SELECT *") {
			t.Errorf("unbounded current-action read rows=%d query=%s", tx.RowsAffected, query)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e.Store().DB().Callback().Query().Remove("test:bounded_actions"); err != nil {
			t.Error(err)
		}
	}()
	got := runningView(e, th, 80)
	if got.Action != "current readable progress" || reads < 6 {
		t.Fatalf("action=%q reads=%d", got.Action, reads)
	}
	if err := e.Store().DB().Delete(&visible).Error; err != nil {
		t.Fatal(err)
	}
	if got := runningView(e, th, 80); got.Action != "" {
		t.Fatalf("hidden-only action=%q", got.Action)
	}
	if err := e.Store().DB().Callback().Query().Before("gorm:query").Register("test:action_failure", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.Table, "events") {
			_ = tx.AddError(errors.New("fixture event read unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e.Store().DB().Callback().Query().Remove("test:action_failure"); err != nil {
			t.Error(err)
		}
	}()
	if got := runningView(e, th, 80); got.Action != "" {
		t.Fatalf("failed action=%q", got.Action)
	}
}

func TestPhoneAskingForHumanDoesNotReadActionHistory(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })
	e := testEngine(t)
	e.Config().Swarm.AutoTitle = false
	e.Config().Memory.AutoReview = false
	th, err := e.CreateThread("human question", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "Inspect the current state."); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !e.Status(th.ID).AwaitingAnswer && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !e.Status(th.ID).AwaitingAnswer {
		t.Fatal("real manager ask did not pause for a human")
	}
	var actionReads int
	if err := e.Store().DB().Callback().Query().After("gorm:query").Register("test:ask_action_reads", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "idx_evt_thread_turn_kind_seq") {
			actionReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e.Store().DB().Callback().Query().Remove("test:ask_action_reads"); err != nil {
			t.Error(err)
		}
	}()
	view := runningView(e, th, 80)
	if view.Action != "ask_user" || !view.AskUser || actionReads != 0 {
		t.Fatalf("human question=%+v action reads=%d", view, actionReads)
	}
}

func TestPhoneLastTurnWindowKeepsTrailingLifecycleCursorAndReadFailures(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("tail", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "agent_message", Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, Kind: "generation_recovered", Text: "receipt"}); err != nil {
			t.Fatal(err)
		}
	}
	page, more, err := lastTurnWindow(e.Store(), th.ID, 2)
	if err != nil || !more || len(page) != 2 || page[0].Seq != 2 || page[1].Seq != 3 {
		t.Fatalf("trailing page=%+v more=%v err=%v", page, more, err)
	}
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			failure := errors.New("fixture event read unavailable")
			reads := 0
			callback := fmt.Sprintf("test:tail_failure_%d", failAt)
			if err := e.Store().DB().Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "events" {
					reads++
					if reads == failAt {
						_ = tx.AddError(failure)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := e.Store().DB().Callback().Query().Remove(callback); err != nil {
					t.Error(err)
				}
			}()
			page, more, err := lastTurnWindow(e.Store(), th.ID, 2)
			if !errors.Is(err, failure) || page != nil || more {
				t.Fatalf("partial read must preserve failure: page=%+v more=%v err=%v", page, more, err)
			}
		})
	}
}
