package server_test

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDesktopRecoveryHTTPWritesTheSameTurnFullTrace(t *testing.T) {
	h := newHarness(t)
	th := h.newThread()
	turn := &store.Turn{ThreadID: th}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"session_id": "session-1", "generation": 1, "attempt": 1, "shell_pid": 123, "engine_pid": os.Getpid(), "thread_id": th, "turn_id": turn.ID, "phase": "recovered", "reason": "paint_timeout", "at": time.Now().UTC(), "duration_ms": 1000}
	h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusOK)
	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	events := trace["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["kind"] != "desktop_recovery" || !strings.Contains(events[0].(map[string]any)["text"].(string), "session-1") {
		t.Fatalf("missing recovery in trace: %+v", trace)
	}
	d["thread_id"] = "wrong"
	h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusBadRequest)
	d["thread_id"] = th
	delete(d, "turn_id")
	metadata := h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusOK)
	if metadata["persisted"] != false {
		t.Fatal("empty conversation fabricated a turn receipt", metadata)
	}
	d["thread_id"] = "missing"
	h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusNotFound)
	delete(d, "thread_id")
	delete(d, "turn_id")
	h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusOK)
	for _, change := range []struct {
		key   string
		value any
	}{{"engine_pid", os.Getpid() + 1}, {"shell_pid", -1}, {"phase", "arbitrary"}, {"reason", strings.Repeat("x", 257)}, {"attempt", -1}, {"session_id", ""}} {
		copy := map[string]any{}
		for k, v := range d {
			copy[k] = v
		}
		copy[change.key] = change.value
		h.json(http.MethodPost, "/api/desktop/recovery", copy, http.StatusBadRequest)
	}
	h.json(http.MethodPost, "/api/desktop/recovery", map[string]string{"unexpected": "reject"}, http.StatusBadRequest)
	request, err := http.NewRequest(http.MethodPost, h.srv.URL+"/api/desktop/recovery", bytes.NewBufferString(`{} {}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := h.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal("multiple diagnostic records accepted in one body", response.StatusCode)
	}
	if err := h.app.Store.DB().Exec("CREATE TRIGGER recovery_disk_failure BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'disk full'); END").Error; err != nil {
		t.Fatal(err)
	}
	d["thread_id"], d["turn_id"] = th, turn.ID
	h.json(http.MethodPost, "/api/desktop/recovery", d, http.StatusBadRequest)
}
