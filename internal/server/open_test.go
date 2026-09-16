package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// Opening a URL in the system browser is a desktop-shell job. A browser
// already has tabs; the web server must not spawn windows on the host.
func TestOpenURLIsDesktopOnly(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPost, "/api/open",
		map[string]any{"url": "https://example.invalid/docs"},
		http.StatusNotImplemented)
	got := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	caps := got["capabilities"].(map[string]any)
	if caps["open_url"] != false {
		t.Fatalf("web mode must not advertise open_url: %v", caps)
	}
}

func TestOpenURLOnDesktop(t *testing.T) {
	h := newHarness(t)

	var opened string
	srv, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   server.ModeDesktop,
		OpenURL: func(url string) error {
			opened = url
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(body any) (*http.Response, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		resp, err := ts.Client().Post(ts.URL+"/api/open",
			"application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, payload
	}

	resp, payload := post(map[string]any{"url": "https://example.invalid/docs"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status %d: %s", resp.StatusCode, payload)
	}
	if opened != "https://example.invalid/docs" {
		t.Fatalf("opened %q", opened)
	}

	for _, raw := range []any{
		map[string]any{"url": "javascript:alert(1)"},
		map[string]any{"url": "file:///etc/passwd"},
		map[string]any{"url": "data:text/html,hi"},
		map[string]any{"url": "ftp://example.invalid/x"},
		map[string]any{"url": "/relative"},
		map[string]any{"url": ""},
	} {
		resp, payload = post(raw)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("accepted %#v: %d %s", raw, resp.StatusCode, payload)
		}
		if opened != "https://example.invalid/docs" {
			t.Fatalf("a rejected URL still launched: %q", opened)
		}
	}

	resp, payload = post("not-an-object")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("accepted a non-object body: %d %s", resp.StatusCode, payload)
	}

	meta := map[string]any{}
	mresp, err := ts.Client().Get(ts.URL + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer mresp.Body.Close()
	raw, _ := io.ReadAll(mresp.Body)
	_ = json.Unmarshal(raw, &meta)
	if meta["capabilities"].(map[string]any)["open_url"] != true {
		t.Fatalf("desktop mode must advertise open_url: %v", meta)
	}
}

func TestOpenURLReportsAFailedLaunch(t *testing.T) {
	h := newHarness(t)
	srv, err := server.New(server.Options{
		Engine:  h.app.Engine,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:    server.ModeDesktop,
		OpenURL: func(string) error { return errors.New("no browser") },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	raw, _ := json.Marshal(map[string]any{"url": "https://example.invalid/docs"})
	resp, err := ts.Client().Post(ts.URL+"/api/open",
		"application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
