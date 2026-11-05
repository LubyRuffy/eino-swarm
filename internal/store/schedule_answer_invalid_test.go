package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyPendingAnswerMigrationPreservesItsUnavailableBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-answer.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	thread := &Thread{Title: "legacy answer migration"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	row := savedPendingAnswer(t, s, thread, turn, "legacy-schema")
	retry := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if err := s.SetScheduleAnswerRecovery(row.ID, 2, 2, 0, retry, false); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec("ALTER TABLE pending_schedule_answers DROP COLUMN classification_invalids").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.Attempts != 2 || got.ClassificationFailures != 2 || got.ClassificationInvalids != 0 || got.Payload != row.Payload || got.State != "pending" || !got.RetryAt.Equal(retry) {
		t.Fatalf("migration changed an existing private candidate: %+v %v", got, err)
	}
	if err := s.SetScheduleAnswerRecovery(row.ID, 3, 0, 1, retry, false); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.ClassificationFailures != 0 || got.ClassificationInvalids != 1 {
		t.Fatalf("new invalid counter was not writable after migration: %+v %v", got, err)
	}
}

// Both streaks belong to the private candidate and must survive rereads and
// legacy retry writes without exposing or replacing its unpublished bytes.
func TestInvalidClassifierBudgetPersistsIndependentlyOfRequestFailures(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "invalid-budget")
	retry := time.Now().UTC().Add(time.Minute)
	for _, step := range []struct {
		attempts, unavailable, invalid int
		deferred                       bool
	}{{1, 1, 0, false}, {2, 0, 1, false}, {3, 0, 2, false}, {4, 0, 3, true}} {
		if err := s.SetScheduleAnswerRecovery(row.ID, step.attempts, step.unavailable, step.invalid, retry, step.deferred); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePendingScheduleAnswer(&row); err != nil {
			t.Fatal(err)
		}
		if row.Attempts != step.attempts || row.ClassificationFailures != step.unavailable || row.ClassificationInvalids != step.invalid || (row.State == "deferred") != step.deferred {
			t.Fatalf("re-save lost private budgets: %+v", row)
		}
		assertNoAdmittedAnswer(t, s, thread.ID)
	}
	if err := s.SetScheduleAnswerRecovery(row.ID, 5, 0, 0, retry, false); !errors.Is(err, ErrScheduleAnswerChanged) {
		t.Fatalf("deferred invalid candidate regained ownership: %v", err)
	}
}

func TestInvalidClassifierBudgetRejectsInvalidAndStaleWrites(t *testing.T) {
	for _, mode := range []string{"negative", "excess", "same-attempt", "restore-known", "restore-idempotent", "legacy-retry", "save-negative", "save-excess"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "invalid-budget")
			retry := time.Now().UTC().Add(time.Minute)
			if err := s.SetScheduleAnswerRecovery(row.ID, 2, 0, 1, retry, false); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "negative":
				err = s.SetScheduleAnswerRecovery(row.ID, 3, 0, -1, retry, false)
			case "excess":
				err = s.SetScheduleAnswerRecovery(row.ID, 3, 0, 4, retry, false)
			case "same-attempt":
				err = s.SetScheduleAnswerRecovery(row.ID, 2, 0, 2, retry, false)
			case "restore-known":
				err = s.RestoreScheduleAnswerRecovery(row.ID, 2, 0, 2, retry, true)
			case "restore-idempotent":
				err = s.RestoreScheduleAnswerRecovery(row.ID, 2, 0, 1, retry, false)
			case "legacy-retry":
				err = s.SetScheduleAnswerRetry(row.ID, 3, retry)
			default:
				row.Attempts, row.ClassificationInvalids = 2, -1
				if mode == "save-excess" {
					row.ClassificationInvalids = 3
				}
				err = s.SavePendingScheduleAnswer(&row)
			}
			if mode == "legacy-retry" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrScheduleAnswerChanged) {
				t.Fatalf("invalid budget write accepted: %v", err)
			}
			got, getErr := s.GetPendingScheduleAnswer(row.ID)
			if getErr != nil || got.ClassificationInvalids != 1 || got.ClassificationFailures != 0 || got.State != "pending" {
				t.Fatalf("refusal lost original budget: %+v %v", got, getErr)
			}
		})
	}
}

func TestBudgetRecoveryDefersPersistedExhaustionWithoutAnotherAttempt(t *testing.T) {
	for _, mode := range []string{"invalid", "attempts", "attempts-without-streak"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "exhausted")
			attempts, invalids := 3, 3
			if mode != "invalid" {
				attempts, invalids = 6, 1
			}
			if mode == "attempts-without-streak" {
				invalids = 0
			}
			if err := s.SetScheduleAnswerRecovery(row.ID, attempts, 0, invalids, time.Time{}, false); err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreScheduleAnswerRecovery(row.ID, attempts, 0, invalids, time.Time{}, true); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetPendingScheduleAnswer(row.ID)
			if err != nil || got.State != "deferred" || got.Attempts != attempts || got.ClassificationInvalids != invalids || got.Payload != row.Payload {
				t.Fatalf("recovered exhaustion changed identity: %+v %v", got, err)
			}
		})
	}
}
