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
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
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
		if a.Search != nil {
			a.Search.Stop()
		}
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
		got := h.json(http.MethodGet, "/api/threads/"+threadID, nil, http.StatusOK)
		status, _ := got["status"].(map[string]any)
		if status["awaiting_answer"] == true {
			resp := h.do(http.MethodPost, "/api/threads/"+threadID+"/answers",
				map[string]any{"text": "the existing approach"})
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusConflict {
				h.t.Fatalf("answers: status %d", resp.StatusCode)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("no turn finished for %s", threadID)
	return store.Turn{}
}

// meta / settings tests live in settings_http_test.go

// ---------- conversations ----------

func TestThreadLifecycle(t *testing.T) {
	h := newHarness(t)

	created := h.json(http.MethodPost, "/api/threads",
		map[string]any{"title": "Named"}, http.StatusCreated)
	id := created["thread"].(map[string]any)["id"].(string)
	if created["thread"].(map[string]any)["title"] != "Named" {
		t.Fatalf("title not kept: %v", created)
	}
	if created["thread"].(map[string]any)["title_auto"] != false {
		t.Fatal("an explicit title must not stay machine-owned")
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

	// the thinking level round-trips; an unknown one is rejected, the same way
	// an unknown provider is, so a typo never runs at the wrong level
	leveled := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"reasoning_effort": "high"}, http.StatusOK)
	if leveled["thread"].(map[string]any)["reasoning_effort"] != "high" {
		t.Fatalf("thinking level not kept: %v", leveled)
	}
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"reasoning_effort": "bogus"}, http.StatusBadRequest)
	// a blank level is allowed: it clears back to the model's own default
	cleared := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"reasoning_effort": ""}, http.StatusOK)
	if cleared["thread"].(map[string]any)["reasoning_effort"] != "" {
		t.Fatalf("a blank level must clear to the default: %v", cleared)
	}

	picked := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"model": "other"}, http.StatusOK)
	if picked["thread"].(map[string]any)["model"] != "other" {
		t.Fatalf("conversation model not kept: %v", picked)
	}

	pinned := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"pinned": true}, http.StatusOK)
	body := pinned["thread"].(map[string]any)
	if body["pinned"] != true {
		t.Fatalf("pin did not stick: %v", pinned)
	}
	if body["pinned_at"] == nil || body["pinned_at"] == "" {
		t.Fatalf("a pin must carry a time so the sidebar can order it: %v", pinned)
	}
	listed := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)
	row := listed["threads"].([]any)[0].(map[string]any)
	if row["pinned"] != true {
		t.Fatalf("the list must show a pin without another round trip: %v", listed)
	}
	unpinned := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"pinned": false}, http.StatusOK)
	clearedPin := unpinned["thread"].(map[string]any)
	if clearedPin["pinned"] != false {
		t.Fatalf("unpin did not stick: %v", unpinned)
	}
	if _, ok := clearedPin["pinned_at"]; ok && clearedPin["pinned_at"] != nil && clearedPin["pinned_at"] != "" {
		t.Fatalf("unpin must drop the time: %v", unpinned)
	}

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
		{http.MethodPost, "/api/threads/nope/preempt", nil},
		{http.MethodDelete, "/api/threads/nope/steers/1", nil},
		{http.MethodGet, "/api/threads/nope/followups", nil},
		{http.MethodPost, "/api/threads/nope/followups", map[string]any{"text": "hi"}},
		{http.MethodDelete, "/api/threads/nope/followups/fu_x", nil},
		{http.MethodPatch, "/api/threads/nope/followups/fu_x", map[string]any{"text": "hi"}},
		{http.MethodPost, "/api/threads/nope/followups/fu_x/steer", nil},
		{http.MethodPost, "/api/threads/nope/interrupt", nil},
		{http.MethodPost, "/api/threads/nope/continue", map[string]any{"continue": true}},
		{http.MethodPost, "/api/threads/nope/answers", map[string]any{"text": "x"}},
		{http.MethodPost, "/api/threads/nope/plan/implement", nil},
		{http.MethodPost, "/api/threads/nope/compact", nil},
		{http.MethodGet, "/api/threads/nope/files", nil},
		{http.MethodGet, "/api/threads/nope/turns", nil},
		{http.MethodGet, "/api/threads/nope/events", nil},
		{http.MethodGet, "/api/threads/nope/log", nil},
		{http.MethodGet, "/api/threads/nope/download/a.txt", nil},
		{http.MethodGet, "/api/threads/nope/input-images/img_ab", nil},
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

func TestContinueTurnEndpoint(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/continue",
		map[string]any{"continue": true}, http.StatusConflict)
	h.json(http.MethodPost, "/api/threads/"+id+"/continue", map[string]any{}, http.StatusBadRequest)

	h.app.Config.Swarm.ManagerMaxIterations = 1
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "look into this"}, http.StatusAccepted)

	deadline := time.Now().Add(15 * time.Second)
	var waiting bool
	for time.Now().Before(deadline) {
		got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
		status, _ := got["status"].(map[string]any)
		if status["awaiting_continue"] == true {
			waiting = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("the turn never paused at the tool-round cap")
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/continue",
		map[string]any{"continue": false}, http.StatusAccepted)
	turn := h.waitTurnDone(id)
	if turn.Status != store.TurnCancelled {
		t.Fatalf("status=%s err=%s", turn.Status, turn.Error)
	}
	if strings.Contains(turn.Error, "NodeRunError") {
		t.Fatalf("the graph dump leaked through the API: %s", turn.Error)
	}
}

// ---------- event stream ----------

// The stream has to replay history and then go live from one subscription, or
// everything that happens between the two is lost.
func TestEventStreamReplaysThenGoesLive(t *testing.T) {
	h := newHarness(t)
	// The namer records after `done`. This test is about the turn's own
	// timeline; a title event landing between the live close and the
	// resume would look like a duplicate.
	h.app.Config.Swarm.AutoTitle = false
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
	h.app.Config.Swarm.AutoTitle = false
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

func TestEventStreamCarriesTheGeneratedTitle(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	fresh := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	if fresh["thread"].(map[string]any)["title_auto"] != true {
		t.Fatal("an untitled conversation must stay machine-owned")
	}
	created := h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "look into the reporting pipeline"}, http.StatusAccepted)
	turn := created["turn"].(map[string]any)
	turnID := turn["id"].(string)
	h.waitForTitleEvent(turnID)

	events, err := h.app.Engine.Replay(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var title store.Event
	for _, ev := range events {
		if ev.Kind == engine.KindTitle {
			title = ev
			break
		}
	}
	if title.Seq == 0 || title.Text == "" || title.AgentID != engine.TitleAgentID {
		t.Fatalf("stored title event=%+v", title)
	}

	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	th := got["thread"].(map[string]any)
	if th["title"] != title.Text {
		t.Fatalf("GET thread title=%v event=%q", th["title"], title.Text)
	}
	if th["title_auto"] != false {
		t.Fatal("a landed name must not stay machine-owned")
	}
	if title.Text == "look into the reporting pipeline" {
		t.Fatal("the conversation is still quoting the request")
	}

	trace := h.json(http.MethodGet, "/api/trace/"+turnID, nil, http.StatusOK)
	found := false
	for _, raw := range trace["events"].([]any) {
		ev := raw.(map[string]any)
		if ev["kind"] == engine.KindTitle {
			found = true
			if ev["text"] != title.Text || ev["agent_id"] != engine.TitleAgentID {
				t.Fatalf("trace title=%v", ev)
			}
		}
	}
	if !found {
		t.Fatal("zwai trace would miss the namer; the title event is not on the turn")
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
	if meta["capabilities"].(map[string]any)["open_url"] != true {
		t.Fatalf("desktop mode should advertise open_url: %v", meta)
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
	th, err := first.Engine.CreateThread("Survivor", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Engine.StartTurn(th.ID, "start something long"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	// Quit: in-memory run stops, the turn stays unfinished for the next start.
	first.Search.Stop()
	first.Engine.Shutdown()
	_ = first.Store.Close()

	second, err := app.New(app.Options{DataDir: dir, Mock: true, NoAssets: true})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { second.Search.Stop(); second.Engine.Shutdown(); _ = second.Store.Close() }()

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
	if len(turns) == 0 {
		t.Fatal("the turn did not survive the restart")
	}
	// The leftover is either already running again or already finished; it
	// must not have been recorded as a user stop.
	for _, turn := range turns {
		if turn.Status == store.TurnCancelled {
			t.Fatalf("a crash was recorded as a stop: %+v", turn)
		}
	}
	if !second.Engine.Status(th.ID).Running {
		got, err := second.Store.GetTurn(turns[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TurnDone {
			t.Fatalf("the leftover turn was neither resumed nor finished: %+v", got)
		}
	}
}

func TestRestartKeepsTheFollowupQueue(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()

	first, err := app.New(app.Options{DataDir: dir, Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := first.Engine.CreateThread("Survivor", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{
		ThreadID:   th.ID,
		UserText:   "continue the leftover request",
		ProviderID: th.ProviderID,
		Model:      th.Model,
	}
	if err := first.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Store.EnqueueFollowup(th.ID, "after this finishes"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Store.EnqueueFollowup(th.ID, "then that"); err != nil {
		t.Fatal(err)
	}
	first.Search.Stop()
	first.Engine.Shutdown()
	_ = first.Store.Close()

	second, err := app.New(app.Options{DataDir: dir, Mock: true, NoAssets: true})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { second.Search.Stop(); second.Engine.Shutdown(); _ = second.Store.Close() }()

	want := []string{"after this finishes", "then that"}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		left, err := second.Engine.ListFollowups(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		turns, err := second.Store.ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, f := range left {
			seen[f.Text] = true
		}
		for _, tn := range turns {
			seen[tn.UserText] = true
		}
		ok := true
		for _, text := range want {
			if !seen[text] {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("queued follow-ups vanished after a restart")
}

func TestNotifyKindsAreStableAcrossTheWire(t *testing.T) {
	// The front end matches on these strings, so a rename in the library must
	// not silently change the protocol.
	for _, want := range []string{
		"agent_message", "spawned", "finished", "tool_call", "tool_result", "tool_delta",
		"turn", "delta", "reasoning_delta", "done", "error",
	} {
		if _, ok := swarm.ParseNotifyKind(want); !ok {
			t.Fatalf("the event kind %q the UI relies on no longer exists", want)
		}
	}
	// The engine's own kinds sit in the same column of the same table and are
	// matched by the same front end, so renaming one is just as breaking.
	for _, tc := range []struct{ got, want string }{
		{engine.KindUser, "user_message"},
		{engine.KindReasoning, "reasoning"},
		{engine.KindSteer, "steer"},
		{engine.KindSteerRetracted, "steer_retracted"},
		{engine.KindSteerPreempted, "steer_preempted"},
		{engine.KindCleanup, "cleanup"},
		{engine.KindProgress, "progress"},
		{engine.KindMemoryReview, "memory_review"},
		{engine.KindTitle, "title"},
		{engine.KindSessionMemory, "session_memory"},
		{engine.KindMaxIterations, "max_iterations"},
		{engine.KindMaxIterationsContinued, "max_iterations_continued"},
		{engine.KindModelRetry, "model_retry"},
		{engine.KindResumed, "resumed"},
		{engine.KindGoal, "goal"},
		{engine.KindGoalComplete, "goal_complete"},
		{engine.KindGoalContinued, "goal_continued"},
		{engine.KindGoalCapped, "goal_capped"},
		{engine.KindGoalBlocked, "goal_blocked"},
		{engine.KindGoalEdited, "goal_edited"},
		{engine.KindGoalResumed, "goal_resumed"},
		{engine.KindGoalIdle, "goal_idle"},
		{engine.KindPlan, "plan"},
		{engine.KindPlanUpdated, "plan_updated"},
		{engine.KindPlanImplemented, "plan_implemented"},
		{engine.KindPlanCancelled, "plan_cancelled"},
		{engine.KindGoalSession, "goal_session"},
		{engine.KindCompacted, "compacted"},
		{engine.KindUsage, "usage"},
		{engine.KindRewound, "rewound"},
		{engine.KindSchedule, "schedule"},
		{engine.KindScheduleFired, "schedule_fired"},
		{engine.KindScheduleSkipped, "schedule_skipped"},
		{engine.KindScheduleReport, "schedule_report"},
		{engine.KindScheduleCancelled, "schedule_cancelled"},
	} {
		if tc.got != tc.want {
			t.Fatalf("the event kind %q the UI relies on is now sent as %q", tc.want, tc.got)
		}
	}
}
