package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/pairlink/client"
	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
)

func TestCLIIncludesRealEncryptedPCAndAnonymousOffer(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready.json")
	previousArgs, previousFlags := os.Args, flag.CommandLine
	os.Args = []string{"account-fixture", "--data-dir", filepath.Join(dir, "data"), "--ready-file", ready, "--max-duration", "3s"}
	flag.CommandLine = flag.NewFlagSet("fixture-test", flag.ContinueOnError)
	defer func() { os.Args = previousArgs; flag.CommandLine = previousFlags }()
	finished := make(chan struct{})
	go func() { main(); close(finished) }()
	defer func() {
		select {
		case <-finished:
		case <-time.After(8 * time.Second):
			t.Error("fixture did not stop its owned resources")
		}
	}()
	var info struct {
		Origin    string `json:"origin"`
		Anonymous string `json:"anonymous_origin"`
	}
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		raw, err := os.ReadFile(ready)
		if err == nil && json.Unmarshal(raw, &info) == nil && info.Origin != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info.Origin == "" {
		t.Fatal("CLI did not publish its loopback readiness")
	}
	native := &http.Client{Timeout: time.Second}
	do := func(method, path, token, body string) string {
		t.Helper()
		req, _ := http.NewRequest(method, info.Origin+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := native.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			t.Fatalf("%s status%d: %s", path, res.StatusCode, raw)
		}
		return string(raw)
	}
	identity, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier := secret(32)
	challenge := sha256.Sum256([]byte(verifier))
	token := "agm_" + strings.Repeat("b", 64)
	input, _ := json.Marshal(map[string]string{"device_pub": linkcrypto.PublicBase64(identity.Public()), "candidate_token": token, "code_challenge": base64.RawURLEncoding.EncodeToString(challenge[:]), "platform": "android"})
	var started struct {
		Data struct {
			DeviceCode   string `json:"device_code"`
			Verification string `json:"verification_uri"`
		}
	}
	if err := json.Unmarshal([]byte(do("POST", clientPrefix+"/auth/mobile/start", "", string(input))), &started); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, Timeout: time.Second}
	page, err := browser.Get(started.Data.Verification)
	if err != nil {
		t.Fatal(err)
	}
	markup, _ := io.ReadAll(page.Body)
	page.Body.Close()
	state := regexp.MustCompile(`name="state" value="([^"]+)"`).FindStringSubmatch(string(markup))
	if len(state) != 2 {
		t.Fatal("Browser did not reach actual fixture login form")
	}
	page, err = browser.PostForm(info.Origin+"/fixture/cas/confirm", url.Values{"state": {state[1]}})
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	do("GET", "/fixture/control?review=approved&pc=show", "", "")
	var observed struct {
		Data struct {
			Confirm string `json:"device_confirm_url"`
		}
	}
	json.Unmarshal([]byte(do("GET", "/fixture/state", "", "")), &observed)
	page, err = browser.Get(observed.Data.Confirm)
	if err != nil {
		t.Fatal(err)
	}
	markup, _ = io.ReadAll(page.Body)
	page.Body.Close()
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(string(markup))
	if len(csrf) != 2 {
		t.Fatal("Approved Browser did not reach explicit consent")
	}
	if strings.Contains(string(markup), "http-equiv") {
		t.Fatal("approved consent must remain stable while the real user activates its button")
	}
	page, err = browser.PostForm(info.Origin+"/fixture/authorize", url.Values{"csrf": {csrf[1]}})
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	poll, _ := json.Marshal(map[string]string{"device_code": started.Data.DeviceCode, "code_verifier": verifier})
	if !strings.Contains(do("POST", clientPrefix+"/auth/mobile/poll", "", string(poll)), "authorized") {
		t.Fatal("real Browser consent failed")
	}
	do("POST", clientPrefix+"/heartbeat", token, `{}`)
	if !strings.Contains(do("GET", clientPrefix+"/link-hosts", token, ""), "Native PC") {
		t.Fatal("registered PC not visible")
	}
	pub, _ := json.Marshal(map[string]string{"device_pub": linkcrypto.PublicBase64(identity.Public())})
	var paired struct {
		Data struct {
			Ticket  string `json:"ticket"`
			HostPub string `json:"host_pub"`
			Session string `json:"session_id"`
		}
	}
	json.Unmarshal([]byte(do("POST", clientPrefix+"/link-hosts/native-pc/connect", token, string(pub))), &paired)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(paired.Data.Ticket) {
		t.Fatal("managed ticket must preserve the Gateway's 32-byte hex wire contract")
	}
	hostPub, err := linkcrypto.ParsePublic(paired.Data.HostPub)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := hex.DecodeString(paired.Data.Session)
	if err != nil || len(sid) != 16 {
		t.Fatal("session identity changed wire")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	connection, err := client.Dial(ctx, client.Config{HubURL: info.Origin, Identity: identity, Token: paired.Data.Ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	link, err := connection.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		t.Fatal(err)
	}
	rpc, _ := json.Marshal(remote.Request{V: remote.ProtocolV, ID: "native", Op: remote.OpList})
	if err := link.Send(rpc); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-link.Recv():
		if !strings.Contains(string(reply), "Native PC conversation") {
			t.Fatal("encrypted reply did not reach actual Eino host")
		}
	case <-ctx.Done():
		t.Fatal("encrypted RPC timed out")
	}
	var offer struct {
		Data struct {
			URI string `json:"uri"`
		}
	}
	json.Unmarshal([]byte(do("GET", "/fixture/anonymous-offer", "", "")), &offer)
	if !strings.HasPrefix(offer.Data.URI, "pairlink:v1:"+info.Anonymous+":") {
		t.Fatal("anonymous QR did not carry the distinct origin of the shared relay")
	}
	parsed, err := protocol.Parse(offer.Data.URI)
	if err != nil {
		t.Fatal(err)
	}
	anonymousIdentity, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := client.RedeemOffer(ctx, parsed, anonymousIdentity); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(do("GET", "/fixture/state", "", ""), `"qr_redeem":1`) {
		t.Fatal("anonymous redemption did not reach actual Hub")
	}
	do("POST", clientPrefix+"/auth/logout", token, `{}`)
}

func TestFixtureRefusesExternalListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", ":0", "192.0.2.1:0", "localhost:0", "bad"} {
		if listener, err := loopbackListener(address); err == nil {
			listener.Close()
			t.Fatalf("external listener admitted: %s", address)
		}
	}
	listener, err := loopbackListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
}

func TestCLIFailsBeforeOpeningResourcesForUnsafeSetup(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{},
		{"--data-dir", dir, "--ready-file", filepath.Join(dir, "ready"), "--max-duration", "31m"},
		{"--data-dir", file, "--ready-file", filepath.Join(dir, "ready")},
		{"--data-dir", dir, "--ready-file", filepath.Join(dir, "ready"), "--listen", "0.0.0.0:0"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			previousArgs, previousFlags := os.Args, flag.CommandLine
			os.Args = append([]string{"account-fixture"}, args...)
			flag.CommandLine = flag.NewFlagSet("fixture-test", flag.ContinueOnError)
			defer func() { os.Args = previousArgs; flag.CommandLine = previousFlags }()
			defer func() {
				if recover() == nil {
					t.Error("unsafe fixture setup did not fail closed")
				}
			}()
			main()
		})
	}
}

func TestNativeAccountFixtureUsesBrowserConsentPKCEAndReview(t *testing.T) {
	f := &fixture{review: "pending", api: true, counts: map[string]int{}}
	server := httptest.NewServer(f)
	defer server.Close()
	f.origin, f.anonymous = server.URL, server.URL
	identity, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier := secret(32)
	challenge := sha256.Sum256([]byte(verifier))
	candidate := "agm_" + strings.Repeat("a", 64)
	post := func(client *http.Client, path, body string, status int) string {
		t.Helper()
		response, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("%s status=%d body=%s", path, response.StatusCode, raw)
		}
		return string(raw)
	}
	raw, _ := json.Marshal(map[string]string{"device_pub": linkcrypto.PublicBase64(identity.Public()), "candidate_token": candidate, "code_challenge": base64.RawURLEncoding.EncodeToString(challenge[:]), "platform": "android"})
	response := post(server.Client(), clientPrefix+"/auth/mobile/start", string(raw), 201)
	if strings.Contains(response, candidate) || strings.Contains(response, verifier) {
		t.Fatal("proof leaked in start response")
	}
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		t.Fatal(err)
	}
	deviceCode, _ := envelope["data"]["device_code"].(string)
	verification, _ := envelope["data"]["verification_uri"].(string)
	poll := func(proof string, status int) string {
		body, _ := json.Marshal(map[string]string{"device_code": deviceCode, "code_verifier": proof})
		return post(server.Client(), clientPrefix+"/auth/mobile/poll", string(body), status)
	}
	poll("wrong", 401)
	if !strings.Contains(poll(verifier, 200), "authorization_pending") {
		t.Fatal("unvisited browser authorized device")
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	login, err := browser.Get(verification)
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	confirmed, err := browser.PostForm(server.URL+"/fixture/cas/confirm", url.Values{"state": {f.flow.State}})
	if err != nil {
		t.Fatal(err)
	}
	confirmed.Body.Close()
	if !strings.Contains(poll(verifier, 200), "account_pending") {
		t.Fatal("pending identity escaped review")
	}
	f.review = "rejected"
	if !strings.Contains(poll(verifier, 200), "account_rejected") {
		t.Fatal("rejected identity escaped review")
	}
	f.review = "approved"
	if !strings.Contains(poll(verifier, 200), "authorization_pending") {
		t.Fatal("approval bypassed browser consent")
	}
	refused, err := browser.PostForm(server.URL+"/fixture/authorize", url.Values{"csrf": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	refused.Body.Close()
	if refused.StatusCode != 403 {
		t.Fatal("missing browser proof accepted")
	}
	allowed, err := browser.PostForm(server.URL+"/fixture/authorize", url.Values{"csrf": {f.flow.CSRF}})
	if err != nil {
		t.Fatal(err)
	}
	allowed.Body.Close()
	if !strings.Contains(poll(verifier, 200), "authorized") {
		t.Fatal("explicit approved consent did not activate device")
	}
	get := func(path string) string {
		t.Helper()
		request, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+candidate)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
		return string(raw)
	}
	bootstrap := get(clientPrefix + "/bootstrap")
	if strings.Contains(bootstrap, candidate) || !strings.Contains(bootstrap, server.URL+"/v1") {
		t.Fatal("bootstrap leaked credential or changed origin")
	}
	if hosts := get(clientPrefix + "/link-hosts"); !strings.Contains(hosts, `"data":[]`) {
		t.Fatal("hidden PC appeared before selection phase")
	}
	if !strings.Contains(get("/v1/models"), "native-fixture-model") {
		t.Fatal("approved server model unavailable")
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"native-fixture-model","stream":true,"messages":[{"role":"user","content":"native fixture"}]}`))
	request.Header.Set("Authorization", "Bearer "+candidate)
	answer, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Body.Close()
	wire, _ := io.ReadAll(answer.Body)
	if !strings.Contains(string(wire), "Native account answer") || f.counts["model_calls"] != 1 || f.counts["connect"] != 0 {
		t.Fatal("server model did not work independently of PC")
	}
	state := get("/fixture/state")
	for _, forbidden := range []string{candidate, verifier, f.flow.DeviceCode, f.flow.CSRF} {
		if strings.Contains(state, forbidden) {
			t.Fatal("private proof entered fixture observation")
		}
	}
}
