package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestReviewProtocolRepairsRefusedNumbersAndUsesProgramReceipt(t *testing.T) {
	const requirement = "For this project, always use concise reports."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(request int) (string, []reviewOperation) {
		content, prose := requirement, "repairing..."
		if request == 1 {
			content, prose = requirement+" Effective year: 2037.", ""
		}
		return prose, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: content, Evidence: []reviewCitation{citation}}}
	})
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: requirement}})
	citation = reviewProtocolCitation(t, e, turn.ID, requirement)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "The task finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{requirement}) {
		t.Fatalf("repaired notes=%+v err=%v", snap, err)
	}
	if out.Err != "" || !out.Changed || len(out.Refused) != 1 || out.Refused[0].Code != "numeric" || out.Note != "Saved 1 note changes and 0 skill changes." {
		t.Fatalf("refusal/prose hid the committed receipt: %+v", out)
	}
	requests := fixture.snapshot()
	if len(requests) != 2 || !strings.Contains(string(requests[1]), "new numeric detail") {
		t.Fatalf("need exactly two calls with repair feedback, got %d: %s", len(requests), requests)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 2 {
		t.Fatalf("Trace calls=%+v err=%v", calls, err)
	}
	for _, call := range calls {
		if call.AgentID != ReviewAgentID || call.TurnID != turn.ID {
			t.Fatalf("review call escaped the original turn: %+v", call)
		}
	}
}

func TestReviewProtocolReadsNotesAfterWaitingForProjectGate(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Confirm the unchanged project conventions."}})
	if _, _, err := pc.memory.Add("initial convention"); err != nil {
		t.Fatal(err)
	}
	gate := e.reviews.begin(pc.reviewKey())
	gate <- struct{}{}
	held := true
	t.Cleanup(func() {
		if held {
			<-gate
			e.reviews.done()
		}
	})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	if len(fixture.snapshot()) != 0 {
		t.Fatal("review called the model before acquiring its project gate")
	}
	const latest = "a newer human correction written while the review waited"
	if _, _, err := pc.memory.Add(latest); err != nil {
		t.Fatal(err)
	}
	<-gate
	e.reviews.done()
	held = false
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 1 || !strings.Contains(reviewProtocolUserInput(t, requests[0]), latest) {
		t.Fatalf("review used stale notes after its gate wait: %s", requests)
	}
}

func TestReviewProtocolFailedTurnExcludesTaskEvidenceAndCannotWriteSkills(t *testing.T) {
	const requirement = "Always use compact project reports."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(request int) (string, []reviewOperation) {
		if request == 1 {
			return "", []reviewOperation{{Target: memory.ToolSkillManage, Action: "create", Name: "unverified-recovery", Description: "An unverified procedure.", Content: "Try a proposed workaround.", Evidence: []reviewCitation{citation}}}
		}
		return "repairing...", []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: requirement, Evidence: []reviewCitation{citation}}}
	})
	th, turn, pc := qualityReviewTurn(t, e, store.TurnError, []store.Event{
		{Kind: KindUser, Text: requirement},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "manager", ToolCallID: "failed-task", Text: "exec(tool-input-must-not-appear)"},
		{Kind: swarm.NotifyToolResult.String(), AgentID: "manager", ToolCallID: "failed-task", Text: "tool-unverified-fix-must-not-appear", Err: "transport failed"},
		{Kind: swarm.NotifyAgentMessage.String(), AgentID: "manager", Text: "assistant-claim-must-not-appear"},
		{Kind: KindGoalContinued, Text: "runtime-continuation-must-not-appear"},
	})
	if err := e.store.UpdateThread(th.ID, map[string]any{"session_memory": "summary-must-not-appear"}); err != nil {
		t.Fatal(err)
	}
	citation = reviewProtocolCitation(t, e, turn.ID, requirement)
	e.scheduleReview(th.ID, turn, store.TurnError, pc, "final-answer-must-not-appear")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	requests := fixture.snapshot()
	if len(requests) != 2 || out.Err != "" || len(out.Refused) != 1 || out.Refused[0].Code != "validation" {
		t.Fatalf("failed-turn protocol requests=%d outcome=%+v", len(requests), out)
	}
	for _, raw := range requests {
		input := reviewProtocolUserInput(t, raw)
		for _, forbidden := range []string{"tool-input-must-not-appear", "tool-unverified-fix-must-not-appear", "assistant-claim-must-not-appear", "runtime-continuation-must-not-appear", "summary-must-not-appear", "final-answer-must-not-appear"} {
			if strings.Contains(input, forbidden) {
				t.Fatalf("failed-turn request contains %q: %s", forbidden, input)
			}
		}
		var request struct {
			Tools []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(raw, &request); err != nil || len(request.Tools) != 1 || request.Tools[0].Function.Name != reviewBatchToolName {
			t.Fatalf("failed turn received extra tools: %s err=%v", raw, err)
		}
	}
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{requirement}) {
		t.Fatalf("failed-turn human note=%+v err=%v", snap, err)
	}
	skills, err := pc.memory.ListSkills()
	if err != nil || len(skills) != 0 || len(out.Skills) != 0 {
		t.Fatalf("failed turn wrote skill files: %+v err=%v outcome=%+v", skills, err, out)
	}
}

func TestReviewProtocolCancellationDoesNotCallTheModel(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnCancelled, []store.Event{{Kind: KindUser, Text: "Remember the project convention for later."}})
	e.scheduleReview(th.ID, turn, store.TurnCancelled, pc, "Cancelled.")
	settleReviewProtocol(t, e)
	if requests := fixture.snapshot(); len(requests) != 0 {
		t.Fatalf("cancellation started %d model requests", len(requests))
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == KindMemoryReview {
			t.Fatalf("cancellation invented a review result: %+v", event)
		}
	}
}

func TestReviewProtocolLibraryWritesSkillsWithoutProjectNotes(t *testing.T) {
	const procedure = "The reusable procedure is to inspect ownership, rebuild the index, then verify a query."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) {
		return "", []reviewOperation{{Target: memory.ToolSkillManage, Action: "create", Name: "index-recovery", Description: "Recover an index while checking its owner.", Content: procedure, Evidence: []reviewCitation{citation}}}
	})
	th, err := e.CreateThread("standalone review protocol", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: procedure}); err != nil {
		t.Fatal(err)
	}
	citation = reviewProtocolCitation(t, e, turn.ID, procedure)
	pc := e.libraryContextForThread(th.ID)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || len(snap.Entries) != 0 || !out.Library || out.Err != "" || len(out.Skills) != 1 || len(fixture.snapshot()) != 1 {
		t.Fatalf("library protocol outcome=%+v notes=%+v err=%v", out, snap, err)
	}
	if skill, err := pc.memory.ReadSkill("index-recovery"); err != nil || skill.Body != procedure {
		t.Fatalf("library skill=%+v err=%v", skill, err)
	}
}

func TestReviewProtocolProseWithoutBatchDoesNotClaimSuccessfulReview(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "I finished the review.", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Keep project reports concise."}})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || out.Changed || out.Err == "" || len(snap.Entries) != 0 || len(fixture.snapshot()) != 1 {
		t.Fatalf("prose was treated as a commit: outcome=%+v notes=%+v err=%v", out, snap, err)
	}
}

func TestReviewCompletionGuardAvoidsBothGenerateAndStreamRequests(t *testing.T) {
	for _, done := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("done=%v/stream=%v", done, stream), func(t *testing.T) {
				inner := &reviewGuardFixture{}
				wrapped := &reviewStreamModel{inner: inner, completed: func() (string, bool) { return "program receipt", done }}
				var got *schema.Message
				var err error
				if stream {
					var reader *schema.StreamReader[*schema.Message]
					reader, err = wrapped.Stream(context.Background(), nil)
					if err == nil {
						defer reader.Close()
						got, err = reader.Recv()
					}
				} else {
					got, err = wrapped.Generate(context.Background(), nil)
				}
				want, calls := "provider reply", int32(1)
				if done {
					want, calls = "program receipt", 0
				}
				if err != nil || got == nil || got.Content != want || inner.streams.Load() != calls || inner.generates.Load() != 0 {
					t.Fatalf("guard message=%+v err=%v streams=%d generates=%d", got, err, inner.streams.Load(), inner.generates.Load())
				}
			})
		}
	}
}

type reviewGuardFixture struct{ streams, generates atomic.Int32 }

func (f *reviewGuardFixture) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	f.generates.Add(1)
	return nil, errors.New("review must use the streaming provider path")
}
func (f *reviewGuardFixture) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.streams.Add(1)
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("provider reply", nil)}), nil
}

type reviewProtocolFixture struct {
	mu       sync.Mutex
	requests []json.RawMessage
}

func (f *reviewProtocolFixture) snapshot() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]json.RawMessage(nil), f.requests...)
}

func newReviewProtocolEngine(t *testing.T, response func(int) (string, []reviewOperation)) (*Engine, *reviewProtocolFixture) {
	t.Helper()
	fixture := &reviewProtocolFixture{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected protocol request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture read failure", http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct{ Role, Content string }
			Stream   bool              `json:"stream"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, body)
		number := len(fixture.requests)
		fixture.mu.Unlock()
		prose, operations := response(number)
		proseOnly := prose != "" && operations == nil
		if operations == nil {
			operations = []reviewOperation{}
		}
		arguments, _ := json.Marshal(map[string]any{"changes": operations})
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(delta map[string]any, finish any) {
			frame, _ := json.Marshal(map[string]any{"id": "review-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			w.(http.Flusher).Flush()
		}
		write(map[string]any{"role": "assistant"}, nil)
		if prose != "" {
			write(map[string]any{"content": prose}, nil)
		}
		if proseOnly {
			write(map[string]any{}, "stop")
		} else {
			write(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("review-call-%d", number), "type": "function", "function": map[string]any{"name": reviewBatchToolName, "arguments": string(arguments)}}}}, nil)
			write(map[string]any{}, "tool_calls")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID, BaseURL: endpoint.URL, Model: "review-fixture-model", ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	cfg.Swarm.AgentTimeoutSeconds = 10
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e, fixture
}

func reviewProtocolCitation(t *testing.T, e *Engine, turnID, quote string) reviewCitation {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == KindUser && strings.Contains(event.Text, quote) {
			return reviewCitation{Seq: event.Seq, Quote: quote}
		}
	}
	t.Fatalf("missing source quote %q", quote)
	return reviewCitation{}
}

func settleReviewProtocol(t *testing.T, e *Engine) {
	t.Helper()
	if !e.reviews.stop(5 * time.Second) {
		t.Fatal("offline HTTP review did not settle")
	}
}

func reviewProtocolUserInput(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var request struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	for _, message := range request.Messages {
		if message.Role == "user" {
			return message.Content
		}
	}
	t.Fatal("HTTP model request contains no user input")
	return ""
}
