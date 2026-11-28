package remote

import (
	"encoding/json"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/slash"
)

func editInput(eng *engine.Engine, cfg config.RemoteConfig, req Request, path, sessionID string) Response {
	if strings.TrimSpace(req.ThreadID) == "" || req.FromEventSeq <= 0 {
		return fail(req.ID, path, sessionID, "bad_request", "thread_id and a positive from_event_seq are required")
	}
	ev, err := eng.Store().GetEvent(req.ThreadID, req.FromEventSeq)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if ev.Kind != engine.KindUser {
		return fail(req.ID, path, sessionID, "bad_request", engine.ErrNotRewindable.Error())
	}
	resp := okBase(req.ID, path, sessionID)
	if req.Replacement == nil {
		// Watch/log deliberately clip even short prompts under some settings.
		// An editable draft needs all its text, including quotes and file paths.
		resp.Event = &EventView{
			ThreadID: ev.ThreadID, TurnID: ev.TurnID, Seq: ev.Seq, Kind: ev.Kind,
			Text: ev.Text, HasImages: len(ev.Images) > 0, CreatedAt: ev.CreatedAt,
		}
		if raw, err := json.Marshal(resp); err != nil || len(raw) > plaintextBudget() {
			return fail(req.ID, path, sessionID, "too_large", "the original input is too large to edit on the phone")
		}
		return resp
	}
	// Commands may change goal/plan state or return without starting a turn.
	// This RPC promises a replacement turn, so only its composer can run them.
	if _, command := slash.Lookup(*req.Replacement, "image", "plan", "goal"); command {
		return fail(req.ID, path, sessionID, "edit_input_command", "edit and resend requires a chat message")
	}
	_, err = eng.StartTurnInput(req.ThreadID, engine.UserInput{
		Text: *req.Replacement, FromEventSeq: ev.Seq,
	})
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	return resp
}
