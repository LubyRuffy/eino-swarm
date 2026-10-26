package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// An unsupported answer is a failed turn, not evidence that the human or an
// external system must unblock the objective. Its retry budget must still end.
func TestScheduleClaimFailuresContinueGoalUntilTheirOwnDurableCap(t *testing.T) {
	e, manager, checks := newScheduleAnswerFixture(t, "no-call", false)
	e.cfg.Swarm.GoalMaxAutoTurns = 12
	th, err := e.CreateThread("schedule claim retry", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "complete the assigned work"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "Inspect the remaining work.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitScheduleClaimGoalCap(t, e, th.ID)
	waitSettled(t, e, th.ID)
	turns, err := e.store.ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 3 {
		t.Fatalf("a schedule claim must get bounded goal retries: turns=%+v", turns)
	}
	for i, turn := range turns {
		if turn.Status != store.TurnError || turn.Final != "" || !strings.Contains(turn.Error, "schedule_claim_unverified") || !strings.Contains(turn.Error, "no_matching_schedule") {
			t.Fatalf("failed candidate changed terminal meaning: %+v", turn)
		}
		if turn.GoalContinue != (i > 0) {
			t.Fatalf("retry lost its automatic origin: %+v", turn)
		}
	}
	got, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalBlocked || got.GoalBlockReason != "" || got.GoalComplete || got.GoalIdle || !got.GoalCapped || got.GoalAutoTurns != 2 {
		t.Fatalf("schedule validation was confused with external blockage or reset the budget: %+v", got)
	}
	if manager.Load() != 6 || checks.Load() != 6 {
		t.Fatalf("each turn gets exactly one manager correction: manager=%d checks=%d", manager.Load(), checks.Load())
	}
	events, err := e.store.ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	caps := 0
	for _, ev := range events {
		if ev.Kind == KindGoalBlocked {
			t.Fatalf("an answer check emitted an external blocker: %+v", ev)
		}
		if ev.Kind != KindGoalCapped {
			continue
		}
		caps++
		var body struct {
			Reason, Code string
			Attempt, Cap int
		}
		if err := json.Unmarshal([]byte(ev.Text), &body); err != nil || body.Reason != "schedule_claim_unverified" || body.Code != "no_matching_schedule" || body.Attempt != 3 || body.Cap != 3 || ev.TurnID != turns[2].ID {
			t.Fatalf("retry cap lost its distinct Trace reason: %+v error=%v", ev, err)
		}
	}
	if caps != 1 {
		t.Fatalf("wanted one durable cap receipt, got %d", caps)
	}
}

func TestScheduleClaimGoalBudgetUsesDurableTurnsAndHumanBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *Engine, string)
		human bool
		want  int
	}{
		{name: "replacement engine retains failures", want: 3},
		{name: "human message grants a new chain", human: true, want: 1},
		{name: "human resume grants a new chain", want: 1, setup: func(t *testing.T, e *Engine, id string) {
			e.record(store.Event{ThreadID: id, TurnID: e.lastTurnID(id), Kind: KindGoalResumed, Role: "human"})
		}},
		{name: "manager reopen grants no new attempts", want: 3, setup: func(t *testing.T, e *Engine, id string) {
			e.record(store.Event{ThreadID: id, TurnID: e.lastTurnID(id), Kind: KindGoalResumed, Role: "manager"})
		}},
		{name: "a new goal starts its own chain", want: 1, setup: func(t *testing.T, e *Engine, id string) {
			if err := e.SetThreadGoal(id, "a new objective"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "success breaks consecutive failures", want: 1, setup: func(t *testing.T, e *Engine, id string) {
			turn := mustStoredTurn(t, e, id, store.Turn{GoalContinue: true})
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Current work was inspected.", ""); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a different failure breaks the chain", want: 1, setup: func(t *testing.T, e *Engine, id string) {
			turn := mustStoredTurn(t, e, id, store.Turn{GoalContinue: true})
			if err := e.store.FinishTurn(turn.ID, store.TurnError, "", "another failure"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("durable schedule failure allowance", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
				t.Fatal(err)
			}
			storedScheduleClaimFailure(t, e, th.ID, false)
			storedScheduleClaimFailure(t, e, th.ID, true)
			if tc.setup != nil {
				tc.setup(t, e, th.ID)
			}
			last := storedScheduleClaimFailure(t, e, th.ID, !tc.human)
			// A new Engine has no previous runtime or counter. The same rows and
			// receipts must recover the exact allowance without a schema cache.
			replacement := New(e.cfg, e.store, e.pool, e.log)
			t.Cleanup(replacement.Shutdown)
			fresh, err := e.store.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			count, err := replacement.scheduleClaimGoalFailures(fresh, last)
			if err != nil || count != tc.want {
				t.Fatalf("durable failure count=%d want=%d err=%v", count, tc.want, err)
			}
		})
	}
}

func TestHumanResumeRestartsCappedGoalScheduleClaimsThroughHTTP(t *testing.T) {
	e, manager, checks := newScheduleAnswerFixture(t, "no-call", false)
	e.cfg.Swarm.GoalMaxAutoTurns = 12
	th, err := e.CreateThread("human goal retry", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitScheduleClaimGoalCap(t, e, th.ID)
	waitSettled(t, e, th.ID)
	resumed, err := e.ResumeThreadGoal(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, resumed.ID)
	waitScheduleClaimGoalCap(t, e, th.ID)
	waitSettled(t, e, th.ID)
	turns, err := e.store.ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 6 || manager.Load() != 12 || checks.Load() != 12 {
		t.Fatalf("human resume did not grant exactly one new bounded chain: turns=%d manager=%d checks=%d", len(turns), manager.Load(), checks.Load())
	}
	got, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalBlocked || !got.GoalCapped || got.GoalAutoTurns != 2 {
		t.Fatalf("resume lost retry limits or blocked the goal: %+v", got)
	}
}

func TestScheduleClaimRetryRefusesUnknownBudgetWithoutExternalBlocker(t *testing.T) {
	for _, failure := range []string{"missing-terminal", "missing-turn", "running-turn", "turn-query", "goal-query", "terminal-query", "cap-write", "cap-event"} {
		t.Run(failure, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("retry evidence refusal", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
				t.Fatal(err)
			}
			storedScheduleClaimFailure(t, e, th.ID, false)
			storedScheduleClaimFailure(t, e, th.ID, true)
			last := storedScheduleClaimFailure(t, e, th.ID, true)
			if failure == "missing-terminal" {
				if err := e.store.DB().Where("turn_id = ?", last.ID).Delete(&store.Event{}).Error; err != nil {
					t.Fatal(err)
				}
			} else if failure == "missing-turn" {
				if err := e.store.DB().Delete(&store.Turn{}, "id = ?", last.ID).Error; err != nil {
					t.Fatal(err)
				}
			} else if failure == "running-turn" {
				if err := e.store.UpdateTurn(last.ID, map[string]any{"status": store.TurnRunning}); err != nil {
					t.Fatal(err)
				}
			} else if failure == "cap-write" {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_claim_cap BEFORE UPDATE OF goal_capped ON threads BEGIN SELECT RAISE(ABORT, 'cap unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			} else if failure == "cap-event" {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_claim_cap_event BEFORE INSERT ON events WHEN NEW.kind='goal_capped' BEGIN SELECT RAISE(ABORT, 'cap receipt unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				// No querying actor is running; this private Store keeps the fault
				// installed until Close rather than racing callback removal.
				if err := e.store.DB().Callback().Query().Before("gorm:query").Register("claim-budget-refusal", func(tx *gorm.DB) {
					if failure == "turn-query" && tx.Statement.Table == "turns" || failure == "goal-query" && tx.Statement.Table == "events" || failure == "terminal-query" && tx.Statement.Table == "events" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), last.ID) {
						tx.AddError(errors.New("retry evidence unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			fresh, err := e.store.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			if e.allowScheduleClaimGoalRetry(fresh, last) {
				t.Fatal("unknown or exhausted allowance permitted another model turn")
			}
			got, err := e.store.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantCap := failure != "cap-write" && failure != "cap-event" && failure != "turn-query" && failure != "missing-turn"
			if got.GoalBlocked || got.GoalBlockReason != "" || got.GoalCapped != wantCap {
				t.Fatalf("budget refusal became an external blocker or claimed an uncommitted cap: %+v", got)
			}
		})
	}
}

func TestCorrectedScheduleClaimPublishesWithoutBlockingOrResettingGoalBudget(t *testing.T) {
	e, manager, checks := newScheduleAnswerFixture(t, "corrected", true)
	th, err := e.CreateThread("corrected goal answer", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal_auto_turns": 2}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(th.ID, UserInput{Text: GoalContinueText(), ContinueGoal: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	if got.Status != store.TurnDone || got.Final != "No future check has been arranged." || manager.Load() != 2 || checks.Load() != 2 {
		t.Fatalf("a corrected answer failed admission: turn=%+v manager=%d checks=%d", got, manager.Load(), checks.Load())
	}
	goal, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !pursuingGoal(goal) || goal.GoalBlocked || goal.GoalBlockReason != "" || goal.GoalCapped || goal.GoalAutoTurns != 2 {
		t.Fatalf("one answer correction reset or blocked the objective: %+v", goal)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, ev := range events {
		if ev.Kind != KindModelRetry {
			continue
		}
		retries++
		var body struct {
			Reason, Code string
			Attempt, Cap int
			Hash         string `json:"candidate_sha256"`
		}
		if err := json.Unmarshal([]byte(ev.Text), &body); err != nil || body.Reason != "schedule_claim_unverified" || body.Code != "no_matching_schedule" || body.Attempt != 1 || body.Cap != 1 || len(body.Hash) != 64 {
			t.Fatalf("correction lost its original receipt: event=%+v err=%v", ev, err)
		}
	}
	if retries != 1 || strings.Contains(replayDump(t, e, th.ID), fixtureScheduleClaim) {
		t.Fatalf("correction replay/retry mismatch: retries=%d", retries)
	}
}

func waitScheduleClaimGoalCap(t *testing.T, e *Engine, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		th, err := e.store.GetThread(id)
		if err != nil {
			t.Fatal(err)
		}
		if th.GoalCapped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the schedule claim retry chain never reached its cap")
}

func TestScheduleClaimFailureIdentityAndCounterBounds(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false}, {errors.New("no_matching_schedule"), false},
		{&scheduleAnswerError{Code: "classification_invalid"}, false},
		{fmt.Errorf("wrapped: %w", &scheduleAnswerError{Code: "no_matching_schedule"}), true},
	} {
		if got := isScheduleClaimMismatch(tc.err); got != tc.want {
			t.Fatalf("typed mismatch(%v)=%v want=%v", tc.err, got, tc.want)
		}
	}
	if isStoredScheduleClaimMismatch(nil) || isStoredScheduleClaimMismatch(&store.Turn{Status: store.TurnDone}) {
		t.Fatal("a successful/missing turn became an unsupported claim")
	}
	e := newTestEngine(t)
	th, err := e.CreateThread("counter bounds", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
		t.Fatal(err)
	}
	storedScheduleClaimFailure(t, e, th.ID, false)
	last := storedScheduleClaimFailure(t, e, th.ID, true)
	storedScheduleClaimFailure(t, e, th.ID, true)
	fresh, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	count, err := e.scheduleClaimGoalFailures(fresh, last)
	if err != nil || count != 2 {
		t.Fatalf("a later turn changed the earlier budget: count=%d err=%v", count, err)
	}
	if err := e.SetThreadGoal(th.ID, "replacement goal"); err != nil {
		t.Fatal(err)
	}
	count, err = e.scheduleClaimGoalFailures(fresh, last)
	if err != nil || count != 0 {
		t.Fatalf("old goal terminal survived the new goal boundary: count=%d err=%v", count, err)
	}
	fresh.GoalCapped = true
	if e.allowScheduleClaimGoalRetry(fresh, last) {
		t.Fatal("an existing cap was ignored")
	}
}

func TestRestoredScheduleClaimFailureNeverBecomesAnExternalGoalBlocker(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("restored goal admission failure", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "complete assigned work"); err != nil {
		t.Fatal(err)
	}
	storedScheduleClaimFailure(t, e, th.ID, false)
	e.blockOpenGoalOnTurnError(th.ID)
	got, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalBlocked || got.GoalCapped {
		t.Fatalf("a restored first failure blocked or capped the objective: %+v", got)
	}
	storedScheduleClaimFailure(t, e, th.ID, true)
	last := storedScheduleClaimFailure(t, e, th.ID, true)
	// This is the same admission used by a recovered idle runtime; it must
	// independently refuse more work even before a cap flag has been written.
	e.runtimeFor(th.ID).continueGoal(store.TurnDone)
	got, err = e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalCapped || got.GoalBlocked || got.GoalBlockReason != "" || e.Status(th.ID).Running {
		t.Fatalf("restored exhausted budget permitted work or fabricated a blocker: %+v", got)
	}
	if e.allowFailedScheduleClaimGoalRetry(last) {
		t.Fatal("a capped goal permitted a failed-turn retry")
	}
	if e.allowFailedScheduleClaimGoalRetry(&store.Turn{ThreadID: "missing-owner"}) {
		t.Fatal("a missing owner permitted a failed-turn retry")
	}
	other, err := e.CreateThread("ordinary turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.allowFailedScheduleClaimGoalRetry(&store.Turn{ThreadID: other.ID}) {
		t.Fatal("an ordinary failed answer invented a goal retry owner")
	}
}

func storedScheduleClaimFailure(t *testing.T, e *Engine, id string, auto bool) *store.Turn {
	t.Helper()
	turn := mustStoredTurn(t, e, id, store.Turn{GoalContinue: auto})
	errText := (&scheduleAnswerError{Code: "no_matching_schedule"}).Error()
	e.record(store.Event{ThreadID: id, TurnID: turn.ID, Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID, Err: errText})
	if err := e.store.FinishTurn(turn.ID, store.TurnError, "", errText); err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
