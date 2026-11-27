package store

import "fmt"

// rosterIdentityColumns are the live-edge sidecar fields. The CASE leaves
// spawned and finished text unread: those bodies are the worker prompt and
// result, and they sit in overflow pages the Agents list does not paint.
// The worker's own log still returns them. cleanup, workers_retained and
// error keep text; those rows are small and the reducer uses them.
const rosterIdentityColumns = `completion, source_thread_id, source_turn_id, id, thread_id, turn_id, seq, kind, agent_id, role, model_binding, worker_generation, config_tag, tool_call_id, tool_receipt_scope, message_id, CASE WHEN kind IN ('spawned', 'finished') THEN '' ELSE text END AS text, err, images, created_at`

// ListRosterIdentities is the open-conversation roster. Resume and the worker
// log keep using ListRosterEvents and ListAgentEvents, which still return the
// stored bodies.
func (s *Store) ListRosterIdentities(threadID string) ([]Event, error) {
	var out []Event
	err := s.db.Select(rosterIdentityColumns).
		Where("thread_id = ? AND kind IN ?", threadID, rosterEventKinds).
		Order("seq asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list roster identities: %w", err)
	}
	return out, nil
}

// turnOpeningColumns keep the footer and the jump rail. final is the stored
// answer, and user_text on an engine-started turn is the protocol prompt.
// Neither is what the open screen reads; Trace and the phone preview still
// load the stored bodies through their own queries. The CASE and the empty
// final are evaluated in SQL so those overflow pages stay unread.
const turnOpeningColumns = `project_id, id, thread_id, seq, status,
CASE WHEN goal_continue <> 0 OR schedule_continue <> 0 THEN '' ELSE user_text END AS user_text,
'' AS final,
error, provider_id, model, worker_model_recovery, reasoning_effort, web_execution,
nuwa_enabled, work_ledger, work_ledger_read_only,
image_command, goal_continue, schedule_run_id, retry_turn_id, quiet, schedule_continue,
started_at, ended_at, duration_ms`

// ListTurnOpenings is the conversation-open turn list. ListTurns remains the
// full row for restore, review and every other engine caller.
func (s *Store) ListTurnOpenings(threadID string) ([]Turn, error) {
	var out []Turn
	err := s.db.Select(turnOpeningColumns).Where("thread_id = ?", threadID).Order("seq asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list turn openings: %w", err)
	}
	return out, nil
}
