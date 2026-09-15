package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type harness struct {
	t   *testing.T
	app *app.App
	srv *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	a, err := app.New(app.Options{
		DataDir:  t.TempDir(),
		Mock:     true,
		Mode:     server.ModeWeb,
		Version:  "test",
		NoAssets: true,
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	a.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(a.Server.Handler())
	t.Cleanup(func() {
		ts.Close()
		a.Engine.Shutdown()
		_ = a.Store.Close()
	})
	return &harness{t: t, app: a, srv: ts}
}

func (h *harness) do(method, path string, body any) *http.Response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (h *harness) json(method, path string, body any, wantStatus int) map[string]any {
	h.t.Helper()
	resp := h.do(method, path, body)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatalf("%s %s: response is not JSON: %s", method, path, raw)
	}
	return out
}

func (h *harness) newThread() string {
	h.t.Helper()
	got := h.json(http.MethodPost, "/api/threads", map[string]any{}, http.StatusCreated)
	return got["thread"].(map[string]any)["id"].(string)
}

func (h *harness) waitTurnDone(threadID string) store.Turn {
	h.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		turns, err := h.app.Store.ListTurns(threadID)
		if err != nil {
			h.t.Fatal(err)
		}
		if len(turns) > 0 {
			last := turns[len(turns)-1]
			if last.Status != store.TurnRunning {
				return last
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("no turn finished for %s", threadID)
	return store.Turn{}
}

// ---------- meta / settings ----------

func TestMetaTellsTheUIWhatItCanDo(t *testing.T) {
	h := newHarness(t)
	got := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)

	if got["version"] != "test" || got["mode"] != server.ModeWeb {
		t.Fatalf("meta=%v", got)
	}
	if got["mock"] != true {
		t.Fatal("a mock build must say so, or a demo answer looks real")
	}
	// the scripted provider needs no setup, so the UI must not show a setup banner
	if got["configured"] != true {
		t.Fatal("a mock build is always configured")
	}
	caps, ok := got["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities: %v", got)
	}
	// in a browser there is no file manager to open, so the UI hides the button
	if caps["reveal"] != false {
		t.Fatalf("web mode must not advertise reveal: %v", caps)
	}
	if got["data_dir"] == "" {
		t.Fatal("meta should report the data directory for troubleshooting")
	}
}

// The API key must never come back out of the settings endpoint; the dialog
// only needs to know whether one is stored.
func TestSettingsNeverReturnsTheAPIKey(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Main", "base_url": "http://endpoint.invalid/v1",
				"model": "m", "api_key": "super-secret", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)

	resp := h.do(http.MethodGet, "/api/settings", nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("the api key leaked through the settings endpoint: %s", raw)
	}
	var out struct {
		Settings struct {
			Models struct {
				Providers []struct {
					ID        string `json:"id"`
					HasAPIKey bool   `json:"has_api_key"`
					Ready     bool   `json:"ready"`
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	p := out.Settings.Models.Providers[0]
	if !p.HasAPIKey || !p.Ready {
		t.Fatalf("the dialog cannot tell a key is stored: %+v", p)
	}

	// and the stored key survives a save that does not mention it
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Renamed", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)
	prov, _ := h.app.Config.Provider("default")
	if prov.APIKey != "super-secret" {
		t.Fatalf("saving other fields wiped the api key: %q", prov.APIKey)
	}
	if prov.Model != "m2" {
		t.Fatalf("the model was not updated: %q", prov.Model)
	}

	// an explicit empty string clears it
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "api_key": "",
			}},
		},
	}, http.StatusOK)
	prov, _ = h.app.Config.Provider("default")
	if prov.APIKey != "" {
		t.Fatalf("an explicit empty key did not clear it: %q", prov.APIKey)
	}
}

func TestSettingsPersistAndValidate(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"max_concurrent": 3, "agent_timeout_seconds": 42,
			"max_turns": 9, "manager_max_iterations": 11},
		"tools": map[string]any{"disabled": []string{"exec"}, "web_search_max_results": 5},
		"log":   map[string]any{"level": "debug"},
	}, http.StatusOK)

	if h.app.Config.Swarm.MaxConcurrent != 3 || h.app.Config.Swarm.AgentTimeoutSeconds != 42 {
		t.Fatalf("swarm settings not applied: %+v", h.app.Config.Swarm)
	}
	if !h.app.Config.Tools.IsDisabled("exec") {
		t.Fatal("tool toggle not applied")
	}
	// and they survive a restart, because they were written to the file
	raw, err := os.ReadFile(h.app.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "exec") {
		t.Fatalf("settings were not persisted to disk:\n%s", raw)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{"default": "x", "providers": []map[string]any{}},
	}, http.StatusBadRequest)
	resp := h.do(http.MethodPut, "/api/settings", "not an object")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage body accepted: %d", resp.StatusCode)
	}
}

func TestModelsAndToolsEndpoints(t *testing.T) {
	h := newHarness(t)
	models := h.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	if len(models["models"].([]any)) == 0 {
		t.Fatal("no models listed")
	}
	if models["default"] == "" {
		t.Fatal("no default model reported")
	}

	tools := h.json(http.MethodGet, "/api/tools", nil, http.StatusOK)
	catalog := tools["catalog"].([]any)
	enabled := tools["enabled"].([]any)
	if len(catalog) < 10 {
		t.Fatalf("the tool catalog looks truncated: %d", len(catalog))
	}
	if len(enabled) == 0 || len(enabled) > len(catalog) {
		t.Fatalf("enabled=%d catalog=%d", len(enabled), len(catalog))
	}
	first := catalog[0].(map[string]any)
	for _, field := range []string{"name", "title", "summary", "group"} {
		if first[field] == nil || first[field] == "" {
			t.Fatalf("catalog entries must be renderable: %v", first)
		}
	}
}

// ---------- conversations ----------

func TestThreadLifecycle(t *testing.T) {
	h := newHarness(t)

	created := h.json(http.MethodPost, "/api/threads",
		map[string]any{"title": "Named"}, http.StatusCreated)
	id := created["thread"].(map[string]any)["id"].(string)
	if created["thread"].(map[string]any)["title"] != "Named" {
		t.Fatalf("title not kept: %v", created)
	}

	list := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)
	if len(list["threads"].([]any)) != 1 {
		t.Fatalf("threads=%v", list)
	}

	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	if got["status"].(map[string]any)["running"] != false {
		t.Fatalf("a fresh conversation is not running: %v", got)
	}

	renamed := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"title": "Renamed"}, http.StatusOK)
	if renamed["thread"].(map[string]any)["title"] != "Renamed" {
		t.Fatalf("rename failed: %v", renamed)
	}
	h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{"title": "  "}, http.StatusBadRequest)
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"provider_id": "nope"}, http.StatusBadRequest)

	// archiving takes it out of the default list but keeps it reachable
	h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{"archived": true}, http.StatusOK)
	list = h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)
	if len(list["threads"].([]any)) != 0 {
		t.Fatalf("archived conversation still listed: %v", list)
	}
	list = h.json(http.MethodGet, "/api/threads?archived=1", nil, http.StatusOK)
	if len(list["threads"].([]any)) != 1 {
		t.Fatalf("archived conversation not reachable: %v", list)
	}

	resp := h.do(http.MethodDelete, "/api/threads/"+id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", resp.StatusCode)
	}
	h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusNotFound)
	h.json(http.MethodDelete, "/api/threads/"+id, nil, http.StatusNotFound)
}

func TestUnknownThreadIs404Everywhere(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/threads/nope", nil},
		{http.MethodPatch, "/api/threads/nope", map[string]any{"title": "x"}},
		{http.MethodDelete, "/api/threads/nope", nil},
		{http.MethodPost, "/api/threads/nope/turns", map[string]any{"text": "hi"}},
		{http.MethodPost, "/api/threads/nope/steer", map[string]any{"text": "hi"}},
		{http.MethodPost, "/api/threads/nope/interrupt", nil},
		{http.MethodGet, "/api/threads/nope/files", nil},
		{http.MethodGet, "/api/threads/nope/turns", nil},
		{http.MethodGet, "/api/threads/nope/events", nil},
		{http.MethodGet, "/api/threads/nope/download/a.txt", nil},
	} {
		h.json(tc.method, tc.path, tc.body, http.StatusNotFound)
	}
}

// A turn is accepted and runs in the background: the request must not be held
// open for the length of the answer.
func TestStartTurnIsAcceptedNotAwaited(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	start := time.Now()
	got := h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "look into this"}, http.StatusAccepted)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the request waited for the answer (%v)", time.Since(start))
	}
	turn := got["turn"].(map[string]any)
	if turn["id"] == "" || turn["status"] != store.TurnRunning {
		t.Fatalf("turn=%v", turn)
	}

	// a second turn while one is running is a conflict the UI can react to
	conflict := h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "again"}, http.StatusConflict)
	if conflict["code"] != "busy" {
		t.Fatalf("the UI cannot tell this apart from a server error: %v", conflict)
	}

	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "  "}, http.StatusBadRequest)
}

// Pressing Enter is one gesture: it steers a running turn and starts a new one
// otherwise, without the browser having to guess which.
func TestSteerFallsBackToStartingATurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	first := h.json(http.MethodPost, "/api/threads/"+id+"/steer",
		map[string]any{"text": "the opening request"}, http.StatusAccepted)
	if first["steered"] != false || first["turn"] == nil {
		t.Fatalf("an idle conversation should have started a turn: %v", first)
	}

	var steered bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(http.MethodPost, "/api/threads/"+id+"/steer",
			map[string]any{"text": "narrow it down"})
		var body map[string]any
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = json.Unmarshal(raw, &body)
		if resp.StatusCode == http.StatusAccepted && body["steered"] == true {
			steered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !steered {
		t.Fatal("could not steer a running turn through the API")
	}
	h.waitTurnDone(id)

	h.json(http.MethodPost, "/api/threads/"+id+"/steer",
		map[string]any{"text": ""}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusConflict)
}

// ---------- event stream ----------

// The stream has to replay history and then go live from one subscription, or
// everything that happens between the two is lost.
func TestEventStreamReplaysThenGoesLive(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "produce a result"}, http.StatusAccepted)

	kinds, lastSeq := readStream(t, h, id, 0, 45*time.Second)
	for _, want := range []string{
		"user_message", "reasoning_delta", "delta", "spawned",
		"tool_call", "tool_result", "finished", "agent_message", "ready", "done",
	} {
		if kinds[want] == 0 {
			t.Fatalf("the stream never carried %q; got %v", want, kinds)
		}
	}
	if lastSeq == 0 {
		t.Fatal("no event carried a sequence number to resume from")
	}

	// reconnecting after the turn replays the persisted timeline, with no
	// deltas in it, and reports ready
	replayKinds, replaySeq := readStream(t, h, id, 0, 10*time.Second)
	if replayKinds["delta"] != 0 || replayKinds["reasoning_delta"] != 0 {
		t.Fatalf("a replay should not contain streamed deltas: %v", replayKinds)
	}
	if replayKinds["reasoning"] == 0 || replayKinds["agent_message"] == 0 {
		t.Fatalf("a replay lost the complete text: %v", replayKinds)
	}
	if replaySeq != lastSeq {
		t.Fatalf("replay ended at seq %d, live ended at %d", replaySeq, lastSeq)
	}

	// and resuming from the end returns nothing but the ready marker
	tail, _ := readStream(t, h, id, lastSeq, 10*time.Second)
	for kind, n := range tail {
		if kind != "ready" && n > 0 {
			t.Fatalf("resuming from the end replayed %q again: %v", kind, tail)
		}
	}
}

// An EventSource reconnects on its own using Last-Event-ID; the server has to
// honour it or every reconnect duplicates the whole conversation.
func TestEventStreamHonoursLastEventID(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "produce a result"}, http.StatusAccepted)
	_, lastSeq := readStream(t, h, id, 0, 45*time.Second)

	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/api/threads/"+id+"/events", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprint(lastSeq))
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type=%q", ct)
	}
	kinds := scanStream(t, resp.Body, 5*time.Second)
	for kind, n := range kinds {
		if kind != "ready" && n > 0 {
			t.Fatalf("Last-Event-ID was ignored; got %q again: %v", kind, kinds)
		}
	}
}

// readStream opens the event stream, reads until the turn's terminal event or
// the ready marker on an idle conversation, and returns the event counts and
// the highest sequence number.
func readStream(t *testing.T, h *harness, threadID string, since int64, timeout time.Duration) (map[string]int, int64) {
	t.Helper()
	url := fmt.Sprintf("%s/api/threads/%s/events?since=%d", h.srv.URL, threadID, since)
	resp, err := h.srv.Client().Get(url)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}

	kinds := map[string]int{}
	var highest int64
	deadline := time.Now().Add(timeout)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var kind string
	sawReady := false
	idle := h.app.Engine.Status(threadID).Running == false
	for scanner.Scan() {
		if time.Now().After(deadline) {
			t.Fatalf("stream did not reach a terminal event within %v: %v", timeout, kinds)
		}
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			var seq int64
			fmt.Sscanf(line, "id: %d", &seq)
			if seq > highest {
				highest = seq
			}
		case strings.HasPrefix(line, "event: "):
			kind = strings.TrimPrefix(line, "event: ")
			kinds[kind]++
			if kind == "ready" {
				sawReady = true
			}
		case line == "":
			if kind == "done" || kind == "error" {
				return kinds, highest
			}
			// nothing is running, so the replay plus ready is the whole stream
			if sawReady && idle {
				return kinds, highest
			}
		}
	}
	return kinds, highest
}

func scanStream(t *testing.T, body io.Reader, window time.Duration) map[string]int {
	t.Helper()
	var mu sync.Mutex
	kinds := map[string]int{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				mu.Lock()
				kinds[strings.TrimPrefix(line, "event: ")]++
				mu.Unlock()
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(window):
	}
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]int, len(kinds))
	for k, v := range kinds {
		out[k] = v
	}
	return out
}

// ---------- trace ----------

// One id, one request, the whole turn: this is the troubleshooting path.
func TestTraceReconstructsATurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "do the work"}, http.StatusAccepted)
	turn := h.waitTurnDone(id)

	got := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	if got["turn"].(map[string]any)["id"] != turn.ID {
		t.Fatalf("trace=%v", got)
	}
	events := got["events"].([]any)
	calls := got["llm_calls"].([]any)
	if len(events) == 0 {
		t.Fatal("a trace with no events cannot explain anything")
	}
	if len(calls) < 3 {
		t.Fatalf("the trace should show every model call, got %d", len(calls))
	}
	first := calls[0].(map[string]any)
	for _, field := range []string{"agent_id", "model", "input_msgs", "duration_ms"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("model call records are missing %q: %v", field, first)
		}
	}
	h.json(http.MethodGet, "/api/trace/nope", nil, http.StatusNotFound)

	turns := h.json(http.MethodGet, "/api/threads/"+id+"/turns", nil, http.StatusOK)
	if len(turns["turns"].([]any)) != 1 {
		t.Fatalf("turns=%v", turns)
	}
}

// ---------- assets ----------

func TestAssetsServeSPAWithFallback(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<html>app shell</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "main.js"),
		[]byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := app.New(app.Options{
		DataDir: t.TempDir(), Mock: true, Mode: server.ModeWeb,
		Assets: os.DirFS(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { a.Engine.Shutdown(); _ = a.Store.Close() }()
	ts := httptest.NewServer(a.Server.Handler())
	defer ts.Close()

	for _, tc := range []struct {
		path, wantBody, wantCache string
	}{
		{"/", "app shell", "no-store"},
		{"/assets/main.js", "console.log(1)", "public, max-age=31536000, immutable"},
		// a deep link and a reload must land on the app, not a 404
		{"/threads/th_abc", "app shell", "no-store"},
	} {
		resp, err := ts.Client().Get(ts.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), tc.wantBody) {
			t.Fatalf("GET %s: %d %q", tc.path, resp.StatusCode, body)
		}
		if got := resp.Header.Get("Cache-Control"); got != tc.wantCache {
			t.Fatalf("GET %s cache-control=%q want %q", tc.path, got, tc.wantCache)
		}
	}

	// an unknown API path is an error, never the app shell: a fetch that gets
	// HTML back fails in a way that is very hard to debug
	resp, err := ts.Client().Get(ts.URL + "/api/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("unknown api path: %d %q", resp.StatusCode, body)
	}
}

// ---------- app wiring ----------

func TestAppListenAndShutdown(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := app.New(app.Options{
		DataDir: t.TempDir(), Addr: "127.0.0.1:0", Mock: true,
		Mode: server.ModeDesktop, NoAssets: true,
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	if a.URL() != "" {
		t.Fatal("URL should be empty before listening")
	}
	url, err := a.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") || strings.HasSuffix(url, ":0") {
		t.Fatalf("a random port must resolve to a real one, got %q", url)
	}
	go func() { _ = a.Serve() }()

	resp, err := http.Get(url + "/api/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var meta map[string]any
	_ = json.Unmarshal(raw, &meta)
	if meta["mode"] != server.ModeDesktop {
		t.Fatalf("meta=%v", meta)
	}
	// the desktop shell can open a file manager, so it advertises it
	if meta["capabilities"].(map[string]any)["reveal"] != true {
		t.Fatalf("desktop mode should advertise reveal: %v", meta)
	}

	a.Shutdown(newShortContext())
	if _, err := http.Get(url + "/api/meta"); err == nil {
		t.Fatal("the server is still serving after shutdown")
	}
}

func newShortContext() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = cancel
	return ctx
}

func TestServerRequiresAnEngine(t *testing.T) {
	if _, err := server.New(server.Options{}); err == nil {
		t.Fatal("want an error with no engine")
	}
}

// A conversation that ran before a restart must come back, with its turn no
// longer marked running.
func TestRestartRecoversConversations(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()

	first, err := app.New(app.Options{DataDir: dir, Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := first.Engine.CreateThread("Survivor", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Engine.StartTurn(th.ID, "start something long"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	// simulate a kill: close the database without letting the engine finish
	_ = first.Store.Close()

	second, err := app.New(app.Options{DataDir: dir, Mock: true, NoAssets: true})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { second.Engine.Shutdown(); _ = second.Store.Close() }()

	got, err := second.Store.GetThread(th.ID)
	if err != nil {
		t.Fatalf("the conversation did not survive the restart: %v", err)
	}
	if got.Title != "Survivor" {
		t.Fatalf("title=%q", got.Title)
	}
	turns, err := second.Store.ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range turns {
		if turn.Status == store.TurnRunning {
			t.Fatalf("a turn is still marked running after a restart: %+v", turn)
		}
	}
	if second.Engine.Status(th.ID).Running {
		t.Fatal("the restarted engine thinks the conversation is working")
	}
}

func TestNotifyKindsAreStableAcrossTheWire(t *testing.T) {
	// The front end matches on these strings, so a rename in the library must
	// not silently change the protocol.
	for _, want := range []string{
		"agent_message", "spawned", "finished", "tool_call", "tool_result",
		"turn", "delta", "reasoning_delta", "done", "error",
	} {
		if _, ok := swarm.ParseNotifyKind(want); !ok {
			t.Fatalf("the event kind %q the UI relies on no longer exists", want)
		}
	}
}
