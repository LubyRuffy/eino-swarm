package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A shell that disconnects must disappear from /api/meta. Otherwise the
// engine's idle exit never fires and a closed window looks like it is still
// attached.
func TestPresenceDropsWhenTheShellDisconnects(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodPost, "/api/presence", map[string]any{"surface": "nope"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown surface: %d", res.StatusCode)
	}
	res.Body.Close()

	res = h.do(http.MethodPost, "/api/presence", map[string]any{"surface": "tui", "pid": 7})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reserve: %d", res.StatusCode)
	}
	var reserved struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&reserved); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	body := metaClients(t, h)
	if strings.Contains(body, reserved.ID) {
		t.Fatalf("a reserve that never connected must not count: %s", body)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/presence/"+reserved.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		buf := make([]byte, 16)
		_, _ = resp.Body.Read(buf)
		close(held)
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("presence hold did not start")
	}
	if !strings.Contains(metaClients(t, h), `"surface":"tui"`) {
		t.Fatalf("meta clients = %s", metaClients(t, h))
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(metaClients(t, h), `"clients":[]`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("client still listed after disconnect: %s", metaClients(t, h))
}

func TestPresenceReconnectReplacesThePreviousHold(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodPost, "/api/presence", []byte("{"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body: %d", res.StatusCode)
	}
	res.Body.Close()

	res = h.do(http.MethodPost, "/api/presence", map[string]any{"surface": "desktop"})
	var reserved struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&reserved); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	missing, err := http.Get(h.srv.URL + "/api/presence/pc_missing")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown hold: %d", missing.StatusCode)
	}

	hold := func() *http.Response {
		t.Helper()
		resp, err := http.Get(h.srv.URL + "/api/presence/" + reserved.ID)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	first := hold()
	defer first.Body.Close()
	buf := make([]byte, 8)
	_, _ = first.Body.Read(buf)
	second := hold()
	defer second.Body.Close()
	_, _ = second.Body.Read(buf)
	// The first socket is replaced. Reading it should end rather than stay
	// the counted client.
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, first.Body)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the replaced hold stayed open")
	}
	if !strings.Contains(metaClients(t, h), `"surface":"desktop"`) {
		t.Fatalf("the replacement must still count: %s", metaClients(t, h))
	}
}

func TestPresenceListsShellsBySurface(t *testing.T) {
	h := newHarness(t)
	hold := func(surface string) *http.Response {
		t.Helper()
		res := h.do(http.MethodPost, "/api/presence", map[string]any{"surface": surface})
		var reserved struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(res.Body).Decode(&reserved); err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		resp, err := http.Get(h.srv.URL + "/api/presence/" + reserved.ID)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 8)
		_, _ = resp.Body.Read(buf)
		return resp
	}
	first := hold("tui")
	defer first.Body.Close()
	second := hold("desktop")
	defer second.Body.Close()
	body := metaClients(t, h)
	desk := strings.Index(body, `"surface":"desktop"`)
	term := strings.Index(body, `"surface":"tui"`)
	if desk < 0 || term < 0 || desk > term {
		t.Fatalf("clients are not ordered by surface: %s", body)
	}
}

func metaClients(t *testing.T, h *harness) string {
	t.Helper()
	res := h.do(http.MethodGet, "/api/meta", nil)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body)
}
