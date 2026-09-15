// progress.go — the pulse a running turn emits so a swarm that has gone quiet
// still looks alive. A sub-agent inside a slow tool call streams nothing for
// minutes; without a pulse the screen is frozen and the only honest reading of
// it is "this is stuck".
package engine

import (
	"context"
	"encoding/json"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindProgress is the pulse's event kind. It travels over the wire and the
// front end matches on it.
const KindProgress = "progress"

// progressPulse is a pulse's payload, carried as JSON in the event text.
//
// It carries no activity text and no running count on purpose. What each agent
// is doing is already on screen from its own events, and the library's live tail
// is the last chunk of a stream — "for " or `findings".` — which would replace a
// readable "thinking" with a fragment. A count would be worse than absent: it is
// up to one interval stale, so it would claim two sub-agents are working next to
// the rows that already show them finished. What a silence is missing is not
// words or totals but proof: who was working, and for how long.
//
// Durations are measured on the server: a client that was asleep, throttled in
// a background tab, or is simply on a different clock would otherwise report a
// confidently wrong age for the work it is watching.
type progressPulse struct {
	ElapsedMS int64           `json:"elapsed_ms"`
	Agents    []progressAgent `json:"agents"`
}

type progressAgent struct {
	AgentID   string `json:"agent_id"`
	Role      string `json:"role,omitempty"`
	Status    string `json:"status"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// Sub-agent states a pulse reports. They match the vocabulary the wait_agents
// report already uses, so a UI does not need a second mapping.
const (
	progressRunning = "running"
	progressDone    = "done"
	progressFailed  = "failed"
)

// pulse summarizes one moment of a turn.
func pulse(snap []swarm.AgentProgress, elapsed time.Duration) progressPulse {
	p := progressPulse{ElapsedMS: elapsed.Milliseconds(), Agents: make([]progressAgent, 0, len(snap))}
	for _, a := range snap {
		row := progressAgent{
			AgentID:   a.AgentID,
			Role:      a.Role,
			Status:    progressDone,
			ElapsedMS: a.Elapsed.Milliseconds(),
		}
		switch {
		case a.Running:
			row.Status = progressRunning
		case a.Err != "":
			row.Status = progressFailed
		}
		p.Agents = append(p.Agents, row)
	}
	return p
}

// liveWorkers counts the sub-agents still running in a snapshot.
func liveWorkers(snap []swarm.AgentProgress) int {
	n := 0
	for _, a := range snap {
		if a.Running {
			n++
		}
	}
	return n
}

// pulseText renders a pulse as an event payload. json.Marshal cannot fail for
// a struct of strings and numbers, so the error is dropped rather than given a
// branch that no test could ever reach.
func pulseText(p progressPulse) string {
	body, _ := json.Marshal(p)
	return string(body)
}

// heartbeat emits a pulse every interval until ctx ends, and never emits a
// first one before then: a turn that answers in half a second should not leave
// a progress row behind on the way past.
//
// Pulses are broadcast, not stored. Every fact in one is already recorded by
// the events it summarizes, so persisting a pulse every few seconds would grow
// a conversation without making its replay any more complete — the same reason
// streamed deltas are not stored.
func (rt *runtime) heartbeat(ctx context.Context, turnID string, reg *swarm.Registry, started time.Time, interval time.Duration) {
	if interval <= 0 {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			rt.engine.emit(store.Event{
				ThreadID: rt.threadID, TurnID: turnID,
				Kind:    KindProgress,
				AgentID: swarm.DefaultManagerID,
				Text:    pulseText(pulse(reg.Progress(), time.Since(started))),
			})
		}
	}
}
