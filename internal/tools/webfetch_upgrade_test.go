package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-tools/webfetch"
	"github.com/cloudwego/eino/components/tool"
)

// Pin the dependency through the toolset the model actually sees. Constructing
// an upstream tool directly would miss a stale pin or a host schema wrapper.
func TestBuiltWebFetchAdvertisesItsAutomaticRenderUpgrade(t *testing.T) {
	fetch := buildWebFetchForTest(t, "")
	info, err := fetch.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	props, required := advertised(t, fetch)
	url, urlOK := props["url"].(map[string]any)
	render, renderOK := props["render"].(map[string]any)
	if !urlOK || !renderOK || len(props) != 2 || url["type"] != "string" || render["type"] != "boolean" {
		t.Fatalf("web_fetch schema changed: %v", props)
	}
	if len(required) != 1 || required[0] != "url" {
		t.Fatalf("URL must remain required and render optional: %v", required)
	}
	for _, description := range []string{info.Desc, fmt.Sprint(render["description"])} {
		lower := strings.ToLower(description)
		for _, fragment := range []string{"http", "bot wall", "once", "direct"} {
			if !strings.Contains(lower, fragment) {
				t.Fatalf("model cannot discover %q in web_fetch description %q", fragment, description)
			}
		}
	}
}

// A loopback proxy supplies every response, so this exercises Build's actual
// proxy configuration without reaching an origin or launching a browser.
func TestBuiltWebFetchUsesItsProxyAndKeepsOrdinaryHTTPFailuresRecoverable(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if !r.URL.IsAbs() || r.URL.Host != "web-fetch.invalid" || r.URL.Path != "/article" {
					t.Errorf("proxy received unexpected target: %s", r.URL)
				}
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("<html><body><article><h1>Local report</h1><p>Readable local fixture content is preserved for the model.</p></article></body></html>"))
			}))
			defer proxy.Close()
			fetch := buildWebFetchForTest(t, proxy.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := fetch.InvokableRun(ctx, webFetchArguments(t, "http://web-fetch.invalid/article"))
			if err != nil {
				t.Fatalf("HTTP failure must be model feedback, not a turn-ending Go error: %v", err)
			}
			if status == http.StatusOK {
				if !strings.Contains(out, "Readable local fixture content") || strings.HasPrefix(out, "error:") {
					t.Fatalf("ordinary page lost its readable content: %q", out)
				}
			} else if !strings.HasPrefix(out, "error:") || !strings.Contains(out, fmt.Sprintf("HTTP error: %d", status)) || strings.Contains(out, "browser rendering") {
				t.Fatalf("ordinary rejection changed its failure semantics: %q", out)
			}
			if requests.Load() != 1 {
				t.Fatalf("want one HTTP request, got %d", requests.Load())
			}
		})
	}
}

// Keep the real HTTP adapter and both host wrappers, substituting only the
// public rendering hook so these pin regressions never start Chromium.
func TestWrappedWebFetchUpgradesOnceAndCapsTheRenderedResult(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests, renders atomic.Int32
			server := webFetchBotWallServer(t, status, &requests)
			fetch := wrappedWebFetchForTest(t, webfetch.Config{
				HTTPClient: server.Client(),
				RenderFetcher: func(_ context.Context, rawURL string) (string, error) {
					renders.Add(1)
					if rawURL != server.URL {
						t.Errorf("render target changed: %q", rawURL)
					}
					return "RENDER-FIRST " + strings.Repeat("正文", 300) + " RENDER-LAST", nil
				},
			}, 96)
			out, err := fetch.InvokableRun(context.Background(), webFetchArguments(t, server.URL))
			if err != nil || !strings.HasPrefix(out, "RENDER-FIRST") || !strings.HasSuffix(out, "RENDER-LAST") || !strings.Contains(out, capNotice) {
				t.Fatalf("rendered result did not cross the host cap intact: out=%q err=%v", out, err)
			}
			if requests.Load() != 1 || renders.Load() != 1 {
				t.Fatalf("HTTP/render attempts = %d/%d, want 1/1", requests.Load(), renders.Load())
			}
		})
	}
}

func TestWrappedWebFetchDoesNotReturnPartialOrChallengeContentAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		err  error
	}{
		{"render_failure", "PARTIAL-MUST-NOT-REACH-MODEL", errors.New("renderer unavailable")},
		{"still_challenged", "[Verify](https://geo.captcha-delivery.com/captcha/?id=fixture)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests, renders atomic.Int32
			server := webFetchBotWallServer(t, http.StatusForbidden, &requests)
			fetch := wrappedWebFetchForTest(t, webfetch.Config{
				HTTPClient: server.Client(),
				RenderFetcher: func(context.Context, string) (string, error) {
					renders.Add(1)
					return tc.body, tc.err
				},
			}, 4096)
			out, err := fetch.InvokableRun(context.Background(), webFetchArguments(t, server.URL))
			if err != nil || !strings.HasPrefix(out, "error:") || !strings.Contains(out, "bot wall") || !strings.Contains(out, "attempted once") {
				t.Fatalf("failed upgrade lost recoverable failure context: out=%q err=%v", out, err)
			}
			if strings.Contains(out, tc.body) || (tc.err != nil && !strings.Contains(out, tc.err.Error())) {
				t.Fatalf("partial/challenge content leaked or render cause was lost: %q", out)
			}
			if requests.Load() != 1 || renders.Load() != 1 {
				t.Fatalf("failed upgrade retried: HTTP/render = %d/%d", requests.Load(), renders.Load())
			}
		})
	}
}

// The tool's public rendering hook must receive the caller's cancellation,
// including through host wrappers; this does not accept Rod's own lifecycle.
func TestWrappedWebFetchPassesCancellationToItsSingleRenderAttempt(t *testing.T) {
	var requests, renders atomic.Int32
	server := webFetchBotWallServer(t, http.StatusUnauthorized, &requests)
	started := make(chan context.Context, 1)
	fetch := wrappedWebFetchForTest(t, webfetch.Config{
		HTTPClient: server.Client(),
		RenderFetcher: func(ctx context.Context, _ string) (string, error) {
			renders.Add(1)
			started <- ctx
			<-ctx.Done()
			return "CANCELLED-PARTIAL", context.Cause(ctx)
		},
	}, 4096)
	cause := errors.New("owner stopped the fetch")
	ctx, cancel := context.WithCancelCause(context.Background())
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	joined := make(chan struct{})
	args := webFetchArguments(t, server.URL)
	go func() {
		defer close(joined)
		out, err := fetch.InvokableRun(ctx, args)
		done <- result{out, err}
	}()
	t.Cleanup(func() {
		cancel(cause)
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("cancelled web_fetch did not join")
		}
	})
	select {
	case received := <-started:
		if received != ctx {
			t.Fatal("render did not receive the original caller context")
		}
	case got := <-done:
		t.Fatalf("fetch returned before attempting render: out=%q err=%v", got.out, got.err)
	case <-time.After(5 * time.Second):
		t.Fatal("render did not start")
	}
	cancel(cause)
	select {
	case got := <-done:
		if got.err != nil || !strings.HasPrefix(got.out, "error:") || !strings.Contains(got.out, cause.Error()) || !strings.Contains(got.out, "attempted once") || strings.Contains(got.out, "CANCELLED-PARTIAL") {
			t.Fatalf("cancellation did not return recoverable feedback: out=%q err=%v", got.out, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render ignored cancellation")
	}
	if requests.Load() != 1 || renders.Load() != 1 {
		t.Fatalf("cancelled fetch retried: HTTP/render = %d/%d", requests.Load(), renders.Load())
	}
}

func buildWebFetchForTest(t *testing.T, proxy string) tool.InvokableTool {
	t.Helper()
	cfg := configFor(t)
	for _, descriptor := range Catalog() {
		if descriptor.Name != webfetch.ToolName {
			cfg.Tools.Disabled = append(cfg.Tools.Disabled, descriptor.Name)
		}
	}
	cfg.Tools.Proxy = config.ProxyConfig{HTTP: proxy, HTTPS: proxy}
	set, err := Build(context.Background(), cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Tools) != 1 {
		t.Fatalf("web_fetch fixture built other tools: %v", set.Names)
	}
	return find(t, set, webfetch.ToolName)
}

func wrappedWebFetchForTest(t *testing.T, cfg webfetch.Config, limit int) tool.InvokableTool {
	t.Helper()
	inner, err := webfetch.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return wrapResultCap(wrapAdvertisedParams(inner), limit).(tool.InvokableTool)
}

func webFetchBotWallServer(t *testing.T, status int, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("X-DataDome", "protected")
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func webFetchArguments(t *testing.T, rawURL string) string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"url": rawURL})
	if err != nil {
		t.Fatal(err)
	}
	return string(args)
}
