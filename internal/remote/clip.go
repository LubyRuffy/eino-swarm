package remote

import (
	"encoding/json"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
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

func watchEvents(cfg config.RemoteConfig) int {
	if cfg.WatchEvents <= 0 {
		return config.DefaultRemoteWatchEvents
	}
	return cfg.WatchEvents
}

// First watch is a tail page, not the whole last turn. 80 rows of a
// pursuing turn is a multi-second pairlink frame; pull-up still uses
// watch_events.
func watchOpenEvents(cfg config.RemoteConfig) int {
	n := watchEvents(cfg)
	if n > config.DefaultRemoteWatchOpen {
		return config.DefaultRemoteWatchOpen
	}
	return n
}

func eventView(ev store.Event, cfg config.RemoteConfig) EventView {
	text := ev.Text
	switch ev.Kind {
	case "spawned", engine.KindRestartCheckpoint:
		// Worker instructions are project/task material. The phone gets
		// the spawn notice, not the prompt body.
		text = ""
	case "generation_interrupted", "generation_recovered":
		text = clipGenerationCause(ev.Text, eventChars(cfg))
	case "tool_delta":
		text = truncate(oneLine(ev.Text), summaryChars(cfg))
	case "usage", "session_memory":
		text = truncate(oneLine(ev.Text), eventChars(cfg))
	default:
		text = truncate(ev.Text, eventChars(cfg))
	}
	return EventView{
		SourceThreadID: ev.SourceThreadID,
		SourceTurnID:   ev.SourceTurnID,
		ThreadID:       ev.ThreadID,
		TurnID:         ev.TurnID,
		Seq:            ev.Seq,
		Kind:           ev.Kind,
		AgentID:        ev.AgentID,
		Role:           ev.Role,
		Text:           text,
		ToolCallID:     ev.ToolCallID,
		MessageID:      ev.MessageID,
		Err:            ev.Err,
		HasImages:      len(ev.Images) > 0,
		CreatedAt:      ev.CreatedAt,
	}
}

func encodeEventPush(path, sessionID, threadID string, ev store.Event, cfg config.RemoteConfig) ([]byte, bool) {
	return encodeEventPushStatus(path, sessionID, threadID, ev, cfg, nil)
}

func encodeEventPushStatus(path, sessionID, threadID string, ev store.Event, cfg config.RemoteConfig, st *WatchStatus) ([]byte, bool) {
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
			Status:    st,
		})
		if err != nil {
			return nil, false
		}
		if len(raw) <= plaintextBudget() {
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

// The reason and recovery fields are protocol data. Clip only the bounded
// explanation; truncating their raw JSON makes a phone lose the entire cause.
func clipGenerationCause(text string, limit int) string {
	var body swarm.GenerationInterruption
	if json.Unmarshal([]byte(text), &body) != nil {
		return text
	}
	limit = min(512, max(1, limit))
	if chars := []rune(body.Detail); len(chars) > limit {
		body.Detail = string(chars[:limit-1]) + "…"
	}
	clipped, _ := json.Marshal(body)
	return string(clipped)
}
