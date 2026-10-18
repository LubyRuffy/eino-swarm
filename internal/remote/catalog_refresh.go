package remote

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

func handleCatalogRefresh(ctx context.Context, eng *engine.Engine, req Request, path, sessionID string) Response {
	models, err := discoverModelViews(ctx, eng)
	if err != nil {
		// Transport errors can contain the PC's endpoint and query credentials.
		return fail(req.ID, path, sessionID, "refresh_failed", "could not refresh model list")
	}
	resp := okBase(req.ID, path, sessionID)
	resp.Models = models
	resp.ReasoningLevels = config.ReasoningEfforts()
	// This response contains only strings, booleans and their slices.
	raw, _ := json.Marshal(resp)
	if len(raw) > maxCatalogFrame {
		return fail(req.ID, path, sessionID, "too_large", "model list does not fit one frame")
	}
	return resp
}

func discoverModelViews(ctx context.Context, eng *engine.Engine) ([]ModelView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if eng == nil || eng.Providers() == nil {
		return nil, errors.New("remote: model catalog unavailable")
	}
	if eng.Providers().IsMock() {
		return modelViews(eng), nil
	}
	var providers []config.Provider
	for _, p := range eng.Config().Models.Providers {
		if p.Listed() && p.EndpointReady() {
			providers = append(providers, p)
		}
	}
	results := make([][]ModelView, len(providers))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cat, err := eng.Providers().Discover(ctx, p)
			if err != nil {
				cancel()
				return
			}
			for _, name := range cat.Names {
				results[i] = append(results[i], ModelView{
					ProviderID: p.ID, ProviderLabel: p.GroupName(), Model: name, Default: name == p.Model,
				})
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var models []ModelView
	for _, r := range results {
		models = append(models, r...)
	}
	return models, nil
}
