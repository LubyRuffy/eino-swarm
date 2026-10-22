package server_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
)

func TestTimeoutPhonePatchAndDesktopPartialSaveDoNotLoseEachOther(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp := h.do(http.MethodPut, "/api/settings", map[string]any{"ui": map[string]any{"locale": "zh"}})
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("desktop=%d", resp.StatusCode)
			}
		}()
		go func() {
			defer wg.Done()
			r := remote.Handle(h.app.Engine, config.RemoteConfig{}, remote.Request{ID: "save", Op: "set_timeouts", TimeoutSettings: json.RawMessage(`{"agent_timeout_seconds":73,"providers":[{"id":"default","first_byte_timeout_seconds":43}]}`)}, "relay", "test")
			if !r.OK {
				t.Error(r.Code)
			}
			_ = h.app.Engine.Providers().List()
			_, _ = h.app.Engine.Providers().Resolve("")
		}()
	}
	wg.Wait()
	s := h.app.Config.Snapshot()
	if s.UI.Locale != "zh" || s.Swarm.AgentTimeoutSeconds != 73 || s.Models.Providers[0].FirstByteTimeoutSeconds != 43 {
		t.Fatal("desktop/phone lost a field")
	}
	// Old desktop clients omit the new field while replacing the provider row.
	h.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{"default": "default", "providers": []map[string]any{{"id": "default", "model": "offline", "timeout_seconds": 99}}}}, 200)
	if p, _ := h.app.Config.Provider("default"); p.FirstByteTimeoutSeconds != 43 {
		t.Fatal("old desktop cleared new first-byte setting")
	}
	loaded, err := config.Load(s.DataDir())
	if err != nil || loaded.Swarm.AgentTimeoutSeconds != 73 || loaded.Models.Providers[0].FirstByteTimeoutSeconds != 43 {
		t.Fatal("save mismatch")
	}
}

// Settings autosave sends only the fields the user edited; timeout fields may
// have changed on the phone while the desktop sheet was already open.
func TestTimeoutDesktopAutosavePatchesKeepLatestPhoneValues(t *testing.T) {
	h := newHarness(t)
	stale := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)
	r := remote.Handle(h.app.Engine, config.RemoteConfig{}, remote.Request{ID: "save", Op: "set_timeouts", TimeoutSettings: json.RawMessage(`{"agent_timeout_seconds":73,"providers":[{"id":"default","timeout_seconds":99,"first_byte_timeout_seconds":43}]}`)}, "relay", "test")
	if !r.OK {
		t.Fatal(r.Code)
	}
	stale["ui"].(map[string]any)["font"] = "serif"
	h.json(http.MethodPut, "/api/settings", map[string]any{"ui": stale["ui"]}, http.StatusOK)
	sw := stale["swarm"].(map[string]any)
	delete(sw, "agent_timeout_seconds")
	sw["max_concurrent"] = 3
	models := stale["models"].(map[string]any)
	row := models["providers"].([]any)[0].(map[string]any)
	delete(row, "timeout_seconds")
	delete(row, "first_byte_timeout_seconds")
	row["label"] = "Renamed"
	// Reorder an existing provider behind a new one; preservation uses identity.
	models["providers"] = []any{map[string]any{"id": "new", "label": "Other", "model": "offline"}, row}
	h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": sw, "models": models}, http.StatusOK)
	s := h.app.Config.Snapshot()
	p, ok := s.Provider("default")
	if !ok || s.UI.Font != "serif" || s.Swarm.AgentTimeoutSeconds != 73 || p.TimeoutSeconds != 99 || p.FirstByteTimeoutSeconds != 43 || p.Label != "Renamed" || s.Swarm.MaxConcurrent != 3 {
		t.Fatalf("omitted shared values were lost: agent=%d provider=%+v font=%q", s.Swarm.AgentTimeoutSeconds, p, s.UI.Font)
	}
	added, ok := s.Provider("new")
	if !ok || added.TimeoutSeconds != int(config.DefaultRequestTimeout.Seconds()) || added.FirstByteTimeoutSeconds != int(config.DefaultFirstByteTimeout.Seconds()) || added.FirstByteTimeout() != config.DefaultFirstByteTimeout {
		t.Fatalf("new provider defaults=%+v", added)
	}
	loaded, err := config.Load(s.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	p, ok = loaded.Provider("default")
	if !ok || loaded.Swarm.AgentTimeoutSeconds != 73 || p.TimeoutSeconds != 99 || p.FirstByteTimeoutSeconds != 43 {
		t.Fatal("persisted timeout values differ")
	}
}

// Explicit values in the public full PUT still replace the saved values.
func TestTimeoutDesktopExplicitValuesRetainReplacementSemantics(t *testing.T) {
	h := newHarness(t)
	stale := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)
	for _, v := range []int{17, 0} {
		sw := stale["swarm"].(map[string]any)
		sw["agent_timeout_seconds"] = v
		row := stale["models"].(map[string]any)["providers"].([]any)[0].(map[string]any)
		row["timeout_seconds"] = v
		row["first_byte_timeout_seconds"] = v
		h.json(http.MethodPut, "/api/settings", stale, http.StatusOK)
		s := h.app.Config.Snapshot()
		p := s.Models.Providers[0]
		if v > 0 {
			if s.Swarm.AgentTimeoutSeconds != v || p.TimeoutSeconds != v || p.FirstByteTimeoutSeconds != v {
				t.Fatal("explicit timeouts did not replace")
			}
		} else if s.Swarm.AgentTimeoutSeconds != config.DefaultAgentTimeoutSeconds || p.TimeoutSeconds != int(config.DefaultRequestTimeout.Seconds()) || p.FirstByteTimeoutSeconds != int(config.DefaultFirstByteTimeout.Seconds()) {
			t.Fatal("explicit zero no longer normalizes as before")
		}
	}
}
