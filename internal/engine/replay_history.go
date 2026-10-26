package engine

import (
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func (e *Engine) replayHistorySkipping(threadID, skipTurnID string) ([]adk.Message, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	through := int64(0)
	if strings.TrimSpace(th.CompactSummary) != "" {
		through = th.CompactThroughSeq
	}
	rows, err := e.store.ListReplayMessages(threadID, through, skipTurnID)
	if err != nil {
		return nil, err
	}
	rows, err = e.withoutRuntimeRows(rows)
	if err != nil {
		return nil, err
	}
	events, err := e.store.ListReplayEvents(threadID, through, skipTurnID)
	if err != nil {
		return nil, err
	}
	out := make([]adk.Message, 0, len(rows))
	seen := map[string]struct{}{}
	retracted := retractedSteerSeqs(events)
	captions := retractedSteerCaptionsFrom(events)
	for _, r := range rows {
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
	return e.appendMissingEventAnswers(threadID, events, seen, out)
}

// appendMissingEventAnswers consumes the same filtered event read as steering
// replay. The Store already checks live turn identity and all stored assistant
// text, including compacted rows, without reading tool bodies into the engine.
func (e *Engine) appendMissingEventAnswers(threadID string, events []store.Event, seen map[string]struct{}, out []adk.Message) ([]adk.Message, error) {
	for _, ev := range events {
		if ev.Kind != swarm.NotifyAgentMessage.String() {
			continue
		}
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		out = append(out, schema.AssistantMessage(ev.Text, nil))
		seen[text] = struct{}{}
		e.persistManagerAnswer(threadID, ev.TurnID, ev.Text)
	}
	return out, nil
}
