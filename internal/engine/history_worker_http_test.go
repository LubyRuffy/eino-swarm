package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

// A manager can delegate the history survey. Inspect the real worker model
// request/result, not merely the manager's tools or Registry spawn text.
func TestDelegatedWorkerQueriesOriginalProjectHistoryAtItsHTTPBoundary(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("Worker source project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original evidence", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	const source = "The observed result has not been verified."
	if err := e.Store().AppendMessages(peer.ID, "", []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: source}}); err != nil {
		t.Fatal(err)
	}
	current, err := e.CreateThread("Delegation", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	steps := 0
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		mu.Lock()
		steps++
		step := steps
		mu.Unlock()
		available := false
		for _, tl := range request.Tools {
			available = available || tl.Function.Name == ToolQueryHistory
		}
		if !available || len(request.Messages) == 0 || !strings.Contains(request.Messages[0].Content, project.ID) || !strings.Contains(request.Messages[0].Content, current.ID) {
			t.Error("worker model input lost the mounted history tool or current identity")
		}
		if step == 1 {
			if strings.Contains(request.Messages[0].Content, source) {
				t.Error("source text was injected instead of read")
			}
			args, _ := json.Marshal(map[string]any{"sql": "SELECT thread_id,seq,role,content FROM messages WHERE thread_id=? ORDER BY seq", "params": []string{peer.ID}})
			writeScheduleCompletionTool(w, request, ToolQueryHistory, string(args))
			return
		}
		found := false
		for _, message := range request.Messages {
			if message.Role == "tool" && strings.Contains(message.Content, source) && strings.Contains(message.Content, peer.ID) && strings.Contains(message.Content, `"ok":true`) {
				found = true
			}
		}
		if !found {
			t.Error("worker's SQL result did not reach its actual next model request")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Original source read: " + peer.ID}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	p := &e.cfg.Models.Providers[0]
	p.BaseURL = endpoint.URL
	p.Model = "offline-history-worker"
	e.pool = provider.New(e.cfg)
	builder, err := e.pool.ModelBuilder(context.Background(), p.ID, p.Model, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(current)
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, pc)
	t.Cleanup(reg.Close)
	owner := mustStoredTurn(t, e, current.ID, store.Turn{})
	if err := e.store.FinishTurn(owner.ID, store.TurnDone, "Original manager check ended.", ""); err != nil {
		t.Fatal(err)
	}
	ownerCtx := withHistoryReadOwner(t.Context(), current.ID, owner.ID)
	var spawnTool, waitTool tool.InvokableTool
	for _, base := range reg.Tools() {
		info, err := base.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		switch info.Name {
		case "spawn_agent":
			spawnTool = base.(tool.InvokableTool)
		case "wait_agents":
			waitTool = base.(tool.InvokableTool)
		}
	}
	if spawnTool == nil || waitTool == nil {
		t.Fatal("manager lifecycle tools are absent")
	}
	spawned, err := spawnTool.InvokableRun(ownerCtx, `{"role":"historian","task":"Read the original earlier project record and retain its source identity."}`)
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(spawned), &identity); err != nil || identity.AgentID == "" {
		t.Fatalf("spawn receipt=%s %v", spawned, err)
	}
	waitArgs, _ := json.Marshal(map[string]any{"agent_ids": []string{identity.AgentID}, "timeout_s": 10})
	waitCtx, cancel := context.WithTimeout(ownerCtx, 10*time.Second)
	defer cancel()
	result, err := waitTool.InvokableRun(waitCtx, string(waitArgs))
	if err != nil || !strings.Contains(result, peer.ID) || !strings.Contains(result, `"status":"done"`) {
		t.Fatalf("worker collection=%s %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if steps != 2 {
		t.Fatalf("worker requests=%d want query then original-source result", steps)
	}
}
