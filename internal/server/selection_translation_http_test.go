package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestSelectionTranslationReturnsTextWithoutChangingTheConversation(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": map[string]bool{"selection_translation": true}}, http.StatusOK)
	id := h.newThread()
	const source = "Do not restart 2 hosts."
	got := h.json(http.MethodPost, "/api/threads/"+id+"/translation", map[string]string{"text": "  " + source + "  "}, http.StatusOK)
	if got["text"] != source {
		t.Fatalf("translation = %#v", got)
	}
	turns, turnErr := h.app.Store.ListTurns(id)
	events, eventErr := h.app.Store.ListEvents(id, 0, 10)
	followups, followupErr := h.app.Store.ListFollowups(id)
	if turnErr != nil || eventErr != nil || followupErr != nil || len(turns) != 0 || len(events) != 0 || len(followups) != 0 {
		t.Fatalf("translation changed the conversation: turns=%v (%v) events=%v (%v) followups=%v (%v)",
			turns, turnErr, events, eventErr, followups, followupErr)
	}
}

func TestSelectionTranslationRejectsInvalidRequests(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": map[string]bool{"selection_translation": true}}, http.StatusOK)
	path := "/api/threads/" + h.newThread() + "/translation"
	overlong := `{"text":"` + strings.Repeat("a", 8001) + `"}`
	for _, tc := range []struct {
		name   string
		body   string
		origin string
		status int
		code   string
	}{
		{"malformed", "{", "", http.StatusBadRequest, "selection_translation_invalid"},
		{"trailing", `{"text":"Hello"} null`, "", http.StatusBadRequest, "selection_translation_invalid"},
		{"empty", `{"text":"  "}`, "", http.StatusBadRequest, "selection_translation_invalid"},
		{"overlong", overlong, "", http.StatusBadRequest, "selection_translation_too_long"},
		{"oversized", `{"text":"` + strings.Repeat("a", 96<<10) + `"}`, "", http.StatusRequestEntityTooLarge, "selection_translation_too_long"},
		{"foreign origin", `{"text":"Hello"}`, "https://foreign.invalid", http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, bytes.NewBufferString(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			resp, err := h.srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.status, raw)
			}
			if tc.code != "" && !strings.Contains(string(raw), tc.code) {
				t.Fatalf("body %s, want code %s", raw, tc.code)
			}
		})
	}
	h.json(http.MethodPost, "/api/threads/th_missing/translation", map[string]string{"text": "Hello"}, http.StatusNotFound)
}

func TestSelectionTranslationHidesProviderErrors(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": map[string]bool{"selection_translation": true}}, http.StatusOK)
	provider.SetMockFailure(errors.New("private-upstream-detail"))
	t.Cleanup(func() { provider.SetMockFailure(nil) })
	got := h.json(http.MethodPost, "/api/threads/"+h.newThread()+"/translation", map[string]string{"text": "Hello"}, http.StatusBadGateway)
	if got["code"] != "selection_translation_unavailable" || strings.Contains(got["error"].(string), "private-upstream-detail") {
		t.Fatalf("upstream error was not redacted: %+v", got)
	}
}

func TestSelectionTranslationWritesNothingWhenTheCallerAlreadyLeft(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": map[string]bool{"selection_translation": true}}, http.StatusOK)
	id := h.newThread()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+id+"/translation", strings.NewReader(`{"text":"Hello"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.app.Server.Handler().ServeHTTP(rec, req)
	if rec.Body.Len() != 0 {
		t.Fatalf("canceled caller received %d %s", rec.Code, rec.Body.String())
	}
}

func TestSelectionTranslationSwitchRejectsHTTPAndProjectsItsCurrentState(t *testing.T) {
	h := newHarness(t)
	path := "/api/threads/" + h.newThread() + "/translation"
	for _, enabled := range []bool{false, true, false} {
		h.json(http.MethodPut, "/api/settings", map[string]any{"swarm": map[string]bool{"selection_translation": enabled}}, http.StatusOK)
		meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
		if meta["model_policy"].(map[string]any)["selection_translation"] != enabled {
			t.Fatalf("the menu would see stale translation settings: %v", meta["model_policy"])
		}
		if enabled {
			h.json(http.MethodPost, path, map[string]string{"text": "Selected text."}, http.StatusOK)
		} else if got := h.json(http.MethodPost, path, map[string]string{"text": "Selected text."}, http.StatusForbidden); got["code"] != "selection_translation_disabled" {
			t.Fatalf("disabled request: %v", got)
		}
	}
}
