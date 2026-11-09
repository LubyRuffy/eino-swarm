package engine

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/schema"
)

// Plan instructions reach the same root System as the planner's role. A
// conflicting instruction here can make the model call an unmounted tool.
// Recovery must use the turn's frozen mode even if the project toggle changed.
func TestManagerPlanInstructionsRespectFrozenNuwaModeAtModelHTTP(t *testing.T) {
	for _, nuwa := range []bool{false, true} {
		for _, planning := range []bool{false, true} {
			for _, recovery := range []bool{false, true} {
				name := fmt.Sprintf("nuwa=%v/planning=%v/recovery=%v", nuwa, planning, recovery)
				t.Run(name, func(t *testing.T) {
					e, recorded := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
						writeContextFlowReply(w, request.Stream, map[string]any{"content": "The current plan and coordination surface were inspected."}, "stop")
					})
					e.cfg.Memory.AutoReview = false
					project, err := e.CreateProject("Plan coordination boundary", "", "", false, nuwa)
					if err != nil {
						t.Fatal(err)
					}
					thread, err := e.CreateThread("Explicit plan coordination", "", project.ID)
					if err != nil {
						t.Fatal(err)
					}
					if err := e.SetPlanMode(thread.ID, true); err != nil {
						t.Fatal(err)
					}
					const plan = "Inspect the available evidence, perform the agreed work, and verify its outcome."
					if err := e.SavePlanMarkdown(thread.ID, plan); err != nil {
						t.Fatal(err)
					}

					var turn *store.Turn
					if recovery {
						if !planning {
							if err := e.SetPlanMode(thread.ID, false); err != nil {
								t.Fatal(err)
							}
						}
						turn = &store.Turn{
							ThreadID: thread.ID, ProjectID: project.ID,
							ProviderID: e.cfg.Models.Providers[0].ID, Model: e.cfg.Models.Providers[0].Model,
							UserText: "Continue the current plan.", NuwaEnabled: nuwa, NuwaReadOnly: nuwa && planning,
						}
						if err := e.store.CreateTurn(turn); err != nil {
							t.Fatal(err)
						}
						if err := e.store.AppendMessages(thread.ID, turn.ID, []store.Message{{
							Role: string(schema.User), Content: turn.UserText, Source: store.MessageSourceHuman,
						}}); err != nil {
							t.Fatal(err)
						}
						changed := !nuwa
						if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &changed); err != nil {
							t.Fatal(err)
						}
						if count, err := e.ResumeOrphanedTurns(); err != nil || count != 1 {
							t.Fatalf("recover original turn: count=%d err=%v", count, err)
						}
					} else if planning {
						turn, err = e.StartTurn(thread.ID, "/plan Inspect the current objective and propose the next steps.")
					} else {
						turn, err = e.ImplementPlan(thread.ID)
					}
					if err != nil || turn == nil {
						t.Fatalf("start plan turn: turn=%+v err=%v", turn, err)
					}
					finished := waitForTurn(t, e, turn.ID)
					waitSettled(t, e, thread.ID)
					if finished.Status != store.TurnDone || finished.NuwaEnabled != nuwa {
						t.Fatalf("plan turn lost its frozen mode or failed: %+v", finished)
					}
					requests := recorded.snapshot()
					if len(requests) != 1 {
						t.Fatalf("expected one actual root HTTP request, got %d", len(requests))
					}
					assertManagerPlanInstructions(t, requests[0], plan, nuwa, planning)
				})
			}
		}
	}
}

func assertManagerPlanInstructions(t *testing.T, request scheduleCompletionRequest, plan string, nuwa, planning bool) {
	t.Helper()
	var system strings.Builder
	for _, message := range request.Messages {
		if message.Role == schema.System {
			system.WriteString(message.Content)
			system.WriteByte('\n')
		}
	}
	prompt := strings.Join(strings.Fields(system.String()), " ")
	if !strings.Contains(prompt, plan) {
		t.Fatal("actual root System lost the current plan")
	}
	available := nuwaPlannerOfferedTools(request)
	for _, name := range []string{"spawn_agent", "wait_agents", "ask_user"} {
		if !available[name] {
			t.Errorf("actual plan root lost coordination tool %s", name)
		}
	}
	if available["propose_plan"] != planning {
		t.Errorf("plan proposal surface: available=%v planning=%v", available["propose_plan"], planning)
	}
	if nuwa {
		for _, obsolete := range []string{"Explore with read and search", "Execute it. Implementation tools are mounted."} {
			if strings.Contains(prompt, obsolete) {
				t.Errorf("Nuwa root still instructs direct task execution: %q", obsolete)
			}
		}
		want := "Delegate its execution to executors"
		if planning {
			want = "Have executors explore with read and search"
		}
		if !strings.Contains(prompt, want) {
			t.Errorf("Nuwa plan System is missing executor delegation: %q", want)
		}
		for _, descriptor := range tools.Catalog() {
			if available[descriptor.Name] {
				t.Errorf("Nuwa plan root received execution tool %q", descriptor.Name)
			}
		}
		return
	}
	want := "Execute it. Implementation tools are mounted."
	if planning {
		want = "Explore with read and search"
	}
	if !strings.Contains(prompt, want) {
		t.Errorf("ordinary plan System changed its established workflow: missing %q", want)
	}
	if !available["read"] || available["write"] == planning || available["exec"] == planning {
		t.Errorf("ordinary plan root lost its established execution scope: planning=%v tools=%v", planning, available)
	}
}

func TestManagerPromptOmitsEmptyPlan(t *testing.T) {
	for _, plannerOnly := range []bool{false, true} {
		for _, markdown := range []string{"", " \t\n"} {
			t.Run(fmt.Sprintf("planner=%v/markdown=%q", plannerOnly, markdown), func(t *testing.T) {
				extra := managerExtra(nil, &store.Thread{PlanMarkdown: markdown}, nil, "", plannerOnly)
				prompt := managerPrompt(&tools.Set{}, extra, plannerOnly, false)
				if strings.Contains(prompt, "## Plan") {
					t.Fatalf("empty plan created a planning or execution instruction: %s", extra)
				}
			})
		}
	}
}
