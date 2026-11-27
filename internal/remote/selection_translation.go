package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// handleSelectionTranslation reuses the PC's model policy without recording a turn.
func handleSelectionTranslation(ctx context.Context, eng *engine.Engine, req Request, path, sessionID string) Response {
	if req.V != 0 && req.V != ProtocolV {
		return fail(req.ID, path, sessionID, "bad_version", "unsupported protocol version")
	}
	if err := requireStartupReady(eng, OpTranslateSelection); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	text, err := eng.TranslateSelection(ctx, req.ThreadID, req.Text)
	if err != nil {
		code := "selection_translation_unavailable"
		switch {
		case errors.Is(err, engine.ErrSelectionTranslationDisabled):
			return fail(req.ID, path, sessionID, "selection_translation_disabled", "Selection translation is switched off.")
		case errors.Is(err, engine.ErrInvalidSelectionTranslation):
			code = "bad_request"
		case errors.Is(err, engine.ErrSelectionTranslationTooLong):
			code = "selection_translation_too_long"
		case errors.Is(err, store.ErrNotFound):
			code = "not_found"
		case errors.Is(err, context.DeadlineExceeded):
			code = "selection_translation_timeout"
		case errors.Is(err, context.Canceled):
			code = "selection_translation_cancelled"
		}
		return fail(req.ID, path, sessionID, code, "The translation could not be completed.")
	}
	resp := okBase(req.ID, path, sessionID)
	resp.Translation = text
	// JSON escaping can expand an otherwise bounded result beyond a sealed frame.
	raw, _ := json.Marshal(resp)
	if len(raw) > plaintextBudget() {
		return fail(req.ID, path, sessionID, "selection_translation_too_long", "The translation does not fit one frame.")
	}
	return resp
}

// One cancellable translation per link keeps model latency off the receive loop.
func (p *LinkPump) startSelectionTranslation(req Request) {
	p.translationMu.Lock()
	defer p.translationMu.Unlock()
	if p.translationClosed {
		return
	}
	if req.ID == "" || len(req.ID) > 256 {
		p.reply(fail("", p.path, p.sessionID, "bad_request", "request id required"))
		return
	}
	if p.translationCancel != nil {
		p.reply(fail(req.ID, p.path, p.sessionID, "selection_translation_busy", "A translation is already running."))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.translationID, p.translationCancel = req.ID, cancel
	p.translationWG.Add(1)
	go func() {
		defer p.translationWG.Done()
		defer cancel()
		resp := handleSelectionTranslation(ctx, p.eng, req, p.path, p.sessionID)
		p.translationMu.Lock()
		defer p.translationMu.Unlock()
		p.translationID, p.translationCancel = "", nil
		if !p.translationClosed {
			p.reply(resp)
		}
	}()
}

func (p *LinkPump) cancelSelectionTranslation(req Request) {
	p.translationMu.Lock()
	defer p.translationMu.Unlock()
	if req.CallID != "" && req.CallID == p.translationID && p.translationCancel != nil {
		p.translationCancel()
	}
	p.reply(okBase(req.ID, p.path, p.sessionID))
}

func (p *LinkPump) stopSelectionTranslation() {
	p.translationMu.Lock()
	p.translationClosed = true
	if p.translationCancel != nil {
		p.translationCancel()
	}
	p.translationMu.Unlock()
	p.translationWG.Wait()
}
