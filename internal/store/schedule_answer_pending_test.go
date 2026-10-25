package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func savedPendingAnswer(t *testing.T, s *Store, thread *Thread, turn *Turn, id string, kind ...string) PendingScheduleAnswer {
	t.Helper()
	row := PendingScheduleAnswer{ID: id, ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: `{"unverified":"private candidate"}`}
	if len(kind) > 0 {
		row.Kind = kind[0]
	}
	if err := s.SavePendingScheduleAnswer(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPendingScheduleAnswerRemainsPrivateAndImmutable(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "")
	if row.ID == "" || row.State != "pending" || row.CreatedAt.IsZero() {
		t.Fatalf("pending identity was not initialized: %+v", row)
	}
	assertNoAdmittedAnswer(t, s, thread.ID)
	retry := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if err := s.SetScheduleAnswerRetry(row.ID, 2, retry); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePendingScheduleAnswer(&row); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.Payload != row.Payload || got.Attempts != 2 || !got.RetryAt.Equal(retry) {
		t.Fatalf("re-saving replaced retry state: %+v %v", got, err)
	}
	for _, field := range []string{"payload", "kind", "thread", "turn"} {
		changed := row
		switch field {
		case "payload":
			changed.Payload += "changed"
		case "kind":
			changed.Kind = "report"
		case "thread":
			changed.ThreadID = "other"
		case "turn":
			changed.TurnID = "other"
		}
		if err := s.SavePendingScheduleAnswer(&changed); !errors.Is(err, ErrScheduleAnswerChanged) {
			t.Fatalf("changed %s replaced candidate identity: %v", field, err)
		}
	}
	got, _ = s.GetPendingScheduleAnswer(row.ID)
	if got.Payload != row.Payload {
		t.Fatal("refused replacement changed the private payload")
	}
}

func TestPendingScheduleAnswerSaveRequiresALiveUnstoppedOwner(t *testing.T) {
	for _, mode := range []string{"missing-thread", "missing-turn", "ended", "stop", "invalid-kind", "invalid-state", "empty-payload", "negative-attempts", "nil"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := &PendingScheduleAnswer{ID: "candidate", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: "private"}
			want := ErrScheduleAnswerChanged
			switch mode {
			case "missing-thread":
				row.ThreadID = "missing"
				want = ErrNotFound
			case "missing-turn":
				row.TurnID = "missing"
				want = ErrNotFound
			case "ended":
				if err := s.FinishTurn(turn.ID, TurnError, "", ""); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerStopped
			case "invalid-kind":
				row.Kind = "tool"
			case "invalid-state":
				row.State = "accepted"
			case "empty-payload":
				row.Payload = ""
			case "negative-attempts":
				row.Attempts = -1
			case "nil":
				row = nil
			}
			if err := s.SavePendingScheduleAnswer(row); !errors.Is(err, want) {
				t.Fatalf("invalid candidate save: error=%v want=%v", err, want)
			}
			var count int64
			if err := s.DB().Model(&PendingScheduleAnswer{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed save retained private state: count=%d error=%v", count, err)
			}
		})
	}
}

func TestPendingScheduleAnswerListIncludesEveryDecisionInStableOrder(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	for _, id := range []string{"b", "a", "c"} {
		row := PendingScheduleAnswer{ID: id, ThreadID: thread.ID, TurnID: turn.ID, Kind: "report", Payload: "private " + id, CreatedAt: at}
		if id == "c" {
			row.CreatedAt = at.Add(time.Second)
		}
		if err := s.SavePendingScheduleAnswer(&row); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer(), "c"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListPendingScheduleAnswers(turn.ID)
	if err != nil || len(rows) != 3 || rows[0].ID != "b" || rows[1].ID != "a" || rows[2].ID != "c" || rows[0].State != "rejected" || rows[1].State != "pending" || rows[2].State != "accepted" {
		t.Fatalf("private decision history lost ordering or terminal rows: %+v %v", rows, err)
	}
	empty, err := s.ListPendingScheduleAnswers("missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty private list: %+v %v", empty, err)
	}
}

func TestPendingScheduleAnswerRetryCannotReviveOrRewindACandidate(t *testing.T) {
	for _, mode := range []string{"pending", "rewind", "negative", "accepted", "rejected", "ended", "stop", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			retry := time.Now().UTC().Add(time.Minute)
			if err := s.SetScheduleAnswerRetry(row.ID, 2, retry); err != nil {
				t.Fatal(err)
			}
			attempts, want := 3, error(nil)
			switch mode {
			case "rewind":
				attempts, want = 1, ErrScheduleAnswerChanged
			case "negative":
				attempts, want = -1, ErrScheduleAnswerChanged
			case "accepted":
				if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer(), row.ID); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerAlreadyAdmitted
			case "rejected":
				if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerChanged
			case "ended":
				if err := s.FinishTurn(turn.ID, TurnError, "", ""); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerChanged
			case "stop":
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerStopped
			case "missing":
				row.ID, want = "missing", ErrNotFound
			}
			err := s.SetScheduleAnswerRetry(row.ID, attempts, retry.Add(time.Minute))
			if !errors.Is(err, want) {
				t.Fatalf("retry transition error=%v want=%v", err, want)
			}
			if mode != "missing" {
				got, err := s.GetPendingScheduleAnswer(row.ID)
				if err != nil {
					t.Fatal(err)
				}
				if want == nil && got.Attempts != 3 || want != nil && got.Attempts != 2 {
					t.Fatalf("retry changed refused state: %+v", got)
				}
			}
		})
	}
}

func TestPendingScheduleAnswerRejectionIsOwnedAndTerminal(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	if err := s.RejectPendingScheduleAnswer("other", turn.ID, row.ID); !errors.Is(err, ErrScheduleAnswerChanged) {
		t.Fatalf("wrong owner rejected: %v", err)
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, "other", row.ID); !errors.Is(err, ErrScheduleAnswerChanged) {
		t.Fatalf("wrong turn rejected: %v", err)
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing candidate rejected: %v", err)
	}
	if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.GetPendingScheduleAnswer(row.ID)
	if got.State != "rejected" || got.Payload != row.Payload {
		t.Fatalf("rejection lost private identity: %+v", got)
	}
}

func TestPendingScheduleAnswerAdmissionCommitsDecisionAndPublicRowsTogether(t *testing.T) {
	for _, fail := range []string{"", "events", "messages", "pending_schedule_answers"} {
		t.Run(fail, func(t *testing.T) {
			s, thread, turn, wake := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			if fail != "" {
				op := "INSERT"
				if fail == "pending_schedule_answers" {
					op = "UPDATE"
				}
				if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER refuse_pending_admission BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'fixture decision failure'); END", op, fail)).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer(), row.ID)
			got, getErr := s.GetPendingScheduleAnswer(row.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if fail != "" {
				if err == nil || got.State != "pending" {
					t.Fatalf("partial decision committed: %+v %v", got, err)
				}
				assertNoAdmittedAnswer(t, s, thread.ID)
				return
			}
			if err != nil || got.State != "accepted" {
				t.Fatalf("admission was not finalized: %+v %v", got, err)
			}
			if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer(), row.ID); !errors.Is(err, ErrScheduleAnswerAlreadyAdmitted) {
				t.Fatalf("duplicate admission could broadcast again: %v", err)
			}
			if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); !errors.Is(err, ErrScheduleAnswerAlreadyAdmitted) {
				t.Fatalf("accepted candidate was rejected: %v", err)
			}
			events, _ := s.ListTurnEvents(turn.ID)
			msgs, _ := s.ListMessages(thread.ID)
			if len(events) != 1 || len(msgs) != 1 {
				t.Fatalf("duplicate public answer: events=%+v messages=%+v", events, msgs)
			}
		})
	}
}

func TestPendingScheduleAnswerAdmissionRejectsMissingWrongOrSettledCandidates(t *testing.T) {
	for _, mode := range []string{"missing", "wrong-thread", "wrong-turn", "rejected", "mixed", "stop", "changed", "duplicate-id"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, wake := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			ids, owner, tid, events := []string{row.ID}, thread.ID, turn.ID, admissionAnswer()
			want := ErrScheduleAnswerChanged
			switch mode {
			case "missing":
				ids, want = []string{"missing"}, ErrNotFound
			case "wrong-thread":
				owner = "other"
			case "wrong-turn":
				tid = "other"
			case "rejected":
				if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); err != nil {
					t.Fatal(err)
				}
			case "mixed":
				accepted := savedPendingAnswer(t, s, thread, turn, "accepted")
				if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer(), accepted.ID); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, accepted.ID)
			case "stop":
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested"}); err != nil {
					t.Fatal(err)
				}
				want = ErrScheduleAnswerStopped
			case "changed":
				if err := s.DB().Model(&Schedule{}).Where("id = ?", wake.ID).Update("status", ScheduleCancelled).Error; err != nil {
					t.Fatal(err)
				}
			case "duplicate-id":
				ids = append(ids, row.ID)
			}
			if err := s.AdmitScheduleAnswer(owner, tid, []Schedule{wake}, events, ids...); !errors.Is(err, want) {
				t.Fatalf("invalid admission error=%v want=%v", err, want)
			}
			got, _ := s.GetPendingScheduleAnswer(row.ID)
			if got.State == "accepted" {
				t.Fatalf("refused candidate accepted: %+v", got)
			}
		})
	}
}

func TestPendingScheduleAnswerWithoutPublicProseStillCommitsItsAdmission(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, row.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.State != "accepted" {
		t.Fatalf("empty answer did not settle its private identity: %+v %v", got, err)
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, row.ID); !errors.Is(err, ErrScheduleAnswerAlreadyAdmitted) {
		t.Fatalf("empty admitted answer was not idempotent: %v", err)
	}
	assertNoAdmittedAnswer(t, s, thread.ID)
}

func TestPendingScheduleAnswerOwnerReadFailuresCannotAdvanceRetry(t *testing.T) {
	for _, table := range []string{"threads", "turns", "events"} {
		t.Run(table, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			if err := s.DB().Exec("DROP TABLE " + table).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.SetScheduleAnswerRetry(row.ID, 1, time.Now()); err == nil || errors.Is(err, ErrNotFound) {
				t.Fatalf("broken owner evidence became a retry or missing record: %v", err)
			}
			got, err := s.GetPendingScheduleAnswer(row.ID)
			if err != nil || got.Attempts != 0 || !got.RetryAt.IsZero() {
				t.Fatalf("failed evidence read changed retry state: %+v %v", got, err)
			}
		})
	}
}

func TestPendingScheduleAnswerCorruptDecisionCannotBeRejected(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	if err := s.DB().Model(&row).Update("state", "unknown").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID); !errors.Is(err, ErrScheduleAnswerChanged) {
		t.Fatalf("corrupt decision became a known rejection: %v", err)
	}
}

func TestPendingScheduleAnswerDeletionAndRewindFollowTheirOwners(t *testing.T) {
	for _, mode := range []string{"project", "rewind", "thread-rollback", "project-rollback", "rewind-rollback"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			project := &Project{Name: "private answers"}
			if err := s.CreateProject(project); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateThread(thread.ID, map[string]any{"project_id": project.ID}); err != nil {
				t.Fatal(err)
			}
			first := savedPendingAnswer(t, s, thread, turn, "first")
			laterTurn := &Turn{ThreadID: thread.ID}
			if err := s.CreateTurn(laterTurn); err != nil {
				t.Fatal(err)
			}
			later := savedPendingAnswer(t, s, thread, laterTurn, "later")
			ev := Event{ThreadID: thread.ID, TurnID: laterTurn.ID, Kind: "user_message", Text: "later input"}
			if err := s.AppendEvent(&ev); err != nil {
				t.Fatal(err)
			}
			rollback := strings.HasSuffix(mode, "-rollback")
			if rollback {
				if err := s.DB().Exec("CREATE TRIGGER refuse_pending_delete BEFORE DELETE ON pending_schedule_answers BEGIN SELECT RAISE(ABORT,'fixture private delete failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			var err error
			switch mode {
			case "project", "project-rollback":
				err = s.DeleteProject(project.ID)
			case "rewind", "rewind-rollback":
				err = s.TruncateFromEventSeq(thread.ID, ev.Seq)
			case "thread-rollback":
				err = s.DeleteThread(thread.ID)
			}
			if (err != nil) != rollback {
				t.Fatalf("owner deletion error=%v rollback=%v", err, rollback)
			}
			_, laterErr := s.GetPendingScheduleAnswer(later.ID)
			if rollback {
				if laterErr != nil {
					t.Fatalf("failed deletion lost private candidate: %v", laterErr)
				}
				if _, err := s.GetTurn(laterTurn.ID); err != nil {
					t.Fatalf("failed deletion lost owning turn: %v", err)
				}
				return
			}
			if !errors.Is(laterErr, ErrNotFound) {
				t.Fatalf("deleted owner retained private candidate: %v", laterErr)
			}
			_, firstErr := s.GetPendingScheduleAnswer(first.ID)
			if mode == "rewind" && firstErr != nil || mode == "project" && !errors.Is(firstErr, ErrNotFound) {
				t.Fatalf("owner scope removed wrong candidate: %v", firstErr)
			}
			if err := s.SavePendingScheduleAnswer(&later); !errors.Is(err, ErrNotFound) {
				t.Fatalf("late write recreated deleted candidate: %v", err)
			}
		})
	}
}

func TestConcurrentPendingScheduleAnswerAdmissionPublishesOnce(t *testing.T) {
	s, thread, turn, wake := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer(), row.ID)
		}()
	}
	wg.Wait()
	close(results)
	var admitted, duplicate int
	for err := range results {
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, ErrScheduleAnswerAlreadyAdmitted):
			duplicate++
		default:
			t.Fatal(err)
		}
	}
	events, _ := s.ListTurnEvents(turn.ID)
	if admitted != 1 || duplicate != 1 || len(events) != 1 {
		t.Fatalf("concurrent publication admitted=%d duplicate=%d events=%+v", admitted, duplicate, events)
	}
}

func TestDeletedThreadRemovesPrivateCandidatesAndCannotBeRecreatedByLateWrites(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	accepted := savedPendingAnswer(t, s, thread, turn, "accepted", "report")
	rejected := savedPendingAnswer(t, s, thread, turn, "rejected", "report")
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, accepted.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, rejected.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{row.ID, accepted.ID, rejected.ID} {
		if _, err := s.GetPendingScheduleAnswer(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted private payload retained for %s: %v", id, err)
		}
	}
	if err := s.SavePendingScheduleAnswer(&row); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late save recreated deleted state: %v", err)
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer(), row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late admission recreated deleted state: %v", err)
	}
	assertNoAdmittedAnswer(t, s, thread.ID)
}

func TestPendingScheduleAnswerStoreFailuresRemainErrors(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"save":             func() error { return s.SavePendingScheduleAnswer(&row) },
		"get":              func() error { _, err := s.GetPendingScheduleAnswer(row.ID); return err },
		"list":             func() error { _, err := s.ListPendingScheduleAnswers(turn.ID); return err },
		"recovery-reports": func() error { _, err := s.ListScheduleReportsForRecovery(thread.ID); return err },
		"retry":            func() error { return s.SetScheduleAnswerRetry(row.ID, 1, time.Now()) },
		"reject":           func() error { return s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID) },
	} {
		if err := call(); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("closed store %s misreported: %v", name, err)
		}
	}
}

func TestFinishTurnDropsPrivateCandidateProseOnlyWithItsTerminalCommit(t *testing.T) {
	for _, status := range []string{TurnDone, TurnError, TurnCancelled} {
		for _, fail := range []string{"", "turn", "payload"} {
			t.Run(status+"/"+fail, func(t *testing.T) {
				s, thread, turn, _ := answerAdmissionFixture(t)
				pending := savedPendingAnswer(t, s, thread, turn, "pending", "report")
				accepted := savedPendingAnswer(t, s, thread, turn, "accepted", "report")
				rejected := savedPendingAnswer(t, s, thread, turn, "rejected", "report")
				if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, accepted.ID); err != nil {
					t.Fatal(err)
				}
				if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, rejected.ID); err != nil {
					t.Fatal(err)
				}
				if err := s.SetScheduleAnswerRetry(pending.ID, 2, time.Now().UTC().Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
				otherTurn := &Turn{ThreadID: thread.ID}
				if err := s.CreateTurn(otherTurn); err != nil {
					t.Fatal(err)
				}
				other := savedPendingAnswer(t, s, thread, otherTurn, "other")
				if fail != "" {
					table := "turns"
					if fail == "payload" {
						table = "pending_schedule_answers"
					}
					if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER refuse_terminal_candidate_clear BEFORE UPDATE ON %s BEGIN SELECT RAISE(ABORT,'fixture terminal write failure'); END", table)).Error; err != nil {
						t.Fatal(err)
					}
				}
				err := s.FinishTurn(turn.ID, status, "terminal answer", "terminal cause")
				gotTurn, getErr := s.GetTurn(turn.ID)
				if getErr != nil {
					t.Fatal(getErr)
				}
				if fail != "" {
					if err == nil || gotTurn.Status != TurnRunning || gotTurn.EndedAt != nil || gotTurn.Final != "" {
						t.Fatalf("failed terminal write changed the turn: %+v %v", gotTurn, err)
					}
				} else if err != nil || gotTurn.Status != status || gotTurn.EndedAt == nil {
					t.Fatalf("terminal status did not commit: %+v %v", gotTurn, err)
				}
				for _, candidate := range []PendingScheduleAnswer{pending, accepted, rejected} {
					got, err := s.GetPendingScheduleAnswer(candidate.ID)
					if err != nil {
						t.Fatal(err)
					}
					wantState := candidate.ID
					if fail != "" && candidate.ID == "pending" {
						wantState = "pending"
					} else if candidate.ID == "pending" {
						wantState = "rejected"
					}
					if got.State != wantState || fail == "" && got.Payload != "" || fail != "" && got.Payload != candidate.Payload {
						t.Fatalf("private recovery/terminal receipt mismatch: got=%+v fail=%s", got, fail)
					}
					if got.ID == pending.ID && got.Attempts != 2 {
						t.Fatalf("terminal clearing lost retry diagnostics: %+v", got)
					}
				}
				gotOther, err := s.GetPendingScheduleAnswer(other.ID)
				if err != nil || gotOther.State != "pending" || gotOther.Payload != other.Payload {
					t.Fatalf("terminal cleanup changed another turn: %+v %v", gotOther, err)
				}
			})
		}
	}
}

func TestRefusedTerminalTurnUpdateRetainsItsCandidateForRecovery(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	row := savedPendingAnswer(t, s, thread, turn, "candidate")
	if err := s.DB().Exec("CREATE TRIGGER ignore_terminal_turn BEFORE UPDATE ON turns BEGIN SELECT RAISE(IGNORE); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(turn.ID, TurnDone, "answer", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused terminal row update reported success: %v", err)
	}
	got, err := s.GetPendingScheduleAnswer(row.ID)
	if err != nil || got.Payload != row.Payload || got.State != "pending" {
		t.Fatalf("refused turn update discarded private recovery state: %+v %v", got, err)
	}
}

func TestIgnoredScheduleAnswerWritesCannotPretendTheyCommitted(t *testing.T) {
	for _, mode := range []string{"save", "retry", "reject", "answer-events", "answer-replay", "answer-decision", "terminal-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			row := savedPendingAnswer(t, s, thread, turn, "candidate")
			table, operation := "pending_schedule_answers", "UPDATE"
			switch mode {
			case "save":
				operation = "INSERT"
			case "answer-events":
				table, operation = "events", "INSERT"
			case "answer-replay":
				table, operation = "messages", "INSERT"
			}
			if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER ignore_schedule_answer_write BEFORE %s ON %s BEGIN SELECT RAISE(IGNORE); END", operation, table)).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "save":
				other := row
				other.ID = "ignored"
				err = s.SavePendingScheduleAnswer(&other)
			case "retry":
				err = s.SetScheduleAnswerRetry(row.ID, 1, time.Now())
			case "reject":
				err = s.RejectPendingScheduleAnswer(thread.ID, turn.ID, row.ID)
			case "terminal-cleanup":
				err = s.FinishTurn(turn.ID, TurnDone, "answer", "")
			default:
				err = s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer(), row.ID)
			}
			if err == nil || errors.Is(err, ErrScheduleAnswerChanged) {
				t.Fatalf("ignored write was not a storage failure: %v", err)
			}
			got, err := s.GetPendingScheduleAnswer(row.ID)
			if err != nil || got.State != "pending" || got.Payload != row.Payload || got.Attempts != 0 {
				t.Fatalf("ignored write changed private state: %+v %v", got, err)
			}
			gotTurn, err := s.GetTurn(turn.ID)
			if err != nil || gotTurn.Status != TurnRunning {
				t.Fatalf("ignored write committed terminal state: %+v %v", gotTurn, err)
			}
			assertNoAdmittedAnswer(t, s, thread.ID)
		})
	}
}

func TestUnreadablePrivateCandidatesCannotCommitATerminalTurn(t *testing.T) {
	s, _, turn, _ := answerAdmissionFixture(t)
	if err := s.DB().Migrator().DropTable(&PendingScheduleAnswer{}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(turn.ID, TurnDone, "answer", ""); err == nil {
		t.Fatal("unreadable private state did not block terminal persistence")
	}
	got, err := s.GetTurn(turn.ID)
	if err != nil || got.Status != TurnRunning || got.EndedAt != nil {
		t.Fatalf("failed private-state read committed a terminal turn: %+v %v", got, err)
	}
}

func TestNewAnswerKeepsOnlyTheLatestTranscriptAndPreservesReportReceipts(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	accepted := savedPendingAnswer(t, s, thread, turn, "accepted")
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, accepted.ID); err != nil {
		t.Fatal(err)
	}
	pending := savedPendingAnswer(t, s, thread, turn, "older")
	for _, id := range []string{"report-pending", "report-accepted"} {
		report := savedPendingAnswer(t, s, thread, turn, id, "report")
		if id == "report-accepted" {
			if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, report.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	otherTurn := &Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(otherTurn); err != nil {
		t.Fatal(err)
	}
	other := savedPendingAnswer(t, s, thread, otherTurn, "other")
	latest := savedPendingAnswer(t, s, thread, turn, "latest")
	for id, wantState := range map[string]string{accepted.ID: "accepted", pending.ID: "rejected"} {
		got, err := s.GetPendingScheduleAnswer(id)
		if err != nil || got.State != wantState || got.Payload != "" {
			t.Fatalf("obsolete answer retained its transcript or changed decision: %+v %v", got, err)
		}
	}
	for _, id := range []string{latest.ID, other.ID, "report-pending", "report-accepted"} {
		got, err := s.GetPendingScheduleAnswer(id)
		if err != nil || got.Payload == "" {
			t.Fatalf("current answer or report receipt was cleared: %+v %v", got, err)
		}
	}
	backdated := PendingScheduleAnswer{ID: "backdated", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: "current input after a clock rollback", CreatedAt: accepted.CreatedAt.Add(-time.Second), Ordinal: 9000}
	if err := s.SavePendingScheduleAnswer(&backdated); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingScheduleAnswer(backdated.ID)
	if err != nil || got.Payload != backdated.Payload || got.Ordinal != latest.Ordinal+1 || got.Ordinal == 9000 {
		t.Fatalf("a clock rollback or caller ordinal changed insertion identity: %+v %v", got, err)
	}
	sameTime := PendingScheduleAnswer{ID: "aaa-same-time", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: "the newest input", CreatedAt: backdated.CreatedAt, Ordinal: 1}
	if err := s.SavePendingScheduleAnswer(&sameTime); err != nil {
		t.Fatal(err)
	}
	if sameTime.Ordinal != backdated.Ordinal+1 {
		t.Fatalf("same-time candidate did not receive its actual insertion ordinal: %+v", sameTime)
	}
	rows, err := s.ListPendingScheduleAnswers(turn.ID)
	if err != nil || rows[len(rows)-1].ID != sameTime.ID {
		t.Fatalf("clock/ID ordering hid the newest answer: %+v %v", rows, err)
	}
	previous, err := s.GetPendingScheduleAnswer(backdated.ID)
	if err != nil || previous.Payload != "" || previous.State != "rejected" {
		t.Fatalf("a newer insertion retained the old full transcript: %+v %v", previous, err)
	}
	if err := s.SavePendingScheduleAnswer(previous); err != nil {
		t.Fatal(err)
	}
	if previous.Ordinal != backdated.Ordinal {
		t.Fatalf("re-saving an identity minted another ordinal: %+v", previous)
	}
	current, err := s.GetPendingScheduleAnswer(sameTime.ID)
	if err != nil || current.Payload != sameTime.Payload {
		t.Fatalf("re-saving old identity cleared the current recovery payload: %+v %v", current, err)
	}
}

func TestFailedNewAnswerSavePreservesThePreviousRecoveryTranscript(t *testing.T) {
	for _, fail := range []string{"insert", "prune", "ignored-prune"} {
		t.Run(fail, func(t *testing.T) {
			s, thread, turn, _ := answerAdmissionFixture(t)
			old := savedPendingAnswer(t, s, thread, turn, "original")
			op, refusal := "INSERT", "RAISE(ABORT,'fixture candidate failure')"
			if fail != "insert" {
				op = "UPDATE"
			}
			if fail == "ignored-prune" {
				refusal = "RAISE(IGNORE)"
			}
			if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER refuse_new_answer BEFORE %s ON pending_schedule_answers BEGIN SELECT %s; END", op, refusal)).Error; err != nil {
				t.Fatal(err)
			}
			newer := PendingScheduleAnswer{ID: "newer", ThreadID: thread.ID, TurnID: turn.ID, Kind: "answer", Payload: "new full transcript"}
			if err := s.SavePendingScheduleAnswer(&newer); err == nil {
				t.Fatal("failed candidate transaction reported success")
			}
			if _, err := s.GetPendingScheduleAnswer(newer.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed save retained its new candidate: %v", err)
			}
			got, err := s.GetPendingScheduleAnswer(old.ID)
			if err != nil || got.Payload != old.Payload || got.State != "pending" {
				t.Fatalf("failed save destroyed the recoverable old candidate: %+v %v", got, err)
			}
		})
	}
}

func TestRecoveryReportReadKeepsAllLiveReceiptsWithoutLoadingAnswerTranscripts(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	savedPendingAnswer(t, s, thread, turn, "full-answer")
	for _, id := range []string{"pending-report", "accepted-report", "rejected-report"} {
		report := savedPendingAnswer(t, s, thread, turn, id, "report")
		switch id {
		case "accepted-report":
			if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil, report.ID); err != nil {
				t.Fatal(err)
			}
		case "rejected-report":
			if err := s.RejectPendingScheduleAnswer(thread.ID, turn.ID, report.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	finished := &Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(finished); err != nil {
		t.Fatal(err)
	}
	savedPendingAnswer(t, s, thread, finished, "finished-report", "report")
	if err := s.FinishTurn(finished.ID, TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	otherThread := &Thread{Title: "another owner"}
	if err := s.CreateThread(otherThread); err != nil {
		t.Fatal(err)
	}
	otherTurn := &Turn{ThreadID: otherThread.ID}
	if err := s.CreateTurn(otherTurn); err != nil {
		t.Fatal(err)
	}
	savedPendingAnswer(t, s, otherThread, otherTurn, "other-report", "report")
	rows, err := s.ListScheduleReportsForRecovery(thread.ID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("recovery report scope/size mismatch: %+v %v", rows, err)
	}
	for i, id := range []string{"pending-report", "accepted-report", "rejected-report"} {
		if rows[i].ID != id || rows[i].Kind != "report" || rows[i].Payload == "" || rows[i].TurnID != turn.ID {
			t.Fatalf("recovery read loaded an answer, cleared receipt or wrong owner: %+v", rows)
		}
	}
	empty, err := s.ListScheduleReportsForRecovery("missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing recovery reports: %+v %v", empty, err)
	}
}
