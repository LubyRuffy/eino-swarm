package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// Exercise the actual model factory and Responses HTTP adapter: shared
// admission must preserve protocol options, hosted receipts and request usage.
func TestBackoffResponsesFactoryKeepsReasoningCapAndCloudReceipts(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "generate"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			type request struct {
				Path string
				Body map[string]any
				Err  error
			}
			requests := make(chan request, 1)
			const completed = `{"id":"resp_backoff","status":"completed","output":[{"id":"cloud_backoff","type":"web_search_call","status":"completed","action":{"type":"search","query":"fixture"}},{"id":"msg_backoff","type":"message","role":"assistant","content":[{"type":"output_text","text":"accepted"}]}],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				err := json.NewDecoder(r.Body).Decode(&body)
				requests <- request{Path: r.URL.Path, Body: body, Err: err}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"accepted\"}\n\n")
					_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"cloud_backoff\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"query\":\"fixture\"}}}\n\n")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", completed)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, completed)
			}))
			defer srv.Close()
			cfg := config.Default()
			cfg.Models.Default = "backoff-fixture"
			cfg.Swarm.MaxCompletionTokens = 321
			cfg.Models.Providers = []config.Provider{{ID: "backoff-fixture", BaseURL: srv.URL + "/v1", Model: "backoff-model", APIMode: "responses", BuiltinTools: []map[string]any{{"type": "web_search"}}}}
			pool := New(cfg)
			records := make(chan CallRecord, 4)
			build, err := pool.ModelBuilder(context.Background(), "backoff-fixture", "", "high", func(r CallRecord) { records <- r })
			if err != nil {
				t.Fatal(err)
			}
			m := build("manager", "backoff-manager")
			input := []*schema.Message{{Role: schema.User, Content: "fixture input"}}
			var text strings.Builder
			if streaming {
				stream, err := m.Stream(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				for {
					msg, err := stream.Recv()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if msg != nil {
						text.WriteString(msg.Content)
						if len(msg.ToolCalls) != 0 {
							t.Fatalf("cloud receipt entered local dispatch: %+v", msg.ToolCalls)
						}
					}
				}
			} else {
				out, err := m.Generate(context.Background(), input)
				if err != nil || out == nil {
					t.Fatalf("generate: out=%+v err=%v", out, err)
				}
				text.WriteString(out.Content)
				if len(out.ToolCalls) != 0 {
					t.Fatalf("cloud receipt entered local dispatch: %+v", out.ToolCalls)
				}
			}
			if text.String() != "accepted" {
				t.Fatalf("answer=%q", text.String())
			}
			req := <-requests
			if req.Err != nil || req.Path != "/v1/responses" {
				t.Fatalf("wrong protocol request: %+v", req)
			}
			reasoning, _ := req.Body["reasoning"].(map[string]any)
			tools, _ := req.Body["tools"].([]any)
			if reasoning["effort"] != "high" || req.Body["max_output_tokens"] != float64(321) || req.Body["model"] != "backoff-model" || len(tools) != 1 {
				t.Fatalf("factory wrapper lost protocol options: %+v", req.Body)
			}
			if tools[0].(map[string]any)["type"] != "web_search" {
				t.Fatalf("configured cloud tool changed: %+v", tools)
			}
			var record CallRecord
			select {
			case record = <-records:
			case <-time.After(time.Second):
				t.Fatal("model request did not produce a receipt")
			}
			if record.Backoff != nil || record.Err != nil || record.AgentID != "backoff-manager" || record.ProviderID != "backoff-fixture" || record.Model != "backoff-model" || record.TotalTokens != 8 {
				t.Fatalf("request receipt lost attribution or usage: %+v", record)
			}
			if len(record.BuiltinTools) != 1 || record.BuiltinTools[0].ID != "cloud_backoff" || record.BuiltinTools[0].Type != "web_search_call" || record.BuiltinTools[0].Status != "completed" {
				t.Fatalf("cloud receipt missing or duplicated: %+v", record.BuiltinTools)
			}
			select {
			case extra := <-records:
				t.Fatalf("one request produced an extra record: %+v", extra)
			default:
			}
		})
	}
}

func TestBackoffResponsesCooldownCancellationRecordsOnlyWait(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "generate"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, `{"error":{"type":"server_error","message":"fixture unavailable"}}`)
			}))
			defer srv.Close()
			cfg := config.Default()
			cfg.Models.Default = "backoff-fixture"
			cfg.Models.Providers = []config.Provider{{ID: "backoff-fixture", BaseURL: srv.URL + "/v1", Model: "backoff-model", APIMode: "responses", BuiltinTools: []map[string]any{{"type": "web_search"}}}}
			pool := New(cfg)
			pool.mu.Lock()
			pool.backoffs = map[string]*backoffGate{backoffKey(srv.URL): newBackoffGate(200*time.Millisecond, 400*time.Millisecond)}
			pool.mu.Unlock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var records []CallRecord
			build, err := pool.ModelBuilder(context.Background(), "backoff-fixture", "", "high", func(record CallRecord) {
				records = append(records, record)
				if record.Backoff != nil {
					cancel()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			input := []*schema.Message{{Role: schema.User, Content: "fixture input"}}
			if _, err := build("manager", "backoff-manager").Generate(context.Background(), input); err == nil || !strings.Contains(err.Error(), "503") {
				t.Fatalf("initial call must observe real HTTP 503: %v", err)
			}
			worker := build("worker", "backoff-waiting-worker")
			if streaming {
				stream, streamErr := worker.Stream(ctx, input)
				err = streamErr
				if stream != nil {
					stream.Close()
				}
			} else {
				_, err = worker.Generate(ctx, input)
			}
			if !errors.Is(err, context.Canceled) || requests.Load() != 1 {
				t.Fatalf("cooldown cancellation reached HTTP: err=%v requests=%d", err, requests.Load())
			}
			if len(records) != 2 || records[0].Backoff != nil || records[0].Err == nil || records[1].Backoff == nil {
				t.Fatalf("expected one failed request and one wait only: %+v", records)
			}
			wait := records[1]
			if wait.AgentID != "backoff-waiting-worker" || wait.Backoff.Attempt != 1 || wait.Backoff.WaitMS <= 0 || wait.Backoff.WaitMS > 200 || len(wait.BuiltinTools) != 0 || wait.TotalTokens != 0 {
				t.Fatalf("wait invented a request, cloud result, or usage: %+v", wait)
			}
		})
	}
}
