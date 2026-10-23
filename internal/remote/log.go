package remote

import (
	"encoding/json"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// lastTurnWindow is a live-edge page: the latest turn, newest-capped
// at `cap`. First watch uses a short cap so the phone is not stuck
// packing a pursuing turn; `log` still pages watch_events.
// Threads with no turns fall back to a live-edge page of raw events.
func lastTurnWindow(st *store.Store, threadID string, cap int) ([]store.Event, bool, error) {
	if cap <= 0 {
		cap = config.DefaultRemoteWatchEvents
	}
	turns, err := st.ListTurns(threadID)
	if err != nil {
		return nil, false, err
	}
	if len(turns) == 0 {
		return st.ListTailEvents(threadID, 0, cap)
	}
	page, moreTurn, err := st.ListTailEventsByTurn(threadID, turns[len(turns)-1].ID, cap)
	if err != nil {
		return nil, false, err
	}
	if len(page) == 0 {
		// Last turn has no rows yet. Do not paint an older turn as if it were live.
		any, _, err := st.ListTailEvents(threadID, 0, 1)
		return nil, len(any) > 0, err
	}
	tail := page[len(page)-1].Seq
	extra, err := st.ListEvents(threadID, tail, cap)
	if err != nil {
		return nil, false, err
	}
	page = append(page, extra...)
	hasMore := moreTurn
	if !hasMore && page[0].Seq > 1 {
		hasMore = true
	}
	if len(page) > cap {
		page = page[len(page)-cap:]
		hasMore = true
	}
	return page, hasMore, nil
}

func handleLog(eng *engine.Engine, cfg config.RemoteConfig, req Request, path, sessionID string) Response {
	tid := strings.TrimSpace(req.ThreadID)
	if tid == "" {
		return fail(req.ID, path, sessionID, "bad_request", "thread_id required")
	}
	if _, err := eng.Store().GetThread(tid); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	before := req.Before
	limit := watchEvents(cfg)
	if before <= 0 {
		window, _, err := lastTurnWindow(eng.Store(), tid, limit)
		if err != nil {
			return fail(req.ID, path, sessionID, "", fmtErr(err))
		}
		if len(window) == 0 {
			page, more, err := eng.Store().ListTailEvents(tid, 0, limit)
			if err != nil {
				return fail(req.ID, path, sessionID, "", fmtErr(err))
			}
			return packLogEvents(req.ID, path, sessionID, tid, page, more, cfg)
		}
		before = window[0].Seq
	}
	var page []store.Event
	hasMore := true
	for hasMore {
		var err error
		page, hasMore, err = eng.Store().ListTailEvents(tid, before, limit)
		if err != nil {
			return fail(req.ID, path, sessionID, "", fmtErr(err))
		}
		if len(page) == 0 || hasPhoneContent(page) {
			break
		}
		before = page[0].Seq
	}
	return packLogEvents(req.ID, path, sessionID, tid, page, hasMore, cfg)
}

// A tool result without its call cannot paint a row. The phone also hides
// subagent-management calls, which can span thousands of stored events.
// Keep scanning locally so one Earlier tap reaches actual conversation text.
func hasPhoneContent(events []store.Event) bool {
	for _, ev := range events {
		if !shouldPush(ev.Kind) || ev.Kind == "tool_result" || ev.Kind == "engine_restart_checkpoint" || ev.Kind == engine.KindDesktopRecovery {
			continue
		}
		if ev.Kind == "model_retry" && !phoneRetryNoticeVisible(ev.Text) {
			continue
		}
		if ev.Kind == "max_iterations" || ev.Kind == "max_iterations_continued" || ev.Kind == "compacted" {
			body := strings.TrimSpace(ev.Text)
			if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") {
				continue
			}
		}
		if ev.Kind == "tool_call" {
			name, _, _ := strings.Cut(strings.TrimSpace(ev.Text), "(")
			if name == "spawn_agent" || name == "close_agent" {
				continue
			}
		}
		return true
	}
	return false
}

// Keep this protocol enum aligned with mobile's modelRetryNotice. Unknown
// envelopes render no phone block and must not strand Earlier on an empty page.
func phoneRetryNoticeVisible(text string) bool {
	var payload struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal([]byte(text), &payload) != nil {
		return false
	}
	switch payload.Reason {
	case "workers_uncollected", "schedule_claim_unverified", "context_overflow", "invalid_tool_json", "rate_limited",
		"model_unavailable", "model_response_invalid", "model_transport", "model_idle":
		return true
	default:
		return false
	}
}

func packLogEvents(id, path, sessionID, threadID string, events []store.Event, more bool, cfg config.RemoteConfig) Response {
	resp := okBase(id, path, sessionID)
	resp.ThreadID = threadID
	resp.More = more
	if len(events) > 0 {
		// Oldest seq of this store page. Empty (filtered) pages still need
		// a cursor so the phone can ask again instead of killing hasMore.
		resp.Seq = events[0].Seq
	}
	views := make([]EventView, 0, len(events))
	for _, ev := range events {
		if !shouldPush(ev.Kind) {
			continue
		}
		views = append(views, eventView(ev, cfg))
	}
	for len(views) > 0 {
		resp.Events = views
		raw, err := json.Marshal(resp)
		if err != nil {
			resp.Events = nil
			return resp
		}
		if len(raw) <= plaintextBudget() {
			return resp
		}
		views = views[1:]
		resp.More = true
	}
	resp.Events = views
	return resp
}
