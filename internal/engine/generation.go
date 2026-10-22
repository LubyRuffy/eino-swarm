package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type generationLog struct {
	open    map[string]store.Event
	pending map[string]store.Event
}

type generationLogs struct {
	sync.Mutex
	threads map[string]*generationLog
}

func (e *Engine) generationLog(threadID string) *generationLog {
	if e.generations.threads == nil {
		e.generations.threads = map[string]*generationLog{}
	}
	if log := e.generations.threads[threadID]; log != nil {
		return log
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			e.log.Warn("could not read generation owner", "thread", threadID, "err", err)
		}
		return nil
	}
	log := &generationLog{open: map[string]store.Event{}, pending: map[string]store.Event{}}
	events, err := e.store.ListGenerationEvents(threadID)
	if err != nil {
		e.log.Warn("could not read generation lifecycle", "thread", threadID, "err", err)
		return nil
	}
	for _, ev := range events {
		log.apply(ev)
	}
	e.generations.threads[threadID] = log
	return log
}

func (g *generationLog) apply(ev store.Event) {
	switch ev.Kind {
	case "generation_started":
		g.open[ev.MessageID] = ev
		for id, pending := range g.pending {
			var body swarm.GenerationInterruption
			if pending.AgentID == ev.AgentID && isManagerAgent(ev.AgentID) && pending.TurnID != ev.TurnID && json.Unmarshal([]byte(pending.Text), &body) == nil && body.Recovery == "none" {
				delete(g.pending, id)
			}
		}
	case "generation_completed":
		delete(g.open, ev.MessageID)
	case "generation_interrupted":
		delete(g.open, ev.MessageID)
		g.pending[ev.MessageID] = ev
	case "generation_recovered":
		delete(g.pending, ev.MessageID)
	}
}

func generationBody(body swarm.GenerationInterruption) string {
	text, _ := json.Marshal(body)
	return string(text)
}

func (a *accumulator) generation(n swarm.Notification) {
	a.gate.Lock()
	defer a.gate.Unlock()
	e := a.engine
	e.generations.Lock()
	defer e.generations.Unlock()
	log := e.generationLog(a.threadID)
	if log == nil {
		return
	}
	ev := a.event(n, n.Kind.String())
	if origin, ok := log.open[n.MessageID]; ok {
		ev.TurnID = origin.TurnID
	}
	switch n.Kind {
	case swarm.NotifyGenerationStarted, swarm.NotifyTurn:
		for _, old := range log.pending {
			if old.AgentID != n.AgentID || old.MessageID == n.MessageID {
				continue
			}
			var body swarm.GenerationInterruption
			if json.Unmarshal([]byte(old.Text), &body) == nil && (!isManagerAgent(n.AgentID) || old.TurnID == a.turnID || body.Recovery == "pending") {
				body.Recovery = "recovered"
				recovered := old
				recovered.ID, recovered.Seq = 0, 0
				recovered.Kind, recovered.Text = "generation_recovered", generationBody(body)
				if e.recordGeneration(&recovered) {
					log.apply(recovered)
				}
			}
		}
	case swarm.NotifyGenerationInterrupted:
		a.flushAgentLiveLocked(n.AgentID)
		a.flushAgent(n.AgentID)
		var body swarm.GenerationInterruption
		if json.Unmarshal([]byte(n.Text), &body) != nil {
			body = swarm.GenerationInterruption{Reason: "unknown", Detail: "The generation was interrupted; its cause was not recorded.", Recovery: "none"}
		}
		e.mu.Lock()
		rt := e.runtimes[a.threadID]
		e.mu.Unlock()
		if rt != nil {
			rt.mu.Lock()
			abandoned, stopped := rt.abandoned, rt.userStopped
			rt.mu.Unlock()
			if stopped {
				body.Recovery = "none"
				if body.Reason == "cancelled" {
					body.Detail = "Generation stopped after a human interruption."
				}
			} else if abandoned {
				body.Recovery = "pending"
				if body.Reason == "cancelled" {
					status := e.GetRestartStatus()
					if status.Stage != "" && status.Stage != "failed" {
						body.Reason, body.Detail = "engine_restart", "The engine interrupted generation for restart handoff."
					} else {
						body.Detail = "The engine is shutting down; generation can resume on the next launch."
					}
				}
			}
		}
		if n.Err != nil && body.Reason == "model_error" {
			detail := strings.Join(strings.Fields(publicTurnError(n.Err)), " ")
			detail = publicAPIBearer.ReplaceAllString(detail, "[redacted]")
			detail = publicAPICredentials.ReplaceAllString(detail, "[redacted]")
			if chars := []rune(detail); len(chars) > 512 {
				detail = string(chars[:511]) + "…"
			}
			body.Detail = detail
		}
		ev.Err = ""
		ev.Text = generationBody(body)
	}
	if n.Kind == swarm.NotifyTurn {
		return
	}
	if e.recordGeneration(&ev) {
		log.apply(ev)
	}
}

// A missing terminal receipt proves an unfinished generation, but a vanished
// process cannot tell us whether it crashed, was killed or lost power.
func (e *Engine) interruptOrphanedGenerations(threadID string, handoff bool) {
	e.generations.Lock()
	defer e.generations.Unlock()
	log := e.generationLog(threadID)
	if log == nil {
		return
	}
	for _, origin := range log.open {
		body := e.generationExitCause(origin, "pending", handoff)
		origin.ID, origin.Seq = 0, 0
		origin.Kind, origin.Text = "generation_interrupted", generationBody(body)
		if e.recordGeneration(&origin) {
			log.apply(origin)
		}
	}
}

// A receipt is observable only after its append succeeds. In-memory recovery
// must not replace a missing durable cause with a claimed successful write.
func (e *Engine) recordGeneration(ev *store.Event) bool {
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[ev.TurnID]; dropped {
		return false
	}
	if err := e.store.AppendEvent(ev); err != nil {
		e.log.Warn("could not persist generation lifecycle", "thread", ev.ThreadID, "kind", ev.Kind, "err", err)
		return false
	}
	e.broadcast(*ev)
	return true
}

// Stop and unavailable ownership are not pending recovery. Keep the original
// output's cause when a planned restart was recorded before restoration failed.
func (e *Engine) stopOrphanedGenerations(threadID string, human bool) {
	e.generations.Lock()
	defer e.generations.Unlock()
	log := e.generationLog(threadID)
	if log == nil {
		return
	}
	for _, origin := range log.open {
		body := e.generationExitCause(origin, "none", true)
		if human {
			body.Reason, body.Detail = "cancelled", "Generation stopped after a recorded human interruption."
		}
		origin.ID, origin.Seq = 0, 0
		origin.Kind, origin.Text = "generation_interrupted", generationBody(body)
		if e.recordGeneration(&origin) {
			log.apply(origin)
		}
	}
	for _, pending := range log.pending {
		var body swarm.GenerationInterruption
		if json.Unmarshal([]byte(pending.Text), &body) != nil || body.Recovery != "pending" {
			continue
		}
		body.Recovery = "none"
		pending.ID, pending.Seq = 0, 0
		pending.Text = generationBody(body)
		if e.recordGeneration(&pending) {
			log.apply(pending)
		}
	}
}

func (e *Engine) generationExitCause(origin store.Event, recovery string, handoff bool) swarm.GenerationInterruption {
	body := swarm.GenerationInterruption{Reason: "process_exit", Detail: "The previous process exited; the specific cause was not recorded.", Recovery: recovery}
	if handoff {
		checkpoints, err := e.store.ListEventsOfKind(origin.ThreadID, KindRestartCheckpoint, origin.Seq, 1)
		if err == nil && len(checkpoints) > 0 {
			body.Reason, body.Detail = "engine_restart", "Generation stopped during the recorded engine restart handoff."
		}
	}
	return body
}
