package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Catalog folding is deterministic and still needed when evidence cannot be
// read for an LLM review. Failing closed must not invent a zero-worker proof.
func TestLifecycleHistoryReadFailurePreservesSkillFolding(t *testing.T) {
	for _, mode := range []string{"review", "disabled", "manager-wrote"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			e.Config().Memory.AutoReview = mode != "disabled"
			p, th := projectThread(t, e)
			plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
			plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")
			turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
			if err := e.Store().CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "continue"})
			if mode == "manager-wrote" {
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "memory", Text: memory.ToolMemory + "({})"})
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), ToolCallID: "memory", Text: "stored"})
			}
			pc, err := e.projectContextFor(th)
			if err != nil {
				t.Fatal(err)
			}
			var fullReads atomic.Int32
			failure := errors.New("fixture: lifecycle history unavailable")
			const callback = "fixture_history_read_failure"
			err = e.Store().DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "events" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "thread_id = ? AND seq > ?") {
					fullReads.Add(1)
					_ = tx.AddError(failure) // Injected into the statement and returned by the query.
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = e.Store().DB().Callback().Query().Remove(callback) })
			e.scheduleReview(th.ID, turn, store.TurnDone, pc, "finished")
			if !e.reviews.stop(2 * time.Second) {
				t.Fatal("the deterministic fold did not finish")
			}
			if listedSkillNames(t, e, p.ID) != "weekly-rollup" {
				t.Fatalf("history failure left competing procedures: %s", listedSkillNames(t, e, p.ID))
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, call := range calls {
				if call.AgentID == ReviewAgentID {
					t.Fatal("reviewer ran without authoritative history")
				}
			}
			want := int32(0)
			if mode == "review" {
				want = 1
			}
			if fullReads.Load() != want {
				t.Fatalf("unnecessary history read in %s: %d, want %d", mode, fullReads.Load(), want)
			}
		})
	}
}

func TestGoalMemoryPersistenceFailuresKeepThePreviousBriefing(t *testing.T) {
	for _, stage := range []string{"history", "write"} {
		t.Run(stage, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
			if err := e.Store().CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{"session_memory": "previous briefing"}); err != nil {
				t.Fatal(err)
			}
			const callback = "fixture_memory_evidence_failure"
			failure := errors.New("fixture: memory evidence unavailable")
			if stage == "history" {
				if err := e.Store().DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "events" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "thread_id = ? AND seq > ?") {
						_ = tx.AddError(failure) // Injected into the statement and returned by the query.
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = e.Store().DB().Callback().Query().Remove(callback) })
			} else {
				if err := e.Store().DB().Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "threads" {
						_ = tx.AddError(failure) // Injected into the statement and returned by the query.
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = e.Store().DB().Callback().Update().Remove(callback) })
			}
			e.mergeSessionMemory(th.ID, turn.ID, "new progress")
			got, _ := e.Store().GetThread(th.ID)
			if got.SessionMemory != "previous briefing" {
				t.Fatalf("a failed %s rewrote the accepted briefing: %q", stage, got.SessionMemory)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil || len(events) != 1 || events[0].Kind != KindSessionMemory || events[0].Err == "" {
				t.Fatalf("a failed %s did not remain diagnosable: %+v, %v", stage, events, err)
			}
		})
	}
}

func TestReviewEvidenceFailureDoesNotStartAReviewer(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture: events unavailable")
	const callback = "fixture_all_event_reads_fail"
	if err := e.Store().DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			_ = tx.AddError(failure) // Injected into the statement and returned by the query.
		}
	}); err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "finished")
	if _, err := e.ReviewTurn(th.ID); !errors.Is(err, failure) {
		t.Fatalf("manual review hid its unavailable evidence: %v", err)
	}
	if err := e.Store().DB().Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReviewTurn(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("an empty turn invented lifecycle evidence to review: %v", err)
	}
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 0 {
		t.Fatalf("a reviewer ran without actual reviewable work: %+v, %v", calls, err)
	}
}
