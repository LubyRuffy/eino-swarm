package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestScheduledRuntimeBindsItsClaimBeforeTheFirstModelTool(t *testing.T) {
	e, thread, _, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	// This fixture's earlier claimed turn is not running; Run now must claim
	// a new real fire, which the runtime must bind before calling a model.
	if err := e.Store().DB().Exec("UPDATE schedule_runs SET status = ? WHERE schedule_id = ?", store.ScheduleRunFindings, schedule).Error; err != nil {
		t.Fatal(err)
	}
	entered := make(chan string, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	db := e.Store().DB()
	name := "test:hold_claim_binding"
	if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "schedule_runs" {
			return
		}
		fields, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		id, _ := fields["turn_id"].(string)
		if id == "" {
			return
		}
		select {
		case entered <- id:
		default:
			return
		}
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblock(); _ = db.Callback().Update().Remove(name) })
	sub := e.Subscribe(thread)
	defer sub.Close()
	type outcome struct {
		turn *store.Turn
		err  error
	}
	finished := make(chan outcome, 1)
	go func() { turn, err := e.RunScheduleNow(schedule); finished <- outcome{turn, err} }()
	var turnID string
	select {
	case turnID = <-entered:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("claim binding was never reached")
	}
	// The binding transaction now holds the only DB connection. Inspect only
	// runtime state here; Status also asks the DB whether a wake is pending.
	startedEarly := e.runtimeFor(thread).status().Running
	var firstResult string
	if startedEarly {
		firstResult = waitLive(t, sub, "tool_result", 5*time.Second).Text
	}
	unblock()
	var got outcome
	select {
	case got = <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Run now did not return after releasing binding")
	}
	if got.err != nil || got.turn == nil || got.turn.ID != turnID {
		t.Fatalf("start result=%+v, expected exact turn %s", got, turnID)
	}
	if startedEarly {
		t.Fatalf("actual first model tool ran before durable binding: %s", firstResult)
	}
	firstResult = waitLive(t, sub, "tool_result", 5*time.Second).Text
	if !strings.Contains(firstResult, `"ok":true`) {
		t.Fatalf("first model tool rejected a valid fire: %s", firstResult)
	}
	waitForTurn(t, e, turnID)
}

func TestFailedClaimBindingDoesNotLaunchAModelOrLeaveATurnRunning(t *testing.T) {
	e, thread, _, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	if err := e.Store().DB().Exec("UPDATE schedule_runs SET status = ? WHERE schedule_id = ?", store.ScheduleRunFindings, schedule).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_claim_binding BEFORE UPDATE OF turn_id ON schedule_runs BEGIN SELECT RAISE(ABORT,'binding rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(schedule)
	if err == nil || turn != nil || e.Status(thread).Running {
		t.Fatalf("unbound runtime launched: turn=%+v err=%v status=%+v", turn, err, e.Status(thread))
	}
	turns, _ := e.Store().ListTurns(thread)
	last := turns[len(turns)-1]
	if last.Status != store.TurnError {
		t.Fatalf("binding failure left an orphan turn: %+v", last)
	}
	var calls int64
	if err := e.Store().DB().Model(&store.LLMCall{}).Where("turn_id = ?", last.ID).Count(&calls).Error; err != nil || calls != 0 {
		t.Fatalf("failed binding called a model: %d %v", calls, err)
	}
}

func TestGoalReportDoesNotPretendAMutationSucceededAfterASecondReadFails(t *testing.T) {
	for _, table := range []string{"schedule_runs", "schedules"} {
		t.Run(table, func(t *testing.T) {
			e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
			reads := 0
			name := "test:fail_second_claim_lookup"
			db := e.Store().DB()
			if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					reads++
					if reads == 2 {
						tx.AddError(errors.New("injected second lookup failure"))
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
			out, err := e.reportScheduleJSON(thread, turn, `{"keep":false}`)
			if err != nil {
				t.Fatal(err)
			}
			requireGoalScheduleToolOK(t, out, false)
			if !strings.Contains(out, "injected second lookup failure") {
				t.Fatalf("tool hid the failed mutation lookup: %s", out)
			}
		})
	}
}
