//go:build !windows

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/terminal"
	"github.com/gorilla/websocket"
)

func TestServeTerminalCopiesBytesBothWays(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fds[0]), "master")
	slave := os.NewFile(uintptr(fds[1]), "slave")
	t.Cleanup(func() {
		_ = slave.Close()
	})
	session := terminal.Attach(master, "/tmp/project", "/bin/zsh")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := terminalUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serveTerminal(ws, session)
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("ready mt=%d", mt)
	}
	var ready map[string]any
	if err := json.Unmarshal(data, &ready); err != nil {
		t.Fatal(err)
	}
	if ready["type"] != "ready" || ready["cwd"] != "/tmp/project" || ready["shell"] != "/bin/zsh" {
		t.Fatalf("ready=%v", ready)
	}

	if _, err := slave.Write([]byte("from-pty")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("pty→ws: %v", err)
	}
	if mt != websocket.BinaryMessage || string(data) != "from-pty" {
		t.Fatalf("pty→ws mt=%d data=%q", mt, data)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("from-ws")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	_ = slave.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := slave.Read(buf)
	if err != nil {
		t.Fatalf("ws→pty: %v", err)
	}
	if string(buf[:n]) != "from-ws" {
		t.Fatalf("ws→pty %q", buf[:n])
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":40,"rows":12}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
