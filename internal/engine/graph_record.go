package engine

import (
	"context"
	"encoding/json"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// graphRecordCue is the one reminder before a turn's answer is kept. It names
// no kind and no task: the model already has the tool definition.
const graphRecordCue = "Tool results from this turn are not on the working graph yet. Record each distinct thing they established with one update_graph call, then answer. Do not repeat those tools."

const modelRetryReasonGraphUnrecorded = "graph_unrecorded"

// graphRecordUsed reports that this turn already spent its one recording pass.
// A later human extension does not grant another: the answer is kept either way.
func graphRecordUsed(events []store.Event) bool {
	for _, ev := range events {
		if !isManagerAgent(ev.AgentID) || ev.Kind != KindModelRetry {
			continue
		}
		var body struct{ Reason string }
		if json.Unmarshal([]byte(ev.Text), &body) == nil && body.Reason == modelRetryReasonGraphUnrecorded {
			return true
		}
	}
	return false
}

// graphRecordContinued reads that allowance. An unreadable log cannot prove
// the pass is still available, so the answer is kept instead of looping.
func (e *Engine) graphRecordContinued(turn *store.Turn, snapshots ...*restoreSnapshot) bool {
	if turn == nil || !e.graphEnabled(turn) || turn.GraphReadOnly {
		return false
	}
	events, err := e.restoreTurnEvents(turn.ID, snapshots...)
	if err != nil {
		e.log.Warn("could not read graph record allowance", "thread", turn.ThreadID, "turn", turn.ID, "err", err)
		return true
	}
	return graphRecordUsed(events)
}

// graphRecordDue is the end-of-answer check: graph tools are on, this turn
// produced tool results the graph has not taken, and the one pass is unused.
func (e *Engine) graphRecordDue(ctx context.Context, turn *store.Turn, continued bool) bool {
	if turn == nil || continued || turn.GraphReadOnly || !e.graphEnabled(turn) {
		return false
	}
	n, err := e.store.GraphUnrecorded(ctx, turn.ThreadID, turn.ID)
	if err != nil {
		e.log.Warn("graph unrecorded count unavailable", "thread", turn.ThreadID, "turn", turn.ID, "err", err)
		return false
	}
	return n > 0
}

// recordGraphRecordRetry grants the optional pass only after its allowance is durable.
func (rt *runtime) recordGraphRecordRetry(turn *store.Turn) bool {
	body, _ := json.Marshal(map[string]any{"attempt": 1, "cap": 1, "reason": modelRetryReasonGraphUnrecorded})
	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return false
	}
	ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindModelRetry, AgentID: swarm.DefaultManagerID, Text: string(body)}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.log.Warn("could not persist graph record allowance", "thread", turn.ThreadID, "turn", turn.ID, "err", err)
		return false
	}
	e.broadcast(ev)
	return true
}
