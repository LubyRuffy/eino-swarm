package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkerMCPFailureBlocksItsManagerAndKeepsTheOriginalCall(t *testing.T) {
	workerMayFinish := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(workerMayFinish) }) }
	server, calls := engineMCPFixtureWithProgress(t, func(ctx context.Context, _ *mcp.CallToolRequest) error {
		select {
		case <-workerMayFinish:
			return errors.New("fixture refused the lookup")
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(unblock)
	_, remoteCall := mcpclient.ProxyNames(server.ID)
	var managerCalls atomic.Int32
	managerCancelled := make(chan struct{})
	fixtureFinished := make(chan struct{})
	var workerCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model fixture: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		manager := false
		for _, offered := range request.Tools {
			manager = manager || offered.Function.Name == "spawn_agent"
		}
		if manager {
			switch managerCalls.Add(1) {
			case 1:
				scriptedToolReply(w, request, "assign-lookup", "spawn_agent", `{"role":"research","task":"Use the remote record tool."}`)
			case 2:
				// The request is already in flight when the worker fails. It must
				// return even though this model response has no other release.
				unblock()
				select {
				case <-r.Context().Done():
					close(managerCancelled)
				case <-fixtureFinished:
				}
			default:
				t.Error("manager requested after a worker MCP failure")
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "unexpected"}, "stop")
			}
			return
		}
		if workerCalls.Add(1) == 1 {
			scriptedToolReply(w, request, "worker-lookup", remoteCall, `{"name":"inspect_record","arguments":{"key":"record"}}`)
			return
		}
		t.Error("worker requested after its MCP failure")
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "unexpected"}, "stop")
	}))
	t.Cleanup(model.Close)
	defer close(fixtureFinished)

	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Swarm.ComposerPredictions, e.cfg.Memory.Enabled = false, false, false
	e.cfg.Models.Providers[0].BaseURL = model.URL
	e.cfg.Models.Providers[0].Model = "offline-mcp-worker"
	e.pool = provider.New(e.cfg)
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.MCP.Servers, c.MCP.DirectLoadPercent = []config.MCPServer{server}, 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Worker MCP fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Assign the remote lookup and wait for its result.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "MCP tool failed") ||
		managerCalls.Load() != 2 || workerCalls.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("status=%s error=%q manager/worker/remote calls=%d/%d/%d",
			finished.Status, finished.Error, managerCalls.Load(), workerCalls.Load(), calls.Load())
	}
	select {
	case <-managerCancelled:
	case <-t.Context().Done():
		t.Fatal("the worker failure left the manager HTTP request in flight")
	case <-time.After(5 * time.Second):
		t.Fatal("the worker failure left the manager HTTP request in flight")
	}
	rows, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Kind == store.KindMCPExecutionBlocked && row.ToolCallID == "worker-lookup" && !isManagerAgent(row.AgentID) {
			return
		}
	}
	t.Fatal("the durable task block lost the original worker or tool call identity")
}
