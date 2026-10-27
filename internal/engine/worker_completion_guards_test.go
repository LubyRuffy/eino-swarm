package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func testCollectionPause() *workerCollectionPause {
	return &workerCollectionPause{Reason: "workers_uncollected", WaitID: "guard-wait", Extend: 2, WorkerIDs: []string{"original-worker"}, Workers: []swarm.PendingAgent{{AgentID: "original-worker", Generation: 1, Running: true}}, AutomaticResume: true}
}

func TestCompletionAuthorizationFailsClosedOnMalformedMissingAndUnreadableEvidence(t *testing.T) {
	p := testCollectionPause()
	for _, kind := range []string{KindMaxIterations, KindWorkersCollectionDeclined} {
		if _, _, err := workerCollectionWaitEvents([]store.Event{{AgentID: "manager", Kind: kind, Text: "{broken"}}); err == nil {
			t.Fatal("malformed wait decision was accepted")
		}
	}
	if workerCollectionReady(nil, nil) {
		t.Fatal("an empty roster established terminal completion")
	}
	for _, mode := range []string{"invalid-value", "deleted", "unreadable", "malformed-pause", "missing-pause"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("authorization guard", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			if mode == "invalid-value" {
				if err := rt.saveWorkerPauseEvent(turn, KindMaxIterations, func() {}); err == nil {
					t.Fatal("unserializable evidence was acknowledged")
				}
				return
			}
			if mode == "deleted" {
				e.droppedTurns[turn.ID] = struct{}{}
				if err := rt.saveWorkerPauseEvent(turn, KindMaxIterations, p); err == nil {
					t.Fatal("deleted owner accepted a wait")
				}
				if err := rt.saveWorkerCollectionContinue(turn, p); err == nil {
					t.Fatal("deleted owner accepted a human continuation")
				}
				return
			}
			if mode == "unreadable" {
				if err := e.store.DB().Callback().Query().Before("gorm:query").Register("guard_unreadable_events", func(tx *gorm.DB) {
					if tx.Statement.Table == "events" {
						tx.AddError(errors.New("injected unreadable wait evidence"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := rt.newWorkerCollectionPause(turn, p.Workers, 1, 2); err == nil {
					t.Fatal("unreadable evidence granted a new automatic wait")
				}
				reg := swarm.NewRegistry()
				defer reg.Close()
				if _, _, err := rt.waitForWorkerCollection(t.Context(), turn, reg, p); err == nil {
					t.Fatal("unreadable evidence entered a completion wait")
				}
			}
			if mode == "malformed-pause" {
				if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", Kind: KindMaxIterations, Text: "{broken"}); err != nil {
					t.Fatal(err)
				}
			}
			rt.mu.Lock()
			err := rt.saveWorkerPauseEvent(turn, KindWorkersCollectionResumed, workerCollectionReceipt{p.WaitID, p.WorkerIDs, p.Workers})
			continueErr := rt.saveWorkerCollectionContinue(turn, p)
			rt.mu.Unlock()
			if err == nil || continueErr == nil {
				t.Fatal("missing or unreadable paired authorization granted a model request")
			}
		})
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("empty wait", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if p, err := e.runtimeFor(th.ID).newWorkerCollectionPause(turn, nil, 0, 2); err != nil || p.AutomaticResume {
		t.Fatalf("empty wait granted automatic work: %+v %v", p, err)
	}
}

func TestUnknownCompletionGenerationFallsBackToHumanWithoutGrantingWork(t *testing.T) {
	for _, writeFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed-generation", true: "refusal-write-fails"}[writeFailure], func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("unknown completion", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			defer reg.Close()
			if writeFailure {
				if err := e.store.DB().Callback().Create().Before("gorm:create").Register("guard_refusal_write", func(tx *gorm.DB) {
					if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == KindMaxIterations && strings.Contains(ev.Text, "worker_generation_changed") {
						tx.AddError(errors.New("refusal evidence unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			proceed, automatic, err := rt.waitForWorkerCollection(ctx, turn, reg, testCollectionPause())
			if proceed || automatic || err == nil {
				t.Fatalf("unknown generation granted work: %v %v %v", proceed, automatic, err)
			}
			if !writeFailure {
				paused, _, readErr := e.workerCollectionWaitState(turn)
				if readErr != nil || paused == nil || paused.AutomaticResume || paused.Refusal != "worker_generation_changed" {
					t.Fatalf("generation refusal was not durable: %+v %v", paused, readErr)
				}
			}
		})
	}
}

func TestHumanSteeringAtCollectionPauseRequiresItsDurableContinuation(t *testing.T) {
	for _, writeFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "steer", true: "continuation-write-fails"}[writeFailure], func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("collection steering", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			defer reg.Close()
			p := testCollectionPause()
			p.AutomaticResume = false
			if writeFailure {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_continue BEFORE INSERT ON events WHEN NEW.kind='max_iterations_continued' BEGIN SELECT RAISE(ABORT,'continuation unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			result := make(chan error, 1)
			go func() {
				proceed, automatic, err := rt.waitForWorkerCollection(t.Context(), turn, reg, p)
				if err == nil && (!proceed || automatic) {
					err = errors.New("human steering did not remain a human continuation")
				}
				result <- err
			}()
			waitRestartState(t, func() bool { rt.mu.Lock(); defer rt.mu.Unlock(); return rt.continueCh != nil })
			if !rt.signalContinue(true) {
				t.Fatal("steering did not reach the active pause")
			}
			if err := <-result; (err != nil) != writeFailure {
				t.Fatalf("continuation persistence boundary: %v", err)
			}
			if !writeFailure {
				events, _ := e.store.ListTurnEvents(turn.ID)
				for _, ev := range events {
					if ev.Kind == KindMaxIterationsContinued && json.Valid([]byte(ev.Text)) {
						t.Fatal("legacy plain continuation text changed wire format")
					}
				}
			}
		})
	}
}
