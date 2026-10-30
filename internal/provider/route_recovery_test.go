package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
)

const routeUnavailableJSON = `{"error":{"message":"no healthy provider can serve this model","type":"provider_unavailable","code":"provider_unavailable"}}`

// A routing rejection never reached a model. Keep one generation alive across
// an outage longer than the manager's two ordinary model-error retries.
func TestRouteRejectionsWaitForRecoveryWithinTheSameModelCall(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "generate"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) <= 4 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, routeUnavailableJSON)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, backoffChunk+"data: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, backoffCompletion)
				}
			}))
			defer endpoint.Close()
			pool := backoffPool(endpoint.URL)
			clock := timingtest.New(time.Now())
			pool.backoffClock = clock
			records := make(chan CallRecord, 16)
			build, err := pool.ModelBuilder(t.Context(), "fixture", "", "", func(rec CallRecord) { records <- rec })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				m := build("manager", "manager")
				if streaming {
					stream, err := m.Stream(ctx, backoffMessages())
					if err == nil {
						err = finishBackoffStream(stream)
						stream.Close()
					}
					done <- err
				} else {
					out, err := m.Generate(ctx, backoffMessages())
					if err == nil && (out == nil || out.Content != "ready") {
						err = errors.New("the successful probe response was lost")
					}
					done <- err
				}
			}()
			defer cancel()
			calls := 0
			for i, delay := range []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second} {
				for {
					select {
					case rec := <-records:
						if rec.Backoff == nil {
							calls++
							if HTTPStatus(rec.Err) != http.StatusServiceUnavailable || rec.Duration > time.Second {
								t.Fatalf("routing request lost its actual cause/duration: %+v", rec)
							}
							continue
						}
						if rec.Backoff.Attempt != i+1 || rec.Backoff.WaitMS != delay.Milliseconds() || requests.Load() != int32(i+1) {
							t.Fatalf("bad recovery wait: %+v requests=%d", rec.Backoff, requests.Load())
						}
						if !pool.RouteUnavailable("") || pool.RouteUnavailable("missing-provider") {
							t.Fatal("routing outage lost its origin identity")
						}
					case err := <-done:
						t.Fatalf("routing outage escaped the live model call after %d requests: %v", requests.Load(), err)
					case <-time.After(3 * time.Second):
						t.Fatal("routing outage did not register its next cancellable wait")
					}
					break
				}
				clock.Advance(delay)
			}
			select {
			case err := <-done:
				if err != nil || requests.Load() != 5 || pool.RouteUnavailable("") {
					t.Fatalf("same-call recovery: err=%v requests=%d", err, requests.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("successful probe did not resume the original call")
			}
			if calls != 4 {
				t.Fatalf("routing refusals were not independently recorded: %d", calls)
			}
			rec := <-records
			if rec.Backoff != nil || rec.Err != nil || rec.AgentID != "manager" {
				t.Fatalf("successful probe lost telemetry: %+v", rec)
			}
		})
	}
}
