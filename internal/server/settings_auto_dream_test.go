package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestSettingsAutoDreamPreservesChoiceWhenLegacyClientSavesMemory(t *testing.T) {
	h := newHarness(t)
	memorySettings := func() map[string]any {
		return h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["memory"].(map[string]any)
	}
	put := func(memory map[string]any) {
		h.json(http.MethodPut, "/api/settings", map[string]any{"memory": memory}, http.StatusOK)
	}
	memory := memorySettings()
	if memory["auto_dream"] != true {
		t.Fatalf("new settings must report the verifier on, got %v", memory["auto_dream"])
	}
	for _, want := range []bool{false, true} {
		memory["auto_dream"] = want
		put(memory)
		if got := memorySettings()["auto_dream"]; got != want {
			t.Fatalf("explicit auto_dream=%t was not applied: %v", want, got)
		}
		delete(memory, "auto_dream")
		// Older clients send the whole Memory section except the new field.
		memory["char_limit"] = memory["char_limit"].(float64) + 1
		put(memory)
		current := memorySettings()
		if current["auto_dream"] != want || current["char_limit"] != memory["char_limit"] {
			t.Fatalf("legacy edit must preserve auto_dream=%t and apply the budget: %v", want, current)
		}
		reloaded, err := config.Load(h.app.Config.DataDir())
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Memory.AutoDream != want {
			t.Fatalf("reloaded auto_dream must be %t", want)
		}
	}
}
