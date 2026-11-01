package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func webFixture(t *testing.T) (*fixture, *httptest.Server, string) {
	t.Helper()
	token := "agm_" + strings.Repeat("a", 64)
	f := &fixture{review: "approved", api: true, counts: map[string]int{}, modelMode: "tools", flow: &flow{Candidate: token, Authorized: true, Expires: time.Now().Add(time.Hour)}}
	server := httptest.NewServer(f)
	f.origin, f.anonymous = server.URL, server.URL
	t.Cleanup(server.Close)
	return f, server, token
}

func webRequest(t *testing.T, server *httptest.Server, path, token, body string, status int) string {
	t.Helper()
	method := http.MethodPost
	if body == "" {
		method = http.MethodGet
	}
	req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != status {
		t.Fatalf("%s status %d want %d: %v", path, res.StatusCode, status, err)
	}
	return string(raw)
}

func TestNativeWebFixtureKeepsPersonalAndGatewayCredentialsSeparate(t *testing.T) {
	f, server, token := webFixture(t)
	raw := webRequest(t, server, "/personal/v1/models", "native-personal-key", "", 200)
	if !strings.Contains(raw, "native-personal-model") {
		t.Fatal("native UI cannot discover its personal model")
	}
	webRequest(t, server, "/personal/v1/models", token, "", 401)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts["personal_mobile_token_leak"] != 1 || f.counts["personal_models"] != 1 {
		t.Fatal("personal discovery did not distinguish the two authorities")
	}
}

func TestNativeWebFixtureModelsUseTwoCloudToolsThenContinueWithoutCredentialLeak(t *testing.T) {
	for _, source := range []string{"gateway", "personal"} {
		t.Run(source, func(t *testing.T) {
			f, server, token := webFixture(t)
			path, key, model := "/v1/chat/completions", token, "native-fixture-model"
			if source == "personal" {
				path, key, model = "/personal/v1/chat/completions", "native-personal-key", "native-personal-model"
			}
			messages := []map[string]any{{"role": "user", "content": "Use web evidence"}}
			for _, expected := range []string{"web_search", "web_fetch", "Native cloud evidence answer"} {
				body, _ := json.Marshal(map[string]any{"model": model, "stream": true, "messages": messages, "tools": []map[string]any{{"type": "function", "function": map[string]any{"name": "web_search"}}, {"type": "function", "function": map[string]any{"name": "web_fetch"}}}})
				raw := webRequest(t, server, path, key, string(body), 200)
				if !strings.Contains(raw, expected) || strings.Contains(raw, token) || strings.Contains(raw, "native-personal-key") {
					t.Fatalf("native %s round lost %s or leaked authority", source, expected)
				}
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": expected, "content": "fixture evidence"})
			}
			plain, _ := json.Marshal(map[string]any{"model": model, "stream": true, "messages": messages})
			if !strings.Contains(webRequest(t, server, path, key, string(plain), 200), "Native ordinary answer") {
				t.Fatal("switching Web tools off did not recover ordinary model chat")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.counts[source+"_model_calls"] != 4 || f.counts["invalid_model_contract"] != 0 {
				t.Fatal("fixture did not independently count the model source")
			}
		})
	}
}

func TestNativeWebFixtureToolDenialAndCancellationRemainObservable(t *testing.T) {
	f, server, token := webFixture(t)
	webRequest(t, server, "/v1/web/search", token, `{"query":"current information"}`, 200)
	webRequest(t, server, "/v1/web/fetch", token, `{"url":"https://public.invalid/page","formats":["markdown"],"actions":[{"type":"wait","milliseconds":1}]}`, 200)
	webRequest(t, server, "/fixture/control?web=denied", "", "", 200)
	webRequest(t, server, "/v1/web/search", token, `{"query":"current information"}`, 403)
	webRequest(t, server, "/fixture/control?web=stall", "", "", 200)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/web/search", strings.NewReader(`{"query":"current information"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		t.Fatal("stalled cloud tool did not wait for native cancellation")
	}
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		f.mu.Lock()
		cancelled := f.counts["web_cancelled"]
		f.mu.Unlock()
		if cancelled == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, expected := range map[string]int{"web_search": 3, "web_fetch": 1, "web_denied": 1, "web_stall_started": 1, "web_cancelled": 1, "web_completed": 0} {
		if f.counts[key] != expected {
			t.Fatalf("native fixture %s=%d want %d", key, f.counts[key], expected)
		}
	}
}
