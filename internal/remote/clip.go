package remote

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func eventChars(cfg config.RemoteConfig) int {
	if cfg.EventChars <= 0 {
		return config.DefaultRemoteEventChars
	}
	return cfg.EventChars
}

func summaryChars(cfg config.RemoteConfig) int {
	if cfg.SummaryChars <= 0 {
		return config.DefaultRemoteSummaryChars
	}
	return cfg.SummaryChars
}

func eventView(ev store.Event, cfg config.RemoteConfig) EventView {
	text := ev.Text
	switch ev.Kind {
	case "spawned":
		// Worker instructions are project/task material. The phone gets
		// the spawn notice, not the prompt body.
		text = ""
	case "tool_delta":
		text = truncate(oneLine(ev.Text), summaryChars(cfg))
	case "usage", "session_memory":
		text = truncate(oneLine(ev.Text), eventChars(cfg))
	default:
		text = truncate(ev.Text, eventChars(cfg))
	}
	return EventView{
		ThreadID:   ev.ThreadID,
		TurnID:     ev.TurnID,
		Seq:        ev.Seq,
		Kind:       ev.Kind,
		AgentID:    ev.AgentID,
		Role:       ev.Role,
		Text:       text,
		ToolCallID: ev.ToolCallID,
		Err:        ev.Err,
		HasImages:  len(ev.Images) > 0,
		CreatedAt:  ev.CreatedAt,
	}
}

func encodeEventPush(path, sessionID, threadID string, ev store.Event, cfg config.RemoteConfig) ([]byte, bool) {
	view := eventView(ev, cfg)
	for {
		raw, err := json.Marshal(Response{
			V:         ProtocolV,
			OK:        true,
			Op:        OpEvent,
			Path:      path,
			SessionID: sessionID,
			ThreadID:  threadID,
			Seq:       view.Seq,
			Event:     &view,
		})
		if err != nil {
			return nil, false
		}
		if len(raw) <= MaxPushPayload {
			return raw, true
		}
		if view.Text == "" || view.Text == "…" {
			if view.Seq == 0 {
				return nil, false
			}
			view.HasImages = false
			if view.Text == "…" {
				view.Text = ""
				continue
			}
			return nil, false
		}
		n := utf8.RuneCountInString(view.Text)
		if n <= 1 {
			view.Text = ""
			continue
		}
		view.Text = truncate(view.Text, n/2)
	}
}
