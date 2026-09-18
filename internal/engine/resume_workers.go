package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// resumeWorkersCue is appended for the manager only when leftover workers
// are being restored. It must stay task-agnostic.
const resumeWorkersCue = "Sub-agents that were still running have been restarted under their existing ids. Wait for those rather than spawning replacements. Finished workers remain available under the same ids."

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
	if e == nil || e.store == nil || strings.TrimSpace(threadID) == "" {
		return nil, nil
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil, nil
	}
	return orphanedWorkers(events)
}

// attachLeftoverWorkers plants leftover ids on a fresh registry and, when
// the manager is expected to wait on them, pins spawn pairs back into the
// prompt. Later turns drop tool results, so without the pin the model (and
// the scripted manager) would mint twins or wait_agents unknown.
func (rt *runtime) attachLeftoverWorkers(reused, pin bool, live int, messages []adk.Message) []adk.Message {
	if rt == nil || rt.engine == nil {
		return messages
	}
	running, finished := rt.engine.workersFromThread(rt.threadID)
	if !reused {
		rt.setWorkerRestore(running, finished)
	}
	if pin {
		messages = appendWorkerPairs(messages, running, finished)
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

func appendWorkerPairs(msgs []adk.Message, running []swarm.RestoredWorker, finished []swarm.FinishedWorker) []adk.Message {
	pairs := pinWorkerPairs(running, finished)
	if len(pairs) == 0 {
		return msgs
	}
	tail := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		if m != nil {
			tail = append(tail, m)
		}
	}
	for _, m := range dropPairsAlreadyIn(pairs, tail) {
		msgs = append(msgs, m)
	}
	return msgs
}

type workerSlot struct {
	role, instruction, result, err string
	running, spawned, inflight     bool
	seed                           strings.Builder
	nseed                          int
	history                        []adk.Message
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
	for _, ev := range events {
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
			s.inflight = false
			if s.role == "" {
				s.role = strings.TrimSpace(ev.Role)
			}
		case swarm.NotifyAgentMessage.String():
			if t := strings.TrimSpace(ev.Text); t != "" {
				fmt.Fprintf(&s.seed, "assistant: %s\n", t)
				s.nseed++
				s.history = append(s.history, schema.AssistantMessage(t, nil))
			}
			s.inflight = false
		case swarm.NotifyToolResult.String():
			if t := strings.TrimSpace(ev.Text); t != "" {
				fmt.Fprintf(&s.seed, "tool: %s\n", t)
				s.nseed++
			}
			s.inflight = false
		case swarm.NotifyToolCall.String():
			s.inflight = true
		}
	}
	for _, id := range order {
		s := byID[id]
		if !s.spawned {
			continue
		}
		if s.running {
			w := swarm.RestoredWorker{
				ID: id, Role: s.role, Instruction: s.instruction, Task: resumeCue,
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
			ID: id, Role: s.role, Result: s.result, Err: ferr, History: s.history,
		})
	}
	return running, finished
}

func dropTrailingIncompleteToolCalls(msgs []adk.Message) []adk.Message {
	lastAsst := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || isSteerUser(msgs[i]) {
			continue
		}
		if msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) > 0 {
			lastAsst = i
			break
		}
		if msgs[i].Role == schema.User || (msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) == 0) {
			return msgs
		}
	}
	if lastAsst < 0 {
		return msgs
	}
	needed := map[string]struct{}{}
	for _, tc := range msgs[lastAsst].ToolCalls {
		if tc.ID != "" {
			needed[tc.ID] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return msgs
	}
	for _, m := range msgs[lastAsst+1:] {
		if m != nil && m.Role == schema.Tool {
			delete(needed, m.ToolCallID)
		}
	}
	if len(needed) == 0 {
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

func (e *Engine) replayHistorySkipping(threadID, skipTurnID string) ([]adk.Message, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return nil, err
	}
	skipThrough := int64(0)
	if strings.TrimSpace(th.CompactSummary) != "" {
		skipThrough = th.CompactThroughSeq
	}
	out := make([]adk.Message, 0, len(rows))
	liveTurns := map[string]struct{}{}
	seen := map[string]struct{}{}
	events, _ := e.store.ListEvents(threadID, 0, 0)
	retracted := retractedSteerSeqs(events)
	captions := retractedSteerCaptionsFrom(events)
	for _, r := range rows {
		if skipTurnID != "" && r.TurnID == skipTurnID {
			continue
		}
		if skipThrough > 0 && r.Seq <= skipThrough {
			continue
		}
		liveTurns[r.TurnID] = struct{}{}
		if skipRetractedSteer(r, retracted, captions) {
			continue
		}
		switch schema.RoleType(r.Role) {
		case schema.User:
			if strings.TrimSpace(r.Content) != "" || len(r.Images) > 0 {
				out = append(out, e.schemaUser(r))
				if key := strings.TrimSpace(r.Content); key != "" {
					seen[key] = struct{}{}
				}
			}
		case schema.Assistant:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, schema.AssistantMessage(r.Content, nil))
				seen[strings.TrimSpace(r.Content)] = struct{}{}
			}
		}
	}
	return e.appendMissingEventAnswers(threadID, liveTurns, seen, out)
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
	live = dropTrailingIncompleteToolCalls(live)
	combined := append(prior, live...)
	text := strings.TrimSpace(turn.UserText)
	if text == "" && !hasUserMessage(combined) {
		return nil, fmt.Errorf("engine: leftover turn has no request to continue")
	}
	running, finished := e.workersFromThread(turn.ThreadID)
	combined = appendWorkerPairs(combined, running, finished)
	msgs := resumeMessages(combined, text)
	if len(running)+len(finished) > 0 && !hasUserContent(msgs, resumeWorkersCue) {
		msgs = append(msgs, schema.UserMessage(resumeWorkersCue))
	}
	return msgs, nil
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
		if r.Role == string(schema.User) && (r.Content == resumeCue || r.Content == resumeWorkersCue || r.Content == parkedWorkersCue) {
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
		out = append(out, spawnPairsFromEvents(events)...)
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

func spawnPairsFromEvents(events []store.Event) []adk.Message {
	out := make([]adk.Message, 0)
	for i, ev := range events {
		if ev.Kind != swarm.NotifySpawned.String() || isManagerAgent(ev.AgentID) {
			continue
		}
		callID := fmt.Sprintf("restore-spawn-%d", i)
		role := strings.TrimSpace(ev.Role)
		args, _ := json.Marshal(map[string]string{"role": role})
		out = append(out, schema.AssistantMessage("", []schema.ToolCall{{
			ID: callID, Type: "function",
			Function: schema.FunctionCall{Name: "spawn_agent", Arguments: string(args)},
		}}))
		body, _ := json.Marshal(map[string]string{"agent_id": ev.AgentID})
		out = append(out, &schema.Message{Role: schema.Tool, Content: string(body), ToolCallID: callID})
	}
	return out
}
