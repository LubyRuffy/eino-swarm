package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Continue resumes work; it must not undo the user's Cancel wait action just
// because the manager chooses a fresh schedule ID rather than the cancelled one.
func TestHumanCancelledWaitCannotReturnOnContinue(t *testing.T) {
	e := newWakePolicyResponseEngine(t, func(store.WakePolicyInput) (int, string) {
		return http.StatusOK, `{"decision":"unchanged","quote":""}`
	})
	th, _ := e.CreateThread("cancelled wait", "", "")
	row := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Continue the outstanding work."})
	runWakePolicyManager(t, e, turn, ToolScheduleWake, `{"every_s":120,"prompt":"Inspect current progress."}`, "thread_wake_prohibited", row.ID)
	rows, err := e.Store().ListSchedules()
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID || rows[0].Status != store.ScheduleCancelled {
		t.Fatalf("Continue recreated a cancelled wait: %+v %v", rows, err)
	}
	if prompt := e.threadWakePolicyPrompt(th.ID); !strings.Contains(prompt, row.ID) || !strings.Contains(prompt, "cancel") {
		t.Fatalf("manager did not receive the human cancellation: %s", prompt)
	}
}

func TestLaterExplicitPermissionCanArmAfterHumanCancellation(t *testing.T) {
	const permission = "You may wait here and schedule the next check."
	e := newWakePolicyResponseEngine(t, func(input store.WakePolicyInput) (int, string) {
		decision, quote := store.WakeDecisionUnchanged, ""
		if input.Text == permission {
			decision, quote = store.WakeDecisionAllow, permission
		}
		answer, _ := json.Marshal(map[string]string{"decision": decision, "quote": quote})
		return http.StatusOK, string(answer)
	})
	th, _ := e.CreateThread("new permission", "", "")
	row := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: permission})
	out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"every_s":120,"prompt":"Inspect current progress."}`)
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("explicit new permission was refused: %s %v", out, err)
	}
	if policy, _, err := e.Store().ReadThreadWakePolicy(th.ID); err != nil || policy.Denied {
		t.Fatalf("later permission did not release the cancellation: %+v %v", policy, err)
	}
}

func TestManagerCancellationDoesNotRevokeHumanWaitingPermission(t *testing.T) {
	e := newWakePolicyFixtureEngine(t, store.WakeDecisionUnchanged, "")
	th, _ := e.CreateThread("manager cancellation", "", "")
	row := mustCreateWake(t, e, th.ID)
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	set := buildTestToolset(t, e, th.ID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func([]*schema.Message) *schema.Message {
			return coordinationCall("cancel-current-wait", ToolCancelSchedule, `{"id":"`+row.ID+`"}`)
		},
		func(messages []*schema.Message) *schema.Message {
			if !strings.Contains(messages[len(messages)-1].Content, `"ok":true`) {
				t.Errorf("manager cancellation failed: %s", messages[len(messages)-1].Content)
			}
			return coordinationCall("new-manager-wait", ToolScheduleWake, `{"every_s":120,"prompt":"Inspect current progress."}`)
		},
		func(messages []*schema.Message) *schema.Message {
			if !strings.Contains(messages[len(messages)-1].Content, `"ok":true`) {
				t.Errorf("manager cancellation became a human prohibition: %s", messages[len(messages)-1].Content)
			}
			return schema.AssistantMessage("The scheduling tools returned their results.", nil)
		},
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	defer reg.Close()
	acc := newAccumulator(e, th.ID, turn.ID, 0)
	if _, err := e.runtimeFor(th.ID).runOwnedTestManager(context.Background(), turn, reg, acc, set, nil, []adk.Message{schema.UserMessage("Inspect the current work state.")}); err != nil {
		t.Fatal(err)
	}
	acc.flushAll()
	if policy, _, err := e.Store().ReadThreadWakePolicy(th.ID); err != nil || policy.Denied {
		t.Fatalf("model cancellation changed human policy: %+v %v", policy, err)
	}
}

func TestHumanCancellationUsesTheWakeTargetAfterRetargeting(t *testing.T) {
	e := newTestEngine(t)
	origin, _ := e.CreateThread("origin", "", "")
	target, _ := e.CreateThread("wake target", "", "")
	row, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: target.ID, OriginThreadID: origin.ID,
		Title: "Scheduled work", Prompt: "Inspect current progress.", EveryS: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	for _, th := range []*store.Thread{origin, target} {
		policy, _, err := e.Store().ReadThreadWakePolicy(th.ID)
		if err != nil || policy.Denied != (th.ID == target.ID) {
			t.Fatalf("cancellation applied to the wrong conversation: %s %+v %v", th.ID, policy, err)
		}
		if n := countThreadKind(t, e, th.ID, KindScheduleCancelled); n != 1 {
			t.Fatalf("cancellation must be idempotent on %s: events=%d", th.ID, n)
		}
	}
}

func TestFailedHumanCancellationDoesNotPublishOrCancelTheWait(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("cancel persistence", "", "")
	row := mustCreateWake(t, e, th.ID)
	cause := errors.New("cancellation receipt unavailable")
	var failed atomic.Bool
	if err := e.Store().DB().Callback().Create().Before("gorm:create").Register("cancel_receipt_failure", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == KindScheduleCancelled && failed.CompareAndSwap(false, true) {
			tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Keep the callback installed until this private Store closes. No actor can
	// race its removal while finalizing an earlier event write.
	if err := e.CancelSchedule(row.ID); !errors.Is(err, cause) {
		t.Fatalf("cancellation persistence failure was hidden: %v", err)
	}
	got, err := e.GetSchedule(row.ID)
	if err != nil || got.Status != store.ScheduleActive || hasKind(t, e, th.ID, KindScheduleCancelled) {
		t.Fatalf("failed cancellation changed visible state: %+v %v", got, err)
	}
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatalf("failed cancellation kept the event ordering lock: %v", err)
	}
}

func TestHumanStandaloneCancellationDoesNotProhibitThreadWakes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("independent work", "", "")
	row, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleStandalone, OriginThreadID: th.ID, Title: "Independent check", Prompt: "Inspect independent work.", EveryS: 60})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	if policy, _, err := e.Store().ReadThreadWakePolicy(th.ID); err != nil || policy.Denied {
		t.Fatalf("independent cancellation prohibited the conversation: %+v %v", policy, err)
	}
	if err := e.CancelSchedule("missing-wait"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing cancellation error lost: %v", err)
	}
}

func TestHumanCancelRefusesAWaitRetargetedAfterSelection(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("new wake target", "", "")
	row, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleStandalone, Title: "Independent check", Prompt: "Inspect independent work.", EveryS: 60})
	if err != nil {
		t.Fatal(err)
	}
	var moved atomic.Bool
	kind := store.ScheduleThread
	if err := e.Store().DB().Callback().Query().After("gorm:after_query").Register("retarget_after_cancel_selection", func(tx *gorm.DB) {
		selected, ok := tx.Statement.Dest.(*store.Schedule)
		if !ok || selected.ID != row.ID || !moved.CompareAndSwap(false, true) {
			return
		}
		// Interleave the actual inbox edit after CancelSchedule reads its old
		// destination, before it can choose the independent cancellation path.
		_, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, ThreadID: &th.ID})
		if err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(row.ID); err == nil {
		t.Error("human cancellation accepted a changed destination without its authority receipt")
	}
	got, err := e.GetSchedule(row.ID)
	if err != nil || !moved.Load() || got.Kind != store.ScheduleThread || got.ThreadID != th.ID || got.Status != store.ScheduleActive {
		t.Fatalf("retargeted wait was partly cancelled: %+v %v", got, err)
	}
	if hasKind(t, e, th.ID, KindScheduleCancelled) {
		t.Fatal("refused cancellation published a cancelled notice")
	}
}

func TestCancellingTerminalHistoryDoesNotStartTheStandingGoal(t *testing.T) {
	for _, status := range []string{store.ScheduleDone, store.ScheduleCancelled} {
		t.Run(status, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("historical scheduled work", "", "")
			row := mustCreateWake(t, e, th.ID)
			if err := e.Store().UpdateSchedule(row.ID, map[string]any{"status": status}); err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "Inspect the outstanding work."); err != nil {
				t.Fatal(err)
			}
			// This action withdraws future waiting permission. There is no armed
			// wait to release, so deleting history must not start new work.
			if err := e.CancelSchedule(row.ID); err != nil {
				t.Fatal(err)
			}
			turns, err := e.Store().ListTurns(th.ID)
			if err != nil || len(turns) != 0 || e.Status(th.ID).Running {
				t.Fatalf("historical deletion started the standing objective: %+v %v", turns, err)
			}
			policy, _, err := e.Store().ReadThreadWakePolicy(th.ID)
			if err != nil || !policy.Denied || policy.SourceText != row.ID {
				t.Fatalf("historical deletion lost waiting authority: %+v %v", policy, err)
			}
		})
	}
}
