package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPatchScheduleDestinationMovesLaterFiresOnly(t *testing.T) {
	e := newTestEngine(t)
	origin, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	project, err := e.CreateProject("desk", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	sch := mustCreateWake(t, e, origin.ID)
	before, err := e.GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := before.NextRunAt

	kind := store.ScheduleStandalone
	projectID := project.ID
	got, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ProjectID: &projectID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != store.ScheduleStandalone || got.ThreadID != "" || got.ProjectID != project.ID {
		t.Fatalf("standalone=%+v", got)
	}
	if got.OriginThreadID != origin.ID || !got.NextRunAt.Equal(next) {
		t.Fatalf("origin or next run moved: %+v", got)
	}
	if got.ProviderID == "" || got.Model == "" {
		t.Fatalf("standalone mint has no model snapshot: %+v", got)
	}
	keptProvider, keptModel := got.ProviderID, got.Model

	threadKind := store.ScheduleThread
	got, err = e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &threadKind, ThreadID: &other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != store.ScheduleThread || got.ThreadID != other.ID || got.ProjectID != "" {
		t.Fatalf("thread=%+v", got)
	}
	if got.OriginThreadID != origin.ID || !got.NextRunAt.Equal(next) {
		t.Fatalf("retarget rewrote origin or cadence: %+v", got)
	}
	if got.ProviderID != keptProvider || got.Model != keptModel {
		t.Fatalf("conversation target wiped the mint snapshot: %+v", got)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(got.Prompt, w) || strings.Contains(got.Title, w) {
			t.Fatalf("leaked %q into %+v", w, got)
		}
	}
}

func TestPatchScheduleDestinationKeepsAUsableModel(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		ProviderID: th.ProviderID, Model: "kept-name",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	kind := store.ScheduleStandalone
	empty := ""
	got, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ProjectID: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != th.ProviderID || got.Model != "kept-name" || got.ThreadID != "" {
		t.Fatalf("snapshot replaced: %+v", got)
	}
}

func TestPatchScheduleDestinationRejectsAClosedOrMissingTarget(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	archived, _ := e.CreateThread("", "", "")
	if err := e.SetThreadArchived(archived.ID, true); err != nil {
		t.Fatal(err)
	}
	sch := mustCreateWake(t, e, th.ID)
	before, err := e.GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}

	kind := store.ScheduleThread
	missing := "th_missing"
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ThreadID: &missing}); err == nil {
		t.Fatal("missing conversation")
	}
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ThreadID: &archived.ID}); err == nil {
		t.Fatal("archived conversation")
	}
	blank := "  "
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ThreadID: &blank}); err == nil {
		t.Fatal("blank conversation")
	}
	standalone := store.ScheduleStandalone
	badProject := "pj_missing"
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &standalone, ProjectID: &badProject}); err == nil {
		t.Fatal("missing project")
	}
	nope := "nope"
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &nope}); err == nil {
		t.Fatal("unknown kind")
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ThreadID: &th.ID}); err == nil {
		t.Fatal("cancelled wait")
	}

	got, err := e.GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != before.Kind || got.ThreadID != before.ThreadID || got.ProjectID != before.ProjectID || got.Status != store.ScheduleCancelled {
		t.Fatalf("rejected patch still wrote: %+v", got)
	}
}

func TestPatchScheduleDestinationAllowsPaused(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if _, err := e.PatchSchedule(sch.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	kind := store.ScheduleThread
	got, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ThreadID: &other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SchedulePaused || got.ThreadID != other.ID {
		t.Fatalf("paused retarget=%+v", got)
	}
}

func TestQuietFinishFollowsTheKindClaimedBeforeARetarget(t *testing.T) {
	e := newTestEngine(t)
	real, _ := e.CreateThread("", "", "")
	minted, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, real.ID)

	claimed := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: real.ID, Kind: store.ScheduleThread,
		Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(claimed); err != nil {
		t.Fatal(err)
	}
	kind := store.ScheduleStandalone
	empty := ""
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind, ProjectID: &empty}); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, real.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: claimed.ID})
	e.finishScheduledRun(turn, store.TurnDone, "", "")
	kept, err := e.Store().GetThread(real.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Archived {
		t.Fatal("retargeting to a new conversation must not archive the conversation already claimed")
	}

	legacyThread, _ := e.CreateThread("", "", "")
	legacy := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: legacyThread.ID, Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(legacy); err != nil {
		t.Fatal(err)
	}
	legacyTurn := mustStoredTurn(t, e, legacyThread.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: legacy.ID})
	e.finishScheduledRun(legacyTurn, store.TurnDone, "", "")
	legacyGot, err := e.Store().GetThread(legacyThread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !legacyGot.Archived {
		t.Fatal("a fire claimed before kind was stored still follows the schedule row")
	}

	fresh := &store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: minted.ID, Kind: store.ScheduleStandalone,
		Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(fresh); err != nil {
		t.Fatal(err)
	}
	threadKind := store.ScheduleThread
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &threadKind, ThreadID: &real.ID}); err != nil {
		t.Fatal(err)
	}
	mintTurn := mustStoredTurn(t, e, minted.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: fresh.ID})
	e.finishScheduledRun(mintTurn, store.TurnDone, "", "")
	got, err := e.Store().GetThread(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Archived {
		t.Fatal("a quiet fire claimed as a new conversation must still leave Recents after the wait is pinned")
	}
}

func TestPatchScheduleDestinationRefusesAModelItCannotMint(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	e.cfg.Models.Default = "nope"
	kind := store.ScheduleStandalone
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Kind: &kind}); err == nil {
		t.Fatal("standalone without a model")
	}
	got, err := e.GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != store.ScheduleThread || got.ThreadID != th.ID {
		t.Fatalf("failed model snapshot still wrote: %+v", got)
	}
}

func TestClaimedKindIgnoresALaterScheduleRow(t *testing.T) {
	if got := claimedScheduleKind(store.Schedule{}, true); got != store.ScheduleStandalone {
		t.Fatalf("standalone fallback=%q", got)
	}
	if got := claimedScheduleKind(store.Schedule{}, false); got != store.ScheduleThread {
		t.Fatalf("thread fallback=%q", got)
	}
	if got := claimedScheduleKind(store.Schedule{Kind: store.ScheduleThread}, true); got != store.ScheduleThread {
		t.Fatalf("stored kind=%q", got)
	}
	if scheduleRunMintsConversation(nil, nil) {
		t.Fatal("nothing claimed")
	}
	if !scheduleRunMintsConversation(&store.ScheduleRun{}, &store.Schedule{Kind: store.ScheduleStandalone}) {
		t.Fatal("empty kind must follow the schedule")
	}
	if scheduleRunMintsConversation(&store.ScheduleRun{Kind: store.ScheduleThread}, &store.Schedule{Kind: store.ScheduleStandalone}) {
		t.Fatal("claimed thread kind must win")
	}
}

func TestDueFireStampsTheKindItClaimed(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": time.Now().UTC().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	runs, err := e.ListRuns(sch.ID)
	if err != nil || len(runs) != 1 || runs[0].Kind != store.ScheduleThread || runs[0].ThreadID != th.ID {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[0].TurnID != "" {
		waitForTurn(t, e, runs[0].TurnID)
	} else {
		turns, err := e.Store().ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) == 1 {
			waitForTurn(t, e, turns[0].ID)
		}
	}
}
