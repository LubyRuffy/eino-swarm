package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A failed running conversation must contribute to startup diagnostics without
// preventing another conversation from recovering through either engine entry.
func TestRunningRecoveryReportsItsFailureAndContinuesOtherConversations(t *testing.T) {
	for _, entry := range []string{"restore", "legacy"} {
		t.Run(entry, func(t *testing.T) {
			e := newTestEngine(t)
			broken, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			failed := plantUnfinishedTurn(t, e, broken.ID, "continue existing work")
			if err := e.store.DB().Model(&store.Turn{}).Where("id = ?", failed.ID).Update("provider_id", "unconfigured-provider").Error; err != nil {
				t.Fatal(err)
			}
			healthy, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			continued := plantUnfinishedTurn(t, e, healthy.ID, "continue independent work")
			// Both entry points must encounter the broken owner first.
			if err := e.store.UpdateThread(broken.ID, map[string]any{"sort_rank": 1}); err != nil {
				t.Fatal(err)
			}
			if err := e.store.UpdateThread(healthy.ID, map[string]any{"sort_rank": 2}); err != nil {
				t.Fatal(err)
			}
			threads, err := e.store.ListThreads(true, "")
			if err != nil || len(threads) != 2 || threads[0].ID != broken.ID {
				t.Fatalf("recovery fixture order changed: %+v err=%v", threads, err)
			}
			var count int
			if entry == "restore" {
				err = e.Restore(context.Background(), nil)
			} else {
				count, err = e.ResumeOrphanedTurns()
			}
			var failures *RestoreFailures
			if !errors.As(err, &failures) || failures.Failed != 1 || !strings.Contains(failures.Error(), broken.ID) || !strings.Contains(failures.Error(), "unknown provider") {
				t.Errorf("running recovery failure was hidden: %v", err)
			}
			if entry == "legacy" && count != 1 {
				t.Errorf("legacy recovery stopped before its independent conversation: count=%d", count)
			}
			got, err := e.store.GetTurn(failed.ID)
			if err != nil || got.Status != store.TurnError || !strings.Contains(got.Error, "unknown provider") {
				t.Fatalf("failed turn lost its original result: %+v err=%v", got, err)
			}
			if got := waitForTurn(t, e, continued.ID); got.Status != store.TurnDone {
				t.Fatalf("independent recovery did not finish: %+v", got)
			}
			if entry == "restore" {
				events, err := e.store.ListTurnEvents(failed.ID)
				if err != nil {
					t.Fatal(err)
				}
				var phases []string
				for _, ev := range events {
					if ev.Kind != "engine_restore" {
						continue
					}
					var receipt struct {
						Phase    string `json:"phase"`
						Category string `json:"error_category"`
					}
					if err := json.Unmarshal([]byte(ev.Text), &receipt); err != nil {
						t.Fatal(err)
					}
					phases = append(phases, receipt.Phase)
					if receipt.Phase != "failed" || receipt.Category != "restore_error" {
						t.Errorf("failed recovery was reported as restored: %s", ev.Text)
					}
				}
				if len(phases) != 1 {
					t.Fatalf("want one failed recovery trace, got %v", phases)
				}
			}
		})
	}
}
