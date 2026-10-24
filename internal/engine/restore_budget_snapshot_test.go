package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Warm recovery must preserve exactly the same turn-scoped allowances as the
// live log, including a human extension before or after the checkpoint.
func TestRestoreSnapshotPreservesBothCorrectionAllowances(t *testing.T) {
	for _, tc := range []struct {
		name               string
		extend, spendAgain bool
	}{
		{name: "consumed before checkpoint"},
		{name: "human renewed after checkpoint", extend: true},
		{name: "renewed then consumed again", extend: true, spendAgain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
			other := mustStoredTurn(t, e, th.ID, store.Turn{})
			cp, _ := json.Marshal(restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}})
			events := []store.Event{
				{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"schedule_claim_unverified"}`},
				{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"workers_uncollected"}`},
				{TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(cp)},
				{TurnID: other.ID, Kind: KindMaxIterationsContinued, AgentID: "manager"},
				{TurnID: turn.ID, Kind: KindMaxIterationsContinued, AgentID: "worker"},
			}
			if tc.extend {
				events = append(events, store.Event{TurnID: turn.ID, Kind: KindMaxIterationsContinued, AgentID: "manager"})
			}
			if tc.spendAgain {
				events = append(events, store.Event{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"workers_uncollected"}`})
			}
			for _, event := range events {
				event.ThreadID = th.ID
				if err := e.store.AppendEvent(&event); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := e.loadRestoreSnapshot(context.Background(), th.ID)
			if err != nil {
				t.Fatal(err)
			}
			// A warm allowance read must use this snapshot, even when another
			// database query would now fail. Normal live failures stay covered.
			if err := e.store.DB().Callback().Query().Before("gorm:query").Register("reject_budget_reread", func(db *gorm.DB) {
				if db.Statement.Table == "events" {
					_ = db.AddError(errors.New("unexpected recovery reread"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if used, err := e.scheduleAnswerContinued(turn, snapshot); err != nil || !used {
				t.Fatalf("schedule allowance was lost: used=%v err=%v", used, err)
			}
			wantCollection := !tc.extend || tc.spendAgain
			if used, err := e.workerCollectionContinued(turn, snapshot); err != nil || used != wantCollection {
				t.Fatalf("collection allowance changed: used=%v want=%v err=%v", used, wantCollection, err)
			}
		})
	}
}

// A resumed cap waits for new human work after the immutable snapshot. Both
// budget guards must see that new durable receipt before the actual HTTP model.
func TestRestoredHumanContinuationReadsItsNewBudgetReceiptBeforeModel(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
	cp, _ := json.Marshal(restartCheckpoint{Version: 1, Pause: "cap", CapUsed: 6, CapExtend: 3,
		ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}})
	for _, event := range []store.Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: turn.UserText},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"workers_uncollected"}`},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(cp)},
	} {
		if err := e.store.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	readsWithContinuation := 0
	var managers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input ordinaryLifecycleInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid model request", http.StatusBadRequest)
			return
		}
		if len(input.Messages) > 0 && input.Messages[0].Content == scheduleAnswerPrompt {
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		managers.Add(1)
		mu.Lock()
		reads := readsWithContinuation
		mu.Unlock()
		if reads != 2 {
			t.Errorf("manager HTTP preceded fresh allowance receipts: reads=%d", reads)
		}
		writeContextFlowReply(w, true, map[string]any{"content": "The assigned inspection is complete."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e.cfg.Models.Providers[0].BaseURL, e.cfg.Models.Providers[0].APIKey = endpoint.URL, ""
	e.cfg.Models.Providers[0].Model = "offline-restored-cap"
	e.pool = provider.New(e.cfg)
	if err := e.store.DB().Callback().Query().After("gorm:query").Register("observe_continued_budget", func(db *gorm.DB) {
		sql := db.Statement.SQL.String()
		if db.Statement.Table != "events" || strings.Contains(sql, "kind") || !strings.Contains(sql, "turn_id =") {
			return
		}
		events, ok := db.Statement.Dest.(*[]store.Event)
		if !ok || managers.Load() != 0 {
			return
		}
		for _, event := range *events {
			if event.Kind == KindMaxIterationsContinued && isManagerAgent(event.AgentID) {
				mu.Lock()
				readsWithContinuation++
				mu.Unlock()
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue })
	if managers.Load() != 0 {
		t.Fatal("model ran before the human continued")
	}
	if err := e.ContinueTurn(th.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone || managers.Load() != 1 {
		t.Fatalf("human continuation did not finish the original turn: turn=%+v managers=%d", got, managers.Load())
	}
}
