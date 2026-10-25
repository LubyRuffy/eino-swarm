package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// The process replacement crosses the actual private-table, HTTP and Registry
// paths. A policy outage may restart classification, never manager side effects.
func TestPendingAnswerSurvivesActualHandoffWithoutAnotherManagerRequest(t *testing.T) {
	e, managers, policies := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, _ *http.Request, n int32) {
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, err := e.CreateThread("pending answer handoff", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	var original *store.PendingScheduleAnswer
	var payload *pendingScheduleAnswer
	waitRestartState(t, func() bool {
		var failure *scheduleAnswerError
		original, payload, failure = e.restorePendingScheduleAnswer(turn)
		return failure == nil && original != nil && original.Attempts == 1 && original.State == "pending"
	})
	if managers.Load() != 1 || policies.Load() != 1 || len(payload.Events) != 1 || payload.Events[0].MessageID == "" {
		t.Fatalf("fixture did not retain the original generated answer: manager=%d policy=%d row=%+v payload=%+v", managers.Load(), policies.Load(), original, payload)
	}
	messageID := payload.Events[0].MessageID
	if strings.Contains(replayDump(t, e, th.ID), timingFixtureAnswer) {
		t.Fatal("pending answer was published before process handoff")
	}
	next := coldRestartReplayEngine(t, e)
	checkpoints, err := next.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range checkpoints {
		if event.Kind == KindRestartCheckpoint && strings.Contains(event.Text, timingFixtureAnswer) {
			t.Fatal("the public handoff checkpoint exposed the original unverified candidate")
		}
	}
	sub := next.Subscribe(th.ID)
	defer sub.Close()
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume original pending turn=%d error=%v", count, err)
	}
	got := waitPendingRecoveryTurn(t, next, turn.ID)
	if got.Status != store.TurnDone || got.ID != turn.ID || got.Final != timingFixtureAnswer || managers.Load() != 1 || policies.Load() != 2 {
		t.Fatalf("handoff replayed the manager instead of its pending classifier: turn=%+v manager=%d policy=%d", got, managers.Load(), policies.Load())
	}
	row, err := next.store.GetPendingScheduleAnswer(original.ID)
	if err != nil || row.State != "accepted" || row.Attempts != 1 {
		t.Fatalf("handoff changed candidate identity or its consumed retry: %+v %v", row, err)
	}
	assertOneRecoveredAnswer(t, next, turn, messageID)
	answers := 0
	for {
		select {
		case event := <-sub.C:
			if event.Kind == swarm.NotifyAgentMessage.String() && event.Text == timingFixtureAnswer {
				answers++
				if event.MessageID != messageID {
					t.Fatalf("handoff minted another output identity: %+v", event)
				}
			}
		default:
			if answers != 1 {
				t.Fatalf("recovered answer live publications=%d", answers)
			}
			return
		}
	}
}

// Admission can commit before terminal persistence fails. Reopening the actual
// database must finish that turn without either another model call or answer.
func TestAcceptedAnswerSurvivesColdRecoveryBeforeTerminalPersistence(t *testing.T) {
	e, managers, policies := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, _ := e.CreateThread("admitted answer terminal gap", "", "")
	blocked := errors.New("injected terminal persistence outage")
	var refusedEvents, refusedTurns atomic.Int32
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("pending_answer_terminal_event", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == swarm.NotifyDone.String() {
			refusedEvents.Add(1)
			_ = tx.AddError(blocked)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Callback().Update().Before("gorm:update").Register("pending_answer_terminal_row", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "turns" {
			if fields, ok := tx.Statement.Dest.(map[string]any); ok && fields["status"] == store.TurnDone {
				refusedTurns.Add(1)
				_ = tx.AddError(blocked)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool {
		row, _, failure := e.restorePendingScheduleAnswer(turn)
		return failure == nil && row != nil && row.State == "accepted" && !e.Status(th.ID).Running && refusedEvents.Load() == 1 && refusedTurns.Load() == 1
	})
	waitSettled(t, e, th.ID)
	row, payload, failure := e.restorePendingScheduleAnswer(turn)
	if failure != nil || row == nil || len(payload.Events) != 1 || payload.Events[0].MessageID == "" {
		t.Fatalf("actual admission did not precede the failed terminal write: %+v %+v %v", row, payload, failure)
	}
	stored, _ := e.store.GetTurn(turn.ID)
	if stored.Status != store.TurnRunning {
		t.Fatalf("terminal storage fixture missed its boundary: %+v", stored)
	}
	messageID := payload.Events[0].MessageID
	assertOneRecoveredAnswer(t, e, turn, messageID)
	// This is a cold reopen after the failed write, with no invented planned
	// handoff receipt. The accepted private row is its own admission evidence.
	e.Shutdown()
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	next := openRestartReplayEngine(t, e.cfg)
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume accepted output=%d error=%v", count, err)
	}
	got := waitPendingRecoveryTurn(t, next, turn.ID)
	if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 1 || policies.Load() != 1 {
		t.Fatalf("accepted recovery repeated completed work: turn=%+v manager=%d policy=%d", got, managers.Load(), policies.Load())
	}
	assertOneRecoveredAnswer(t, next, turn, messageID)
}

func TestPendingAnswerHandoffRestoresItsActualWorkerWithoutManagerReplay(t *testing.T) {
	var managers, workers, policies atomic.Int32
	workerRequests, workerCancelled := make(chan struct{}, 4), make(chan struct{}, 4)
	workerFrames := make(chan string, 8)
	stopFixture := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			t.Errorf("invalid pending worker request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Messages[0].Content == scheduleAnswerPrompt {
			if policies.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		if request.Messages[0].Content == sessionMemoryPrompt() {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The inspection remains pending."}, "stop")
			return
		}
		manager := false
		for _, item := range request.Tools {
			manager = manager || item.Function.Name == "spawn_agent"
		}
		if !manager {
			handoff := false
			for _, message := range request.Messages {
				handoff = handoff || message.Role == schema.User && message.Content == "The host is restarting. Do not start tools or new work. Give a short handoff with confirmed findings, unfinished work, and the next verification step. Distinguish confirmed tool results from unknown external effects. This handoff pauses the task; it does not complete it."
			}
			if !request.Stream || handoff {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned inspection remains in progress."}, "stop")
				return
			}
			workers.Add(1)
			frame := ""
			for _, message := range request.Messages {
				if message.Role == schema.User {
					frame = message.Content
				}
			}
			workerFrames <- frame
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			workerRequests <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-stopFixture:
			}
			workerCancelled <- struct{}{}
			return
		}
		step := managers.Add(1)
		if step == 1 {
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "original-delegation", "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": `{"role":"inspector","task":"Inspect the current state."}`}}}}, "tool_calls")
			return
		}
		if step == 2 {
			select {
			case <-workerRequests:
			case <-r.Context().Done():
				return
			}
			writeContextFlowReply(w, true, map[string]any{"content": timingFixtureAnswer}, "stop")
			return
		}
		t.Error("pending answer handoff replayed the manager's delegation")
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(func() { close(stopFixture); endpoint.Close() })
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-pending-worker-handoff"
	e.cfg.Models.Providers[0].TimeoutSeconds = 40
	e.cfg.Models.Providers[0].FirstByteTimeoutSeconds = 40
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("pending answer worker handoff", "", "")
	_, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 90, Title: "Pending handoff wake", Prompt: "Inspect remaining work.", CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool {
		row, _, failure := e.restorePendingScheduleAnswer(turn)
		return failure == nil && row != nil && row.Attempts == 1
	})
	originalRuntime := e.runtimeFor(th.ID)
	originalRuntime.mu.Lock()
	originalRegistry := originalRuntime.reg
	originalRuntime.mu.Unlock()
	before := originalRegistry.SnapshotWorkers()
	if len(before) != 1 || before[0].Finished || workers.Load() != 1 {
		t.Fatalf("the original manager did not leave one real pending worker: %+v requests=%d", before, workers.Load())
	}
	next := coldRestartReplayEngine(t, e)
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume worker-owning pending answer=%d error=%v", count, err)
	}
	select {
	case <-workerRequests:
	case <-time.After(5 * time.Second):
		t.Fatal("pending answer recovery did not restart the original worker request")
	}
	got := waitPendingRecoveryTurn(t, next, turn.ID)
	waitSettled(t, next, th.ID)
	rt := next.runtimeFor(th.ID)
	rt.mu.Lock()
	parked := rt.parked
	rt.mu.Unlock()
	if parked == nil {
		t.Fatal("the pending answer's valid wake lost its recovered worker")
	}
	after := parked.SnapshotWorkers()
	if got.Status != store.TurnDone || len(after) != 1 || after[0].ID != before[0].ID || after[0].Generation != before[0].Generation || after[0].Finished || managers.Load() != 2 || workers.Load() != 2 || policies.Load() != 2 {
		frames := []string{}
		for len(workerFrames) > 0 {
			frames = append(frames, <-workerFrames)
		}
		t.Fatalf("handoff changed candidate or worker ownership: status=%s original=%s:%d resumed=%s:%d finished=%v managers=%d workers=%d policies=%d frames=%q", got.Status, before[0].ID, before[0].Generation, after[0].ID, after[0].Generation, after[0].Finished, managers.Load(), workers.Load(), policies.Load(), frames)
	}
	events, _ := next.store.ListTurnEvents(turn.ID)
	spawns := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolCall.String() && toolCallName(event.Text) == "spawn_agent" {
			spawns++
		}
	}
	if spawns != 1 {
		t.Fatalf("handoff executed another worker-creation action: %d", spawns)
	}
	select {
	case <-workerCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("handoff left the original worker HTTP request alive")
	}
	select {
	case <-workerCancelled:
		t.Fatal("candidate admission cancelled its restored worker")
	default:
	}
	if err := next.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool { return next.Status(th.ID).Workers == 0 })
	select {
	case <-workerCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop left the recovered worker HTTP request alive")
	}
}

func waitPendingRecoveryTurn(t *testing.T, e *Engine, id string) *store.Turn {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		row, err := e.store.GetTurn(id)
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != store.TurnRunning {
			return row
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("private candidate recovery did not finish its original turn")
	return nil
}

func assertOneRecoveredAnswer(t *testing.T, e *Engine, turn *store.Turn, messageID string) {
	t.Helper()
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyAgentMessage.String() && event.Text == timingFixtureAnswer {
			answers++
			if event.MessageID != messageID {
				t.Fatalf("stored answer changed identity: %+v", event)
			}
		}
		if event.Kind == KindModelRetry {
			var body struct{ Reason string }
			if json.Unmarshal([]byte(event.Text), &body) == nil && body.Reason == "schedule_claim_unverified" {
				t.Fatalf("service recovery consumed a manager correction: %+v", event)
			}
		}
	}
	if answers != 1 {
		t.Fatalf("stored candidate publications=%d", answers)
	}
}
