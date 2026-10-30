package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func TestNOSECRetryRecoversNewDeviceCandidateBeforeOldActiveCredential(t *testing.T) {
	requestCh := make(chan loginStartRequest, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req loginStartRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		requestCh <- req
		http.Error(w, "stopped", 409)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = gateway.URL
	oldToken := strings.Repeat("a", 64)
	newToken := strings.Repeat("b", 64)
	path, err := cfg.StageRemoteToken(oldToken, gateway.URL, "old-public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: oldToken, Origin: gateway.URL, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	_ = cfg.RemovePendingRemoteToken(path)
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.StageRemoteToken(newToken, gateway.URL, crypto.PublicBase64(id.Public())); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requestCh:
		if request.Candidate != newToken || request.Previous != newToken {
			t.Fatal("retry preferred revoked active token over possibly authorized new-device candidate")
		}
	case <-time.After(time.Second):
		t.Fatal("retry request missing")
	}
}

func TestNOSECUncertainCandidateCannotBeShadowedByAnotherNewDevice(t *testing.T) {
	var requests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporary uncertainty", 500)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = gateway.URL
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	candidate := strings.Repeat("c", 64)
	if _, err := cfg.StageRemoteToken(candidate, gateway.URL, crypto.PublicBase64(id.Public())); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	if err := host.StartLogin(func(string) error { return nil }, true); err == nil {
		t.Fatal("new device shadowed unresolved candidate that may already be authorized")
	}
	pending, err := cfg.PendingRemoteToken(gateway.URL, crypto.PublicBase64(id.Public()))
	if err != nil || pending.Token != candidate || requests.Load() != 0 {
		t.Fatal("rejected reset changed prior candidate or sent a remote request")
	}
}

func TestNOSECSuccessCannotDeleteConcurrentRetryPrivateProof(t *testing.T) {
	memory := pstore.NewMemory()
	relayHandler := relay.New(memory).Handler()
	afterActivation := make(chan struct{})
	releaseActivation := make(chan struct{})
	var activationCount atomic.Int32
	var starts atomic.Int32
	started := make(chan int32, 2)
	polls := make(chan struct{}, 2)
	var origin string
	var current loginStartRequest
	var currentMu sync.Mutex
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case desktopAuthPrefix + "start":
			currentMu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&current)
			currentMu.Unlock()
			started <- starts.Add(1)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
		case desktopAuthPrefix + "poll":
			currentMu.Lock()
			request := current
			currentMu.Unlock()
			pub, _ := crypto.ParsePublic(request.PublicKey)
			_ = memory.PutHost(r.Context(), pstore.Host{Pub: pub, TokenHash: pstore.HashSecret(request.Candidate), Created: time.Now()})
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "authorized", "host_id": strings.Repeat("a", 32), "user": map[string]string{"id": strings.Repeat("b", 32), "username": "account"}}})
		default:
			relayHandler.ServeHTTP(w, r)
		}
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	defer host.Stop()
	host.loginWait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-polls:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	host.afterLoginActivation = func() {
		if activationCount.Add(1) == 1 {
			close(afterActivation)
			<-releaseActivation
		}
	}
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	<-started
	host.mu.Lock()
	first := host.loginActive
	host.mu.Unlock()
	polls <- struct{}{}
	select {
	case <-afterActivation:
	case <-time.After(time.Second):
		t.Fatal("first login did not activate")
	}
	secondStarted := make(chan error, 1)
	go func() { secondStarted <- host.StartLogin(func(string) error { return nil }, false) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(releaseActivation)
		t.Fatal("second start never obtained its durable candidate")
	}
	close(releaseActivation)
	finishLogin(first)
	if err := <-secondStarted; err != nil {
		t.Fatal(err)
	}
	polls <- struct{}{}
	waitNOSEC(t, func() bool { st := host.Status(); return st.Online || st.Login.State == "error" })
	if !host.Status().Online {
		t.Fatal("earlier success removed concurrent retry's reused private proof")
	}
}

func TestNOSECRejectedCandidateBecomesUncertainBeforeRetryRequest(t *testing.T) {
	var origin string
	var requests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "definitive conflict", 409)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	host.loginWait = func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	defer host.Stop()
	open := func(string) error { return nil }
	if err := host.StartLogin(open, false); err != nil {
		t.Fatal(err)
	}
	waitNOSEC(t, func() bool { return host.Status().Login.ErrorCode == "device_conflict" })
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	public := crypto.PublicBase64(id.Public())
	persisted, err := config.Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := persisted.PendingRemoteToken(origin, public)
	if err != nil || !rejected.ProvenRejected {
		t.Fatal("definitive conflict did not survive restart")
	}
	if err := host.StartLogin(open, false); err != nil {
		t.Fatal(err)
	}
	waitNOSEC(t, func() bool { return host.Status().Login.State == "authorization_pending" })
	persisted, err = config.Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := persisted.PendingRemoteToken(origin, public)
	if err != nil || uncertain.ProvenRejected || uncertain.Token != rejected.Token {
		t.Fatal("retry did not durably restore uncertainty with the same token")
	}
	if err := host.StartLogin(open, true); err == nil {
		t.Fatal("new device shadowed retry that already reached a fresh server flow")
	}
	if requests.Load() != 2 {
		t.Fatal("rejected new device performed network IO")
	}
}

func TestNOSECLateStartConflictCannotRejectReplacementFlowProof(t *testing.T) {
	var origin string
	var requests atomic.Int32
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
			http.Error(w, "late conflict", 409)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
	}))
	origin = gateway.URL
	defer gateway.Close()
	defer close(releaseFirst)
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	host.loginWait = func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	defer host.Stop()
	open := func(string) error { return nil }
	if err := host.StartLogin(open, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first start missing")
	}
	if err := host.StartLogin(open, false); err != nil {
		t.Fatal(err)
	}
	waitNOSEC(t, func() bool { return host.Status().Login.State == "authorization_pending" })
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := cfg.PendingRemoteToken(origin, crypto.PublicBase64(id.Public()))
	if err != nil || pending.Token == "" || pending.ProvenRejected {
		t.Fatal("late conflict destroyed replacement flow proof")
	}
}
