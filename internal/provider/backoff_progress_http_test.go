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
)

// Opening the gate on content cannot hide a later idle timeout, and closing it
// again must leave already admitted requests running on their original contexts.
func TestModelBackoffProbeIdleAfterContentRenewsCooldownWithoutCancelingParallelWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var requests atomic.Int32
	probeEnded := make(chan struct{})
	parallelArrived := make(chan string, 1)
	completeParallel := make(chan struct{})
	var completeOnce sync.Once
	releaseParallel := func() { completeOnce.Do(func() { close(completeParallel) }) }
	defer releaseParallel()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode model request: %v", err)
			return
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, backoffCompletion)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, backoffChunk)
		w.(http.Flusher).Flush()
		if body.Model == "probe-model" {
			defer close(probeEnded)
			<-r.Context().Done()
			return
		}
		parallelArrived <- body.Model
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-completeParallel:
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				return
			case <-ticker.C:
				// Transport activity keeps this independent stream healthy without
				// filling the model relay while the probe's body becomes idle.
				if _, err := io.WriteString(w, ": still active\n\n"); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer endpoint.Close()
	defer cancel() // Reap blocked handlers before closing the test server.
	pool := backoffPool(endpoint.URL)
	pool.cfg.Models.Providers[0].TimeoutSeconds = 1
	gate := newBackoffGate(200*time.Millisecond, 800*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
	build, err := pool.ModelBuilder(ctx, "fixture", "probe-model", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := build("manager", "probe-thread-manager")
	otherBuild, err := pool.ModelBuilder(ctx, "fixture", "parallel-model", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := otherBuild("worker", "other-thread-worker")
	if _, err := probe.Generate(ctx, backoffMessages()); err == nil {
		t.Fatal("initial HTTP 503 did not fail")
	}
	stream, err := probe.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Recv()
	if err != nil || first == nil || first.Content != "ready" {
		t.Fatalf("probe did not produce decoded content: first=%+v err=%v", first, err)
	}
	parallel, err := worker.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatalf("content did not admit a different role/model: %v", err)
	}
	defer parallel.Close()
	select {
	case got := <-parallelArrived:
		if got != "parallel-model" {
			t.Fatalf("wrong model reached HTTP: %q", got)
		}
	case <-ctx.Done():
		t.Fatal("parallel worker never reached HTTP")
	}
	parallelDone := make(chan error, 1)
	go func() { parallelDone <- finishBackoffStream(parallel) }()
	if err := finishBackoffStream(stream); !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("silent probe did not retain its actual idle error: %v", err)
	}
	select {
	case <-probeEnded:
	case <-ctx.Done():
		t.Fatal("idle timeout left its HTTP request alive")
	}
	state := backoffProgressStateOf(gate)
	if state.failures != 2 || state.delay != 400*time.Millisecond || state.probe || state.probeReady {
		t.Fatalf("idle probe did not advance and close recovery: %+v", state)
	}
	select {
	case err := <-parallelDone:
		t.Fatalf("renewed cooldown ended already admitted work: %v", err)
	default:
	}
	blockedCtx, blockedCancel := context.WithTimeout(ctx, 30*time.Millisecond)
	_, err = worker.Generate(blockedCtx, backoffMessages())
	blockedCancel()
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 3 {
		t.Fatalf("next request skipped renewed cooldown: requests=%d err=%v", requests.Load(), err)
	}
	releaseParallel()
	select {
	case err := <-parallelDone:
		if err != nil {
			t.Fatalf("admitted parallel work failed after the probe timed out: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("admitted parallel work did not finish")
	}
	if got := backoffProgressStateOf(gate); got.failures != 2 || got.generation != state.generation {
		t.Fatalf("late parallel success erased the probe's idle failure: %+v", got)
	}
}

func TestModelBackoffErrorFrameCannotReleaseTheProbeGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var requests atomic.Int32
	probeStarted := make(chan struct{})
	emitError := make(chan struct{})
	var emitOnce sync.Once
	releaseError := func() { emitOnce.Do(func() { close(emitError) }) }
	defer releaseError()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(probeStarted)
			select {
			case <-emitError:
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"unexpected EOF from fixture\",\"type\":\"server_error\"}}\n\n")
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, backoffCompletion)
		}
	}))
	defer endpoint.Close()
	defer cancel()
	pool := backoffPool(endpoint.URL)
	gate := newBackoffGate(200*time.Millisecond, 800*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
	m, err := pool.Get(ctx, "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(ctx, backoffMessages()); err == nil {
		t.Fatal("initial HTTP 503 did not fail")
	}
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case <-probeStarted:
	case <-ctx.Done():
		t.Fatal("probe did not reach the HTTP server")
	}
	waiting := make(chan BackoffRecord, 1)
	build, err := pool.ModelBuilder(ctx, "fixture", "other-model", "", func(record CallRecord) {
		if record.Backoff != nil {
			waiting <- *record.Backoff
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	blockedCtx, blockedCancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer blockedCancel()
	parallelDone := make(chan error, 1)
	go func() {
		_, err := build("worker", "waiting-other-thread").Generate(blockedCtx, backoffMessages())
		parallelDone <- err
	}()
	select {
	case notice := <-waiting:
		if notice.WaitMS != 0 {
			t.Fatalf("expected an active-probe wait before the error frame: %+v", notice)
		}
	case <-ctx.Done():
		t.Fatal("other model did not wait behind the probe")
	}
	releaseError()
	if err := finishBackoffStream(stream); err == nil {
		t.Fatal("error frame became a successful model response")
	}
	state := backoffProgressStateOf(gate)
	if state.failures != 2 || state.probeReady || state.probe {
		t.Fatalf("error frame opened or cleared the probe gate: %+v", state)
	}
	select {
	case err := <-parallelDone:
		if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
			t.Fatalf("error frame released an existing waiter: requests=%d err=%v", requests.Load(), err)
		}
	case <-ctx.Done():
		t.Fatal("waiting model did not retain cancellation after the probe error")
	}
}
