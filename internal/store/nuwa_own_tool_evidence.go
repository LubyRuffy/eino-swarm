package store

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const maxNuwaOwnToolCallIDs = 16

// ErrNuwaOwnToolReceiptUnavailable means the requested IDs do not identify a
// unique completed tool invocation within the caller's durable scope.
var ErrNuwaOwnToolReceiptUnavailable = invalidNuwa("own tool receipt unavailable")

func resolveNuwaOwnToolEvidence(tx *gorm.DB, caller NuwaNode, receipt NuwaWriteReceipt) ([]NuwaEvidence, error) {
	ids := receipt.OwnToolCallIDs
	if len(ids) == 0 {
		return []NuwaEvidence{}, nil
	}
	if len(ids) > maxNuwaOwnToolCallIDs || strings.TrimSpace(receipt.CurrentToolCallID) == "" {
		return nil, ErrNuwaOwnToolReceiptUnavailable
	}
	type toolPair struct {
		call, result int64
		resultTurnID string
	}
	pairs := make(map[string]toolPair, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || id == receipt.CurrentToolCallID {
			return nil, ErrNuwaOwnToolReceiptUnavailable
		}
		if _, exists := pairs[id]; exists {
			return nil, ErrNuwaOwnToolReceiptUnavailable
		}
		pairs[id] = toolPair{}
	}

	var events []struct {
		ToolCallID string
		Kind       string
		Seq        int64
		TurnID     string
	}
	query := tx.Model(&Event{}).Select("tool_call_id", "kind", "seq", "turn_id").
		Where("thread_id = ? AND kind IN ? AND agent_id = ? AND worker_generation = ? AND tool_call_id IN ?",
			caller.ThreadID, []string{"tool_call", "tool_result"}, caller.AgentID, caller.Generation, ids)
	if receipt.ToolReceiptScope == "" {
		query = query.Where("turn_id = ? AND COALESCE(tool_receipt_scope, '') = ''", caller.TurnID)
	} else {
		// Match the partial index predicate as well as the exact execution.
		query = query.Where("tool_receipt_scope <> '' AND tool_receipt_scope = ?", receipt.ToolReceiptScope)
	}
	// Read one extra row so duplicates cannot hide behind the limit.
	err := query.Limit(2*len(ids) + 1).Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("resolve own tool evidence: %w", err)
	}
	if len(events) != 2*len(ids) {
		return nil, ErrNuwaOwnToolReceiptUnavailable
	}
	for _, ev := range events {
		pair := pairs[ev.ToolCallID]
		seq := &pair.result
		if ev.Kind == "tool_call" {
			seq = &pair.call
		} else {
			pair.resultTurnID = ev.TurnID
		}
		if *seq != 0 || ev.Seq <= 0 || ev.TurnID == "" {
			return nil, ErrNuwaOwnToolReceiptUnavailable
		}
		*seq = ev.Seq
		pairs[ev.ToolCallID] = pair
	}

	evidence := make([]NuwaEvidence, 0, len(ids))
	for _, id := range ids {
		pair := pairs[id]
		if pair.call == 0 || pair.result <= pair.call {
			return nil, ErrNuwaOwnToolReceiptUnavailable
		}
		evidence = append(evidence, NuwaEvidence{ThreadID: caller.ThreadID, TurnID: pair.resultTurnID, Seq: pair.result})
	}
	return evidence, nil
}
