package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const scheduleWaitPrompt = "Continue the wait."

func TestScheduleKindsAreStable(t *testing.T) {
	// These strings sit in the database and on the wire. Renaming one
	// silently breaks replay of existing conversations.
	if KindSchedule != "schedule" || KindScheduleFired != "schedule_fired" ||
		KindScheduleSkipped != "schedule_skipped" || KindScheduleReport != "schedule_report" ||
		KindScheduleCancelled != "schedule_cancelled" {
		t.Fatalf("kinds drifted: %q %q %q %q %q",
			KindSchedule, KindScheduleFired, KindScheduleSkipped, KindScheduleReport, KindScheduleCancelled)
	}
}

// A thread wake has to land as a Trace chip on the origin conversation,
// armed, without starting a turn. If this is missing, the human never sees
// that a wait was set.
func TestCreateThreadWakeRecordsScheduleEvent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	before := time.Now().UTC()
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "wake", Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sch.ID, "sch_") {
		t.Fatalf("id=%q", sch.ID)
	}
	if sch.Kind != store.ScheduleThread || sch.ThreadID != th.ID || sch.OriginThreadID != th.ID {
		t.Fatalf("ids=%+v", sch)
	}
	if sch.Status != store.ScheduleActive || sch.EveryS != 60 || sch.CreatedBy != store.ScheduleCreatedHuman {
		t.Fatalf("row=%+v", sch)
	}
	if sch.Prompt != scheduleWaitPrompt {
		t.Fatalf("prompt=%q", sch.Prompt)
	}
	wantNext := before.Add(60 * time.Second)
	latest := after.Add(60 * time.Second)
	if sch.NextRunAt.Before(wantNext) || sch.NextRunAt.After(latest) {
		t.Fatalf("next_run_at=%s window [%s,%s]", sch.NextRunAt, wantNext, latest)
	}
	if e.Status(th.ID).Running {
		t.Fatal("create must not fire a turn")
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("turns=%d, create is arming not firing", len(turns))
	}
	if hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("create must not record a fire")
	}
	ev := eventOfKind(t, e, th.ID, KindSchedule)
	var payload struct {
		ID        string    `json:"id"`
		Kind      string    `json:"kind"`
		Title     string    `json:"title"`
		Prompt    string    `json:"prompt"`
		ThreadID  string    `json:"thread_id"`
		Status    string    `json:"status"`
		NextRunAt time.Time `json:"next_run_at"`
	}
	if err := json.Unmarshal([]byte(ev.Text), &payload); err != nil {
		t.Fatalf("payload %q: %v", ev.Text, err)
	}
	if payload.ID != sch.ID || payload.Kind != store.ScheduleThread || payload.Title != "wake" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Prompt != scheduleWaitPrompt || payload.ThreadID != th.ID || payload.Status != store.ScheduleActive {
		t.Fatalf("payload=%+v", payload)
	}
	if !payload.NextRunAt.Equal(sch.NextRunAt) {
		t.Fatalf("payload next_run_at=%s want %s", payload.NextRunAt, sch.NextRunAt)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(ev.Text, w) || strings.Contains(sch.Prompt, w) {
			t.Fatalf("leaked %q", w)
		}
	}
}

func TestCreateScheduleRejectsIntervalBelowFloor(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 1,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("every_s=1 must fail a 30s floor")
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("rejected create still persisted: %+v", listed)
	}
}

func TestCancelScheduleWritesCancelledKind(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleCancelled {
		t.Fatalf("status=%q", got.Status)
	}
	if !hasKind(t, e, th.ID, KindScheduleCancelled) {
		t.Fatal("origin thread needs a cancelled chip")
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatalf("second cancel should be a no-op: %v", err)
	}
	if n := countThreadKind(t, e, th.ID, KindScheduleCancelled); n != 1 {
		t.Fatalf("cancelled events=%d, a no-op must not double the chip", n)
	}
}

func TestCreateScheduleUnknownThreadIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: "th_missing",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v, missing conversation must be ErrNotFound", err)
	}
}

func TestCreateScheduleThreadWakeNeedsAConversation(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.CreateSchedule(ScheduleInput{
		Kind:   store.ScheduleThread,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("thread wake without ThreadID")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("empty ThreadID is a bad request, not a missing row")
	}
}

func TestCreateStandaloneScheduleDoesNotFire(t *testing.T) {
	e := newTestEngine(t)
	origin, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		Title: "job", Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sch.ThreadID != "" {
		t.Fatalf("standalone ThreadID=%q, fires mint their own conversation", sch.ThreadID)
	}
	if sch.OriginThreadID != origin.ID || sch.CreatedBy != store.ScheduleCreatedManager {
		t.Fatalf("row=%+v", sch)
	}
	if e.Status(origin.ID).Running {
		t.Fatal("standalone create must not fire")
	}
	if hasKind(t, e, origin.ID, KindScheduleFired) {
		t.Fatal("standalone create must not record a fire")
	}
	if !hasKind(t, e, origin.ID, KindSchedule) {
		t.Fatal("origin still gets the armed chip")
	}
}

func TestCreateStandaloneScheduleWithoutOrigin(t *testing.T) {
	e := newTestEngine(t)
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind:   store.ScheduleStandalone,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sch.DelayS != 90 || sch.EveryS != 0 || sch.ThreadID != "" || sch.OriginThreadID != "" {
		t.Fatalf("row=%+v", sch)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != sch.ID {
		t.Fatalf("list=%+v", listed)
	}
}

// nextAfter is zero for a one-shot. The first due time is now+delay, or a
// delay wait looks due immediately (or never) and the ticker fires wrong.
func TestCreateDelaySetsNextRunAtFromNow(t *testing.T) {
	e := newTestEngine(t)
	before := time.Now().UTC()
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind:   store.ScheduleStandalone,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}
	if sch.NextRunAt.IsZero() {
		t.Fatal("delay next_run_at is zero; that is nextAfter, not the first due time")
	}
	if !sch.NextRunAt.After(after) {
		t.Fatalf("next_run_at=%s is not after now=%s; looks like now+0", sch.NextRunAt, after)
	}
	wantNext := before.Add(90 * time.Second)
	latest := after.Add(90 * time.Second)
	if sch.NextRunAt.Before(wantNext) || sch.NextRunAt.After(latest) {
		t.Fatalf("next_run_at=%s window [%s,%s]", sch.NextRunAt, wantNext, latest)
	}
}

func TestCreateScheduleCronSetsNextRunAtFromSpec(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	now := time.Now().UTC()
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, Cron: "0 0 1 1 *",
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sch.Cron != "0 0 1 1 *" || sch.EveryS != 0 || sch.DelayS != 0 {
		t.Fatalf("cadence fields=%+v", sch)
	}
	if !sch.NextRunAt.After(now) {
		t.Fatalf("cron next_run_at=%s not after %s", sch.NextRunAt, now)
	}
}

func TestCreateScheduleRejectsCronWithNoNextRun(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, Cron: "0 0 31 2 *",
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("February 31 never matches; create must fail")
	}
}

func TestCreateScheduleRejectsUnknownKind(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: "nope", Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("unknown kind")
	}
}

func TestCreateScheduleRejectsWhenActiveCapIsHit(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ScheduleMaxActive = 1
	th, _ := e.CreateThread("", "", "")
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("cap must reject the extra active row")
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("cap leak: %d rows", len(listed))
	}
}

// Inbox create used to read e.cfg.Swarm while the ticker had already
// frozen the cap. A PUT /settings Replace (or a test write) then let
// the inbox arm more waits than fireDueSchedules would run.
func TestCreateScheduleUsesFrozenCapWhenTickerIsLive(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.ScheduleMaxActive = 1
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	e.cfg.Swarm.ScheduleMaxActive = 32

	th, _ := e.CreateThread("", "", "")
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil || !strings.Contains(err.Error(), "too many active") {
		t.Fatalf("err=%v, create must use the frozen cap", err)
	}

	e.cfg.Swarm.ScheduleMaxActive = 2
	e.ApplyLiveSwarmLimits()
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatalf("settings must refresh the frozen cap: %v", err)
	}
}

func TestPatchScheduleResumeUsesFrozenCapWhenTickerIsLive(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.ScheduleMaxActive = 1
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	th, _ := e.CreateThread("", "", "")
	first := mustCreateWake(t, e, th.ID)
	if _, err := e.PatchSchedule(first.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.ScheduleMaxActive = 32
	_, err := e.PatchSchedule(first.ID, store.ScheduleActive)
	if err == nil || !strings.Contains(err.Error(), "too many active") {
		t.Fatalf("err=%v, resume must use the frozen cap", err)
	}
}

func TestPatchScheduleFieldsUsesFrozenMinIntervalWhenTickerIsLive(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	e.cfg.Swarm.ScheduleMinIntervalSeconds = 120
	every := 60
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{EveryS: &every}); err != nil {
		t.Fatalf("patch must use the frozen min interval, not a later cfg write: %v", err)
	}
}

func TestListSchedulesReturnsCreatedRows(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	a := mustCreateWake(t, e, th.ID)
	b, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 120,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("len=%d", len(listed))
	}
	ids := map[string]bool{}
	for _, row := range listed {
		ids[row.ID] = true
	}
	if !ids[a.ID] || !ids[b.ID] {
		t.Fatalf("listed=%+v", listed)
	}
}

func TestPatchSchedulePausesAndResumes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	paused, err := e.PatchSchedule(sch.ID, store.SchedulePaused)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != store.SchedulePaused {
		t.Fatalf("status=%q", paused.Status)
	}
	again, err := e.PatchSchedule(sch.ID, store.SchedulePaused)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != store.SchedulePaused {
		t.Fatalf("second pause=%q", again.Status)
	}
	n, err := e.Store().CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("paused row still counts as active: %d", n)
	}
	resumed, err := e.PatchSchedule(sch.ID, store.ScheduleActive)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != store.ScheduleActive {
		t.Fatalf("resumed status=%q", resumed.Status)
	}
}

func TestPatchScheduleResumeRespectsCap(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ScheduleMaxActive = 1
	th, _ := e.CreateThread("", "", "")
	first := mustCreateWake(t, e, th.ID)
	if _, err := e.PatchSchedule(first.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.PatchSchedule(first.ID, store.ScheduleActive)
	if err == nil {
		t.Fatal("resume must not blow past the active cap")
	}
}

func TestPatchScheduleUnknownIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.PatchSchedule("sch_missing", store.SchedulePaused)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelScheduleUnknownIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	if err := e.CancelSchedule("sch_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelScheduleRecordsOnWakeThread(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	if !hasKind(t, e, th.ID, KindScheduleCancelled) {
		t.Fatal("wake cancel lands on the target conversation")
	}
}

func TestPatchScheduleRejectsInvalidStatus(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	_, err := e.PatchSchedule(sch.ID, store.ScheduleCancelled)
	if err == nil {
		t.Fatal("patch is pause/resume, not cancel")
	}
}

func TestCreateScheduleUnknownOriginIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: "th_missing",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelStandaloneWithoutOriginLeavesNoChip(t *testing.T) {
	e := newTestEngine(t)
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind:   store.ScheduleStandalone,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleCancelled {
		t.Fatalf("status=%q", got.Status)
	}
}

func TestCancelScheduleFallsBackToWakeThread(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	row := &store.Schedule{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		NextRunAt: time.Now().UTC().Add(time.Minute),
		CreatedBy: store.ScheduleCreatedHuman,
	}
	if err := e.Store().CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	if !hasKind(t, e, th.ID, KindScheduleCancelled) {
		t.Fatal("empty origin must fall back to thread_id")
	}
}

func TestPatchScheduleRejectsTerminalRow(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	_, err := e.PatchSchedule(sch.ID, store.SchedulePaused)
	if err == nil {
		t.Fatal("cancelled row is not pauseable")
	}
}

func TestListSchedulesFailsWhenTheTableIsGone(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Store().DB().Migrator().DropTable(&store.Schedule{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListSchedules(); err == nil {
		t.Fatal("list must fail without the table")
	}
}

func TestCreateScheduleFailsWhenTheTableIsGone(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.Store().DB().Migrator().DropTable(&store.Schedule{}); err != nil {
		t.Fatal(err)
	}
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("create must fail without the table")
	}
}

func TestCreateScheduleRejectsEmptyPrompt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	_, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: "   ", EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err == nil {
		t.Fatal("empty prompt")
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(err.Error(), w) {
			t.Fatalf("leaked %q in %v", w, err)
		}
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("rejected create still persisted: %+v", listed)
	}
}

func TestCreateScheduleConcurrentStaysAtCap(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ScheduleMaxActive = 2
	const n = 10
	errs := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := e.CreateSchedule(ScheduleInput{
				Kind:   store.ScheduleStandalone,
				Prompt: scheduleWaitPrompt, EveryS: 60,
				CreatedBy: store.ScheduleCreatedHuman,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok int
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	active, err := e.Store().CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if active > 2 || ok > 2 {
		t.Fatalf("active=%d created=%d, cap leaked", active, ok)
	}
	if active != 2 || ok != 2 {
		t.Fatalf("active=%d created=%d, want 2", active, ok)
	}
}

func TestCancelScheduleConcurrentWritesOneEvent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := e.CancelSchedule(sch.ID); err != nil {
				t.Errorf("cancel: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := countThreadKind(t, e, th.ID, KindScheduleCancelled); n != 1 {
		t.Fatalf("cancelled events=%d, CAS must write one chip", n)
	}
}

func mustCreateWake(t *testing.T, e *Engine, threadID string) *store.Schedule {
	t.Helper()
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: threadID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func eventOfKind(t *testing.T, e *Engine, threadID, kind string) store.Event {
	t.Helper()
	events, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == kind {
			return ev
		}
	}
	t.Fatalf("missing %s event", kind)
	return store.Event{}
}

func countThreadKind(t *testing.T, e *Engine, threadID, kind string) int {
	t.Helper()
	events, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}
