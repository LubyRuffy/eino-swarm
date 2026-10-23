package server_test

import (
	"net/http"
	"testing"
)

func TestMemoryPanelsUseTheConfiguredStaleThreshold(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Memory.SkillStaleDays = 17
	p := h.newProject(map[string]any{"name": "memory threshold"})
	for _, path := range []string{"/api/projects/" + p["id"].(string) + "/memory", "/api/library"} {
		got := h.json(http.MethodGet, path, nil, http.StatusOK)["memory"].(map[string]any)
		if got["skill_stale_days"] != float64(17) {
			t.Errorf("%s threshold=%v", path, got["skill_stale_days"])
		}
	}
}
