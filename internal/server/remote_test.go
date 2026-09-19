package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func TestRemoteStatusDoesNotEchoTheHostToken(t *testing.T) {
	h := newHarness(t)
	if err := h.app.Config.WriteHostToken("never-return-this-token"); err != nil {
		t.Fatal(err)
	}
	got := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "never-return-this-token") {
		t.Fatalf("token leaked: %s", raw)
	}
	if got["has_token"] != true {
		t.Fatalf("%v", got)
	}
	settings := h.do(http.MethodGet, "/api/settings", nil)
	defer settings.Body.Close()
	body, err := io.ReadAll(settings.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "never-return-this-token") {
		t.Fatal("settings echoed the host token")
	}
	h.json(http.MethodPut, "/api/remote/token", map[string]any{"token": ""}, http.StatusOK)
	cleared := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	if cleared["has_token"] != false {
		t.Fatalf("%v", cleared)
	}
}

func TestRemoteOfferWithoutHubIsOffline(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/api/remote/offer", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d", resp.StatusCode)
	}
	got := h.json(http.MethodGet, "/api/remote/bindings", nil, http.StatusOK)
	if _, ok := got["bindings"]; !ok {
		t.Fatalf("%v", got)
	}
	revoke := h.do(http.MethodPost, "/api/remote/bindings/missing/revoke", map[string]any{})
	defer revoke.Body.Close()
	if revoke.StatusCode != http.StatusConflict && revoke.StatusCode != http.StatusOK && revoke.StatusCode < 400 {
		t.Fatalf("revoke %d", revoke.StatusCode)
	}
	h.json(http.MethodPut, "/api/remote/token", map[string]any{"token": "once"}, http.StatusOK)
}

func TestRemoteOfferMintsAScannablePNG(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hubStore)
	if err != nil {
		t.Fatal(err)
	}

	h := newHarness(t)
	h.app.Config.Remote.Enabled = true
	h.app.Config.Remote.HubURL = srv.URL
	if err := h.app.Config.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	h.app.Remote.Reload()
	got := h.json(http.MethodPost, "/api/remote/offer", map[string]any{}, http.StatusOK)
	uri, _ := got["uri"].(string)
	png, _ := got["png"].(string)
	if !strings.HasPrefix(uri, "pairlink:v1:") {
		t.Fatalf("uri %q", uri)
	}
	if !strings.HasPrefix(png, "data:image/png;base64,") {
		t.Fatalf("png %q", png[:min(40, len(png))])
	}
	binds := h.json(http.MethodGet, "/api/remote/bindings", nil, http.StatusOK)
	if _, ok := binds["bindings"]; !ok {
		t.Fatalf("%v", binds)
	}

	req, err := http.NewRequest(http.MethodPut, h.srv.URL+"/api/remote/token", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("junk token body %d", resp.StatusCode)
	}
}
