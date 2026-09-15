// webui.go — HTTP server, SSE hub, desktop wrapper.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm"
)

//go:embed all:app/dist
var distFS embed.FS

// distRoot serves the built React app (Vite output).
func distRoot() http.Handler {
	sub, err := fs.Sub(distFS, "app/dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			// SPA fallback: unknown paths serve index.html
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

// notificationJSON is the SSE wire format.
type notificationJSON struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
	Text    string `json:"text"`
	Err     string `json:"err,omitempty"`
	Time    string `json:"time"`
}

type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub() *hub { return &hub{clients: map[chan []byte]struct{}{}} }

func (h *hub) add(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) broadcast(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default: // slow client: drop rather than block the swarm
		}
	}
}

// pump runs one swarm task and forwards every notification to the hub.
func pump(ctx context.Context, reg *swarm.Registry, h *hub, task string) {
	start := time.Now()
	_, _ = reg.Run(ctx, task, func(n swarm.Notification) {
		b, _ := json.Marshal(notificationJSON{
			Kind: n.Kind.String(), AgentID: n.AgentID, Role: n.Role,
			Text: n.Text, Err: errStr(n.Err),
			Time: time.Since(start).Round(10 * time.Millisecond).String(),
		})
		h.broadcast(b)
	})
}

func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func splitPort(a net.Addr) string {
	s := a.String()
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[i:]
		}
	}
	return ""
}

// Serve runs the webui until ctx is canceled.
func Serve(ctx context.Context, reg *swarm.Registry, addr, task string, openBrowser bool) {
	h := newHub()
	mux := serveMux(h, reg, ctx)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	url := fmt.Sprintf("http://localhost%s", splitPort(ln.Addr()))
	fmt.Fprintf(os.Stderr, "zwai webui at %s (Ctrl+C to stop)\n", url)
	if openBrowser {
		openBrowserAt(url)
	}
	if task != "" {
		go pump(ctx, reg, h, task)
	}
	srv := &http.Server{Handler: mux}
	go func() { <-ctx.Done(); srv.Close() }()
	_ = srv.Serve(ln)
}

// Desktop wraps the webui in a native app window (Chrome --app; zero WebView
// C dependencies). The server blocks on ctx (Ctrl+C); Chrome reuses an
// existing browser process for --app, so waiting on the launcher process
// would exit early and kill the server (ERR_CONNECTION_REFUSED).
func Desktop(ctx context.Context, reg *swarm.Registry, task string) {
	h := newHub()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	url := fmt.Sprintf("http://localhost%s", splitPort(ln.Addr()))
	mux := serveMux(h, reg, ctx)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	fmt.Fprintf(os.Stderr, "zwai desktop at %s (Ctrl+C to stop)\n", url)
	if task != "" {
		go func() {
			time.Sleep(500 * time.Millisecond) // let the page connect first
			b, _ := json.Marshal(struct{ Task string }{task})
			_ = postTask(url, b)
		}()
	}
	openAppWindow(url) // open the app window (returns immediately on some setups)
	<-ctx.Done()       // keep the server alive until Ctrl+C / cancel
	_ = srv.Close()
}

func serveMux(h *hub, reg *swarm.Registry, ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", distRoot())
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch := make(chan []byte, 256)
		h.add(ch)
		defer h.remove(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case b, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Task string `json:"task"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		go pump(ctx, reg, h, req.Task)
		w.WriteHeader(204)
	})
	return mux
}

func postTask(url string, body []byte) error {
	resp, err := http.Post(url+"/run", "application/json",
		bytesReader(body))
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func openBrowserAt(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}

// openAppWindow opens url in an OS-native app window and waits for it to
// close (Chrome --app on macOS; browser fallback elsewhere).
func openAppWindow(url string) {
	switch runtime.GOOS {
	case "darwin":
		for _, chrome := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			if fileExists(chrome) {
				cmd := exec.Command(chrome, "--app="+url, "--window-size=1280,860")
				_ = cmd.Start()
				_ = cmd.Wait() // window close = app exit
				return
			}
		}
		_ = exec.Command("open", "-W", url).Start() // -W waits
	case "windows":
		_ = exec.Command("cmd", "/c", "start", "/wait", url).Run()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}

// DesktopProbe starts the desktop server and prints the URL (for testing).
func DesktopProbe(ctx context.Context, reg *swarm.Registry) string {
	h := newHub()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	mux := serveMux(h, reg, ctx)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return fmt.Sprintf("http://localhost%s", splitPort(ln.Addr()))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
