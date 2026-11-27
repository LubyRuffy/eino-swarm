package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func isAuxiliaryAgent(id string) bool {
	switch id {
	case TitleAgentID, CompactAgentID, SessionMemoryAgentID, ComposerPredictionAgentID:
		return true
	default:
		return false
	}
}

// record persists an event and then broadcasts it.
//
// Persisting and broadcasting happen under one lock because workers and the
// manager record concurrently: without it, an event assigned sequence 7 can
// reach subscribers before sequence 6, and a client that resumes from the
// highest sequence it has seen would discard the older one as a duplicate and
// lose it until the next reload.
func (e *Engine) record(ev store.Event) {
	if e.skipRestartAskResult(ev) {
		return
	}
	e.recordMu.Lock()
	if ev.TurnID != "" {
		if _, dropped := e.droppedTurns[ev.TurnID]; dropped {
			e.recordMu.Unlock()
			return
		}
	}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.logPersistFailure("could not persist an event", err, "thread", ev.ThreadID, "kind", ev.Kind)
		if errors.Is(err, store.ErrNotFound) {
			e.recordMu.Unlock()
			return
		}
	}
	// Store the manager answer before the event is visible. A client that
	// reacts to agent_message by reading replay (or a crash right after the
	// broadcast) would otherwise miss the on-screen text.
	if ev.Kind == swarm.NotifyAgentMessage.String() && isManagerAgent(ev.AgentID) {
		e.persistManagerAnswer(ev.ThreadID, ev.TurnID, ev.Text)
	}
	e.broadcast(ev)
	e.recordMu.Unlock()
}

func isManagerAgent(id string) bool {
	return id == "" || id == swarm.DefaultManagerID
}

func (e *Engine) persistManagerAnswer(threadID, turnID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if _, err := e.store.GetTurn(turnID); errors.Is(err, store.ErrNotFound) {
		return
	}
	if err := e.store.AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.Assistant), Content: text, AgentID: swarm.DefaultManagerID},
	}); err != nil {
		e.logPersistFailure("could not persist assistant message", err, "turn", turnID)
		return
	}
	e.reindex(threadID)
}

// emit broadcasts an event without persisting it. Used for streamed deltas:
// they are superseded within milliseconds, and the complete text is persisted
// at the end of the stream, so storing every token would grow the database
// without making replay any better. Seq stays 0 to mark them unreplayable.
func (e *Engine) emit(ev store.Event) {
	ev.CreatedAt = time.Now().UTC()
	e.broadcast(ev)
}

// ---------- transcript ----------

// persistTranscript stores the messages this turn added. The transcript the
// swarm returns starts with the system instruction and then replays the input
// messages, so only the tail past them is new.
func (e *Engine) persistTranscript(threadID, turnID string, transcript []adk.Message, inputCount int, stoppedAsks ...stoppedAskReceipt) {
	// The graph digest is added to every model request and is not part of the
	// input count; left in, it moves the tail one message early and stores the
	// last input again.
	transcript = withoutGraphDigest(transcript)
	start := min(compactPersistStart(transcript, inputCount), len(transcript))
	tail := e.stoppedAskTranscript(threadID, turnID, transcript[start:], stoppedAsks)
	if len(tail) == 0 {
		return
	}
	have := e.storedAssistantText(threadID, turnID)
	haveUsers := e.storedUserText(threadID, turnID)
	var rows []store.Message
	for _, m := range tail {
		if _, runtimeState := workerstate.Parse(m); runtimeState {
			continue
		}
		if m == nil || isRuntimeMessage(m) {
			continue
		}
		row := store.Message{
			Role:              string(m.Role),
			Content:           m.Content,
			Reasoning:         m.ReasoningContent,
			ToolCallID:        m.ToolCallID,
			WorkRecordPayload: workRecordToolResult(m),
			Source:            messageSource(m),
		}
		if len(m.ToolCalls) > 0 {
			for _, call := range m.ToolCalls {
				row.WorkRecordPayload = row.WorkRecordPayload || isWorkRecordTool(call.Function.Name)
			}
			if raw, err := json.Marshal(m.ToolCalls); err == nil {
				row.ToolCalls = string(raw)
			}
		}
		if isCompactBriefingMessage(m) {
			continue
		}
		if row.Role == string(schema.User) && strings.HasPrefix(strings.TrimSpace(row.Content), "[steer]") {
			continue // already stored by Steer
		}
		if row.Role == string(schema.User) && haveUsers[strings.TrimSpace(row.Content)] {
			continue
		}
		if row.Role == string(schema.Assistant) {
			key := strings.TrimSpace(row.Content)
			if key != "" && have[key] {
				continue // already stored with the agent_message event
			}
			if key != "" {
				have[key] = true
			}
		}
		rows = append(rows, row)
	}
	if err := e.store.AppendMessages(threadID, turnID, rows); err != nil {
		e.logPersistFailure("could not persist transcript", err, "turn", turnID)
		return
	}
	e.reindex(threadID)
}

func (e *Engine) storedAssistantText(threadID, turnID string) map[string]bool {
	if m := e.assistantTextByTurn(threadID)[turnID]; m != nil {
		return m
	}
	return map[string]bool{}
}

func (e *Engine) assistantTextByTurn(threadID string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return out
	}
	for _, m := range rows {
		if schema.RoleType(m.Role) != schema.Assistant {
			continue
		}
		key := strings.TrimSpace(m.Content)
		if key == "" {
			continue
		}
		if out[m.TurnID] == nil {
			out[m.TurnID] = map[string]bool{}
		}
		out[m.TurnID][key] = true
	}
	return out
}

// replayHistory rebuilds the conversation to hand to the next turn.
//
// It deliberately drops previous turns' tool calls and tool results and keeps
// only what was said. Tool output is the bulky part of an agent transcript —
// whole files, whole web pages — and replaying all of it costs the user tokens
// on every later turn to re-read material the assistant already summarized in
// its answer. Dropping a tool call and its results together also keeps the
// message sequence valid for providers that require the pairing.
func (e *Engine) replayHistory(threadID string) ([]adk.Message, error) {
	return e.replayHistorySkipping(threadID, "")
}
