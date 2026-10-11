package store

import (
	"fmt"
	"sort"
	"strings"
)

// ConversationSchedules is what an open conversation lists. Schedules is
// the union, live rows first. CreatedBy is the subset whose fire minted
// this conversation; a wake that only lands here is not in that list.
type ConversationSchedules struct {
	Schedules []Schedule
	CreatedBy []string
}

// SchedulesForThread lists waits aimed at this conversation and waits that
// minted it. A cancelled wake that used to land here stays off. A cancelled
// job that minted the conversation stays, so the chat can still open it.
// An empty kind on a fire follows the schedule row, same as quiet archive
// for fires claimed before the column existed.
func (s *Store) SchedulesForThread(threadID string) (ConversationSchedules, error) {
	threadID = strings.TrimSpace(threadID)
	empty := ConversationSchedules{Schedules: []Schedule{}, CreatedBy: []string{}}
	if threadID == "" {
		return empty, nil
	}
	var bound []Schedule
	err := s.db.Where(
		"kind = ? AND thread_id = ? AND status <> ?",
		ScheduleThread, threadID, ScheduleCancelled,
	).Find(&bound).Error
	if err != nil {
		return ConversationSchedules{}, fmt.Errorf("store: schedules for thread: %w", err)
	}
	var runs []ScheduleRun
	if err := s.db.Where("thread_id = ?", threadID).Find(&runs).Error; err != nil {
		return ConversationSchedules{}, fmt.Errorf("store: schedule runs for thread: %w", err)
	}
	minted := map[string]struct{}{}
	legacy := map[string]struct{}{}
	for _, run := range runs {
		id := strings.TrimSpace(run.ScheduleID)
		if id == "" {
			continue
		}
		switch strings.TrimSpace(run.Kind) {
		case ScheduleStandalone:
			minted[id] = struct{}{}
		case "":
			legacy[id] = struct{}{}
		}
	}
	created := map[string]Schedule{}
	if len(minted)+len(legacy) > 0 {
		ids := make([]string, 0, len(minted)+len(legacy))
		seen := map[string]struct{}{}
		for id := range minted {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		for id := range legacy {
			if _, ok := seen[id]; ok {
				continue
			}
			ids = append(ids, id)
		}
		var rows []Schedule
		if err := s.db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return ConversationSchedules{}, fmt.Errorf("store: schedules for thread: %w", err)
		}
		for _, row := range rows {
			if _, ok := minted[row.ID]; ok {
				created[row.ID] = row
				continue
			}
			if row.Kind == ScheduleStandalone {
				created[row.ID] = row
			}
		}
	}
	createdIDs := make([]string, 0, len(created))
	for id := range created {
		createdIDs = append(createdIDs, id)
	}
	sort.Strings(createdIDs)
	return ConversationSchedules{
		Schedules: mergeConversationSchedules(bound, created),
		CreatedBy: createdIDs,
	}, nil
}

func mergeConversationSchedules(bound []Schedule, created map[string]Schedule) []Schedule {
	byID := make(map[string]Schedule, len(bound)+len(created))
	for _, row := range bound {
		byID[row.ID] = row
	}
	for id, row := range created {
		byID[id] = row
	}
	out := make([]Schedule, 0, len(byID))
	for _, row := range byID {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return conversationScheduleLess(out[i], out[j])
	})
	return out
}

func conversationScheduleLess(a, b Schedule) bool {
	liveA := a.Status == ScheduleActive || a.Status == SchedulePaused
	liveB := b.Status == ScheduleActive || b.Status == SchedulePaused
	if liveA != liveB {
		return liveA
	}
	if !a.NextRunAt.Equal(b.NextRunAt) {
		return a.NextRunAt.Before(b.NextRunAt)
	}
	return a.ID < b.ID
}
