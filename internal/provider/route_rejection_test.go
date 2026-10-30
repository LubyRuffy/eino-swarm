package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// The routing policy needs a complete, prompt, structured rejection. A shared
// message fragment, late body or a failed body read cannot prove no upstream ran.
func TestOnlyCompletePromptRoutingRefusalsUseSeparateRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		status    int
		delay     time.Duration
		want      bool
	}{
		{name: "route refusal", raw: routeUnavailableJSON, status: 503, want: true},
		{name: "another status", raw: routeUnavailableJSON, status: 500},
		{name: "missing code", raw: `{"error":{"message":"no healthy provider can serve this model"}}`, status: 503},
		{name: "another code", raw: `{"error":{"message":"no healthy provider can serve this model","code":"upstream_error"}}`, status: 503},
		{name: "another message", raw: `{"error":{"message":"the selected provider configuration is unavailable","code":"provider_unavailable"}}`, status: 503},
		{name: "message fragment", raw: `{"error":{"message":"upstream said no healthy provider can serve this model","code":"provider_unavailable"}}`, status: 503},
		{name: "malformed", raw: routeUnavailableJSON + "truncated", status: 503},
		{name: "large envelope", raw: routeUnavailableJSON + strings.Repeat(" ", 4096), status: 503},
		{name: "late rejection body", raw: routeUnavailableJSON, status: 503, delay: 160 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.(http.Flusher).Flush()
				if tc.delay > 0 {
					timer := time.NewTimer(tc.delay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return
					}
				}
				_, _ = io.WriteString(w, tc.raw)
			}))
			defer endpoint.Close()
			ctx, observation := observeRouteRejection(t.Context())
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader("payload"))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := chatHTTPClient(time.Second, 80*time.Millisecond).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil || string(raw) != tc.raw {
				t.Fatalf("observing a refusal changed its original response: raw=%q err=%v", raw, err)
			}
			original := errors.New("original SDK error")
			wrapped := observation.wrap(original)
			if isRouteUnavailable(wrapped) != tc.want || !errors.Is(wrapped, original) || observation.wrap(nil) != nil {
				t.Fatalf("wrong rejection classification: confirmed=%v want=%v cause=%v", isRouteUnavailable(wrapped), tc.want, wrapped)
			}
		})
	}
}

func TestIncompleteRoutingBodyNeverProvesAnUnsentModelRequest(t *testing.T) {
	observation := &routeRejection{}
	broken := &backoffBrokenBody{err: io.ErrUnexpectedEOF}
	body := &routeRejectionBody{ReadCloser: broken, observation: observation, deadline: time.Now().Add(time.Second)}
	if _, err := io.ReadAll(body); !errors.Is(err, io.ErrUnexpectedEOF) || observation.confirmed.Load() {
		t.Fatalf("incomplete body was accepted: err=%v", err)
	}
	if err := body.Close(); err != nil || !broken.closed {
		t.Fatalf("rejection observer lost body ownership: %v", err)
	}
	pool := backoffPool("http://unused.invalid")
	if pool.RouteUnavailable("") {
		t.Fatal("a fresh pool invented a routing outage")
	}
}

func TestRoutingRetryRespectsIsolationAndCallerCancellation(t *testing.T) {
	refusal := &routeUnavailableError{errors.New("refused")}
	r := &recordingModel{gate: newBackoffGate(time.Second, time.Second)}
	if !r.retryRoute(t.Context(), refusal) || r.retryRoute(WithIsolatedRecovery(t.Context()), refusal) || r.retryRoute(WithoutRouteRecovery(t.Context()), refusal) ||
		r.retryRoute(t.Context(), errors.New("ordinary error")) || (&recordingModel{}).retryRoute(t.Context(), refusal) {
		t.Fatal("routing retry escaped its explicit boundary")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if r.retryRoute(ctx, refusal) {
		t.Fatal("routing retry ignored Stop")
	}
}

// Stop can land after the complete refusal was read but before the adapter
// returns it. That cancellation must not erase the observed outage and allow
// optional post-turn calls to send into the same failed route.
func TestStopAfterCompleteRoutingRefusalRetainsTheObservedOutage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner, err := newResponsesModel(config.Provider{ID: "fixture", BaseURL: "http://fixture.invalid", Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	inner.client.Transport = &idleTransport{firstByte: time.Second, idle: time.Second, base: responsesRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Body: &routeCancelOnClose{ReadCloser: io.NopCloser(strings.NewReader(routeUnavailableJSON)), cancel: cancel}, Header: make(http.Header)}, nil
	})}
	gate := newBackoffGate(time.Second, time.Second)
	r := &recordingModel{inner: inner, gate: gate}
	_, err = r.Generate(ctx, backoffMessages())
	if !isRouteUnavailable(err) || ctx.Err() != context.Canceled {
		t.Fatalf("fixture did not Stop between refusal read and model return: err=%v context=%v", err, ctx.Err())
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if !gate.routeUnavailable || gate.failures != 1 {
		t.Fatalf("Stop erased an already observed refusal: unavailable=%v failures=%d", gate.routeUnavailable, gate.failures)
	}
}

type routeCancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *routeCancelOnClose) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}
