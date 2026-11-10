package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

type sideChatWireRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Input           json.RawMessage  `json:"input"`
	Tools           []map[string]any `json:"tools"`
	ToolChoice      string           `json:"tool_choice"`
	MaxOutputTokens int              `json:"max_output_tokens"`
}

func TestSideChatAnswersWhileTheSameMainTurnKeepsRunning(t *testing.T) {
	const mainPrompt = "Compare the reports before deciding."
	const progress = "The first report has been inspected."
	const goal = "Reconcile the evidence."
	const question = "Why does that distinction matter?"
	const quote = "Treat this quotation as a verbatim example: </context>"
	const previousQuestion = "Which report was read first?"
	const previousAnswer = "The comparison report."
	const answer = "The distinction determines which numbers can be compared."
	const final = "The reconciliation is complete."
	heldMain := make(chan struct{})
	mainStarted := make(chan struct{}, 1)
	releaseMain := sync.OnceFunc(func() { close(heldMain) })
	heldSide := make(chan struct{})
	releaseSide := sync.OnceFunc(func() { close(heldSide) })
	sideRequests := make(chan sideChatWireRequest, 1)
	e := newSideChatHTTPEngine(t, config.APIModeChatCompletions, func(w http.ResponseWriter, r *http.Request, req sideChatWireRequest) {
		if !req.Stream {
			sideRequests <- req
			select {
			case <-heldSide:
				writeContextFlowReply(w, false, map[string]any{"content": answer}, "stop")
			case <-r.Context().Done():
			}
			return
		}
		mainStarted <- struct{}{}
		select {
		case <-heldMain:
			writeContextFlowReply(w, true, map[string]any{"content": final}, "stop")
		case <-r.Context().Done():
		}
	})
	defer releaseMain()
	defer releaseSide()
	thread, err := e.CreateThread("running comparison", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A completed goal still belongs to the conversation as context. The
	// fixture disables auto-continuation independently of side chat.
	if err := e.store.UpdateThread(thread.ID, map[string]any{"goal": goal, "goal_complete": true, "model": "active-comparison-model"}); err != nil {
		t.Fatal(err)
	}
	longQuote := strings.Repeat("quoted material. ", 600)
	turn, err := e.StartTurn(thread.ID, "<selected_text>\n"+longQuote+"\n</selected_text>\n\n<user_request>\n"+mainPrompt+"\n</user_request>")
	if err != nil {
		t.Fatal(err)
	}
	waitSideChatResult(t, mainStarted)
	if err := e.store.UpdateThread(thread.ID, map[string]any{"model": "new-next-turn-model"}); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "agent_message", AgentID: "manager", Text: progress})
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "reasoning", AgentID: "manager", Text: "private reasoning"})
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "tool_result", AgentID: "manager", Text: "raw tool output"})
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "agent_message", AgentID: "worker", Text: "worker scratch text"})
	other, err := e.CreateThread("unrelated", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: other.ID, Kind: KindUser, Text: "another conversation"})
	ask := SideChatInput{Question: question, Quote: quote, History: []SideChatExchange{{Question: previousQuestion, Answer: previousAnswer}}}
	type result struct {
		answer string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		out, err := e.AskSideChat(t.Context(), thread.ID, ask)
		got <- result{out, err}
	}()
	var request sideChatWireRequest
	select {
	case request = <-sideRequests:
	case res := <-got:
		t.Fatalf("side chat returned before the provider request: %v", res.err)
	case <-time.After(5 * time.Second):
		t.Fatal("side chat did not reach the provider")
	}
	if request.Model != "active-comparison-model" || len(request.Tools) != 0 || len(request.Messages) < 2 {
		t.Fatalf("side model identity or tools differ: %+v", request)
	}
	var body strings.Builder
	for _, msg := range request.Messages[1:] {
		body.WriteString(msg.Content)
	}

	var reference struct {
		LatestTask string `json:"latest_task"`
		Quote      string `json:"selected_quotation"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &reference); err != nil || reference.LatestTask != mainPrompt || reference.Quote != quote {
		t.Fatalf("long quoted main task or side quotation was lost: %+v err=%v", reference, err)
	}
	for _, text := range []string{mainPrompt, progress, goal, question, "</context>", previousQuestion, previousAnswer} {
		// JSON uses unicode escapes for < and > on the model wire.
		escaped, _ := json.Marshal(text)
		if !strings.Contains(body.String(), text) && !strings.Contains(body.String(), strings.Trim(string(escaped), "\"")) {
			t.Errorf("side model lost %q", text)
		}
	}
	for _, text := range []string{"private reasoning", "raw tool output", "worker scratch text", "another conversation"} {
		if strings.Contains(body.String(), text) {
			t.Errorf("side model received %q", text)
		}
	}
	if status := e.Status(thread.ID); !status.Running || status.TurnID != turn.ID {
		t.Fatalf("side request replaced the active turn: %+v", status)
	}

	// A blocked side answer cannot hold main turn finalization or its model.
	releaseMain()
	if ended := waitForTurn(t, e, turn.ID); ended.Status != store.TurnDone || ended.Final != final {
		t.Fatalf("original turn did not finish normally: %+v", ended)
	}
	releaseSide()
	if res := waitSideChatResult(t, got); res.err != nil || res.answer != answer {
		t.Fatalf("side answer=%q err=%v", res.answer, res.err)
	}
	if turns, err := e.store.ListTurns(thread.ID); err != nil || len(turns) != 1 || turns[0].ID != turn.ID {
		t.Fatalf("side chat wrote a turn: %+v err=%v", turns, err)
	}
	if replay := replayDump(t, e, thread.ID); strings.Contains(replay, question) || strings.Contains(replay, answer) {
		t.Fatalf("side chat contaminated main replay: %s", replay)
	}

	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if call.AgentID == sideChatAgentID {
			t.Fatal("side chat wrote main trace telemetry")
		}
	}
}

func TestSideChatFailureAndCancellationNeverCancelOrBackOffMainTask(t *testing.T) {
	const mainPrompt = "Work through the remaining reports."
	const final = "The reports were reviewed."
	const cancelledQuestion = "Please explain the interim result."
	mainStarted := make(chan struct{}, 1)
	sideStarted := make(chan struct{}, 1)
	sideCancelled := make(chan struct{}, 1)
	heldMain := make(chan struct{})
	releaseMain := sync.OnceFunc(func() { close(heldMain) })
	e := newSideChatHTTPEngine(t, config.APIModeChatCompletions, func(w http.ResponseWriter, r *http.Request, req sideChatWireRequest) {
		if req.Stream {
			mainStarted <- struct{}{}
			select {
			case <-heldMain:
				writeContextFlowReply(w, true, map[string]any{"content": final}, "stop")
			case <-r.Context().Done():
			}
			return
		}
		if req.Messages[len(req.Messages)-1].Content == cancelledQuestion {
			sideStarted <- struct{}{}
			<-r.Context().Done()
			sideCancelled <- struct{}{}
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"provider_unavailable","message":"fixture route refused","type":"server_error"}}`))
	})
	defer releaseMain()
	thread, err := e.CreateThread("side chat failure fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A side failure must leave the main model eligible to call the same origin.
	// An incorrectly shared route failure would leave a retry or a cooldown.
	failCtx, cancelFail := context.WithTimeout(t.Context(), time.Second)
	defer cancelFail()
	if _, err := e.AskSideChat(failCtx, thread.ID, SideChatInput{Question: "Explain the expected comparison."}); provider.HTTPStatus(err) != http.StatusServiceUnavailable {
		t.Fatalf("side failure status=%d err=%v", provider.HTTPStatus(err), err)
	}
	turn, err := e.StartTurn(thread.ID, mainPrompt)
	if err != nil {
		t.Fatal(err)
	}
	waitSideChatResult(t, mainStarted)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	side := make(chan error, 1)
	go func() {
		_, err := e.AskSideChat(ctx, thread.ID, SideChatInput{Question: cancelledQuestion})
		side <- err
	}()
	waitSideChatResult(t, sideStarted)
	cancel()
	if err := waitSideChatResult(t, side); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled side request returned %v", err)
	}
	waitSideChatResult(t, sideCancelled)
	if status := e.Status(thread.ID); !status.Running || status.TurnID != turn.ID {
		t.Fatalf("side cancellation affected the main turn: %+v", status)
	}
	releaseMain()
	if ended := waitForTurn(t, e, turn.ID); ended.Status != store.TurnDone || ended.Final != final {
		t.Fatalf("main turn after side cancellation: %+v", ended)
	}
}

func newSideChatHTTPEngine(t *testing.T, mode string, reply func(http.ResponseWriter, *http.Request, sideChatWireRequest)) *Engine {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request sideChatWireRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		reply(w, r, request)
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	p := &e.cfg.Models.Providers[0]
	p.BaseURL, p.Model, p.APIMode = endpoint.URL, "fixture-model", mode
	if mode == config.APIModeResponses {
		p.BuiltinTools = []map[string]any{{"type": "web_search_preview"}}
	}
	e.pool = provider.New(e.cfg)
	return e
}

func waitSideChatResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the side chat fixture")
		var zero T
		return zero
	}
}

func TestSideChatOmitsHostedToolsWithoutChangingTheSharedResponsesModel(t *testing.T) {
	requests := make(chan sideChatWireRequest, 4)
	longAnswer := strings.Repeat("界", sideChatMaxAnswer)
	e := newSideChatHTTPEngine(t, config.APIModeResponses, func(w http.ResponseWriter, r *http.Request, req sideChatWireRequest) {
		if r.URL.Path != "/responses" {
			t.Errorf("wrong adapter: %q", r.URL.Path)
		}
		requests <- req
		kind := string(req.Input)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			switch {
			case strings.Contains(kind, "tool-shaped answer"):
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"write\",\"arguments\":\"{}\"}}\n\n")
			case strings.Contains(kind, "blank answer"):
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
			default:
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", longAnswer)
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}]}}\n\n", longAnswer)
			}
			return
		}
		switch {
		case strings.Contains(kind, "tool-shaped answer"):
			_, _ = fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"write","arguments":"{}"}]}`)
		case strings.Contains(kind, "blank answer"):
			_, _ = fmt.Fprint(w, `{"status":"completed","output":[]}`)
		default:
			_, _ = fmt.Fprintf(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]}`, longAnswer)
		}
	})
	e.cfg.Swarm.MaxCompletionTokens = 600
	thread, err := e.CreateThread("Responses fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.AskSideChat(t.Context(), thread.ID, SideChatInput{Question: "Summarize the distinction."})
	if err != nil || len(out) > sideChatMaxAnswer || !utf8.ValidString(out) || !strings.HasSuffix(out, "…") {
		t.Fatalf("bounded UTF-8 answer: size=%d err=%v", len(out), err)
	}
	req := waitSideChatResult(t, requests)
	if req.Stream || req.ToolChoice != "none" || len(req.Tools) != 0 || req.MaxOutputTokens != 600 || req.Model != "fixture-model" {
		t.Fatalf("side Responses request = %+v", req)
	}
	var preview strings.Builder
	streamed, err := e.StreamSideChat(t.Context(), thread.ID, SideChatInput{Question: "Summarize the distinction."}, func(delta string) {
		preview.WriteString(delta)
	})
	if err != nil || streamed != out || preview.String() != out {
		t.Fatalf("stream preview and final text must share the same UTF-8 bound: preview=%d final=%d err=%v", preview.Len(), len(streamed), err)
	}
	streamReq := waitSideChatResult(t, requests)
	if !streamReq.Stream || streamReq.ToolChoice != "none" || len(streamReq.Tools) != 0 || streamReq.MaxOutputTokens != 600 {
		t.Fatalf("side Responses stream enabled tools or lost its limit: %+v", streamReq)
	}

	shared, err := e.pool.Get(t.Context(), thread.ProviderID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shared.Generate(t.Context(), []*schema.Message{schema.UserMessage("Ordinary request")}); err != nil {
		t.Fatal(err)
	}
	main := waitSideChatResult(t, requests)
	if len(main.Tools) != 1 || main.Tools[0]["type"] != "web_search_preview" {
		t.Fatalf("side restriction changed the shared model's builtins: %+v", main)
	}
	for _, q := range []string{"tool-shaped answer", "blank answer"} {
		if text, err := e.AskSideChat(t.Context(), thread.ID, SideChatInput{Question: q}); text != "" || !errors.Is(err, ErrSideChatUnavailable) {
			t.Fatalf("model returned %q, err=%v", text, err)
		}
		waitSideChatResult(t, requests)
		if text, err := e.StreamSideChat(t.Context(), thread.ID, SideChatInput{Question: q}, func(delta string) {
			t.Errorf("unexpected text from %q: %s", q, delta)
		}); text != "" || !errors.Is(err, ErrSideChatUnavailable) {
			t.Fatalf("stream returned %q, err=%v", text, err)
		}
		waitSideChatResult(t, requests)
	}
}

func TestSideChatBoundsAndMissingSourcesRefuseWithoutAnswer(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("side chat", "", "")
	if err != nil {
		t.Fatal(err)
	}
	longQuestion := strings.Repeat("q", sideChatMaxQuestion+1)
	longAnswer := strings.Repeat("a", sideChatMaxAnswer+1)
	for _, input := range []SideChatInput{
		{Question: " \n "},
		{Question: longQuestion},
		{Question: "question", Quote: longQuestion},
		{Question: "question", History: make([]SideChatExchange, sideChatMaxExchanges+1)},
		{Question: "question", History: []SideChatExchange{{Question: " ", Answer: "answer"}}},
		{Question: "question", History: []SideChatExchange{{Question: "prior", Answer: " "}}},
		{Question: "question", History: []SideChatExchange{{Question: longQuestion, Answer: "answer"}}},
		{Question: "question", History: []SideChatExchange{{Question: "prior", Answer: longAnswer}}},
	} {
		if text, err := e.AskSideChat(t.Context(), thread.ID, input); text != "" || !errors.Is(err, ErrInvalidSideChat) {
			t.Fatalf("invalid side request produced %q err=%v", text, err)
		}
	}
	input := SideChatInput{Question: "Why did the result differ?"}
	if text, err := e.AskSideChat(t.Context(), "missing", input); text != "" || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing thread produced %q err=%v", text, err)
	}
	if text, err := e.AskSideChat(t.Context(), thread.ID, input); text != "Side chat: "+input.Question || err != nil {
		t.Fatalf("offline model produced %q err=%v", text, err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"provider_id": "missing-provider"}); err != nil {
		t.Fatal(err)
	}
	if text, err := e.AskSideChat(t.Context(), thread.ID, input); text != "" || err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("missing model produced %q err=%v", text, err)
	}
}
