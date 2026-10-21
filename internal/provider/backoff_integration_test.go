package provider

import (
	"context"
	"errors"
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

// Sibling workers must wait while the one recovery probe is still streaming.
// Only its complete valid response may restore ordinary parallel requests.
func TestModelBackoffRecoveryWaitsForTheCompleteProbeStream(t *testing.T) {
	var requests atomic.Int32
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseProbe) }) }
	t.Cleanup(release)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable","type":"server_error"}}`)
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, backoffChunk)
			w.(http.Flusher).Flush()
			close(probeStarted)
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
	pool := backoffPool(endpoint.URL)
	pool.backoffs = map[string]*backoffGate{backoffKey(endpoint.URL): newBackoffGate(20*time.Millisecond, 80*time.Millisecond)}
	m, err := pool.Get(context.Background(), "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(context.Background(), backoffMessages()); err == nil {
		t.Fatal("the initial unavailable response must fail")
	}
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelProbe()
	stream, err := m.Stream(probeCtx, backoffMessages())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case <-probeStarted:
	case <-probeCtx.Done():
		t.Fatal("recovery probe did not reach the fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	_, err = m.Generate(ctx, backoffMessages())
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 2 {
		t.Fatalf("HTTP 200 reopened requests before the probe finished: err=%v requests=%d", err, requests.Load())
	}
	release()
	if err := finishBackoffStream(stream); err != nil {
		t.Fatalf("complete recovery stream: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := m.Generate(ctx, backoffMessages())
	if err != nil || out == nil || out.Content != "ready" || requests.Load() != 3 {
		t.Fatalf("complete stream did not restore requests: output=%+v err=%v requests=%d", out, err, requests.Load())
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
