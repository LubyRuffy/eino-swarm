package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/terminal"
	"github.com/gorilla/websocket"
)

func TestServeTerminalNilPTYStillSendsReady(t *testing.T) {
	session := terminal.Attach(nil, "/cwd", "/bin/sh")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := terminalUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serveTerminal(ws, session)
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+trimHTTP(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ready := readJSON(t, conn)
	if ready["type"] != "ready" || ready["cwd"] != "/cwd" || ready["shell"] != "/bin/sh" {
		t.Fatalf("ready=%v", ready)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":40,"rows":12}`)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTerminalIs429WhenTheHubIsFull(t *testing.T) {
	s, e := testTerminalServer(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for s.terminals.Acquire() {
	}
	req := httptest.NewRequest(http.MethodGet, "/api/threads/"+th.ID+"/terminal", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.Bytes())
	}
}

func TestOpenTerminalReleasesTheSlotWhenUpgradeFails(t *testing.T) {
	s, e := testTerminalServer(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/threads/"+th.ID+"/terminal", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if s.terminals.Count() != 0 {
		t.Fatalf("a failed upgrade leaked a slot: %d", s.terminals.Count())
	}
}

func testTerminalServer(t *testing.T) (*Server, *engine.Engine) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := engine.New(cfg, st, provider.NewMock(cfg), log)
	t.Cleanup(e.Shutdown)
	s, err := New(Options{Engine: e, Logger: log, Mode: ModeWeb})
	if err != nil {
		t.Fatal(err)
	}
	return s, e
}

func trimHTTP(u string) string {
	if len(u) >= 4 && u[:4] == "http" {
		return u[4:]
	}
	return u
}

func readJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json %s: %v", data, err)
	}
	return out
}
