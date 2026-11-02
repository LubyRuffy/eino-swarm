package swarm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// One real parent HTTP model must be able to dispatch a child through ADK's
// tool node, while both requests retain the same Registry and selected model.
func TestWorkerCanDelegateThroughRealHTTPModelAndRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		toolResult := false
		for _, m := range request.Messages {
			toolResult = toolResult || m.Role == "tool"
		}
		delta := map[string]any{"role": "assistant", "content": "complete"}
		finish := "stop"
		if request.Model == "worker-one" && !toolResult {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "spawn-child", "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": `{"role":"child","task":"inspect child input","model_ref":"two","parent_id":"forged-parent"}`}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(map[string]any{"id": "delegation-output", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"delegation-output\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", body, finish)
	}))
	defer srv.Close()
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: srv.URL + "/v1", Model: "manager-default"}}
	pool := provider.New(cfg)
	reg := swarm.NewRegistry()
	defer reg.Close()
	reg.EnableDelegation = true
	reg.SetMaxConcurrent(3)
	reg.ModelBuilder, err = pool.ModelBuilder(context.Background(), "fixture", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bindHTTPWorkerModels(reg, pool, nil)
	out := callBindingTool(t, reg, "spawn_agent", `{"role":"parent","task":"coordinate child work","model_ref":"one"}`)
	if strings.Contains(out, `"error"`) {
		t.Fatal(out)
	}
	if err := reg.Wait(context.Background(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	workers := reg.SnapshotWorkers()
	if len(workers) != 2 {
		t.Fatalf("parent model could not delegate through real tool dispatch: workers=%+v", workers)
	}
	for _, worker := range workers {
		if worker.Err != "" {
			t.Fatalf("worker failed: %+v", worker)
		}
		if worker.ID == "child-2" && worker.ModelBinding.Model != "worker-two" {
			t.Fatalf("child model binding %+v", worker.ModelBinding)
		}
		if worker.ID == "child-2" && worker.ParentID != "parent-1" {
			t.Fatalf("model arguments forged its parent %+v", worker)
		}
	}
	encoded, err := json.Marshal(workers)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoints []swarm.WorkerCheckpoint
	if err := json.Unmarshal(encoded, &checkpoints); err != nil {
		t.Fatal(err)
	}
	restored := swarm.NewRegistry()
	defer restored.Close()
	restored.EnableDelegation = true
	restored.SetMaxConcurrent(3)
	bindHTTPWorkerModels(restored, pool, nil)
	for _, checkpoint := range checkpoints {
		if _, err := restored.Restore(context.Background(), swarm.RestoredWorker{ID: checkpoint.ID, Role: checkpoint.Role, ParentID: checkpoint.ParentID, ModelBinding: checkpoint.ModelBinding, Task: checkpoint.Task, History: checkpoint.History, Generation: checkpoint.Generation}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := restored.Wait(context.Background(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, checkpoint := range restored.SnapshotWorkers() {
		if checkpoint.Err != "" {
			t.Fatalf("restored worker failed %+v", checkpoint)
		}
		if checkpoint.ID == "child-2" && (checkpoint.ParentID != "parent-1" || checkpoint.ModelBinding.Model != "worker-two") {
			t.Fatalf("recovery changed parent/model %+v", checkpoint)
		}
	}
}

// A manager slice ending does not end its worker. Reconfiguring the next
// slice must neither race that worker's dispatch nor revoke its startup mode.
func TestParkedHTTPWorkerDelegatesWithItsOriginalConfigurationAfterNextTurn(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	models := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name       string          `json:"name"`
					Parameters json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		models = append(models, request.Model)
		mu.Unlock()
		hasResult := false
		for _, m := range request.Messages {
			hasResult = hasResult || m.Role == "tool"
		}
		var args string
		if request.Model == "manager-old" && !hasResult {
			args = `{"role":"parent","task":"coordinate a later child","model_ref":"one"}`
		}
		if request.Model == "worker-one" && !hasResult {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			args = `{"role":"child","task":"inspect child source","model_ref":"two"}`
		}
		if request.Model == "manager-new" {
			for _, offered := range request.Tools {
				if offered.Function.Name == "spawn_agent" && strings.Contains(string(offered.Function.Parameters), "model_ref") {
					t.Error("next ordinary manager retained the old mode schema")
				}
			}
		}
		delta := map[string]any{"role": "assistant", "content": "complete"}
		finish := "stop"
		if args != "" {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "dispatch-" + request.Model, "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": args}}}}
			finish = "tool_calls"
		}
		body, _ := json.Marshal(map[string]any{"id": "parked-output", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"parked-output\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", body, finish)
	}))
	defer srv.Close()
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: srv.URL + "/v1", Model: "manager-old"}}
	pool := provider.New(cfg)
	reg := swarm.NewRegistry()
	defer reg.Close()
	reg.EnableDelegation = true
	reg.SetMaxConcurrent(4)
	reg.ModelBuilder, err = pool.ModelBuilder(context.Background(), "fixture", "manager-old", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bindHTTPWorkerModels(reg, pool, nil)
	original := reg.WorkerConfiguration()
	original.Tag = "original-turn"
	reg.ConfigureWorkers(original)
	reg.ResolveWorkerConfig = func(_ context.Context, tag string) (swarm.WorkerConfig, error) {
		if tag != "original-turn" {
			return swarm.WorkerConfig{}, fmt.Errorf("unknown configuration")
		}
		return original, nil
	}
	if _, err := reg.RunWith(context.Background(), swarm.RunConfig{Task: "start coordinated work"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn did not leave a live HTTP worker")
	}
	next := original
	next.Tag = "next-turn"
	next.EnableDelegation = false
	next.SelectWorkerModel = nil
	next.ModelBuilder, err = pool.ModelBuilder(context.Background(), "fixture", "manager-new", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg.ConfigureWorkers(next)
	if _, err := reg.RunWith(context.Background(), swarm.RunConfig{Task: "an ordinary next turn"}, nil); err != nil {
		t.Fatal(err)
	}
	configured := make(chan struct{})
	go func() {
		defer close(configured)
		for range 100 {
			reg.ConfigureWorkers(next)
		}
	}()
	close(release)
	if err := reg.Wait(context.Background(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	<-configured
	workers := reg.SnapshotWorkers()
	if len(workers) != 2 {
		t.Fatalf("next-turn mode revoked old worker delegation %+v", workers)
	}
	for _, worker := range workers {
		if worker.Err != "" || worker.ConfigTag != "original-turn" || worker.DelegationEnabled == nil || !*worker.DelegationEnabled {
			t.Fatalf("worker lost startup snapshot %+v", worker)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(models, ",") == "" {
		t.Fatal("no model requests")
	}
	foundChild, foundNew := false, false
	for _, name := range models {
		foundChild = foundChild || name == "worker-two"
		foundNew = foundNew || name == "manager-new"
	}
	if !foundChild || !foundNew {
		t.Fatalf("wrong actual per-turn/child models %v", models)
	}
}
