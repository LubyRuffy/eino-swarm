package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A full recent-state preview is not task recall. Two actual manager-spawned
// workers must find different older records before either calls read_work.
// Reading detail and correcting its source then exercise the same live model
// boundary again, rather than accepting a one-time prompt assembled at spawn.
func TestNuwaWorkersRecallRelatedOlderRecordsAtActualHTTP(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory=%v/fork=%v", memory, memory), func(t *testing.T) {
			const account, planner, modelA, modelB = "recall-fixture", "recall-planner", "recall-worker-a", "recall-worker-b"
			var e *Engine
			var sourceA, sourceB, dependent, foreign store.NuwaNode
			var managerCalls, callsA, callsB atomic.Int32
			var mu sync.Mutex
			var managers, workersA, workersB []scheduleCompletionRequest
			var spawnedA, spawnedB toolScopeWorkerID
			topicA, topicB := store.NewID("topic_"), store.NewID("topic_")
			detailA, detailB, corrected := store.NewID("detail_"), store.NewID("detail_"), store.NewID("corrected_")
			resultA, resultB, persona := store.NewID("result_"), store.NewID("result_"), store.NewID("persona_")
			human := "Preserve this human reference: " + store.NewID("human_")

			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire struct {
					scheduleCompletionRequest
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture input", http.StatusBadRequest)
					return
				}
				request := wire.scheduleCompletionRequest
				if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
					return
				}
				answer := func(text string) {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop")
				}
				call := func(id, name string, args any) {
					raw, err := json.Marshal(args)
					if err != nil {
						t.Error(err)
						answer("The fixture arguments could not be encoded.")
						return
					}
					nuwaValidationTool(w, request, id, name, string(raw))
				}
				switch wire.Model {
				case modelA:
					mu.Lock()
					workersA = append(workersA, request)
					mu.Unlock()
					switch callsA.Add(1) {
					case 1:
						call("recall-read-a", "read_work", map[string]any{"id": sourceA.ID})
					case 2:
						assertNuwaRecallDetailAtHTTP(t, request, "recall-read-a", sourceA.ID, sourceA.Version, detailA)
						update := sourceA
						update.Summary, update.Detail = topicA+" "+corrected, corrected
						if err := e.store.UpsertNuwaNode(t.Context(), &update, sourceA.Version); err != nil {
							t.Errorf("commit inter-request source correction: %v", err)
						}
						call("recall-refresh-a", "list_models", struct{}{})
					case 3:
						if receipt := nuwaValidationReceipt(request, "recall-refresh-a"); receipt == "" || !strings.Contains(receipt, modelA) {
							t.Error("worker did not execute its actual refresh coordination tool")
						}
						call("recall-read-a-again", "read_work", map[string]any{"id": sourceA.ID})
					case 4:
						assertNuwaRecallDetailAtHTTP(t, request, "recall-read-a-again", sourceA.ID, sourceA.Version+1, corrected)
						answer(resultA)
					default:
						t.Error("unexpected extra first-worker model request")
						answer(resultA)
					}
				case modelB:
					mu.Lock()
					workersB = append(workersB, request)
					mu.Unlock()
					switch callsB.Add(1) {
					case 1:
						call("recall-read-b", "read_work", map[string]any{"id": sourceB.ID})
					case 2:
						assertNuwaRecallDetailAtHTTP(t, request, "recall-read-b", sourceB.ID, sourceB.Version, detailB)
						answer(resultB)
					default:
						t.Error("unexpected extra second-worker model request")
						answer(resultB)
					}
				case planner:
					mu.Lock()
					managers = append(managers, request)
					mu.Unlock()
					switch managerCalls.Add(1) {
					case 1:
						call("recall-catalog", "list_models", struct{}{})
					case 2:
						var catalog struct {
							Models []nuwaModel `json:"models"`
						}
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "recall-catalog")), &catalog); err != nil {
							t.Error(err)
						}
						for _, option := range catalog.Models {
							if option.ProviderID == account && option.Model == modelA {
								call("recall-spawn-a", "spawn_agent", map[string]any{
									"role": "archive-reader-a", "task": "Inspect related project records for " + topicA,
									"model_ref": option.Ref, "tool_names": []string{}, "fork_context": memory,
								})
								return
							}
						}
						t.Error("actual catalog omitted the first worker model")
						answer("The first worker model was unavailable.")
					case 3:
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "recall-spawn-a")), &spawnedA); err != nil || spawnedA.AgentID == "" || spawnedA.Generation != 1 {
							t.Errorf("first actual worker did not spawn: %+v err=%v", spawnedA, err)
							answer("The first worker could not start.")
							return
						}
						call("recall-wait-a", "wait_agents", map[string]any{"agent_ids": []string{spawnedA.AgentID}, "timeout_s": 5})
					case 4:
						var collected toolScopeWait
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "recall-wait-a")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 {
							t.Errorf("first actual worker generation was not collected: %+v err=%v", collected, err)
						}
						for _, worker := range collected.Agents {
							if worker.AgentID != spawnedA.AgentID || worker.Status != "done" || worker.Generation != 1 || worker.Result != resultA {
								t.Errorf("wrong first collected worker result: %+v", worker)
							}
						}
						call("recall-spawn-b", "spawn_agent", map[string]any{
							"role": "archive-reader-b", "task": "Inspect related project records for " + topicB,
							"model_ref": provider.ChoiceID(account, modelB), "tool_names": []string{}, "fork_context": memory,
						})
					case 5:
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "recall-spawn-b")), &spawnedB); err != nil || spawnedB.AgentID == "" || spawnedB.Generation != 1 {
							t.Errorf("second actual worker did not spawn: %+v err=%v", spawnedB, err)
							answer("The second worker could not start.")
							return
						}
						call("recall-wait-b", "wait_agents", map[string]any{"agent_ids": []string{spawnedB.AgentID}, "timeout_s": 5})
					case 6:
						var collected toolScopeWait
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "recall-wait-b")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 {
							t.Errorf("second actual worker generation was not collected: %+v err=%v", collected, err)
						}
						for _, worker := range collected.Agents {
							if worker.AgentID != spawnedB.AgentID || worker.Status != "done" || worker.Generation != 1 || worker.Result != resultB {
								t.Errorf("wrong second collected worker result: %+v", worker)
							}
						}
						answer("The recorded worker results were collected.")
					default:
						t.Error("unexpected extra manager model request")
						answer("The coordination request was unexpected.")
					}
				default:
					t.Errorf("unexpected model route %q", wire.Model)
					answer("The model route was unexpected.")
				}
			}))
			t.Cleanup(endpoint.Close)
			e = newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = memory, false
			e.cfg.Personality.Instructions = persona
			e.cfg.MCP.Servers = nil
			e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{
				ID: account, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, modelA, modelB}, ContextWindow: 2_000_000,
			}}}
			e.pool = provider.New(e.cfg)
			project, seedThread, seedTurn := nuwaContextHTTPSource(t, e, "Task-related shared records")
			makeNode := func(kind, title, summary, detail string, prerequisites []string) store.NuwaNode {
				node := store.NuwaNode{ProjectID: project.ID, ThreadID: seedThread.ID, TurnID: seedTurn.ID,
					AgentID: swarm.DefaultManagerID, Kind: kind, Title: title, Summary: summary, Detail: detail, DependsOn: prerequisites}
				if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
					t.Fatal(err)
				}
				return node
			}
			sourceA = makeNode("fact", topicA, store.NewID("observation_"), detailA, nil)
			sourceB = makeNode("fact", store.NewID("archive_"), topicB, detailB, nil)
			dependent = makeNode("task", topicA+" "+store.NewID("dependent_"), "", "", []string{sourceA.ID})
			var noise []store.NuwaNode
			for i := 0; i < 12; i++ {
				noise = append(noise, makeNode("fact", store.NewID("unrelated_"), store.NewID("observation_"), "", nil))
			}
			foreignProject, foreignThread, foreignTurn := nuwaContextHTTPSource(t, e, "Foreign matching records")
			foreign = nuwaValidationNode(t, e, foreignProject, foreignThread, foreignTurn, topicA+" "+topicB, store.NewID("foreign_detail_"))

			// Size the fixture to exactly four recent metadata records. The
			// old related pair still fits when ranked first; no old record fits
			// behind the full recent page. This uses the existing character
			// budget, not a new count limit or a shortened model-output budget.
			revision, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			var recent []store.NuwaNode
			for _, wanted := range noise[len(noise)-4:] {
				for _, node := range nodes {
					if node.ID == wanted.ID {
						node.Detail = ""
						recent = append(recent, node)
					}
				}
			}
			if len(recent) != 4 {
				t.Fatal("fixture did not retain its four recent records")
			}
			envelope, err := json.Marshal(map[string]any{
				"project_id": project.ID, "status": "available", "revision": revision,
				"total": len(nodes), "omitted": len(nodes) - len(recent), "nodes": recent,
			})
			if err != nil {
				t.Fatal(err)
			}
			e.cfg.Tools.ResultMaxChars = utf8.RuneCountInString(nuwaContextPrefix) + utf8.RuneCount(envelope)
			thread, err := e.CreateThread("Related work delegation", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, human)
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			mu.Lock()
			managerInputs := append([]scheduleCompletionRequest(nil), managers...)
			inputsA := append([]scheduleCompletionRequest(nil), workersA...)
			inputsB := append([]scheduleCompletionRequest(nil), workersB...)
			mu.Unlock()
			if finished.Status != store.TurnDone || len(managerInputs) != 6 || len(inputsA) != 4 || len(inputsB) != 2 {
				t.Fatalf("recall HTTP flow: turn=%+v manager=%d first=%d second=%d", finished, len(managerInputs), len(inputsA), len(inputsB))
			}
			managerView := nuwaContextHTTPBlock(t, managerInputs[0], true)
			if len(managerView.Nodes) == 0 || managerView.Nodes[0].ID != noise[len(noise)-1].ID {
				t.Error("manager no-task preview lost its existing recent-first ordering")
			}
			for _, node := range managerView.Nodes {
				if node.ID == sourceA.ID || node.ID == sourceB.ID || node.ID == dependent.ID {
					t.Error("fixture did not push the older records out of the manager's recent page")
				}
			}
			firstA := nuwaContextHTTPBlock(t, inputsA[0], true)
			firstB := nuwaContextHTTPBlock(t, inputsB[0], true)
			for _, check := range []struct {
				view  nuwaContextHTTPView
				node  store.NuwaNode
				topic string
			}{{firstA, sourceA, topicA}, {firstB, sourceB, topicB}} {
				found := nuwaContextHTTPNode(t, check.view, check.node.ID)
				if found.Version != check.node.Version || found.Detail != "" {
					t.Errorf("task recall omitted the current metadata or injected detail: %+v", found)
				}
				if len(check.view.Nodes) == 0 || !strings.Contains(check.view.Nodes[0].Title+" "+check.view.Nodes[0].Summary, check.topic) {
					t.Error("worker preview did not prioritize records related to its own task")
				}
			}
			updated := nuwaContextHTTPBlock(t, inputsA[2], true)
			changed := nuwaContextHTTPNode(t, updated, sourceA.ID)
			if changed.Version != sourceA.Version+1 || changed.Summary != topicA+" "+corrected || changed.Detail != "" || updated.Revision <= firstA.Revision {
				t.Errorf("related recall kept stale or expanded source data: %+v", changed)
			}
			stale := nuwaContextHTTPNode(t, updated, dependent.ID).StaleDependencies
			if len(stale) != 1 || stale[0] != sourceA.ID {
				t.Errorf("corrected related prerequisite lost its invalidation: %v", stale)
			}
			if !strings.Contains(joinedContent(managerInputs[0].Messages), persona) {
				t.Error("fixture did not exercise manager personality isolation")
			}
			for _, request := range append(append([]scheduleCompletionRequest(nil), inputsA...), inputsB...) {
				all := joinedContent(request.Messages)
				if strings.Contains(all, persona) || strings.Contains(all, nuwaPlannerInstruction) {
					t.Error("task recall leaked the manager personality or planner instruction")
				}
				if memory && !strings.Contains(all, human) {
					t.Error("forked worker lost the original human input")
				}
			}
			for _, request := range append(append(managerInputs, inputsA...), inputsB...) {
				if strings.Contains(joinedContent(request.Messages), foreign.ID) || strings.Contains(joinedContent(request.Messages), foreign.Detail) {
					t.Error("task recall disclosed another project's matching record")
				}
			}
			assertNuwaContextNotDurable(t, e, thread.ID, human)
		})
	}
}

func assertNuwaRecallDetailAtHTTP(t *testing.T, request scheduleCompletionRequest, callID, nodeID string, version int64, detail string) {
	t.Helper()
	var reply struct {
		Node store.NuwaNode `json:"node"`
	}
	if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, callID)), &reply); err != nil {
		t.Errorf("actual read_work receipt was not JSON: %v", err)
		return
	}
	if reply.Node.ID != nodeID || reply.Node.Version != version || reply.Node.Detail != detail {
		t.Errorf("actual read_work did not return its full current source: %+v", reply.Node)
	}
}
