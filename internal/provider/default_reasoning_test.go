package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

type defaultReasoningRequest struct {
	Stream    bool    `json:"stream"`
	Effort    *string `json:"reasoning_effort"`
	Reasoning *struct {
		Effort *string `json:"effort"`
	} `json:"reasoning"`
}

func defaultReasoningEndpoint(t *testing.T, apiMode string) (*httptest.Server, <-chan defaultReasoningRequest) {
	t.Helper()
	requests := make(chan defaultReasoningRequest, 8)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := "/v1/chat/completions"
		if apiMode == config.APIModeResponses {
			path = "/v1/responses"
		}
		if r.URL.Path != path {
			t.Errorf("model endpoint path=%q, want %q", r.URL.Path, path)
		}
		var request defaultReasoningRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		select {
		case requests <- request:
		default:
			t.Error("unexpected repeated model requests")
		}
		const response = `{"id":"fixture-response","status":"completed","output":[{"id":"fixture-message","type":"message","role":"assistant","content":[{"type":"output_text","text":"ready"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			if apiMode == config.APIModeResponses {
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
			} else {
				_, _ = io.WriteString(w, backoffChunk+"data: [DONE]\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if apiMode == config.APIModeResponses {
			_, _ = io.WriteString(w, response)
		} else {
			_, _ = io.WriteString(w, backoffCompletion)
		}
	}))
	t.Cleanup(endpoint.Close)
	return endpoint, requests
}

// Raw YAML keeps these regressions runnable before the new configuration field
// exists. The assertion belongs at the actual provider HTTP request boundary.
func loadDefaultReasoningFixture(t *testing.T, endpoint, apiMode, setting string, omit bool) *config.Config {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	raw := fmt.Sprintf("models:\n  default: fixture\n  providers:\n    - id: fixture\n      base_url: %q\n      model: fixture\n      api_mode: %q\n", endpoint+"/v1", apiMode)
	if !omit {
		raw += fmt.Sprintf("swarm:\n  default_reasoning_effort: %q\n", setting)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func assertDefaultReasoningRequest(t *testing.T, requests <-chan defaultReasoningRequest, apiMode, want string) {
	t.Helper()
	select {
	case request := <-requests:
		got := request.Effort
		if apiMode == config.APIModeResponses {
			got = nil
			if request.Reasoning != nil {
				got = request.Reasoning.Effort
			}
		}
		if want == "" {
			if got != nil {
				t.Fatalf("unset reasoning effort was sent as %q", *got)
			}
		} else if got == nil || *got != want {
			value := "<omitted>"
			if got != nil {
				value = *got
			}
			t.Fatalf("HTTP reasoning effort=%q, want %q", value, want)
		}
	default:
		t.Fatal("model invocation did not reach the HTTP endpoint")
	}
	if len(requests) != 0 {
		t.Fatal("one invocation sent multiple model requests")
	}
}

func TestModelBuilderDefaultReasoningAtHTTP(t *testing.T) {
	for _, apiMode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		t.Run(apiMode, func(t *testing.T) {
			for _, tc := range []struct {
				name, setting, effort, want string
				omit                        bool
			}{
				{name: "older configuration remains unset", omit: true},
				{name: "empty default remains unset"},
				{name: "invalid default remains unset", setting: "unsupported"},
				{name: "empty effort inherits high", setting: "high", want: "high"},
				{name: "explicit low overrides high", setting: "high", effort: "low", want: "low"},
				{name: "explicit high overrides low", setting: "low", effort: "high", want: "high"},
				{name: "normalized empty effort inherits normalized default", setting: " HIGH ", effort: " \t ", want: "high"},
				{name: "normalized explicit effort wins", setting: "high", effort: " LOW ", want: "low"},
				{name: "invalid explicit effort normalizes before inheritance", setting: "high", effort: "unsupported", want: "high"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, streamed := range []bool{false, true} {
						t.Run(fmt.Sprintf("stream=%t", streamed), func(t *testing.T) {
							endpoint, requests := defaultReasoningEndpoint(t, apiMode)
							cfg := loadDefaultReasoningFixture(t, endpoint.URL, apiMode, tc.setting, tc.omit)
							builder, err := New(cfg).ModelBuilder(t.Context(), "", "", tc.effort, nil)
							if err != nil {
								t.Fatal(err)
							}
							m := builder("manager", "fixture-manager")
							input := []*schema.Message{schema.UserMessage("Inspect the available input.")}
							if streamed {
								stream, err := m.Stream(t.Context(), input)
								if err != nil {
									t.Fatal(err)
								}
								defer stream.Close()
								if err := finishBackoffStream(stream); err != nil {
									t.Fatal(err)
								}
							} else if _, err := m.Generate(t.Context(), input); err != nil {
								t.Fatal(err)
							}
							assertDefaultReasoningRequest(t, requests, apiMode, tc.want)
						})
					}
				})
			}
		})
	}
}

func TestRegistryWorkerInheritsDefaultReasoningAtHTTP(t *testing.T) {
	endpoint, requests := defaultReasoningEndpoint(t, config.APIModeChatCompletions)
	cfg := loadDefaultReasoningFixture(t, endpoint.URL, config.APIModeChatCompletions, "high", false)
	builder, err := New(cfg).ModelBuilder(t.Context(), "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	worker, err := reg.Spawn(t.Context(), "reader", "Inspect the available input.", builder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish its actual model request")
	}
	if _, err, done := worker.Result(); !done || err != nil {
		t.Fatalf("worker HTTP outcome: done=%t, err=%v", done, err)
	}
	assertDefaultReasoningRequest(t, requests, config.APIModeChatCompletions, "high")
}

func TestAuxiliaryBuilderInheritsDefaultReasoningAtHTTP(t *testing.T) {
	for _, tc := range []struct{ name, effort, want string }{
		{"empty auxiliary effort inherits", "", "high"},
		{"explicit background low remains low", "low", "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, requests := defaultReasoningEndpoint(t, config.APIModeChatCompletions)
			cfg := loadDefaultReasoningFixture(t, endpoint.URL, config.APIModeChatCompletions, "high", false)
			builder, err := New(cfg).AuxiliaryModelBuilder(t.Context(), "", "", tc.effort, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := builder("policy", "fixture-policy").Generate(t.Context(), []*schema.Message{schema.UserMessage("Inspect the available input.")}); err != nil {
				t.Fatal(err)
			}
			assertDefaultReasoningRequest(t, requests, config.APIModeChatCompletions, tc.want)
		})
	}
}

// A restored worker keeps the effort selected for that worker, including an
// omitted effort; changing the installation default only affects new selection.
func TestWorkerReasoningBindingSurvivesDefaultChangesAtHTTP(t *testing.T) {
	for _, apiMode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		t.Run(apiMode, func(t *testing.T) {
			for _, tc := range []struct{ name, initial, later, want string }{
				{"inherited high stays high", "high", "low", "high"},
				{"selected omission stays omitted", "", "high", ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					endpoint, requests := defaultReasoningEndpoint(t, apiMode)
					cfg := loadDefaultReasoningFixture(t, endpoint.URL, apiMode, tc.initial, false)
					pool := New(cfg)
					binding, original, err := pool.WorkerModel(t.Context(), "", "", "", nil)
					if err != nil {
						t.Fatal(err)
					}
					if binding.ReasoningEffort != tc.want {
						t.Fatalf("worker bound effort=%q, want %q", binding.ReasoningEffort, tc.want)
					}
					next := cfg.Snapshot()
					raw, err := json.Marshal(map[string]string{"default_reasoning_effort": tc.later})
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(raw, &next.Swarm); err != nil {
						t.Fatal(err)
					}
					if err := cfg.Replace(next); err != nil {
						t.Fatal(err)
					}
					pool.Invalidate()
					input := []*schema.Message{schema.UserMessage("Inspect the available input.")}
					if _, err := original("reader", "original-worker").Generate(t.Context(), input); err != nil {
						t.Fatal(err)
					}
					assertDefaultReasoningRequest(t, requests, apiMode, tc.want)
					restored, err := New(cfg).RestoreWorkerModel(t.Context(), binding, nil)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := restored("reader", "restored-worker").Generate(t.Context(), input); err != nil {
						t.Fatal(err)
					}
					assertDefaultReasoningRequest(t, requests, apiMode, tc.want)
				})
			}
		})
	}
}

func TestLegacyEmptyWorkerBindingDoesNotInheritANewDefaultAtHTTP(t *testing.T) {
	for _, apiMode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		t.Run(apiMode, func(t *testing.T) {
			endpoint, requests := defaultReasoningEndpoint(t, apiMode)
			cfg := loadDefaultReasoningFixture(t, endpoint.URL, apiMode, "high", false)
			legacy := swarm.ModelBinding{ProviderID: "fixture", Model: "fixture"}
			builder, err := New(cfg).RestoreWorkerModel(t.Context(), legacy, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := builder("reader", "legacy-worker").Generate(t.Context(), []*schema.Message{schema.UserMessage("Inspect the available input.")}); err != nil {
				t.Fatal(err)
			}
			assertDefaultReasoningRequest(t, requests, apiMode, "")
		})
	}
}
