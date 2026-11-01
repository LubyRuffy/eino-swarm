package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudWebFailuresNeverExecuteLocallyOrExposeServerSecrets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "old account requires new login", status: 401, body: `{"error":{"code":"invalid_api_key","message":"private-server-message"}}`, want: "sign in to the server again"},
		{name: "revoked grant", status: 403, body: `{"error":{"code":"permission_denied","message":"private-server-message"}}`, want: "HTTP 403, permission_denied"},
		{name: "rate limit", status: 429, body: `{"error":{"code":"rate_limit_exceeded"}}`, want: "HTTP 429, rate_limit_exceeded"},
		{name: "logging failure", status: 503, body: `{"error":{"code":"logging_unavailable"}}`, want: "HTTP 503, logging_unavailable"},
		{name: "invalid error code", status: 500, body: `{"error":{"code":"private-server-message"}}`, want: "request_failed"},
		{name: "error echoes host token", status: 500, body: `{"error":{"code":"` + strings.Repeat("a", 64) + `"}}`, want: "request_failed"},
		{name: "unsuccessful envelope", status: 200, body: `{"success":false,"error":{"code":"web_search_unavailable"}}`, want: "web_search_unavailable"},
		{name: "invalid JSON", status: 200, body: `private-server-message`, want: "request_failed"},
		{name: "missing data", status: 200, body: `{"success":true}`, want: "invalid server response"},
		{name: "null data", status: 200, body: `{"success":true,"data":null}`, want: "invalid server response"},
		{name: "missing Markdown", status: 200, body: `{"success":true,"data":{}}`, want: "invalid page response"},
		{name: "invalid Markdown", status: 200, body: `{"success":true,"data":{"markdown":42}}`, want: "invalid page response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cloud, local atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/client/desktop/v1/web/fetch" {
					local.Add(1)
					return
				}
				cloud.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			cfg := configFor(t)
			token := signInWebAccount(t, cfg, server.URL)
			args, err := json.Marshal(map[string]string{"url": server.URL + "/local-page"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := builtCloudTool(t, cfg, "web_fetch").InvokableRun(context.Background(), string(args))
			if err != nil || !strings.Contains(out, tc.want) || strings.Contains(out, token) || strings.Contains(out, "private-server-message") || local.Load() != 0 || cloud.Load() != 1 {
				t.Fatalf("cloud failure lost its boundary: %q error=%v cloud=%d local=%d", out, err, cloud.Load(), local.Load())
			}
		})
	}
}

func TestCloudWebRedirectCannotForwardTheHostToken(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	cfg := configFor(t)
	token := signInWebAccount(t, cfg, server.URL)
	out, err := builtCloudTool(t, cfg, "web_fetch").InvokableRun(context.Background(), `{"url":"https://page.invalid"}`)
	if err != nil || !strings.Contains(out, "HTTP 307") || strings.Contains(out, token) || redirected.Load() != 0 {
		t.Fatalf("cloud redirect leaked authority: %q, %v, calls=%d", out, err, redirected.Load())
	}
}

func TestStopCancelsTheActualCloudRequest(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
			return
		}
		close(entered)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()
	cfg := configFor(t)
	signInWebAccount(t, cfg, server.URL)
	fetch := builtCloudTool(t, cfg, "web_fetch")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetch.InvokableRun(ctx, `{"url":"https://page.invalid"}`)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cloud request never reached the server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop lost its cancellation cause: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not release the tool caller")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel the actual HTTP connection")
	}
}

func TestCloudWebResponseReadsAreBoundedAndTransportFailuresStayPrivate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		close   bool
		want    string
	}{
		{name: "oversized response", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", cloudWebResponseLimit+1)))
		}, want: "response exceeds the size limit"},
		{name: "truncated response", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("short"))
		}, want: "server response is unavailable"},
		{name: "closed server", handler: func(http.ResponseWriter, *http.Request) {}, close: true, want: "server transport is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			cfg := configFor(t)
			token := signInWebAccount(t, cfg, server.URL)
			if tc.close {
				server.Close()
			}
			out, err := builtCloudTool(t, cfg, "web_fetch").InvokableRun(context.Background(), `{"url":"https://page.invalid"}`)
			if err != nil || !strings.Contains(out, tc.want) || strings.Contains(out, token) || strings.Contains(out, server.URL) {
				t.Fatalf("response error lost its bounded private form: %q, %v", out, err)
			}
		})
	}
}

func TestStopWhileReadingTheCloudResponsePreservesCancellation(t *testing.T) {
	reading, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,`)
		w.(http.Flusher).Flush()
		close(reading)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()
	cfg := configFor(t)
	signInWebAccount(t, cfg, server.URL)
	fetch := builtCloudTool(t, cfg, "web_fetch")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetch.InvokableRun(ctx, `{"url":"https://page.invalid"}`)
		done <- err
	}()
	select {
	case <-reading:
	case <-time.After(2 * time.Second):
		t.Fatal("cloud response did not begin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("response read lost its cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not release the response reader")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("response transport did not observe cancellation")
	}
}

func TestInvalidCloudToolArgumentsNeverMakeANetworkRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	cfg := configFor(t)
	signInWebAccount(t, cfg, server.URL)
	for _, tc := range []struct{ tool, input, want string }{
		{"web_fetch", "invalid-json", "invalid tool arguments"},
		{"web_fetch", `{}`, "URL is required"},
		{"web_search", `{"query":"  "}`, "query is required"},
	} {
		t.Run(fmt.Sprint(tc.tool, "/", tc.input), func(t *testing.T) {
			out, err := builtCloudTool(t, cfg, tc.tool).InvokableRun(context.Background(), tc.input)
			if err != nil || !strings.Contains(out, tc.want) {
				t.Fatalf("invalid arguments were not explained: %q, %v", out, err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments reached the cloud")
	}
}

func TestCloudSearchValidatesItsResponseAndKeepsEmptyResults(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"success":true,"data":{"results":{}}}`, "invalid search response"},
		{`{"success":true,"data":{"results":[]}}`, `"results":[]`},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
		cfg := configFor(t)
		signInWebAccount(t, cfg, server.URL)
		out, err := builtCloudTool(t, cfg, "web_search").InvokableRun(context.Background(), `{"query":"references"}`)
		server.Close()
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatalf("unexpected search result: %q, %v", out, err)
		}
	}
}
