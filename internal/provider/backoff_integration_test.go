package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// A manager error must also slow newly built workers and later turns on that
// server. Per-model or per-wrapper timers let those callers bypass the pause.
func TestModelBackoffSurvivesRoleModelProviderAndCacheChanges(t *testing.T) {
	var failedRequests atomic.Int32
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failedRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable","type":"server_error"}}`)
	}))
	t.Cleanup(failed.Close)
	var healthyRequests atomic.Int32
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, backoffCompletion)
	}))
	t.Cleanup(healthy.Close)
	cfg := config.Default()
	cfg.Models.Default = "primary"
	cfg.Models.Providers = []config.Provider{
		{ID: "primary", BaseURL: failed.URL + "/v1", Model: "first-model"},
		{ID: "alias", BaseURL: failed.URL + "/alternate", Model: "other-model"},
		{ID: "healthy", BaseURL: healthy.URL, Model: "healthy-model"},
	}
	pool := New(cfg)
	m, err := pool.Get(context.Background(), "primary", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(context.Background(), backoffMessages()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("initial request must reach the failing HTTP adapter: %v", err)
	}
	if failedRequests.Load() != 1 {
		t.Fatalf("initial requests=%d, want 1", failedRequests.Load())
	}
	for _, invalidate := range []bool{false, true} {
		if invalidate {
			pool.Invalidate()
		}
		build, err := pool.ModelBuilder(context.Background(), "alias", "new-model", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		_, err = build("worker", "worker-after-failure").Generate(ctx, backoffMessages())
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("invalidate=%v: cooling call must end at its context deadline, got %v", invalidate, err)
		}
		if failedRequests.Load() != 1 {
			t.Errorf("invalidate=%v: role/model/provider change bypassed cooldown: requests=%d, want 1", invalidate, failedRequests.Load())
		}
	}
	other, err := pool.Get(context.Background(), "healthy", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := other.Generate(ctx, backoffMessages())
	if err != nil || out == nil || out.Content != "ready" || healthyRequests.Load() != 1 {
		t.Fatalf("independent server must remain usable: output=%+v err=%v requests=%d", out, err, healthyRequests.Load())
	}
}

// The telemetry wrapper sits outside the shared gate. Canceling before that
// gate admits HTTP must leave a wait event, rather than invent a second call.
func TestModelBackoffCanceledBeforeHTTPRecordsOnlyTheWait(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "generate"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable","type":"server_error"}}`)
			}))
			t.Cleanup(endpoint.Close)
			pool := backoffPool(endpoint.URL)
			var records []CallRecord
			var mu sync.Mutex
			build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", func(record CallRecord) {
				mu.Lock()
				records = append(records, record)
				mu.Unlock()
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := build("manager", "manager").Generate(context.Background(), backoffMessages()); err == nil {
				t.Fatal("the initial service failure must reach the fixture")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()
			worker := build("worker", "waiting-worker")
			if streaming {
				stream, streamErr := worker.Stream(ctx, backoffMessages())
				err = streamErr
				if stream != nil {
					stream.Close()
				}
			} else {
				_, err = worker.Generate(ctx, backoffMessages())
			}
			if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
				t.Fatalf("canceling pre-request cooldown: err=%v requests=%d", err, requests.Load())
			}
			mu.Lock()
			defer mu.Unlock()
			calls, waits := 0, 0
			for _, record := range records {
				if record.Backoff == nil {
					calls++
					continue
				}
				waits++
				if record.AgentID != "waiting-worker" || record.Backoff.Attempt != 1 || record.Backoff.WaitMS <= 0 {
					t.Errorf("cooldown lost the actual caller or delay: %+v", record)
				}
			}
			if calls != 1 || waits != 1 {
				t.Fatalf("one HTTP request and one canceled wait produced calls=%d waits=%d; records=%+v", calls, waits, records)
			}
		})
	}
}

// A 200 handshake cannot prove recovery: response fields may still be invalid.
func TestMalformedSuccessfulModelStreamKeepsTheOriginCooling(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":\"invalid-shape\"}\n\n")
	}))
	t.Cleanup(endpoint.Close)
	pool := backoffPool(endpoint.URL)
	build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := build("worker", "worker-1").Stream(context.Background(), backoffMessages())
	if err != nil {
		t.Fatalf("the malformed field is in the successful stream body: %v", err)
	}
	defer stream.Close()
	if err := finishBackoffStream(stream); err == nil {
		t.Fatal("the actual SDK must reject the malformed successful stream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_, err = build("manager", "manager").Generate(ctx, backoffMessages())
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
		t.Fatalf("invalid stream must cool later roles: err=%v requests=%d", err, requests.Load())
	}
}

// A long recovery generation must release sibling models after decoded content,
// while retaining sole authority over the outage until its terminal result.
func TestModelBackoffRecoveryReopensAfterUsefulProbeContent(t *testing.T) {
	for _, tc := range []struct{ name, prelude, delta string }{
		{"headers", "", `{"content":"ready"}`},
		{"role", `data: {"choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n", `{"content":"ready"}`},
		{"empty", `data: {"choices":[{"index":0,"delta":{}}]}` + "\n\n", `{"content":"ready"}`},
		{"usage", `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n\n", `{"content":"ready"}`},
		{"empty_tool", `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function"}]}}]}` + "\n\n", `{"content":"ready"}`},
		{"reasoning", "", `{"reasoning_content":"working"}`},
		{"tool_name", "", `{"tool_calls":[{"index":0,"function":{"name":"check"}}]}`},
		{"tool_arguments", "", `{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}`},
		{"tool_id", "", `{"tool_calls":[{"index":0,"id":"call_fixture"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			probeStarted, content, releaseProbe := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var contentOnce, releaseOnce sync.Once
			release := func() { contentOnce.Do(func() { close(content) }); releaseOnce.Do(func() { close(releaseProbe) }) }
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
				case 2:
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.prelude)
					w.(http.Flusher).Flush()
					close(probeStarted)
					select {
					case <-content:
					case <-r.Context().Done():
						return
					}
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", tc.delta)
					w.(http.Flusher).Flush()
					select {
					case <-releaseProbe:
						_, _ = io.WriteString(w, "data: [DONE]\n\n")
					case <-r.Context().Done():
					}
				default:
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, backoffCompletion)
				}
			}))
			t.Cleanup(endpoint.Close)
			t.Cleanup(release)
			pool := backoffPool(endpoint.URL)
			gate := newBackoffGate(time.Millisecond, 4*time.Millisecond)
			pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
			build, err := pool.ModelBuilder(context.Background(), "fixture", "probe-model", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			m := build("manager", "probe-manager")
			other, err := pool.ModelBuilder(context.Background(), "fixture", "other-model", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			worker := other("worker", "other-conversation-worker")
			if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
				t.Fatal("initial outage must fail")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := m.Stream(ctx, backoffMessages())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			select {
			case <-probeStarted:
			case <-ctx.Done():
				t.Fatal("probe did not reach fixture")
			}
			waiting, cancelWaiting := context.WithTimeout(ctx, 40*time.Millisecond)
			_, err = worker.Generate(waiting, backoffMessages())
			cancelWaiting()
			if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
				t.Fatalf("non-content released gate: err=%v requests=%d", err, requests.Load())
			}
			contentOnce.Do(func() { close(content) })
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("decoded content: %v", err)
			}
			parallel, cancelParallel := context.WithTimeout(ctx, 250*time.Millisecond)
			_, err = worker.Generate(parallel, backoffMessages())
			cancelParallel()
			if err != nil || requests.Load() != 3 {
				t.Fatalf("content did not admit other role/model before stream completion: err=%v requests=%d", err, requests.Load())
			}
			gate.mu.Lock()
			failures, probing := gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 1 || !probing {
				t.Fatalf("parallel success settled active probe: failures=%d probe=%v", failures, probing)
			}
			release()
			if err := finishBackoffStream(stream); err != nil {
				t.Fatalf("complete stream: %v", err)
			}
			gate.mu.Lock()
			failures, probing = gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 0 || probing {
				t.Fatalf("completed probe did not clear outage: failures=%d probe=%v", failures, probing)
			}
		})
	}
}

// Stop cancels the HTTP context even when a recovering stream stops emitting.
// Its abandoned probe must release the shared gate without proving recovery.
func TestModelBackoffCancelingASilentProbeReleasesTheGate(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, backoffChunk)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, backoffCompletion)
		}
	}))
	t.Cleanup(endpoint.Close)
	pool := backoffPool(endpoint.URL)
	gate := newBackoffGate(20*time.Millisecond, 80*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
	m, err := pool.Get(context.Background(), "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
		t.Fatal("the initial service failure must reach the fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("probe did not reach its live stream: %v", err)
	}
	cancel()
	ended := make(chan error, 1)
	go func() { ended <- finishBackoffStream(stream) }()
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop must retain context cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not interrupt the silent probe")
	}
	gate.mu.Lock()
	failures, probing := gate.failures, gate.probe
	gate.mu.Unlock()
	if failures != 1 || probing {
		t.Fatalf("canceled probe changed outage budget or retained permission: failures=%d probe=%v", failures, probing)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := m.Generate(ctx, backoffMessages())
	if err != nil || out == nil || out.Content != "ready" || requests.Load() != 3 {
		t.Fatalf("replacement probe could not recover: output=%+v err=%v requests=%d", out, err, requests.Load())
	}
}

func TestHeaderReconnectWaitsForBackoffBeforeResending(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		_, _ = io.WriteString(w, "ready")
	}))
	t.Cleanup(endpoint.Close)
	waiting := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, reconnectBackoffKey{}, func() error {
		close(waiting)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader("request"))
		if err != nil {
			done <- err
			return
		}
		resp, err := chatHTTPClient(time.Second, 40*time.Millisecond).Do(req)
		if err == nil {
			defer resp.Body.Close()
			var body []byte
			body, err = io.ReadAll(resp.Body)
			if err == nil && string(body) != "ready" {
				err = errors.New("reconnect response did not contain the expected body")
			}
		}
		done <- err
	}()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("header reconnect bypassed the cooldown callback: %v", err)
	case <-ctx.Done():
		t.Fatal("header reconnect never requested cooldown")
	}
	if requests.Load() != 1 {
		t.Fatalf("reconnect sent a second POST while cooldown was pending: %d", requests.Load())
	}
	unblock()
	if err := <-done; err != nil || requests.Load() != 2 {
		t.Fatalf("reconnect after cooldown: err=%v requests=%d", err, requests.Load())
	}
}

func TestCanceledHeaderReconnectDoesNotSendASecondPayload(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(endpoint.Close)
	var cooldowns atomic.Int32
	ctx := context.WithValue(context.Background(), reconnectBackoffKey{}, func() error {
		cooldowns.Add(1)
		return context.Canceled
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := chatHTTPClient(time.Second, 40*time.Millisecond).Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) || cooldowns.Load() != 1 || requests.Load() != 1 {
		t.Fatalf("canceled reconnect: err=%v cooldowns=%d requests=%d", err, cooldowns.Load(), requests.Load())
	}
}

const backoffCompletion = `{"id":"fixture","object":"chat.completion","created":1,"model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"ready"},"finish_reason":"stop"}]}`

const backoffChunk = "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ready\"},\"finish_reason\":null}]}\n\n"

func backoffPool(baseURL string) *Pool {
	cfg := config.Default()
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: baseURL, Model: "fixture-model"}}
	return New(cfg)
}

func finishBackoffStream(stream *schema.StreamReader[*schema.Message]) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func backoffMessages() []*schema.Message {
	return []*schema.Message{schema.UserMessage("perform the requested check")}
}

// The SDK exposes EOF both for [DONE] and a socket ending after a valid delta.
// Decoded output may release concurrency, but a later truncation must restore
// cooldown and retain the failure streak and usage.
func TestModelBackoffTruncatedDeltaStreamRenewsCooldownAfterOpening(t *testing.T) {
	var requests atomic.Int32
	releaseProbe := make(chan struct{})
	peerStarted, releasePeer := make(chan struct{}), make(chan struct{})
	var releaseOnce, peerOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseProbe) }) }
	releaseParallel := func() { peerOnce.Do(func() { close(releasePeer) }) }
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, backoffChunk)
			_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
			}
		case 4:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 5:
			close(peerStarted)
			select {
			case <-releasePeer:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, backoffCompletion)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, backoffCompletion)
		}
	}))
	t.Cleanup(endpoint.Close)
	t.Cleanup(release)
	t.Cleanup(releaseParallel)
	pool := backoffPool(endpoint.URL)
	gate := newBackoffGate(100*time.Millisecond, 400*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
	records := make(chan CallRecord, 16)
	build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", func(record CallRecord) { records <- record })
	if err != nil {
		t.Fatal(err)
	}
	m := build("manager", "manager")
	if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
		t.Fatal("initial outage did not reach the HTTP fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("probe content: %v", err)
	}
	// Both outcomes belong to parallel calls, not to the health probe.
	for _, wantFailure := range []bool{false, true} {
		out, err := build("worker", "parallel-worker").Generate(ctx, backoffMessages())
		if (err != nil) != wantFailure || (!wantFailure && out == nil) {
			t.Fatalf("parallel failure=%v: out=%v err=%v", wantFailure, out, err)
		}
		gate.mu.Lock()
		failures, probing := gate.failures, gate.probe
		gate.mu.Unlock()
		if failures != 1 || !probing {
			t.Fatalf("parallel result changed probe health: failures=%d probe=%v", failures, probing)
		}
	}
	peerDone := make(chan error, 1)
	go func() {
		_, err := build("worker", "still-running-worker").Generate(ctx, backoffMessages())
		peerDone <- err
	}()
	select {
	case <-peerStarted:
	case <-ctx.Done():
		t.Fatal("parallel call did not reach fixture")
	}
	release()
	if err := finishBackoffStream(stream); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a stream ending without a terminal response must fail, got %v", err)
	}
	for {
		select {
		case record := <-records:
			if !errors.Is(record.Err, io.ErrUnexpectedEOF) {
				continue
			}
			if record.TotalTokens != 5 || record.PromptTokens != 3 || record.CompletionTokens != 2 || record.AgentID != "manager" {
				t.Fatalf("truncated stream lost received usage or attribution: %+v", record)
			}
		case <-time.After(time.Second):
			t.Fatal("truncated model request was not recorded")
		}
		break
	}
	gate.mu.Lock()
	failures, probing := gate.failures, gate.probe
	gate.mu.Unlock()
	if failures != 2 || probing {
		t.Fatalf("truncated recovery probe reset the service: failures=%d probe=%v", failures, probing)
	}
	waiting, cancelWaiting := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancelWaiting()
	if _, err := m.Generate(waiting, backoffMessages()); !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 5 {
		t.Fatalf("truncated stream let another request escape: err=%v requests=%d", err, requests.Load())
	}
	select {
	case err := <-peerDone:
		t.Fatalf("probe failure canceled an admitted call: %v", err)
	default:
	}
	releaseParallel()
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("admitted call failed after probe truncation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("admitted call did not finish")
	}
	gate.mu.Lock()
	failures = gate.failures
	gate.mu.Unlock()
	if failures != 2 {
		t.Fatalf("late parallel success reset new outage: failures=%d", failures)
	}

}

// Compatible endpoints may omit [DONE] after an explicit finish_reason; both
// terminal forms prove completion, and their billed usage must be retained.
func TestModelBackoffCompleteHTTPStreamPreservesUsageAndRestoresRequests(t *testing.T) {
	for _, ending := range []string{"done", "stop", "length", "tool_calls"} {
		t.Run(ending, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
				case 2:
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, backoffChunk)
					finish := ending
					if ending == "done" {
						finish = ""
					}
					_, _ = fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n", finish)
					if ending == "done" {
						_, _ = io.WriteString(w, "data: [DONE]\n\n")
					}
				default:
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, backoffCompletion)
				}
			}))
			t.Cleanup(endpoint.Close)
			pool := backoffPool(endpoint.URL)
			gate := newBackoffGate(time.Millisecond, 4*time.Millisecond)
			pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
			records := make(chan CallRecord, 8)
			build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", func(record CallRecord) { records <- record })
			if err != nil {
				t.Fatal(err)
			}
			m := build("manager", "manager")
			if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
				t.Fatal("initial outage did not reach the fixture")
			}
			stream, err := m.Stream(context.Background(), backoffMessages())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if err := finishBackoffStream(stream); err != nil {
				t.Fatalf("complete stream rejected: %v", err)
			}
			gate.mu.Lock()
			failures, probing := gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 0 || probing {
				t.Fatalf("terminal %s did not reset health: failures=%d probe=%v", ending, failures, probing)
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := m.Generate(ctx, backoffMessages()); err != nil || requests.Load() != 3 {
				t.Fatalf("completed probe did not reopen requests: err=%v requests=%d", err, requests.Load())
			}
			calls, billed := 0, 0
			for len(records) > 0 {
				record := <-records
				if record.Backoff == nil {
					calls++
				}
				if record.TotalTokens == 5 {
					billed++
					if record.AgentID != "manager" || record.PromptTokens != 3 || record.CompletionTokens != 2 || record.Err != nil {
						t.Fatalf("completed probe lost provenance or usage: %+v", record)
					}
				}
			}
			if calls != 3 || billed != 1 {
				t.Fatalf("wait inflated call/billing totals: calls=%d billed=%d", calls, billed)
			}
		})
	}
}

// Eino's Close notifies its writer; cancellation belongs to the request
// context. Real callers drain the model stream or cancel that context on Stop.
func TestModelBackoffClosedReaderRetainsProbeUntilRequestCancellation(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, backoffChunk)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(endpoint.Close)
	pool := backoffPool(endpoint.URL)
	gate := newBackoffGate(time.Millisecond, 4*time.Millisecond)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): gate}
	finished := make(chan CallRecord, 8)
	build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", func(record CallRecord) { finished <- record })
	if err != nil {
		t.Fatal(err)
	}
	m := build("worker", "worker-closed-reader")
	if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
		t.Fatal("initial outage did not reach the fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := m.Stream(ctx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		stream.Close()
		t.Fatal(err)
	}
	stream.Close()
	if ctx.Err() != nil {
		t.Fatalf("closing an Eino reader unexpectedly canceled its context: %v", ctx.Err())
	}
	gate.mu.Lock()
	failures, probing := gate.failures, gate.probe
	gate.mu.Unlock()
	if failures != 1 || !probing {
		t.Fatalf("closing an incomplete response claimed recovery: failures=%d probe=%v", failures, probing)
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case record := <-finished:
			if !errors.Is(record.Err, context.Canceled) {
				continue
			}
			gate.mu.Lock()
			failures, probing = gate.failures, gate.probe
			gate.mu.Unlock()
			if failures != 1 || probing {
				t.Fatalf("request cancellation did not release the original probe: failures=%d probe=%v", failures, probing)
			}
			return
		case <-deadline:
			t.Fatal("request cancellation did not finish the silent probe")
		}
	}
}

// The header timeout starts with the actual network attempt, not with a
// prior shared cooldown. The caller's context still bounds the whole call.
func TestHeaderReconnectBudgetStartsAfterSharedBackoff(t *testing.T) {
	var requests atomic.Int32
	var mu sync.Mutex
	var arrivals []time.Time
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		if requests.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		_, _ = io.WriteString(w, "ready")
	}))
	t.Cleanup(endpoint.Close)
	gate := newBackoffGate(60*time.Millisecond, 120*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, finish, err := (&recordingModel{gate: gate}).admit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := chatHTTPClient(time.Second, 20*time.Millisecond).Do(req)
	if err != nil {
		finish(err)
		t.Fatalf("shared wait consumed the next header attempt's budget: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	finish(err)
	if err != nil || string(body) != "ready" || requests.Load() != 2 {
		t.Fatalf("reconnect result: body=%q err=%v requests=%d", body, err, requests.Load())
	}
	mu.Lock()
	gap := arrivals[1].Sub(arrivals[0])
	mu.Unlock()
	if gap < 60*time.Millisecond {
		t.Fatalf("header reconnect bypassed the shared wait: %s", gap)
	}
}
