package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Unchecking a tool or MCP server during a turn changes the next model request.
// The toolset built at the start of the turn is not the list that gets sent.
func TestUncheckedToolsLeaveTheNextModelRequest(t *testing.T) {
	listName, callName := mcpclient.ProxyNames("desk")
	var e *Engine
	var wire *scheduleCompletionHTTP
	e, wire = newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			if !requestHasTool(request, "web_search") || !requestHasTool(request, listName) || !requestHasTool(request, callName) {
				t.Errorf("first request missing a checked tool: %s", requestToolNames(request))
			}
			if err := e.cfg.Update(func(cfg *config.Config) error {
				cfg.Tools.Disabled = append(cfg.Tools.Disabled, "web_search")
				for i := range cfg.MCP.Servers {
					if cfg.MCP.Servers[i].ID == "desk" {
						cfg.MCP.Servers[i].Enabled = false
					}
				}
				return nil
			}); err != nil {
				t.Errorf("uncheck: %v", err)
			}
			args, _ := json.Marshal(map[string]any{"command": "printf 'offer-kept\\n'"})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "offer-exec-1", "type": "function",
				"function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		case 2:
			if requestHasTool(request, "web_search") || requestHasTool(request, listName) || requestHasTool(request, callName) {
				t.Errorf("unchecked tools stayed on the next request: %s", requestToolNames(request))
			}
			if !requestHasTool(request, "exec") || !requestHasTool(request, "spawn_agent") || !requestHasTool(request, "read") {
				t.Errorf("unchecked request dropped a tool that stayed on: %s", requestToolNames(request))
			}
			if err := e.cfg.Update(func(cfg *config.Config) error {
				cfg.Tools.Disabled = nil
				for i := range cfg.MCP.Servers {
					if cfg.MCP.Servers[i].ID == "desk" {
						cfg.MCP.Servers[i].Enabled = true
					}
				}
				return nil
			}); err != nil {
				t.Errorf("recheck: %v", err)
			}
			args, _ := json.Marshal(map[string]any{"command": "printf 'offer-restored\\n'"})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "offer-exec-2", "type": "function",
				"function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		case 3:
			if !requestHasTool(request, "web_search") || !requestHasTool(request, listName) || !requestHasTool(request, callName) {
				t.Errorf("rechecked tools did not return on the next request: %s", requestToolNames(request))
			}
			if requestHasTool(request, "python_runner") {
				t.Errorf("an opt-in tool was offered without being checked: %s", requestToolNames(request))
			}
			if err := e.cfg.Update(func(cfg *config.Config) error {
				cfg.Tools.Enabled = append(cfg.Tools.Enabled, "python_runner")
				return nil
			}); err != nil {
				t.Errorf("check python: %v", err)
			}
			args, _ := json.Marshal(map[string]any{"command": "printf 'offer-late\\n'"})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "offer-exec-3", "type": "function",
				"function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		case 4:
			if requestHasTool(request, "python_runner") {
				t.Errorf("a tool switched on after the turn started was mounted immediately: %s", requestToolNames(request))
			}
			if !requestHasTool(request, "web_search") || !requestHasTool(request, "exec") {
				t.Errorf("the late check dropped a tool this turn already had: %s", requestToolNames(request))
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The selection is applied."}, "stop")
		default:
			t.Errorf("unexpected manager request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The selection is applied."}, "stop")
		}
	})
	if err := e.cfg.Update(func(cfg *config.Config) error {
		cfg.Memory.Enabled, cfg.Memory.AutoReview, cfg.Memory.AutoDream = false, false, false
		cfg.Swarm.AutoTitle, cfg.Swarm.ComposerPredictions = false, false
		cfg.MCP.DirectLoadPercent = 0
		cfg.MCP.Servers = []config.MCPServer{{
			ID: "desk", Name: "Desk", Transport: "stdio", Command: "true", Enabled: true,
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Check the current tool selection, then stop.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || len(wire.snapshot()) != 4 {
		t.Fatalf("status=%s requests=%d err=%q", finished.Status, len(wire.snapshot()), finished.Error)
	}
}

func requestHasTool(request scheduleCompletionRequest, name string) bool {
	for _, item := range request.Tools {
		if item.Function.Name == name {
			return true
		}
	}
	return false
}

func requestToolNames(request scheduleCompletionRequest) string {
	names := make([]string, 0, len(request.Tools))
	for _, item := range request.Tools {
		names = append(names, item.Function.Name)
	}
	return strings.Join(names, ",")
}
