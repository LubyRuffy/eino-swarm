package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const sharedProjectInstructionsHTTPHeader = "## Shared project instructions"

func sharedProjectInstructionsHTTPEngine(t *testing.T, endpoint, persona string, memory bool) *Engine {
	t.Helper()
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = memory, false
	e.cfg.Personality.Instructions = persona
	e.cfg.MCP.Servers = nil
	e.cfg.Models = config.ModelsConfig{Default: "project-context-fixture", Providers: []config.Provider{{
		ID: "project-context-fixture", BaseURL: endpoint, Model: "project-context-model", ContextWindow: 2_000_000,
	}}}
	e.pool = provider.New(e.cfg)
	return e
}

func assertSharedProjectInstructionsAtHTTP(t *testing.T, request scheduleCompletionRequest, original, goal, acceptance, persona string, want bool) {
	t.Helper()
	all := joinedContent(request.Messages)
	count := 0
	if want {
		count = 1
	}
	for _, marker := range []string{original, goal, acceptance} {
		if marker != "" && strings.Count(all, marker) != count {
			t.Errorf("worker input contains project instruction/marker %d times, want %d", strings.Count(all, marker), count)
		}
	}
	var system strings.Builder
	for _, message := range request.Messages {
		if strings.Contains(message.Content, persona) || strings.Contains(message.Content, nuwaPlannerInstruction) {
			t.Errorf("worker inherited manager personality or planner policy through %s input", message.Role)
		}
		if message.Role == schema.System {
			system.WriteString(message.Content)
			system.WriteByte('\n')
		}
	}
	if got := strings.Count(system.String(), sharedProjectInstructionsHTTPHeader); got != count {
		t.Errorf("worker System has %d shared project instruction blocks, want %d", got, count)
	}
	if want && !strings.Contains(system.String(), original) {
		t.Error("project instructions did not reach the worker independently of its historical fork seed")
	}
}

// Project requirements reach a newly delegated worker independently of memory
// and fork. The fork remains conversation evidence, not the manager's persona.
func TestNuwaProjectInstructionsReachNewWorkersAtActualHTTP(t *testing.T) {
	for _, tc := range []struct {
		name         string
		nuwa, memory bool
		fork, empty  bool
	}{
		{"memory-on", true, true, false, false},
		{"memory-on-fork", true, true, true, false},
		{"memory-off", true, false, false, false},
		{"memory-off-fork", true, false, true, false},
		{"ordinary-memory-on-fork", false, true, true, false},
		{"ordinary-memory-off", false, false, false, false},
		{"empty-memory-on-fork", true, true, true, true},
		{"empty-memory-off", true, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			persona, goal, acceptance := store.NewID("persona_"), store.NewID("goal_"), store.NewID("acceptance_")
			original := "Project goal: " + goal + "\nAcceptance requirements: " + acceptance +
				"\nCoordinator responsibilities: choose collaborators and collect their results."
			if tc.empty {
				original = ""
			}
			human, assignment, result := store.NewID("human_"), store.NewID("assignment_"), store.NewID("result_")
			var managerCalls atomic.Int32
			var mu sync.Mutex
			var managers, workers []scheduleCompletionRequest
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request scheduleCompletionRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture input", http.StatusBadRequest)
					return
				}
				answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
				var system strings.Builder
				for _, message := range request.Messages {
					if message.Role == schema.System {
						system.WriteString(message.Content)
					}
				}
				if strings.Contains(system.String(), assignment) {
					mu.Lock()
					workers = append(workers, request)
					mu.Unlock()
					answer(result)
					return
				}
				if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
					return
				}
				mu.Lock()
				managers = append(managers, request)
				mu.Unlock()
				call := func(id, name string, args any) {
					raw, err := json.Marshal(args)
					if err != nil {
						t.Error(err)
						answer("The fixture arguments could not be encoded.")
						return
					}
					nuwaValidationTool(w, request, id, name, string(raw))
				}
				switch managerCalls.Add(1) {
				case 1:
					call("project-context-spawn", "spawn_agent", map[string]any{
						"role": "project-observer", "task": assignment, "fork_context": tc.fork,
					})
				case 2:
					var spawned toolScopeWorkerID
					if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "project-context-spawn")), &spawned); err != nil || spawned.AgentID == "" || spawned.Generation != 1 {
						t.Errorf("actual worker spawn failed: %+v err=%v", spawned, err)
						answer("The assigned worker could not start.")
						return
					}
					call("project-context-wait", "wait_agents", map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
				case 3:
					var collected toolScopeWait
					if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "project-context-wait")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 || collected.Agents[0].Status != "done" || collected.Agents[0].Result != result || collected.Agents[0].Generation != 1 {
						t.Errorf("actual worker generation was not collected: %+v err=%v", collected, err)
					}
					answer("The assigned result was collected.")
				default:
					t.Error("unexpected manager model request")
					answer("The coordination request was unexpected.")
				}
			}))
			t.Cleanup(endpoint.Close)
			e := sharedProjectInstructionsHTTPEngine(t, endpoint.URL, persona, tc.memory)
			project, err := e.CreateProject("Shared project instruction fixture", original, "", tc.memory, tc.nuwa)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit shared instruction case", "", project.ID)
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
			managerInput := append([]scheduleCompletionRequest(nil), managers...)
			workerInput := append([]scheduleCompletionRequest(nil), workers...)
			mu.Unlock()
			if finished.Status != store.TurnDone || len(managerInput) != 3 || len(workerInput) != 1 {
				t.Fatalf("project instruction flow: status=%s manager=%d worker=%d", finished.Status, len(managerInput), len(workerInput))
			}
			if !strings.Contains(joinedContent(managerInput[0].Messages), persona) || original != "" && !strings.Contains(joinedContent(managerInput[0].Messages), original) {
				t.Fatal("fixture did not exercise the actual manager personality and project instruction")
			}
			assertSharedProjectInstructionsAtHTTP(t, workerInput[0], original, goal, acceptance, persona, tc.nuwa && !tc.empty)
			if got := strings.Contains(joinedContent(workerInput[0].Messages), human); got != tc.fork {
				t.Errorf("actual fork inherited human input=%v, want %v", got, tc.fork)
			}
		})
	}
}

// Save the instruction built for a real new worker, not a hand-written
// checkpoint. Later project edits must not rewrite that worker's role/context.
func TestNuwaProjectInstructionsSurviveWorkerCheckpointAtActualHTTP(t *testing.T) {
	for _, memory := range []bool{true, false} {
		name := "memory-off"
		if memory {
			name = "memory-on"
		}
		t.Run(name, func(t *testing.T) {
			persona, goal, acceptance := store.NewID("persona_"), store.NewID("goal_"), store.NewID("acceptance_")
			original := "Project goal: " + goal + "\nAcceptance requirements: " + acceptance
			replacement, task := store.NewID("replacement_"), store.NewID("assignment_")
			entered := make(chan struct{}, 1)
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request scheduleCompletionRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture input", http.StatusBadRequest)
					return
				}
				assertSharedProjectInstructionsAtHTTP(t, request, original, goal, acceptance, persona, true)
				if strings.Contains(joinedContent(request.Messages), replacement) {
					t.Error("restored worker replaced its saved project context with a later project edit")
				}
				if requests.Add(1) == 1 {
					entered <- struct{}{}
					<-r.Context().Done()
					return
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The preserved assignment was inspected."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := sharedProjectInstructionsHTTPEngine(t, endpoint.URL, persona, memory)
			project, err := e.CreateProject("Checkpointed project instruction fixture", original, "", memory, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit worker instruction recovery", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: thread.ProviderID, Model: thread.Model, NuwaEnabled: true}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Awaiting the next check.", ""); err != nil {
				t.Fatal(err)
			}
			pc, err := e.projectContextFor(thread)
			if err != nil {
				t.Fatal(err)
			}
			set, err := e.conversationToolsForExecution(t.Context(), thread, e.turnWebExecution(thread, turn), false)
			if err != nil {
				t.Fatal(err)
			}
			builder, err := e.pool.ModelBuilder(t.Context(), turn.ProviderID, turn.Model, turn.ReasoningEffort, e.callRecorder(thread.ID, turn.ID))
			if err != nil {
				t.Fatal(err)
			}
			reg := e.newTurnRegistry(builder, set, pc, turn)
			t.Cleanup(reg.Close)
			worker, err := reg.SpawnForked(t.Context(), "project-observer", task, builder, []adk.Message{
				schema.SystemMessage(persona + "\n" + nuwaPlannerInstruction), schema.UserMessage(store.NewID("human_")),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				reg.Close()
				select {
				case <-worker.Done():
				case <-time.After(5 * time.Second):
					t.Error("original worker did not exit before fixture cleanup")
				}
			})
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("new worker never reached the actual HTTP model")
			}
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			saved := reg.PrepareRestart(cancelled)
			reg.Close()
			select {
			case <-worker.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("checkpointed worker did not exit before recovery")
			}
			if len(saved) != 1 || saved[0].ID != worker.ID || saved[0].Finished || saved[0].ConfigTag != turn.ID {
				t.Fatalf("real worker checkpoint lost its execution identity: %+v", saved)
			}
			if strings.Count(saved[0].Instruction, original) != 1 {
				t.Error("new worker checkpoint did not preserve its original project instruction")
			}
			appendParkedCheckpoint(t, e, thread.ID, turn.ID, restartCheckpoint{Version: 1, Workers: saved})
			if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, Title: "Shared instruction recovery owner", Prompt: "Inspect progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager}); err != nil {
				t.Fatal(err)
			}
			if _, err := e.UpdateProject(project.ID, nil, &replacement, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			next := restartedTestEngine(t, e)
			if count, err := next.ResumeParkedWorkers(); err != nil || count != 1 {
				t.Fatalf("recover actual worker checkpoint=%d err=%v", count, err)
			}
			waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
			_, finished := next.workersFromThread(thread.ID)
			if requests.Load() != 2 || len(finished) != 1 || finished[0].ID != worker.ID || finished[0].Generation != saved[0].Generation || finished[0].Err != nil {
				t.Fatalf("saved worker did not finish under its original identity: requests=%d workers=%+v", requests.Load(), finished)
			}
		})
	}
}
