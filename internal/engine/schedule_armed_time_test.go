package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func assertArmedTime(t *testing.T, e *Engine, out string) {
	t.Helper()
	var receipt struct {
		OK   bool   `json:"ok"`
		ID   string `json:"id"`
		Next string `json:"next_run_at"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || !receipt.OK {
		t.Fatalf("arm failed: %s %v", out, err)
	}
	row, err := e.store.GetSchedule(receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339, receipt.Next)
	if err != nil || receipt.Next != row.NextRunAt.Format(time.RFC3339Nano) || at.Sub(row.NextRunAt) <= -time.Second || at.After(row.NextRunAt) {
		t.Fatalf("receipt must quote the persisted instant: %s row=%s err=%v", out, row.NextRunAt, err)
	}
}

// All arming entry points must return the persisted instant, including replacements.
func TestArmedReceiptsGiveThePersistedNextRunTime(t *testing.T) {
	for _, cadence := range []string{`"delay_s":90`, `"every_s":90`, `"cron":"*/5 * * * *"`} {
		for _, tool := range []string{"wake", "task", "replace"} {
			t.Run(tool+cadence, func(t *testing.T) {
				e := newTestEngine(t)
				th, _ := e.CreateThread("receipt time", "", "")
				turn := mustStoredTurn(t, e, th.ID, store.Turn{})
				if tool == "replace" {
					_, _ = e.scheduleWakeJSON(th.ID, turn.ID, `{"title":"prior wait","prompt":"inspect","every_s":120}`)
				}
				call := e.scheduleWakeJSON
				if tool == "task" {
					call = e.scheduleTaskJSON
				}
				out, err := call(th.ID, turn.ID, `{"title":"time receipt","prompt":"inspect",`+cadence+`}`)
				if err != nil {
					t.Fatal(err)
				}
				assertArmedTime(t, e, out)
			})
		}
	}
	t.Run("report-rearm", func(t *testing.T) {
		e := newTestEngine(t)
		th, _ := e.CreateThread("report receipt", "", "")
		row := mustCreateWake(t, e, th.ID)
		assertArmedTime(t, e, reportNextInS(t, e, th.ID, mustRunningRun(t, e, row.ID, th.ID), 90))
	})
	t.Run("failure", func(t *testing.T) {
		e := newTestEngine(t)
		th, _ := e.CreateThread("failed receipt", "", "")
		turn := mustStoredTurn(t, e, th.ID, store.Turn{})
		for _, call := range []func(string, string, string) (string, error){e.scheduleWakeJSON, e.scheduleTaskJSON} {
			out, _ := call(th.ID, turn.ID, `{"delay_s":-1}`)
			var receipt map[string]any
			if json.Unmarshal([]byte(out), &receipt) != nil || receipt["ok"] != false || receipt["next_run_at"] != nil {
				t.Fatalf("failure includes armed time: %s", out)
			}
		}
	})
}
