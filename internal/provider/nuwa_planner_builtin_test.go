package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A planner's restriction must survive every local binding path, while the
// original model remains usable by an executor with all configured cloud tools.
func TestNuwaPlannerBuiltinRestrictionSurvivesResponsesToolBinding(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[streaming], func(t *testing.T) {
			endpoint, requests := newNuwaPlannerBuiltinEndpoint(t)
			base := testResponsesModel(t, config.Provider{BaseURL: endpoint.URL + "/v1", Model: "binding-model", BuiltinTools: nuwaPlannerBuiltinConfig()})
			if err := base.BindTools([]*schema.ToolInfo{{Name: "read_work"}}); err != nil {
				t.Fatal(err)
			}
			planner := WithoutBuiltinTools(base)
			// A subsequent bind on the original instance cannot replace the
			// planner's previously selected coordination tool.
			if err := base.BindTools([]*schema.ToolInfo{{Name: "write"}}); err != nil {
				t.Fatal(err)
			}
			bound, err := planner.(model.ToolCallingChatModel).WithTools([]*schema.ToolInfo{{Name: "record_work"}})
			if err != nil {
				t.Fatal(err)
			}
			rebound, err := bound.WithTools([]*schema.ToolInfo{{Name: "list_models"}})
			if err != nil {
				t.Fatal(err)
			}
			empty, err := bound.WithTools(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name    string
				model   model.BaseChatModel
				options []model.Option
				builtin bool
				local   string
			}{
				{name: "original before planner", model: base, builtin: true, local: "write"},
				{name: "existing binding", model: planner, local: "read_work"},
				{name: "WithTools", model: bound, local: "record_work"},
				{name: "repeated WithTools", model: rebound, local: "list_models"},
				{name: "call override", model: bound, options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: "read_work"}})}, local: "read_work"},
				{name: "empty binding", model: empty},
				{name: "empty call override", model: bound, options: []model.Option{model.WithTools(nil)}},
				{name: "original after planner", model: base, builtin: true, local: "write"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					callNuwaPlannerBuiltinModel(t, tc.model, streaming, tc.options...)
					assertNuwaPlannerBuiltinRequest(t, <-requests, tc.builtin, tc.local)
				})
			}
			if err := planner.(model.ChatModel).BindTools([]*schema.ToolInfo{{Name: "list_models"}}); err != nil {
				t.Fatal(err)
			}
			callNuwaPlannerBuiltinModel(t, planner, streaming)
			assertNuwaPlannerBuiltinRequest(t, <-requests, false, "list_models")
			// BindTools on the planner is isolated from both its immutable
			// derivatives and the original cached model.
			callNuwaPlannerBuiltinModel(t, bound, streaming)
			assertNuwaPlannerBuiltinRequest(t, <-requests, false, "record_work")
			callNuwaPlannerBuiltinModel(t, base, streaming)
			assertNuwaPlannerBuiltinRequest(t, <-requests, true, "write")
		})
	}
}

// Root and workers may share the same cached client and request context. Only
// the explicit planner instance loses hosted tools; attribution and receipts
// must remain complete for ordinary managers and workers.
func TestNuwaPlannerBuiltinRestrictionKeepsSharedWorkerAndCallTelemetry(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[streaming], func(t *testing.T) {
			endpoint, requests := newNuwaPlannerBuiltinEndpoint(t)
			cfg := config.Default()
			cfg.Models.Default = "builtin-fixture"
			cfg.Swarm.MaxCompletionTokens = 321
			cfg.Models.Providers = []config.Provider{{ID: "builtin-fixture", BaseURL: endpoint.URL + "/v1", Model: "shared-model", APIMode: config.APIModeResponses, BuiltinTools: nuwaPlannerBuiltinConfig()}}
			pool := New(cfg)
			records := make(chan CallRecord, 4)
			build, err := pool.ModelBuilder(t.Context(), "builtin-fixture", "", config.ReasoningHigh, func(record CallRecord) { records <- record })
			if err != nil {
				t.Fatal(err)
			}
			ordinary := build("manager", "ordinary-manager")
			planner := WithoutBuiltinTools(build("manager", "nuwa-planner"))
			worker := build("executor", "selected-worker")
			for _, tc := range []struct {
				model   model.BaseChatModel
				agent   string
				builtin bool
			}{
				{ordinary, "ordinary-manager", true},
				{planner, "nuwa-planner", false},
				{worker, "selected-worker", true},
				{ordinary, "ordinary-manager", true},
			} {
				callNuwaPlannerBuiltinModel(t, tc.model, streaming, model.WithTools([]*schema.ToolInfo{{Name: "read_work"}}))
				request := <-requests
				assertNuwaPlannerBuiltinRequest(t, request, tc.builtin, "read_work")
				if request.Model != "shared-model" || request.Reasoning.Effort != "high" || request.MaxOutputTokens != 321 {
					t.Fatalf("planner restriction changed model options: %+v", request)
				}
				var record CallRecord
				select {
				case record = <-records:
				case <-time.After(time.Second):
					t.Fatal("model call did not produce telemetry")
				}
				if record.Err != nil || record.Backoff != nil || record.AgentID != tc.agent || record.ProviderID != "builtin-fixture" ||
					record.Model != "shared-model" || record.PromptTokens != 4 || record.CompletionTokens != 3 || record.TotalTokens != 7 {
					t.Fatalf("restriction changed attribution or usage: %+v", record)
				}
				var kinds []string
				for _, event := range record.BuiltinTools {
					if event.ID != "builtin_"+strings.TrimSuffix(event.Type, "_call") || event.Status != "completed" {
						t.Fatalf("hosted execution receipt changed: %+v", event)
					}
					kinds = append(kinds, event.Type)
				}
				var want []string
				if tc.builtin {
					want = []string{"web_search_call", "file_search_call", "code_interpreter_call"}
				}
				if !slices.Equal(kinds, want) {
					t.Fatalf("hosted receipts missing, duplicated or leaked to planner: got=%v want=%v", kinds, want)
				}
			}
			if len(cfg.Models.Providers[0].BuiltinTools) != 3 {
				t.Fatal("planner restriction changed global provider configuration")
			}
			select {
			case extra := <-records:
				t.Fatalf("one physical request produced extra telemetry: %+v", extra)
			default:
			}
		})
	}
}

func TestWithoutBuiltinToolsKeepsOtherModelImplementations(t *testing.T) {
	inner := &fakeModel{}
	if WithoutBuiltinTools(inner) != inner || WithoutBuiltinTools(nil) != nil {
		t.Fatal("a model without provider-executed tools was replaced")
	}
}

func nuwaPlannerBuiltinConfig() []map[string]any {
	return []map[string]any{
		{"type": "web_search", "search_context_size": "low"},
		{"type": "file_search", "vector_store_ids": []string{"vs_fixture"}},
		{"type": "code_interpreter", "container": map[string]any{"type": "auto"}},
	}
}

type nuwaPlannerBuiltinRequest struct {
	Model           string                  `json:"model"`
	Stream          bool                    `json:"stream"`
	Tools           []map[string]any        `json:"tools"`
	MaxOutputTokens int                     `json:"max_output_tokens"`
	Reasoning       struct{ Effort string } `json:"reasoning"`
}

func newNuwaPlannerBuiltinEndpoint(t *testing.T) (*httptest.Server, <-chan nuwaPlannerBuiltinRequest) {
	t.Helper()
	requests := make(chan nuwaPlannerBuiltinRequest, 16)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nuwaPlannerBuiltinRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || r.URL.Path != "/v1/responses" {
			t.Errorf("invalid Responses request: path=%q error=%v", r.URL.Path, err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		requests <- request
		var hosted []map[string]any
		for _, offered := range request.Tools {
			kind, _ := offered["type"].(string)
			if kind != "function" {
				hosted = append(hosted, map[string]any{"id": "builtin_" + kind, "type": kind + "_call", "status": "completed"})
			}
		}
		output := append([]map[string]any(nil), hosted...)
		output = append(output, map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "bounded answer"}}})
		completed := map[string]any{"status": "completed", "output": output, "usage": map[string]any{"input_tokens": 4, "output_tokens": 3, "total_tokens": 7}}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			write := func(event any) {
				raw, err := json.Marshal(event)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
			}
			write(map[string]any{"type": "response.output_text.delta", "delta": "bounded answer"})
			for _, item := range hosted {
				write(map[string]any{"type": "response.output_item.done", "item": item})
			}
			write(map[string]any{"type": "response.completed", "response": completed})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(completed); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(endpoint.Close)
	return endpoint, requests
}

func assertNuwaPlannerBuiltinRequest(t *testing.T, request nuwaPlannerBuiltinRequest, builtin bool, local string) {
	t.Helper()
	var types, names []string
	for _, offered := range request.Tools {
		kind, _ := offered["type"].(string)
		types = append(types, kind)
		if kind == "function" {
			name, _ := offered["name"].(string)
			names = append(names, name)
		}
	}
	var wantTypes, wantNames []string
	if builtin {
		wantTypes = []string{"web_search", "file_search", "code_interpreter"}
	}
	if local != "" {
		wantTypes, wantNames = append(wantTypes, "function"), []string{local}
	}
	if !slices.Equal(types, wantTypes) || !slices.Equal(names, wantNames) {
		t.Fatalf("Responses tools changed: types=%v names=%v want=%v/%v", types, names, wantTypes, wantNames)
	}
}

func callNuwaPlannerBuiltinModel(t *testing.T, m model.BaseChatModel, streaming bool, opts ...model.Option) {
	t.Helper()
	input := []*schema.Message{schema.UserMessage("Inspect the available evidence.")}
	var text strings.Builder
	if streaming {
		stream, err := m.Stream(t.Context(), input, opts...)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		for {
			part, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part != nil {
				text.WriteString(part.Content)
				if len(part.ToolCalls) != 0 {
					t.Fatalf("a hosted tool entered local dispatch: %+v", part.ToolCalls)
				}
			}
		}
	} else {
		out, err := m.Generate(t.Context(), input, opts...)
		if err != nil || out == nil {
			t.Fatalf("Generate: message=%+v err=%v", out, err)
		}
		text.WriteString(out.Content)
		if len(out.ToolCalls) != 0 {
			t.Fatalf("a hosted tool entered local dispatch: %+v", out.ToolCalls)
		}
	}
	if text.String() != "bounded answer" {
		t.Fatalf("model output changed: %q", text.String())
	}
}
