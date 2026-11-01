package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateFile(path, body); err != nil {
		t.Fatal(err)
	}
}

func hasFullResult(content, expected string) bool {
	var report struct {
		Agents []struct {
			Result string `json:"result"`
		} `json:"agents"`
	}
	if json.Unmarshal([]byte(content), &report) != nil {
		return false
	}
	for _, entry := range report.Agents {
		if entry.Result == expected {
			return true
		}
	}
	return false
}

func TestRealRegistryADKAndFinalSDKRequestKeepFirstFullAndReferenceOnlyDuplicates(t *testing.T) {
	for _, arm := range []string{"a", "b"} {
		t.Run(arm, func(t *testing.T) {
			body := strings.Repeat("condition=false; amount=-73; preserve every line\n", 2000) + "TAIL: expires only if approval is revoked"
			var mu sync.Mutex
			var requests [][]*schema.Message
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []*schema.Message `json:"messages"`
					Tools    []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if len(request.Tools) != 5 {
					t.Errorf("SDK tool catalog size=%d want 5 lifecycle tools", len(request.Tools))
				}
				for _, entry := range request.Tools {
					if entry.Function.Name == "read" || entry.Function.Name == "write" || entry.Function.Name == "exec" {
						t.Error("filesystem/process tool reached SDK catalog")
					}
				}
				mu.Lock()
				requests = append(requests, request.Messages)
				number := len(requests)
				mu.Unlock()
				message := map[string]any{"role": "assistant", "content": "round finished"}
				finish := "stop"
				if number == 1 || number == 2 || number == 4 {
					message = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
						"index": 0, "id": fmt.Sprintf("wait-%d", number), "type": "function",
						"function": map[string]any{"name": "wait_agents", "arguments": `{"agent_ids":["actual-worker"],"timeout_s":1}`}}}}
					finish = "tool_calls"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				response := map[string]any{"id": "fixture", "object": "chat.completion.chunk", "model": "fixture-model",
					"choices": []any{map[string]any{"index": 0, "delta": message, "finish_reason": finish}},
					"usage":   map[string]int{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110}}
				encoded, _ := json.Marshal(response)
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
			}))
			t.Cleanup(server.Close)
			root := t.TempDir()
			fixturePath := filepath.Join(root, "fixture.json")
			writeTestJSON(t, fixturePath, map[string]any{"schema_version": 1, "instruction": "Collect the worker results without changing their conditions.",
				"rounds":  []any{map[string]string{"user": "Inspect the result."}, map[string]string{"user": "Verify the result again."}},
				"workers": []any{map[string]any{"agent_id": "actual-worker", "role": "verifier", "generation": 4, "status": "done", "result": body}}})
			cfg := config.Default()
			cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: server.URL, APIKey: "private-fixture-key", Model: "fixture-model"}}
			configPath := filepath.Join(root, "config.yaml")
			encoded, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := privateFile(configPath, encoded); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(root, "evidence")
			var stdout, stderr bytes.Buffer
			status := runCLI(context.Background(), []string{"--config", configPath, "--provider", "fixture", "--fixture", fixturePath, "--out", out, "--arm", arm, "--max-iterations", "8", "--timeout-seconds", "10.0"}, &stdout, &stderr)
			if status != 0 {
				t.Fatalf("status=%d stderr=%s", status, &stderr)
			}
			var result sessionResult
			resultBytes, err := os.ReadFile(filepath.Join(out, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(resultBytes, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Rounds) != 2 || result.Rounds[1].Final != "round finished" || len(result.Calls) != 5 || !result.CostComplete || !result.Cleanup.RegistryClosed || result.Cleanup.LiveWorkers != 0 {
				t.Fatalf("incomplete execution: %+v", result)
			}
			mu.Lock()
			forwarded := requests[len(requests)-1]
			mu.Unlock()
			full, refs := 0, 0
			for _, message := range forwarded {
				if message.Role != schema.Tool {
					continue
				}
				if hasFullResult(message.Content, body) {
					full++
				}
				if strings.Contains(message.Content, `"result_ref"`) {
					refs++
				}
			}
			if arm == "a" && (full != 3 || refs != 0 || result.InputMetrics.ReferenceCount != 0) {
				t.Fatalf("A did not retain native full context: full=%d refs=%d", full, refs)
			}
			if arm == "b" && (full != 1 || refs != 2 || result.InputMetrics.ReferenceCount == 0) {
				t.Fatalf("B SDK request did not retain exactly one full: full=%d refs=%d", full, refs)
			}
			for _, message := range forwarded {
				if message.Role != schema.Tool {
					continue
				}
				var doc struct {
					Agents []struct {
						Ref *resultReference `json:"result_ref"`
					} `json:"agents"`
				}
				if err := json.Unmarshal([]byte(message.Content), &doc); err != nil {
					t.Fatal(err)
				}
				for _, entry := range doc.Agents {
					if entry.Ref == nil {
						continue
					}
					found := false
					for _, source := range forwarded {
						if source.Role == schema.Tool && source.ToolCallID == entry.Ref.ToolCallID && hasFullResult(source.Content, body) {
							found = entry.Ref.AgentID == "actual-worker" && entry.Ref.Generation == 4 && entry.Ref.SHA256 == resultHash(body, "")
						}
					}
					if !found {
						t.Fatal("SDK reference no longer has its exact full source in this request")
					}
				}
			}
			var receipts []toolReceipt
			data, _ := os.ReadFile(filepath.Join(out, "notifications.json"))
			if err := json.Unmarshal(data, &receipts); err != nil {
				t.Fatal(err)
			}
			if len(receipts) != 3 {
				t.Fatalf("native wait receipts=%d", len(receipts))
			}
			for _, receipt := range receipts {
				if !hasFullResult(receipt.Result, body) || receipt.CallID == "" {
					t.Fatal("actual Registry output was replaced/truncated")
				}
			}
			transcript, _ := os.ReadFile(filepath.Join(out, "transcript.json"))
			if strings.Contains(string(transcript), `result_ref`) {
				t.Fatal("candidate references polluted persistent manager history")
			}
			if err := filepath.WalkDir(out, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if info.Mode().Perm() != 0o600 {
					return fmt.Errorf("evidence mode=%o", info.Mode().Perm())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIRefusesInvalidInputsAndNeverOverwritesEvidence(t *testing.T) {
	for _, args := range [][]string{nil, {"--arm", "c"}, {"--unknown"}, {"--config", "missing", "--provider", "p", "--fixture", "missing", "--out", "unused", "--arm", "a"}, {"--timeout-seconds", "0"}} {
		if runCLI(context.Background(), args, io.Discard, io.Discard) != 2 {
			t.Fatalf("invalid args accepted: %q", args)
		}
	}
	root := t.TempDir()
	out := filepath.Join(root, "out")
	if err := privateFile(out, []byte("keep")); err != nil {
		t.Fatal(err)
	}
	if err := privateFile(out, []byte("overwrite")); err == nil {
		t.Fatal("private evidence was overwritten")
	}
}

func TestActualSDKCancellationRetainsFailureAndCannotClaimCompleteCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	fixturePath := filepath.Join(root, "case.json")
	writeTestJSON(t, fixturePath, validFixture())
	cfg := config.Default()
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: server.URL, APIKey: "private-key", Model: "fixture-model"}}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := privateFile(configPath, data); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "evidence")
	var stderr bytes.Buffer
	status := runCLI(context.Background(), []string{"--config", configPath, "--provider", "fixture", "--fixture", fixturePath, "--out", out, "--arm", "b", "--timeout-seconds", "0.1"}, io.Discard, &stderr)
	if status != 2 {
		t.Fatalf("cancelled request was accepted: status=%d %s", status, &stderr)
	}
	var result sessionResult
	raw, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Error == "" || result.CostComplete || len(result.Rounds) != 1 || result.Rounds[0].Error == "" || !result.Cleanup.RegistryClosed {
		t.Fatalf("cancellation evidence missing: %+v", result)
	}
}
