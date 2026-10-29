package engine

import (
	"context"
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindHistoryReportRequested is an explicitly selected answer mode, not a guessed
// intent or an instruction copied from the retained source material.
const KindHistoryReportRequested = "history_report_requested"

func (e *Engine) recordHistoryRegistryInheritance(ctx context.Context, threadID, targetTurnID, sourceTurnID string) error {
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	for _, id := range []string{targetTurnID, sourceTurnID} {
		if _, dropped := e.droppedTurns[id]; dropped {
			return store.ErrNotFound
		}
	}
	event, created, err := e.store.RecordHistoryRegistryInheritanceEvent(ctx, threadID, targetTurnID, sourceTurnID)
	if err != nil {
		return fmt.Errorf("record original history Registry inheritance: %w", err)
	}
	if created {
		e.broadcast(*event)
	}
	return nil
}

// Read before rewind removes the original turn's mode receipt.
func (e *Engine) historyReportRequestAt(threadID string, seq int64) (bool, error) {
	event, err := e.store.GetEvent(threadID, seq)
	if err != nil {
		return false, err
	}
	return e.store.TurnHasHistoryReportRequest(threadID, event.TurnID)
}

func (e *Engine) recordHistoryReportRequest(turn *store.Turn) error {
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return store.ErrNotFound
	}
	event := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindHistoryReportRequested}
	if err := e.store.AppendEvent(&event); err != nil {
		return fmt.Errorf("record history report request: %w", err)
	}
	e.broadcast(event)
	return nil
}
