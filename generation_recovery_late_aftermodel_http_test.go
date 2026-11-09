package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type lateAfterModelRecoveryFailure struct {
	adk.BaseChatModelAgentMiddleware
	observed                 <-chan struct{}
	ready, release, returned chan struct{}
	late                     error
}

func (h *lateAfterModelRecoveryFailure) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	defer close(h.returned)
	// This is the real ADK hook after the Registry's own historyRecorder. Wait
	// until the UI drain has classified the cap before producing a distinct error.
	select {
	case <-h.observed:
	case <-ctx.Done():
		return ctx, state, ctx.Err()
	}
	close(h.ready)
	select {
	case <-h.release:
	case <-ctx.Done():
		return ctx, state, ctx.Err()
	}
	return ctx, state, h.late
}

func TestRegistryObservedOutputCapKeepsCauseAfterLateAfterModelErrorOverHTTP(t *testing.T) {
	const instruction = "Complete the current request using its retained evidence."
	const task = "Inspect the current operation without discarding its existing context."
	const thought = "Unfinished private planning must remain outside the recovery input."
	lateErr := errors.New("private fixture late AfterModel terminal error")
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages        []*schema.Message `json:"messages"`
			MaxTokens       int               `json:"max_tokens"`
			ReasoningEffort string            `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture input", 400)
			return
		}
		calls.Add(1)
		if len(input.Messages) != 2 || input.Messages[0].Content != instruction || input.Messages[1].Content != task || input.MaxTokens != 16384 || input.ReasoningEffort != "high" {
			t.Errorf("actual model HTTP input or fixed settings changed: messages=%d cap=%d effort=%q", len(input.Messages), input.MaxTokens, input.ReasoningEffort)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "reasoning_content": thought}, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": "length"}} {
			raw, _ := json.Marshal(map[string]any{"id": "late-hook-cap", "object": "chat.completion.chunk", "choices": []any{choice}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	capTokens := 16384
	inner, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{BaseURL: endpoint.URL, Model: "offline-late-aftermodel", MaxTokens: &capTokens, ReasoningEffort: openaimodel.ReasoningEffortLevelHigh})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	defer reg.Close()
	reg.ModelBuilder = func(string, string) model.BaseChatModel { return inner }
	observed, ready, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce, observeOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var observedID atomic.Value
	observedID.Store("")
	var startedID atomic.Value
	startedID.Store("")
	var interrupts atomic.Int32
	hook := &lateAfterModelRecoveryFailure{observed: observed, ready: ready, release: release, returned: returned, late: lateErr}
	input := schema.UserMessage(task)
	input.Extra = map[string]any{"private_fixture_source": "human"}
	cfg := RunConfig{Instruction: instruction, Messages: []adk.Message{input}, MaxIterations: 100, CaptureReasoningRecoveryInput: true, ManagerMiddlewares: []adk.ChatModelAgentMiddleware{hook}}
	type outcome struct {
		result RunResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := reg.RunWith(ctx, cfg, Callback(func(n Notification) {
			if n.Kind == NotifyGenerationStarted {
				startedID.Store(n.MessageID)
			}
			if n.Kind == NotifyGenerationInterrupted && errors.Is(n.Err, errOutputBudget) {
				interrupts.Add(1)
				observedID.Store(n.MessageID)
				observeOnce.Do(func() { close(observed) })
			}
		}))
		finished <- outcome{result, err}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("actual AfterModel hook never observed the UI-classified cap")
	}
	select {
	case early := <-finished:
		unblock()
		<-returned
		t.Fatalf("typed cap returned before its real late-error handler joined: %v", early.err)
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	var got outcome
	select {
	case got = <-finished:
	case <-ctx.Done():
		t.Fatal("RunWith did not finish after the real AfterModel error")
	}
	select {
	case <-returned:
	default:
		t.Fatal("RunWith returned before the real AfterModel hook completed")
	}
	var cap *ReasoningOutputLimit
	if !errors.As(got.err, &cap) || !errors.Is(got.err, errOutputBudget) || errors.Is(got.err, lateErr) || got.err.Error() != errOutputBudget.Error() {
		t.Fatalf("late AfterModel error replaced or altered the already observed cap: %v", got.err)
	}
	if cap.MessageID == "" || cap.MessageID != observedID.Load().(string) || cap.MessageID != startedID.Load().(string) || interrupts.Load() != 1 {
		t.Fatalf("late error changed the original generation identity: cap=%q observed=%v started=%v interruptions=%d", cap.MessageID, observedID.Load(), startedID.Load(), interrupts.Load())
	}
	if cap.RecoveryUnavailable != "" || len(cap.RecoveryInput) != 2 || cap.RecoveryInput[0].Role != schema.System || cap.RecoveryInput[0].Content != instruction || cap.RecoveryInput[1].Role != schema.User || cap.RecoveryInput[1].Content != task || cap.RecoveryInput[1].Extra["private_fixture_source"] != "human" {
		t.Fatal("late error lost the original independent model input")
	}
	for _, message := range cap.RecoveryInput {
		if strings.Contains(message.Content, thought) || strings.Contains(message.ReasoningContent, thought) {
			t.Fatal("late error moved interrupted reasoning into recoverable input")
		}
	}
	if got.result.Final != "" || calls.Load() != 1 {
		t.Fatalf("core RunWith retried or fabricated a final answer: HTTP=%d final=%q", calls.Load(), got.result.Final)
	}
	t.Log("actual HTTP length cap observed before the real AfterModel terminal error; old iterator/hook joined, original typed cause, generation ID and recovery input retained")
}
