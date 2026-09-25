package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUntaggedBuildDoesNotOfferAnUpdate(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodGet, "/api/update", nil)
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"error"`) {
		t.Fatalf("status %d body %s", res.StatusCode, raw)
	}
	res = h.do(http.MethodPost, "/api/update", map[string]any{"version": "1.2.4"})
	defer res.Body.Close()
	raw, _ = io.ReadAll(res.Body)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("install %d %s", res.StatusCode, raw)
	}
}
