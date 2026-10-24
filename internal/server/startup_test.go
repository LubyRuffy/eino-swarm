package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRecoveryServesHistoryAndPresenceButRejectsMutations(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.app.Engine.BeginRestore()
	h.app.Engine.SetRestoreProgress(2, 5)
	meta := h.json("GET", "/api/meta", nil, 200)
	if meta["startup"] != "restoring" || meta["restore"].(map[string]any)["completed"] != float64(2) {
		t.Fatalf("bad progress: %+v", meta)
	}
	for _, path := range []string{"/api/threads", "/api/threads/" + id, "/api/threads/" + id + "/log", "/api/settings", "/api/engine/restart"} {
		resp := h.do("GET", path, nil)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("read %s: %d", path, resp.StatusCode)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/threads/" + id + "/turns"}, {"POST", "/api/threads/" + id + "/steer"},
		{"GET", "/api/threads/" + id + "/terminal"}, {"GET", "/api/projects/missing/terminal"},
		{"POST", "/api/threads/" + id + "/preempt"}, {"POST", "/api/threads/" + id + "/continue"},
		{"POST", "/api/schedules"}, {"PATCH", "/api/schedules/missing"}, {"DELETE", "/api/schedules/missing"}, {"POST", "/api/schedules/missing/run"},
		{"DELETE", "/api/threads/" + id}, {"PUT", "/api/settings"}, {"POST", "/api/engine/restart"},
	} {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			got := h.json(tc.method, tc.path, map[string]any{}, 409)
			if got["code"] != "restoring" {
				t.Fatalf("bad refusal: %+v", got)
			}
		})
	}
	h.json("POST", "/api/threads/"+id+"/read", nil, 400)
	// Stop stays a real lifecycle request, so an empty thread reports idle,
	// rather than being hidden behind the startup gate.
	got := h.json("POST", "/api/threads/"+id+"/interrupt", nil, http.StatusConflict)
	if got["code"] != "idle" {
		t.Fatalf("Stop gated: %+v", got)
	}
	h.app.Engine.FinishRestore()
	h.json("POST", "/api/threads/"+id+"/turns", map[string]any{"text": "run"}, 202)
}
