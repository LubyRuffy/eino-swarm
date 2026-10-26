package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// A classifier's private recovery cannot claim the origin's probe or delay a
// different model while ordinary streamed work on that service remains healthy.
func TestIsolatedClassifierHeaderTimeoutDoesNotBlockOtherModels(t *testing.T) {
	for _, mode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			var classificationRequests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if !body.Stream {
					if classificationRequests.Add(1) == 1 {
						close(started)
					}
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == config.APIModeResponses {
					fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", responsesTimeoutFixture)
				} else {
					fmt.Fprint(w, backoffChunk, "data: [DONE]\n\n")
				}
			}))
			t.Cleanup(endpoint.Close)
			pool := backoffPool(endpoint.URL)
			pool.cfg.Models.Providers[0].FirstByteTimeoutSeconds = 1
			pool.cfg.Models.Providers[0].APIMode = mode
			var records []CallRecord
			var mu sync.Mutex
			recorder := func(record CallRecord) {
				mu.Lock()
				records = append(records, record)
				mu.Unlock()
			}
			classify, err := pool.ModelBuilder(context.Background(), "fixture", "policy-model", "", recorder)
			if err != nil {
				t.Fatal(err)
			}
			work, err := pool.ModelBuilder(context.Background(), "fixture", "work-model", "", recorder)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			classificationDone := make(chan error, 1)
			go func() {
				_, err := classify("policy", "classifier").Generate(WithIsolatedRecovery(ctx), backoffMessages())
				classificationDone <- err
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("classifier did not reach the HTTP provider")
			}
			for stage := range 2 {
				callCtx, callCancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
				stream, err := work("manager", "other-conversation").Stream(callCtx, backoffMessages())
				if err == nil {
					err = finishBackoffStream(stream)
					stream.Close()
				}
				callCancel()
				if err != nil {
					t.Fatalf("healthy model was blocked at stage %d: %v", stage, err)
				}
				if stage == 0 {
					err = <-classificationDone
					var timeout FirstByteTimeoutError
					if !errors.As(err, &timeout) || timeout.Wait != time.Second {
						t.Fatalf("classification did not retain its one full header budget: %v", err)
					}
				}
			}
			if classificationRequests.Load() != 1 {
				t.Fatalf("private recovery received %d hidden HTTP attempts", classificationRequests.Load())
			}
			mu.Lock()
			defer mu.Unlock()
			if len(records) != 3 {
				t.Fatalf("expected actual classifier and two work calls, records=%+v", records)
			}
			for _, record := range records {
				if record.Backoff != nil {
					t.Fatalf("classification changed shared recovery: %+v", record)
				}
			}
		})
	}
}

func TestIsolatedClassifierFailuresCannotStartOrRecoverOriginCooldown(t *testing.T) {
	for _, failure := range []string{"connection", "503", "invalid envelope", "invalid claims", "success"} {
		t.Run(failure, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				switch failure {
				case "connection":
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				case "503":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "invalid envelope":
					fmt.Fprint(w, "{invalid")
				case "invalid claims":
					fmt.Fprint(w, backoffCompletion)
				default:
					fmt.Fprint(w, backoffCompletion)
				}
			}))
			t.Cleanup(endpoint.Close)
			pool := backoffPool(endpoint.URL)
			m, err := pool.Get(context.Background(), "fixture", "")
			if err != nil {
				t.Fatal(err)
			}
			gate := pool.backoffs[backoffKey(endpoint.URL)]
			for _, cooling := range []bool{false, true} {
				if cooling {
					mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
				}
				gate.mu.Lock()
				failures, generation, next := gate.failures, gate.generation, gate.next
				gate.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
				_, err := m.Generate(WithIsolatedRecovery(ctx), backoffMessages())
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("private candidate waited for shared cooldown: %v", err)
				}
				if wantSuccess := failure == "success" || failure == "invalid claims"; (err == nil) != wantSuccess {
					t.Fatalf("fixture failure %q: err=%v", failure, err)
				}
				gate.mu.Lock()
				unchanged := failures == gate.failures && generation == gate.generation && next == gate.next && !gate.probe
				gate.mu.Unlock()
				if !unchanged {
					t.Fatal("private classifier changed shared origin health")
				}
			}
		})
	}
}

func TestIsolatedClassifierStopCancelsItsActualHeaderWait(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	t.Cleanup(endpoint.Close)
	pool := backoffPool(endpoint.URL)
	m, err := pool.Get(context.Background(), "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := m.Generate(WithIsolatedRecovery(ctx), backoffMessages())
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("classifier did not enter the actual HTTP request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop lost its cancellation cause: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the classifier")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop returned without canceling the provider request")
	}
}
