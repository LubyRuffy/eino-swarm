package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

func TestChatHTTPClientUsesAnIndependentFirstByteBudget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first time.Duration
		want  time.Duration
	}{
		{"default", 0, config.DefaultFirstByteTimeout},
		{"negative", -time.Second, config.DefaultFirstByteTimeout},
		{"longer than idle", 120 * time.Second, 120 * time.Second},
		{"short", 10 * time.Millisecond, 10 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := chatHTTPClient(time.Second, tc.first).Transport.(*idleTransport)
			if got := tr.base.(*http.Transport).ResponseHeaderTimeout; got != tc.want {
				t.Fatalf("header budget=%v want %v", got, tc.want)
			}
			if tr.idle != time.Second {
				t.Fatalf("idle=%v", tr.idle)
			}
		})
	}
}

// The real Eino adapter must receive the configured budget, including when a
// provider takes longer to begin than its later per-chunk idle allowance.
func TestOpenAIAdapterAllowsConfiguredFirstByteWaitLongerThanIdle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"r","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}))
	t.Cleanup(srv.Close)
	m, err := buildOpenAI(context.Background(), config.Provider{ID: "test", BaseURL: srv.URL + "/v1", Model: "test", TimeoutSeconds: 1, FirstByteTimeoutSeconds: 3})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("test")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var content string
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content += msg.Content
	}
	if content != "ok" {
		t.Fatalf("content=%q", content)
	}
}

func TestFirstByteTimeoutKeepsTheActualBudgetAndOriginalCause(t *testing.T) {
	first := 30 * time.Millisecond
	// Handler scheduling may lag behind both client deadlines. It must not be
	// used as the observation boundary for a bounded transport reconnect.
	allowHandler := make(chan struct{})
	defer close(allowHandler)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-allowHandler
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	var attempts atomic.Int32
	client := chatHTTPClient(time.Second, first)
	transport := client.Transport.(*idleTransport)
	base := transport.base
	transport.base = backoffRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return base.RoundTrip(r)
	})
	_, err := client.Post(srv.URL, "text/plain", strings.NewReader("body"))
	var timeout FirstByteTimeoutError
	if !errors.As(err, &timeout) || timeout.Wait != first {
		t.Fatalf("timeout must retain the configured budget: %v", err)
	}
	if timeout.Err == nil || !errors.Is(timeout, timeout.Err) || !headerWaitTimedOut(err) {
		t.Fatalf("original transport timeout lost: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d; reconnect must stay bounded", attempts.Load())
	}
}

func TestFirstByteTimeoutCanBeCancelledWithoutAReconnect(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = chatHTTPClient(time.Second, time.Second).Do(req)
	var timeout FirstByteTimeoutError
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) {
		t.Fatalf("err=%v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
}

func TestOpenAIAdapterRetainsFirstByteTimeoutBudget(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	m, err := buildOpenAI(context.Background(), config.Provider{
		ID: "test", BaseURL: srv.URL + "/v1", Model: "test",
		TimeoutSeconds: 3, FirstByteTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Stream(context.Background(), []*schema.Message{schema.UserMessage("test")})
	var timeout FirstByteTimeoutError
	if !errors.As(err, &timeout) || timeout.Wait != time.Second {
		t.Fatalf("model adapter lost first-byte budget: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("adapter made %d attempts", attempts.Load())
	}
}
