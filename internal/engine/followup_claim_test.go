package engine

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Editing may win immediately before a manual send removes the row. The
// sent or restored request must contain that edit, never the earlier text.
func TestSteerFollowupUsesTheRequestActuallyRemovedFromTheQueue(t *testing.T) {
	for _, outcome := range []string{"start", "steer", "restore"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1200")
			e := newTestEngine(t)
			thread, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			var live *store.Turn
			if outcome == "steer" {
				live, err = e.StartTurn(thread.ID, "the current task")
				if err != nil {
					t.Fatal(err)
				}
			}
			selected, err := e.Store().EnqueueFollowup(thread.ID, "the earlier request")
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "restore" {
				if err := e.Store().UpdateThread(thread.ID, map[string]any{"provider_id": "removed-provider"}); err != nil {
					t.Fatal(err)
				}
			}
			var updated atomic.Bool
			var edited *store.Followup
			if err := e.Store().DB().Callback().Delete().Before("gorm:delete").Register("test:edit_before_followup_claim", func(tx *gorm.DB) {
				if tx.Statement.Table != "followups" || !updated.CompareAndSwap(false, true) {
					return
				}
				var err error
				edited, err = e.RequeueFollowup(thread.ID, selected.ID, "the revised request")
				if err != nil {
					_ = tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}

			started, err := e.SteerFollowup(thread.ID, selected.ID)
			if !updated.Load() || edited == nil {
				t.Fatal("the edit did not reach the claim boundary")
			}
			switch outcome {
			case "start":
				if err != nil || started == nil || started.UserText != edited.Text {
					t.Fatalf("manual send discarded the edit: %+v, %v", started, err)
				}
				waitForTurn(t, e, started.ID)
			case "steer":
				if err != nil || started != nil {
					t.Fatalf("manual send did not steer the live turn: %+v, %v", started, err)
				}
				events, err := e.Replay(thread.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				var steers []string
				for _, event := range events {
					if event.Kind == KindSteer && event.TurnID == live.ID {
						steers = append(steers, event.Text)
					}
				}
				if len(steers) != 1 || steers[0] != edited.Text {
					t.Fatalf("live injection discarded or duplicated the edit: %v", steers)
				}
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				waitForTurn(t, e, live.ID)
			case "restore":
				if started != nil || err == nil || !strings.Contains(err.Error(), "removed-provider") {
					t.Fatalf("unavailable provider unexpectedly accepted the request: %+v, %v", started, err)
				}
				left, err := e.ListFollowups(thread.ID)
				if err != nil || len(left) != 1 || left[0].ID != edited.ID || left[0].Text != edited.Text {
					t.Fatalf("failed manual send restored an obsolete edit: %+v, %v", left, err)
				}
			}
		})
	}
}
