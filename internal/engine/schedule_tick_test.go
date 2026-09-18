package engine

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newScheduleClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
}

func TestWakeSkippedWhenTheConversationIsRunning(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	live, err := e.StartTurn(th.ID, "work on this")
	if err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)

	sch := armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	if !hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("a busy conversation must record schedule_skipped")
	}
	if hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("a busy tick must not start a scheduled turn")
	}
	if hasKind(t, e, th.ID, KindSteer) {
		t.Fatal("a missed beat is not steering")
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].ID != live.ID {
		t.Fatalf("turns=%d, busy skip must not start a second turn", len(turns))
	}
	if e.Status(th.ID).TurnID != live.ID {
		t.Fatal("the live turn must keep the conversation")
	}

	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := clk.Now().Add(60 * time.Second)
	if !got.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%s want %s (from now, not a queued catch-up)", got.NextRunAt, wantNext)
	}
	if got.Status != store.ScheduleActive {
		t.Fatalf("status=%q, skip is not done", got.Status)
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunSkippedBusy {
		t.Fatalf("runs=%+v, want one skipped_busy", runs)
	}

	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestMissedTicksDoNotBurstAfterRestart(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	stale := clk.Now().Add(-2 * time.Hour)
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": stale}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d, overdue every_s=60 must fire once, not 120 catch-up turns", len(turns))
	}
	if !turns[0].ScheduleContinue {
		t.Fatal("the fire must mark ScheduleContinue so schedule_task stays blocked")
	}
	if turns[0].ScheduleRunID == "" {
		t.Fatal("the fire must bind ScheduleRunID")
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := clk.Now().Add(60 * time.Second)
	if !got.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%s want %s from now, not from the two-hour-stale due time", got.NextRunAt, wantNext)
	}
	if got.RunCount != 1 || got.LastRunAt == nil {
		t.Fatalf("run_count=%d last_run_at=%v", got.RunCount, got.LastRunAt)
	}

	clk.Advance(2 * time.Hour)
	e.fireDueSchedules()
	turns, err = e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d after a second tick; a still-running run must not double-fire", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
}

func TestScheduleFiredIsNotAUserMessage(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	turn := turns[0]
	if !turn.ScheduleContinue {
		t.Fatal("Turn.ScheduleContinue must be set so schedule_task stays blocked")
	}
	if turn.ScheduleRunID == "" {
		t.Fatal("ScheduleRunID empty")
	}
	waitForTurn(t, e, turn.ID)

	if !hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("timeline needs schedule_fired")
	}
	if hasKind(t, e, th.ID, KindUser) {
		t.Fatal("a scheduled check must not look like a human user_message")
	}
	ev := eventOfKind(t, e, th.ID, KindScheduleFired)
	if ev.Text != scheduleFiredNotice {
		t.Fatalf("timeline text=%q want %q", ev.Text, scheduleFiredNotice)
	}
	if ev.Text == ScheduleContinueText(sch.Prompt) {
		t.Fatal("the durable prompt must not be the timeline user_message")
	}

	wrapped := ScheduleContinueText(scheduleWaitPrompt)
	if turn.UserText != wrapped {
		t.Fatalf("stored user_text=%q want wrapper", turn.UserText)
	}
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 || msgs[0].Role != "user" || msgs[0].Content != wrapped {
		t.Fatalf("replay user=%+v, the model needs the wrapper", msgs)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(wrapped, w) || strings.Contains(ev.Text, w) || strings.Contains(sch.Prompt, w) {
			t.Fatalf("leaked %q", w)
		}
	}
	if !strings.Contains(wrapped, "report_schedule") || !strings.Contains(wrapped, scheduleWaitPrompt) {
		t.Fatalf("wrapper=%q", wrapped)
	}
}

func TestScheduleContinueTextStaysGeneric(t *testing.T) {
	got := ScheduleContinueText("  " + scheduleWaitPrompt + "  ")
	if !strings.HasPrefix(got, "This turn is a scheduled check.") {
		t.Fatalf("prefix: %q", got)
	}
	if !strings.Contains(got, scheduleWaitPrompt) {
		t.Fatalf("missing prompt: %q", got)
	}
	if strings.Contains(got, "  "+scheduleWaitPrompt) {
		t.Fatal("prompt must be trimmed")
	}
	for _, w := range []string{"CI", "deploy", "GitHub", "pull request", "cron-job"} {
		if strings.Contains(got, w) {
			t.Fatalf("leaked %q", w)
		}
	}
}

func TestWakeSkippedDuringPlanMode(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	sch := armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	if e.Status(th.ID).Running {
		t.Fatal("plan-mode skip must not start a turn")
	}
	if !hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("plan mode is the same skip as busy")
	}
	if hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("plan mode must not fire")
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("turns=%d", len(turns))
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := clk.Now().Add(60 * time.Second)
	if !got.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%s want %s", got.NextRunAt, wantNext)
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunSkippedBusy {
		t.Fatalf("runs=%+v", runs)
	}
}

func TestDelaySkipStaysDueForTheNextIdleTick(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	live, err := e.StartTurn(th.ID, "work on this")
	if err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)

	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()

	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A one-shot that hits a busy tick is not done: marking it done would
	// drop the check forever. next_run_at=now keeps it due for the next
	// idle tick instead of catching up or waiting another delay_s.
	if got.Status != store.ScheduleActive {
		t.Fatalf("status=%q, delay skip must stay active", got.Status)
	}
	if !got.NextRunAt.Equal(clk.Now()) {
		t.Fatalf("next_run_at=%s want now=%s", got.NextRunAt, clk.Now())
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].ID != live.ID {
		t.Fatalf("turns=%d", len(turns))
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunSkippedBusy {
		t.Fatalf("runs=%+v", runs)
	}

	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestCronSkipAdvancesFromNowWithoutCatchUp(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "work on this"); err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)

	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, Cron: "* * * * *",
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{
		"next_run_at": clk.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()

	spec, err := parseScheduleSpec(0, 0, "* * * * *", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	wantNext := spec.nextAfter(clk.Now())
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%s want %s from now; missed cron beats are not queued", got.NextRunAt, wantNext)
	}
	if hasKind(t, e, th.ID, KindScheduleFired) || hasKind(t, e, th.ID, KindSteer) {
		t.Fatal("cron skip must not fire or steer")
	}

	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestDelayFireMarksTheScheduleDone(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()

	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleDone {
		t.Fatalf("status=%q, a fired one-shot is done", got.Status)
	}
	if got.RunCount != 1 {
		t.Fatalf("run_count=%d", got.RunCount)
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || !turns[0].ScheduleContinue {
		t.Fatalf("turns=%+v", turns)
	}
	waitForTurn(t, e, turns[0].ID)
}

func TestStandaloneDueFireMintsAConversation(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	origin, _ := e.CreateThread("origin", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		Title: "nightly check", Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: origin.ProviderID, CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()

	if e.Status(origin.ID).Running {
		t.Fatal("standalone must not occupy the origin")
	}
	threads, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	var minted *store.Thread
	for i := range threads {
		if threads[i].ID != origin.ID {
			minted = &threads[i]
			break
		}
	}
	if minted == nil {
		t.Fatal("standalone fire must mint a conversation")
	}
	if minted.Title != "nightly check" {
		t.Fatalf("title=%q", minted.Title)
	}
	if minted.ProviderID != origin.ProviderID {
		t.Fatalf("provider=%q", minted.ProviderID)
	}
	if minted.Archived {
		t.Fatal("Task 9 archives quiet; a just-fired standalone stays visible")
	}
	turns, err := e.Store().ListTurns(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || !turns[0].ScheduleContinue {
		t.Fatalf("minted turns=%+v", turns)
	}
	waitForTurn(t, e, turns[0].ID)
}

func TestConcurrentFiresRespectMaxActive(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	a, _ := e.CreateThread("a", "", "")
	b, _ := e.CreateThread("b", "", "")
	sa := armDueWake(t, e, clk, a.ID, 60)
	sb := armDueWake(t, e, clk, b.ID, 60)
	// Create-time cap is this same knob; drop it after insert so two
	// rows exist and the ticker still has to leave one due.
	e.cfg.Swarm.ScheduleMaxActive = 1

	e.fireDueSchedules()

	ta, _ := e.Store().ListTurns(a.ID)
	tb, _ := e.Store().ListTurns(b.ID)
	started := 0
	if len(ta) > 0 {
		started++
	}
	if len(tb) > 0 {
		started++
	}
	if started != 1 {
		t.Fatalf("started=%d, the cap must leave leftover due rows for the next tick", started)
	}

	left := sa
	leftThread := a.ID
	if len(ta) > 0 {
		left = sb
		leftThread = b.ID
	}
	got, err := e.Store().GetSchedule(left.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive || !got.NextRunAt.Equal(clk.Now()) {
		t.Fatalf("leftover row=%+v, do not skip_busy a cap defer", got)
	}
	if hasKind(t, e, leftThread, KindScheduleSkipped) {
		t.Fatal("a cap defer is not skipped_busy")
	}

	if len(ta) > 0 {
		waitForTurn(t, e, ta[0].ID)
	}
	if len(tb) > 0 {
		waitForTurn(t, e, tb[0].ID)
	}
}

func TestScheduledFireDoesNotResetGoalBudgetOrAutoTitle(t *testing.T) {
	// 默认 mock 会 complete_goal，那是把目标结了，不是把预算清了。别搅在一块。
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now

	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_auto_turns": 3, "goal_capped": true, "title": "", "title_auto": true,
	}); err != nil {
		t.Fatal(err)
	}
	_ = armDueWake(t, e, clk, th.ID, 60)
	e.fireDueSchedules()

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hasKind(t, e, th.ID, KindTitle) {
			t.Fatal("a scheduled check must not run the conversation namer")
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalAutoTurns != 3 || !got.GoalCapped {
		t.Fatalf("budget was reset: auto=%d capped=%v", got.GoalAutoTurns, got.GoalCapped)
	}
	if got.Title != "" {
		t.Fatalf("title after the turn finished: %q", got.Title)
	}
}

func TestStartSchedulerIsIdempotentAndShutdownStopsIt(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	e.StartScheduler()
	e.fireDueSchedules()
	e.StopScheduler()
	e.StopScheduler()
	e.StartScheduler()
	e.Shutdown()
}

func armDueWake(t *testing.T, e *Engine, clk *fakeClock, threadID string, everyS int) *store.Schedule {
	t.Helper()
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: threadID,
		Prompt: scheduleWaitPrompt, EveryS: everyS,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	sch, err = e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func scheduleRuns(t *testing.T, e *Engine, scheduleID string) []store.ScheduleRun {
	t.Helper()
	var out []store.ScheduleRun
	if err := e.Store().DB().Where("schedule_id = ?", scheduleID).Order("created_at asc").Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func waitUntilRunning(t *testing.T, e *Engine, threadID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(threadID).Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("conversation never went busy")
}

func TestResumedScheduledTurnRecordsScheduleFired(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{
		ThreadID:         th.ID,
		UserText:         ScheduleContinueText(scheduleWaitPrompt),
		ProviderID:       th.ProviderID,
		Model:            th.Model,
		ScheduleContinue: true,
		ScheduleRunID:    "srun_orphan",
	}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: turn.UserText},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := e.ResumeOrphanedTurns()
	if err != nil || n != 1 {
		t.Fatalf("resumed %d err=%v", n, err)
	}
	waitForTurn(t, e, turn.ID)
	if !hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("resume must record schedule_fired, not invent a human send")
	}
	if hasKind(t, e, th.ID, KindUser) {
		t.Fatal("a resumed scheduled turn must not grow a user_message")
	}
}

func TestDueWakeWithoutAConversationIsLeft(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	row := &store.Schedule{
		Kind: store.ScheduleThread, ThreadID: "th_gone",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		Status: store.ScheduleActive, NextRunAt: clk.Now(),
		CreatedBy: store.ScheduleCreatedHuman,
	}
	if err := e.Store().CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	got, err := e.Store().GetSchedule(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive || got.RunCount != 0 {
		t.Fatalf("missing conversation must not consume the row: %+v", got)
	}
}

func TestBrokenCadenceDoesNotConsumeADueRow(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"delay_s": 90, "every_s": 60}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	if hasKind(t, e, th.ID, KindScheduleFired) {
		t.Fatal("broken cadence must not fire")
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive || got.RunCount != 0 {
		t.Fatalf("row=%+v", got)
	}
}

func TestStandaloneFireLeavesABrokenProviderDue(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "job",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: "provider-missing", CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive || got.RunCount != 0 {
		t.Fatalf("a mint failure must leave the job due: %+v", got)
	}
}

func TestStandaloneFireCopiesModelAndEffort(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	origin, _ := e.CreateThread("origin", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		Title: "nightly check", Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: origin.ProviderID, Model: "copied-model", ReasoningEffort: "low",
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	threads, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	var minted *store.Thread
	for i := range threads {
		if threads[i].ID != origin.ID {
			minted = &threads[i]
			break
		}
	}
	if minted == nil {
		t.Fatal("expected a minted conversation")
	}
	if minted.Model != "copied-model" || minted.ReasoningEffort != "low" {
		t.Fatalf("copied fields=%+v", minted)
	}
	turns, err := e.Store().ListTurns(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	waitForTurn(t, e, turns[0].ID)
}

func TestScheduledStartOnABusyThreadBecomesSkipped(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "work on this"); err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)
	sch := armDueWake(t, e, clk, th.ID, 60)
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.startScheduledTurn(*row, spec, clk.Now(), th.ID)
	if !hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("StartTurn ErrBusy must become skipped_busy")
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) == 0 || runs[len(runs)-1].Status != store.ScheduleRunSkippedBusy {
		t.Fatalf("runs=%+v", runs)
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NextRunAt.Equal(clk.Now().Add(60 * time.Second)) {
		t.Fatalf("next_run_at=%s", got.NextRunAt)
	}
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestScheduledStartOnAMissingThreadFinishesTheClaim(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.startScheduledTurn(*row, spec, clk.Now(), "th_gone")
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunError {
		t.Fatalf("runs=%+v", runs)
	}
}

func TestFireDueOnAClosedStoreIsANoop(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Store().Close()
	e.fireDueSchedules()
	e.skipBusy(*sch, spec, clk.Now(), th.ID)
	e.startScheduledTurn(*sch, spec, clk.Now(), th.ID)
	e.advanceAfterSkip(*sch, spec, clk.Now())
	delaySpec, err := parseScheduleSpec(90, 0, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	e.advanceAfterSkip(*sch, delaySpec, clk.Now())
	e.advanceAfterFire(*sch, spec, clk.Now())
	e.advanceAfterFire(*sch, delaySpec, clk.Now())
	e.fireStandalone(*sch, spec, clk.Now())
}
