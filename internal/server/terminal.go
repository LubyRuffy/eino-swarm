package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/terminal"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type terminalResize struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type terminalReady struct {
	Type  string `json:"type"`
	Cwd   string `json:"cwd"`
	Shell string `json:"shell"`
}

type terminalError struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

func (s *Server) threadTerminal(c *gin.Context) {
	s.openTerminal(c, c.Param("id"), "")
}

func (s *Server) projectTerminal(c *gin.Context) {
	s.openTerminal(c, "", c.Param("id"))
}

func (s *Server) openTerminal(c *gin.Context, threadID, projectID string) {
	dir, err := s.engine.TerminalDir(threadID, projectID)
	if err != nil {
		s.fail(c, err)
		return
	}
	if !s.terminals.Acquire() {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "too many terminals are already open",
		})
		return
	}
	cols, rows := termSize(c.Request.URL.Query())
	ws, err := terminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.terminals.Release()
		s.log.Warn("terminal upgrade", "err", err)
		return
	}
	defer s.terminals.Release()
	shell, _ := terminal.Shell()
	// Tab names the directory before fork. A sandbox that refuses PTY
	// still gets this frame, then error — not "Terminal 1".
	_ = writeTerminalReady(ws, dir, shell)
	session, err := terminal.Start(terminal.Options{Dir: dir, Cols: cols, Rows: rows})
	if err != nil {
		_ = ws.WriteJSON(terminalError{Type: "error", Error: err.Error()})
		_ = ws.Close()
		return
	}
	copyTerminal(ws, session)
}

func writeTerminalReady(ws *websocket.Conn, cwd, shell string) error {
	return ws.WriteJSON(terminalReady{Type: "ready", Cwd: cwd, Shell: shell})
}

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     sameOrigin,
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// curl and tests. A remote socket with no Origin is a raw TTY.
		return remoteIsLoopback(r)
	}
	return originMatchesLoopbackHost(r)
}

func originMatchesLoopbackHost(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if !isLoopbackHostname(u.Hostname()) {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func isLoopbackHostname(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func remoteIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func termSize(q url.Values) (int, int) {
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	if cols == 0 {
		cols = terminal.DefaultCols
	}
	if rows == 0 {
		rows = terminal.DefaultRows
	}
	return terminal.ClampSize(cols, rows)
}

func serveTerminal(ws *websocket.Conn, session *terminal.Session) {
	if session != nil {
		_ = writeTerminalReady(ws, session.Cwd, session.Shell)
	}
	copyTerminal(ws, session)
}

func copyTerminal(ws *websocket.Conn, session *terminal.Session) {
	defer session.Close()
	defer ws.Close()

	var writeMu sync.Mutex
	write := func(mt int, p []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return ws.WriteMessage(mt, p)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		pty := session.PTY()
		if pty == nil {
			return
		}
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				if werr := write(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = ws.Close()
				return
			}
		}
	}()

	ws.SetReadLimit(1 << 20)
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			if pty := session.PTY(); pty != nil && len(data) > 0 {
				_, _ = pty.Write(data)
			}
		case websocket.TextMessage:
			_ = applyTerminalControl(session, data)
		}
	}
	// Closing the PTY unblocks the reader. Waiting first deadlocks: the
	// reader is blocked on Read until the kernel sees the close.
	_ = session.Close()
	<-done
}

func applyTerminalControl(session *terminal.Session, data []byte) error {
	var msg terminalResize
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}
	if !strings.EqualFold(msg.Type, "resize") {
		return nil
	}
	return session.Resize(msg.Cols, msg.Rows)
}
