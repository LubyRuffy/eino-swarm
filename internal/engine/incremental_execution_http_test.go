package engine

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The opt-in policy must reach actual model input without changing the
// planner's role or overriding the project's instructions.
func TestIncrementalExecutionGuidanceAtActualManagerHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		enabled, planner, planMode bool
		wantGuidance               bool
	}{
		{name: "default disabled"},
		{name: "ordinary manager opts in", enabled: true, wantGuidance: true},
		{name: "planning mode keeps its role", enabled: true, planMode: true},
		{name: "Nuwa planner keeps delegation", enabled: true, planner: true},
		{name: "Nuwa plan keeps its role", enabled: true, planner: true, planMode: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The supplied request was inspected."}, "stop")
			})
			next := e.cfg.Snapshot()
			next.Memory.Enabled, next.Memory.AutoReview = false, false
			// Raw JSON lets the pre-implementation revision compile and fail
			// at the Engine/Registry/model boundary instead of a missing field.
			encoded, err := json.Marshal(map[string]bool{"incremental_execution": tc.enabled})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &next.Swarm); err != nil {
				t.Fatal(err)
			}
			if err := e.cfg.Replace(next); err != nil {
				t.Fatal(err)
			}
			const projectRule = "Preserve the project's explicit scope and authorization requirements."
			project, err := e.CreateProject("Execution guidance fixture", projectRule, "", false, tc.planner)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit execution guidance fixture", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.planMode {
				if err := e.SetPlanMode(thread.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			requestText := "Inspect the supplied request and describe the next action."
			if tc.planMode {
				requestText = "/plan " + requestText
			}
			turn, err := e.StartTurn(thread.ID, requestText)
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			requests := wire.snapshot()
			if finished.Status != store.TurnDone || len(requests) != 1 {
				t.Fatalf("actual manager fixture failed: status=%s requests=%d", finished.Status, len(requests))
			}
			var system strings.Builder
			for _, message := range requests[0].Messages {
				if message.Role == schema.System {
					system.WriteString(message.Content)
					system.WriteByte('\n')
				}
			}
			prompt := system.String()
			for _, term := range []string{
				"## Incremental execution",
				"requested deliverables, paths, quantities, interfaces, and failure cases",
				"small runnable increment",
				"focused tool runs",
				"each explicit requirement",
			} {
				if present := strings.Contains(prompt, term); present != tc.wantGuidance {
					t.Errorf("actual manager guidance %q present=%v, want %v", term, present, tc.wantGuidance)
				}
			}
			if !strings.Contains(prompt, projectRule) {
				t.Fatal("actual manager input lost the project instructions")
			}
			if tc.wantGuidance && strings.Index(prompt, projectRule) < strings.Index(prompt, "## Incremental execution") {
				t.Fatal("generic execution guidance displaced project instruction precedence")
			}
			if !tc.planner && !strings.Contains(prompt, "Assess delegation before substantial work") {
				t.Fatal("ordinary manager input lost proactive delegation")
			}
			if tc.planner && strings.Contains(prompt, "You are the manager of a team of AI agents") {
				t.Fatal("the ordinary manager role leaked into the Nuwa planner")
			}
		})
	}
}

// The policy derives details from each request. Concrete paths, quantities,
// endpoints and worked examples here would bias unrelated tasks.
func TestIncrementalExecutionGuidanceRemainsGeneric(t *testing.T) {
	for _, marker := range []string{"for example", "e.g.", "http://", "https://", "```"} {
		if strings.Contains(strings.ToLower(incrementalExecutionInstruction), marker) {
			t.Errorf("generic execution policy contains a concrete example marker %q", marker)
		}
	}
	if regexp.MustCompile(`(^|\s)(/[[:alnum:]_.-]+)+|\b[0-9]+\b`).MatchString(incrementalExecutionInstruction) {
		t.Fatal("generic execution policy contains a fixed path or quantity")
	}
}
