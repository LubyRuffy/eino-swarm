package provider

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestChatHTTPClientHasNoTotalTimeout(t *testing.T) {
	c := chatHTTPClient(time.Second)
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout=%v; a total timeout kills a live thinking stream", c.Timeout)
	}
	tr, ok := c.Transport.(*idleTransport)
	if !ok {
		t.Fatal("the client must wrap the body with an idle reader")
	}
	if tr.idle != time.Second {
		t.Fatalf("idle=%v", tr.idle)
	}
}

func TestALongStreamIdleDoesNotExtendTheWaitForTheFirstByte(t *testing.T) {
	c := chatHTTPClient(5 * time.Minute)
	tr := c.Transport.(*idleTransport)
	base, ok := tr.base.(*http.Transport)
	if !ok {
		t.Fatal("expected http.Transport")
	}
	if base.ResponseHeaderTimeout != config.DefaultFirstByteTimeout {
		t.Fatalf("header timeout=%v want %v", base.ResponseHeaderTimeout, config.DefaultFirstByteTimeout)
	}
	if tr.idle != 5*time.Minute {
		t.Fatalf("stream idle=%v", tr.idle)
	}
	short := chatHTTPClient(80 * time.Millisecond).Transport.(*idleTransport).base.(*http.Transport)
	if short.ResponseHeaderTimeout != 80*time.Millisecond {
		t.Fatalf("a shorter idle must also cap headers: %v", short.ResponseHeaderTimeout)
	}
}

func TestChatHTTPClientZeroIdleUsesTheDefault(t *testing.T) {
	for _, idle := range []time.Duration{0, -time.Second} {
		c := chatHTTPClient(idle)
		tr, ok := c.Transport.(*idleTransport)
		if !ok {
			t.Fatal("expected idleTransport")
		}
		if tr.idle != config.DefaultRequestTimeout {
			t.Fatalf("idle=%v want %v", tr.idle, config.DefaultRequestTimeout)
		}
	}
}

func TestIdleTimeoutErrorUnwraps(t *testing.T) {
	err := IdleTimeoutError{Idle: 5 * time.Minute}
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatal("errors.Is must see through IdleTimeoutError")
	}
	wrapped := fmt.Errorf("failed to receive stream chunk: %w", err)
	if !errors.Is(wrapped, ErrIdleTimeout) {
		t.Fatal("eino wraps this; errors.Is must still match")
	}
}

// A thinking model that keeps emitting tokens must outlast the idle setting.
func TestIdleBodyAllowsALongStreamThatKeepsMoving(t *testing.T) {
	idle := 80 * time.Millisecond
	body := newIdleBody(&pacedReader{
		chunks: []string{"a", "b", "c", "d", "e"},
		pause:  idle / 2,
	}, idle)
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcde" {
		t.Fatalf("got %q", got)
	}
}

// Silence longer than the idle setting is a hung endpoint, not a long thought.
func TestIdleBodyCutsASilentStream(t *testing.T) {
	idle := 40 * time.Millisecond
	body := newIdleBody(&stallReader{}, idle)
	defer body.Close()
	_, err := io.ReadAll(body)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("err=%v", err)
	}
}

func TestIdleBodyOnIdleAfterCloseIsANoOp(t *testing.T) {
	body := newIdleBody(&stallReader{}, time.Hour)
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	body.onIdle()
	body.mu.Lock()
	timedOut := body.timedOut
	body.mu.Unlock()
	if timedOut {
		t.Fatal("a closed body must not become a timeout")
	}
}

func TestIdleBodyCloseStopsTheTimer(t *testing.T) {
	body := newIdleBody(&stallReader{}, 40*time.Millisecond)
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	body.mu.Lock()
	timedOut := body.timedOut
	body.mu.Unlock()
	if timedOut {
		t.Fatal("Close must Stop the idle timer so a finished call is not a timeout")
	}
}

func TestIdleClientCutsSlowHeaders(t *testing.T) {
	idle := 80 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(4 * idle)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	_, err := chatHTTPClient(idle).Get(srv.URL)
	if err == nil {
		t.Fatal("headers slower than the idle setting must fail")
	}
	if !strings.Contains(err.Error(), "Timeout") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want a header timeout, got %v", err)
	}
}

// The first attempt can sit on a dead HTTP/2 connection until the header
// budget is gone. The call itself never started, so the next connection
// has to be allowed to answer.
func TestIdleClientRetriesOnceAfterAHeaderTimeout(t *testing.T) {
	idle := 40 * time.Millisecond
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			time.Sleep(4 * idle)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)

	resp, err := chatHTTPClient(idle).Post(srv.URL, "text/plain", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" || n.Load() != 2 {
		t.Fatalf("body=%q attempts=%d", got, n.Load())
	}
}

func TestIdleClientAllowsALongHTTPStreamThatKeepsMoving(t *testing.T) {
	idle := 80 * time.Millisecond
	n := 6
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < n; i++ {
			time.Sleep(idle / 2)
			fmt.Fprintf(w, "data: %d\n\n", i)
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	resp, err := chatHTTPClient(idle).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), fmt.Sprintf("data: %d", n-1)) {
		t.Fatalf("body=%q", got)
	}
}

func TestIdleClientCutsASilentHTTPStream(t *testing.T) {
	idle := 60 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		fmt.Fprint(w, "data: start\n\n")
		flusher.Flush()
		time.Sleep(4 * idle)
		fmt.Fprint(w, "data: late\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	resp, err := chatHTTPClient(idle).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("err=%v body=%q", err, got)
	}
	if strings.Contains(string(got), "late") {
		t.Fatal("a chunk after the idle gap must not be delivered")
	}
}

// pacedReader emits one chunk per Read, sleeping between them so a total-time
// timeout would fire while an idle timeout must not.
type pacedReader struct {
	chunks []string
	pause  time.Duration
	i      int
}

func (p *pacedReader) Read(b []byte) (int, error) {
	if p.i >= len(p.chunks) {
		return 0, io.EOF
	}
	if p.i > 0 {
		time.Sleep(p.pause)
	}
	n := copy(b, p.chunks[p.i])
	p.i++
	return n, nil
}

func (p *pacedReader) Close() error { return nil }

// stallReader blocks until Close, which is what a silent SSE body looks like.
type stallReader struct {
	mu     sync.Mutex
	closed chan struct{}
}

func (s *stallReader) ch() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed == nil {
		s.closed = make(chan struct{})
	}
	return s.closed
}

func (s *stallReader) Read(p []byte) (int, error) {
	<-s.ch()
	return 0, io.EOF
}

func (s *stallReader) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed == nil {
		s.closed = make(chan struct{})
	}
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}
