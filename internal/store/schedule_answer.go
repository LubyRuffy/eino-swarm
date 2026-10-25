package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var ErrScheduleAnswerChanged = errors.New("store: scheduling evidence changed before answer admission")
var ErrScheduleAnswerStopped = errors.New("store: human Stop was recorded before answer admission")
var ErrScheduleAnswerAlreadyAdmitted = errors.New("store: scheduling answer was already admitted")

// AdmitScheduleAnswer rechecks the exact rows used to validate a scheduling
// claim and commits its public answer and replay messages together. The sole
// SQLite connection orders cancellation/retargeting against this transaction.
// The engine holds recordMu through this commit and the subsequent broadcast.
// Candidate decisions commit with those rows, including an empty public answer.
// ErrScheduleAnswerAlreadyAdmitted means callers must not broadcast again.
func (s *Store) AdmitScheduleAnswer(threadID, turnID string, expected []Schedule, events []Event, candidateIDs ...string) error {
	if len(events) == 0 && len(candidateIDs) == 0 {
		return nil
	}
	msgs := make([]Message, 0, len(events))
	now := time.Now().UTC()
	for i := range events {
		if (events[i].Kind != "agent_message" && events[i].Kind != "schedule_report") || (events[i].AgentID != "" && events[i].AgentID != "manager") {
			return fmt.Errorf("store: invalid schedule answer event")
		}
		seq, err := s.nextEventSeq(threadID)
		if err != nil {
			return err
		}
		events[i].ThreadID, events[i].TurnID = threadID, turnID
		events[i].Seq, events[i].CreatedAt = seq, now
		if events[i].Kind == "agent_message" {
			msgs = append(msgs, Message{ThreadID: threadID, TurnID: turnID, Role: "assistant", AgentID: "manager", Content: events[i].Text, CreatedAt: now,
				Seq: s.nextSeq(s.msgSeq, threadID, func() int64 { return s.maxSeq(&Message{}, "seq", threadID) })})
		}
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := pendingScheduleAnswerAdmission(tx, threadID, turnID, candidateIDs); err != nil {
			return err
		}
		if err := scheduleAnswerOwner(tx, threadID, turnID); err != nil {
			return err
		}
		for _, want := range expected {
			var got Schedule
			if err := tx.First(&got, "id = ?", want.ID).Error; err != nil {
				return ErrScheduleAnswerChanged
			}
			if !sameScheduleAnswerEvidence(want, got) {
				return ErrScheduleAnswerChanged
			}
		}
		if len(events) > 0 {
			if err := requireScheduleAnswerWrite(tx.Create(&events), int64(len(events))); err != nil {
				return err
			}
		}
		if len(msgs) > 0 {
			if err := requireScheduleAnswerWrite(tx.Create(&msgs), int64(len(msgs))); err != nil {
				return err
			}
		}
		if len(candidateIDs) > 0 {
			return requireScheduleAnswerWrite(tx.Model(&PendingScheduleAnswer{}).Where("id IN ?", candidateIDs).Update("state", "accepted"), int64(len(candidateIDs)))
		}
		return nil
	})
}

func pendingScheduleAnswerAdmission(tx *gorm.DB, threadID, turnID string, ids []string) error {
	accepted := 0
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return ErrScheduleAnswerChanged
		}
		seen[id] = true
		var row PendingScheduleAnswer
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.ThreadID != threadID || row.TurnID != turnID {
			return ErrScheduleAnswerChanged
		}
		switch row.State {
		case "accepted":
			accepted++
		case "pending":
		default:
			return ErrScheduleAnswerChanged
		}
	}
	if accepted > 0 {
		if accepted == len(ids) {
			return ErrScheduleAnswerAlreadyAdmitted
		}
		return ErrScheduleAnswerChanged
	}
	return nil
}

func sameScheduleAnswerEvidence(want, got Schedule) bool {
	return got.Kind == want.Kind && got.ThreadID == want.ThreadID && got.OriginThreadID == want.OriginThreadID &&
		got.Status == ScheduleActive && got.Status == want.Status && got.ArmedTurnID == want.ArmedTurnID &&
		got.NextRunAt.Equal(want.NextRunAt) && got.DelayS == want.DelayS && got.EveryS == want.EveryS && got.Cron == want.Cron
}
