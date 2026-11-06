package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Historical requests must keep their recorded worker generation even when the
// current roster has advanced. Older calls have no such evidence to reconstruct.
func TestTracePreservesRecordedWorkerGenerationsWithoutGuessingLegacyCalls(t *testing.T) {
	for _, nuwa := range []bool{false, true} {
		name := "ordinary"
		if nuwa {
			name = "nuwa"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			project := h.newProject(map[string]any{"name": "Request provenance", "nuwa_enabled": nuwa, "memory_enabled": false})
			thread, err := h.app.Engine.CreateThread("Recorded model requests", "", project["id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: thread.ProjectID, Status: store.TurnDone, NuwaEnabled: nuwa}
			if err := h.app.Store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			for _, event := range []store.Event{
				{Kind: "spawned", AgentID: "worker-1", Role: "reader", WorkerGeneration: 1},
				{Kind: "finished", AgentID: "worker-1", Role: "reader", WorkerGeneration: 1},
				{Kind: "spawned", AgentID: "worker-1", Role: "reader", WorkerGeneration: 2},
			} {
				event.ThreadID, event.TurnID = thread.ID, turn.ID
				if err := h.app.Store.AppendEvent(&event); err != nil {
					t.Fatal(err)
				}
			}

			cases := []struct {
				agent      string
				generation int
				include    bool
			}{
				{"worker-1", 1, true},
				{"worker-1", 2, true},
				{"worker-1", 0, false}, // A legacy request must not inherit the latest roster generation.
				{"worker-1", 0, true},
				{"manager", 0, false},
				{"memory-reviewer", 0, false},
			}
			for i, tc := range cases {
				seed := map[string]any{
					"thread_id": thread.ID, "turn_id": turn.ID, "agent_id": tc.agent,
					"provider_id": "fixture", "model": "fixture-model", "total_tokens": i + 11,
				}
				if tc.include {
					seed["generation"] = tc.generation
				}
				raw, err := json.Marshal(seed)
				if err != nil {
					t.Fatal(err)
				}
				// Decode the wire field rather than naming the new Go field: the old
				// schema compiles but drops it, producing a semantic HTTP regression.
				var call store.LLMCall
				if err := json.Unmarshal(raw, &call); err != nil {
					t.Fatal(err)
				}
				if err := h.app.Store.AppendLLMCall(&call); err != nil {
					t.Fatal(err)
				}
			}

			trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
			calls, ok := trace["llm_calls"].([]any)
			if !ok || len(calls) != len(cases) {
				t.Fatalf("Trace lost recorded calls: %v", trace["llm_calls"])
			}
			for i, tc := range cases {
				call := calls[i].(map[string]any)
				if call["agent_id"] != tc.agent || call["thread_id"] != thread.ID || call["turn_id"] != turn.ID ||
					call["provider_id"] != "fixture" || call["model"] != "fixture-model" || call["total_tokens"] != float64(i+11) {
					t.Errorf("Trace changed request identity or usage: %v", call)
				}
				generation, present := call["generation"]
				if tc.generation > 0 {
					if !present || generation != float64(tc.generation) {
						t.Errorf("recorded worker generation lost after storage and HTTP: call=%d got=%v want=%d", i, generation, tc.generation)
					}
				} else if present {
					t.Errorf("unknown/manager/auxiliary generation must stay omitted, without roster inference: call=%d generation=%v", i, generation)
				}
			}
		})
	}
}
