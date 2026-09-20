package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

func reportNextInS(t *testing.T, e *Engine, threadID, runID string, nextInS int) string {
	t.Helper()
	turn := mustStoredTurn(t, e, threadID, store.Turn{ScheduleContinue: true, ScheduleRunID: runID})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(threadID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		fmt.Sprintf(`{"findings":"note","next_in_s":%d}`, nextInS))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustRunningRun(t *testing.T, e *Engine, schID, threadID string) string {
	t.Helper()
	run := &store.ScheduleRun{ScheduleID: schID, ThreadID: threadID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestReportScheduleNextInSDoesNotRearmCancelled(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	out := reportNextInS(t, e, th.ID, mustRunningRun(t, e, sch.ID, th.ID), 60)
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("cancelled recadence: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil || row.Status != store.ScheduleCancelled {
		t.Fatalf("status=%v err=%v, next_in_s must not undo cancel", row.Status, err)
	}
}

func TestReportScheduleNextInSDoesNotRearmPaused(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"status": store.SchedulePaused}); err != nil {
		t.Fatal(err)
	}
	out := reportNextInS(t, e, th.ID, mustRunningRun(t, e, sch.ID, th.ID), 60)
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("paused recadence: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil || row.Status != store.SchedulePaused {
		t.Fatalf("status=%v err=%v, next_in_s must not resume a pause", row.Status, err)
	}
}

func TestReportScheduleNextInSRespectsTheActiveCap(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"status": store.ScheduleDone}); err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.ScheduleMaxActive = 1
	out := reportNextInS(t, e, th.ID, mustRunningRun(t, e, sch.ID, th.ID), 60)
	if !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "too many active") {
		t.Fatalf("cap: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil || row.Status != store.ScheduleDone {
		t.Fatalf("status=%v err=%v", row.Status, err)
	}
}

func TestRearmScheduleIntervalNoopsOnZero(t *testing.T) {
	e := newTestEngine(t)
	if err := e.rearmScheduleInterval(nil, 60); err != nil {
		t.Fatal(err)
	}
	sch := &store.Schedule{ID: "sch_x", Status: store.ScheduleDone}
	if err := e.rearmScheduleInterval(sch, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRearmScheduleIntervalDoesNotResurrectAStaleActive(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	stale := *sch
	stale.Status = store.ScheduleActive
	if err := e.rearmScheduleInterval(&stale, 60); err == nil {
		t.Fatal("stale active copy must not recadence a cancelled row")
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil || row.Status != store.ScheduleCancelled {
		t.Fatalf("status=%v err=%v", row.Status, err)
	}
}

func TestScheduleWakeWithoutIdFailsWhenListBreaks(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if err := e.Store().DB().Migrator().DropTable(&store.Schedule{}); err != nil {
		t.Fatal(err)
	}
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("list broken: %s %v", out, err)
	}
}

func TestOpenThreadWakeIDPicksTheSoonestActive(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	later := mustCreateWake(t, e, th.ID)
	if err := e.Store().UpdateSchedule(later.ID, map[string]any{
		"next_run_at": time.Now().UTC().Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	sooner, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sooner.ID, map[string]any{
		"next_run_at": time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	paused, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 90,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(paused.ID, map[string]any{
		"status": store.SchedulePaused, "next_run_at": time.Now().UTC().Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	other, _ := e.CreateThread("", "", "")
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: other.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := e.openThreadWakeID(th.ID)
	if err != nil || id != sooner.ID {
		t.Fatalf("id=%q err=%v want %s", id, err, sooner.ID)
	}
}

func TestOpenThreadWakeIDEmptyIsNone(t *testing.T) {
	e := newTestEngine(t)
	id, err := e.openThreadWakeID("")
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	e.store = nil
	id, err = e.openThreadWakeID("th_x")
	if err != nil || id != "" {
		t.Fatalf("nil store id=%q err=%v", id, err)
	}
}

func TestRearmScheduleIntervalSurfacesAStoreError(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.Store().DB().Migrator().DropTable(&store.Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := e.rearmScheduleInterval(sch, 60); err == nil {
		t.Fatal("missing table must fail recadence")
	}
}

func TestRearmScheduleIntervalSurfacesADoneMismatch(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	stale := *sch
	stale.Status = store.ScheduleDone
	if err := e.rearmScheduleInterval(&stale, 60); err == nil {
		t.Fatal("cancelled row is not done")
	}
}
