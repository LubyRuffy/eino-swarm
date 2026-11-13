package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type entry struct {
	gate                                    chan struct{}
	server                                  config.MCPServer
	workspace, fingerprint, key, catalogDir string
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	uses                                    int  // Manager.mu
	broken                                  bool // Manager.mu
	session                                 *mcp.ClientSession
	catalog                                 Catalog
	failed                                  bool // gate; a failed initialization is never replayed on this entry
	progressMu                              sync.Mutex
	progress                                map[string]*activeProgress
}

// Waiting for somebody else's initialization does not own its connection.
type waiterCancellation struct{ error }

func (e *waiterCancellation) Unwrap() error { return e.error }

func (e *entry) connect(caller context.Context) (*mcp.ClientSession, Catalog, error) {
	if err := caller.Err(); err != nil {
		return nil, Catalog{}, &waiterCancellation{err}
	}
	select {
	case e.gate <- struct{}{}:
		defer func() { <-e.gate }()
	case <-caller.Done():
		return nil, Catalog{}, &waiterCancellation{caller.Err()}
	case <-e.ctx.Done():
		return nil, Catalog{}, &Error{Code: "closed", ServerID: e.server.ID}
	}
	if err := caller.Err(); err != nil {
		return nil, Catalog{}, &waiterCancellation{err}
	}
	if e.ctx.Err() != nil {
		return nil, Catalog{}, &Error{Code: "closed", ServerID: e.server.ID}
	}
	if e.session != nil {
		return e.session, e.catalog, nil
	}
	if e.failed {
		return nil, Catalog{}, &Error{Code: "session_expired", ServerID: e.server.ID}
	}
	e.failed = true
	// SSE binds its long-lived GET to Connect's context. Bound only the
	// establishment phase; cancelling a successful handshake kills that stream.
	ctx, cancel := context.WithCancel(e.ctx)
	timer := time.AfterFunc(e.server.Timeout(), cancel)
	stop := context.AfterFunc(caller, cancel)
	connected := false
	defer func() {
		timer.Stop()
		stop()
		if !connected {
			cancel()
		}
	}()
	if err := e.server.Validate(); err != nil {
		return nil, Catalog{}, &Error{Code: "invalid_configuration", ServerID: e.server.ID}
	}
	var transport mcp.Transport
	if e.server.Transport == "stdio" {
		cmd := exec.Command(e.server.Command, e.server.Args...)
		cmd.Dir = e.workspace
		cmd.Env = os.Environ()
		for key, value := range e.server.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		cmd.Stderr = io.Discard
		transport = &commandTransport{command: cmd}
	} else {
		client := &http.Client{Transport: headerTransport{headers: e.server.Headers, endpoint: e.server.URL, lifetime: ctx, closeTimeout: min(e.server.Timeout(), time.Second)}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if e.server.Transport == "sse" {
			transport = &mcp.SSEClientTransport{Endpoint: e.server.URL, HTTPClient: client}
		} else {
			transport = &mcp.StreamableClientTransport{Endpoint: e.server.URL, HTTPClient: client, MaxRetries: -1}
		}
	}
	catalogDir := e.catalogDir
	fingerprint := e.fingerprint
	client := mcp.NewClient(&mcp.Implementation{Name: "zwai", Version: "1"}, &mcp.ClientOptions{
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
		ProgressNotificationHandler: e.handleProgress,
		// A live session drops the saved catalog. The current turn keeps the
		// tools it already mounted; the next turn lists again.
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			if catalogDir == "" || fingerprint == "" {
				return
			}
			_ = os.Remove(catalogPath(catalogDir, fingerprint))
		},
	})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, Catalog{}, &Error{Code: "connection_failed", ServerID: e.server.ID}
	}
	catalog := Catalog{ServerID: e.server.ID, Tools: []ToolDescriptor{}}
	seen := map[string]bool{}
	for remote, err := range session.Tools(ctx, nil) {
		if err != nil {
			_ = session.Close()
			return nil, Catalog{}, &Error{Code: "discovery_failed", ServerID: e.server.ID}
		}
		if remote == nil || remote.Name == "" || seen[remote.Name] {
			_ = session.Close()
			return nil, Catalog{}, &Error{Code: "invalid_catalog", ServerID: e.server.ID}
		}
		seen[remote.Name] = true
		d := ToolDescriptor{Name: remote.Name, OriginalName: remote.Name, Description: remote.Description, InputSchema: remote.InputSchema}
		if _, err := toolInfo(d); err != nil {
			catalog.Skipped = append(catalog.Skipped, remote.Name)
			continue
		}
		catalog.Tools = append(catalog.Tools, d)
	}
	e.session = session
	e.catalog = catalog
	e.failed = false
	connected = true
	return session, catalog, nil
}

func (e *entry) close() {
	e.cancel()
	e.gate <- struct{}{}
	defer func() { <-e.gate }()
	if e.session != nil {
		_ = e.session.Close()
		e.session = nil
	}
}

type headerTransport struct {
	headers      map[string]string
	endpoint     string
	closeTimeout time.Duration
	lifetime     context.Context
}

func (h headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A legacy SSE server supplies its POST URL. Never forward configured
	// credentials to another origin through that endpoint or an HTTP redirect.
	base, _ := http.NewRequest(http.MethodGet, h.endpoint, nil)
	if base == nil || req.URL.Scheme != base.URL.Scheme || req.URL.Host != base.URL.Host {
		return nil, fmt.Errorf("MCP endpoint changed origin")
	}
	clone := req.Clone(req.Context())
	var stop func() bool
	var cancel context.CancelFunc
	if h.lifetime != nil && req.Method != http.MethodDelete {
		var ctx context.Context
		ctx, cancel = context.WithCancel(req.Context())
		stop = context.AfterFunc(h.lifetime, cancel)
		clone = clone.WithContext(ctx)
	}
	if req.Method == http.MethodDelete && h.closeTimeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), h.closeTimeout)
		defer cancel()
		clone = clone.WithContext(ctx)
	}
	for key, value := range h.headers {
		clone.Header.Set(key, value)
	}
	response, err := http.DefaultTransport.RoundTrip(clone)
	if cancel != nil {
		if err != nil {
			stop()
			cancel()
		} else {
			response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel, stop: stop}
		}
	}
	return response, err
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	stop   func() bool
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.stop(); b.cancel(); return err }

func jsonResult(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
