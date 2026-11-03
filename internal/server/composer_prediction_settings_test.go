package server_test

import (
	"net/http"
	"testing"
)

func TestOmittedComposerPredictionSaveKeepsTheStoredChoice(t *testing.T) {
	h := newHarness(t)
	if !h.app.Config.Swarm.ComposerPredictions {
		t.Fatal("next-step suggestions default off")
	}
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"max_concurrent": 4},
	}, http.StatusOK)
	if !h.app.Config.Swarm.ComposerPredictions || h.app.Config.Swarm.MaxConcurrent != 4 {
		t.Fatalf("an older settings save changed suggestions: %+v", h.app.Config.Swarm)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"composer_predictions": false},
	}, http.StatusOK)
	if h.app.Config.Swarm.ComposerPredictions {
		t.Fatal("explicit off did not stick")
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"max_concurrent": 5},
	}, http.StatusOK)
	if h.app.Config.Swarm.ComposerPredictions || h.app.Config.Swarm.MaxConcurrent != 5 {
		t.Fatalf("omitting the switch turned suggestions back on: %+v", h.app.Config.Swarm)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"composer_predictions": true},
	}, http.StatusOK)
	if !h.app.Config.Swarm.ComposerPredictions {
		t.Fatal("explicit on did not stick")
	}
}
