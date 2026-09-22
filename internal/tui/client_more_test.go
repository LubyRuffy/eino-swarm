package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPumpClientSettlesAOneShotTurn(t *testing.T) {
	var running int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/presence" && r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"id":"pc_1"}`)
		case strings.HasPrefix(r.URL.Path, "/api/presence/"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ": held\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case r.URL.Path == "/api/threads" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"thread":{"id":"th_1"}}`)
		case r.URL.Path == "/api/threads/th_1/turns" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/api/threads/th_1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: user_message\ndata: {\"text\":\"work\"}\n\n")
			_, _ = io.WriteString(w, "event: done\ndata: {\"agent_id\":\"manager\",\"text\":\"ok\"}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case r.URL.Path == "/api/threads/th_1" && r.Method == http.MethodGet:
			running++
			_, _ = io.WriteString(w, `{"status":{"running":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan notificationMsg, 8)
	go pumpClient(ctx, ClientConfig{BaseURL: srv.URL, Task: "work"}, nil, out, nil, nil, nil)

	var sawUser, sawIdle bool
	deadline := time.After(3 * time.Second)
	for !sawIdle {
		select {
		case msg, ok := <-out:
			if !ok {
				t.Fatal("the pump closed before the turn settled")
			}
			if msg.userText == "work" {
				sawUser = true
			}
			if msg.idle {
				sawIdle = true
			}
		case <-deadline:
			t.Fatal("the one-shot pump did not settle")
		}
	}
	if !sawUser {
		t.Fatal("the stored user line never arrived")
	}
	cancel()
}

func TestOpenThreadRecordsTheWorkdirAndTheGoal(t *testing.T) {
	var project, patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/projects" && r.Method == http.MethodPost:
			project = true
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"project":{"id":"pj_1"}}`)
		case r.URL.Path == "/api/threads" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"thread":{"id":"th_1"}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/threads/th_1":
			raw, _ := io.ReadAll(r.Body)
			patched = strings.Contains(string(raw), `"goal"`) && strings.Contains(string(raw), `"plan_mode":true`)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	id, err := NewClient(srv.URL).OpenThread(context.Background(), ClientConfig{
		Task: "work", Goal: "stand", Plan: "look", Workspace: "/tmp/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "th_1" || !project || !patched {
		t.Fatalf("id=%s project=%v patched=%v", id, project, patched)
	}
}

func TestReadSSEJoinsContinuedData(t *testing.T) {
	var gotEvent, gotData string
	readSSE(strings.NewReader(": comment\nevent: agent_message\ndata: {\"text\":\ndata: \"ab\"}\n\n"), func(event, data string) {
		gotEvent, gotData = event, data
	})
	if gotEvent != "agent_message" || gotData != "{\"text\":\n\"ab\"}" {
		t.Fatalf("event=%q data=%q", gotEvent, gotData)
	}
}

func TestSettledRunningNoticesATurnThatRestarts(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		running := n > 1
		_, _ = io.WriteString(w, `{"status":{"running":`)
		if running {
			_, _ = io.WriteString(w, "true}}")
			return
		}
		_, _ = io.WriteString(w, "false}}")
	}))
	defer srv.Close()
	if !NewClient(srv.URL).settledRunning(context.Background(), "th") {
		t.Fatal("a turn that starts during the grace must keep the stream")
	}
	if NewClient("http://127.0.0.1:1").Running(context.Background(), "th") {
		t.Fatal("an unreachable engine must not look busy")
	}
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if NewClient(srv.URL).settledRunning(dead, "th") {
		t.Fatal("a cancelled wait must not keep the stream")
	}
}

func TestReasonCommandClearsTheDefault(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := NewClient(srv.URL).Send(context.Background(), "th", "/reason default", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"reasoning_effort":""`) {
		t.Fatalf("body=%s", body)
	}
	if err := NewClient(srv.URL).Send(context.Background(), "th", "/model", false); err == nil {
		t.Fatal("a model command without a name must fail")
	}
	if err := NewClient(srv.URL).Send(context.Background(), "th", "  ", false); err != nil {
		t.Fatal(err)
	}
}

func TestClientReportsHTTPFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain":
			http.Error(w, "nope", http.StatusBadGateway)
		case "/coded":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"busy","code":"busy"}`)
		case "/named":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"nope"}`)
		case "/api/threads":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"thread":{}}`)
		case "/api/threads/th/events":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"missing"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{`)
		}
	}))
	defer srv.Close()
	c := Client{Base: srv.URL}
	if err := c.do(context.Background(), http.MethodGet, "/plain", nil, nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("plain error: %v", err)
	}
	err := c.do(context.Background(), http.MethodGet, "/coded", nil, nil)
	var se *statusError
	if !errors.As(err, &se) || se.Code != "busy" || !strings.Contains(se.Error(), "busy") {
		t.Fatalf("coded error: %v", err)
	}
	if _, err := c.Stream(context.Background(), "th"); err == nil {
		t.Fatal("a failed event stream must be an error")
	}
	var out struct{ N int }
	if err := c.do(context.Background(), http.MethodGet, "/bad-json", nil, &out); err == nil {
		t.Fatal("broken JSON must fail")
	}
	if (Client{}).http() == nil {
		t.Fatal("a client without a transport still has the default")
	}
	if err := c.do(context.Background(), "NOT A METHOD", "/plain", nil, nil); err == nil {
		t.Fatal("a bad method must fail before the request")
	}
	if err := c.do(context.Background(), http.MethodPost, "/plain", make(chan int), nil); err == nil {
		t.Fatal("a body that cannot be encoded must fail")
	}
	named := c.do(context.Background(), http.MethodGet, "/named", nil, nil)
	var namedErr *statusError
	if !errors.As(named, &namedErr) || namedErr.Code != "" || namedErr.Error() != "nope" {
		t.Fatalf("named error: %v", named)
	}
	if _, err := c.OpenThread(context.Background(), ClientConfig{}); err == nil {
		t.Fatal("a conversation without an id must fail")
	}
	if err := NewClient("http://127.0.0.1:1").HoldPresence(context.Background()); err == nil {
		t.Fatal("presence against a dead port must fail")
	}
}

func TestWireMessagesDropsNoise(t *testing.T) {
	if wireMessages("user_message", `{"text":" "}`) != nil {
		t.Fatal("a blank user line is not a block")
	}
	if wireMessages("nope", `{"text":"`+strings.Repeat("x", 300)+`"}`) != nil {
		t.Fatal("a long unknown event is not a notice")
	}
	note := wireMessages("nope", `{"text":"heads up"}`)
	if len(note) != 1 || note[0].notice != "heads up" {
		t.Fatalf("notice=%+v", note)
	}
	failed := wireMessages("error", `{"agent_id":"manager","err":"boom","text":"boom"}`)
	if len(failed) != 2 || failed[0].Err == nil || !failed[1].idle {
		t.Fatalf("error=%+v", failed)
	}
	said := wireMessages("agent_message", `{"agent_id":"manager","text":"hello"}`)
	if len(said) != 1 || said[0].idle || said[0].Text != "hello" {
		t.Fatalf("agent line=%+v", said)
	}
}

func TestPumpClientSurfacesAnEngineThatWillNotTalk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan notificationMsg, 4)
	go pumpClient(ctx, ClientConfig{BaseURL: "http://127.0.0.1:1", Task: "work"}, nil, out, nil, nil, nil)
	select {
	case msg := <-out:
		if msg.Err == nil {
			t.Fatalf("msg=%+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pump did not report the failure")
	}
}

func TestSubmitRemoteKeepsScreenCommandsLocal(t *testing.T) {
	m := newModel(nil)
	m.remote = true
	m.prompts = make(chan string, 1)
	if cmd := m.submitRemote("/exit"); cmd == nil || !m.quitting {
		t.Fatal("/exit must leave")
	}
	m.quitting = false
	if cmd := m.submitRemote("/help"); cmd != nil || m.notice != "" && len(m.manager.blocks) == 0 {
		t.Fatal("/help must stay on this screen")
	}
	if cmd := m.submitRemote("/clear"); cmd != nil || m.notice != "cleared" {
		t.Fatalf("clear notice=%q cmd=%v", m.notice, cmd != nil)
	}
	cmd := m.submitRemote("/goal stand")
	if cmd == nil {
		t.Fatal("/goal must be sent")
	}
	cmd()
	if got := <-m.prompts; got != "/goal stand" {
		t.Fatalf("sent %q", got)
	}
}

func TestRunClientReturnsWhenTheContextEnds(t *testing.T) {
	prev := exitProcess
	exitProcess = func(int) {}
	t.Cleanup(func() { exitProcess = prev })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	RunClient(ctx, ClientConfig{BaseURL: "http://127.0.0.1:1"})
}

func TestPumpClientStaysForAFollowUpAndARestartedTurn(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/presence":
			_, _ = io.WriteString(w, `{"id":"pc_1"}`)
		case strings.HasPrefix(r.URL.Path, "/api/presence/"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ": held\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case r.URL.Path == "/api/threads" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"thread":{"id":"th_1"}}`)
		case strings.HasSuffix(r.URL.Path, "/turns"):
			sent.Add(1)
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: done\ndata: {\"agent_id\":\"manager\",\"text\":\"ok\"}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case r.URL.Path == "/api/threads/th_1":
			_, _ = io.WriteString(w, `{"status":{"running":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prompts := make(chan string, 1)
	prompts <- "later"
	out := make(chan notificationMsg, 8)
	go pumpClient(ctx, ClientConfig{BaseURL: srv.URL}, nil, out, prompts, nil, nil)
	deadline := time.Now().Add(2 * time.Second)
	for sent.Load() == 0 && time.Now().Before(deadline) {
		select {
		case <-out:
		case <-time.After(20 * time.Millisecond):
		}
	}
	if sent.Load() == 0 {
		t.Fatal("the follow-up was not sent")
	}
	cancel()
}

func TestPumpClientSteersAndStops(t *testing.T) {
	var steered, stopped atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/threads" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"threads":[{"id":"th_1"}]}`)
		case r.URL.Path == "/api/presence":
			_, _ = io.WriteString(w, `{"id":"pc_1"}`)
		case strings.HasSuffix(r.URL.Path, "/steer"):
			steered.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"steered":true}`)
		case strings.HasSuffix(r.URL.Path, "/interrupt"):
			stopped.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"interrupted":true}`)
		case strings.HasSuffix(r.URL.Path, "/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	steers := make(chan string, 1)
	stops := make(chan struct{}, 1)
	if err := NewClient(srv.URL).Steer(context.Background(), "th_1", "  "); err != nil {
		t.Fatal(err)
	}
	steers <- "focus"
	stops <- struct{}{}
	out := make(chan notificationMsg, 4)
	go pumpClient(ctx, ClientConfig{BaseURL: srv.URL, Interactive: true}, nil, out, nil, steers, stops)
	deadline := time.Now().Add(2 * time.Second)
	for (steered.Load() == 0 || stopped.Load() == 0) && time.Now().Before(deadline) {
		select {
		case msg := <-out:
			if msg.Err != nil {
				t.Fatalf("engine error: %v", msg.Err)
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if steered.Load() == 0 || stopped.Load() == 0 {
		t.Fatalf("steered=%d stopped=%d", steered.Load(), stopped.Load())
	}
}
