package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// The manager must receive the same cadence facts that the answer verifier
// checks, without translating an English receipt or rounding a timestamp.
func TestManagerPromptExplainsReportRearmAndCopiesScheduleFacts(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{}, "")
	for _, need := range []string{
		"report_schedule arms the next wait only when next_in_s is supplied",
		"recurring every_s interval, not a one-shot delay",
		"Without next_in_s it only reports",
		"successful receipt from this turn or a matching row in Scheduled",
		"id, cadence_type and next_run_at exactly",
		"Do not convert or round",
		"armed_this_turn: true",
		"kind=thread",
		"independent job",
		"do not promise a later automatic check",
	} {
		if !strings.Contains(prompt, need) {
			t.Errorf("missing schedule fact guidance %q", need)
		}
	}
	if strings.Contains(prompt, "report_schedule does not arm the next wait") {
		t.Fatal("the prompt still contradicts report_schedule next_in_s")
	}
}

func TestAutomaticTurnCuesDoNotOfferIndependentTaskCreation(t *testing.T) {
	for name, prompt := range map[string]string{
		"goal":      GoalContinueText(),
		"scheduled": ScheduleContinueText("Inspect the current state."),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(prompt, "schedule_task is unavailable in this turn") {
				t.Fatal("automatic turns must not suggest the rejected independent task tool")
			}
		})
	}
	if strings.Contains(ManagerPrompt(&tools.Set{}, ""), "schedule_task only when") {
		t.Fatal("shared instructions must not offer independent creation on automatic turns")
	}
}

func TestPriorGoalContinuationCueRetainsItsRuntimeIdentity(t *testing.T) {
	for _, cue := range []string{legacyGoalContinueCue, GoalContinueText()} {
		if !legacyRuntimeCue(cue) {
			t.Fatalf("a runtime continuation lost its exact legacy identity: %q", cue)
		}
		if legacyRuntimeCue("Quoted instruction: " + cue) {
			t.Fatal("nearby human wording must not become a runtime instruction")
		}
	}
}

func TestScheduledListPreservesExactMatchingFields(t *testing.T) {
	next := time.Date(2026, time.June, 2, 4, 5, 6, 123456789, time.FixedZone("fixture", 3600))
	for _, row := range []store.Schedule{
		{ID: "sch_delay", Kind: store.ScheduleThread, DelayS: 37, NextRunAt: next},
		{ID: "sch_every", Kind: store.ScheduleThread, EveryS: 47, NextRunAt: next},
		{ID: "sch_cron", Kind: store.ScheduleThread, Cron: "7 * * * *", NextRunAt: next},
	} {
		t.Run(row.ID, func(t *testing.T) {
			section := scheduleSection([]store.Schedule{row})
			for _, fact := range []string{
				"These ids may be quoted in an answer or passed to cancel_schedule, never to schedule_wake",
				"id=" + row.ID,
				"kind=thread",
				"cadence_type=" + cadenceType(row),
				fmt.Sprintf("delay_s=%d", row.DelayS),
				fmt.Sprintf("every_s=%d", row.EveryS),
				fmt.Sprintf("cron=%q", row.Cron),
				"next_run_at=" + next.UTC().Format(time.RFC3339Nano),
			} {
				if !strings.Contains(section, fact) {
					t.Errorf("Scheduled list missing %q: %s", fact, section)
				}
			}
			if strings.Contains(section, "armed_this_turn=true") {
				t.Fatal("a pre-existing list row cannot establish this turn's arm")
			}
		})
	}
}
