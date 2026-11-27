package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"gorm.io/gorm"
)

var errGraphToolPayloadEvidence = fmt.Errorf("%w: evidence cannot reference graph tool traffic; cite the original event", graph.ErrInvalid)

var errOwnToolReceiptUnavailable = fmt.Errorf("%w: own tool receipt unavailable", graph.ErrInvalid)

// ErrOwnToolReceiptUnavailable is returned when a named tool call id does not
// resolve to this caller's own completed call, so a caller can cite its real
// results instead.
var ErrOwnToolReceiptUnavailable = errOwnToolReceiptUnavailable

// graphEvidenceEvent checks that an event exists inside the owner's reach: its
// own conversation, or any conversation of the owning project.
func graphEvidenceEvent(tx *gorm.DB, caller GraphCaller, ev graph.Evidence) (turnID string, err error) {
	var event Event
	query := tx.Table("events").Select("events.turn_id", "events.work_record_payload").
		Joins("JOIN threads ON threads.id = events.thread_id").
		Where("events.thread_id = ? AND events.seq = ?", ev.ThreadID, ev.Seq)
	if caller.ProjectID == "" {
		query = query.Where("threads.id = ?", caller.ThreadID)
	} else {
		query = query.Where("threads.project_id = ?", caller.ProjectID)
	}
	if err := query.First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%w: evidence event is missing or outside this graph's scope", graph.ErrInvalid)
		}
		return "", err
	}
	if event.WorkRecordPayload {
		return "", errGraphToolPayloadEvidence
	}
	if ev.TurnID != "" && ev.TurnID != event.TurnID {
		return "", fmt.Errorf("%w: evidence turn does not match its event", graph.ErrInvalid)
	}
	return event.TurnID, nil
}

// resolveGraphEvidence turns the caller's evidence claim into verified rows.
// Literal evidence is checked event by event; own_tool_call_ids is resolved
// to this agent's own completed tool results.
func resolveGraphEvidence(tx *gorm.DB, caller GraphCaller, batch graph.Batch) ([]graph.Evidence, error) {
	if batch.OwnToolCallIDs != nil {
		return resolveOwnToolEvidence(tx, caller, batch.OwnToolCallIDs)
	}
	if len(batch.Evidence) == 0 && batch.CiteOwnResults {
		return citeCallerToolResults(tx, caller)
	}
	var out []graph.Evidence
	seen := map[string]bool{}
	for _, ev := range batch.Evidence {
		if ev.URL == "" {
			turn, err := graphEvidenceEvent(tx, caller, ev)
			if err != nil {
				return nil, err
			}
			ev.TurnID = turn
		}
		if !seen[ev.Key()] {
			seen[ev.Key()] = true
			out = append(out, ev)
		}
	}
	return out, nil
}

// resolveOwnToolEvidence finds the completed tool results of the calling
// agent's own named calls. The host never trusts a model-authored sequence
// number for these: the call ids are looked up in the durable events.
func resolveOwnToolEvidence(tx *gorm.DB, caller GraphCaller, ids []string) ([]graph.Evidence, error) {
	if len(ids) == 0 {
		return []graph.Evidence{}, nil
	}
	if len(ids) > graph.MaxOwnToolCalls || strings.TrimSpace(caller.CurrentToolCallID) == "" {
		return nil, errOwnToolReceiptUnavailable
	}
	type toolPair struct {
		call, result int64
		resultTurnID string
		payload      bool
	}
	pairs := make(map[string]toolPair, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || id == caller.CurrentToolCallID {
			return nil, errOwnToolReceiptUnavailable
		}
		if _, dup := pairs[id]; dup {
			return nil, errOwnToolReceiptUnavailable
		}
		pairs[id] = toolPair{}
	}
	var events []struct {
		ToolCallID        string
		Kind              string
		Seq               int64
		TurnID            string
		WorkRecordPayload bool
	}
	query := tx.Model(&Event{}).Select("tool_call_id", "kind", "seq", "turn_id", "work_record_payload").
		Where("thread_id = ? AND kind IN ? AND agent_id = ? AND worker_generation = ? AND tool_call_id IN ?",
			caller.ThreadID, []string{"tool_call", "tool_result"}, caller.AgentID, caller.Generation, ids)
	if caller.ToolReceiptScope == "" {
		query = query.Where("turn_id = ? AND COALESCE(tool_receipt_scope, '') = ''", caller.TurnID)
	} else {
		query = query.Where("tool_receipt_scope <> '' AND tool_receipt_scope = ?", caller.ToolReceiptScope)
	}
	// One extra row so duplicates cannot hide behind the limit.
	if err := query.Limit(2*len(ids) + 1).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("resolve own tool evidence: %w", err)
	}
	if len(events) != 2*len(ids) {
		return nil, errOwnToolReceiptUnavailable
	}
	for _, ev := range events {
		pair := pairs[ev.ToolCallID]
		seq := &pair.result
		if ev.Kind == "tool_call" {
			seq = &pair.call
		} else {
			pair.resultTurnID, pair.payload = ev.TurnID, ev.WorkRecordPayload
		}
		if *seq != 0 || ev.Seq <= 0 || ev.TurnID == "" {
			return nil, errOwnToolReceiptUnavailable
		}
		*seq = ev.Seq
		pairs[ev.ToolCallID] = pair
	}
	out := make([]graph.Evidence, 0, len(ids))
	for _, id := range ids {
		pair := pairs[id]
		if pair.call == 0 || pair.result <= pair.call {
			return nil, errOwnToolReceiptUnavailable
		}
		if pair.payload {
			return nil, errGraphToolPayloadEvidence
		}
		out = append(out, graph.Evidence{ThreadID: caller.ThreadID, TurnID: pair.resultTurnID, Seq: pair.result})
	}
	return out, nil
}

// citeCallerToolResults cites the caller's own completed tool results in this
// turn, newest first, skipping graph-tool traffic and the call in progress.
// An empty list is not an error: a settled claim with nothing to cite is
// still refused by the settlement check.
func citeCallerToolResults(tx *gorm.DB, caller GraphCaller) ([]graph.Evidence, error) {
	type row struct {
		ToolCallID        string
		Kind              string
		Seq               int64
		TurnID            string
		WorkRecordPayload bool
	}
	var events []row
	query := tx.Model(&Event{}).Select("tool_call_id", "kind", "seq", "turn_id", "work_record_payload").
		Where("thread_id = ? AND kind IN ? AND agent_id = ? AND worker_generation = ? AND tool_call_id <> ''",
			caller.ThreadID, []string{"tool_call", "tool_result"}, caller.AgentID, caller.Generation)
	if caller.CurrentToolCallID != "" {
		query = query.Where("tool_call_id <> ?", caller.CurrentToolCallID)
	}
	if caller.ToolReceiptScope == "" {
		query = query.Where("turn_id = ? AND COALESCE(tool_receipt_scope, '') = ''", caller.TurnID)
	} else {
		query = query.Where("tool_receipt_scope <> '' AND tool_receipt_scope = ?", caller.ToolReceiptScope)
	}
	if err := query.Order("seq DESC").Limit(200).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("cite own tool results: %w", err)
	}
	type pair struct {
		call, result int64
		turn         string
		payload      bool
		bad          bool
	}
	pairs := map[string]*pair{}
	order := make([]string, 0, len(events))
	for _, ev := range events {
		p := pairs[ev.ToolCallID]
		if p == nil {
			p = &pair{}
			pairs[ev.ToolCallID] = p
			order = append(order, ev.ToolCallID)
		}
		if ev.Seq <= 0 || ev.TurnID == "" {
			p.bad = true
			continue
		}
		if ev.Kind == "tool_call" {
			if p.call != 0 {
				p.bad = true
				continue
			}
			p.call = ev.Seq
		} else {
			if p.result != 0 {
				p.bad = true
				continue
			}
			p.result, p.turn, p.payload = ev.Seq, ev.TurnID, ev.WorkRecordPayload
		}
	}
	out := make([]graph.Evidence, 0, graph.MaxOwnToolCalls)
	for _, id := range order {
		p := pairs[id]
		if p.bad || p.call == 0 || p.result <= p.call || p.payload {
			continue
		}
		out = append(out, graph.Evidence{ThreadID: caller.ThreadID, TurnID: p.turn, Seq: p.result})
		if len(out) == graph.MaxOwnToolCalls {
			break
		}
	}
	return out, nil
}

// unionEvidence appends additions that are not already present, keeping the
// newest MaxEvidence entries so a row cannot grow without bound.
func unionEvidence(current, additions []graph.Evidence) []graph.Evidence {
	seen := make(map[string]bool, len(current))
	out := append([]graph.Evidence(nil), current...)
	for _, ev := range current {
		seen[ev.Key()] = true
	}
	for _, ev := range additions {
		if !seen[ev.Key()] {
			seen[ev.Key()] = true
			out = append(out, ev)
		}
	}
	if len(out) > graph.MaxEvidence {
		out = out[len(out)-graph.MaxEvidence:]
	}
	return out
}
