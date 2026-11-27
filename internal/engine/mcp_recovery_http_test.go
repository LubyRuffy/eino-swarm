package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolReauthenticatesOnlyForAnExpiredSession(t *testing.T) {
	for _, tc := range []struct {
		name      string
		first     int
		next      int
		direct    bool
		wantCalls int
		wantExec  int32
		timeout   bool
		toolError bool
		rateLimit bool
		ok        bool
	}{
		{name: "proxy renews one expired binding", first: http.StatusNotFound, wantCalls: 2, wantExec: 1, ok: true},
		{name: "direct renews one expired binding", first: http.StatusNotFound, direct: true, wantCalls: 2, wantExec: 1, ok: true},
		{name: "repeat expiration pauses the task", first: http.StatusNotFound, next: http.StatusNotFound, wantCalls: 2},
		{name: "authentication failure pauses the task", first: http.StatusUnauthorized, wantCalls: 1},
		{name: "proxy continues after a call deadline", timeout: true, wantCalls: 2, wantExec: 2, ok: true},
		{name: "direct continues after a call deadline", timeout: true, direct: true, wantCalls: 2, wantExec: 2, ok: true},
		{name: "expiry after a deadline renews only the later call", timeout: true, next: http.StatusNotFound, wantCalls: 3, wantExec: 2, ok: true},
		{name: "authentication failure after a deadline stops the task", timeout: true, next: http.StatusUnauthorized, wantCalls: 2, wantExec: 1},
		{name: "remote tool error pauses without retry", toolError: true, wantCalls: 1, wantExec: 1},
		{name: "proxy continues after a rate limit", rateLimit: true, wantCalls: 2, wantExec: 2, ok: true},
		{name: "direct continues after a rate limit", rateLimit: true, direct: true, wantCalls: 2, wantExec: 2, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "transport fixture", Version: "test"}, nil)
			var ran atomic.Int32
			stopFixture := make(chan struct{})
			server.AddTool(&mcp.Tool{Name: "lookup", InputSchema: map[string]any{"type": "object"}},
				func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					ran.Add(1)
					if tc.timeout {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-stopFixture:
							return nil, context.Canceled
						}
					}
					if tc.rateLimit {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{
							&mcp.TextContent{Text: "Upstream HTTP 429 Too Many Requests"},
						}}, nil
					}
					if tc.toolError {
						// The remote operation ran. Validation-like prose cannot prove
						// that it was rejected before execution.
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{
							&mcp.TextContent{Text: "Missing required parameter: lookup_key"},
						}}, nil
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "lookup verified"}}}, nil
				})
			server.AddTool(&mcp.Tool{Name: "verify", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					ran.Add(1)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "independent lookup verified"}}}, nil
				})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
			var mu sync.Mutex
			var requestedSessions []string
			var requestedTools []string
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("read fixture request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var method struct {
						Method string
						Params struct{ Name string }
					}
					if err := json.Unmarshal(raw, &method); err != nil {
						t.Errorf("decode fixture request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
					if method.Method == "tools/call" {
						mu.Lock()
						requestedSessions = append(requestedSessions, r.Header.Get("Mcp-Session-Id"))
						requestedTools = append(requestedTools, method.Params.Name)
						count := len(requestedSessions)
						mu.Unlock()
						var code int
						switch count {
						case 1:
							code = tc.first
						case 2:
							code = tc.next
						}
						if tc.first == http.StatusUnauthorized && r.Header.Get("Authorization") != "Bearer repaired-fixture" {
							code = http.StatusUnauthorized
						}
						if code != 0 {
							http.Error(w, http.StatusText(code), code)
							return
						}
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(remote.Close)
			t.Cleanup(func() { close(stopFixture) })

			s := config.MCPServer{ID: "evidence", Enabled: true, Transport: "streamable_http", URL: remote.URL, TimeoutSeconds: 2}
			_, toolName := mcpclient.ProxyNames(s.ID)
			args := `{"name":"lookup","arguments":{}}`
			nextTool, nextArgs := toolName, `{"name":"verify","arguments":{}}`
			if tc.direct {
				toolName = mcpclient.DirectToolName(s.ID, "lookup")
				args = "{}"
				nextTool, nextArgs = mcpclient.DirectToolName(s.ID, "verify"), "{}"
			}
			e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					scriptedToolReply(w, request, "lookup-call", toolName, args)
				case 2:
					if tc.timeout {
						receipt := scriptedToolReceipt(request, "lookup-call")
						if !strings.Contains(receipt, `"code":"timeout"`) || !strings.Contains(receipt, `"outcome":"unknown"`) {
							t.Errorf("the model lost the timeout or uncertain outcome: %q", receipt)
						}
						scriptedToolReply(w, request, "independent-call", nextTool, nextArgs)
						return
					}
					if tc.rateLimit {
						receipt := scriptedToolReceipt(request, "lookup-call")
						if !strings.Contains(receipt, "Upstream HTTP 429 Too Many Requests") ||
							!strings.Contains(receipt, "Verify it before repeating") || !strings.Contains(receipt, `"isError":true`) {
							t.Errorf("the original model receipt lost the rate limit: %q", receipt)
						}
						scriptedToolReply(w, request, "independent-call", nextTool, nextArgs)
						return
					}
					if tc.first == http.StatusUnauthorized {
						scriptedToolReply(w, request, "repaired-lookup", toolName, args)
						return
					}
					if tc.ok && !strings.Contains(scriptedToolReceipt(request, "lookup-call"), "lookup verified") {
						t.Error("the model did not receive the recovered result")
					}
					scriptedToolReply(w, request, "next-tool", "exec", `{"command":"printf should-run-only-after-a-success"}`)
				default:
					if (tc.timeout || tc.rateLimit) && !strings.Contains(scriptedToolReceipt(request, "independent-call"), "independent lookup verified") {
						t.Error("the model did not receive the later independent result")
					}
					if tc.first == http.StatusUnauthorized && !strings.Contains(scriptedToolReceipt(request, "repaired-lookup"), "lookup verified") {
						t.Error("the human retry did not receive the repaired result")
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "Fixture finished."}, "stop")
				}
			})
			e.cfg.Swarm.ComposerPredictions = false
			e.cfg.Memory.Enabled = false
			e.cfg.MCP.DirectLoadPercent = 0
			if tc.direct {
				e.cfg.MCP.DirectLoadPercent = 100
			}
			if err := e.UpdateConfig(func(cfg *config.Config) error {
				cfg.MCP.Servers = []config.MCPServer{s}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("MCP transport fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Read this record with the remote tool.")
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			mu.Lock()
			ids := append([]string(nil), requestedSessions...)
			toolNames := append([]string(nil), requestedTools...)
			mu.Unlock()
			if len(ids) != tc.wantCalls || ids[0] == "" || ran.Load() != tc.wantExec {
				t.Fatalf("MCP wire calls=%v, executed=%d, status=%s", toolNames, ran.Load(), finished.Status)
			}
			if tc.timeout {
				if toolNames[0] != "lookup" || toolNames[1] != "verify" || ids[1] != ids[0] {
					t.Error("the deadline replayed the original call or changed the session for the next call")
				}
				if len(ids) == 3 && (toolNames[2] != "verify" || ids[2] == "" || ids[2] == ids[0]) {
					t.Error("confirmed expiry did not renew the session for just the later call")
				}
				events, err := e.store.ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				var timeoutReceipts int
				for _, event := range events {
					if event.Kind == store.KindMCPExecutionBlocked && event.ToolCallID == "lookup-call" {
						t.Error("the timeout left a durable execution block")
					}
					if event.Kind == "tool_result" && event.ToolCallID == "lookup-call" &&
						strings.Contains(event.Text, `"code":"timeout"`) && strings.Contains(event.Text, `"outcome":"unknown"`) {
						timeoutReceipts++
					}
				}
				if timeoutReceipts != 1 {
					t.Errorf("durable timeout receipts = %d, want 1", timeoutReceipts)
				}
			} else if tc.rateLimit {
				if toolNames[0] != "lookup" || toolNames[1] != "verify" || ids[1] != ids[0] {
					t.Error("rate limit replayed the call or replaced the session")
				}
				events, err := e.store.ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				receipts := 0
				for _, event := range events {
					if event.Kind == "tool_result" && event.ToolCallID == "lookup-call" &&
						strings.Contains(event.Text, "Upstream HTTP 429 Too Many Requests") {
						receipts++
					}
				}
				if receipts != 1 {
					t.Fatalf("durable rate limit receipts = %d, want 1", receipts)
				}
			} else if len(ids) == 2 && (ids[1] == "" || ids[0] == ids[1]) {
				t.Error("confirmed expiry did not renew the session")
			}
			if !tc.ok {
				recovered := e.restoredMCPFailure(turn.ID, nil)
				if recovered == nil || !strings.Contains(recovered.Error(), "MCP") {
					t.Errorf("restore lost the blocking failure: %v", recovered)
				}
				wantRequests := 1
				if tc.timeout {
					wantRequests = 2
					if recovered == nil || !strings.Contains(recovered.Error(), "call_failed") {
						t.Errorf("restore did not retain the later authentication failure: %v", recovered)
					}
				}
				if finished.Status != store.TurnError || len(requests.snapshot()) != wantRequests || !strings.Contains(finished.Error, "MCP") {
					t.Fatalf("MCP failure did not stop the task: status=%s model calls=%d error=%q", finished.Status, len(requests.snapshot()), finished.Error)
				}
			} else if finished.Status != store.TurnDone || len(requests.snapshot()) != 3 {
				t.Fatalf("recovered task did not finish: status=%s model calls=%d error=%q", finished.Status, len(requests.snapshot()), finished.Error)
			} else if tc.timeout || tc.rateLimit {
				if recovered := e.restoredMCPFailure(turn.ID, nil); recovered != nil {
					t.Errorf("recovery would reinstate a tool block: %v", recovered)
				}
			}

			if tc.first == http.StatusUnauthorized {
				// Simulate interruption after the adapter receipt but before the
				// terminal event and row. Startup must refuse a new upstream request.
				if err := e.store.DB().Where("turn_id = ? AND kind = ?", turn.ID, "error").Delete(&store.Event{}).Error; err != nil {
					t.Fatal(err)
				}
				if err := e.store.UpdateTurn(turn.ID, map[string]any{"status": store.TurnRunning, "ended_at": nil}); err != nil {
					t.Fatal(err)
				}
				ask := store.Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: "manager", Kind: "tool_call",
					ToolCallID: "unanswered-before-interruption",
					Text:       `ask_user {"questions":[{"id":"q","prompt":"Choose a scope","options":[{"id":"a","label":"One"},{"id":"b","label":"All"}]}]}`}
				if err := e.store.AppendEvent(&ask); err != nil {
					t.Fatal(err)
				}
				if count, err := e.ResumeOrphanedTurns(); err != nil || count != 1 {
					t.Fatalf("resume the interrupted task: count=%d error=%v", count, err)
				}
				after := waitForTurn(t, e, turn.ID)
				waitSettled(t, e, thread.ID)
				if after.Status != store.TurnError || len(requests.snapshot()) != 1 ||
					!strings.Contains(after.Error, "MCP tool failed") {
					t.Fatalf("recovery lost the task block: status=%s model requests=%d error=%s", after.Status, len(requests.snapshot()), after.Error)
				}
				events, err := e.store.ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					if event.Kind == KindAskRecovered {
						t.Fatal("startup re-armed a question in an MCP-blocked task")
					}
				}
				if err := e.UpdateConfig(func(cfg *config.Config) error {
					cfg.MCP.Servers[0].Headers = map[string]string{"Authorization": "Bearer repaired-fixture"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				retried, err := e.RetryFailedTurn(thread.ID, turn.ID)
				if err != nil || retried.ID != turn.ID {
					t.Fatalf("explicit retry lost the failed turn: turn=%+v err=%v", retried, err)
				}
				repaired := waitForTurn(t, e, turn.ID)
				waitSettled(t, e, thread.ID)
				if repaired.Status != store.TurnDone || len(requests.snapshot()) != 3 || ran.Load() != 1 {
					t.Fatalf("repair did not resume the original task: status=%s model requests=%d remote executions=%d",
						repaired.Status, len(requests.snapshot()), ran.Load())
				}
			}

		})
	}
}
