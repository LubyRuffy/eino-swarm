package store

import (
	"errors"
	"testing"
	"time"
)

// Rows from before kind existed must keep their creation relationship even
// when future fires are moved to another destination.
func TestRetargetFreezesLegacyRunKindAndHistory(t *testing.T) {
	for _, oldKind := range []string{ScheduleStandalone, ScheduleThread} {
		t.Run(oldKind, func(t *testing.T) {
			s := openTestStore(t)
			chat, target := mustThread(t, s), mustThread(t, s)
			row := mustSchedule(t, s, &Schedule{Kind: oldKind, ThreadID: chat.ID,
				Prompt: "Continue the wait.", EveryS: 60, NextRunAt: time.Now().Add(time.Hour)})
			run := &ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID, Status: ScheduleRunFindings}
			if err := s.CreateRun(run); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().Model(run).UpdateColumn("kind", nil).Error; err != nil {
				t.Fatal(err)
			}
			newKind := ScheduleThread
			if oldKind == ScheduleThread {
				newKind = ScheduleStandalone
			}
			if err := s.UpdateSchedule(row.ID, map[string]any{"kind": newKind, "thread_id": target.ID}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetRun(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != oldKind || got.ThreadID != chat.ID || got.Status != run.Status || !got.UpdatedAt.Equal(run.UpdatedAt) {
				t.Fatalf("legacy fire changed meaning or history: %+v", got)
			}
			history, err := s.SchedulesForThread(chat.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantMint := oldKind == ScheduleStandalone
			if containsID(history.CreatedBy, row.ID) != wantMint {
				t.Fatalf("created_by=%v, old kind=%s", history.CreatedBy, oldKind)
			}
			updated, err := s.GetSchedule(row.ID)
			if err != nil || !updated.NextRunAt.Equal(row.NextRunAt) {
				t.Fatalf("retarget moved cadence: %+v err=%v", updated, err)
			}
		})
	}
}

func TestRetargetRollbackKeepsLegacyRunAndDestination(t *testing.T) {
	for _, table := range []string{"schedule_runs", "schedules"} {
		t.Run(table, func(t *testing.T) {
			s := openTestStore(t)
			row := mustSchedule(t, s, &Schedule{Kind: ScheduleStandalone, Prompt: "Continue the wait.", NextRunAt: time.Now().Add(time.Hour)})
			run := &ScheduleRun{ScheduleID: row.ID, Status: ScheduleRunRunning}
			if err := s.CreateRun(run); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().Exec("CREATE TRIGGER refuse_retarget BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateSchedule(row.ID, map[string]any{"kind": ScheduleThread, "thread_id": "new-target"}); err == nil {
				t.Fatal("destination edit ignored a required write failure")
			}
			got, err := s.GetRun(run.ID)
			if err != nil || got.Kind != "" || !got.UpdatedAt.Equal(run.UpdatedAt) {
				t.Fatalf("failed edit partially froze history: %+v err=%v", got, err)
			}
			current, err := s.GetSchedule(row.ID)
			if err != nil || current.Kind != row.Kind || current.ThreadID != row.ThreadID || !current.NextRunAt.Equal(row.NextRunAt) {
				t.Fatalf("failed edit changed destination: %+v err=%v", current, err)
			}
		})
	}
}

func TestRetargetPreservesExplicitRunsAndClaimedWakeOwnership(t *testing.T) {
	s := openTestStore(t)
	chat, next := mustThread(t, s), mustThread(t, s)
	row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: chat.ID, Prompt: "Continue the wait.", NextRunAt: time.Now().Add(time.Hour)})
	run := &ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID, Kind: ScheduleThread, Status: ScheduleRunRunning}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(row.ID, map[string]any{"kind": ScheduleStandalone, "thread_id": ""}); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.HasPendingThreadWake(chat.ID); err != nil || !pending {
		t.Fatalf("claimed old wake lost its owner: pending=%v err=%v", pending, err)
	}
	if ids, err := s.PendingWakeThreadIDs(); err != nil || !containsID(ids, chat.ID) {
		t.Fatalf("waiting sidebar lost claimed old wake: ids=%v err=%v", ids, err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil || got.Kind != run.Kind || !got.UpdatedAt.Equal(run.UpdatedAt) {
		t.Fatalf("explicit run changed: %+v err=%v", got, err)
	}
	if err := s.FinishRun(run.ID, ScheduleRunQuiet, "", false); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.HasPendingThreadWake(chat.ID); err != nil || pending {
		t.Fatalf("finished fire retained owner: pending=%v err=%v", pending, err)
	}
	standalone := &ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID, Kind: ScheduleStandalone}
	if err := s.CreateRun(standalone); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(row.ID, map[string]any{"kind": ScheduleThread, "thread_id": next.ID}); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.HasPendingThreadWake(chat.ID); err != nil || pending {
		t.Fatalf("claimed standalone masqueraded as old wake: pending=%v err=%v", pending, err)
	}
	if ids, err := s.PendingWakeThreadIDs(); err != nil || containsID(ids, chat.ID) || !containsID(ids, next.ID) {
		t.Fatalf("waiting sidebar retargeted a claimed standalone: ids=%v err=%v", ids, err)
	}
}

func TestRetargetReadFailuresDoNotChangeHistory(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpdateSchedule("missing", map[string]any{"kind": ScheduleThread}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing schedule: %v", err)
	}
	if err := s.DB().Migrator().DropTable(&Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule("missing", map[string]any{"kind": ScheduleThread}); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("storage error must retain its class: %v", err)
	}
}

func TestRetargetAndResumeRespectCapTogether(t *testing.T) {
	s := openTestStore(t)
	row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: "old-target", Status: SchedulePaused, Prompt: "Continue the wait.", NextRunAt: time.Now().Add(time.Hour)})
	run := &ScheduleRun{ScheduleID: row.ID, ThreadID: row.ThreadID}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	active := mustSchedule(t, s, &Schedule{Kind: ScheduleStandalone, Prompt: "Continue the wait."})
	fields := map[string]any{"kind": ScheduleStandalone, "thread_id": "", "status": ScheduleActive}
	if _, err := s.PatchScheduleFields(row.ID, fields, 1); !errors.Is(err, ErrScheduleCap) {
		t.Fatalf("resume cap: %v", err)
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil || got.Kind != row.Kind || got.Status != row.Status || got.ThreadID != row.ThreadID || !got.UpdatedAt.Equal(row.UpdatedAt) {
		t.Fatalf("rejected resume moved the destination: %+v err=%v", got, err)
	}
	claimed, err := s.GetRun(run.ID)
	if err != nil || claimed.Kind != "" {
		t.Fatalf("rejected resume changed legacy ownership: %+v err=%v", claimed, err)
	}
	if err := s.UpdateSchedule(active.ID, map[string]any{"status": SchedulePaused}); err != nil {
		t.Fatal(err)
	}
	before, err := s.PatchScheduleFields(row.ID, fields, 1)
	if err != nil || before.Kind != row.Kind || before.Status != row.Status || before.ThreadID != row.ThreadID {
		t.Fatalf("actual displaced owner: %+v err=%v", before, err)
	}
	got, err = s.GetSchedule(row.ID)
	if err != nil || got.Kind != ScheduleStandalone || got.Status != ScheduleActive || got.ThreadID != "" || !got.NextRunAt.Equal(row.NextRunAt) {
		t.Fatalf("atomic resume and move: %+v err=%v", got, err)
	}
	claimed, err = s.GetRun(run.ID)
	if err != nil || claimed.Kind != row.Kind {
		t.Fatalf("successful resume lost legacy ownership: %+v err=%v", claimed, err)
	}
}

func TestRetargetRejectsTerminalPauseAndResumeWithoutMutation(t *testing.T) {
	for _, status := range []string{ScheduleActive, SchedulePaused} {
		t.Run(status, func(t *testing.T) {
			s := openTestStore(t)
			row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: "old-target", Status: ScheduleDone, Prompt: "Continue the wait."})
			if _, err := s.PatchScheduleFields(row.ID, map[string]any{"kind": ScheduleStandalone, "status": status}, 1); err == nil {
				t.Fatal("terminal schedule resurrected by field edit")
			}
			got, err := s.GetSchedule(row.ID)
			if err != nil || got.Kind != row.Kind || got.Status != row.Status || !got.UpdatedAt.Equal(row.UpdatedAt) {
				t.Fatalf("rejected terminal edit mutated the row: %+v err=%v", got, err)
			}
		})
	}
}

func TestRetargetIgnoredUpdateRollsBackLegacyFreeze(t *testing.T) {
	s := openTestStore(t)
	row := mustSchedule(t, s, &Schedule{Kind: ScheduleStandalone, Prompt: "Continue the wait."})
	run := &ScheduleRun{ScheduleID: row.ID}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec("CREATE TRIGGER ignore_retarget BEFORE UPDATE ON schedules BEGIN SELECT RAISE(IGNORE); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(row.ID, map[string]any{"kind": ScheduleThread}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ignored update: %v", err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil || got.Kind != "" || !got.UpdatedAt.Equal(run.UpdatedAt) {
		t.Fatalf("ignored edit froze history without its destination: %+v err=%v", got, err)
	}
	if before, err := s.PatchScheduleFields("missing", nil, 1); err != nil || before != nil {
		t.Fatalf("empty patch: %+v err=%v", before, err)
	}
}

func TestPendingWakeListingReportsStorageFailures(t *testing.T) {
	for _, table := range []any{&Schedule{}, &ScheduleRun{}} {
		s := openTestStore(t)
		if err := s.DB().Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PendingWakeThreadIDs(); err == nil {
			t.Fatal("broken pending-wake lookup was reported as no owner")
		}
	}
}

func TestRetargetConcurrentResumeStaysUnderCap(t *testing.T) {
	s := openTestStore(t)
	var rows []*Schedule
	var runs []*ScheduleRun
	for range 2 {
		row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: "old-target", Status: SchedulePaused, Prompt: "Continue the wait."})
		run := &ScheduleRun{ScheduleID: row.ID, ThreadID: row.ThreadID}
		if err := s.CreateRun(run); err != nil {
			t.Fatal(err)
		}
		rows, runs = append(rows, row), append(runs, run)
	}
	start, results := make(chan struct{}), make(chan error, len(rows))
	for _, row := range rows {
		go func(id string) {
			<-start
			_, err := s.PatchScheduleFields(id, map[string]any{"kind": ScheduleStandalone, "thread_id": "", "status": ScheduleActive}, 1)
			results <- err
		}(row.ID)
	}
	close(start)
	accepted := 0
	for range rows {
		if err := <-results; err == nil {
			accepted++
		} else if !errors.Is(err, ErrScheduleCap) {
			t.Fatalf("concurrent resume: %v", err)
		}
	}
	if active, err := s.CountActive(); err != nil || active != 1 || accepted != 1 {
		t.Fatalf("cap exceeded: active=%d accepted=%d err=%v", active, accepted, err)
	}
	for i, row := range rows {
		current, err := s.GetSchedule(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.GetRun(runs[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == ScheduleActive {
			if current.Kind != ScheduleStandalone || current.ThreadID != "" || run.Kind != row.Kind {
				t.Fatalf("winning resume did not move and freeze together: %+v %+v", current, run)
			}
		} else if current.Kind != row.Kind || current.ThreadID != row.ThreadID || run.Kind != "" {
			t.Fatalf("capped resume left a partial retarget: %+v %+v", current, run)
		}
	}
}
