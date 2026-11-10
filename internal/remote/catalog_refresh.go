package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func handleCatalogRefresh(ctx context.Context, eng *engine.Engine, req Request, path, sessionID string) Response {
	models, err := discoverModelViews(ctx, eng)
	if err != nil {
		// Transport errors can contain the PC's endpoint and query credentials.
		return fail(req.ID, path, sessionID, "refresh_failed", "could not refresh model list")
	}
	resp := okBase(req.ID, path, sessionID)
	resp.ComposerPredictions = new(bool)
	*resp.ComposerPredictions = eng.Config().SwarmSnapshot().ComposerPredictions
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
	for _, p := range eng.Config().ModelsSnapshot().Providers {
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
					ProviderID: p.ID, ProviderLabel: p.GroupName(), Model: name,
					Default: name == strings.TrimSpace(p.Model), DefaultFollowsSetting: name == strings.TrimSpace(p.Model) && strings.TrimSpace(p.Model) != "",
					Ready: true,
				})
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The account's models are already the server's current answer, refreshed
	// on its own schedule, and they always lead the list.
	models := accountModelViews(eng)
	for _, r := range results {
		models = append(models, r...)
	}
	return models, nil
}

func accountModelViews(eng *engine.Engine) []ModelView {
	var out []ModelView
	for _, info := range eng.Providers().List() {
		if info.Source != provider.SourceAccount || strings.TrimSpace(info.Model) == "" {
			continue
		}
		if !info.Ready && !(info.Default && info.DefaultFollowsSetting) {
			continue
		}
		out = append(out, ModelView{
			ProviderID: info.ProviderID, ProviderLabel: info.ProviderLabel, Model: info.Model,
			Default: info.Default, DefaultFollowsSetting: info.DefaultFollowsSetting, Ready: info.Ready,
		})
	}
	return out
}
