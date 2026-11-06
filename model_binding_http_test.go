package swarm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/components/tool"
)

type bindingHTTPFixture struct {
	mu     sync.Mutex
	models []string
}

func newBindingHTTPPool(t *testing.T) (*provider.Pool, *bindingHTTPFixture) {
	t.Helper()
	f := &bindingHTTPFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("request: %v", err)
			w.WriteHeader(400)
			return
		}
		f.mu.Lock()
		f.models = append(f.models, request.Model)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"binding-output\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"completed\"}}]}\n\n", request.Model)
		fmt.Fprint(w, "data: {\"id\":\"binding-output\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: srv.URL + "/v1", Model: "manager-default", Catalog: []string{"manager-default", "worker-one", "worker-two"}}}
	return provider.New(cfg), f
}

func bindHTTPWorkerModels(reg *swarm.Registry, pool *provider.Pool, recorder provider.Recorder) {
	reg.SelectWorkerModel = func(ctx context.Context, ref string) (swarm.ModelBinding, swarm.ModelBuilder, error) {
		name := "worker-one"
		switch ref {
		case "", "one":
		case "two":
			name = "worker-two"
		default:
			return swarm.ModelBinding{}, nil, fmt.Errorf("unknown model reference %q", ref)
		}
		binding, build, err := pool.WorkerModel(ctx, "fixture", name, "low", recorder)
		binding.Ref = ref
		return binding, build, err
	}
	reg.RestoreWorkerModel = func(ctx context.Context, binding swarm.ModelBinding) (swarm.ModelBuilder, error) {
		return pool.RestoreWorkerModel(ctx, binding, recorder)
	}
}

func waitBindingWorkers(t *testing.T, reg *swarm.Registry, ids ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		args, _ := json.Marshal(map[string]any{"agent_ids": ids, "timeout_s": 1})
		out := callBindingTool(t, reg, "wait_agents", string(args))
		if !strings.Contains(out, `"status":"running"`) {
			return
		}
	}
	t.Fatal("workers did not finish")
}

// Selection must reach a real HTTP request; model labels in spawn prose alone
// cannot prove routing, independent manager choice, billing, or pinned recovery.
func TestSelectedHTTPWorkerModelSurvivesResumeArchiveAndRestore(t *testing.T) {
	pool, fixture := newBindingHTTPPool(t)
	var mu sync.Mutex
	var records []provider.CallRecord
	recorder := func(record provider.CallRecord) { mu.Lock(); defer mu.Unlock(); records = append(records, record) }
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	var err error
	reg.ModelBuilder, err = pool.ModelBuilder(context.Background(), "fixture", "", "", recorder)
	if err != nil {
		t.Fatal(err)
	}
	bindHTTPWorkerModels(reg, pool, recorder)
	if _, err := reg.RunWith(context.Background(), swarm.RunConfig{Task: "inspect the objective"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		`{"role":"first","task":"inspect first input","model_ref":"one"}`,
		`{"role":"second","task":"inspect second input","model_ref":"two"}`,
	} {
		callBindingTool(t, reg, "spawn_agent", args)
	}
	waitBindingWorkers(t, reg, "first-1", "second-2")
	if out := callBindingTool(t, reg, "spawn_agent", `{"role":"first","task":"inspect followup"}`); !strings.Contains(out, `"resumed_from":"first-1"`) {
		t.Fatal(out)
	}
	waitBindingWorkers(t, reg, "first-1")
	reg.Stats() // The finished archive must preserve the binding too.
	if out := callBindingTool(t, reg, "spawn_agent", `{"role":"first","task":"inspect with the same explicit model","model_ref":"one"}`); strings.Contains(out, `"error"`) {
		t.Fatal(out)
	}
	waitBindingWorkers(t, reg, "first-1")
	callBindingTool(t, reg, "resume_agent", `{"agent_id":"first-1","task":"inspect another followup"}`)
	waitBindingWorkers(t, reg, "first-1")
	var saved swarm.WorkerCheckpoint
	for _, cp := range reg.SnapshotWorkers() {
		if cp.ID == "first-1" {
			saved = cp
		}
	}
	if saved.Generation != 4 || saved.ModelBinding == nil || saved.ModelBinding.Model != "worker-one" || saved.ModelBinding.ReasoningEffort != "low" {
		t.Fatalf("missing concrete binding or generation: %+v", saved)
	}
	body, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint swarm.WorkerCheckpoint
	if err := json.Unmarshal(body, &checkpoint); err != nil {
		t.Fatal(err)
	}
	// A changed catalog can no longer resolve the old ref, but recovery uses
	// the concrete provider/model, and does not silently follow that catalog.
	restored := swarm.NewRegistry()
	t.Cleanup(restored.Close)
	bindHTTPWorkerModels(restored, pool, recorder)
	restored.SelectWorkerModel = func(context.Context, string) (swarm.ModelBinding, swarm.ModelBuilder, error) {
		return swarm.ModelBinding{}, nil, fmt.Errorf("catalog entry removed")
	}
	worker, err := restored.Restore(context.Background(), swarm.RestoredWorker{ID: checkpoint.ID, Role: checkpoint.Role, Task: "recover assigned work", ModelBinding: checkpoint.ModelBinding, History: checkpoint.History, Generation: checkpoint.Generation}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("restored request did not complete")
	}
	if _, err, done := worker.Result(); !done || err != nil {
		t.Fatalf("restore result done=%v err=%v", done, err)
	}
	mu.Lock()
	defer mu.Unlock()
	var got []string
	generations := map[string][]int{}
	for _, r := range records {
		got = append(got, r.AgentID+":"+r.Model)
		// Reflection keeps this a semantic RED before CallRecord has the new
		// field: absence is unknown/zero, not a compilation failure.
		generation := 0
		if field := reflect.ValueOf(r).FieldByName("WorkerGeneration"); field.IsValid() && field.Kind() == reflect.Int {
			generation = int(field.Int())
		}
		generations[r.AgentID] = append(generations[r.AgentID], generation)
		if r.ProviderID != "fixture" {
			t.Errorf("wrong provider attribution %+v", r)
		}
	}
	want := map[string]int{"manager:manager-default": 1, "first-1:worker-one": 5, "second-2:worker-two": 1}
	for _, r := range got {
		want[r]--
	}
	for key, n := range want {
		if n != 0 {
			t.Errorf("request attribution %s mismatch=%d; got=%v", key, n, got)
		}
	}
	for id, want := range map[string][]int{"manager": {0}, "first-1": {1, 2, 3, 4, 4}, "second-2": {1}} {
		if !reflect.DeepEqual(generations[id], want) {
			t.Errorf("actual HTTP call generations for %s = %v, want %v", id, generations[id], want)
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.models) != 7 {
		t.Fatalf("actual request models=%v", fixture.models)
	}
}

func callBindingTool(t *testing.T, reg *swarm.Registry, name, arguments string) string {
	t.Helper()
	for _, candidate := range reg.Tools() {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == name {
			out, err := candidate.(tool.InvokableTool).InvokableRun(context.Background(), arguments)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("missing tool %s", name)
	return ""
}

// An opt-in model selection must not silently be ignored by the ordinary mode:
// otherwise an intended cheap worker executes against the manager's model.
func TestModelReferenceWithoutSelectionSupportDoesNotStartHTTPWorker(t *testing.T) {
	pool, fixture := newBindingHTTPPool(t)
	build, err := pool.ModelBuilder(context.Background(), "fixture", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	reg.ModelBuilder = build
	out := callBindingTool(t, reg, "spawn_agent", `{"role":"analysis","task":"inspect assigned input","model_ref":"not-enabled"}`)
	if !strings.Contains(out, `"error"`) {
		for _, checkpoint := range reg.SnapshotWorkers() {
			callBindingTool(t, reg, "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":2}`, checkpoint.ID))
		}
		t.Errorf("unsupported model_ref created a worker instead of refusing: %s", out)
	}
	if workers := reg.SnapshotWorkers(); len(workers) != 0 {
		t.Errorf("refusal left workers: %+v", workers)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.models) != 0 {
		t.Errorf("refusal made actual model requests: %v", fixture.models)
	}
}
