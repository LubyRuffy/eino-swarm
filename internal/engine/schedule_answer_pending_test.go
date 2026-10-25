package engine

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func TestPendingAnswerSaveRequiresLiveOwnershipAndDurablePrivateState(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("private admission ownership", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	_, done := ownManagerTestFixture(context.Background(), rt, turn.ID, reg)
	t.Cleanup(done)
	payload := pendingScheduleAnswer{AnswerID: "owned-answer"}
	if row, failure := e.savePendingScheduleAnswer(turn, "answer", payload.AnswerID, payload); failure != nil || row.State != "pending" {
		t.Fatalf("live candidate not saved: %+v %v", row, failure)
	}
	broken := payload
	broken.Transcript = []*schema.Message{{Extra: map[string]any{"invalid": func() {}}}}
	if _, failure := e.savePendingScheduleAnswer(turn, "answer", "bad-payload", broken); failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("invalid private payload was accepted: %+v", failure)
	}
	rt.mu.Lock()
	rt.userStopped = true
	rt.mu.Unlock()
	if _, failure := e.savePendingScheduleAnswer(turn, "answer", "stopped", payload); failure == nil || failure.Code != "answer_owner_stopped" {
		t.Fatalf("stopped runtime saved candidate: %+v", failure)
	}
	rt.mu.Lock()
	rt.userStopped = false
	rt.mu.Unlock()
	e.recordMu.Lock()
	e.droppedTurns[turn.ID] = struct{}{}
	e.recordMu.Unlock()
	if _, failure := e.savePendingScheduleAnswer(turn, "answer", "deleted", payload); failure == nil || failure.Code != "answer_owner_deleted" {
		t.Fatalf("deleted owner was recreated: %+v", failure)
	}
	e.recordMu.Lock()
	delete(e.droppedTurns, turn.ID)
	e.recordMu.Unlock()
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_private_candidate BEFORE INSERT ON pending_schedule_answers BEGIN SELECT RAISE(ABORT, 'private persistence unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, failure := e.savePendingScheduleAnswer(turn, "answer", "unwritten", payload); failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("failed persistence masqueraded as saved candidate: %+v", failure)
	}
	if failure := rt.rejectPendingScheduleAnswer(turn, nil); failure != nil {
		t.Fatal(failure)
	}
	if failure := rt.rejectPendingScheduleAnswer(turn, &store.PendingScheduleAnswer{ID: "missing"}); failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("lost candidate was reported rejected: %+v", failure)
	}
}

func TestScheduleAnswerRecoveryWaitIsCappedAndCancellable(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		wait    time.Duration
	}{{0, 5 * time.Second}, {1, 5 * time.Second}, {2, 10 * time.Second}, {3, 20 * time.Second}, {4, 30 * time.Second}, {100, 30 * time.Second}} {
		if got := scheduleAnswerRecoveryDelay(tc.attempt); got != tc.wait {
			t.Fatalf("attempt=%d delay=%s want=%s", tc.attempt, got, tc.wait)
		}
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("cancel recovery wait", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	for _, wait := range []time.Duration{0, time.Minute} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errScheduleAnswerSuperseded)
		_, failure := e.runtimeFor(th.ID).verifyPendingScheduleAnswer(ctx, turn, &store.PendingScheduleAnswer{RetryAt: time.Now().Add(wait)}, &pendingScheduleAnswer{})
		if failure == nil || failure.Code != "answer_superseded" || !errors.Is(failure, errScheduleAnswerSuperseded) {
			t.Fatalf("recovery wait ignored human guidance: %+v", failure)
		}
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	if !reg.SteerManager("updated instruction") {
		t.Fatal("could not queue existing guidance")
	}
	ctx, done := e.runtimeFor(th.ID).scheduleAnswerContext(context.Background(), reg)
	defer done()
	if !errors.Is(context.Cause(ctx), errScheduleAnswerSuperseded) {
		t.Fatal("already queued guidance did not supersede pending answer")
	}
}

func TestScheduleAnswerRecoveryCannotInventADurableRetryOnStorageFailure(t *testing.T) {
	e, _, _ := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, _ *http.Request, _ int32) { w.WriteHeader(http.StatusServiceUnavailable) })
	th, _ := e.CreateThread("retry storage failure", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	row := &store.PendingScheduleAnswer{ID: "retry-storage", ThreadID: th.ID, TurnID: turn.ID, Kind: "answer", Payload: `{}`}
	if err := e.store.SavePendingScheduleAnswer(row); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_private_retry BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT RAISE(ABORT, 'retry persistence unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	_, failure := e.runtimeFor(th.ID).verifyPendingScheduleAnswer(context.Background(), turn, row, &pendingScheduleAnswer{Text: []string{timingFixtureAnswer}})
	if failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("unwritten recovery attempt became durable: %+v", failure)
	}
}

func TestPendingReportAdmissionRejectsCorruptStateAndHonorsTheLiveOwner(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("private report admission", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	_, done := ownManagerTestFixture(context.Background(), rt, turn.ID, reg)
	t.Cleanup(done)
	save := func(id string) {
		t.Helper()
		_, failure := e.savePendingScheduleAnswer(turn, "report", id, pendingScheduleAnswer{AnswerID: id, Events: []store.Event{{ThreadID: th.ID, TurnID: turn.ID, Kind: KindScheduleReport, AgentID: "manager", Text: `{"findings":"","quiet":true}`}}})
		if failure != nil {
			t.Fatal(failure)
		}
	}
	save("quiet-report")
	if failure := rt.admitPendingScheduleReports(context.Background(), turn); failure != nil {
		t.Fatal(failure)
	}
	if failure := rt.admitPendingScheduleReports(context.Background(), turn); failure != nil {
		t.Fatalf("accepted report was not idempotent: %v", failure)
	}
	save("superseded-report")
	reg.SteerManager("updated instruction")
	if failure := rt.admitPendingScheduleReports(context.Background(), turn); failure == nil || failure.Code != "answer_superseded" {
		t.Fatalf("late guidance was bypassed by old report: %+v", failure)
	}
	reg.TakePendingSteerMessages()
	save("corrupt-report")
	if err := e.store.DB().Model(&store.PendingScheduleAnswer{}).Where("id = ?", "corrupt-report").Update("payload", `{"version":2}`).Error; err != nil {
		t.Fatal(err)
	}
	if failure := rt.admitPendingScheduleReports(context.Background(), turn); failure == nil || failure.Code != "answer_restore_invalid" {
		t.Fatalf("corrupt report became publishable: %+v", failure)
	}
	if err := e.store.DB().Migrator().DropTable(&store.PendingScheduleAnswer{}); err != nil {
		t.Fatal(err)
	}
	if failure := rt.admitPendingScheduleReports(context.Background(), turn); failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("missing private store was ignored: %+v", failure)
	}
}

func TestUnsupportedReportCannotPretendItsRejectedStateWasSaved(t *testing.T) {
	e, managers, policies := newScheduleAnswerFixture(t, "no-call", false)
	th, _ := e.CreateThread("report rejection storage", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	_, done := ownManagerTestFixture(context.Background(), rt, turn.ID, reg)
	t.Cleanup(done)
	_, failure := e.savePendingScheduleAnswer(turn, "report", "report-reject-storage", pendingScheduleAnswer{AnswerID: "report-reject-storage", Text: []string{fixtureScheduleClaim}})
	if failure != nil {
		t.Fatal(failure)
	}
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_report_rejection BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT RAISE(ABORT, 'private rejection unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	failure = rt.admitPendingScheduleReports(context.Background(), turn)
	if failure == nil || failure.Code != "answer_storage_failed" || managers.Load() != 0 || policies.Load() != 1 {
		t.Fatalf("unwritten rejection became a recoverable claim correction: failure=%+v managers=%d policies=%d", failure, managers.Load(), policies.Load())
	}
}
