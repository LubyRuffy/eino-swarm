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

// Both protocols share the origin, including a different conversation's role
// and model. Streaming progress frees that caller without settling the probe.
func TestBackoffResponsesContentReleasesOtherModelsBeforeCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, delta, output string
	}{
		{"text", `{"type":"response.output_text.delta","delta":"progress"}`, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"progress"}]}]`},
		{"reasoning", `{"type":"response.reasoning_summary_text.delta","delta":"progress"}`, `[{"type":"reasoning","summary":[{"text":"progress"}]}]`},
		{"function", `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_probe","name":"inspect","arguments":""}}` + "\n\n" + `data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`, `[{"type":"function_call","call_id":"call_probe","name":"inspect","arguments":"{}"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var requests atomic.Int32
			started := make(chan struct{})
			content := make(chan struct{})
			complete := make(chan struct{})
			var contentOnce, completeOnce sync.Once
			releaseContent := func() { contentOnce.Do(func() { close(content) }) }
			releaseComplete := func() { completeOnce.Do(func() { close(complete) }) }
			defer releaseContent()
			defer releaseComplete()
			models := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
				}
				models <- body.Model
				switch requests.Add(1) {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
				case 2:
					w.Header().Set("Content-Type", "text/event-stream")
					// None of these decoded events contains usable output. Usage
					// in an in-progress response also cannot establish recovery.
					_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"usage\":{\"input_tokens\":3}}}\n\n"+
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"\"}\n\n"+
						"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"\"}\n\n")
					w.(http.Flusher).Flush()
					close(started)
					select {
					case <-content:
					case <-r.Context().Done():
						return
					}
					_, _ = fmt.Fprintf(w, "data: %s\n\n", tc.delta)
					w.(http.Flusher).Flush()
					select {
					case <-complete:
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"probe\",\"status\":\"completed\",\"output\":%s,\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n", tc.output)
					case <-r.Context().Done():
					}
				default:
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"parallel","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ready"}]}]}`)
				}
			}))
			defer srv.Close()
			defer cancel() // Cancel a blocked handler before the server is closed.
			pool, gate := progressResponsesPool(srv.URL)
			build, err := pool.ModelBuilder(ctx, "fixture", "probe-model", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			probe := build("manager", "manager-probe")
			otherBuild, err := pool.ModelBuilder(ctx, "fixture", "other-model", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			worker := otherBuild("worker", "other-thread-worker")
			if _, err := probe.Generate(ctx, backoffMessages()); err == nil {
				t.Fatal("fixture did not produce initial 503")
			}
			stream, err := probe.Stream(ctx, backoffMessages())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("probe did not reach HTTP")
			}
			blockedCtx, blockedCancel := context.WithTimeout(ctx, 40*time.Millisecond)
			_, err = worker.Generate(blockedCtx, backoffMessages())
			blockedCancel()
			if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
				t.Fatalf("metadata released another model: requests=%d err=%v", requests.Load(), err)
			}
			releaseContent()
			for {
				part, err := stream.Recv()
				if err != nil {
					t.Fatalf("read probe progress: %v", err)
				}
				if part != nil && (part.Content != "" || part.ReasoningContent != "" || len(part.ToolCalls) != 0) {
					break
				}
			}
			parallelCtx, parallelCancel := context.WithTimeout(ctx, time.Second)
			out, err := worker.Generate(parallelCtx, backoffMessages())
			parallelCancel()
			if err != nil || out == nil || out.Content != "ready" || requests.Load() != 3 {
				t.Fatalf("live probe still blocked other model: output=%+v requests=%d err=%v", out, requests.Load(), err)
			}
			gate.mu.Lock()
			failures, probing := gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 1 || !probing {
				t.Fatalf("parallel success settled active probe: failures=%d probe=%v", failures, probing)
			}
			releaseComplete()
			if err := finishBackoffStream(stream); err != nil {
				t.Fatalf("valid response.completed failed: %v", err)
			}
			gate.mu.Lock()
			failures, probing = gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 0 || probing {
				t.Fatalf("completed probe did not clear failures: failures=%d probe=%v", failures, probing)
			}
			for _, want := range []string{"probe-model", "probe-model", "other-model"} {
				if got := <-models; got != want {
					t.Fatalf("model request=%q, want %q", got, want)
				}
			}
		})
	}
}

func progressResponsesPool(endpoint string) (*Pool, *backoffGate) {
	cfg := config.Default()
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: endpoint + "/v1", Model: "fixture-model", APIMode: "responses"}}
	pool := New(cfg)
	gate := newBackoffGate(10*time.Millisecond, 40*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint): gate}
	return pool, gate
}

// Generate has no decoded intermediate output; a flushed HTTP 200 must still
// leave all other calls behind the one probe until its complete body validates.
func TestBackoffGenerateHeadersDoNotReleaseOtherRequests(t *testing.T) {
	for _, mode := range []string{"chat", "responses"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var requests atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case 2:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if mode == "responses" {
					_, _ = io.WriteString(w, `{"id":"complete","status":"completed","output":[]}`)
				} else {
					_, _ = io.WriteString(w, backoffCompletion)
				}
			}))
			defer srv.Close()
			defer cancel()
			pool, gate := progressResponsesPool(srv.URL)
			if mode == "chat" {
				pool = backoffPool(srv.URL)
				pool.backoffs = map[string]*backoffGate{backoffKey(srv.URL): gate}
			}
			m, err := pool.Get(ctx, "fixture", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Generate(ctx, backoffMessages()); err == nil {
				t.Fatal("initial 503 did not fail")
			}
			result := make(chan error, 1)
			go func() {
				_, err := m.Generate(ctx, backoffMessages())
				result <- err
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("Generate probe did not reach HTTP")
			}
			waitingCtx, waitingCancel := context.WithTimeout(ctx, 40*time.Millisecond)
			_, err = m.Generate(waitingCtx, backoffMessages())
			waitingCancel()
			if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
				t.Fatalf("headers released another Generate: requests=%d err=%v", requests.Load(), err)
			}
			unblock()
			if err := <-result; err != nil {
				t.Fatalf("completed Generate probe: %v", err)
			}
			if _, err := m.Generate(ctx, backoffMessages()); err != nil || requests.Load() != 3 {
				t.Fatalf("completed Generate left calls blocked: requests=%d err=%v", requests.Load(), err)
			}
		})
	}
}

// A terminal event carrying an error is not a successful tool increment, even
// if that same event also includes a function-call output item.
func TestBackoffResponsesFailedCompletionCannotPublishProbeProgress(t *testing.T) {
	for _, tc := range []struct{ name, response string }{
		{"failed status", `{"status":"failed","output":[{"type":"function_call","call_id":"failed_call","name":"inspect","arguments":"{}"}]}`},
		{"completed with error", `{"status":"completed","error":{"type":"server_error","message":"rejected completion"},"output":[{"type":"function_call","call_id":"failed_call","name":"inspect","arguments":"{}"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var requests atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
				case 2:
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					close(started)
					select {
					case <-release:
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", tc.response)
					case <-r.Context().Done():
					}
				default:
					t.Error("failed completion released another request")
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer srv.Close()
			defer cancel()
			pool, gate := progressResponsesPool(srv.URL)
			gate.initial, gate.max = 80*time.Millisecond, 320*time.Millisecond
			m, err := pool.Get(ctx, "fixture", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Generate(ctx, backoffMessages()); err == nil {
				t.Fatal("initial outage did not fail")
			}
			stream, err := m.Stream(ctx, backoffMessages())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("probe did not reach HTTP")
			}
			unblock()
			for {
				part, recvErr := stream.Recv()
				if part != nil && (part.Content != "" || part.ReasoningContent != "" || len(part.ToolCalls) != 0) {
					t.Fatalf("error frame published progress before validation: %+v", part)
				}
				if recvErr != nil {
					if errors.Is(recvErr, io.EOF) {
						t.Fatal("contradictory completed event became successful")
					}
					break
				}
			}
			gate.mu.Lock()
			failures, probing := gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 1 || probing {
				t.Fatalf("error completion changed failure count: failures=%d probe=%v", failures, probing)
			}
			waitingCtx, waitingCancel := context.WithTimeout(ctx, time.Millisecond)
			defer waitingCancel()
			if _, err := m.Generate(waitingCtx, backoffMessages()); !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
				t.Fatalf("error completion opened origin: requests=%d err=%v", requests.Load(), err)
			}
		})
	}
}

func TestBackoffResponsesTruncationAfterProgressKeepsProbeHealthOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var requests atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1, 3:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"progress\"}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-release: // EOF without response.completed must remain a failure.
			case <-r.Context().Done():
			}
		default:
			t.Error("truncated probe bypassed cooldown")
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	defer cancel()
	pool, gate := progressResponsesPool(srv.URL)
	gate.initial, gate.max = 80*time.Millisecond, 320*time.Millisecond
	m, err := pool.Get(ctx, "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(ctx, backoffMessages()); err == nil {
		t.Fatal("initial outage did not fail")
	}
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if part, err := stream.Recv(); err != nil || part == nil || part.Content != "progress" {
		t.Fatalf("probe did not publish progress: part=%+v err=%v", part, err)
	}
	parallelCtx, parallelCancel := context.WithTimeout(ctx, time.Second)
	_, err = m.Generate(parallelCtx, backoffMessages())
	parallelCancel()
	if err == nil || requests.Load() != 3 {
		t.Fatalf("parallel request did not reach fixture 503: requests=%d err=%v", requests.Load(), err)
	}
	gate.mu.Lock()
	failures, probing := gate.failures, gate.probe
	gate.mu.Unlock()
	if failures != 1 || !probing {
		t.Fatalf("parallel failure settled active probe: failures=%d probe=%v", failures, probing)
	}
	unblock()
	if err := finishBackoffStream(stream); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated Responses stream: %v", err)
	}
	gate.mu.Lock()
	failures, probing, delay := gate.failures, gate.probe, gate.delay
	gate.mu.Unlock()
	if failures != 2 || probing || delay != 160*time.Millisecond {
		t.Fatalf("probe truncation lost failure progression: failures=%d probe=%v delay=%s", failures, probing, delay)
	}
	waitingCtx, waitingCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer waitingCancel()
	if _, err := m.Generate(waitingCtx, backoffMessages()); !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 3 {
		t.Fatalf("truncation failed to reclose origin: requests=%d err=%v", requests.Load(), err)
	}
}
