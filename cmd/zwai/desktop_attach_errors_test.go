package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// Discovery can win just before a holder commits idle exit. Exercise the
// acknowledged stopping response and a connection that closes before it can
// return that response. Neither permits a second writer before lease release.
func TestDesktopAttachRetriesOnlyAfterTheOldHolderDeparts(t *testing.T) {
	for _, stage := range []string{"post stopping", "get stopping", "post disconnect", "get disconnect"} {
		t.Run(stage, func(t *testing.T) {
			t.Cleanup(stopTestEngine)
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			var failed atomic.Bool
			old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/meta" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": version})
					return
				}
				if strings.HasPrefix(stage, "get") && r.Method == http.MethodPost {
					_, _ = io.WriteString(w, `{"id":"pc_0123456789abcdef"}`)
					return
				}
				failed.Store(true)
				_ = holder.Process.Kill()
				deadline := time.Now().Add(2 * time.Second)
				for lease.Busy(dir) && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if strings.HasSuffix(stage, "disconnect") {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"code":"engine_stopping"}`)
			}))
			defer old.Close()
			self := lease.ThisBuild(version)
			if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: old.URL, Mock: true, Version: self.Version, Dev: self.Dev, Ino: self.Ino}); err != nil {
				t.Fatal(err)
			}
			base, held, err := ensureDesktopEngine(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			defer held.release()
			if !failed.Load() || base == old.URL || len(held.id) != 19 {
				t.Fatalf("did not attach to the replacement: failed=%v base=%s id=%s", failed.Load(), base, held.id)
			}
			rec, err := lease.Discover(dir, true)
			if err != nil || rec.URL != base || !lease.SameEngine(rec, self) {
				t.Fatalf("replacement identity: %+v %v", rec, err)
			}
		})
	}
}

func TestDesktopAttachKeepsAnUnhealthyOrMismatchedHolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		mock   bool
		want   error
	}{
		{"stopping but lease held", 503, `{"code":"engine_stopping"}`, true, server.ErrEngineStopping},
		{"forbidden", 403, `{"code":"engine_stopping"}`, true, nil},
		{"wrong failure code", 503, `{"code":"different"}`, true, nil},
		{"mock mismatch", 200, `{}`, false, lease.ErrMockMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := engineBootWait
			engineBootWait = 40 * time.Millisecond
			defer func() { engineBootWait = previous }()
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/meta" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": tc.mock, "version": version})
					return
				}
				requests.Add(1)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			self := lease.ThisBuild(version)
			if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: true, Version: self.Version, Dev: self.Dev, Ino: self.Ino}); err != nil {
				t.Fatal(err)
			}
			_, held, err := ensureDesktopEngine(dir, true)
			if err == nil || held != nil || (tc.want != nil && !errors.Is(err, tc.want)) || !lease.Busy(dir) {
				t.Fatalf("unhealthy holder must remain: err=%v release=%v busy=%v", err, held != nil, lease.Busy(dir))
			}
			wantRequests := int32(1)
			if tc.want == lease.ErrMockMismatch {
				wantRequests = 0
			}
			if requests.Load() != wantRequests {
				t.Fatalf("unexpected repeated handshake: %d", requests.Load())
			}
		})
	}
}

func TestDesktopPresenceHandshakeRejectsErrorsAndMalformedReservations(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"id":"pc_wrong"}`, `{"id":"pc_0123456789abcdeg"}`, `{"id":"pc_0123456789ABCDEF"}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			if held, err := holdDesktopPresence(srv.URL); err == nil || held != nil {
				t.Fatalf("invalid reservation became a hold: %s %v", body, err)
			}
		})
	}
	if _, err := reserveDesktopPresence(context.Background(), ":invalid"); err == nil {
		t.Fatal("invalid URL accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reserveDesktopPresence(cancelled, "http://127.0.0.1:1"); err == nil {
		t.Fatal("cancelled reserve was accepted")
	}
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{503, `{"code":"engine_stopping"}`, server.ErrEngineStopping},
		{403, `{"code":"engine_stopping"}`, nil},
		{503, `{`, nil},
	} {
		err := checkDesktopPresenceResponse(&http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))})
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Fatalf("failure category lost: %d %v", tc.status, err)
		}
	}
	badDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(badDir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ensureDesktopEngine(badDir, true); err == nil {
		t.Fatal("file data dir accepted")
	}
}

func TestDesktopHoldRequiresAcknowledgementAndHasNoStreamingTimeout(t *testing.T) {
	previous := engineBootWait
	engineBootWait = 30 * time.Millisecond
	defer func() { engineBootWait = previous }()
	for _, mode := range []string{"timeout", "wrong content", "stream"} {
		t.Run(mode, func(t *testing.T) {
			dropped := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = io.WriteString(w, `{"id":"pc_0123456789abcdef"}`)
					return
				}
				if mode == "wrong content" {
					_, _ = io.WriteString(w, `{}`)
					return
				}
				if mode == "stream" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, ": held\n\n")
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(dropped)
			}))
			defer srv.Close()
			held, err := holdDesktopPresence(srv.URL)
			if mode != "stream" {
				if err == nil || held != nil {
					t.Fatal("non-SSE/timeout was acknowledged")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-dropped:
				t.Fatal("acknowledged hold timed out before frontend handoff")
			case <-time.After(3 * engineBootWait):
			}
			held.release()
			held.release()
			select {
			case <-dropped:
			case <-time.After(time.Second):
				t.Fatal("cancel did not close presence")
			}
		})
	}
}
