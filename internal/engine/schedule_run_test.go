package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRunScheduleNowStartsAContinueTurnOnIdle(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if !sch.NextRunAt.After(time.Now().UTC()) {
		t.Fatal("create arms the future; run-now must still fire")
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if turn == nil || !turn.ScheduleContinue {
		t.Fatalf("turn=%+v, run-now must start a ScheduleContinue", turn)
	}
	if turn.ThreadID != th.ID {
		t.Fatalf("thread=%q want %q", turn.ThreadID, th.ID)
	}
}

func TestRunScheduleNowBusyTargetIsSkippedBusy(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	live, err := e.StartTurn(th.ID, scheduleWaitPrompt)
	if err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)

	_, err = e.RunScheduleNow(sch.ID)
	if !errors.Is(err, ErrSkippedBusy) {
		t.Fatalf("err=%v, want ErrSkippedBusy", err)
	}
	if !hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("busy run-now must record schedule_skipped")
	}
	running, err := e.Store().HasRunningRun(sch.ID)
	if err != nil || running {
		t.Fatalf("HasRunningRun=%v err=%v, skip must not claim a fire", running, err)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	_ = waitForTurn(t, e, live.ID)
}

func TestRunScheduleNowPausedIsAnError(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if _, err := e.PatchSchedule(sch.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(sch.ID)
	if err == nil {
		t.Fatal("paused run-now must fail")
	}
	if errors.Is(err, ErrSkippedBusy) {
		t.Fatal("paused is not skipped_busy")
	}
}

func TestRunScheduleNowUnknownIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.RunScheduleNow("sch_missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunScheduleNowInFlightRunIsSkippedBusy(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(sch.ID)
	if !errors.Is(err, ErrSkippedBusy) {
		t.Fatalf("err=%v", err)
	}
	if hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("an in-flight claim is not a second skipped run")
	}
}

func TestRunScheduleNowStandaloneMintsAContinueTurn(t *testing.T) {
	e := newTestEngine(t)
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "check",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if turn == nil || !turn.ScheduleContinue || turn.ThreadID == "" {
		t.Fatalf("turn=%+v", turn)
	}
}

func TestRunScheduleNowBrokenCadenceIsAnError(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{
		"delay_s": 0, "every_s": 0, "cron": "",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(sch.ID)
	if err == nil {
		t.Fatal("a stored row with no cadence must not fire")
	}
	if errors.Is(err, ErrSkippedBusy) {
		t.Fatal("broken cadence is not skipped_busy")
	}
}

func TestRunScheduleNowMissingThreadIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	row := &store.Schedule{
		Kind: store.ScheduleThread, ThreadID: "th_gone",
		Prompt: scheduleWaitPrompt, EveryS: 60, Status: store.ScheduleActive,
		NextRunAt: time.Now().UTC().Add(time.Minute),
		CreatedBy: store.ScheduleCreatedHuman,
	}
	if err := e.Store().CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(row.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunScheduleNowStandaloneNeedsAUsableModel(t *testing.T) {
	e := newTestEngine(t)
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "check",
		Prompt: scheduleWaitPrompt, EveryS: 60, ProviderID: "nope",
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.RunScheduleNow(sch.ID)
	if err == nil {
		t.Fatal("unusable model must not mint a turn")
	}
	if errors.Is(err, ErrSkippedBusy) {
		t.Fatal("a missing model is not skipped_busy")
	}
}

func TestRunScheduleNowHasRunningRunErrorSurfaces(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.Store().DB().Migrator().DropTable(&store.ScheduleRun{}); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(sch.ID)
	if err == nil {
		t.Fatal("missing runs table")
	}
}

func TestPatchScheduleFieldsRecadencesCron(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	cron := "0 9 * * *"
	got, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Cron: &cron})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cron != cron || got.EveryS != 0 || got.DelayS != 0 {
		t.Fatalf("row=%+v", got)
	}
	title := "named"
	got, err = e.PatchScheduleFields(sch.ID, ScheduleFields{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Cron != cron {
		t.Fatalf("title-only patch clobbered cadence: %+v", got)
	}
}

func TestScheduleCapHelpersFallBackWhenUnset(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ScheduleMaxActive = 0
	e.Config().Swarm.ScheduleMinIntervalSeconds = 0
	if e.maxActiveSchedules() != config.DefaultScheduleMaxActive {
		t.Fatalf("max=%d", e.maxActiveSchedules())
	}
	if e.scheduleMinInterval() != time.Duration(config.DefaultScheduleMinIntervalSeconds)*time.Second {
		t.Fatalf("min=%s", e.scheduleMinInterval())
	}
	e.Config().Swarm.ScheduleMaxActive = 4
	e.Config().Swarm.ScheduleMinIntervalSeconds = 45
	if e.maxActiveSchedules() != 4 {
		t.Fatalf("max=%d", e.maxActiveSchedules())
	}
	if e.scheduleMinInterval() != 45*time.Second {
		t.Fatalf("min=%s", e.scheduleMinInterval())
	}
	wantDefault := e.Config().Models.Default
	if e.scheduleDefaultProvider() != wantDefault {
		t.Fatalf("default=%q", e.scheduleDefaultProvider())
	}
	e.Config().Models.Default = "nope"
	if e.scheduleDefaultProvider() != "nope" {
		t.Fatal("unfrozen default follows cfg")
	}
	e.Config().Models.Default = wantDefault

	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	e.schedMaxActive.Store(0)
	e.schedMinIntervalS.Store(0)
	if e.maxActiveSchedules() != config.DefaultScheduleMaxActive {
		t.Fatalf("frozen zero max=%d", e.maxActiveSchedules())
	}
	if e.scheduleMinInterval() != time.Duration(config.DefaultScheduleMinIntervalSeconds)*time.Second {
		t.Fatalf("frozen zero min=%s", e.scheduleMinInterval())
	}
	e.snapshotScheduleCaps()
	e.Config().Swarm.ScheduleMaxActive = 1
	e.Config().Swarm.ScheduleMinIntervalSeconds = 90
	e.Config().Models.Default = "nope"
	if e.maxActiveSchedules() != 4 || e.scheduleMinInterval() != 45*time.Second {
		t.Fatal("a live ticker must not follow unsynced cfg writes")
	}
	if e.scheduleDefaultProvider() != wantDefault {
		t.Fatal("a live ticker must not follow unsynced default-provider writes")
	}
	e.schedDefaultProvider.Store(nil)
	if e.scheduleDefaultProvider() != "nope" {
		t.Fatal("frozen without a snapshot falls back to cfg")
	}
}

func TestRunScheduleNowPlanModeIsSkippedBusy(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.RunScheduleNow(sch.ID)
	if !errors.Is(err, ErrSkippedBusy) {
		t.Fatalf("err=%v", err)
	}
	if !hasKind(t, e, th.ID, KindScheduleSkipped) {
		t.Fatal("plan mode must record schedule_skipped")
	}
}

func TestScheduleInboxWrappersRoundTrip(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	got, err := e.GetSchedule(sch.ID)
	if err != nil || got.ID != sch.ID {
		t.Fatalf("GetSchedule=%+v err=%v", got, err)
	}
	if _, err := e.GetSchedule("sch_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing GetSchedule err=%v", err)
	}
	runs, err := e.ListRuns(sch.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("ListRuns=%+v err=%v", runs, err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, Status: store.ScheduleRunFindings, Unread: true}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	n, err := e.CountUnreadRuns()
	if err != nil || n != 1 {
		t.Fatalf("unread=%d err=%v", n, err)
	}
	if err := e.MarkRunRead(run.ID); err != nil {
		t.Fatal(err)
	}
	n, err = e.CountUnreadRuns()
	if err != nil || n != 0 {
		t.Fatalf("after read unread=%d err=%v", n, err)
	}
	listed, err := e.ListRuns(sch.ID)
	if err != nil || len(listed) != 1 || listed[0].Unread {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if err := e.MarkRunRead("srun_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing MarkRunRead err=%v", err)
	}
}

func TestPatchScheduleFieldsUpdatesTitlePromptAndCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	title := "later check"
	prompt := "Look again."
	every := 90
	got, err := e.PatchScheduleFields(sch.ID, ScheduleFields{
		Title: &title, Prompt: &prompt, EveryS: &every,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Prompt != prompt || got.EveryS != 90 || got.DelayS != 0 || got.Cron != "" {
		t.Fatalf("row=%+v", got)
	}
	if !got.NextRunAt.After(time.Now().UTC().Add(80 * time.Second)) {
		t.Fatalf("next_run_at=%s, recadence must recompute from now", got.NextRunAt)
	}
	paused := store.SchedulePaused
	got, err = e.PatchScheduleFields(sch.ID, ScheduleFields{Status: &paused})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SchedulePaused {
		t.Fatalf("status=%q", got.Status)
	}
}

func TestPatchScheduleFieldsRejectsBadCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	every := 1
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{EveryS: &every}); err == nil {
		t.Fatal("every_s below the floor")
	}
	delay, extra := 90, 60
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{DelayS: &delay, EveryS: &extra}); err == nil {
		t.Fatal("two cadences")
	}
	blank := "   "
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Prompt: &blank}); err == nil {
		t.Fatal("empty prompt")
	}
	if _, err := e.PatchScheduleFields("sch_missing", ScheduleFields{Title: &blank}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err=%v", err)
	}
	got, err := e.GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EveryS != 60 || got.Prompt != scheduleWaitPrompt {
		t.Fatalf("rejected patch still wrote: %+v", got)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(got.Prompt, w) {
			t.Fatalf("leaked %q", w)
		}
	}
}
