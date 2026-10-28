package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type answeredRestartMessage struct {
	Role, Content string
	ToolCallID    string `json:"tool_call_id"`
	ToolCalls     []struct {
		ID       string
		Function struct{ Name, Arguments string }
	} `json:"tool_calls"`
}

// A rejected tool invocation never became a human question. A later answered
// question must survive restart without that old error reopening a human wait.
func TestHTTPRestartContinuesAnsweredAskAfterAnEarlierInvalidQuestion(t *testing.T) {
	const rejectedID = "rejected-question"
	const answeredID = "answered-question"
	const arguments = `{"questions":[{"id":"decision","prompt":"Which approach should be used?","options":[{"id":"inspect","label":"Inspect first"},{"id":"verify","label":"Verify first"}]}]}`
	const final = "The selected approach is recorded."
	answeredInput := make(chan []answeredRestartMessage, 1)
	restoredInput := make(chan []answeredRestartMessage, 1)
	finish := make(chan struct{})
	var finishOnce sync.Once
	release := func() { finishOnce.Do(func() { close(finish) }) }
	var managerCalls atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("offline restart fixture received credentials")
		}
		var req struct {
			Stream   bool
			Messages []answeredRestartMessage
			Tools    []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid offline request", http.StatusBadRequest)
			return
		}
		if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, "## Schedule answer claims") {
			slashReply(w, req.Stream, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		manager := false
		for _, tool := range req.Tools {
			manager = manager || tool.Function.Name == engine.ToolAskUser
		}
		if !manager {
			slashReply(w, req.Stream, map[string]any{"content": "No additional work is needed."}, "stop")
			return
		}
		switch step := managerCalls.Add(1); step {
		case 1, 2:
			id, args := rejectedID, "{}"
			if step == 2 {
				id, args = answeredID, arguments
			}
			slashReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": engine.ToolAskUser, "arguments": args},
			}}}, "tool_calls")
		case 3:
			select {
			case answeredInput <- req.Messages:
			case <-r.Context().Done():
				return
			}
			// The real manager has consumed the successful tool result. Hold its
			// next request until the HTTP handoff cancels the old generation.
			<-r.Context().Done()
		case 4:
			select {
			case restoredInput <- req.Messages:
			case <-r.Context().Done():
				return
			}
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
			slashReply(w, req.Stream, map[string]any{"content": final}, "stop")
		default:
			t.Errorf("restart made an unexpected manager request: %d", step)
			http.Error(w, "unexpected manager request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(fixture.Close)
	t.Cleanup(release)
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	first, closeFirst := newRestartAskHTTPHarness(t, dir, fixture.URL)
	if err := first.app.Config.Save(); err != nil {
		t.Fatal(err)
	}
	thread := first.json(http.MethodPost, "/api/threads", map[string]any{"title": "Answered decision restart"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	started := first.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "Inspect the assigned input."}, http.StatusAccepted)
	turnID := started["turn"].(map[string]any)["id"].(string)
	waitHTTPRestartAsk(t, first, thread, turnID)
	answer := map[string]any{"call_id": answeredID, "answers": engine.AskAnswers{"decision": {Answers: []string{"Inspect first"}}}}
	first.json(http.MethodPost, "/api/threads/"+thread+"/answers", answer, http.StatusAccepted)
	select {
	case input := <-answeredInput:
		assertAnsweredRestartInput(t, input, rejectedID, answeredID)
	case <-time.After(5 * time.Second):
		t.Fatal("the accepted answer did not reach the real manager before restart")
	}
	checkpoint := first.json(http.MethodPost, "/api/engine/restart", nil, http.StatusOK)
	if checkpoint["ready"] != true || checkpoint["threads_checkpointed"] != float64(1) {
		t.Fatalf("the answered turn was not durably checkpointed: %+v", checkpoint)
	}
	before, err := first.app.Store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Pause  string
		Answer *struct {
			CallID  string `json:"call_id"`
			Answers engine.AskAnswers
		}
		History []answeredRestartMessage `json:"manager_history"`
	}
	checkpointCount := 0
	for _, event := range before {
		if event.Kind == engine.KindRestartCheckpoint {
			checkpointCount++
			if err := json.Unmarshal([]byte(event.Text), &saved); err != nil {
				t.Fatal(err)
			}
		}
	}
	if checkpointCount == 0 || saved.Pause != "" || saved.Answer == nil || saved.Answer.CallID != answeredID ||
		!reflect.DeepEqual(saved.Answer.Answers["decision"].Answers, []string{"Inspect first"}) {
		t.Fatalf("the actual handoff lost the already-consumed answer: %+v", saved)
	}
	assertAnsweredRestartInput(t, saved.History, rejectedID, answeredID)
	closeFirst()

	// App.Restore exercises startup's filtered recovery snapshot. A full-event
	// ResumeOrphanedTurns fixture can hide missing pre-checkpoint identities.
	next, err := app.New(app.Options{DataDir: dir, NoAssets: true, DeferRestore: true, Mode: server.ModeWeb})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(next.Server.Handler())
	t.Cleanup(func() { ts.Close(); next.Shutdown(context.Background()) })
	second := &harness{t: t, app: next, srv: ts}
	if err := next.Restore(t.Context()); err != nil {
		t.Fatalf("restore the original answered turn: %v", err)
	}
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case input := <-restoredInput:
			assertAnsweredRestartInput(t, input, rejectedID, answeredID)
			goto restored
		case <-ticker.C:
			turn, err := next.Store.GetTurn(turnID)
			if err != nil {
				t.Fatal(err)
			}
			if turn.Status != store.TurnRunning {
				t.Fatalf("restart refused the already-answered question: status=%s error=%q", turn.Status, turn.Error)
			}
		case <-deadline:
			t.Fatal("restart did not continue the already-answered question")
		}
	}

restored:
	status := next.Engine.Status(thread)
	if !status.Running || status.AwaitingAnswer || status.TurnID != turnID {
		t.Fatalf("restoration reopened a human wait or changed the turn: %+v", status)
	}
	duplicate := second.json(http.MethodPost, "/api/threads/"+thread+"/answers", answer, http.StatusConflict)
	if duplicate["code"] != "ask_mismatch" {
		t.Fatalf("the consumed answer was not rejected during continuation: %+v", duplicate)
	}
	release()
	until := time.Now().Add(5 * time.Second)
	for {
		turn, err := next.Store.GetTurn(turnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status != store.TurnRunning {
			if turn.Status != store.TurnDone || turn.Final != final {
				t.Fatalf("the original answered turn did not finish: %+v", turn)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("the original answered turn did not finish after continuation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := managerCalls.Load(); got != 4 {
		t.Fatalf("restart duplicated manager input: calls=%d, want 4", got)
	}
	after, err := next.Store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, event := range after {
		if event.Kind == engine.KindAskRecovered {
			t.Fatal("restart emitted a recovered question for an already-answered call")
		}
		if event.Kind == "tool_result" && event.ToolCallID == answeredID {
			answers++
		}
	}
	if answers != 1 {
		t.Fatalf("the original answer was lost or repeated: %d results", answers)
	}
	for _, original := range before {
		found := false
		for _, event := range after {
			if event.Seq == original.Seq {
				found = event.Kind == original.Kind && event.Text == original.Text && event.ToolCallID == original.ToolCallID
				break
			}
		}
		if !found {
			t.Fatalf("recovery rewrote the original handoff evidence: %+v", original)
		}
	}
	turns, err := next.Store.ListTurns(thread)
	if err != nil || len(turns) != 1 || turns[0].ID != turnID {
		t.Fatalf("restoration created another turn: %+v err=%v", turns, err)
	}
}

func assertAnsweredRestartInput(t *testing.T, input []answeredRestartMessage, rejectedID, answeredID string) {
	t.Helper()
	calls := map[string]int{}
	results := map[string]int{}
	for _, message := range input {
		for _, call := range message.ToolCalls {
			if call.Function.Name == engine.ToolAskUser {
				calls[call.ID]++
			}
		}
		if message.Role != "tool" {
			continue
		}
		switch message.ToolCallID {
		case rejectedID:
			var failure struct {
				OK    bool
				Error string
			}
			if err := json.Unmarshal([]byte(message.Content), &failure); err != nil || failure.OK || failure.Error != "ask_user: questions are required" {
				t.Fatalf("the invalid question's actual failure was changed: %q err=%v", message.Content, err)
			}
		case answeredID:
			var result struct{ Answers engine.AskAnswers }
			if err := json.Unmarshal([]byte(message.Content), &result); err != nil ||
				!reflect.DeepEqual(result.Answers["decision"].Answers, []string{"Inspect first"}) {
				t.Fatalf("the original human answer was changed: %q err=%v", message.Content, err)
			}
		default:
			continue
		}
		results[message.ToolCallID]++
	}
	for _, id := range []string{rejectedID, answeredID} {
		if calls[id] != 1 || results[id] != 1 {
			t.Fatalf("the real model lost or duplicated %q call/result: %d/%d", id, calls[id], results[id])
		}
	}
}
