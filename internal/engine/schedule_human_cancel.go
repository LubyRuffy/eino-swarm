package engine

import (
	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// CancelSchedule applies an explicit human cancellation. For a thread wake,
// cancellation also revokes the manager's permission to park that conversation
// until the human explicitly permits another wait.
func (e *Engine) CancelSchedule(id string) error {
	row, err := e.store.GetSchedule(id)
	if err != nil {
		return err
	}
	if row.Kind != store.ScheduleThread || row.ThreadID == "" {
		// An inbox edit can move an independent job onto a conversation after
		// the read above. Refuse that changed destination rather than cancelling
		// a thread wake without recording the human's authority.
		row, err = e.store.CancelScheduleAtDestination(id, row.Kind, row.ThreadID)
		if err != nil || row == nil {
			return err
		}
		e.recordScheduleCancelled(row)
		return nil
	}
	wasWaiting := row.Status == store.ScheduleActive || row.Status == store.SchedulePaused
	// Use the existing event ordering lock through commit and broadcast. The
	// cancellation receipt and the cancelled row must be durable together before
	// a concurrently finishing turn or a new human Continue can observe them.
	e.recordMu.Lock()
	ev := store.Event{
		ThreadID: row.ThreadID, TurnID: e.lastTurnID(row.ThreadID),
		Kind: KindScheduleCancelled, AgentID: swarm.DefaultManagerID,
		Role: "human", Text: id,
	}
	row, err = e.store.CancelScheduleByHuman(id, &ev)
	if err == nil && row != nil {
		e.broadcast(ev)
	}
	e.recordMu.Unlock()
	if err != nil || row == nil {
		return err
	}
	if row.OriginThreadID != "" && row.OriginThreadID != row.ThreadID {
		// The origin still gets its ordinary chip. Authority belongs only to the
		// actual target, including waits moved from another conversation.
		e.recordScheduleCancelled(row)
	}
	if wasWaiting {
		e.continueGoalAfterWakeStop(row)
	}
	return nil
}
