package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func TestSameRelayCountsActualRequestOrigins(t *testing.T) {
	f := &fixture{counts: map[string]int{}, hub: relay.New(pstore.NewMemory()).Handler()}
	primary := httptest.NewServer(f)
	defer primary.Close()
	anonymous := httptest.NewServer(f)
	defer anonymous.Close()
	f.origin, f.anonymous = primary.URL, anonymous.URL
	for _, origin := range []string{primary.URL, anonymous.URL} {
		response, err := http.Post(origin+"/pairlink/v1/pairings/redeem", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid proof must reach the real relay: %d", response.StatusCode)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts["primary_origin_redeem"] != 1 || f.counts["anonymous_origin_redeem"] != 1 || f.counts["anonymous_redeem"] != 2 {
		t.Fatal("same relay path lost the actual listener Host identity")
	}
}

func TestFixtureHTTPProofAndTransportFailureBoundaries(t *testing.T) {
	f := &fixture{review: "pending", api: true, counts: map[string]int{}}
	server := httptest.NewServer(f)
	defer server.Close()
	f.origin, f.anonymous = server.URL, server.URL
	native := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(client *http.Client, method, path, body, token string, expected int, headers ...string) string {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != expected {
			t.Fatalf("%s status=%d want=%d: %v", path, response.StatusCode, expected, err)
		}
		return string(raw)
	}
	control := func(query string) { request(native, "GET", "/fixture/control?"+query, "", "", 200) }
	count := func(name string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.counts[name] }
	waitCount := func(name string) {
		t.Helper()
		until := time.Now().Add(time.Second)
		for time.Now().Before(until) {
			if count(name) > 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("cancellation did not reach fixture: " + name)
	}
	request(native, "OPTIONS", clientPrefix+"/bootstrap", "", "", 204)
	request(native, "GET", "/fixture/loopback-ws", "", "", 400)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/fixture/loopback-ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, body, err := ws.ReadMessage(); err != nil || string(body) != "loopback" {
		t.Fatalf("loopback WebSocket probe failed: %v", err)
	}
	ws.Close()
	request(native, "GET", "/mobile-login", "", "", 404)
	request(native, "GET", "/favicon.ico", "", "", 404)
	if count("unauthorized_business") != 0 || count("non_api_probe") != 1 {
		t.Fatal("browser asset probes must not masquerade as unauthorized business calls")
	}
	request(native, "GET", clientPrefix+"/bootstrap", "", "", 401)
	request(native, "GET", "/fixture/control?review=unknown", "", "", 400)
	control("start=redirect")
	request(native, "POST", clientPrefix+"/auth/mobile/start", `{}`, "", 307)
	if count("redirect_target") != 0 || f.flow != nil {
		t.Fatal("denied redirect activated a flow or reached target")
	}
	control("start=delay")
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+clientPrefix+"/auth/mobile/start", strings.NewReader(`{}`))
	if response, err := native.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("delayed response ignored cancellation")
	}
	cancel()
	waitCount("delay_cancelled")
	if count("delay_completed") != 0 {
		t.Fatal("cancelled start completed late")
	}
	control("start=normal")
	for _, body := range []string{`{`, `{"candidate_token":"wrong","code_challenge":"x"}`} {
		request(native, "POST", clientPrefix+"/auth/mobile/start", body, "", 400)
	}
	identity, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier := secret(32)
	proof := sha256.Sum256([]byte(verifier))
	token := "agm_" + strings.Repeat("d", 64)
	input := map[string]string{"device_pub": "bad", "candidate_token": token, "code_challenge": base64.RawURLEncoding.EncodeToString(proof[:]), "platform": "android"}
	raw, _ := json.Marshal(input)
	request(native, "POST", clientPrefix+"/auth/mobile/start", string(raw), "", 400)
	input["device_pub"] = linkcrypto.PublicBase64(identity.Public())
	raw, _ = json.Marshal(input)
	request(native, "POST", clientPrefix+"/auth/mobile/start", string(raw), "", 201)
	request(native, "POST", clientPrefix+"/auth/mobile/poll", `{}`, "", 401)
	request(native, "GET", clientPrefix+"/auth/sso/nosec/start?mobile_code=wrong", "", "", 400)
	request(native, "GET", "/fixture/cas/login?service=wrong", "", "", 400)
	request(native, "GET", "/fixture/cas/confirm", "", "", 400)
	request(native, "GET", "/auth/cas/callback?state=wrong", "", "", 401)
	request(native, "GET", "/mobile-login?code="+f.flow.PublicCode, "", "", 401)
	request(native, "GET", "/fixture/authorize", "", "", 403)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	request(browser, "GET", clientPrefix+"/auth/sso/nosec/start?mobile_code="+f.flow.PublicCode, "", "", 200)
	page, err := browser.PostForm(server.URL+"/fixture/cas/confirm", url.Values{"state": {f.flow.State}})
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	request(browser, "GET", "/auth/cas/callback?state="+f.flow.State+"&ticket="+f.flow.Ticket, "", "", 401)
	request(browser, "GET", "/mobile-login?code=wrong", "", "", 401)
	control("review=rejected")
	if !strings.Contains(request(browser, "GET", "/mobile-login?code="+f.flow.PublicCode, "", "", 200), "Account rejected") {
		t.Fatal("rejected browser offered consent")
	}
	control("review=approved&api=off")
	page, err = browser.PostForm(server.URL+"/fixture/authorize", url.Values{"csrf": {f.flow.CSRF}})
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	request(native, "GET", clientPrefix+"/auth/me", "", token, 200)
	request(native, "GET", clientPrefix+"/bootstrap", "", token, 200, "Cookie", "unexpected-browser-cookie")
	if count("native_cookie_leak") != 1 {
		t.Fatal("fixture observation missed leaked cookie")
	}
	request(native, "GET", "/v1/models", "", token, 403)
	request(native, "POST", "/v1/chat/completions", `{}`, token, 403)
	control("review=rejected")
	request(native, "GET", clientPrefix+"/heartbeat", "", token, 403)
	control("review=approved&api=on&catalog=redirect&model=redirect")
	const modelBody = `{"model":"native-fixture-model","stream":true,"messages":[{"role":"user","content":"native fixture"}]}`
	request(native, "POST", "/v1/chat/completions", `{"model":"wrong-source","stream":true,"messages":[{"role":"user","content":"native fixture"}]}`, token, 400)
	request(native, "POST", clientPrefix+"/link-hosts/native-pc/connect", `{}`, token, 409)
	request(native, "GET", "/v1/models", "", token, 307)
	request(native, "POST", "/v1/chat/completions", modelBody, token, 307)
	if count("redirect_target") != 0 {
		t.Fatal("native caller followed transport redirect")
	}
	control("model=stall")
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	req, _ = http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(modelBody))
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := native.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(io.Discard, response.Body); err == nil {
		t.Fatal("stalled stream ignored cancellation")
	}
	response.Body.Close()
	cancel()
	waitCount("model_cancelled")
	if count("model_completed") != 0 {
		t.Fatal("cancelled model completed late")
	}
	request(native, "GET", "/not-a-resource", "", token, 404)
	request(native, "GET", "/fixture/forbidden-target", "", "", 400)
	if count("redirect_target") != 1 {
		t.Fatal("fixture target observation did not detect an actual visit")
	}
}
