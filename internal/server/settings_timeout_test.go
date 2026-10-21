package server_test

import (
	"net/http"
	"testing"
)

func TestProviderFirstByteTimeoutRoundTripsAndOlderClientsKeepIt(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name  string
		value any
		want  float64
	}{
		{"configured", 120, 120},
		{"older client", nil, 120},
		{"zero resets default", 0, 30},
		{"negative resets default", -3, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := map[string]any{"id": "default", "model": "test", "timeout_seconds": 10}
			if tc.value != nil {
				p["first_byte_timeout_seconds"] = tc.value
			}
			h.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{
				"default": "default", "providers": []map[string]any{p},
			}}, http.StatusOK)
			got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)
			row := got["settings"].(map[string]any)["models"].(map[string]any)["providers"].([]any)[0].(map[string]any)
			if row["first_byte_timeout_seconds"] != tc.want || row["timeout_seconds"] != float64(10) {
				t.Fatalf("initial and idle waits must round-trip independently: %+v", row)
			}
			stored, _ := h.app.Config.Provider("default")
			if stored.FirstByteTimeoutSeconds != int(tc.want) {
				t.Fatalf("stored=%+v", stored)
			}
		})
	}
}
