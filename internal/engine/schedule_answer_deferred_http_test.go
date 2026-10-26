package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The manager proceeds with the original live worker and actual tool receipt,
// while only the classifier times out. A different model on that same origin
// must remain usable before, between and after those failures.
func TestClassifierHeaderOutageYieldsGoalWorkWithoutBlockingAnotherConversation(t *testing.T) {
	const withheld = "The private inspection candidate is awaiting verification."
	const healthy = "The independent inspection completed."
	var managers, workers, checks atomic.Int32
	workerStarted := make(chan struct{}, 1)
	continued := make(chan []*schema.Message, 1)
	stop := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Model    string            `json:"model"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) == 0 {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if req.Messages[0].Content == scheduleAnswerPrompt {
			var candidate struct {
				Messages []string `json:"messages"`
			}
			if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 || json.Unmarshal([]byte(req.Messages[1].Content), &candidate) != nil || len(candidate.Messages) != 1 {
				http.Error(w, "invalid classifier protocol", http.StatusBadRequest)
				return
			}
			switch candidate.Messages[0] {
			case withheld:
				checks.Add(1)
				<-r.Context().Done()
			case healthy:
				writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			default:
				http.Error(w, "unknown candidate", http.StatusBadRequest)
			}
			return
		}
		if req.Model == "independent-fixture-model" {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": healthy}, "stop")
			return
		}
		manager := false
		for _, tool := range req.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			if !req.Stream {
				writeContextFlowReply(w, false, map[string]any{"content": "The inspection remains in progress."}, "stop")
				return
			}
			workers.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			workerStarted <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-stop:
			}
			return
		}
		switch managers.Add(1) {
		case 1:
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "one-delegation", "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": `{"role":"inspector","task":"Inspect the current state."}`}}}}, "tool_calls")
		case 2:
			select {
			case <-workerStarted:
			case <-r.Context().Done():
				return
			}
			writeContextFlowReply(w, true, map[string]any{"content": withheld}, "stop")
		case 3:
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "later-inbox-read", "type": "function", "function": map[string]any{"name": "read_task_messages", "arguments": `{}`}}}}, "tool_calls")
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			continued <- req.Messages
			select {
			case <-r.Context().Done():
			case <-stop:
			}
		}
	}))
	t.Cleanup(func() { close(stop); endpoint.Close() })
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "goal-fixture-model"
	e.cfg.Models.Providers[0].FirstByteTimeoutSeconds = 1
	e.cfg.Models.Providers[0].TimeoutSeconds = 60
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("continue actual work", "", "")
	if err := e.SetThreadGoal(th.ID, "Inspect the current state until evidence is available."); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Interrupt(th.ID) }()
	deadline := time.Now().Add(25 * time.Second)
	lastCheck := int32(-1)
	var next []*schema.Message
	for next == nil && time.Now().Before(deadline) {
		if n := checks.Load(); n != lastCheck {
			lastCheck = n
			other, _ := e.CreateThread("independent work", "", "")
			if err := e.SetThreadProvider(other.ID, e.cfg.Models.Providers[0].ID, "independent-fixture-model"); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			second, err := e.StartTurn(other.ID, "Inspect independently.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, second.ID)
			if got.Status != store.TurnDone || got.Final != healthy || time.Since(started) > 2*time.Second {
				t.Fatalf("independent work blocked: %+v", got)
			}
			events, _ := e.store.ListTurnEvents(second.ID)
			for _, ev := range events {
				if ev.Kind == KindModelBackoff {
					t.Fatalf("classifier cooled independent work: %+v", ev)
				}
			}
		}
		select {
		case next = <-continued:
		case <-time.After(10 * time.Millisecond):
		}
	}
	if next == nil || managers.Load() != 4 || workers.Load() != 1 || checks.Load() != config.DefaultScheduleAnswerFailures {
		t.Fatalf("classification froze or replayed goal work: managers=%d workers=%d checks=%d", managers.Load(), workers.Load(), checks.Load())
	}
	encoded, _ := json.Marshal(next)
	if strings.Contains(string(encoded), withheld) || !strings.Contains(string(encoded), "one-delegation") || !strings.Contains(string(encoded), scheduleAnswerDeferredCue) {
		t.Fatalf("continuation lost real receipts or retained withheld prose: %s", encoded)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	progress := reg.SnapshotWorkers()
	if len(progress) != 1 || progress[0].Finished || !e.Status(th.ID).Running {
		t.Fatalf("classification changed worker/turn ownership: %+v", progress)
	}
	rows, _ := e.store.ListPendingScheduleAnswers(turn.ID)
	if len(rows) != 1 || rows[0].State != "deferred" || rows[0].ClassificationFailures != config.DefaultScheduleAnswerFailures || !strings.Contains(rows[0].Payload, withheld) {
		t.Fatalf("candidate not retained durably: %+v", rows)
	}
	assertPendingScheduleAnswerIsPrivate(t, e, th.ID, turn.ID)
	events, _ := e.store.ListTurnEvents(turn.ID)
	for _, ev := range events {
		if strings.Contains(ev.Text, withheld) {
			t.Fatalf("deferred prose entered public history: %+v", ev)
		}
		if ev.Kind == KindModelRetry {
			t.Fatalf("classification spent correction allowance: %+v", ev)
		}
		if ev.Kind == KindModelBackoff && !strings.Contains(ev.Text, `"scope":"schedule_answer"`) {
			t.Fatalf("classification moved origin health: %+v", ev)
		}
	}
	if strings.Contains(replayDump(t, e, th.ID), withheld) {
		t.Fatal("deferred prose reached model replay")
	}
	// A later real tool call is newer than the deferred candidate. Handoff
	// must retain that checkpoint rather than restoring the old private prefix.
	resumed := coldRestartReplayEngine(t, e)
	if count, err := resumed.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume=%d error=%v", count, err)
	}
	defer func() { _ = resumed.Interrupt(th.ID) }()
	select {
	case request := <-continued:
		encoded, _ := json.Marshal(request)
		if !strings.Contains(string(encoded), "later-inbox-read") || strings.Contains(string(encoded), withheld) {
			t.Fatalf("handoff replaced newer work with deferred history: %s", encoded)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deferred answer blocked the handed-off manager")
	}
	if checks.Load() != config.DefaultScheduleAnswerFailures {
		t.Fatalf("handoff reclassified exhausted candidate: %d", checks.Load())
	}
	resumedRT := resumed.runtimeFor(th.ID)
	resumedRT.mu.Lock()
	resumedReg := resumedRT.reg
	resumedRT.mu.Unlock()
	resumedWorkers := resumedReg.SnapshotWorkers()
	if len(resumedWorkers) != 1 || resumedWorkers[0].ID != progress[0].ID || resumedWorkers[0].Generation != progress[0].Generation {
		t.Fatalf("handoff changed deferred worker identity: before=%+v after=%+v", progress, resumedWorkers)
	}
}

// An exhausted durable candidate is restored as withheld state, never placed
// back in front of the manager. The request sees only real history and receipts.
func TestDeferredCandidateColdRecoveryContinuesWithoutClassifyingOldProse(t *testing.T) {
	e, managers, checks := newScheduleAnswerTimingFixture(t, 1, 1, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, _ := e.CreateThread("deferred cold recovery", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	// A wake permits the recovered turn to continue without creating a goal's
	// subsequent automatic turns after this fixture's successful answer.
	_, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 90, Title: "Deferred recovery wake", Prompt: "Inspect remaining work.", CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	_, release := ownManagerTestFixture(context.Background(), e.runtimeFor(th.ID), turn.ID, reg)
	id := "deferred-original"
	message := markPendingAnswer(schema.AssistantMessage("A withheld older answer.", nil), id)
	row, failure := e.savePendingScheduleAnswer(turn, "answer", id, pendingScheduleAnswer{AnswerID: id, Text: []string{message.Content}, Final: message.Content, Transcript: []*schema.Message{schema.UserMessage("Inspect the current state."), message}})
	if failure != nil {
		t.Fatal(failure)
	}
	if err := e.store.SetScheduleAnswerRecovery(row.ID, 3, 3, time.Now().Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	release()
	reg.Close()
	e.Shutdown()
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	next := openRestartReplayEngine(t, e.cfg)
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("restore=%d error=%v", count, err)
	}
	got := waitForTurn(t, next, turn.ID)
	if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 1 || checks.Load() != 1 {
		t.Fatalf("deferred answer blocked cold recovery: %+v managers=%d policies=%d", got, managers.Load(), checks.Load())
	}
	if strings.Contains(replayDump(t, next, th.ID), message.Content) {
		t.Fatal("cold recovery published deferred prose")
	}
}
