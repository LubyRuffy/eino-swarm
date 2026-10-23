package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// resumeWorkersCue is appended for the manager only when leftover workers
// are being restored. It must stay task-agnostic.
const resumeWorkersCue = "Sub-agents that were still running have been restarted under their existing ids. Wait for those rather than spawning replacements. Finished workers remain available under the same ids; they are already stopped, so do not close_agent them."

// cleanedUpWorkerErr is how a worker that Cleanup killed looks after a
// restart. Distinct from a crash: that worker was not left running.
const cleanedUpWorkerErr = "stopped at the end of the turn"

func (rt *runtime) isAbandoned() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.abandoned
}

func (e *Engine) isAbandoned(threadID string) bool {
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return false
	}
	return rt.isAbandoned()
}

func (rt *runtime) setWorkerRestore(running []swarm.RestoredWorker, finished []swarm.FinishedWorker) {
	rt.mu.Lock()
	rt.restore, rt.planted = running, finished
	rt.mu.Unlock()
}

func (rt *runtime) takeWorkerRestore() ([]swarm.RestoredWorker, []swarm.FinishedWorker) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	running, finished := rt.restore, rt.planted
	rt.restore, rt.planted = nil, nil
	return running, finished
}

func (e *Engine) workersFromTurn(turn *store.Turn) ([]swarm.RestoredWorker, []swarm.FinishedWorker) {
	if turn == nil {
		return nil, nil
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return nil, nil
	}
	return orphanedWorkers(events)
}

// workersFromThread is the restart roster: later turns drop previous tool
// results, and a new registry after a process death has none of the ids.
// Conversation events still know who was live. A /goal continuation that
// only looked at the new empty turn made wait_agents report unknown.
func (e *Engine) workersFromThread(threadID string) ([]swarm.RestoredWorker, []swarm.FinishedWorker) {
	running, finished, _ := e.readWorkersFromThread(threadID)
	return running, finished
}

func (e *Engine) readWorkersFromThread(threadID string) ([]swarm.RestoredWorker, []swarm.FinishedWorker, error) {
	if e == nil || e.store == nil || strings.TrimSpace(threadID) == "" {
		return nil, nil, nil
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	running, finished := orphanedWorkers(events)
	return running, finished, nil
}

// attachLeftoverWorkers plants leftover ids on a fresh registry and, when
// the manager needs their identities, adds an explicit runtime state snapshot.
// Later turns drop tool results; identity must survive without replaying
// historical workers as new tool executions.
func (rt *runtime) attachLeftoverWorkers(reused, pin bool, live int, messages []adk.Message) []adk.Message {
	if rt == nil || rt.engine == nil {
		return messages
	}
	running, finished, err := rt.engine.readWorkersFromThread(rt.threadID)
	if err != nil {
		return messages
	}
	if !reused {
		rt.setWorkerRestore(running, finished)
	}
	if pin {
		messages = appendWorkerRoster(messages, running, finished)
	}
	switch {
	case reused && live > 0:
		if !hasUserContent(messages, parkedWorkersCue) {
			messages = append(messages, schema.UserMessage(parkedWorkersCue))
		}
	case !reused && len(running) > 0:
		if !hasUserContent(messages, resumeWorkersCue) {
			messages = append(messages, schema.UserMessage(resumeWorkersCue))
		}
	}
	return messages
}

func appendWorkerRoster(msgs []adk.Message, running []swarm.RestoredWorker, finished []swarm.FinishedWorker) []adk.Message {
	roster := workerRosterMessages(running, finished)
	messages := withoutLegacyWorkerPairs(workerstate.Without(msgs), roster)
	leading := 0
	for leading < len(messages) && messages[leading] != nil && messages[leading].Role == schema.System {
		leading++
	}
	out := append([]adk.Message{}, messages[:leading]...)
	out = append(out, roster...)
	return append(out, messages[leading:]...)
}

type workerSlot struct {
	role, instruction, result, err string
	running, spawned, inflight     bool
	seed                           strings.Builder
	nseed                          int
	timeoutFailures                int
	history                        []adk.Message
	restoredHistory                []*schema.Message
	inbox                          []string
}

func orphanedWorkers(events []store.Event) (running []swarm.RestoredWorker, finished []swarm.FinishedWorker) {
	byID := map[string]*workerSlot{}
	order := make([]string, 0)
	note := func(id string) *workerSlot {
		s, ok := byID[id]
		if !ok {
			s = &workerSlot{}
			byID[id] = s
			order = append(order, id)
		}
		return s
	}
	if checkpoint, index := latestRestartCheckpoint(events); checkpoint != nil {
		for _, w := range checkpoint.Workers {
			s := note(w.ID)
			s.role, s.instruction, s.spawned, s.running = w.Role, w.Instruction, true, !w.Finished
			s.result, s.err, s.timeoutFailures = w.Result, w.Err, w.TimeoutFailures
			if checkpoint.HumanStopped {
				s.running = false
				s.err = "interrupted"
			}
			s.restoredHistory, s.inbox = w.History, w.Inbox
			s.history = append(s.history, w.History...)
			if w.Summary != "" {
				fmt.Fprintf(&s.seed, "handoff summary: %s\n", w.Summary)
				s.nseed++
			}
			if len(w.InFlight) > 0 {
				body, _ := json.Marshal(w.InFlight)
				fmt.Fprintf(&s.seed, "The following tool operations have unknown outcomes. Inspect actual state or a receipt before deciding whether retry is safe; never assume they failed: %s\n", body)
				s.nseed++
				s.inflight = true
			}
		}
		events = events[index+1:]
	}
	for _, ev := range events {
		if ev.Kind == KindUser || (ev.Kind == KindGoalResumed && ev.Role == "human") {
			for _, s := range byID {
				s.timeoutFailures = 0
			}
		}
		if ev.Kind == KindCleanup {
			// Cleanup does not emit finished per worker. Without this a
			// later turn would Restore killed agents as still running.
			for _, id := range order {
				s := byID[id]
				if s == nil || !s.spawned || !s.running {
					continue
				}
				s.running = false
				s.err = cleanedUpWorkerErr
				// A refused legacy Restore has no new cancellation to reset its budget.
				if s.timeoutFailures < swarm.MaxConsecutiveWorkerTimeouts {
					s.timeoutFailures = 0
				}
				s.inflight = false
			}
			continue
		}
		if isManagerAgent(ev.AgentID) {
			continue
		}
		s := note(ev.AgentID)
		switch ev.Kind {
		case swarm.NotifySpawned.String():
			s.spawned = true
			s.running = true
			s.role = strings.TrimSpace(ev.Role)
			if s.role == "" {
				s.role = strings.TrimSpace(ev.Text)
			}
			s.instruction = ev.Text
			s.result, s.err = "", ""
			s.inflight = false
		case swarm.NotifyFinished.String():
			s.running = false
			s.result = ev.Text
			s.err = ev.Err
			// Existing persisted watchdog receipts have this exact per-worker prefix.
			// Network/idle deadlines and user cancellations never count as watchdogs.
			if strings.HasPrefix(ev.Err, "swarm: agent "+ev.AgentID+" exceeded timeout ") {
				s.timeoutFailures++
			} else {
				s.timeoutFailures = 0
			}
			s.inflight = false
			if s.role == "" {
				s.role = strings.TrimSpace(ev.Role)
			}
		case swarm.NotifyAgentMessage.String():
			if t := strings.TrimSpace(ev.Text); t != "" {
				fmt.Fprintf(&s.seed, "assistant: %s\n", t)
				s.nseed++
				s.history = append(s.history, schema.AssistantMessage(t, nil))
				if s.restoredHistory != nil {
					s.restoredHistory = append(s.restoredHistory, schema.AssistantMessage(t, nil))
				}
			}
			s.inflight = false
		case swarm.NotifyToolResult.String():
			if t := strings.TrimSpace(ev.Text); t != "" {
				fmt.Fprintf(&s.seed, "tool: %s\n", t)
				s.nseed++
			}
			if s.restoredHistory != nil {
				s.restoredHistory = append(s.restoredHistory, schema.ToolMessage(ev.Text, ev.ToolCallID))
			}
			s.inflight = false
		case swarm.NotifyToolCall.String():
			s.inflight = true
			if s.restoredHistory != nil {
				name, args := splitToolCallText(ev.Text)
				s.restoredHistory = append(s.restoredHistory, schema.AssistantMessage("", []schema.ToolCall{{ID: ev.ToolCallID, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}))
			}
		}
	}
	for _, id := range order {
		s := byID[id]
		if !s.spawned {
			continue
		}
		if s.running {
			w := swarm.RestoredWorker{
				ID: id, Role: s.role, Instruction: s.instruction, Task: resumeCue, TimeoutFailures: s.timeoutFailures,
				History: s.restoredHistory, Inbox: s.inbox,
			}
			if s.nseed > 0 {
				w.Seed = "conversation so far:\n" + s.seed.String()
			}
			if s.inflight {
				if w.Seed != "" {
					w.Seed += "\n"
				}
				w.Seed += "a tool call was in flight when the process stopped; check the workspace rather than repeating completed work."
			}
			running = append(running, w)
			continue
		}
		var ferr error
		if s.err != "" {
			ferr = errors.New(s.err)
		}
		finished = append(finished, swarm.FinishedWorker{
			ID: id, Role: s.role, Result: s.result, Err: ferr, History: s.history, TimeoutFailures: s.timeoutFailures,
		})
	}
	return running, finished
}

func dropTrailingIncompleteToolCalls(msgs []adk.Message) []adk.Message {
	lastAsst, needed := trailingIncompleteToolCalls(msgs)
	if lastAsst < 0 || len(needed) == 0 {
		return msgs
	}
	out := append([]adk.Message{}, msgs[:lastAsst]...)
	for _, m := range msgs[lastAsst+1:] {
		if isSteerUser(m) {
			out = append(out, m)
		}
	}
	return out
}

func isSteerUser(m adk.Message) bool {
	return m != nil && m.Role == schema.User && strings.HasPrefix(userMessageText(m), "[steer]")
}

func (e *Engine) resumeConversation(turn *store.Turn) ([]adk.Message, error) {
	prior, err := e.replayHistorySkipping(turn.ThreadID, turn.ID)
	if err != nil {
		return nil, err
	}
	live, err := e.thisTurnMessages(turn)
	if err != nil {
		return nil, err
	}
	if events, err := e.store.ListTurnEvents(turn.ID); err == nil {
		if checkpoint, index := latestRestartCheckpoint(events); checkpoint != nil && len(checkpoint.ManagerHistory) > 0 {
			prior = nil
			live = checkpointManagerMessages(checkpoint, events, index)
		}
	}
	running, finished := e.workersFromThread(turn.ThreadID)
	live = sealTrailingIncompleteToolCalls(live, running, finished)
	combined := append(prior, live...)
	text := strings.TrimSpace(turn.UserText)
	if text == "" && !hasUserMessage(combined) {
		return nil, fmt.Errorf("engine: leftover turn has no request to continue")
	}
	// Keep all resumable identities as explicit state; completed workers must
	// not look like newly executed spawn calls after a restart.
	combined = appendWorkerRoster(combined, running, finished)
	msgs := resumeMessages(combined, text)
	if events, err := e.store.ListTurnEvents(turn.ID); err == nil {
		for _, call := range orphanedToolCalls(events) {
			name := toolCallName(call.Text)
			if name != waitAgentsToolName && name != ToolAskUser {
				msgs = append(msgs, schema.UserMessage("This tool has an unknown outcome after restart. Inspect actual state or an external receipt before deciding whether retry is safe; do not assume failure: "+call.Text))
			}
		}
	}
	if len(running) > 0 && !hasUserContent(msgs, resumeWorkersCue) {
		msgs = append(msgs, schema.UserMessage(resumeWorkersCue))
	}
	return e.appendInterruptedWorkerCue(turn.ThreadID, msgs), nil
}

func hasUserContent(msgs []adk.Message, text string) bool {
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && m.Content == text {
			return true
		}
	}
	return false
}

func (e *Engine) thisTurnMessages(turn *store.Turn) ([]adk.Message, error) {
	rows, err := e.store.ListMessages(turn.ThreadID)
	if err != nil {
		return nil, err
	}
	skipThrough := e.compactWatermark(turn.ThreadID)
	out := make([]adk.Message, 0, len(rows))
	seen := map[string]struct{}{}
	hasTools := false
	events, evErr := e.store.ListTurnEvents(turn.ID)
	retracted := retractedSteerSeqs(events)
	captions := retractedSteerCaptionsFrom(events)
	for _, r := range rows {
		if r.TurnID != turn.ID {
			continue
		}
		if skipRetractedSteer(r, retracted, captions) {
			continue
		}
		if skipThrough > 0 && r.Seq <= skipThrough {
			if key := strings.TrimSpace(r.Content); key != "" {
				seen[key] = struct{}{}
			}
			continue
		}
		if r.Role == string(schema.User) && isEngineOnlyUser(r.Content) {
			continue
		}
		msg := e.storedToSchema(r)
		if msg == nil {
			continue
		}
		if schema.RoleType(r.Role) == schema.Tool || strings.TrimSpace(r.ToolCalls) != "" {
			hasTools = true
		}
		out = append(out, msg)
		if key := strings.TrimSpace(r.Content); key != "" {
			seen[key] = struct{}{}
		}
	}
	if evErr != nil {
		return out, nil
	}
	kind := swarm.NotifyAgentMessage.String()
	for _, ev := range events {
		if ev.Kind != kind || !isManagerAgent(ev.AgentID) {
			continue
		}
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, schema.AssistantMessage(text, nil))
	}
	if !hasTools {
		out = append(out, workerRosterFromEvents(events)...)
	}
	return out, nil
}

func (e *Engine) storedToSchema(r store.Message) adk.Message {
	switch schema.RoleType(r.Role) {
	case schema.User:
		if strings.TrimSpace(r.Content) == "" && len(r.Images) == 0 {
			return nil
		}
		return e.schemaUser(r)
	case schema.Tool:
		return &schema.Message{Role: schema.Tool, Content: r.Content, ToolCallID: r.ToolCallID}
	case schema.Assistant:
		if strings.TrimSpace(r.Content) == "" && strings.TrimSpace(r.ToolCalls) == "" {
			return nil
		}
		msg := schema.AssistantMessage(r.Content, nil)
		msg.ReasoningContent = r.Reasoning
		if strings.TrimSpace(r.ToolCalls) != "" {
			_ = json.Unmarshal([]byte(r.ToolCalls), &msg.ToolCalls)
		}
		return msg
	default:
		return nil
	}
}

func workerRosterFromEvents(events []store.Event) []adk.Message {
	running, finished := orphanedWorkers(events)
	return workerRosterMessages(running, finished)
}
