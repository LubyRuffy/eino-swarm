package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
)

func answerAdmissionFixture(t *testing.T) (*Store, *Thread, *Turn, Schedule) {
	t.Helper()
	s := open(t)
	thread := &Thread{Title: "admission"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	wake := Schedule{Kind: ScheduleThread, ThreadID: thread.ID, OriginThreadID: thread.ID,
		Status: ScheduleActive, ArmedTurnID: turn.ID, EveryS: 90, NextRunAt: time.Now().UTC().Add(time.Minute)}
	if err := s.CreateSchedule(&wake); err != nil {
		t.Fatal(err)
	}
	return s, thread, turn, wake
}

func admissionAnswer() []Event {
	return []Event{{Kind: "agent_message", AgentID: "manager", Text: "The requested check is arranged."}}
}

func assertNoAdmittedAnswer(t *testing.T, s *Store, threadID string) {
	t.Helper()
	events, err := s.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := s.ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(messages) != 0 {
		t.Fatalf("partial answer became visible: events=%+v messages=%+v", events, messages)
	}
}

func TestAnswerAdmissionRejectsAnEndedTurn(t *testing.T) {
	for _, status := range []string{TurnDone, TurnCancelled, TurnError} {
		t.Run(status, func(t *testing.T) {
			s, thread, turn, wake := answerAdmissionFixture(t)
			if err := s.FinishTurn(turn.ID, status, "", ""); err != nil {
				t.Fatal(err)
			}
			if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer()); err == nil {
				t.Fatal("ended turn admitted a new assistant answer")
			}
			assertNoAdmittedAnswer(t, s, thread.ID)
		})
	}
}

func TestAnswerAdmissionAndReplayCommitTogether(t *testing.T) {
	for _, fail := range []string{"", "events", "messages"} {
		t.Run(fail, func(t *testing.T) {
			s, thread, turn, wake := answerAdmissionFixture(t)
			if fail != "" {
				if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER refuse_admission BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'fixture admission failure'); END", fail)).Error; err != nil {
					t.Fatal(err)
				}
			}
			events := admissionAnswer()
			err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, events)
			if fail != "" {
				if err == nil {
					t.Fatal("injected storage failure reported success")
				}
				assertNoAdmittedAnswer(t, s, thread.ID)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := s.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			msgs, err := s.ListMessages(thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(stored) != 1 || len(msgs) != 1 || stored[0].Text != events[0].Text || msgs[0].Content != events[0].Text || events[0].Seq <= 0 || events[0].CreatedAt.IsZero() {
				t.Fatalf("incomplete committed answer: events=%+v msgs=%+v input=%+v", stored, msgs, events)
			}
		})
	}
}

func TestAnswerAdmissionRejectsChangedScheduleEvidence(t *testing.T) {
	patches := []map[string]any{
		{"status": ScheduleCancelled}, {"status": SchedulePaused}, {"thread_id": "other"}, {"origin_thread_id": "other"},
		{"kind": ScheduleStandalone}, {"armed_turn_id": "other"}, {"next_run_at": time.Now().UTC().Add(time.Hour)},
		{"delay_s": 12}, {"every_s": 20}, {"cron": "0 * * * *"},
	}
	for _, patch := range patches {
		t.Run(fmt.Sprint(patch), func(t *testing.T) {
			s, thread, turn, wake := answerAdmissionFixture(t)
			if err := s.DB().Model(&Schedule{}).Where("id = ?", wake.ID).Updates(patch).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer()); !errors.Is(err, ErrScheduleAnswerChanged) {
				t.Fatalf("changed schedule admitted: %v", err)
			}
			assertNoAdmittedAnswer(t, s, thread.ID)
		})
	}
	t.Run("deleted", func(t *testing.T) {
		s, thread, turn, wake := answerAdmissionFixture(t)
		if err := s.DB().Delete(&wake).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer()); !errors.Is(err, ErrScheduleAnswerChanged) {
			t.Fatalf("deleted schedule admitted: %v", err)
		}
		assertNoAdmittedAnswer(t, s, thread.ID)
	})
}

// Pause the actual event insertion inside the admission transaction. A later
// cancellation must wait, so callers can publish the accepted fact first.
func TestAnswerAdmissionOrdersAConcurrentCancellation(t *testing.T) {
	s, thread, turn, wake := answerAdmissionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	if err := s.DB().Callback().Create().Before("gorm:create").Register("answer_admission_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.DB().Callback().Create().Remove("answer_admission_barrier")
	admitted := make(chan error, 1)
	go func() { admitted <- s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not reach its transaction barrier")
	}
	cancelled := make(chan error, 1)
	go func() {
		cancelled <- s.DB().Model(&Schedule{}).Where("id = ?", wake.ID).Update("status", ScheduleCancelled).Error
	}()
	select {
	case err := <-cancelled:
		close(release)
		t.Fatalf("cancellation overtook answer commit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	if err := <-cancelled; err != nil {
		t.Fatal(err)
	}
	row, err := s.GetSchedule(wake.ID)
	if err != nil || row.Status != ScheduleCancelled {
		t.Fatalf("later cancellation lost: %+v %v", row, err)
	}
	events, err := s.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("accepted answer lost: %+v %v", events, err)
	}
}

func TestAnswerAdmissionRejectsInvalidScopeAndEventKinds(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []Event{{Kind: "tool_call", AgentID: "manager"}, {Kind: "agent_message", AgentID: "worker"}} {
		if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, []Event{ev}); err == nil {
			t.Fatalf("invalid answer accepted: %+v", ev)
		}
	}
	if err := s.AdmitScheduleAnswer(thread.ID, "missing", nil, admissionAnswer()); err == nil {
		t.Fatal("missing turn admitted")
	}
	assertNoAdmittedAnswer(t, s, thread.ID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, nil, admissionAnswer()); err == nil {
		t.Fatal("closed store admitted")
	}
}

func TestAdmittedScheduleFindingsRemainAReport(t *testing.T) {
	s, thread, turn, wake := answerAdmissionFixture(t)
	events := []Event{{Kind: "schedule_report", AgentID: "manager", Text: `{"findings":"The requested check is arranged.","quiet":false}`}}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, events); err != nil {
		t.Fatal(err)
	}
	stored, err := s.ListTurnEvents(turn.ID)
	if err != nil || len(stored) != 1 || stored[0].Kind != "schedule_report" || stored[0].Text != events[0].Text {
		t.Fatalf("admitted report missing: %+v %v", stored, err)
	}
	messages, err := s.ListMessages(thread.ID)
	if err != nil || len(messages) != 0 {
		t.Fatalf("report became an assistant message: %+v %v", messages, err)
	}
}

func TestDurableStopRejectsAnswerBeforeTheTurnStatusChanges(t *testing.T) {
	s, thread, turn, wake := answerAdmissionFixture(t)
	stop := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_stop_requested", AgentID: "manager"}
	if err := s.AppendEvent(&stop); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitScheduleAnswer(thread.ID, turn.ID, []Schedule{wake}, admissionAnswer()); !errors.Is(err, ErrScheduleAnswerStopped) {
		t.Fatalf("answer did not preserve its durable Stop cause: %v", err)
	}
	stored, err := s.ListTurnEvents(turn.ID)
	if err != nil || len(stored) != 1 || stored[0].Kind != "user_stop_requested" {
		t.Fatalf("Stop receipt changed or late answer persisted: %+v %v", stored, err)
	}
	messages, err := s.ListMessages(thread.ID)
	if err != nil || len(messages) != 0 {
		t.Fatalf("late answer reached replay: %+v %v", messages, err)
	}
}
