package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
	"github.com/cloudwego/eino/components/model"
)

// Responses must recover in the real factory too, retaining the successful
// probe's output rather than making another request once the route is healthy.
func TestResponsesRouteRecoveryRetainsItsOriginalRequestAndSuccessfulResponse(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%v", streaming), func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model           string `json:"model"`
					Stream          bool   `json:"stream"`
					MaxOutputTokens int    `json:"max_output_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if r.URL.Path != "/v1/responses" || body.Model != "route-model" || body.Stream != streaming || body.MaxOutputTokens != 321 {
					t.Errorf("recovery changed request protocol/options: path=%s body=%+v", r.URL.Path, body)
				}
				if requests.Add(1) <= 4 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, routeUnavailableJSON)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", responsesTimeoutFixture)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, responsesTimeoutFixture)
				}
			}))
			t.Cleanup(endpoint.Close)
			clock := timingtest.New(time.Now())
			cfg := config.Default()
			cfg.Models.Default = "fixture"
			cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: endpoint.URL + "/v1", Model: "route-model", APIMode: config.APIModeResponses}}
			cfg.Swarm.MaxCompletionTokens = 321
			pool := NewWithBackoffClock(cfg, clock)
			records := make(chan CallRecord, 32)
			build, err := pool.ModelBuilder(t.Context(), "fixture", "", "", func(r CallRecord) { records <- r })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan protocolRouteResult, 1)
			go func() { done <- callProtocolRoute(ctx, build("manager", "response-manager"), streaming) }()
			failedCalls := 0
			for attempt, delay := range []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second} {
				for {
					record := nextProtocolRouteRecord(t, records)
					if record.Backoff == nil {
						failedCalls++
						if HTTPStatus(record.Err) != http.StatusServiceUnavailable || record.OutputChars != 0 || record.AgentID != "response-manager" {
							t.Fatalf("routing refusal lost its request receipt: %+v", record)
						}
						continue
					}
					if record.Backoff.Attempt != attempt+1 || record.Backoff.WaitMS != delay.Milliseconds() || requests.Load() != int32(attempt+1) || !pool.RouteUnavailable("") {
						t.Fatalf("Responses bypassed routing recovery: record=%+v requests=%d", record, requests.Load())
					}
					break
				}
				if !clock.AdvanceNext() {
					t.Fatal("Responses route wait has no active timer")
				}
			}
			select {
			case result := <-done:
				if result.err != nil || result.text != "ready" || requests.Load() != 5 || failedCalls != 4 || pool.RouteUnavailable("") {
					t.Fatalf("Responses lost successful probe or re-sent it: result=%+v requests=%d failed=%d", result, requests.Load(), failedCalls)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Responses did not resume from the successful route probe")
			}
			if record := nextProtocolRouteRecord(t, records); record.Backoff != nil || record.Err != nil || record.OutputChars != len("ready") {
				t.Fatalf("successful Responses probe lost its call receipt: %+v", record)
			}
		})
	}
}

// Private classifiers own their retry policy. Even the exact route rejection
// must neither wait for nor mutate the origin's existing recovery state.
func TestIsolatedRouteRejectionSendsOnceWithoutWaitingOrChangingOriginHealth(t *testing.T) {
	for _, mode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		for _, streaming := range []bool{false, true} {
			for _, cooling := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream_%v/cooling_%v", mode, streaming, cooling), func(t *testing.T) {
					var requests atomic.Int32
					endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, routeUnavailableJSON)
					}))
					t.Cleanup(endpoint.Close)
					clock := timingtest.New(time.Now())
					pool := backoffPool(endpoint.URL)
					pool.backoffClock = clock
					pool.cfg.Models.Providers[0].APIMode = mode
					m, err := pool.Get(t.Context(), "fixture", "")
					if err != nil {
						t.Fatal(err)
					}
					gate := pool.backoffs[backoffKey(endpoint.URL)]
					if cooling {
						seedCtx, cancelSeed := context.WithCancel(t.Context())
						build, err := pool.ModelBuilder(seedCtx, "fixture", "", "", func(record CallRecord) {
							if record.Backoff != nil {
								cancelSeed()
							}
						})
						if err != nil {
							t.Fatal(err)
						}
						_, err = build("manager", "seed-manager").Generate(seedCtx, backoffMessages())
						cancelSeed()
						if !errors.Is(err, context.Canceled) || !pool.RouteUnavailable("") {
							t.Fatalf("actual routing failure did not seed shared recovery: %v", err)
						}
					}
					gate.mu.Lock()
					beforeFailures, beforeGeneration, beforeNext, beforeRoute := gate.failures, gate.generation, gate.next, gate.routeUnavailable
					gate.mu.Unlock()
					beforeRequests := requests.Load()
					ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
					defer cancel()
					result := callProtocolRoute(WithIsolatedRecovery(ctx), m, streaming)
					if HTTPStatus(result.err) != http.StatusServiceUnavailable || errors.Is(result.err, context.DeadlineExceeded) || requests.Load() != beforeRequests+1 {
						t.Fatalf("isolated routing refusal waited or retried: result=%+v requests=%d", result, requests.Load())
					}
					gate.mu.Lock()
					unchanged := gate.failures == beforeFailures && gate.generation == beforeGeneration && gate.next == beforeNext && gate.routeUnavailable == beforeRoute && !gate.probe
					gate.mu.Unlock()
					if !unchanged || clock.AdvanceNext() {
						t.Fatal("isolated routing refusal changed shared state or registered a timer")
					}
				})
			}
		}
	}
}

func TestStoppedRouteRecoverySurvivesAliasAndInvalidationUntilCompleteSuccess(t *testing.T) {
	var requests atomic.Int32
	var healthy atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, routeUnavailableJSON)
			return
		}
		_, _ = io.WriteString(w, backoffCompletion)
	}))
	t.Cleanup(endpoint.Close)
	clock := timingtest.New(time.Now())
	pool := backoffPool(endpoint.URL)
	pool.backoffClock = clock
	pool.cfg.Models.Providers = append(pool.cfg.Models.Providers, config.Provider{ID: "alias", BaseURL: endpoint.URL + "/v1", Model: "other-model"})
	ctx, cancel := context.WithCancel(t.Context())
	build, err := pool.ModelBuilder(ctx, "fixture", "", "", func(record CallRecord) {
		if record.Backoff != nil {
			cancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = build("manager", "stopped-manager").Generate(ctx, backoffMessages())
	cancel()
	if !errors.Is(err, context.Canceled) || requests.Load() != 1 || !pool.RouteUnavailable("") || !pool.RouteUnavailable("alias") {
		t.Fatalf("stopped routing recovery lost its shared observation: err=%v requests=%d", err, requests.Load())
	}
	pool.Invalidate()
	if !pool.RouteUnavailable("alias") {
		t.Fatal("client invalidation erased the origin's routing outage")
	}
	healthy.Store(true)
	records := make(chan CallRecord, 4)
	alias, err := pool.ModelBuilder(t.Context(), "alias", "", "", func(record CallRecord) { records <- record })
	if err != nil {
		t.Fatal(err)
	}
	recoveryCtx, cancelRecovery := context.WithCancel(t.Context())
	defer cancelRecovery()
	done := make(chan protocolRouteResult, 1)
	go func() { done <- callProtocolRoute(recoveryCtx, alias("worker", "replacement-worker"), false) }()
	if record := nextProtocolRouteRecord(t, records); record.Backoff == nil || record.Backoff.Attempt != 1 || requests.Load() != 1 {
		t.Fatalf("alias bypassed retained routing cooldown: %+v requests=%d", record, requests.Load())
	}
	if !clock.AdvanceNext() {
		t.Fatal("alias recovery has no active timer")
	}
	select {
	case result := <-done:
		if result.err != nil || result.text != "ready" || pool.RouteUnavailable("") || pool.RouteUnavailable("alias") || requests.Load() != 2 {
			t.Fatalf("complete alias success did not clear the shared route outage: result=%+v requests=%d", result, requests.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("alias did not finish the successful probe")
	}
}

// Two real HTTP callers share the same half-open permit after a routing
// outage. A blocked probe cannot turn into one request per waiting model.
func TestRouteRecoveryAdmitsOnlyOneRealHTTPProbePerOrigin(t *testing.T) {
	var requests atomic.Int32
	probeStarted, releaseProbe := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, routeUnavailableJSON)
			return
		case 2:
			close(probeStarted)
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, backoffCompletion)
	}))
	t.Cleanup(endpoint.Close)
	clock := timingtest.New(time.Now())
	pool := backoffPool(endpoint.URL)
	pool.backoffClock = clock
	seedCtx, cancelSeed := context.WithCancel(t.Context())
	seed, err := pool.ModelBuilder(seedCtx, "fixture", "", "", func(record CallRecord) {
		if record.Backoff != nil {
			cancelSeed()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = seed("manager", "seed-manager").Generate(seedCtx, backoffMessages())
	cancelSeed()
	if !errors.Is(err, context.Canceled) || !pool.RouteUnavailable("") {
		t.Fatalf("fixture did not establish the actual routing outage: %v", err)
	}
	records := make(chan CallRecord, 12)
	build, err := pool.ModelBuilder(t.Context(), "fixture", "", "", func(record CallRecord) { records <- record })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan protocolRouteResult, 2)
	for _, agent := range []string{"peer-one", "peer-two"} {
		m := build("worker", agent)
		go func() { done <- callProtocolRoute(ctx, m, false) }()
	}
	for range 2 {
		if record := nextProtocolRouteRecord(t, records); record.Backoff == nil || record.Backoff.Attempt != 1 {
			t.Fatalf("peer escaped the shared cooldown: %+v", record)
		}
	}
	if requests.Load() != 1 || !clock.AdvanceNext() {
		t.Fatal("peers bypassed the actual shared recovery timer")
	}
	select {
	case <-probeStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("no peer acquired the due HTTP probe")
	}
	select {
	case result := <-done:
		t.Fatalf("caller escaped while the only probe was blocked: %+v", result)
	case <-time.After(30 * time.Millisecond):
	}
	if requests.Load() != 2 || !pool.RouteUnavailable("") {
		t.Fatalf("multiple peers probed or incomplete output restored health: requests=%d", requests.Load())
	}
	close(releaseProbe)
	for range 2 {
		select {
		case result := <-done:
			if result.err != nil || result.text != "ready" {
				t.Fatalf("complete probe did not release the waiting peer: %+v", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shared routing recovery did not release both callers")
		}
	}
	if requests.Load() != 3 || pool.RouteUnavailable("") {
		t.Fatalf("probe completion did not settle the shared origin: requests=%d", requests.Load())
	}
}

type protocolRouteResult struct {
	text string
	err  error
}

func callProtocolRoute(ctx context.Context, m model.BaseChatModel, streaming bool) protocolRouteResult {
	if !streaming {
		out, err := m.Generate(ctx, backoffMessages())
		if out == nil {
			return protocolRouteResult{err: err}
		}
		return protocolRouteResult{text: out.Content, err: err}
	}
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		return protocolRouteResult{err: err}
	}
	defer stream.Close()
	var text strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return protocolRouteResult{text: text.String()}
		}
		if err != nil {
			return protocolRouteResult{text: text.String(), err: err}
		}
		if msg != nil {
			text.WriteString(msg.Content)
		}
	}
}

func nextProtocolRouteRecord(t *testing.T, records <-chan CallRecord) CallRecord {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(3 * time.Second):
		t.Fatal("model recovery did not record its actual request or wait")
		return CallRecord{}
	}
}
