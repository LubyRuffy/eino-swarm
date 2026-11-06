package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An ID survives resume and restoration. Its model-call generation must come
// from that execution, while a request already in flight keeps its starting
// turn even when the Registry is claimed by the next manager.
func TestWorkerLLMCallGenerationsPersistAcrossResumeRestoreAndTurnOwnershipAtHTTP(t *testing.T) {
	arrived, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	releaseStream := func() { finishOnce.Do(func() { close(finish) }) }
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"held-generation\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Inspecting\"},\"finish_reason\":null}]}\n\n")
			w.(http.Flusher).Flush()
			close(arrived)
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
		}
		writeAccountingStream(w, map[string]any{"content": "The assigned inspection completed."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e, thread := newAccountingEngine(t, endpoint.URL)
	first := plantUnfinishedTurn(t, e, thread.ID, "Inspect the current evidence.")
	second := plantUnfinishedTurn(t, e, thread.ID, "Inspect the follow-up evidence.")
	reg := accountingRegistry(t, e, first)
	rt := e.runtimeFor(thread.ID)
	registries := []*swarm.Registry{reg}
	var releaseOwner func()
	occupy := func(current *swarm.Registry, turnID string) func() {
		_, release := ownManagerTestFixture(t.Context(), rt, turnID, current)
		rt.mu.Lock()
		owned := rt.running && rt.turnID == turnID && rt.reg == current
		rt.mu.Unlock()
		if !owned {
			release()
			t.Fatal("HTTP fixture does not own the executing turn and Registry")
		}
		return release
	}
	t.Cleanup(func() {
		releaseStream()
		for _, current := range registries {
			current.Cleanup()
			if err := current.Wait(context.Background(), 5*time.Second); err != nil {
				t.Error(err)
			}
			current.Close()
		}
		if releaseOwner != nil {
			releaseOwner()
		}
	})
	collect := func(current *swarm.Registry, id string, generation int) {
		out := invokeModelRequirementTool(t, current, "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, id))
		var receipt toolScopeWait
		if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.TimedOut || len(receipt.Agents) != 1 ||
			receipt.Agents[0].AgentID != id || receipt.Agents[0].Generation != generation || receipt.Agents[0].Status != "done" {
			t.Fatalf("worker generation was not collected: %s err=%v", out, err)
		}
	}

	releaseOwner = occupy(reg, first.ID)
	h, err := reg.Spawn(t.Context(), "inspector", "Inspect the assigned evidence.", reg.WorkerConfiguration().ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	awaitAccounting(t, arrived)
	rt.parkRegistry(reg)
	releaseOwner()
	releaseOwner = nil
	if rt.takeParkedRegistry() != reg {
		t.Fatal("successor lost the retained Registry")
	}
	replaceAccountingFactory(reg, accountingBuilder(t, e, second))
	releaseOwner = occupy(reg, second.ID)
	attachHostNotify(reg, newAccumulator(e, thread.ID, second.ID, 0))
	releaseStream()
	awaitAccounting(t, h.Done())
	collect(reg, h.ID, 1)
	for generation := 2; generation <= 3; generation++ {
		out := invokeModelRequirementTool(t, reg, "resume_agent", fmt.Sprintf(`{"agent_id":%q,"task":"Inspect the next supplied evidence."}`, h.ID))
		var resumed toolScopeWorkerID
		if err := json.Unmarshal([]byte(out), &resumed); err != nil || resumed.AgentID != h.ID || resumed.Generation != generation {
			t.Fatalf("resume changed worker identity/generation: %s err=%v", out, err)
		}
		collect(reg, h.ID, generation)
	}
	var saved swarm.WorkerCheckpoint
	for _, candidate := range reg.SnapshotWorkers() {
		if candidate.ID == h.ID {
			saved = candidate
		}
	}
	if saved.Generation != 3 || saved.ID != h.ID {
		t.Fatalf("actual checkpoint lost generation: %+v", saved)
	}
	releaseOwner()
	releaseOwner = nil
	reg.Close()
	restored := swarm.NewRegistry()
	registries = append(registries, restored)
	restored.ConfigureWorkers(swarm.WorkerConfig{ModelBuilder: accountingBuilder(t, e, second)})
	attachHostNotify(restored, newAccumulator(e, thread.ID, second.ID, 0))
	releaseOwner = occupy(restored, second.ID)
	recovered, err := restored.Restore(t.Context(), swarm.RestoredWorker{
		ID: saved.ID, Role: saved.Role, Task: saved.Task, Instruction: saved.Instruction,
		History: saved.History, Generation: saved.Generation,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(restored, recovered.ID, 3)
	if _, err := restored.RunWith(t.Context(), swarm.RunConfig{Task: "Review the collected inspection."}, nil); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 5 {
		t.Fatalf("actual model requests=%d want=5", requests.Load())
	}
	for _, tc := range []struct {
		turn        *store.Turn
		generations []int
		manager     bool
	}{{first, []int{1}, false}, {second, []int{2, 3, 3}, true}} {
		calls, err := e.store.ListLLMCalls(tc.turn.ID)
		wantCount := len(tc.generations)
		if tc.manager {
			wantCount++
		}
		if err != nil || len(calls) != wantCount {
			t.Fatalf("request starting-turn ownership changed: turn=%s calls=%+v err=%v", tc.turn.ID, calls, err)
		}
		body, err := json.Marshal(calls)
		if err != nil {
			t.Fatal(err)
		}
		// Missing JSON generation remains zero on the old implementation, so
		// this fails on persisted behavior instead of a nonexistent Go field.
		var projected []struct {
			AgentID    string `json:"agent_id"`
			TurnID     string `json:"turn_id"`
			Generation int    `json:"generation"`
		}
		if err := json.Unmarshal(body, &projected); err != nil {
			t.Fatal(err)
		}
		var got []int
		managerCalls := 0
		for i, call := range projected {
			if call.TurnID != tc.turn.ID || calls[i].TotalTokens != 11 {
				t.Errorf("persisted call owner/usage changed: %+v", calls[i])
			}
			if call.AgentID == swarm.DefaultManagerID {
				managerCalls++
				if call.Generation != 0 {
					t.Errorf("manager inherited worker generation %d", call.Generation)
				}
			} else if call.AgentID == h.ID {
				got = append(got, call.Generation)
			} else {
				t.Errorf("unexpected persisted model actor: %+v", call)
			}
		}
		if !reflect.DeepEqual(got, tc.generations) || (managerCalls == 1) != tc.manager {
			t.Errorf("persisted JSON generations: turn=%s worker=%v want=%v manager_calls=%d", tc.turn.ID, got, tc.generations, managerCalls)
		}
	}
}
