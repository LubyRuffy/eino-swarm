package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/wakeup"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

// A hub can accept TCP while never answering registration. Local lifecycle
// operations must remain available so the window can open or quit anyway.
func TestHostPendingRegistrationDoesNotBlockStatusOrStop(t *testing.T) {
	for _, operation := range []string{"status", "stop"} {
		t.Run(operation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(unblock)
			e := testEngine(t)
			e.Config().Remote.Enabled = true
			e.Config().Remote.HubURL = srv.URL
			h := New(e, e.Config(), nil)
			started := make(chan struct{})
			go func() { h.Start(); close(started) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("registration never reached the controlled hub")
			}
			done := make(chan struct{})
			go func() {
				if operation == "status" {
					h.Status()
				} else {
					h.Stop()
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Errorf("%s waited on the hub registration", operation)
			}
			unblock()
			<-started
			<-done
			h.Stop()
		})
	}
}

func TestBackgroundStartSharesAttemptAndKeepsTheConnection(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var registrations atomic.Int32
	handler := hub.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/pairlink/v1/hosts" {
			if registrations.Add(1) == 1 {
				close(entered)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled, cfg.Remote.HubURL = true, srv.URL
	token, err := relay.IssueHostToken(t.Context(), hubStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	t.Cleanup(h.Stop)
	rec := wakeup.NewRecorder()
	h.wake = rec
	h.StartBackground()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("background connection did not reach registration")
	}
	h.StartBackground()
	if st := h.Status(); st.Online || !st.Awake || st.Error != "" {
		t.Fatalf("pending connection status %+v", st)
	}
	started := make(chan struct{})
	go func() { h.Start(); close(started) }()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := h.Offer(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled offer must stop waiting for the shared attempt: %v", err)
	}
	select {
	case <-started:
		t.Fatal("synchronous Start returned before the shared attempt finished")
	default:
	}
	unblock()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("synchronous Start did not observe background success")
	}
	if got := registrations.Load(); got != 1 {
		t.Fatalf("concurrent starts made %d registrations", got)
	}
	if st := h.Status(); !st.Online || st.Error != "" || st.Fingerprint == "" {
		t.Fatalf("connection must survive completion of its initial dial context: %+v", st)
	}
	if offer, err := h.Offer(t.Context()); err != nil || offer.URI == "" {
		t.Fatalf("connected host could not communicate after startup: %v", err)
	}
}

func TestStopCancelsPendingRegistrationAndSocketHandshake(t *testing.T) {
	for _, stage := range []string{"register", "socket"} {
		t.Run(stage, func(t *testing.T) {
			entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == "/pairlink/v1/hosts" && stage == "socket" {
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.URL.Path == "/pairlink/v1/info" {
					_, _ = io.WriteString(w, "{}")
					return
				}
				close(entered)
				select {
				case <-r.Context().Done():
					close(cancelled)
				case <-release:
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(unblock)
			e := testEngine(t)
			e.Config().Remote.Enabled, e.Config().Remote.HubURL = true, srv.URL
			h := New(e, e.Config(), nil)
			h.StartBackground()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("connection did not reach the held stage")
			}
			stopped := make(chan struct{})
			go func() { h.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(500 * time.Millisecond):
				unblock()
				<-stopped
				t.Fatal("Stop waited for an unresponsive remote stage")
			}
			if stage == "register" {
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Fatal("Stop did not cancel the registration request")
				}
			}
			unblock()
			if st := h.Status(); st.Online || st.Error != "" || st.Awake {
				t.Fatalf("stopped attempt changed host status: %+v", st)
			}
		})
	}
}

func TestReloadCancelsOldAttemptWithoutReleasingKeepAwake(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	handler := hub.Handler()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var registrations atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/pairlink/v1/hosts" && registrations.Add(1) == 1 {
			close(entered)
			<-release // Return after the replacement is already connected.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled, cfg.Remote.HubURL = true, srv.URL
	token, err := relay.IssueHostToken(t.Context(), hubStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	t.Cleanup(h.Stop)
	rec := wakeup.NewRecorder()
	h.wake = rec
	h.StartBackground()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("initial registration never started")
	}
	rec.Reset()
	reloaded := make(chan struct{})
	go func() { h.Reload(); close(reloaded) }()
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		unblock()
		<-reloaded
		t.Fatal("Reload waited for the old registration")
	}
	unblock()
	if st := h.Status(); !st.Online || st.Error != "" || !st.Awake {
		t.Fatalf("late old registration corrupted the replacement: %+v", st)
	}
	for _, held := range rec.Sets() {
		if !held {
			t.Fatalf("Reload released keep-awake: %v", rec.Sets())
		}
	}
}

func TestOfferCancellationBoundsItsOwnConnectionAttempt(t *testing.T) {
	for _, cancelledFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "during registration", true: "before registration"}[cancelledFirst], func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				requests.Add(1)
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)
			e := testEngine(t)
			e.Config().Remote.Enabled, e.Config().Remote.HubURL = true, srv.URL
			h := New(e, e.Config(), nil)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			if cancelledFirst {
				cancel()
			}
			if _, err := h.Offer(ctx); !errors.Is(err, ctx.Err()) {
				t.Fatalf("Offer lost its caller cancellation: %v", err)
			}
			if got := requests.Load(); cancelledFirst && got != 0 {
				t.Fatalf("cancelled Offer sent %d registration requests", got)
			}
			h.Stop()
		})
	}
}

func TestBackgroundStartReportsIdentityPreparationFailure(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled, cfg.Remote.HubURL = true, "http://127.0.0.1:1"
	if err := cfg.WriteHostToken("offline-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cfg.RemoteDir(), "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	t.Cleanup(h.Stop)
	h.StartBackground()
	if st := h.Status(); st.Online || st.Error == "" {
		t.Fatalf("identity failure must remain visible: %+v", st)
	}
}

func TestStoppedHostClosesALateSocketHandshake(t *testing.T) {
	entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	upgraded := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/pairlink/v1/hosts" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !websocket.IsWebSocketUpgrade(r) {
			_, _ = io.WriteString(w, "{}")
			return
		}
		close(entered)
		<-release
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		upgraded <- err
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(closed)
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	e := testEngine(t)
	e.Config().Remote.Enabled, e.Config().Remote.HubURL = true, srv.URL
	h := New(e, e.Config(), nil)
	h.StartBackground()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("host did not reach the held socket upgrade")
	}
	stopped := make(chan struct{})
	go func() { h.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		unblock()
		<-stopped
		t.Fatal("Stop waited on the initial socket response")
	}
	unblock()
	select {
	case err := <-upgraded:
		if err != nil {
			t.Fatalf("the held handshake did not complete: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the released socket did not finish upgrading")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("the cancelled dial did not close its late connection")
	}
	if st := h.Status(); st.Online || st.Error != "" || st.Awake {
		t.Fatalf("late connection revived a stopped host: %+v", st)
	}
}
