package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The manager resends its system prompt and tool definitions on every model
// call, about 7K tokens before any conversation. Providers that cache a
// stable prefix charge that once; any per-call or per-turn variation in it
// (a timestamp, a reordered tool list, a live counter) silently turns every
// call into a full-price miss. Hold that prefix byte-identical across the
// calls of one turn and across turns of one conversation.
func TestManagerPrefixStaysIdenticalAcrossCallsAndTurns(t *testing.T) {
	type prefix struct{ system, tools string }
	var mu sync.Mutex
	var seen []prefix
	var calls int
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
			Stream   bool              `json:"stream"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
			t.Errorf("request: err=%v", err)
			return
		}
		if replyNoScheduleClaimsFixture(t, w, req.Messages[0].Content, req.Stream, len(req.Tools)) {
			return
		}
		if !strings.Contains(req.Messages[0].Content, "## Waiting") {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "The fixture state was checked."}, "stop")
			return
		}
		tools, _ := json.Marshal(req.Tools)
		mu.Lock()
		seen = append(seen, prefix{req.Messages[0].Content, string(tools)})
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "", "tool_calls": []any{map[string]any{"index": 0, "id": "list-workspace", "type": "function", "function": map[string]any{"name": "ls", "arguments": "{}"}}}}, "tool_calls")
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The workspace was inspected."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-prefix-fixture"
	e.pool = provider.New(e.cfg)

	th, err := e.CreateThread("prefix stability", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Inspect the workspace.", "Inspect it again."} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
			t.Fatalf("turn=%+v", got)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("manager calls=%d, want two in the first turn and one in the second", len(seen))
	}
	for i, p := range seen[1:] {
		if p.system != seen[0].system {
			t.Fatalf("call %d changed the manager system prompt:\nfirst:\n%s\nlater:\n%s", i+2, seen[0].system, p.system)
		}
		if p.tools != seen[0].tools {
			t.Fatalf("call %d changed the tool definitions", i+2)
		}
	}
	if seen[0].tools == "null" || len(seen[0].tools) < 100 {
		t.Fatalf("the fixture did not capture tool definitions: %s", seen[0].tools)
	}
}
