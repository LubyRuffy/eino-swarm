package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Updated notes and skills must not leave a stale route in the project prompt
// that every later turn receives. Exercise the real manager tool and check the
// next turn and a fresh conversation with no migration message in its history.
func TestManagerUpdatesProjectDescriptionForFreshConversations(t *testing.T) {
	const policy = "Only perform read-only diagnostics during scheduled checks."
	const oldRoute = "Reach the four DGX Spark machines by jumping through e1001."
	const newRoute = "Direct LAN addresses for the four DGX Spark machines: "
	const addresses = "192.0.2.21, 192.0.2.22, 192.0.2.23, 192.0.2.24"
	const before = policy + "\n" + oldRoute
	const after = policy + "\n" + newRoute + addresses
	const siblingText = "Use a separate gateway for the staging machines."
	const toolName = "update_project_description"

	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		reply := func(answer string) {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
		if len(request.Messages) == 0 || request.Messages[0].Role != "system" {
			t.Error("actual manager request has no system prompt")
			reply("No project prompt.")
			return
		}
		system := request.Messages[0].Content
		switch step {
		case 1:
			if !strings.Contains(system, before) || !strings.Contains(system, "Direct access is now available.") || !strings.Contains(system, "inspect-spark-link") {
				t.Error("fixture did not reproduce stale project context alongside updated memory and skills")
			}
			for _, tool := range request.Tools {
				if tool.Function.Name == toolName {
					args, err := json.Marshal(map[string]string{"description": after})
					if err != nil {
						t.Error(err)
						reply("Could not encode description.")
						return
					}
					writeScheduleCompletionTool(w, request, toolName, string(args))
					return
				}
			}
			t.Errorf("manager HTTP request has no %s tool", toolName)
			reply("I could only read the project description.")
		case 2:
			for _, message := range request.Messages {
				if message.Role != "tool" {
					continue
				}
				var receipt struct {
					OK bool `json:"ok"`
				}
				if err := json.Unmarshal([]byte(message.Content), &receipt); err == nil && receipt.OK {
					reply("The project description is updated.")
					return
				}
			}
			t.Error("the real ADK tool did not return a successful project description update receipt")
			reply("The project update failed.")
		case 3, 4:
			if strings.Contains(system, oldRoute) || !strings.Contains(system, policy) {
				t.Errorf("manager prompt for request %d retained the gateway or lost unrelated instructions", step)
			}
			for _, line := range strings.Split(system, "\n") {
				if value, ok := strings.CutPrefix(line, newRoute); ok {
					reply(value)
					return
				}
			}
			t.Errorf("manager prompt for request %d did not contain the direct route", step)
			reply("No direct addresses are documented.")
		default:
			t.Errorf("unexpected manager request %d", step)
			reply("Unexpected request.")
		}
	})
	e.cfg.Memory.AutoReview = false
	project, err := e.CreateProject("Spark operations", before, "", true)
	if err != nil {
		t.Fatal(err)
	}
	mem := e.ProjectMemory(project.ID)
	if _, _, err := mem.Add("Direct access is now available."); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.WriteSkill("inspect-spark-link", "When diagnosing local Spark connectivity", "Check the direct interface before inspecting the switch port."); err != nil {
		t.Fatal(err)
	}
	sibling, err := e.CreateProject("Staging operations", siblingText, "", false)
	if err != nil {
		t.Fatal(err)
	}
	newThread := func(title string) string {
		t.Helper()
		thread, err := e.CreateThread(title, "", project.ID)
		if err != nil {
			t.Fatal(err)
		}
		return thread.ID
	}
	run := func(threadID, message string) *store.Turn {
		t.Helper()
		turn, err := e.StartTurn(threadID, message)
		if err != nil {
			t.Fatal(err)
		}
		result := waitForTurn(t, e, turn.ID)
		waitSettled(t, e, threadID)
		if result.Status != store.TurnDone {
			t.Fatalf("turn did not complete: %+v", result)
		}
		return result
	}
	migrationThread := newThread("Switch migration")
	run(migrationThread, "We removed e1001 after installing a new switch. Update the project description to document these four direct addresses: "+addresses+". Keep the read-only diagnostics policy.")
	updated, err := e.GetProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SystemPrompt != after || updated.Name != project.Name || updated.Workdir != project.Workdir || updated.MemoryEnabled != project.MemoryEnabled {
		t.Fatalf("saved project does not match the requested description-only update: %+v", updated)
	}
	unchanged, err := e.GetProject(sibling.ID)
	if err != nil || unchanged.SystemPrompt != siblingText {
		t.Fatalf("an unrelated project changed: %+v (%v)", unchanged, err)
	}
	const question = "List the documented direct addresses for the four machines."
	if answer := run(migrationThread, question).Final; answer != addresses {
		t.Fatalf("subsequent turn did not use the saved direct addresses: %q", answer)
	}
	if answer := run(newThread("Routine access"), question).Final; answer != addresses {
		t.Fatalf("fresh conversation did not use the saved direct addresses: %q", answer)
	}
	if requests := fixture.snapshot(); len(requests) != 4 {
		t.Errorf("manager HTTP requests=%d, want update, receipt, subsequent turn, and fresh conversation", len(requests))
	}
}

// The description editor belongs to an ordinary project turn, even with
// Memory disabled. A planning or standalone turn must not advertise a write.
func TestManagerAdvertisesProjectDescriptionEditorOnlyInWritableProjectTurns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		project bool
		plan    bool
		writer  bool
	}{
		{name: "project without memory", project: true, writer: true},
		{name: "project plan", project: true, plan: true},
		{name: "standalone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
				hasWriter := false
				for _, tool := range request.Tools {
					hasWriter = hasWriter || tool.Function.Name == "update_project_description"
				}
				if hasWriter != tc.writer {
					t.Errorf("manager HTTP request offered project description editor=%t, want %t", hasWriter, tc.writer)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The conversation is ready."}, "stop")
			})
			projectID := ""
			if tc.project {
				project, err := e.CreateProject("Fixture", "Use the documented settings.", "", false)
				if err != nil {
					t.Fatal(err)
				}
				projectID = project.ID
			}
			thread, err := e.CreateThread("Tool availability", "", projectID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.plan {
				if err := e.SetPlanMode(thread.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			turn, err := e.StartTurn(thread.ID, "Tell me when you are ready.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || len(fixture.snapshot()) != 1 {
				t.Fatalf("expected one completed manager request: turn=%+v requests=%d", got, len(fixture.snapshot()))
			}
		})
	}
}
