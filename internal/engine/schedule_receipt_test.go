package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The manager must be able to repeat the exact persisted facts without
// translating the human-readable cadence note back into scheduling arguments.
func TestScheduleToolReceiptsExposePersistedArmingFacts(t *testing.T) {
	for _, tc := range []struct {
		name, args, cadence string
	}{
		{"delay", `"delay_s":90`, "delay"},
		{"every", `"every_s":120`, "every"},
		{"cron", `"cron":"*/5 * * * *"`, "cron"},
	} {
		for _, toolName := range []string{ToolScheduleWake, ToolScheduleTask} {
			t.Run(toolName+"/"+tc.name, func(t *testing.T) {
				e := newTestEngine(t)
				th, _ := e.CreateThread("Schedule receipt", "", "")
				turn := mustStoredTurn(t, e, th.ID, store.Turn{})
				for _, tl := range e.runtimeFor(th.ID).managerScheduleTools(turn) {
					info, err := tl.Info(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if info.Name != toolName {
						continue
					}
					out, err := mustInvokable(t, tl).InvokableRun(context.Background(), `{"title":"Receipt check","prompt":"Inspect current work",`+tc.args+`}`)
					if err != nil {
						t.Fatal(err)
					}
					kind := "thread"
					if toolName == ToolScheduleTask {
						kind = "independent"
					}
					assertScheduleReceiptFacts(t, e, out, kind, tc.cadence)
					return
				}
				t.Fatalf("manager tool %s is missing", toolName)
			})
		}
	}
}

func TestReportReceiptDistinguishesReportAcceptanceFromArming(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		armed      bool
	}{
		{"report-only", `{"findings":""}`, false},
		{"repeating-rearm", `{"findings":"","next_in_s":90}`, true},
		{"cancel-with-next", `{"findings":"","keep":false,"next_in_s":90}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("Report receipt", "", "")
			sch := mustCreateWake(t, e, th.ID)
			run := mustRunningRun(t, e, sch.ID, th.ID)
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run})
			out, err := e.reportOwnedTestScheduleJSON(th.ID, turn.ID, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if tc.armed {
				assertScheduleReceiptFacts(t, e, out, "thread", "every")
				return
			}
			var receipt map[string]any
			if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt["ok"] != true || receipt["armed_this_turn"] != false {
				t.Fatalf("accepted report must explicitly deny arming: %s error=%v", out, err)
			}
			for _, field := range []string{"next_run_at", "cadence_type", "delay_s", "every_s", "cron"} {
				if _, exists := receipt[field]; exists {
					t.Fatalf("report-only receipt contains arming field %s: %s", field, out)
				}
			}
		})
	}
}

func assertScheduleReceiptFacts(t *testing.T, e *Engine, out, kind, cadence string) {
	t.Helper()
	var receipt struct {
		OK            bool   `json:"ok"`
		ID            string `json:"id"`
		Kind          string `json:"kind"`
		CadenceType   string `json:"cadence_type"`
		DelayS        int    `json:"delay_s"`
		EveryS        int    `json:"every_s"`
		Cron          string `json:"cron"`
		NextRunAt     string `json:"next_run_at"`
		ArmedThisTurn bool   `json:"armed_this_turn"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || !receipt.OK || !receipt.ArmedThisTurn || receipt.Kind != kind || receipt.CadenceType != cadence {
		t.Fatalf("receipt has no exact arming facts: %s error=%v", out, err)
	}
	row, err := e.store.GetSchedule(receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.DelayS != row.DelayS || receipt.EveryS != row.EveryS || receipt.Cron != row.Cron || receipt.NextRunAt != row.NextRunAt.Format(time.RFC3339Nano) {
		t.Fatalf("receipt differs from durable schedule: receipt=%s row=%+v", out, row)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"delay_s", "every_s", "cron"} {
		if _, exists := fields[field]; !exists {
			t.Fatalf("unused cadence field %s must still be explicit: %s", field, out)
		}
	}
}
