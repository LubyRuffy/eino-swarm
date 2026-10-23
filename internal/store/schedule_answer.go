package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var ErrScheduleAnswerChanged = errors.New("store: scheduling evidence changed before answer admission")
var ErrScheduleAnswerStopped = errors.New("store: human Stop was recorded before answer admission")

// AdmitScheduleAnswer rechecks the exact rows used to validate a scheduling
// claim and commits its public answer and replay messages together. The sole
// SQLite connection orders cancellation/retargeting against this transaction.
// The engine holds recordMu through this commit and the subsequent broadcast.
func (s *Store) AdmitScheduleAnswer(threadID, turnID string, expected []Schedule, events []Event) error {
	if len(events) == 0 {
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
		var turn Turn
		if err := tx.First(&turn, "id = ? AND thread_id = ?", turnID, threadID).Error; err != nil {
			return err
		}
		if turn.Status != TurnRunning {
			return ErrScheduleAnswerChanged
		}
		var stopped int64
		if err := tx.Model(&Event{}).Where("thread_id = ? AND turn_id = ? AND kind = ?", threadID, turnID, "user_stop_requested").Count(&stopped).Error; err != nil {
			return err
		}
		if stopped != 0 {
			return ErrScheduleAnswerStopped
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
		if err := tx.Create(&events).Error; err != nil {
			return err
		}
		if len(msgs) > 0 {
			return tx.Create(&msgs).Error
		}
		return nil
	})
}

func sameScheduleAnswerEvidence(want, got Schedule) bool {
	return got.Kind == want.Kind && got.ThreadID == want.ThreadID && got.OriginThreadID == want.OriginThreadID &&
		got.Status == ScheduleActive && got.Status == want.Status && got.ArmedTurnID == want.ArmedTurnID &&
		got.NextRunAt.Equal(want.NextRunAt) && got.DelayS == want.DelayS && got.EveryS == want.EveryS && got.Cron == want.Cron
}
