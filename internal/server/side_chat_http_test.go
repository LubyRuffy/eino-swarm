package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestSideChatKeepsConversationHistoryAndFollowupsSeparate(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	path := "/api/threads/" + id
	for _, question := range []string{"What has been established?", "What evidence supports that?"} {
		body := map[string]any{
			"question": question,
			"quote":    "a task excerpt",
			"history":  []map[string]string{{"question": "What is known?", "answer": "A prior answer."}},
		}
		got := h.json(http.MethodPost, path+"/side-chat", body, http.StatusOK)
		if answer, ok := got["answer"].(string); !ok || strings.TrimSpace(answer) == "" {
			t.Fatalf("side chat returned no answer: %v", got)
		}
	}
	turns, turnErr := h.app.Store.ListTurns(id)
	events, eventErr := h.app.Store.ListEvents(id, 0, 10)
	followups, followupErr := h.app.Store.ListFollowups(id)
	if turnErr != nil || eventErr != nil || followupErr != nil ||
		len(turns) != 0 || len(events) != 0 || len(followups) != 0 {
		t.Fatalf("side chat changed the main conversation: turns=%v (%v), events=%v (%v), followups=%v (%v)",
			turns, turnErr, events, eventErr, followups, followupErr)
	}
}

func TestSideChatRejectsInvalidRequestsAtHTTPBoundary(t *testing.T) {
	h := newHarness(t)
	path := "/api/threads/" + h.newThread() + "/side-chat"
	for _, tc := range []struct {
		name   string
		body   string
		origin string
		status int
		code   string
	}{
		{"malformed", "{", "", http.StatusBadRequest, "invalid_side_chat"},
		{"trailing", `{"question":"hello"} null`, "", http.StatusBadRequest, "invalid_side_chat"},
		{"empty", `{"question":"  "}`, "", http.StatusBadRequest, "invalid_side_chat"},
		{"oversized", `{"question":"` + strings.Repeat("x", 384<<10) + `"}`, "", http.StatusRequestEntityTooLarge, "invalid_side_chat"},
		{"invalid history", `{"question":"hello","history":[{"question":"a"}]}`, "", http.StatusBadRequest, "invalid_side_chat"},
		{"incomplete model selection", `{"question":"hello","model":"chosen"}`, "", http.StatusBadRequest, "invalid_side_chat"},
		{"foreign origin", `{"question":"hello"}`, "https://foreign.invalid", http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, bytes.NewBufferString(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", tc.origin)
			resp, err := h.srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.status, raw)
			}
			if tc.code != "" {
				var got struct{ Code string }
				if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || got.Code != tc.code {
					t.Fatalf("code %q, want %q: %v", got.Code, tc.code, err)
				}
			}
		})
	}
	h.json(http.MethodPost, "/api/threads/th_missing/side-chat", map[string]string{"question": "hello"}, http.StatusNotFound)
}

func TestSideChatCanAnswerWhileTheMainTurnAwaitsHumanInput(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })
	h := newHarness(t)
	id := h.newThread()
	path := "/api/threads/" + id
	accepted := h.json(http.MethodPost, path+"/turns", map[string]string{"text": "Ask me before deciding how to proceed."}, http.StatusAccepted)
	turnID := accepted["turn"].(map[string]any)["id"].(string)
	// The real provider loopback test covers an active model call. Here the
	// main task is paused for an explicit human answer, which side chat must
	// never supply as a side effect of sending a question.
	waitHTTPRestartAsk(t, h, id, turnID)
	before, err := h.app.Store.ListTurns(id)
	if err != nil || len(before) != 1 {
		t.Fatalf("expected one paused turn: %v, %v", before, err)
	}
	h.json(http.MethodPost, path+"/side-chat", map[string]string{"question": "Why does this task need input?"}, http.StatusOK)
	status := h.app.Engine.Status(id)
	turns, err := h.app.Store.ListTurns(id)
	if err != nil || len(turns) != 1 || turns[0].ID != before[0].ID ||
		turns[0].Status != store.TurnRunning || !status.AwaitingAnswer {
		t.Fatalf("side chat changed the paused main task: turns=%v, status=%+v, err=%v", turns, status, err)
	}
}

func TestSideChatProviderFailuresAndCancellationStaySeparateFromLiveTask(t *testing.T) {
	const mainText = "Evaluate the competing design constraints."
	mainEntered := make(chan struct{}, 1)
	finishMain := make(chan struct{})
	releaseMain := sync.OnceFunc(func() { close(finishMain) })
	defer releaseMain()
	signalMain := sync.OnceFunc(func() { close(mainEntered) })
	sideEntered := make(chan struct{}, 4)
	sideCancelled := make(chan struct{}, 4)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var input struct {
			Stream   bool
			Tools    []json.RawMessage
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			t.Error(err)
			return
		}
		switch {
		case len(input.Messages) > 0 && strings.HasPrefix(input.Messages[0].Content, "## Schedule answer claims"):
			slashReply(w, input.Stream, map[string]any{"role": "assistant", "content": `{"claims":[]}`}, "stop")
		case input.Stream && len(input.Tools) > 0:
			if bytes.Contains(raw, []byte("side-question-marker")) {
				t.Error("side question reached the main task")
			}
			signalMain()
			select {
			case <-finishMain:
			case <-r.Context().Done():
				t.Error("main model call was canceled")
				return
			}
			slashReply(w, true, map[string]any{"role": "assistant", "content": "The main task finished."}, "stop")
		case bytes.Contains(raw, []byte("side-question-marker")):
			if len(input.Tools) != 0 || !bytes.Contains(raw, []byte(mainText)) {
				t.Error("side model call lacked task context or acquired tools")
			}
			if bytes.Contains(raw, []byte("provider-failure")) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"private-upstream-detail","type":"invalid_request_error"}}`)
				return
			}
			sideEntered <- struct{}{}
			<-r.Context().Done()
			sideCancelled <- struct{}{}
		default:
			slashReply(w, input.Stream, map[string]any{"role": "assistant", "content": "No additional work is needed."}, "stop")
		}
	}))
	t.Cleanup(fixture.Close)
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	h, _ := newRestartAskHTTPHarness(t, t.TempDir(), fixture.URL)
	id := h.newThread()
	path := "/api/threads/" + id
	accepted := h.json(http.MethodPost, path+"/turns", map[string]string{"text": mainText}, http.StatusAccepted)
	turnID := accepted["turn"].(map[string]any)["id"].(string)
	waitSideChatSignal(t, mainEntered, "main model request")

	failed := h.json(http.MethodPost, path+"/side-chat",
		map[string]string{"question": "side-question-marker provider-failure"}, http.StatusBadGateway)
	if failed["code"] != "side_chat_unavailable" || strings.Contains(failed["error"].(string), "private-upstream-detail") {
		t.Fatalf("upstream error was not redacted: %+v", failed)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 4)
	for range cap(results) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.srv.URL+path+"/side-chat",
			strings.NewReader(`{"question":"side-question-marker waiting"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		go func() {
			resp, err := h.srv.Client().Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			results <- err
		}()
	}
	for range cap(results) {
		waitSideChatSignal(t, sideEntered, "parallel side model request")
	}
	busy := h.json(http.MethodPost, path+"/side-chat", map[string]string{"question": "Another question"}, http.StatusTooManyRequests)
	if busy["code"] != "side_chat_busy" {
		t.Fatalf("wrong side capacity response: %+v", busy)
	}

	releaseMain()
	var turn store.Turn
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.app.Store.GetTurn(turnID)
		if err != nil {
			t.Fatal(err)
		}
		turn = *got
		if turn.Status != store.TurnRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if turn.ID != turnID || turn.Status != store.TurnDone || turn.Final != "The main task finished." {
		t.Fatalf("main task did not finish with its original identity: %+v", turn)
	}
	cancel()
	for range cap(results) {
		waitSideChatSignal(t, sideCancelled, "side model cancellation")
		select {
		case err := <-results:
			if err == nil {
				t.Fatal("canceled HTTP request unexpectedly succeeded")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("canceled HTTP request did not return")
		}
	}
}

func TestSideChatStreamsSelectedModelBeforeCompletionAndCancelsIndependently(t *testing.T) {
	const first = "Evidence so far: "
	const last = "已检查。"
	const selected = "side-only-model"
	finish := make(chan struct{})
	cancelled := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string
			Stream   bool
			Tools    []json.RawMessage
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Model != selected || !req.Stream || len(req.Tools) != 0 || len(req.Messages) != 3 {
			t.Errorf("wrong isolated side model request: %+v", req)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		delta := func(text string) {
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"index": 0, "delta": map[string]string{"content": text},
			}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			w.(http.Flusher).Flush()
		}
		delta(first)
		switch req.Messages[2].Content {
		case "cancel":
			<-r.Context().Done()
			close(cancelled)
		case "fail":
			_, _ = io.WriteString(w, "data: private-provider-error\n\n")
		default:
			select {
			case <-finish:
				delta(last)
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			case <-r.Context().Done():
			}
		}
	}))
	t.Cleanup(fixture.Close)
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	h, _ := newRestartAskHTTPHarness(t, t.TempDir(), fixture.URL)
	id := h.newThread()
	before, err := h.app.Store.GetThread(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range []string{"complete", "cancel", "fail"} {
		t.Run(question, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			body, _ := json.Marshal(map[string]string{
				"question": question, "provider_id": h.app.Config.Models.Providers[0].ID, "model": selected,
			})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.srv.URL+"/api/threads/"+id+"/side-chat", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept", "text/event-stream")
			resp, err := h.srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("wrong stream response: %d %v", resp.StatusCode, resp.Header)
			}
			reader := bufio.NewReader(resp.Body)
			frame := func(kind string, payload map[string]string) {
				t.Helper()
				event, err := reader.ReadString('\n')
				if err != nil || event != "event: "+kind+"\n" {
					t.Fatalf("wanted %s event, got %q: %v", kind, event, err)
				}
				line, _ := reader.ReadString('\n')
				var data map[string]string
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
					t.Fatalf("bad SSE data: %q: %v", line, err)
				}
				for key, expected := range payload {
					if data[key] != expected {
						t.Fatalf("expected %s=%q, got %v", key, expected, data)
					}
				}
				if blank, err := reader.ReadString('\n'); err != nil || blank != "\n" {
					t.Fatalf("unterminated SSE frame: %q: %v", blank, err)
				}
			}
			// The fixture cannot send the second chunk until this one arrives.
			frame("delta", map[string]string{"text": first})
			switch question {
			case "complete":
				close(finish)
				frame("delta", map[string]string{"text": last})
				frame("done", map[string]string{"answer": first + last})
			case "cancel":
				cancel()
				waitSideChatSignal(t, cancelled, "provider stream cancellation")
			case "fail":
				frame("error", map[string]string{"code": "side_chat_unavailable", "error": "Side chat could not get an answer."})
			}
		})
	}
	after, err := h.app.Store.GetThread(id)
	if err != nil || after.ProviderID != before.ProviderID || after.Model != before.Model {
		t.Fatalf("side selection changed the main conversation: before=%+v after=%+v err=%v", before, after, err)
	}
}

func waitSideChatSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
