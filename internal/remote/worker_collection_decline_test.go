package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The receipt must cross the real engine bus and encrypted watch. A manually
// projected event would miss the forwarding allowlist that caused the gap.
func TestWorkerCollectionDeclineReachesActualPairlinkWatchAndReplay(t *testing.T) {
	var managers, workers atomic.Int32
	workerEntered, workerCancelled := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream   bool
			Messages []struct{ Role, Content string }
			Tools    []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture input", 400)
			return
		}
		if !request.Stream && len(request.Messages) > 0 && strings.HasPrefix(request.Messages[0].Content, "## Schedule answer claims") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": `{"claims":[]}`}, "finish_reason": "stop"}}})
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(delta map[string]any, reason any) {
			body, _ := json.Marshal(map[string]any{"id": "collection-fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": reason}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		}
		if !manager {
			if workers.Add(1) != 1 {
				t.Error("worker was replaced")
				return
			}
			emit(map[string]any{"role": "assistant", "reasoning_content": "Inspecting the assigned work."}, nil)
			w.(http.Flusher).Flush()
			close(workerEntered)
			<-r.Context().Done()
			close(workerCancelled)
			return
		}
		step := managers.Add(1)
		if step == 1 {
			emit(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "spawn-worker", "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": `{"role":"verifier","task":"Inspect the assigned work."}`}}}}, nil)
			emit(map[string]any{}, "tool_calls")
		} else {
			if step > 3 {
				t.Error("stopped manager resumed")
			}
			emit(map[string]any{"role": "assistant", "content": "The assigned review is complete."}, nil)
			emit(map[string]any{}, "stop")
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "collection-fixture"
	cfg.Models.Providers = []config.Provider{{ID: "collection-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := engine.New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	th, err := e.CreateThread("Collection receipt forwarding", "", "")
	if err != nil {
		t.Fatal(err)
	}
	link := newFollowupRelay(t, e)
	if ready := link.rpc(t, Request{ID: "watch-collection", Op: OpWatch, ThreadID: th.ID}); !ready.OK || ready.Op != OpReady {
		t.Fatalf("watch=%+v", ready)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the assigned work.")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !e.Status(th.ID).AwaitingContinue && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !e.Status(th.ID).AwaitingContinue {
		t.Fatal("actual manager did not reach collection pause")
	}
	select {
	case <-workerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual worker HTTP stream never entered")
	}
	if err = e.ContinueTurn(th.ID, false); err != nil {
		t.Fatal(err)
	}
	var live Response
	terminal := false
	receivedDeadline := time.After(5 * time.Second)
	for !terminal {
		response := link.receive(t, receivedDeadline)
		if response.Event == nil {
			continue
		}
		if response.Event.Kind == "workers_collection_declined" {
			live = response
		}
		terminal = response.Event.Kind == "error" && response.Event.TurnID == turn.ID
	}
	events, err := st.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var durable *store.Event
	for i := range events {
		if events[i].Kind == "workers_collection_declined" {
			if durable != nil {
				t.Fatal("duplicate durable decline")
			}
			durable = &events[i]
		}
	}
	if durable == nil {
		t.Fatal("typed Stop did not persist its actual decline")
	}
	if live.Op != OpEvent || live.Event == nil {
		t.Fatalf("durable collection decline seq=%d was missing from actual Pairlink watch; shouldPush=%v", durable.Seq, shouldPush(durable.Kind))
	}
	if live.Seq != durable.Seq || live.Event.Seq != durable.Seq || live.Event.TurnID != turn.ID || live.Event.ThreadID != th.ID || live.Event.Text != durable.Text {
		t.Fatalf("live receipt changed owner, seq or body: %+v durable=%+v", live, durable)
	}
	var decision struct {
		WaitID  string `json:"wait_id"`
		Workers []struct {
			ID         string `json:"agent_id"`
			Generation int    `json:"generation"`
			Running    bool   `json:"running"`
		} `json:"workers"`
	}
	if err = json.Unmarshal([]byte(live.Event.Text), &decision); err != nil {
		t.Fatal(err)
	}
	if decision.WaitID == "" || len(decision.Workers) != 1 || decision.Workers[0].ID == "" || decision.Workers[0].Generation != 1 || !decision.Workers[0].Running {
		t.Fatalf("pre-Stop snapshot changed: %+v", decision)
	}
	select {
	case <-workerCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("typed Stop did not cancel original HTTP worker")
	}
	for e.Status(th.ID).Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if e.Status(th.ID).Running {
		t.Fatal("manager finalization did not release its runtime")
	}
	if managers.Load() != 3 || workers.Load() != 1 {
		t.Fatalf("Stop changed execution: manager=%d worker=%d", managers.Load(), workers.Load())
	}
	ready := link.rpc(t, Request{ID: "rewatch-collection", Op: OpWatch, ThreadID: th.ID})
	found := false
	for _, event := range ready.Events {
		found = found || event.Kind == durable.Kind && event.Seq == durable.Seq && event.Text == durable.Text
	}
	if !ready.OK || !found || ready.Status == nil || ready.Status.Running {
		t.Fatalf("cold watch lost internal receipt or revived Stop: %+v", ready)
	}
	page := link.rpc(t, Request{ID: "page-collection", Op: OpLog, ThreadID: th.ID, Before: durable.Seq + 1})
	found = false
	for _, event := range page.Events {
		found = found || event.Kind == durable.Kind && event.Seq == durable.Seq && event.Text == durable.Text
	}
	if !page.OK || !found {
		t.Fatalf("OpLog lost the forwarded diagnostic: %+v", page)
	}
	// Optional private evidence binds the actual wire event to a phone reducer
	// check without requiring Node or a phone toolchain in ordinary Go tests.
	if path := os.Getenv("ZWAI_TEST_COLLECTION_WIRE_EVIDENCE"); path != "" {
		raw, err := json.Marshal(map[string]any{"live": live, "ready": ready, "page": page, "durable": durable})
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

func TestPhoneEarlierSkipsCollectionDeclineOnlyPagesButPreservesMixedDiagnostics(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			e := testEngine(t)
			th, err := e.CreateThread("Collection history paging", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: th.ID}
			if err = e.Store().CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			appendEvent := func(kind, text string) store.Event {
				event := store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", Kind: kind, Text: text}
				if err := e.Store().AppendEvent(&event); err != nil {
					t.Fatal(err)
				}
				return event
			}
			appendEvent("agent_message", "Earlier visible answer")
			for i := 0; i < 5; i++ {
				appendEvent("workers_collection_declined", `{"wait_id":"wait","worker_ids":["worker"]}`)
			}
			if mixed {
				appendEvent("agent_message", "Newest visible answer")
			}
			tail, _, err := e.Store().ListTailEvents(th.ID, 0, 1)
			if err != nil {
				t.Fatal(err)
			}
			response := Handle(e, config.RemoteConfig{WatchEvents: 2}, Request{ID: "older", Op: OpLog, ThreadID: th.ID, Before: tail[0].Seq + 1}, "relay", "session")
			if !response.OK {
				t.Fatalf("page=%+v", response)
			}
			expected := "Earlier visible answer"
			if mixed {
				expected = "Newest visible answer"
			}
			visible, receipt := false, false
			for _, event := range response.Events {
				visible = visible || event.Text == expected
				receipt = receipt || event.Kind == "workers_collection_declined"
			}
			if !visible {
				t.Fatalf("Earlier stopped on a client-invisible receipt page: %+v", response)
			}
			if mixed && !receipt {
				t.Fatalf("mixed visible page discarded its diagnostic receipt: %+v", response)
			}
		})
	}
}
