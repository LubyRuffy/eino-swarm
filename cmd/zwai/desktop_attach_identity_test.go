package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// Port reuse does not mean that the original lease holder survived. The new
// writer still needs the launcher's complete identity and presence handshake.
func TestDesktopAttachRecognizesAReplacementAtTheSameURL(t *testing.T) {
	previous := engineBootWait
	engineBootWait = time.Second
	defer func() { engineBootWait = previous }()
	dir := t.TempDir()
	old := startLeaseHolder(t, dir)
	self := lease.ThisBuild(version)
	var posts atomic.Int32
	var replacementPID atomic.Int32
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/meta" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": version})
			return
		}
		if r.Method == http.MethodPost {
			if posts.Add(1) == 1 {
				_ = old.Process.Kill()
				until := time.Now().Add(time.Second)
				for lease.Busy(dir) && time.Now().Before(until) {
					time.Sleep(time.Millisecond)
				}
				replacement := startLeaseHolder(t, dir)
				replacementPID.Store(int32(replacement.Process.Pid))
				if err := lease.Publish(dir, lease.Record{PID: replacement.Process.Pid, URL: base, Mock: true,
					Version: self.Version, Dev: self.Dev, Ino: self.Ino}); err != nil {
					t.Error(err)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"engine_stopping"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"pc_0123456789abcdef"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": held\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	base = srv.URL
	if err := lease.Publish(dir, lease.Record{PID: old.Process.Pid, URL: base, Mock: true,
		Version: self.Version, Dev: self.Dev, Ino: self.Ino}); err != nil {
		t.Fatal(err)
	}
	got, held, err := ensureDesktopEngine(dir, true)
	if held != nil {
		defer held.release()
	}
	if err != nil || got != base || held == nil || posts.Load() != 2 {
		t.Fatalf("replacement at the same URL did not receive a new handshake: URL=%s hold=%v err=%v POSTs=%d", got, held != nil, err, posts.Load())
	}
	rec, err := lease.Discover(dir, true)
	if err != nil || rec.PID != int(replacementPID.Load()) || !lease.SameEngine(rec, self) || !lease.Busy(dir) {
		t.Fatalf("replacement identity or lease was lost: record=%+v err=%v", rec, err)
	}
}
