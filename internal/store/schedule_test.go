package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A thread wake must come back with a filesystem-safe id and the durable
// instruction the manager stored. If this round-trip fails, the ticker has
// nothing to fire and the human cannot cancel it.
func TestCreateScheduleRoundTripsAThreadWake(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	row := &Schedule{
		Kind:           ScheduleThread,
		ThreadID:       th.ID,
		OriginThreadID: th.ID,
		Title:          "wake",
		Prompt:         "Continue the wait.",
		EveryS:         60,
		NextRunAt:      next,
		CreatedBy:      ScheduleCreatedManager,
	}
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(row.ID, "sch_") {
		t.Fatalf("id=%q", row.ID)
	}
	if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not filled: %+v", row)
	}
	if row.Status != ScheduleActive {
		t.Fatalf("empty status must land active, got %q", row.Status)
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.EveryS != 60 || got.ThreadID != th.ID || got.Kind != ScheduleThread {
		t.Fatalf("got=%+v", got)
	}
	if got.Prompt != "Continue the wait." {
		t.Fatalf("prompt=%q", got.Prompt)
	}
	if !got.NextRunAt.Equal(next) {
		t.Fatalf("next_run_at=%s want %s", got.NextRunAt, next)
	}
	if got.CreatedBy != ScheduleCreatedManager {
		t.Fatalf("created_by=%q", got.CreatedBy)
	}

	presetID := NewID("sch_")
	until := next.Add(24 * time.Hour)
	preset := &Schedule{
		ID: presetID, Kind: ScheduleStandalone, OriginThreadID: th.ID,
		ProjectID: "pj_x", ProviderID: "p1", Model: "m", ReasoningEffort: "low",
		Title: "preset", Prompt: "Continue the wait.",
		DelayS: 90, Cron: "* * * * *", MaxRuns: 3, UntilAt: &until, Status: ScheduleActive,
		NextRunAt: next, CreatedBy: ScheduleCreatedHuman,
	}
	if err := s.CreateSchedule(preset); err != nil {
		t.Fatal(err)
	}
	if preset.ID != presetID {
		t.Fatalf("caller-supplied id overwritten: %q", preset.ID)
	}
	gotPreset, err := s.GetSchedule(presetID)
	if err != nil {
		t.Fatal(err)
	}
	if gotPreset.DelayS != 90 || gotPreset.Cron != "* * * * *" || gotPreset.MaxRuns != 3 || gotPreset.ProjectID != "pj_x" ||
		gotPreset.ProviderID != "p1" || gotPreset.Model != "m" || gotPreset.ReasoningEffort != "low" {
		t.Fatalf("standalone fields=%+v", gotPreset)
	}
	if gotPreset.UntilAt == nil || !gotPreset.UntilAt.Equal(until) {
		t.Fatalf("until_at=%v want %s", gotPreset.UntilAt, until)
	}
}

// ListDue compares next_run_at against now.UTC(). A local wall time left on
// the struct would make a due row look future (or the reverse) after a
// timezone shift.
func TestCreateScheduleStoresNextRunAtInUTC(t *testing.T) {
	s := openTestStore(t)
	loc := time.FixedZone("west", -7*3600)
	when := time.Date(2026, 9, 18, 12, 0, 0, 0, loc)
	until := when.Add(time.Hour)
	last := when.Add(-time.Hour)
	row := &Schedule{
		Kind: ScheduleStandalone, Title: "t", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: when, UntilAt: &until, LastRunAt: &last, CreatedBy: ScheduleCreatedHuman,
	}
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if row.NextRunAt.Location() != time.UTC {
		t.Fatalf("next_run_at loc=%s", row.NextRunAt.Location())
	}
	if row.UntilAt == nil || row.UntilAt.Location() != time.UTC {
		t.Fatalf("until_at loc=%v", row.UntilAt)
	}
	if row.LastRunAt == nil || row.LastRunAt.Location() != time.UTC {
		t.Fatalf("last_run_at loc=%v", row.LastRunAt)
	}
	if !row.NextRunAt.Equal(when.UTC()) {
		t.Fatalf("next_run_at=%s want %s", row.NextRunAt, when.UTC())
	}
}

// The ticker asks for what is due now. A pause and a future next_run_at must
// not sneak into that list, or a paused wait would keep firing.
func TestListDueSchedulesSkipsPausedAndFuture(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	due := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "due", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: now.Add(-time.Minute), CreatedBy: ScheduleCreatedManager,
	}
	future := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "later", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: now.Add(time.Hour), CreatedBy: ScheduleCreatedManager,
	}
	paused := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "paused", Prompt: "Continue the wait.",
		EveryS: 60, Status: SchedulePaused,
		NextRunAt: now.Add(-time.Minute), CreatedBy: ScheduleCreatedHuman,
	}
	for _, row := range []*Schedule{due, future, paused} {
		if err := s.CreateSchedule(row); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListDue(now)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("due list=%+v", got)
	}

	n, err := s.CountActive()
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if n != 2 {
		t.Fatalf("active=%d, paused must not count toward the cap", n)
	}
}

// Deleting a conversation must stop wakes that would have landed on it.
// A standalone job that only originated there keeps running: it mints its
// own conversation per fire.
func TestDeleteThreadCancelsTargetedWakes(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	wake := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: now.Add(time.Minute), CreatedBy: ScheduleCreatedManager,
	}
	paused := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "paused", Prompt: "Continue the wait.",
		EveryS: 60, Status: SchedulePaused,
		NextRunAt: now.Add(-time.Minute), CreatedBy: ScheduleCreatedManager,
	}
	standalone := &Schedule{
		Kind: ScheduleStandalone, OriginThreadID: th.ID,
		Title: "job", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: now.Add(time.Minute), CreatedBy: ScheduleCreatedHuman,
	}
	for _, row := range []*Schedule{wake, paused, standalone} {
		if err := s.CreateSchedule(row); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}

	gotWake, err := s.GetSchedule(wake.ID)
	if err != nil {
		t.Fatalf("wake row must survive as cancelled, err=%v", err)
	}
	if gotWake.Status != ScheduleCancelled {
		t.Fatalf("wake status=%q, deleting the target must cancel it", gotWake.Status)
	}
	gotPaused, err := s.GetSchedule(paused.ID)
	if err != nil {
		t.Fatalf("paused wake must survive as cancelled, err=%v", err)
	}
	if gotPaused.Status != ScheduleCancelled {
		t.Fatalf("paused wake status=%q", gotPaused.Status)
	}
	gotJob, err := s.GetSchedule(standalone.ID)
	if err != nil {
		t.Fatalf("standalone row must survive, err=%v", err)
	}
	if gotJob.Status != ScheduleActive {
		t.Fatalf("standalone status=%q, origin-only delete must not cancel it", gotJob.Status)
	}

	if err := s.CancelSchedulesForThread(""); err != nil {
		t.Fatalf("empty thread id: %v", err)
	}
	gotJob, _ = s.GetSchedule(standalone.ID)
	if gotJob.Status != ScheduleActive {
		t.Fatalf("empty cancel must not wipe origin-only jobs: %q", gotJob.Status)
	}
}

// DeleteProject does not call DeleteThread. Wakes on those conversations and
// standalone jobs pinned to the project must still stop, or ListDue keeps
// firing into a workspace that no longer exists.
func TestDeleteProjectCancelsTargetedWakesAndPinnedJobs(t *testing.T) {
	s := openTestStore(t)
	p := &Project{Name: "P"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "in", ProjectID: p.ID}
	other := &Thread{Title: "out"}
	for _, x := range []*Thread{th, other} {
		if err := s.CreateThread(x); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	dueAt := now.Add(-time.Minute)
	wake := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: dueAt, CreatedBy: ScheduleCreatedManager,
	}
	outside := &Schedule{
		Kind: ScheduleThread, ThreadID: other.ID, OriginThreadID: other.ID,
		Title: "other", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: dueAt, CreatedBy: ScheduleCreatedManager,
	}
	pinned := &Schedule{
		Kind: ScheduleStandalone, ProjectID: p.ID, OriginThreadID: th.ID,
		Title: "pinned", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: dueAt, CreatedBy: ScheduleCreatedHuman,
	}
	originOnly := &Schedule{
		Kind: ScheduleStandalone, OriginThreadID: th.ID,
		Title: "loose", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: dueAt, CreatedBy: ScheduleCreatedHuman,
	}
	for _, row := range []*Schedule{wake, outside, pinned, originOnly} {
		if err := s.CreateSchedule(row); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}

	due, err := s.ListDue(now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range due {
		if row.ID == wake.ID || row.ID == pinned.ID {
			t.Fatalf("deleted project's schedule still due: id=%s kind=%s thread=%s project=%s",
				row.ID, row.Kind, row.ThreadID, row.ProjectID)
		}
	}

	gotWake, err := s.GetSchedule(wake.ID)
	if err != nil {
		t.Fatalf("wake row must survive as cancelled, err=%v", err)
	}
	if gotWake.Status != ScheduleCancelled {
		t.Fatalf("wake status=%q", gotWake.Status)
	}
	gotPinned, err := s.GetSchedule(pinned.ID)
	if err != nil {
		t.Fatalf("pinned job must survive as cancelled, err=%v", err)
	}
	if gotPinned.Status != ScheduleCancelled {
		t.Fatalf("pinned job status=%q", gotPinned.Status)
	}
	gotLoose, err := s.GetSchedule(originOnly.ID)
	if err != nil {
		t.Fatalf("origin-only job must survive, err=%v", err)
	}
	if gotLoose.Status != ScheduleActive {
		t.Fatalf("origin-only job status=%q, project delete must not cancel it", gotLoose.Status)
	}
	gotOutside, err := s.GetSchedule(outside.ID)
	if err != nil {
		t.Fatalf("outside wake must survive, err=%v", err)
	}
	if gotOutside.Status != ScheduleActive {
		t.Fatalf("outside wake status=%q", gotOutside.Status)
	}
	if err := cancelSchedulesForProject(s.DB(), ""); err != nil {
		t.Fatalf("empty project id: %v", err)
	}
}

// A fire has to be reconstructible: id prefix, running until FinishRun, then
// the findings/quiet status the inbox reads. Zero-value unread must actually
// persist or quiet runs stay marked unread forever.
func TestCreateRunRoundTripsAndFinishMarksFindings(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	sch := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "wake", Prompt: "Continue the wait.",
		EveryS: 60, Status: ScheduleActive,
		NextRunAt: time.Now().UTC(), CreatedBy: ScheduleCreatedManager,
	}
	if err := s.CreateSchedule(sch); err != nil {
		t.Fatal(err)
	}

	findings := &ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID}
	if err := s.CreateRun(findings); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(findings.ID, "srun_") {
		t.Fatalf("run id=%q", findings.ID)
	}
	if findings.Status != ScheduleRunRunning {
		t.Fatalf("new run status=%q", findings.Status)
	}

	turn := &Turn{
		ThreadID:         th.ID,
		UserText:         "Continue the wait.",
		ScheduleRunID:    findings.ID,
		ScheduleContinue: true,
	}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(findings.ID, ScheduleRunFindings, "something changed", true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(findings.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != ScheduleRunFindings || !got.Unread || got.Summary != "something changed" || got.TurnID != "" {
		t.Fatalf("findings run=%+v", got)
	}
	storedTurn, err := s.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTurn.ScheduleRunID != findings.ID || !storedTurn.ScheduleContinue || storedTurn.Quiet {
		t.Fatalf("scheduled turn fields=%+v", storedTurn)
	}

	quiet := &ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, TurnID: turn.ID, Unread: true}
	if err := s.CreateRun(quiet); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(quiet.ID, ScheduleRunQuiet, "", false); err != nil {
		t.Fatal(err)
	}
	gotQuiet, err := s.GetRun(quiet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuiet.Status != ScheduleRunQuiet || gotQuiet.Unread || gotQuiet.TurnID != turn.ID {
		t.Fatalf("quiet run must clear unread: %+v", gotQuiet)
	}

	quietTurn := &Turn{
		ThreadID:         th.ID,
		UserText:         "Continue the wait.",
		ScheduleRunID:    quiet.ID,
		ScheduleContinue: true,
		Quiet:            true,
	}
	if err := s.CreateTurn(quietTurn); err != nil {
		t.Fatal(err)
	}
	storedQuiet, err := s.GetTurn(quietTurn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !storedQuiet.Quiet || storedQuiet.ScheduleRunID != quiet.ID {
		t.Fatalf("quiet turn fields=%+v", storedQuiet)
	}

	skipID := NewID("srun_")
	skip := &ScheduleRun{
		ID: skipID, ScheduleID: sch.ID, ThreadID: th.ID,
		Status: ScheduleRunSkippedBusy,
	}
	if err := s.CreateRun(skip); err != nil {
		t.Fatal(err)
	}
	if skip.ID != skipID {
		t.Fatalf("caller-supplied run id overwritten: %q", skip.ID)
	}
	gotSkip, err := s.GetRun(skip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSkip.Status != ScheduleRunSkippedBusy || gotSkip.TurnID != "" {
		t.Fatalf("skipped run=%+v", gotSkip)
	}
}

// Missing ids must report ErrNotFound so HTTP can 404 instead of looking
// like a successful no-op.
func TestUnknownScheduleIsNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetSchedule("sch_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSchedule err=%v", err)
	}
	if _, err := s.GetRun("srun_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRun err=%v", err)
	}
	if err := s.FinishRun("srun_missing", ScheduleRunError, "x", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FinishRun err=%v", err)
	}
}

// Storage errors must not look like "not found": the HTTP layer would 404 a
// conversation whose database just died.
func TestScheduleWritesFailWhenTheTableIsGone(t *testing.T) {
	s := openTestStore(t)
	if err := s.DB().Migrator().DropTable(&Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSchedule(&Schedule{Kind: ScheduleThread, Prompt: "Continue the wait."}); err == nil {
		t.Fatal("CreateSchedule must fail without the table")
	}
	if _, err := s.GetSchedule("sch_x"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSchedule err=%v, want a storage error", err)
	}
	if _, err := s.ListDue(time.Now().UTC()); err == nil {
		t.Fatal("ListDue must fail without the table")
	}
	if _, err := s.CountActive(); err == nil {
		t.Fatal("CountActive must fail without the table")
	}
	if err := s.CancelSchedulesForThread("th_x"); err == nil {
		t.Fatal("CancelSchedulesForThread must fail without the table")
	}

	runs := openTestStore(t)
	if err := runs.DB().Migrator().DropTable(&ScheduleRun{}); err != nil {
		t.Fatal(err)
	}
	if err := runs.CreateRun(&ScheduleRun{}); err == nil {
		t.Fatal("CreateRun must fail without the table")
	}
	if _, err := runs.GetRun("srun_x"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRun err=%v, want a storage error", err)
	}
	if err := runs.FinishRun("srun_x", ScheduleRunError, "", true); err == nil {
		t.Fatal("FinishRun must fail without the table")
	}
}
