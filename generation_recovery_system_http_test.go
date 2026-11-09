package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type independentRecoverySystemMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	prepend                 bool
	instruction, constraint string
}

func (m *independentRecoverySystemMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	for _, message := range state.Messages {
		if message != nil && message.Extra != nil && message.Extra["private_fixture_system_origin"] == "independent_system" {
			return ctx, state, nil
		}
	}
	next := *state
	first, equalText := schema.SystemMessage(m.constraint), schema.SystemMessage(m.instruction)
	first.Extra = map[string]any{"private_fixture_system_origin": "independent_system"}
	equalText.Extra = map[string]any{"private_fixture_system_origin": "same_text_independent_system"}
	extra := []*schema.Message{first, equalText}
	if m.prepend {
		next.Messages = append(extra, state.Messages...)
	} else {
		next.Messages = append(append([]*schema.Message(nil), state.Messages...), extra...)
	}
	return ctx, &next, nil
}

// A constructor-owned system message is regenerated on RunWith; independent
// system messages, including equal text, must retain their separate identity.
func TestReasoningRecoveryRetainsIndependentSystemMessagesOverHTTP(t *testing.T) {
	for _, prepend := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepend=%t", prepend), func(t *testing.T) {
			const instruction = "The manager constructor owns this instruction."
			const constraint = "An independent system constraint must survive recovery."
			var calls atomic.Int32
			var sequenceMu sync.Mutex
			var firstSequence []string
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []*schema.Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					http.Error(w, "bad fixture request", 400)
					return
				}
				step := calls.Add(1)
				system, same, independent := 0, 0, 0
				var sequence []string
				for _, m := range input.Messages {
					if m != nil && m.Role == schema.System {
						system++
						sequence = append(sequence, m.Content)
						if m.Content == instruction {
							same++
						}
						if m.Content == constraint {
							independent++
						}
					}
				}
				sequenceMu.Lock()
				if step == 1 {
					firstSequence = append([]string(nil), sequence...)
				} else if !reflect.DeepEqual(sequence, firstSequence) {
					t.Errorf("recovery reordered independent and constructor system messages: first=%q next=%q", firstSequence, sequence)
				}
				sequenceMu.Unlock()
				if system != 3 || same != 2 || independent != 1 {
					t.Errorf("actual HTTP request%d lost system identity: total=%d equal-instruction=%d independent=%d", step, system, same, independent)
				}
				delta, finish := map[string]any{"content": "The independent constraints remain available."}, "stop"
				if step == 1 {
					delta, finish = map[string]any{"reasoning_content": "An interrupted internal check."}, "length"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
					b, _ := json.Marshal(map[string]any{"id": "system-recovery", "object": "chat.completion.chunk", "choices": []any{choice}})
					_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
				}
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer endpoint.Close()
			ctx := context.Background()
			m, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{BaseURL: endpoint.URL, Model: "offline-system-recovery"})
			if err != nil {
				t.Fatal(err)
			}
			reg := NewRegistry()
			defer reg.Close()
			reg.ModelBuilder = func(string, string) model.BaseChatModel { return m }
			cfg := independentSystemRecoveryConfig("Preserve the current independent constraints.")
			cfg.Instruction = instruction
			cfg.ManagerMiddlewares = []adk.ChatModelAgentMiddleware{&independentRecoverySystemMiddleware{prepend: prepend, instruction: instruction, constraint: constraint}}
			_, err = reg.RunWith(ctx, cfg, nil)
			var cap *ReasoningOutputLimit
			if !errors.As(err, &cap) || len(cap.RecoveryInput) == 0 {
				t.Fatalf("actual capped generation did not carry recoverable model input: %v", err)
			}
			ownedConstraint, ownedEqual := 0, 0
			for _, message := range cap.RecoveryInput {
				if message == nil || message.Extra == nil {
					continue
				}
				if message.Extra["private_fixture_system_origin"] == "independent_system" {
					ownedConstraint++
				}
				if message.Extra["private_fixture_system_origin"] == "same_text_independent_system" {
					ownedEqual++
				}
			}
			if ownedConstraint != 1 || ownedEqual != 1 {
				t.Fatalf("typed recovery input lost independent system identity metadata: constraint=%d equal-text=%d", ownedConstraint, ownedEqual)
			}
			next := independentSystemRecoveryConfig("")
			next.Instruction = ""
			next.Messages = cap.RecoveryInput
			next.ManagerMiddlewares = cfg.ManagerMiddlewares
			got, err := reg.RunWith(ctx, next, nil)
			if err != nil || got.Final != "The independent constraints remain available." || calls.Load() != 2 {
				t.Fatalf("same-registry continuation failed: calls=%d final=%q error=%v", calls.Load(), got.Final, err)
			}
		})
	}
}

func independentSystemRecoveryConfig(task string) RunConfig {
	return RunConfig{Instruction: "Coordinate current work.", Task: task, MaxIterations: 100, CaptureReasoningRecoveryInput: true}
}
