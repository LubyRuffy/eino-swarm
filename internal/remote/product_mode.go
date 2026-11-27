package remote

import (
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// This is the same install-wide debug switch as desktop Settings. The paired
// phone requests a source, never supplies a credential or unlocks a preset.
func handleProductMode(e *engine.Engine, req Request, path, sessionID string) Response {
	if req.ProductMode != config.ProductModeLocal && req.ProductMode != config.ProductModeFOFA {
		return fail(req.ID, path, sessionID, "bad_request", "product_mode must be local or fofa")
	}
	mode := config.ProductConfig{Mode: req.ProductMode}
	err := e.UpdateConfigForMode(&mode, func(next *config.Config) error {
		next.Product = mode
		return nil
	})
	if errors.Is(err, engine.ErrBackupBusy) || errors.Is(err, engine.ErrRestoring) {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err != nil {
		// Config persistence errors can contain private filesystem paths.
		return fail(req.ID, path, sessionID, "save_failed", "could not save PC execution mode")
	}
	resp := okBase(req.ID, path, sessionID)
	resp.ProductMode = mode.Resolved()
	policy := e.ModelPolicyView()
	resp.ModelPolicy = &policy
	return resp
}
