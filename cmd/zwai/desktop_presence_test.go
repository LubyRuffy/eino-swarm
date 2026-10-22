package main

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

	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

func TestNativePresenceShortAcknowledgedStreamsExhaustRecovery(t *testing.T) {
	var posts, gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprintf(w, `{"id":"pc_%016x"}`, posts.Add(1))
			return
		}
		gets.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": held\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()
	held, err := holdDesktopPresence(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	select {
	case err := <-held.fatal:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("lost stream cause: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("short acknowledgements reset the recovery budget forever")
	}
	if posts.Load() != 4 || gets.Load() != 4 {
		t.Fatalf("unbounded handshakes: POST=%d GET=%d", posts.Load(), gets.Load())
	}
}

func TestStableNativePresenceRenewsTheNextRecoveryBudget(t *testing.T) {
	opened := make(chan chan struct{}, 8)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprintf(w, `{"id":"pc_%016x"}`, posts.Add(1))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": held\n\n")
		w.(http.Flusher).Flush()
		drop := make(chan struct{})
		opened <- drop
		select {
		case <-drop:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	held, err := holdDesktopPresence(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	next := func() chan struct{} {
		t.Helper()
		select {
		case d := <-opened:
			return d
		case err := <-held.fatal:
			t.Fatalf("premature fatal: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("no recovered stream")
		}
		return nil
	}
	close(next())
	close(next())
	stable := next()
	// After a genuinely stable connection, later faults have their own bound.
	time.Sleep(lease.IdleGrace + 20*time.Millisecond)
	close(stable)
	close(next())
	next()
	if posts.Load() != 5 {
		t.Fatalf("independent recovery lost its budget: %d", posts.Load())
	}
}

func TestNativePresenceRecoveryFailurePreservesStoppingCause(t *testing.T) {
	drop := make(chan struct{})
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if posts.Add(1) == 1 {
				io.WriteString(w, `{"id":"pc_0123456789abcdef"}`)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"code":"engine_stopping"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": held\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-drop:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	held, err := holdDesktopPresence(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	close(drop)
	select {
	case err := <-held.fatal:
		if !errors.Is(err, server.ErrEngineStopping) {
			t.Fatalf("lost stopping cause: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed recovery never became fatal")
	}
	if posts.Load() != 4 {
		t.Fatalf("retries=%d", posts.Load()-1)
	}
}

func TestClosingNativePresenceInterruptsAnInFlightRecovery(t *testing.T) {
	drop := make(chan struct{})
	retry := make(chan struct{})
	cancelled := make(chan struct{})
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if posts.Add(1) == 1 {
				io.WriteString(w, `{"id":"pc_0123456789abcdef"}`)
				return
			}
			io.Copy(io.Discard, r.Body)
			close(retry)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": held\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-drop:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	held, err := holdDesktopPresence(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	close(drop)
	select {
	case <-retry:
	case <-time.After(time.Second):
		t.Fatal("no recovery request")
	}
	held.release()
	held.release()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("release did not cancel recovery HTTP")
	}
	select {
	case err, ok := <-held.fatal:
		if ok || err != nil {
			t.Fatalf("normal shutdown became fatal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not leave")
	}
}

func TestClosingNativePresenceInterruptsRecoveryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fatal := make(chan error, 1)
	closed := make(chan struct{})
	go maintainDesktopPresence(ctx, "http://127.0.0.1:1", &desktopPresenceStream{
		body: io.NopCloser(strings.NewReader("")), started: time.Now(), close: func() { close(closed) },
	}, fatal)
	<-closed
	cancel()
	select {
	case err, ok := <-fatal:
		if ok || err != nil {
			t.Fatalf("cancelled backoff became fatal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("backoff ignored cancellation")
	}
}
