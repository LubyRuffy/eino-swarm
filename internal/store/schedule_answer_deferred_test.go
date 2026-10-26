package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassifierFailureBudgetAndDeferralSurviveReadbackWithoutPublication(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	retry := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	for _, step := range []struct {
		attempts int
		failures int
		deferred bool
	}{{1, 1, false}, {2, 0, false}, {3, 1, false}, {4, 2, false}, {5, 3, true}} {
		if err := s.SetScheduleAnswerRecovery(row.ID, step.attempts, step.failures, retry, step.deferred); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetPendingScheduleAnswer(row.ID)
		if err != nil || got.Attempts != step.attempts || got.ClassificationFailures != step.failures || !got.RetryAt.Equal(retry) || got.Payload != row.Payload {
			t.Fatalf("private classifier budget readback=%+v error=%v", got, err)
		}
		wantState := "pending"
		if step.deferred {
			wantState = "deferred"
		}
		if got.State != wantState {
			t.Fatalf("classifier decision=%s want=%s", got.State, wantState)
		}
		assertNoAdmittedAnswer(t, s, thread.ID)
	}
	if err := s.SavePendingScheduleAnswer(&row); err != nil {
		t.Fatal(err)
	}
	if row.State != "deferred" || row.ClassificationFailures != 3 || row.Attempts != 5 {
		t.Fatalf("re-saving reset durable deferral: %+v", row)
	}
	for _, retryCall := range []func() error{
		func() error { return s.SetScheduleAnswerRetry(row.ID, 6, retry) },
		func() error { return s.SetScheduleAnswerRecovery(row.ID, 6, 0, retry, false) },
	} {
		if err := retryCall(); !errors.Is(err, ErrScheduleAnswerChanged) {
			t.Fatalf("deferred candidate was revived: %v", err)
		}
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, row.ID); !errors.Is(err, ErrScheduleAnswerChanged) {
		t.Fatalf("deferred candidate admitted without new verification: %v", err)
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.State != "rejected" || got.Payload != row.Payload || got.ClassificationFailures != 3 {
		t.Fatalf("reject deferred candidate=%+v error=%v", got, err)
	}
}

func TestClassifierRecoveryRejectsStaleInvalidAndUnownedChanges(t *testing.T) {
	for _, mode := range []string{"idempotent", "legacy-preserves-failures", "negative-attempts", "negative-failures", "excess-failures", "empty-deferral", "same-attempt-failures", "same-attempt-deferral", "rewind", "retry-time", "accepted", "rejected", "missing", "ended", "stop", "read-failure", "owner-failure", "write-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			retry := time.Now().UTC().Add(time.Minute)
			if err := s.SetScheduleAnswerRecovery(row.ID, 2, 1, retry, false); err != nil {
				t.Fatal(err)
			}
			attempts, failures, at, deferred := 3, 2, retry, false
			want := error(ErrScheduleAnswerChanged)
			switch mode {
			case "idempotent":
				attempts, failures, want = 2, 1, nil
			case "legacy-preserves-failures":
				want = nil
			case "negative-attempts":
				attempts = -1
			case "negative-failures":
				failures = -1
			case "excess-failures":
				failures = 4
			case "empty-deferral":
				failures, deferred = 0, true
			case "same-attempt-failures":
				attempts = 2
			case "same-attempt-deferral":
				attempts, failures, deferred = 2, 1, true
			case "rewind":
				attempts, failures = 1, 0
			case "retry-time":
				attempts, failures, at = 2, 1, retry.Add(time.Second)
			case "accepted":
				if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, row.ID); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerAlreadyAdmitted
			case "rejected":
				if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
					t.Fatal(err)
				}
			case "missing":
				row.ID, want = "missing", ErrNotFound
			case "ended":
				if err := s.FinishTurn(turn.ID, TurnDone, "", ""); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerStopped
			case "read-failure":
				if err := s.DB().Migrator().DropTable(&PendingScheduleAnswer{}); err != nil {
					t.Fatal(err)
				}
			case "owner-failure":
				if err := s.DB().Migrator().DropTable(&Thread{}); err != nil {
					t.Fatal(err)
				}
			case "write-failure":
				if err := s.DB().Exec("CREATE TRIGGER refuse_classifier_recovery BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT RAISE(IGNORE); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if mode == "legacy-preserves-failures" {
				err = s.SetScheduleAnswerRetry(row.ID, attempts, at)
			} else {
				err = s.SetScheduleAnswerRecovery(row.ID, attempts, failures, at, deferred)
			}
			storageFailure := strings.HasSuffix(mode, "failure")
			if storageFailure {
				if err == nil || errors.Is(err, ErrScheduleAnswerChanged) || errors.Is(err, ErrNotFound) {
					t.Fatalf("storage failure misreported: %v", err)
				}
			} else if !errors.Is(err, want) {
				t.Fatalf("classifier recovery error=%v want=%v", err, want)
			}
			if mode != "missing" && mode != "read-failure" {
				got, getErr := s.GetPendingScheduleAnswer(row.ID)
				wantAttempts := 2
				if mode == "legacy-preserves-failures" {
					wantAttempts = 3
				}
				if getErr != nil || got.Attempts != wantAttempts || got.ClassificationFailures != 1 {
					t.Fatalf("refused mutation lost classifier budget=%+v error=%v", got, getErr)
				}
			}
		})
	}
}

func TestNewAnswerRetainsDeferredProseButOnlyOneRecoveryTranscript(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := PendingScheduleAnswer{ID: "deferred", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: `{"version":1,"answer_id":"deferred","text":["private original words"],"final":"private final","events":[{"text":"private event"}],"transcript":[{"content":"full recovery prefix"}]}`}
	if err := s.SavePendingScheduleAnswer(&row); err != nil {
		t.Fatal(err)
	}
	if err := s.SetScheduleAnswerRecovery(row.ID, 3, 3, time.Time{}, true); err != nil {
		t.Fatal(err)
	}
	latest, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || latest.Payload != row.Payload {
		t.Fatalf("latest deferred lost restart transcript=%+v error=%v", latest, err)
	}
	report := savedPendingAnswer(t, s, thread, turn, "report", "report")
	newer := savedPendingAnswer(t, s, thread, turn, "newer")
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.State != "deferred" || got.Attempts != 3 || got.ClassificationFailures != 3 {
		t.Fatalf("new work lost original deferred identity=%+v error=%v", got, err)
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.Payload), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got.Payload), &after); err != nil {
		t.Fatal(err)
	}
	delete(before, "transcript")
	beforeBytes, _ := json.Marshal(before)
	afterBytes, _ := json.Marshal(after)
	if string(beforeBytes) != string(afterBytes) || after["transcript"] != nil {
		t.Fatalf("deferred compaction changed private candidate: %s", got.Payload)
	}
	for _, kept := range []PendingScheduleAnswer{report, newer} {
		read, err := s.GetPendingScheduleAnswer(kept.ID)
		if err != nil || read.Payload != kept.Payload {
			t.Fatalf("deferred compaction changed another payload=%+v error=%v", read, err)
		}
	}
	// Repeated newer work can compact the same candidate without reviving it.
	savedPendingAnswer(t, s, thread, turn, "newest")
	again, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || again.Payload != got.Payload || again.State != "deferred" {
		t.Fatalf("repeated compaction lost deferred content=%+v error=%v", again, err)
	}
	assertNoAdmittedAnswer(t, s, thread.ID)
}

func TestFailedDeferredCompactionPreservesTheOnlyRecoveryTranscript(t *testing.T) {
	for _, mode := range []string{"malformed", "null", "array", "write-error", "ignored-write"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "original")
			payload := `{"text":["private"],"transcript":[{"content":"original recovery"}]}`
			switch mode {
			case "malformed":
				payload = "{"
			case "null":
				payload = "null"
			case "array":
				payload = "[]"
			}
			if err := s.DB().Model(&row).Update("payload", payload).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.SetScheduleAnswerRecovery(row.ID, 3, 3, time.Time{}, true); err != nil {
				t.Fatal(err)
			}
			if mode == "write-error" || mode == "ignored-write" {
				refusal := "RAISE(ABORT,'fixture compaction refusal')"
				if mode == "ignored-write" {
					refusal = "RAISE(IGNORE)"
				}
				if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER refuse_deferred_compaction BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT %s; END", refusal)).Error; err != nil {
					t.Fatal(err)
				}
			}
			newer := PendingScheduleAnswer{ID: "newer", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: "new recovery"}
			if err := s.SavePendingScheduleAnswer(&newer); err == nil {
				t.Fatal("incomplete deferred compaction admitted new work")
			}
			if _, err := s.GetPendingScheduleAnswer(newer.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed compaction retained new candidate: %v", err)
			}
			got, err := s.GetPendingScheduleAnswer(row.ID)
			if err != nil || got.State != "deferred" || got.Payload != payload || got.ClassificationFailures != 3 {
				t.Fatalf("failed compaction changed original recovery=%+v error=%v", got, err)
			}
		})
	}
}

func TestDeferredCandidateCleanupCommitsWithTerminalTurnAndDeletion(t *testing.T) {
	for _, action := range []string{"done", "cancel", "terminal-failure", "delete"} {
		t.Run(action, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			if err := s.SetScheduleAnswerRecovery(row.ID, 3, 3, time.Time{}, true); err != nil {
				t.Fatal(err)
			}
			if action == "delete" {
				if err := s.DeleteThread(thread.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.GetPendingScheduleAnswer(row.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("deleted owner retained deferred candidate: %v", err)
				}
				return
			}
			if action == "terminal-failure" {
				if err := s.DB().Exec("CREATE TRIGGER refuse_deferred_terminal BEFORE UPDATE ON pending_schedule_answers BEGIN SELECT RAISE(IGNORE); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			status := TurnDone
			if action == "cancel" {
				status = TurnCancelled
			}
			err := s.FinishTurn(turn.ID, status, "", "")
			got, getErr := s.GetPendingScheduleAnswer(row.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if action == "terminal-failure" {
				owner, ownerErr := s.GetTurn(turn.ID)
				if err == nil || got.State != "deferred" || got.Payload != row.Payload || ownerErr != nil || owner.Status != TurnRunning {
					t.Fatalf("failed terminal discarded deferred recovery=%+v owner=%+v error=%v/%v", got, owner, err, ownerErr)
				}
			} else if err != nil || got.State != "rejected" || got.Payload != "" || got.ClassificationFailures != 3 {
				t.Fatalf("terminal left private deferred payload=%+v error=%v", got, err)
			}
		})
	}
}

func TestLegacyClassifierRecoveryRestoresOnlyTheAlreadyConsumedBudget(t *testing.T) {
	for _, failures := range []int{1, 3} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "legacy")
			retry := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
			if err := s.SetScheduleAnswerRetry(row.ID, 3, retry); err != nil {
				t.Fatal(err)
			}
			if err := s.RestoreScheduleAnswerRecovery(row.ID, 3, failures, retry, failures == 3); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetPendingScheduleAnswer(row.ID)
			wantState := "pending"
			if failures == 3 {
				wantState = "deferred"
			}
			if err != nil || got.Attempts != 3 || got.ClassificationFailures != failures || got.State != wantState || !got.RetryAt.Equal(retry) || got.Payload != row.Payload {
				t.Fatalf("legacy budget recovery changed consumed attempts or candidate: %+v %v", got, err)
			}
			assertNoAdmittedAnswer(t, s, thread.ID)
		})
	}
}

func TestLegacyClassifierRecoveryCannotReplaceKnownOrChangedState(t *testing.T) {
	for _, mode := range []string{"known-failures", "new-attempt", "older-attempt", "empty-failures", "negative-failures", "excess-failures", "changed-time", "zero-time", "accepted", "deferred", "rejected", "missing", "stop", "ended"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "legacy")
			retry := time.Now().UTC().Add(time.Minute)
			if mode == "zero-time" {
				retry = time.Time{}
			}
			if err := s.SetScheduleAnswerRetry(row.ID, 3, retry); err != nil {
				t.Fatal(err)
			}
			attempts, failures, at := 3, 3, retry
			want := error(ErrScheduleAnswerChanged)
			switch mode {
			case "known-failures":
				if err := s.SetScheduleAnswerRecovery(row.ID, 4, 1, retry, false); err != nil {
					t.Fatal(err)
				}
				attempts = 4
			case "new-attempt":
				attempts = 4
			case "older-attempt":
				attempts = 2
			case "empty-failures":
				failures = 0
			case "negative-failures":
				failures = -1
			case "excess-failures":
				failures = 4
			case "changed-time", "zero-time":
				at = retry.Add(time.Second)
			case "accepted":
				if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, row.ID); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerAlreadyAdmitted
			case "deferred":
				if err := s.RestoreScheduleAnswerRecovery(row.ID, 3, 3, retry, true); err != nil {
					t.Fatal(err)
				}
			case "rejected":
				if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
					t.Fatal(err)
				}
			case "missing":
				row.ID, want = "missing", ErrNotFound
			case "stop":
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerStopped
			case "ended":
				if err := s.FinishTurn(turn.ID, TurnDone, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			before, beforeErr := s.GetPendingScheduleAnswer(row.ID)
			err := s.RestoreScheduleAnswerRecovery(row.ID, attempts, failures, at, true)
			if !errors.Is(err, want) {
				t.Fatalf("legacy budget refusal=%v want=%v", err, want)
			}
			if mode != "missing" {
				after, afterErr := s.GetPendingScheduleAnswer(row.ID)
				if beforeErr != nil || afterErr != nil || *before != *after {
					t.Fatalf("refused legacy budget changed candidate: before=%+v after=%+v errors=%v/%v", before, after, beforeErr, afterErr)
				}
			}
		})
	}
}
