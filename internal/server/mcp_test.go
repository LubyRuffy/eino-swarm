package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServersPersistWithoutReturningSecrets(t *testing.T) {
	h := newHarness(t)
	path := "/api/mcp/local"
	input := map[string]any{"name": "Local tools", "transport": "stdio", "command": "test-command", "args": []string{"--stdio"}, "enabled": false, "timeout_seconds": 20, "env": map[string]string{"TOKEN": "private-mcp-secret"}}
	h.json(http.MethodPut, path, input, http.StatusOK)
	resp := h.do(http.MethodGet, "/api/mcp", nil)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || strings.Contains(string(raw), "private-mcp-secret") || !strings.Contains(string(raw), "TOKEN") {
		t.Fatalf("unsafe catalog status=%d err=%v body=%s", resp.StatusCode, err, raw)
	}
	delete(input, "env")
	input["name"] = "Renamed"
	h.json(http.MethodPut, path, input, http.StatusOK)
	stored := h.app.Config.Snapshot().MCP.Servers[0]
	if stored.Env["TOKEN"] != "private-mcp-secret" {
		t.Fatal("omitted private entry was lost")
	}
	input["env"] = map[string]string{}
	h.json(http.MethodPut, path, input, http.StatusOK)
	if len(h.app.Config.Snapshot().MCP.Servers[0].Env) != 0 {
		t.Fatal("explicit clear ignored")
	}
	h.json(http.MethodDelete, path, nil, http.StatusOK)
	h.json(http.MethodDelete, path, nil, http.StatusNotFound)
}

func TestMCPInspectsTheActualProtocolWithoutExposingCredentials(t *testing.T) {
	h := newHarness(t)
	fixture := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	fixture.AddTool(&mcp.Tool{Name: "echo", Description: "Echo input", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture result"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return fixture }, nil)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mcp-private-token" {
			http.Error(w, "no credentials", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)
	input := map[string]any{"name": "Remote", "transport": "streamable_http", "url": remote.URL, "enabled": false, "headers": map[string]string{"Authorization": "Bearer mcp-private-token"}}
	h.json(http.MethodPut, "/api/mcp/remote", input, http.StatusOK)
	delete(input, "headers")
	h.json(http.MethodPut, "/api/mcp/remote", input, http.StatusOK)
	h.json(http.MethodPut, "/api/settings", map[string]any{"ui": map[string]any{"locale": "en"}}, http.StatusOK)
	if h.app.Config.Snapshot().MCP.Servers[0].Headers["Authorization"] != "Bearer mcp-private-token" {
		t.Fatal("unrelated settings or omitted header fields overwrote private configuration")
	}
	got := h.json(http.MethodPost, "/api/mcp/remote/inspect", nil, http.StatusOK)
	if tools, ok := got["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("catalog=%v", got)
	}
	input["headers"] = map[string]string{}
	h.json(http.MethodPut, "/api/mcp/remote", input, http.StatusOK)
	failure := h.json(http.MethodPost, "/api/mcp/remote/inspect", nil, http.StatusBadGateway)
	if failure["reason"] != "connection_failed" {
		t.Fatalf("missing safe failure category: %v", failure)
	}
	resp := h.do(http.MethodGet, "/api/settings", nil)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "mcp-private-token") {
		t.Fatal("settings leaked MCP headers")
	}
}

func TestMCPRefusesMalformedPrivateEntriesAndReportsPersistenceFailure(t *testing.T) {
	h := newHarness(t)
	path := "/api/mcp/private"
	input := map[string]any{"transport": "stdio", "command": "unused", "env": map[string]string{"BAD=KEY": "secret"}}
	h.json(http.MethodPut, path, input, http.StatusBadRequest)
	input["env"] = map[string]string{"OK": "secret"}
	input["headers"] = map[string]string{"X-Key": "bad\r\n"}
	h.json(http.MethodPut, path, input, http.StatusBadRequest)
	h.json(http.MethodPut, path, []string{"not an object"}, http.StatusBadRequest)
	delete(input, "headers")
	if err := os.Mkdir(filepath.Join(h.app.Config.DataDir(), "config.yaml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPut, path, input, http.StatusInternalServerError)
	if len(h.app.Config.Snapshot().MCP.Servers) != 0 {
		t.Fatal("failed save published")
	}
}

func TestMCPCancelledInspectionDoesNotWriteAnErrorResponse(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"other", "cancelled"} {
		h.json(http.MethodPut, "/api/mcp/"+id, map[string]any{"transport": "stdio", "command": "missing-mcp-fixture-command"}, http.StatusOK)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/cancelled/inspect", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	h.app.Server.Handler().ServeHTTP(recorder, req)
	if recorder.Body.Len() != 0 {
		t.Fatalf("cancelled request wrote a body: %s", recorder.Body.String())
	}
}

func TestMCPCreateCannotOverwriteAnExistingConnectionFromAnotherWindow(t *testing.T) {
	h := newHarness(t)
	input := map[string]any{"name": "Original", "transport": "stdio", "command": "original", "env": map[string]string{"TOKEN": "original-secret"}}
	h.json(http.MethodPut, "/api/mcp/shared", input, http.StatusOK)
	input["command"] = "replacement"
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/mcp/shared", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-None-Match", "*")
	recorder := httptest.NewRecorder()
	h.app.Server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusPreconditionFailed {
		t.Fatalf("status=%d %s", recorder.Code, recorder.Body.String())
	}
	stored := h.app.Config.Snapshot().MCP.Servers[0]
	if stored.Command != "original" || stored.Env["TOKEN"] != "original-secret" {
		t.Fatal("conditional create overwrote an existing connection")
	}
	req = httptest.NewRequest(http.MethodPut, "/api/mcp/new", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-None-Match", "*")
	recorder = httptest.NewRecorder()
	h.app.Server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMCPJSONImportMergesTheDocumentAndKeepsPrivateValues(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/mcp/kept", map[string]any{
		"name": "Kept", "transport": "stdio", "command": "original", "enabled": false,
		"env": map[string]string{"TOKEN": "secret-value"},
	}, http.StatusOK)
	document := map[string]any{"mcpServers": map[string]any{
		"Kept": map[string]any{"command": "next", "args": []string{"--new"}},
		"added": map[string]any{
			"command": "tool --flag",
			"env":     map[string]string{"ADDED": "secret-value"},
		},
	}}
	resp := h.do(http.MethodPost, "/api/mcp/import", document)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Contains(string(raw), "secret-value") || !strings.Contains(string(raw), "ADDED") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	servers := h.app.Config.Snapshot().MCP.Servers
	if len(servers) != 2 || servers[0].ID != "kept" || servers[0].Command != "next" || servers[0].Env["TOKEN"] != "secret-value" || servers[0].Enabled {
		t.Fatalf("kept server: %+v", servers)
	}
	if servers[1].ID != "added" || servers[1].Command != "tool" || len(servers[1].Args) != 1 || servers[1].Args[0] != "--flag" || servers[1].Env["ADDED"] != "secret-value" {
		t.Fatalf("added server: %+v", servers[1])
	}
	cleared := map[string]any{"mcpServers": map[string]any{"added": map[string]any{"command": "tool", "args": []string{"--flag"}, "env": map[string]string{}}}}
	h.json(http.MethodPost, "/api/mcp/import", cleared, http.StatusOK)
	if len(h.app.Config.Snapshot().MCP.Servers[1].Env) != 0 || h.app.Config.Snapshot().MCP.Servers[0].Env["TOKEN"] != "secret-value" {
		t.Fatal("clearing one server changed another credential")
	}
}

func TestMCPJSONImportRejectsTheDocumentWithoutPublishingIt(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/mcp/kept", map[string]any{"name": "Kept", "transport": "stdio", "command": "original", "env": map[string]string{"TOKEN": "secret-value"}}, http.StatusOK)
	rejected := [][]byte{
		[]byte(`{"mcpServers":{"bad":{"url":"http://user:secret-value@example.invalid/mcp"}}}`),
		[]byte(`{"TOKEN":"secret-value"`),
	}
	for _, body := range rejected {
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		h.app.Server.Handler().ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "secret-value") {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/import", bytes.NewReader(bytes.Repeat([]byte("a"), 1<<20+1)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.app.Server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), strings.Repeat("a", 32)) {
		t.Fatalf("oversized status=%d", recorder.Code)
	}
	if len(h.app.Config.Snapshot().MCP.Servers) != 1 || h.app.Config.Snapshot().MCP.Servers[0].Command != "original" || h.app.Config.Snapshot().MCP.Servers[0].Env["TOKEN"] != "secret-value" {
		t.Fatal("rejected import published")
	}
	if err := os.Mkdir(filepath.Join(h.app.Config.DataDir(), "config.yaml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/mcp/import", map[string]any{"mcpServers": map[string]any{"added": map[string]any{"command": "tool"}}}, http.StatusInternalServerError)
	if len(h.app.Config.Snapshot().MCP.Servers) != 1 || h.app.Config.Snapshot().MCP.Servers[0].Command != "original" {
		t.Fatal("failed save published")
	}
}

func TestMCPRejectsInvalidServersBeforeSaving(t *testing.T) {
	h := newHarness(t)
	for _, input := range []map[string]any{
		{"transport": "stdio"},
		{"transport": "unknown", "command": "unused"},
		{"transport": "streamable_http", "url": "file:///private"},
		{"transport": "sse", "url": "http://name:secret@example.invalid/mcp"},
		{"transport": "stdio", "command": "unused", "timeout_seconds": -1},
	} {
		h.json(http.MethodPut, "/api/mcp/test", input, http.StatusBadRequest)
	}
	h.json(http.MethodPost, "/api/mcp/missing/inspect", nil, http.StatusNotFound)
	h.json(http.MethodPut, "/api/mcp/bad.id", map[string]any{"transport": "stdio", "command": "unused"}, http.StatusBadRequest)
}

func TestMCPToolSwitchSurvivesAServerEditThatOmitsIt(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/mcp/desk", map[string]any{
		"name": "Desk", "transport": "stdio", "command": "tool", "args": []string{},
		"enabled": true, "disabled_tools": []string{"lookup", "lookup"},
	}, http.StatusOK)
	stored := h.app.Config.Snapshot().MCP.Servers[0].DisabledTools
	if len(stored) != 1 || stored[0] != "lookup" {
		t.Fatalf("stored switches: %v", stored)
	}
	h.json(http.MethodPut, "/api/mcp/desk", map[string]any{
		"name": "Renamed", "transport": "stdio", "command": "tool", "args": []string{}, "enabled": true,
	}, http.StatusOK)
	server := h.app.Config.Snapshot().MCP.Servers[0]
	if server.Name != "Renamed" || len(server.DisabledTools) != 1 || server.DisabledTools[0] != "lookup" {
		t.Fatalf("server edit cleared the tool switch: %+v", server)
	}
	body := h.json(http.MethodGet, "/api/mcp", nil, http.StatusOK)
	raw, err := json.Marshal(body)
	if err != nil || !strings.Contains(string(raw), `"disabled_tools":["lookup"]`) || strings.Contains(string(raw), `"enabled":false`) {
		t.Fatalf("catalog: %s %v", raw, err)
	}
	h.json(http.MethodPut, "/api/mcp/desk", map[string]any{
		"name": "Renamed", "transport": "stdio", "command": "tool", "args": []string{},
		"enabled": true, "disabled_tools": []string{},
	}, http.StatusOK)
	if len(h.app.Config.Snapshot().MCP.Servers[0].DisabledTools) != 0 {
		t.Fatal("explicit clear ignored")
	}
	h.json(http.MethodPut, "/api/mcp/desk", map[string]any{
		"name": "Renamed", "transport": "stdio", "command": "tool", "enabled": true,
		"disabled_tools": []string{""},
	}, http.StatusBadRequest)
	if len(h.app.Config.Snapshot().MCP.Servers[0].DisabledTools) != 0 {
		t.Fatal("rejected switch published")
	}
}
