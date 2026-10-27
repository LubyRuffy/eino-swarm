package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestFinalGoalWorkerPromptRequiresTheLastBudgetAndNoActiveWake(t *testing.T) {
	for _, boundary := range []string{"missing", "empty", "remaining", "final", "over", "paused", "plan", "wake", "paused-wake"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("prompt boundary", "", "")
			rt := e.runtimeFor(th.ID)
			if boundary == "missing" {
				if rt.finalGoalWorkerSection(nil) != "" {
					t.Fatal("missing goal created a final-turn instruction")
				}
				return
			}
			th.Goal = "Verify the assigned state."
			th.GoalAutoTurns = e.cfg.Swarm.GoalAutoTurns()
			switch boundary {
			case "empty":
				th.Goal = ""
			case "remaining":
				th.GoalAutoTurns--
			case "over":
				th.GoalAutoTurns++
			case "paused":
				th.GoalCapped = true
			case "plan":
				th.PlanMode = true
			}
			if boundary == "wake" || boundary == "paused-wake" {
				wake := mustCreateWake(t, e, th.ID)
				if boundary == "paused-wake" {
					if err := e.store.UpdateSchedule(wake.ID, map[string]any{"status": store.SchedulePaused}); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := boundary == "final" || boundary == "over" || boundary == "paused-wake"
			got := rt.finalGoalWorkerSection(th)
			if strings.Contains(got, "last automatic continuation") != want {
				t.Fatalf("last-turn prompt=%q want=%v", got, want)
			}
		})
	}
}
