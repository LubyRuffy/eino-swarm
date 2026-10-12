package remote

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A keyset cursor survives reading/deleting the boundary task. Offset and
// ID-only cursors can skip unseen work or restart forever when that row leaves.
type attentionCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func attentionStart(rows []store.Thread, cursor string) (int, error) {
	if cursor == "" || cursor == "first" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	var after attentionCursor
	if err := json.Unmarshal(raw, &after); err != nil {
		return 0, err
	}
	if after.At.IsZero() || after.ID == "" {
		return 0, fmt.Errorf("invalid attention cursor")
	}
	for i, row := range rows {
		if row.LastActiveAt.Before(after.At) || (row.LastActiveAt.Equal(after.At) && row.ID < after.ID) {
			return i, nil
		}
	}
	return len(rows), nil
}

func packAttention(eng *engine.Engine, cfg config.RemoteConfig, req Request, resp Response) Response {
	rows, err := eng.Store().ListUnreadThreads()
	if err != nil {
		return mapErr(req.ID, resp.Path, resp.SessionID, err)
	}
	start, err := attentionStart(rows, req.Cursor)
	if err != nil {
		return fail(req.ID, resp.Path, resp.SessionID, "bad_request", "invalid attention cursor")
	}
	if raw, err := json.Marshal(resp); err != nil || len(raw) > plaintextBudget() {
		return fail(req.ID, resp.Path, resp.SessionID, "too_large", "inbox metadata exceeds frame budget")
	}
	fit := resp
	for i := start; i < len(rows); i++ {
		resp.Attention = append(resp.Attention, threadView(eng, rows[i], cfg))
		resp.AttentionMore = i+1 < len(rows)
		resp.AttentionCursor = ""
		if resp.AttentionMore {
			raw, _ := json.Marshal(attentionCursor{At: rows[i].LastActiveAt, ID: rows[i].ID})
			resp.AttentionCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		raw, err := json.Marshal(resp)
		if err != nil || len(raw) > plaintextBudget() {
			if len(fit.Attention) > 0 {
				return fit
			}
			if req.Group == GroupAttention {
				return fail(req.ID, resp.Path, resp.SessionID, "too_large", "completed task exceeds frame budget")
			}
			// The existing inbox metadata may fill page one. A dedicated More
			// can still fetch attention without retransmitting those sections.
			fit.AttentionMore, fit.AttentionCursor = true, "first"
			raw, err = json.Marshal(fit)
			if err != nil || len(raw) > plaintextBudget() {
				return fail(req.ID, resp.Path, resp.SessionID, "too_large", "inbox metadata exceeds frame budget")
			}
			return fit
		}
		fit = resp
	}
	return fit
}
