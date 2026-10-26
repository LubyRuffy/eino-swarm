package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestLegacyClassifierFailureBudgetCannotBlockAgainAfterUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name               string
		codes              []string
		attempts, failures int
		deferred           bool
	}{
		{"exhausted", []string{"classification_unavailable", "classification_unavailable", "classification_unavailable"}, 3, 3, true},
		{"invalid-breaks-streak", []string{"classification_unavailable", "classification_invalid", "classification_unavailable"}, 3, 1, false},
		{"invalid-latest", []string{"classification_unavailable", "classification_invalid"}, 2, 0, false},
		{"gap", []string{"classification_unavailable"}, 2, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("legacy verification budget", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			row := &store.PendingScheduleAnswer{ID: "legacy-candidate", ThreadID: th.ID, TurnID: turn.ID, Kind: "answer", Payload: `{"version":1,"answer_id":"legacy-candidate"}`}
			if err := e.store.SavePendingScheduleAnswer(row); err != nil {
				t.Fatal(err)
			}
			row.Attempts = tc.attempts
			row.RetryAt = time.Now().Add(time.Hour)
			if err := e.store.SetScheduleAnswerRetry(row.ID, row.Attempts, row.RetryAt); err != nil {
				t.Fatal(err)
			}
			for i, code := range tc.codes {
				body, _ := json.Marshal(map[string]any{"candidate_id": row.ID, "attempt": i + 1, "code": code})
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: string(body)})
			}
			// Another candidate, malformed telemetry and a generic gate wait
			// cannot establish or erase this candidate's consumed allowance.
			for _, text := range []string{`{"candidate_id":"different","code":"classification_invalid"}`, `{`, `{"attempt":99}`} {
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: text})
			}
			if failure := e.restoreScheduleAnswerFailureBudget(turn, row); failure != nil {
				t.Fatal(failure)
			}
			got, err := e.store.GetPendingScheduleAnswer(row.ID)
			if err != nil || got.ClassificationFailures != tc.failures || (got.State == "deferred") != tc.deferred || !got.RetryAt.Equal(row.RetryAt) || got.Attempts != tc.attempts {
				t.Fatalf("budget=%+v error=%v", got, err)
			}
			if tc.deferred {
				_, failure := e.runtimeFor(th.ID).verifyPendingScheduleAnswer(context.Background(), turn, row, &pendingScheduleAnswer{})
				if failure == nil || failure.Code != "classification_deferred" {
					t.Fatalf("exhausted candidate regained execution: %v", failure)
				}
			}
		})
	}
}

func TestClassifierBudgetRecoveryCannotGrantWorkWhenStorageFails(t *testing.T) {
	for _, boundary := range []string{"read", "write"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("budget persistence", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			row := &store.PendingScheduleAnswer{ID: "restore-budget", ThreadID: th.ID, TurnID: turn.ID, Kind: "answer", Payload: `{}`}
			if err := e.store.SavePendingScheduleAnswer(row); err != nil {
				t.Fatal(err)
			}
			row.Attempts = 1
			if err := e.store.SetScheduleAnswerRetry(row.ID, 1, time.Time{}); err != nil {
				t.Fatal(err)
			}
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: `{"candidate_id":"restore-budget","attempt":1,"code":"classification_unavailable"}`})
			if boundary == "read" {
				if err := e.store.DB().Exec("ALTER TABLE events RENAME TO unavailable_budget_events").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_budget_restore BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT RAISE(ABORT, 'budget write unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			failure := e.restoreScheduleAnswerFailureBudget(turn, row)
			if failure == nil || failure.Code != "answer_storage_failed" {
				t.Fatalf("unreadable budget allowed work: %v", failure)
			}
		})
	}
}

// Without an executable goal/wake the classifier cannot invent permission to
// rerun the user's tools. Preserve the turn and use its existing human pause.
func TestDeferredOrdinaryAnswerRequiresHumanContinuation(t *testing.T) {
	for _, action := range []string{"continue", "already-approved", "already-steered", "decline", "stop", "already-stopped"} {
		t.Run(action, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("deferred human continuation", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			defer reg.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, release := ownManagerTestFixture(ctx, rt, turn.ID, reg)
			defer release()
			if action == "already-stopped" {
				cancel()
			}
			if action == "already-steered" {
				reg.SteerManager("Continue with the updated inspection.")
			}
			type outcome struct {
				messages []adk.Message
				budget   int
				err      error
			}
			result := make(chan outcome, 1)
			go func() {
				messages, budget, err := rt.continueAfterDeferredAnswer(ctx, turn, reg, swarm.RunResult{}, []adk.Message{schema.UserMessage("Inspect current state.")}, 20, 900, action == "already-approved")
				result <- outcome{messages, budget, err}
			}()
			if action != "already-stopped" && action != "already-approved" && action != "already-steered" {
				waitRestartState(t, func() bool { rt.mu.Lock(); defer rt.mu.Unlock(); return rt.continueCh != nil })
				switch action {
				case "continue":
					if err := e.ContinueTurn(th.ID, true); err != nil {
						t.Fatal(err)
					}
				case "decline":
					if err := e.ContinueTurn(th.ID, false); err != nil {
						t.Fatal(err)
					}
				case "stop":
					cancel()
				}
			}
			select {
			case got := <-result:
				switch action {
				case "continue", "already-approved", "already-steered":
					if got.err != nil || got.budget != e.cfg.SwarmSnapshot().ManagerIterations() || len(got.messages) < 2 || got.messages[len(got.messages)-1].Content != scheduleAnswerDeferredCue {
						t.Fatalf("wrong human continuation: %+v", got)
					}
				case "decline":
					if !isLimitStop(got.err) {
						t.Fatalf("decline=%v", got.err)
					}
				default:
					if !errors.Is(got.err, context.Canceled) {
						t.Fatalf("Stop=%v", got.err)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("deferred pause ignored human input")
			}
		})
	}
}
