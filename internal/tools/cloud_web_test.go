package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
)

func signInWebAccount(t *testing.T, cfg *config.Config, origin string) string {
	t.Helper()
	cfg.Remote.AuthMode = config.RemoteAuthAccount
	cfg.Remote.HubURL = origin
	token := strings.Repeat("a", 64)
	path, err := cfg.StageRemoteToken(token, origin, "test-public-key")
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.ActivateRemoteToken(path, config.RemoteCredential{
		Token: token, Origin: origin, Authorized: true,
		HostID: strings.Repeat("1", 32), UserID: strings.Repeat("2", 32), Username: "Account",
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func builtCloudTool(t *testing.T, cfg *config.Config, name string) tool.InvokableTool {
	t.Helper()
	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	return find(t, set, name)
}

func TestCloudSearchPreservesFunctionParametersAndResultSummaries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if r.URL.Path != "/client/desktop/v1/web/search" || r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || body["query"] != "public references" || body["max_results"] != float64(13) {
			t.Error("cloud search changed its route, query or configured result count")
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" {
			t.Error("cloud credential did not stay exclusively in its Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"provider":"fixture","query":"public references","results":[{"title":"Reference","url":"https://reference.invalid/page","snippet":"Summary","position":1,"published_at":"2026-10-01"}]}}`))
	}))
	defer server.Close()
	cfg := configFor(t)
	signInWebAccount(t, cfg, server.URL)
	cfg.Tools.WebSearchMaxResults = 13
	search := builtCloudTool(t, cfg, "web_search")
	info, err := search.Info(context.Background())
	if err != nil || info.Name != "web_search" {
		t.Fatal("cloud execution changed the function identity")
	}
	parameters, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || parameters.Properties.Len() != 1 || !reflect.DeepEqual(parameters.Required, []string{"query"}) {
		t.Fatal("cloud execution changed the search function schema")
	}
	out, err := search.InvokableRun(context.Background(), `{"query":"public references"}`)
	var result struct {
		Provider string `json:"provider"`
		Results  []struct {
			Summary     string `json:"summary"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	}
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Provider != "fixture" || len(result.Results) != 1 || result.Results[0].Summary != "Summary" || result.Results[0].PublishedAt != "2026-10-01" || calls.Load() != 1 {
		t.Fatalf("cloud search did not retain structured results: %q, %v", out, err)
	}
}

func TestCloudFetchRenderForcesTheBrowserAndKeepsTheResultCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Actions []struct {
				Type         string `json:"type"`
				Milliseconds int    `json:"milliseconds"`
			} `json:"actions"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Actions) != 1 || body.Actions[0].Type != "wait" || body.Actions[0].Milliseconds != 1 {
			t.Error("render=true did not require browser execution")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"markdown": strings.Repeat("Page content ", 1000)}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	cfg := configFor(t)
	signInWebAccount(t, cfg, server.URL)
	cfg.Tools.ResultMaxChars = 1000
	fetch := builtCloudTool(t, cfg, "web_fetch")
	info, err := fetch.Info(context.Background())
	if err != nil || info.Name != "web_fetch" {
		t.Fatal("cloud execution changed the fetch function identity")
	}
	parameters, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || parameters.Properties.Len() != 2 || !reflect.DeepEqual(parameters.Required, []string{"url"}) {
		t.Fatal("cloud execution changed the fetch function schema")
	}
	out, err := fetch.InvokableRun(context.Background(), `{"url":"https://page.invalid/article","render":true}`)
	if err != nil || !strings.HasPrefix(out, "Page content ") || !strings.Contains(out, "truncated") || len(out) >= 12000 {
		t.Fatalf("cloud page bypassed the configured result cap: length=%d error=%v", len(out), err)
	}
}

func TestSignedInWebFetchUsesCloudWithoutCallingLocalTarget(t *testing.T) {
	var cloud, local atomic.Int32
	var expectedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/desktop/v1/web/fetch" {
			local.Add(1)
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><title>Local</title><body><article>Local target content must not be requested after account sign-in.</article></body></html>"))
			return
		}
		cloud.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+expectedToken {
			t.Error("cloud request did not use the dedicated account route and credential")
		}
		var body struct {
			URL     string   `json:"url"`
			Formats []string `json:"formats"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !strings.HasSuffix(body.URL, "/local-page") || len(body.Formats) != 1 || body.Formats[0] != "markdown" {
			t.Error("cloud fetch did not preserve the target URL and Markdown format")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"markdown":"Cloud account result"}}`))
	}))
	defer server.Close()
	cfg := configFor(t)
	expectedToken = signInWebAccount(t, cfg, server.URL)
	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"url": server.URL + "/local-page"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := find(t, set, "web_fetch").InvokableRun(context.Background(), string(args))
	if err != nil || out != "Cloud account result" || cloud.Load() != 1 || local.Load() != 0 {
		t.Fatalf("signed-in fetch did not stay in the cloud: output=%q error=%v cloud=%d local=%d", out, err, cloud.Load(), local.Load())
	}
}
