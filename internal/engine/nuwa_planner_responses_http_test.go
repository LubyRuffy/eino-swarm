package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type nuwaPlannerResponsesRequest struct {
	Model      string          `json:"model"`
	Input      json.RawMessage `json:"input"`
	Stream     bool            `json:"stream"`
	ToolChoice string          `json:"tool_choice"`
	Tools      []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tools"`
}

// Hosted execution tools are added below the local Registry tool surface.
// Inspect the actual Responses HTTP body so filtering only ManagerTools cannot
// satisfy the planner contract while leaving upstream execution available.
func TestNuwaPlannerResponsesRequestExcludesHostedExecutionTools(t *testing.T) {
	for _, nuwa := range []bool{true, false} {
		t.Run(fmt.Sprintf("nuwa=%v", nuwa), func(t *testing.T) {
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request nuwaPlannerResponsesRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture body", http.StatusBadRequest)
					return
				}
				if replyNoScheduleClaimsResponsesFixture(t, w, request.Input, request.Stream, request.ToolChoice) {
					return
				}
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/responses" || !request.Stream {
					t.Errorf("manager missed actual streaming Responses boundary: method=%s path=%s stream=%v", r.Method, r.URL.Path, request.Stream)
				}
				assertNuwaPlannerResponsesHostedTools(t, request, !nuwa)
				writeAnswerResponses(w, request.Stream, []map[string]any{{
					"type": "message", "id": "planner-response", "role": "assistant",
					"content": []map[string]any{{"type": "output_text", "text": "The available coordination surface was inspected."}},
				}})
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
			e.cfg.Models = config.ModelsConfig{Default: "responses-separation", Providers: []config.Provider{{
				ID: "responses-separation", BaseURL: endpoint.URL, Model: "responses-planner",
				APIMode: config.APIModeResponses, BuiltinTools: nuwaPlannerResponsesHostedTools(),
			}}}
			e.pool = provider.New(e.cfg)
			project, err := e.CreateProject("Responses planning boundary", "", "", false, nuwa)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Inspect Responses coordination", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect the available coordination surface.")
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if finished.Status != store.TurnDone || calls.Load() != 1 {
				t.Fatalf("Responses coordination request failed: turn=%+v calls=%d", finished, calls.Load())
			}
		})
	}
}

// Restricting one planner instance must not mutate its provider or the model
// factory used for real workers. Continue the actual Responses manager graph
// through selected-model spawn, worker completion, and result collection.
func TestNuwaPlannerResponsesWorkersKeepHostedExecutionTools(t *testing.T) {
	const account, planner, executor = "responses-delegation", "planning-model", "execution-model"
	const workerResult = "The assigned evidence was inspected."
	var managerCalls, workerCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nuwaPlannerResponsesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture body", http.StatusBadRequest)
			return
		}
		if replyNoScheduleClaimsResponsesFixture(t, w, request.Input, request.Stream, request.ToolChoice) {
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/responses" || !request.Stream {
			t.Errorf("actor missed actual streaming Responses boundary: model=%q method=%s path=%s stream=%v", request.Model, r.Method, r.URL.Path, request.Stream)
		}
		answer := func(id, text string) {
			writeAnswerResponses(w, request.Stream, []map[string]any{{
				"type": "message", "id": id, "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": text}},
			}})
		}
		if request.Model == executor {
			workerCalls.Add(1)
			assertNuwaPlannerResponsesHostedTools(t, request, true)
			answer("executor-answer", workerResult)
			return
		}
		if request.Model != planner {
			t.Errorf("unexpected actor model %q", request.Model)
		}
		assertNuwaPlannerResponsesHostedTools(t, request, false)
		var input []answerResponsesItem
		if err := json.Unmarshal(request.Input, &input); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		receipt := func(id string) string {
			for _, item := range input {
				if item.Type == "function_call_output" && item.CallID == id {
					return item.Output
				}
			}
			return ""
		}
		call := func(id, name string, args any) {
			raw, err := json.Marshal(args)
			if err != nil {
				t.Error(err)
				http.Error(w, "invalid tool arguments", http.StatusBadRequest)
				return
			}
			writeAnswerResponses(w, request.Stream, []map[string]any{{
				"type": "function_call", "id": "item-" + id, "call_id": id,
				"name": name, "arguments": string(raw),
			}})
		}
		switch managerCalls.Add(1) {
		case 1:
			call("responses-delegation-spawn", "spawn_agent", map[string]string{
				"role": "evidence-executor", "task": "Inspect the assigned evidence and report the observation.",
				"model_ref": nuwaModelRef(account, executor),
			})
		case 2:
			var spawned toolScopeWorkerID
			if err := json.Unmarshal([]byte(receipt("responses-delegation-spawn")), &spawned); err != nil || spawned.AgentID == "" || spawned.Generation != 1 {
				t.Errorf("actual Responses spawn did not create a worker: %+v err=%v", spawned, err)
				answer("spawn-refused", "The worker could not be started.")
				return
			}
			call("responses-delegation-wait", "wait_agents", map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
		case 3:
			var collected toolScopeWait
			if err := json.Unmarshal([]byte(receipt("responses-delegation-wait")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 || collected.Agents[0].Status != "done" || collected.Agents[0].Result != workerResult {
				t.Errorf("actual Responses manager did not collect the worker result: %+v err=%v", collected, err)
			}
			answer("planner-collected", "The executor result was collected.")
		default:
			t.Error("unexpected Responses manager request")
			answer("planner-unexpected", "The manager request was unexpected.")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{
		ID: account, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, executor},
		APIMode: config.APIModeResponses, BuiltinTools: nuwaPlannerResponsesHostedTools(),
	}}}
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Responses delegated execution", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Responses worker collection", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate the inspection and collect its result.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if finished.Status != store.TurnDone || managerCalls.Load() != 3 || workerCalls.Load() != 1 {
		t.Fatalf("Responses delegation failed: turn=%+v manager=%d worker=%d", finished, managerCalls.Load(), workerCalls.Load())
	}
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil || len(view.Workers) != 1 || view.Workers[0].Status != "done" || view.Workers[0].ModelBinding == nil || view.Workers[0].ModelBinding.Ref != provider.ChoiceID(account, executor) {
		t.Fatalf("Responses worker lost its selected model or durable completion: workers=%+v err=%v", view.Workers, err)
	}
}

func nuwaPlannerResponsesHostedTools() []map[string]any {
	return []map[string]any{
		{"type": "web_search"},
		{"type": "file_search", "vector_store_ids": []string{"vs_fixture"}},
		{"type": "code_interpreter", "container": map[string]any{"type": "auto"}},
	}
}

func assertNuwaPlannerResponsesHostedTools(t *testing.T, request nuwaPlannerResponsesRequest, wantHosted bool) {
	t.Helper()
	available := make(map[string]bool, len(request.Tools))
	spawn := false
	for _, candidate := range request.Tools {
		available[candidate.Type] = true
		spawn = spawn || candidate.Type == "function" && candidate.Name == "spawn_agent"
		if !wantHosted && candidate.Type != "function" {
			t.Errorf("Nuwa planner received upstream execution tool %q", candidate.Type)
		}
	}
	if !spawn {
		t.Error("actual Responses request lost local delegation tools")
	}
	for _, kind := range []string{"web_search", "file_search", "code_interpreter"} {
		if available[kind] != wantHosted {
			t.Errorf("actual Responses hosted tool %s available=%v want=%v", kind, available[kind], wantHosted)
		}
	}
}
