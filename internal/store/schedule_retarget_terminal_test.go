package store

import (
	"reflect"
	"testing"
	"time"
)

// Transactional validation is authoritative even when an engine caller saw an
// active wait before a concurrent cancel or one-shot claim committed.
func TestRetargetChecksActualTerminalStateBeforeFreezingHistory(t *testing.T) {
	for _, status := range []string{ScheduleActive, SchedulePaused, ScheduleDone, ScheduleCancelled} {
		t.Run(status, func(t *testing.T) {
			s := open(t)
			thread := mustThread(t, s)
			row := mustSchedule(t, s, &Schedule{
				Kind: ScheduleThread, ThreadID: thread.ID, Status: status,
				Title: "Original destination", Prompt: "Inspect outstanding work.",
				EveryS: 60, NextRunAt: time.Now().UTC().Add(time.Hour),
			})
			legacy := &ScheduleRun{ScheduleID: row.ID, ThreadID: thread.ID, Status: ScheduleRunFindings}
			if err := s.CreateRun(legacy); err != nil {
				t.Fatal(err)
			}
			before, err := s.GetSchedule(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := s.PatchScheduleFields(row.ID, map[string]any{
				"kind": ScheduleStandalone, "thread_id": "", "title": "Stale edit",
				"prompt": "Inspect revised work.", "every_s": 120,
			}, 10)
			terminal := status == ScheduleDone || status == ScheduleCancelled
			if terminal && (err == nil || previous != nil) {
				t.Errorf("terminal destination edit was accepted: previous=%+v err=%v", previous, err)
			}
			if !terminal && (err != nil || !reflect.DeepEqual(previous, before)) {
				t.Fatalf("active/paused destination edit was refused: previous=%+v err=%v", previous, err)
			}
			got, readErr := s.GetSchedule(row.ID)
			run, runErr := s.GetRun(legacy.ID)
			if readErr != nil || runErr != nil {
				t.Fatalf("read destination/history: %v %v", readErr, runErr)
			}
			if terminal {
				if !reflect.DeepEqual(got, before) || run.Kind != "" || !run.UpdatedAt.Equal(legacy.UpdatedAt) {
					t.Fatalf("terminal refusal partially changed schedule or history: got=%+v before=%+v run=%+v", got, before, run)
				}
			} else if got.Kind != ScheduleStandalone || got.ThreadID != "" || got.Status != status || got.Title != "Stale edit" || got.EveryS != 120 || run.Kind != ScheduleThread || !run.UpdatedAt.Equal(legacy.UpdatedAt) {
				t.Fatalf("normal retarget did not preserve lifecycle and freeze history: got=%+v run=%+v", got, run)
			}
		})
	}
}
