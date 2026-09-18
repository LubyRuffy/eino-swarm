package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalIs404ForAMissingConversation(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodGet, "/api/threads/th_missing/terminal", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/pj_missing/terminal", nil, http.StatusNotFound)
}

func TestTerminalStartsInTheProjectDirectory(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SHELL", "/bin/sh")
	repo := t.TempDir()
	p := h.newProject(map[string]any{"name": "Repo", "workdir": repo})
	th := h.json(http.MethodPost, "/api/threads", map[string]any{
		"project_id": p["id"],
	}, http.StatusCreated)["thread"].(map[string]any)
	ws := dialTerminal(t, h, "/api/threads/"+th["id"].(string)+"/terminal?cols=80&rows=24")
	defer ws.Close()
	ready := readReady(t, ws)
	if ready["cwd"] != repo {
		t.Fatalf("ready cwd=%v want %s", ready["cwd"], repo)
	}
	got := terminalPWD(t, ws, repo)
	if !strings.Contains(got, repo) {
		skipIfPTYForbidden(t, got)
		t.Fatalf("shell pwd output %q does not contain the project directory %q", got, repo)
	}
}

func TestTerminalOnAProjectWithoutAConversation(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SHELL", "/bin/sh")
	repo := t.TempDir()
	p := h.newProject(map[string]any{"name": "Repo", "workdir": repo})
	ws := dialTerminal(t, h, "/api/projects/"+p["id"].(string)+"/terminal")
	defer ws.Close()
	ready := readReady(t, ws)
	if ready["cwd"] != repo {
		t.Fatalf("ready cwd=%v want %s", ready["cwd"], repo)
	}
}

func TestTerminalIgnoresAClientSuppliedCwd(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SHELL", "/bin/sh")
	th := h.newThread()
	ws := dialTerminal(t, h, "/api/threads/"+th+"/terminal?cwd=/etc")
	defer ws.Close()
	ready := readReady(t, ws)
	cwd, _ := ready["cwd"].(string)
	if cwd == "/etc" || strings.Contains(cwd, "/etc") && !strings.Contains(cwd, "workspaces") {
		t.Fatalf("client cwd query must not choose the directory: %q", cwd)
	}
	if _, err := os.Stat(cwd); err != nil {
		t.Fatalf("resolved cwd %q: %v", cwd, err)
	}
}

func TestTerminalRefusesAForeignOrigin(t *testing.T) {
	h := newHarness(t)
	th := h.newThread()
	u := wsURL(h, "/api/threads/"+th+"/terminal")
	hdr := http.Header{}
	hdr.Set("Origin", "https://evil.example")
	_, resp, err := websocket.DefaultDialer.Dial(u, hdr)
	if err == nil {
		t.Fatal("a foreign Origin must not upgrade")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
}

func TestTerminalSameOriginUpgrade(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SHELL", "/bin/sh")
	th := h.newThread()
	u := wsURL(h, "/api/threads/"+th+"/terminal")
	hdr := http.Header{}
	hdr.Set("Origin", h.srv.URL)
	ws, _, err := websocket.DefaultDialer.Dial(u, hdr)
	if err != nil {
		t.Fatalf("same origin: %v", err)
	}
	defer ws.Close()
	ready := readReady(t, ws)
	if ready["type"] != "ready" {
		t.Fatalf("ready=%v", ready)
	}
}

func TestTerminalResizeIsAccepted(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SHELL", "/bin/sh")
	th := h.newThread()
	ws := dialTerminal(t, h, "/api/threads/"+th+"/terminal")
	defer ws.Close()
	_ = readReady(t, ws)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":40,"rows":12}`)); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("echo resized\n")); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, ws, "resized", 4*time.Second)
	if !strings.Contains(got, "resized") {
		skipIfPTYForbidden(t, got)
		t.Fatalf("after resize: %q", got)
	}
}

func skipIfPTYForbidden(t *testing.T, got string) {
	t.Helper()
	if strings.Contains(got, "operation not permitted") {
		t.Skip(got)
	}
}

func dialTerminal(t *testing.T, h *harness, path string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(h, path), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return ws
}

func wsURL(h *harness, path string) string {
	return "ws" + strings.TrimPrefix(h.srv.URL, "http") + path
}

func readReady(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	mt, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("ready mt=%d data=%q", mt, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("ready json: %s", data)
	}
	if out["type"] == "error" {
		msg := fmt.Sprint(out["error"])
		if strings.Contains(msg, "operation not permitted") {
			t.Skip(msg)
		}
		t.Fatalf("ready error: %v", out)
	}
	if out["type"] != "ready" {
		t.Fatalf("first message=%v", out)
	}
	if strings.TrimSpace(fmt.Sprint(out["cwd"])) == "" {
		t.Fatalf("ready missing cwd: %v", out)
	}
	drainTerminalControl(t, ws)
	return out
}

func drainTerminalControl(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	mt, data, err := ws.ReadMessage()
	if err != nil {
		return
	}
	if mt != websocket.TextMessage {
		return
	}
	var extra map[string]any
	if json.Unmarshal(data, &extra) != nil {
		return
	}
	switch extra["type"] {
	case "ready":
		// leftover control frame from an older server that sent ready twice
	case "error":
		msg := fmt.Sprint(extra["error"])
		if strings.Contains(msg, "operation not permitted") {
			t.Skip(msg)
		}
		t.Fatalf("terminal error: %v", extra)
	}
}

func terminalPWD(t *testing.T, ws *websocket.Conn, want string) string {
	t.Helper()
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("pwd\n")); err != nil {
		t.Fatal(err)
	}
	return readUntil(t, ws, want, 4*time.Second)
}

func readUntil(t *testing.T, ws *websocket.Conn, want string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	var b strings.Builder
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		mt, data, err := ws.ReadMessage()
		if err != nil {
			// A failed Start closes the socket after the error frame.
			// Reading again panics inside gorilla; the bytes we have are
			// the whole story.
			return b.String()
		}
		if mt == websocket.BinaryMessage {
			b.Write(data)
		} else {
			b.Write(data)
		}
		if strings.Contains(b.String(), want) {
			return b.String()
		}
	}
	return b.String()
}
