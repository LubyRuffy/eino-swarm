package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMountingMCPProxiesIsPureAndRetiredUnconnectedToolsCannotLaunch(t *testing.T) {
	var requests atomic.Int32
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected connection", 500)
	}))
	defer h.Close()
	s := config.MCPServer{ID: "pure", Name: "Named data source", Transport: "streamable_http", URL: h.URL, Enabled: true}
	m := NewManager()
	defer m.Close()
	set, err := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
	if err != nil || len(set) != 2 {
		t.Fatalf("mount %d %v", len(set), err)
	}
	for _, base := range set {
		info, err := base.Info(t.Context())
		if err != nil || !strings.Contains(info.Desc, s.Name) || !strings.Contains(info.Desc, s.ID) {
			t.Fatalf("server identity missing: %+v %v", info, err)
		}
	}
	if requests.Load() != 0 || len(m.entries) != 0 {
		t.Fatal("tool construction connected to an MCP server")
	}
	m.Reload(nil)
	out, err := set[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	if err != nil || !strings.Contains(out, "session_expired") || requests.Load() != 0 {
		t.Fatalf("retired proxy launched old configuration: %s %v requests=%d", out, err, requests.Load())
	}
}

func TestMoreThanEightConfiguredServersCanAllExecuteInOneWorkspace(t *testing.T) {
	s, calls := testServer(t, "streamable_http")
	var servers []config.MCPServer
	for i := 0; i < maxIdleSessions+3; i++ {
		next := cloneServer(s)
		next.ID = fmt.Sprintf("source-%d", i)
		servers = append(servers, next)
	}
	m := NewManager()
	defer m.Close()
	set, err := m.Tools(t.Context(), servers, t.TempDir(), 10000)
	if err != nil || len(set) != 2*len(servers) {
		t.Fatalf("mount %d %v", len(set), err)
	}
	for i := 0; i < len(set); i += 2 {
		out, err := set[i].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
		if err != nil || !strings.Contains(out, `"name":"lookup"`) {
			t.Fatalf("discovery %d %s %v", i, out, err)
		}
	}
	for i := 1; i < len(set); i += 2 {
		out, err := set[i].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
		if err != nil || strings.Contains(out, `"error"`) {
			t.Fatalf("call %d %s %v", i, out, err)
		}
	}
	if calls.Load() != int32(len(servers)) {
		t.Fatalf("some configured servers could not execute: %d", calls.Load())
	}
}

func TestMCPDiscoverySupportsPagingAndExactSchemaLookup(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "paged", Version: "test"}, nil)
	for i := 0; i < 3; i++ {
		s.AddTool(&mcp.Tool{Name: fmt.Sprintf("record_%d", i), InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer h.Close()
	cfg := config.MCPServer{ID: "pages", Transport: "streamable_http", URL: h.URL, Enabled: true}
	m := NewManager()
	defer m.Close()
	set, _ := m.Tools(t.Context(), []config.MCPServer{cfg}, t.TempDir(), 10000)
	for _, args := range []string{`{"offset":-1}`, `{"limit":0}`, `{"offset":"wrong"}`} {
		out, _ := set[0].(tool.InvokableTool).InvokableRun(t.Context(), args)
		if !strings.Contains(out, "invalid_arguments") {
			t.Fatalf("invalid page accepted: %s", out)
		}
	}
	for _, tc := range []struct {
		args         string
		count, total int
		more         bool
	}{{`{"offset":1,"limit":1}`, 1, 3, true}, {`{"name":"record_2"}`, 1, 1, false}, {`{"name":"absent"}`, 0, 0, false}, {`{"offset":20}`, 0, 3, false}} {
		out, err := set[0].(tool.InvokableTool).InvokableRun(t.Context(), tc.args)
		var got struct {
			Tools []ToolDescriptor `json:"tools"`
			Total int              `json:"total"`
			More  bool             `json:"more"`
		}
		if err != nil || json.Unmarshal([]byte(out), &got) != nil || len(got.Tools) != tc.count || got.Total != tc.total || got.More != tc.more {
			t.Fatalf("page %s: %s %v", tc.args, out, err)
		}
	}
	for _, args := range []string{`{}`, `{"name":"record_0"}`, `{"name":"record_0","arguments":[]}`} {
		out, _ := set[1].(tool.InvokableTool).InvokableRun(t.Context(), args)
		if !strings.Contains(out, "invalid_arguments") {
			t.Fatalf("invalid call accepted: %s", out)
		}
	}
	out, _ := set[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"absent","arguments":{}}`)
	if !strings.Contains(out, "unknown_tool") {
		t.Fatalf("unknown tool executed: %s", out)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, _ = set[0].(tool.InvokableTool).InvokableRun(ctx, "{}")
	if !strings.Contains(out, "cancelled") {
		t.Fatalf("cancelled proxy: %s", out)
	}
}

func TestConcurrentDiscoveryWaiterCancelsWithoutWaitingForTheFirstHandshake(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer h.Close()
	defer close(release)
	s := config.MCPServer{ID: "blocked", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 60}
	m := NewManager()
	defer m.Close()
	set, _ := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
	first, stopFirst := context.WithCancel(t.Context())
	defer stopFirst()
	second, stopSecond := context.WithCancel(t.Context())
	defer stopSecond()
	doneFirst, doneSecond := make(chan string, 1), make(chan string, 1)
	go func() { out, _ := set[0].(tool.InvokableTool).InvokableRun(first, "{}"); doneFirst <- out }()
	<-started
	go func() { out, _ := set[0].(tool.InvokableTool).InvokableRun(second, "{}"); doneSecond <- out }()
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		m.mu.Lock()
		uses := 0
		for _, e := range m.entries {
			uses = e.uses
		}
		m.mu.Unlock()
		if uses == 2 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		t.Fatal("second discovery did not enter the shared handshake wait")
	}
	stopSecond()
	select {
	case out := <-doneSecond:
		if !strings.Contains(out, "cancelled") {
			t.Fatalf("waiter cancellation %s", out)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter remained behind handshake lock")
	}
	select {
	case <-doneFirst:
		t.Fatal("waiter cancellation interrupted another caller")
	default:
	}
	stopFirst()
	select {
	case <-doneFirst:
	case <-time.After(2 * time.Second):
		t.Fatal("first handshake ignored cancellation")
	}
	select {
	case <-started:
		t.Fatal("a failed handshake was replayed")
	default:
	}
}

func TestCancelledDiscoveryWaiterDoesNotRetireTheHealthySessionOwner(t *testing.T) {
	entered := make(chan struct{})
	allow := make(chan struct{})
	var initialized atomic.Bool
	var calls atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "concurrent", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "perform", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "healthy session"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var request struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(raw, &request)
			if request.Method == "initialize" && initialized.CompareAndSwap(false, true) {
				close(entered)
				select {
				case <-allow:
				case <-r.Context().Done():
					return
				}
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer h.Close()
	defer func() {
		select {
		case <-allow:
		default:
			close(allow)
		}
	}()
	m := NewManager()
	defer m.Close()
	set, _ := m.Tools(t.Context(), []config.MCPServer{{ID: "concurrent", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 60}}, t.TempDir(), 10000)
	firstDone, secondDone := make(chan string, 1), make(chan string, 1)
	go func() { out, _ := set[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}"); firstDone <- out }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { out, _ := set[0].(tool.InvokableTool).InvokableRun(ctx, "{}"); secondDone <- out }()
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		m.mu.Lock()
		uses := 0
		for _, e := range m.entries {
			uses = e.uses
		}
		m.mu.Unlock()
		if uses == 2 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		t.Fatal("second discovery did not enter the shared handshake wait")
	}
	cancel()
	select {
	case out := <-secondDone:
		if !strings.Contains(out, "cancelled") {
			t.Fatalf("waiter result %s", out)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not cancel")
	}
	close(allow)
	select {
	case out := <-firstDone:
		if strings.Contains(out, `"error"`) {
			t.Fatalf("healthy owner failed %s", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not finish handshake")
	}
	out, _ := set[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"perform","arguments":{}}`)
	if !strings.Contains(out, "healthy session") || calls.Load() != 1 {
		t.Fatalf("cancelled waiter retired healthy session: %s calls=%d", out, calls.Load())
	}
}
