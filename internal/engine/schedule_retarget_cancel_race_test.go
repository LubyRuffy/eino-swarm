package engine

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// TLC's SelectEdit -> Cancel -> CommitEdit trace reaches the real inbox path:
// a validation read cannot authorize moving a wait after it becomes terminal.
func TestRetargetRefusesAWaitThatEndedAfterSelection(t *testing.T) {
	for _, ending := range []string{"human_cancel", "manager_cancel", "claimed_one_shot"} {
		t.Run(ending, func(t *testing.T) {
			e := newTestEngine(t)
			origin, err := e.CreateThread("original wake owner", "", "")
			if err != nil {
				t.Fatal(err)
			}
			target, err := e.CreateThread("requested new wake owner", "", "")
			if err != nil {
				t.Fatal(err)
			}
			row, err := e.CreateSchedule(ScheduleInput{
				Kind: store.ScheduleThread, ThreadID: origin.ID, Title: "Retarget lifecycle",
				Prompt: "Inspect outstanding work.", DelayS: 60,
			})
			if err != nil {
				t.Fatal(err)
			}
			legacy := &store.ScheduleRun{ScheduleID: row.ID, ThreadID: origin.ID, Status: store.ScheduleRunFindings}
			if err := e.Store().CreateRun(legacy); err != nil {
				t.Fatal(err)
			}
			var ended atomic.Bool
			var afterEnding *store.Schedule
			if err := e.Store().DB().Callback().Query().After("gorm:after_query").Register("end_wait_after_retarget_selection", func(tx *gorm.DB) {
				selected, ok := tx.Statement.Dest.(*store.Schedule)
				if !ok || selected.ID != row.ID || !ended.CompareAndSwap(false, true) {
					return
				}
				var err error
				switch ending {
				case "human_cancel":
					err = e.CancelSchedule(row.ID)
				case "manager_cancel":
					err = e.cancelSchedule(row.ID)
				case "claimed_one_shot":
					if e.claimDueSchedule(*selected, time.Now().UTC()) == nil {
						err = fmt.Errorf("one-shot claim was not admitted")
					}
				}
				if err == nil {
					afterEnding, err = e.GetSchedule(row.ID)
				}
				if err != nil {
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			// The private callback remains installed until Store.Close. All waits
			// have explicit titles, so no asynchronous namer can race the seam.
			kind, title := store.ScheduleThread, "stale editor title"
			if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, ThreadID: &target.ID, Title: &title}); err == nil {
				t.Error("stale inbox edit moved a wait after its terminal transition")
			}
			got, err := e.GetSchedule(row.ID)
			if err != nil || !ended.Load() || afterEnding == nil || !reflect.DeepEqual(got, afterEnding) {
				t.Fatalf("rejected edit changed terminal state: got=%+v want=%+v err=%v", got, afterEnding, err)
			}
			wantStatus := store.ScheduleCancelled
			if ending == "claimed_one_shot" {
				wantStatus = store.ScheduleDone
			}
			if got.Status != wantStatus || got.ThreadID != origin.ID {
				t.Fatalf("terminal transition lost its original owner: %+v", got)
			}
			run, err := e.Store().GetRun(legacy.ID)
			if err != nil || run.Kind != "" || !run.UpdatedAt.Equal(legacy.UpdatedAt) {
				t.Fatalf("rejected edit partially froze legacy fire identity: %+v %v", run, err)
			}
			for _, th := range []*store.Thread{origin, target} {
				policy, _, err := e.Store().ReadThreadWakePolicy(th.ID)
				if err != nil || policy.Denied != (ending == "human_cancel" && th.ID == origin.ID) {
					t.Fatalf("retarget changed human authority on %s: %+v %v", th.ID, policy, err)
				}
			}
		})
	}
}
