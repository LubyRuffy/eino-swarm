package server_test

import (
	"net/http"
	"testing"
)

// A project instruction is sufficient to describe a new domain. Enabling its
// collaboration runtime must not require a role template or rewrite that text.
func TestNuwaProjectModeSurvivesCreateEditAndReload(t *testing.T) {
	h := newHarness(t)
	instruction := "Use the supplied evidence to decide the work and verify the result."
	p := h.newProject(map[string]any{
		"name": "Independent work", "system_prompt": instruction, "nuwa_enabled": true,
	})
	id := p["id"].(string)
	if p["nuwa_enabled"] != true {
		t.Fatalf("project creation lost the selected collaboration mode: %v", p)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		var body map[string]any
		if method == http.MethodPatch {
			body = map[string]any{"name": "Renamed"}
		}
		got := h.json(method, "/api/projects/"+id, body, http.StatusOK)["project"].(map[string]any)
		if got["nuwa_enabled"] != true || got["system_prompt"] != instruction {
			t.Fatalf("%s changed mode or project instruction: %v", method, got)
		}
	}
	got := h.json(http.MethodPatch, "/api/projects/"+id,
		map[string]any{"nuwa_enabled": false}, http.StatusOK)["project"].(map[string]any)
	if got["nuwa_enabled"] != false || got["system_prompt"] != instruction {
		t.Fatalf("explicitly disabling collaboration did not preserve its instruction: %v", got)
	}
	ordinary := h.newProject(map[string]any{"name": "Existing behavior"})
	if ordinary["nuwa_enabled"] != false {
		t.Fatalf("ordinary projects must retain their default mode: %v", ordinary)
	}
}
