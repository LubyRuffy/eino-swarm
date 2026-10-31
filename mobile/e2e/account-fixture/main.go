// account-fixture is a loopback-only native-client contract fixture. Its CAS
// pages are synthetic; it does not authenticate a production NOSEC account.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

const clientPrefix = "/client/api/v1"

type flow struct {
	DeviceCode, PublicCode, Candidate, Challenge, DevicePub string
	State, Ticket, Browser, CSRF                            string
	Logged, Authorized                                      bool
	Platform                                                string
	Expires                                                 time.Time
}

type fixture struct {
	mu                                sync.Mutex
	origin, anonymous                 string
	review                            string
	api, showPC                       bool
	startMode, catalogMode, modelMode string
	flow                              *flow
	counts                            map[string]int
	bindings                          *pstore.Memory
	hub                               http.Handler
	relay                             *relay.Hub
	host                              *remote.Host
	hostPub                           []byte
}

func secret(size int) string {
	raw := make([]byte, size)
	// Go 1.26 rand.Read always fills the buffer or terminates the process.
	rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func publicCode() string {
	raw := make([]byte, 8)
	rand.Read(raw)
	code := hex.EncodeToString(raw)
	return code[:4] + "-" + code[4:8] + "-" + code[8:12] + "-" + code[12:]
}

func data(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value})
}

func (f *fixture) user() map[string]any {
	return map[string]any{"id": "native-owner", "username": "Native test account", "enabled": true, "approval_status": f.review}
}

func (f *fixture) device() map[string]any {
	return map[string]any{"id": "native-mobile", "expires_at": f.flow.Expires.UTC().Format(time.RFC3339), "name": "Native test phone", "platform": f.flow.Platform}
}

func (f *fixture) observe(w http.ResponseWriter) {
	f.counts["inference"] = f.counts["model_calls"]
	f.counts["qr_redeem"] = f.counts["anonymous_redeem"]
	if f.relay != nil {
		f.counts["peer_messages"] = len(f.relay.SniffedPayloads())
	}
	value := map[string]any{"origin": f.origin, "anonymous_origin": f.anonymous, "review": f.review, "api_enabled": f.api, "pc_visible": f.showPC, "host_name": "Native PC", "host_id": "native-pc", "counts": f.counts}
	if f.flow != nil {
		value["verification_uri"] = f.origin + clientPrefix + "/auth/sso/nosec/start?mobile_code=" + f.flow.PublicCode
		value["device_confirm_url"] = f.origin + "/mobile-login?code=" + f.flow.PublicCode
	}
	data(w, value)
}

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/fixture/loopback-ws" {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.WriteMessage(websocket.TextMessage, []byte("loopback"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/pairlink/") {
		if r.URL.Path == "/pairlink/v1/pairings/redeem" || r.URL.Path == "/pairlink/v1/ws" {
			f.mu.Lock()
			kind := "redeem"
			if r.URL.Path == "/pairlink/v1/ws" {
				kind = "ws"
			}
			if r.Host == strings.TrimPrefix(f.anonymous, "http://") {
				f.counts["anonymous_origin_"+kind]++
			} else if r.Host == strings.TrimPrefix(f.origin, "http://") {
				f.counts["primary_origin_"+kind]++
			}
			f.mu.Unlock()
		}
		if r.URL.Path == "/pairlink/v1/pairings/redeem" {
			f.mu.Lock()
			f.counts["anonymous_redeem"]++
			if r.Header.Get("Authorization") != "" {
				f.counts["anonymous_authorization_leak"]++
			}
			f.mu.Unlock()
		}
		f.hub.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/fixture/anonymous-offer" {
		offer, err := f.host.Offer(r.Context())
		if err != nil {
			http.Error(w, "fixture host unavailable", 503)
			return
		}
		parsed, err := protocol.Parse(offer.URI)
		if err != nil {
			http.Error(w, "fixture offer unavailable", 503)
			return
		}
		parsed.HubURL = f.anonymous
		uri, err := protocol.Encode(parsed)
		if err != nil {
			http.Error(w, "fixture offer invalid", 503)
			return
		}
		data(w, map[string]string{"uri": uri})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	if path == "/fixture/forbidden-target" {
		f.counts["redirect_target"]++
		http.Error(w, "redirect followed", http.StatusBadRequest)
		return
	}
	if path == "/fixture/state" || path == "/fixture/control" {
		if path == "/fixture/control" {
			for key, target := range map[string]*string{"start": &f.startMode, "catalog": &f.catalogMode, "model": &f.modelMode} {
				if mode := r.URL.Query().Get(key); mode != "" {
					*target = mode
				}
			}
			if review := r.URL.Query().Get("review"); review != "" {
				if review != "pending" && review != "approved" && review != "rejected" {
					http.Error(w, "invalid review", 400)
					return
				}
				f.review = review
			}
			if pc := r.URL.Query().Get("pc"); pc != "" {
				f.showPC = pc == "show"
			}
			if api := r.URL.Query().Get("api"); api != "" {
				f.api = api == "on"
			}
		}
		f.observe(w)
		return
	}
	if path == clientPrefix+"/auth/mobile/start" && r.Method == http.MethodPost {
		if f.startMode == "redirect" {
			f.counts["redirect_sent"]++
			http.Redirect(w, r, f.anonymous+"/fixture/forbidden-target", http.StatusTemporaryRedirect)
			return
		}
		if f.startMode == "delay" {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				http.Error(w, "invalid delayed request", 400)
				return
			}
			f.counts["delay_started"]++
			f.mu.Unlock()
			select {
			case <-r.Context().Done():
				f.mu.Lock()
				f.counts["delay_cancelled"]++
				return
			case <-time.After(20 * time.Second):
				f.mu.Lock()
				f.counts["delay_completed"]++
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		var input struct {
			Name, Platform, Version string
			DevicePub               string `json:"device_pub"`
			Candidate               string `json:"candidate_token"`
			Challenge               string `json:"code_challenge"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input) != nil || len(input.Challenge) != 43 || len(input.Candidate) != 68 || !strings.HasPrefix(input.Candidate, "agm_") {
			http.Error(w, "invalid native device proof", 400)
			return
		}
		if _, err := linkcrypto.ParsePublic(input.DevicePub); err != nil {
			http.Error(w, "invalid device public key", 400)
			return
		}
		f.flow = &flow{DeviceCode: secret(32), PublicCode: publicCode(), Candidate: input.Candidate, Challenge: input.Challenge, DevicePub: input.DevicePub, State: secret(24), Browser: secret(24), CSRF: secret(24), Platform: input.Platform, Expires: time.Now().Add(time.Hour)}
		f.counts["start"]++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		data(w, map[string]any{"device_code": f.flow.DeviceCode, "user_code": f.flow.PublicCode, "verification_uri": f.origin + clientPrefix + "/auth/sso/nosec/start?mobile_code=" + f.flow.PublicCode, "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339), "interval": 5})
		return
	}
	if path == clientPrefix+"/auth/mobile/poll" && r.Method == http.MethodPost {
		var input struct {
			DeviceCode string `json:"device_code"`
			Verifier   string `json:"code_verifier"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || f.flow == nil || input.DeviceCode != f.flow.DeviceCode {
			http.Error(w, "invalid device code", 401)
			return
		}
		proof := sha256.Sum256([]byte(input.Verifier))
		if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(proof[:])), []byte(f.flow.Challenge)) != 1 {
			f.counts["invalid_pkce"]++
			http.Error(w, "invalid PKCE", 401)
			return
		}
		f.counts["poll"]++
		status := "authorization_pending"
		if f.flow.Logged && f.review != "approved" {
			status = "account_" + f.review
		}
		if f.flow.Authorized && f.review == "approved" {
			data(w, map[string]any{"status": "authorized", "mobile_id": "native-mobile", "user": f.user()})
		} else {
			data(w, map[string]string{"status": status})
		}
		return
	}
	if f.browser(w, r) {
		return
	}
	if !strings.HasPrefix(path, clientPrefix+"/") && !strings.HasPrefix(path, "/v1/") {
		f.counts["non_api_probe"]++
		http.NotFound(w, r)
		return
	}
	if f.flow == nil || !f.flow.Authorized || r.Header.Get("Authorization") != "Bearer "+f.flow.Candidate {
		f.counts["unauthorized_business"]++
		http.Error(w, "mobile login required", 401)
		return
	}
	if r.Header.Get("Cookie") != "" {
		f.counts["native_cookie_leak"]++
	}
	if path == clientPrefix+"/auth/me" {
		data(w, map[string]any{"user": f.user(), "device": f.device()})
		return
	}
	if path == clientPrefix+"/auth/logout" {
		f.counts["logout"]++
		f.flow.Authorized = false
		data(w, map[string]bool{"ok": true})
		return
	}
	if f.review != "approved" {
		f.counts["unapproved_business"]++
		http.Error(w, "review required", 403)
		return
	}
	switch path {
	case clientPrefix + "/bootstrap":
		f.counts["bootstrap"]++
		data(w, map[string]any{"user": f.user(), "device": f.device(), "models": map[string]any{"source_id": "gateway:native-owner", "source_kind": "gateway", "label": "Native gateway", "execution": "phone", "api_base_url": f.origin + "/v1", "catalog_url": f.origin + "/v1/models", "api_access_enabled": f.api}, "pairlink": map[string]any{"enabled": true, "hub_url": f.origin, "redeem_url": f.origin + "/pairlink/v1/pairings/redeem", "websocket_url": "ws" + strings.TrimPrefix(f.origin, "http") + "/pairlink/v1/ws", "info_url": f.origin + "/pairlink/v1/info"}, "heartbeat_interval": 30, "presence_ttl": 90})
	case clientPrefix + "/heartbeat":
		f.counts["heartbeat"]++
		data(w, map[string]any{"ok": true, "heartbeat_interval": 30, "presence_ttl": 90})
	case clientPrefix + "/link-hosts":
		hosts := []map[string]any{}
		if f.showPC {
			hosts = append(hosts, map[string]any{"id": "native-pc", "name": "Native PC", "hostname": "native-pc", "version": "native-fixture", "enabled": true, "online": f.host.Status().Online})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": hosts, "meta": map[string]int{"total": len(hosts)}})
	case clientPrefix + "/link-hosts/native-pc/connect":
		if !f.showPC || !f.host.Status().Online {
			http.Error(w, "PC unavailable", 409)
			return
		}
		var input struct {
			DevicePub string `json:"device_pub"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.DevicePub != f.flow.DevicePub {
			http.Error(w, "device proof mismatch", 400)
			return
		}
		pub, err := linkcrypto.ParsePublic(input.DevicePub)
		if err != nil {
			http.Error(w, "invalid device key", 400)
			return
		}
		rawTicket := make([]byte, 32)
		rand.Read(rawTicket)
		ticket, id := hex.EncodeToString(rawTicket), secret(16)
		sid, _ := base64.RawURLEncoding.DecodeString(secret(protocol.SessionIDSize))
		if err := f.bindings.PutBinding(r.Context(), pstore.Binding{ID: id, HostPub: f.hostPub, DevicePub: pub, TicketHash: pstore.HashSecret(ticket), SessionID: sid, Created: time.Now()}); err != nil {
			http.Error(w, "binding unavailable", 503)
			return
		}
		f.counts["connect"]++
		data(w, map[string]any{"binding_id": id, "ticket": ticket, "host_pub": linkcrypto.PublicBase64(f.hostPub), "session_id": hex.EncodeToString(sid)})
	case "/v1/models":
		if f.catalogMode == "redirect" {
			f.counts["catalog_redirect_sent"]++
			http.Redirect(w, r, f.anonymous+"/fixture/forbidden-target", http.StatusTemporaryRedirect)
			return
		}
		if !f.api {
			http.Error(w, "API disabled", 403)
			return
		}
		f.counts["models"]++
		data(w, []map[string]string{{"id": "native-fixture-model"}})
	case "/v1/chat/completions":
		if !f.api {
			http.Error(w, "API disabled", 403)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid model body", 400)
			return
		}
		var invocation struct {
			Model    string            `json:"model"`
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
		}
		if json.Unmarshal(body, &invocation) != nil || invocation.Model != "native-fixture-model" || !invocation.Stream || len(invocation.Messages) == 0 {
			f.counts["invalid_model_contract"]++
			http.Error(w, "invalid fixture model invocation", 400)
			return
		}
		f.counts["model_calls"]++
		if f.modelMode == "redirect" {
			f.counts["model_redirect_sent"]++
			http.Redirect(w, r, f.anonymous+"/fixture/forbidden-target", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if f.modelMode == "stall" {
			f.counts["model_stall_started"]++
			w.WriteHeader(http.StatusOK)
			if flush, ok := w.(http.Flusher); ok {
				flush.Flush()
			}
			f.mu.Unlock()
			select {
			case <-r.Context().Done():
				f.mu.Lock()
				f.counts["model_cancelled"]++
				return
			case <-time.After(30 * time.Second):
				f.mu.Lock()
				f.counts["model_completed"]++
			}
		}
		f.counts["model_successes"]++
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Native account answer\"}}]}\n\ndata: [DONE]\n\n")
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) browser(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != clientPrefix+"/auth/sso/nosec/start" && path != "/fixture/cas/login" && path != "/fixture/cas/confirm" && path != "/auth/cas/callback" && path != "/mobile-login" && path != "/fixture/authorize" {
		return false
	}
	if f.flow == nil {
		http.Error(w, "device flow unavailable", 404)
		return true
	}
	q := url.Values{"state": {f.flow.State}}
	service := f.origin + "/auth/cas/callback?" + q.Encode()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch path {
	case clientPrefix + "/auth/sso/nosec/start":
		if r.URL.Query().Get("mobile_code") != f.flow.PublicCode {
			http.Error(w, "wrong public code", 400)
			return true
		}
		f.counts["browser_start"]++
		http.SetCookie(w, &http.Cookie{Name: "native_fixture_browser", Value: f.flow.Browser, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, f.origin+"/fixture/cas/login?"+url.Values{"service": {service}}.Encode(), http.StatusSeeOther)
	case "/fixture/cas/login":
		if r.URL.Query().Get("service") != service {
			http.Error(w, "wrong service", 400)
			return true
		}
		_, _ = fmt.Fprintf(w, `<meta name="viewport" content="width=device-width"><h1>Fixture NOSEC sign in</h1><form action="/fixture/cas/confirm" method="post"><input type="hidden" name="state" value="%s"><button>Sign in to test NOSEC</button></form>`, html.EscapeString(f.flow.State))
	case "/fixture/cas/confirm":
		if r.Method != http.MethodPost || r.FormValue("state") != f.flow.State {
			http.Error(w, "wrong state", 400)
			return true
		}
		f.flow.Ticket = secret(24)
		f.counts["cas_login"]++
		http.Redirect(w, r, service+"&ticket="+url.QueryEscape(f.flow.Ticket), http.StatusSeeOther)
	case "/auth/cas/callback":
		cookie, err := r.Cookie("native_fixture_browser")
		if err != nil || cookie.Value != f.flow.Browser || r.URL.Query().Get("state") != f.flow.State || r.URL.Query().Get("ticket") != f.flow.Ticket || f.flow.Logged {
			http.Error(w, "invalid callback", 401)
			return true
		}
		f.flow.Logged = true
		f.counts["cas_callback"]++
		http.Redirect(w, r, "/mobile-login?code="+f.flow.PublicCode, http.StatusSeeOther)
	case "/mobile-login":
		cookie, err := r.Cookie("native_fixture_browser")
		if err != nil || cookie.Value != f.flow.Browser || !f.flow.Logged || r.URL.Query().Get("code") != f.flow.PublicCode {
			http.Error(w, "browser proof required", 401)
			return true
		}
		_, _ = io.WriteString(w, `<meta name="viewport" content="width=device-width">`)
		if f.review != "approved" {
			_, _ = io.WriteString(w, `<meta http-equiv="refresh" content="2">`)
		}
		if f.review == "approved" {
			_, _ = fmt.Fprintf(w, `<h1>Authorize native phone</h1><form method="post" action="/fixture/authorize"><input type="hidden" name="csrf" value="%s"><button>Authorize test device</button></form>`, html.EscapeString(f.flow.CSRF))
		} else if f.review == "rejected" {
			_, _ = io.WriteString(w, "<h1>Account rejected</h1>")
		} else {
			_, _ = io.WriteString(w, "<h1>Waiting for administrator approval</h1>")
		}
	case "/fixture/authorize":
		cookie, err := r.Cookie("native_fixture_browser")
		if err != nil || cookie.Value != f.flow.Browser || r.Method != http.MethodPost || f.review != "approved" || !f.flow.Logged || r.FormValue("csrf") != f.flow.CSRF {
			http.Error(w, "authorization refused", 403)
			return true
		}
		f.flow.Authorized = true
		f.counts["browser_authorize"]++
		_, _ = io.WriteString(w, `<meta name="viewport" content="width=device-width"><h1>Device authorized. Return to zwai.</h1>`)
	}
	return true
}

func loopbackListener(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("native fixture requires a literal loopback address")
	}
	return net.Listen("tcp", address)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "literal loopback listen address")
	directory := flag.String("data-dir", "", "private disposable fixture directory")
	ready := flag.String("ready-file", "", "private JSON readiness receipt")
	duration := flag.Duration("max-duration", 10*time.Minute, "maximum lifetime of this disposable fixture")
	flag.Parse()
	if *directory == "" || *ready == "" || *duration <= 0 || *duration > 30*time.Minute {
		panic("private data-dir and ready-file are required")
	}
	cfg, err := config.Load(*directory)
	if err != nil {
		panic(err)
	}
	listener, err := loopbackListener(*listen)
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	anonymous, err := loopbackListener("127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer anonymous.Close()
	f := &fixture{origin: "http://" + listener.Addr().String(), anonymous: "http://" + anonymous.Addr().String(), review: "pending", api: true, counts: map[string]int{}, bindings: pstore.NewMemory()}
	hub := relay.New(f.bindings)
	f.relay = hub
	f.hub = hub.Handler()
	server, anonServer := &http.Server{Handler: f, ReadHeaderTimeout: 5 * time.Second}, &http.Server{Handler: f, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() { _ = anonServer.Serve(anonymous) }()
	cfg.Remote.Enabled, cfg.Remote.HubURL, cfg.Remote.DisplayName, cfg.Remote.KeepAwake = true, f.origin, "Native PC", false
	token, err := relay.IssueHostToken(context.Background(), f.bindings)
	if err != nil {
		panic(err)
	}
	if err := cfg.WriteHostToken(token); err != nil {
		panic(err)
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		panic(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, db, provider.NewMock(cfg), logger)
	if _, err := eng.CreateThread("Native PC conversation", "", ""); err != nil {
		panic(err)
	}
	f.host = remote.New(eng, cfg, logger)
	f.host.Start()
	identity, err := cfg.RemoteIdentity()
	if err != nil {
		panic(err)
	}
	key, err := linkcrypto.FromPrivate(identity)
	if err != nil {
		panic(err)
	}
	f.hostPub = key.Public()
	if !f.host.Status().Online {
		panic("native fixture PC failed to connect")
	}
	if err := os.MkdirAll(filepath.Dir(*ready), 0700); err != nil {
		panic(err)
	}
	raw, _ := json.Marshal(map[string]any{"origin": f.origin, "anonymous_origin": f.anonymous, "fixture": "synthetic CAS with real native HTTP and Pairlink/Eino host", "pc_online": true})
	if err := os.WriteFile(*ready, raw, 0600); err != nil {
		panic(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case <-time.After(*duration):
	}
	signal.Stop(stop)
	f.host.Stop()
	hub.ClosePeers()
	eng.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	_ = anonServer.Shutdown(ctx)
	_ = db.Close()
}
