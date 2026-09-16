package engine

import (
	"encoding/json"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindUsage is the live token snapshot. It travels over the wire; the front
// end matches on it. Seq stays 0 and it is not stored: every number is already
// on llm_calls, and persisting a copy per call would double the table that
// trace already reads.
const KindUsage = "usage"

func usageText(snap store.UsageSnapshot) string {
	body, _ := json.Marshal(snap)
	return string(body)
}

// Usage is the composer's meter: last manager prompt vs the selected model's
// window, plus billed totals for this turn and the conversation.
func (e *Engine) Usage(threadID, turnID string) (store.UsageSnapshot, error) {
	snap, err := e.store.SummarizeUsage(threadID, turnID)
	if err != nil {
		return snap, err
	}
	if th, err := e.store.GetThread(threadID); err == nil {
		snap.ContextWindow = e.pool.WindowFor(th.ProviderID, th.Model)
	}
	return snap, nil
}

func (e *Engine) emitUsage(threadID, turnID string) {
	snap, err := e.Usage(threadID, turnID)
	if err != nil {
		e.log.Warn("could not summarise token usage", "thread", threadID, "err", err)
		return
	}
	e.emit(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind:    KindUsage,
		AgentID: swarm.DefaultManagerID,
		Text:    usageText(snap),
	})
}
