package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestWebExecutionSelectionRespectsAccountAndExplicitLocalChoice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    string
		signed  bool
		legacy  bool
		foreign bool
	}{
		{name: "anonymous auto remains local", mode: "auto"},
		{name: "old missing mode remains local"},
		{name: "explicit local survives sign-in", mode: "local", signed: true},
		{name: "legacy mode is not an account", mode: "auto", signed: true, legacy: true},
		{name: "other server receipt is not a current account", mode: "auto", signed: true, foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var local, cloud atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/page" {
					cloud.Add(1)
				}
				local.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("account token leaked to the local page target")
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html><title>Page</title><body><article>Local result from this device.</article></body></html>"))
			}))
			defer server.Close()
			cfg := configFor(t)
			if tc.signed {
				signInWebAccount(t, cfg, server.URL)
			}
			cfg.Tools.WebExecution = tc.mode
			if tc.legacy {
				cfg.Remote.AuthMode = config.RemoteAuthIndependent
			}
			if tc.foreign {
				cfg.Remote.HubURL = "https://another-gateway.invalid"
			}
			fetch := builtCloudTool(t, cfg, "web_fetch")
			args, err := json.Marshal(map[string]any{"url": server.URL + "/page"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := fetch.InvokableRun(context.Background(), string(args))
			if err != nil || !strings.Contains(out, "Local result") || local.Load() != 1 || cloud.Load() != 0 {
				t.Fatalf("local selection changed execution: output=%q error=%v local=%d cloud=%d", out, err, local.Load(), cloud.Load())
			}
		})
	}
}

func TestWebToolsReadTheCurrentAccountAtInvocation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"data":{"markdown":"Current cloud account"}}`))
	}))
	defer server.Close()
	cfg := configFor(t)
	fetch := builtCloudTool(t, cfg, "web_fetch")
	signInWebAccount(t, cfg, server.URL)
	out, err := fetch.InvokableRun(context.Background(), `{"url":"https://page.invalid/article"}`)
	if err != nil || out != "Current cloud account" || calls.Load() != 1 {
		t.Fatalf("an already-built toolset missed account sign-in: %q, %v", out, err)
	}
	if err := cfg.Update(func(next *config.Config) error { next.Tools.WebExecution = config.WebExecutionCloud; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteHostToken(strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	out, err = fetch.InvokableRun(context.Background(), `{"url":"https://page.invalid/article"}`)
	if err != nil || !strings.Contains(out, "sign in to the server again") || calls.Load() != 1 {
		t.Fatal("cloud execution reused an account after its receipt was replaced")
	}
}

func TestCloudRoutingRefusesMissingOrUnreadableAuthority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, *config.Config)
		want    string
	}{
		{name: "signed out cloud", want: "sign in to the server again"},
		{name: "unapproved receipt", prepare: func(_ *testing.T, cfg *config.Config) { cfg.Remote.AuthMode = config.RemoteAuthAccount }, want: "sign in to the server again"},
		{name: "invalid setting", prepare: func(_ *testing.T, cfg *config.Config) { cfg.Tools.WebExecution = "unknown" }, want: "invalid execution setting"},
		{name: "invalid origin", prepare: func(_ *testing.T, cfg *config.Config) {
			cfg.Remote.AuthMode = config.RemoteAuthAccount
			cfg.Remote.HubURL = "https://gateway.invalid/path"
		}, want: "invalid server origin"},
		{name: "corrupt private receipt", prepare: func(t *testing.T, cfg *config.Config) {
			cfg.Remote.AuthMode = config.RemoteAuthAccount
			if err := os.WriteFile(filepath.Join(cfg.RemoteDir(), "host_token"), []byte(`{"schema":9,"token":"private-value"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "private account credential is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configFor(t)
			cfg.Tools.WebExecution = config.WebExecutionCloud
			if tc.prepare != nil {
				tc.prepare(t, cfg)
			}
			out, err := builtCloudTool(t, cfg, "web_fetch").InvokableRun(context.Background(), `{"url":"https://must-not-be-contacted.invalid"}`)
			if err != nil || !strings.Contains(out, tc.want) || strings.Contains(out, "private-value") {
				t.Fatalf("invalid authority did not fail closed: %q, %v", out, err)
			}
		})
	}
}

func TestDisabledCloudWebToolsRemainAbsent(t *testing.T) {
	cfg := configFor(t)
	cfg.Tools.WebExecution = config.WebExecutionCloud
	cfg.Tools.Disabled = []string{"web_search", "web_fetch"}
	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "workspace"))
	if err != nil || containsStr(set.Names, "web_search") || containsStr(set.Names, "web_fetch") {
		t.Fatal("cloud routing bypassed the existing tool switches")
	}
}

func TestTurnWebExecutionBuilderRejectsUnknownOverrides(t *testing.T) {
	cfg := configFor(t)
	workspace := filepath.Join(t.TempDir(), "must-not-be-created")
	if _, err := BuildForWebExecution(context.Background(), cfg, workspace, "unknown"); err == nil {
		t.Fatal("unknown per-turn execution override was accepted")
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("invalid override created a workspace before validation")
	}
}
