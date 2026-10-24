package store

import (
	"context"
	"fmt"
)

// LatestEventOfKind walks the thread/kind/seq index newest first. An optional
// validator skips corrupt/unsupported checkpoints in the same query; normally
// only one body is read. Validation belongs to the owner of the event schema.
func (s *Store) LatestEventOfKind(ctx context.Context, threadID, kind string, valid ...func(*Event) bool) (*Event, error) {
	rows, err := s.db.WithContext(ctx).Model(&Event{}).Where("thread_id = ? AND kind = ?", threadID, kind).Order("seq desc").Rows()
	if rows != nil {
		defer func() { _ = rows.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("store: latest event kind: %w", err)
	}
	for rows.Next() {
		var event Event
		if err := s.db.ScanRows(rows, &event); err != nil {
			return nil, fmt.Errorf("store: decode latest event kind: %w", err)
		}
		if len(valid) == 0 || valid[0](&event) {
			return &event, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read latest event kind: %w", err)
	}
	return nil, nil
}

// ListRecoveryEvents reads lifecycle evidence once. Tool/output bodies older
// than the accepted checkpoint and token deltas are not recovery inputs.
func (s *Store) ListRecoveryEvents(ctx context.Context, threadID string, checkpointSeq int64, pendingCallIDs ...string) ([]Event, error) {
	lifecycle := []string{"spawned", "finished", "cleanup", "workers_retained", "error", "done", "user_message", "goal_resumed", "user_stop_requested", "generation_started", "generation_completed", "generation_interrupted", "generation_recovered", "steer", "steer_retracted", "steer_consumed", "steer_revised", "steer_preempted", "goal_continued", "schedule_fired", "model_retry", "max_iterations_continued"}
	content := []string{"tool_call", "tool_result", "agent_message"}
	var events []Event
	condition := "kind IN ? OR (seq > ? AND kind IN ?)"
	args := []any{lifecycle, checkpointSeq, content}
	if len(pendingCallIDs) > 0 {
		condition += " OR (kind IN ? AND tool_call_id IN ?)"
		args = append(args, []string{"tool_call", "tool_result"}, pendingCallIDs)
	}
	args = append([]any{threadID}, args...)
	err := s.db.WithContext(ctx).Where("thread_id = ? AND ("+condition+")", args...).Order("seq asc").Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("store: list recovery events: %w", err)
	}
	return events, nil
}
