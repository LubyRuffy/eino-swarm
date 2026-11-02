package server_test

import (
	"net/http"
	"testing"
)

// The existing panel retains an off project's view when revision > 0. A turn
// may already own Nuwa mode before its first record/spawn; disabling at that
// point must leave the empty collaboration view reachable for its later work.
func TestNuwaDisablingBeforeTheFirstResultRetainsTheProjectView(t *testing.T) {
	h := newHarness(t)
	created := h.newProject(map[string]any{"name": "In-flight collaboration", "nuwa_enabled": true})
	id := created["id"].(string)
	if created["nuwa_revision"] != float64(0) {
		t.Fatalf("creation must keep its initial revision: %v", created)
	}
	updated := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{"nuwa_enabled": false}, http.StatusOK)["project"].(map[string]any)
	if updated["nuwa_enabled"] != false || updated["nuwa_revision"] != float64(1) {
		t.Fatalf("disabling an empty project removed the revision needed to retain its collaboration view: %v", updated)
	}
	snapshot := h.json(http.MethodGet, "/api/projects/"+id+"/nuwa", nil, http.StatusOK)
	if snapshot["revision"] != updated["nuwa_revision"] || len(snapshot["nodes"].([]any)) != 0 || len(snapshot["workers"].([]any)) != 0 {
		t.Fatalf("mode invalidation fabricated work or lost its revision: %v", snapshot)
	}
	ordinary := h.newProject(map[string]any{"name": "Ordinary project"})
	for _, patch := range []map[string]any{{"name": "Renamed ordinary project"}, {"name": "Ordinary edit", "nuwa_enabled": false}} {
		got := h.json(http.MethodPatch, "/api/projects/"+ordinary["id"].(string), patch, http.StatusOK)["project"].(map[string]any)
		if got["nuwa_revision"] != float64(0) {
			t.Fatalf("ordinary edits exposed the collaboration view: %v", got)
		}
	}
}
