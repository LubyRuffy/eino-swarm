package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

const scheduleToolWaitPrompt = "Continue the wait."

func TestScheduleToolNamesAreStable(t *testing.T) {
	// These names sit in transcripts and Trace. Renaming one is a protocol change.
	if ToolScheduleWake != "schedule_wake" || ToolScheduleTask != "schedule_task" ||
		ToolCancelSchedule != "cancel_schedule" || ToolReportSchedule != "report_schedule" {
		t.Fatalf("names drifted: %q %q %q %q",
			ToolScheduleWake, ToolScheduleTask, ToolCancelSchedule, ToolReportSchedule)
	}
}

func TestScheduleToolInfoStaysGeneric(t *testing.T) {
	for _, tl := range []tool.BaseTool{
		ScheduleWakeTool(nil),
		ScheduleTaskTool(nil),
		CancelScheduleTool(nil),
		ReportScheduleTool(nil),
	} {
		info, err := tl.Info(context.Background())
		if err != nil || info == nil {
			t.Fatalf("info: %+v %v", info, err)
		}
		blob := strings.ToLower(info.Name + " " + info.Desc)
		for _, leak := range []string{"ci", "deploy", "cron example", "github"} {
			if strings.Contains(blob, leak) {
				t.Fatalf("%q leaked into %s: %s", leak, info.Name, info.Desc)
			}
		}
	}
}

func TestScheduleWakeUpsertsOnThisConversation(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	tl := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}))

	first, err := tl.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60,"title":"wake"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, first).ID
	if !strings.HasPrefix(id, "sch_") {
		t.Fatalf("id=%q", id)
	}
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != store.ScheduleThread || row.ThreadID != th.ID || row.CreatedBy != store.ScheduleCreatedManager {
		t.Fatalf("row=%+v", row)
	}
	if row.Prompt != scheduleToolWaitPrompt || row.EveryS != 60 || row.Title != "wake" {
		t.Fatalf("spec=%+v", row)
	}

	again, err := tl.InvokableRun(context.Background(),
		`{"id":"`+id+`","prompt":"`+scheduleToolWaitPrompt+`","delay_s":90,"title":"later"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, again)
	if got.ID != id {
		t.Fatalf("upsert minted a second id: %q then %q", id, got.ID)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("wakes=%d, upsert must not mint a second", len(listed))
	}
	row, err = e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.DelayS != 90 || row.EveryS != 0 || row.Title != "later" {
		t.Fatalf("updated spec=%+v", row)
	}
}

func TestScheduleWakeWithoutIdReplacesTheOpenWake(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	tl := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}))

	first, err := tl.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60,"title":"wake"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, first).ID

	again, err := tl.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","delay_s":90,"title":"later"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, again)
	if got.ID != id {
		t.Fatalf("omitted id minted a second wait: %q then %q", id, got.ID)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("wakes=%d, omitted id must replace", len(listed))
	}
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.DelayS != 90 || row.EveryS != 0 || row.Title != "later" || row.Status != store.ScheduleActive {
		t.Fatalf("replaced spec=%+v", row)
	}
}

func TestScheduleTaskRejectedOnSyntheticTurns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")

	schedTurn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, schedTurn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("ContinueSchedule: %s %v", out, err)
	}

	goalTurn := mustStoredTurn(t, e, th.ID, store.Turn{GoalContinue: true})
	out, err = ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, goalTurn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("ContinueGoal: %s %v", out, err)
	}

	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("rejected creates still persisted: %+v", listed)
	}
}

func TestScheduleTaskRejectedOnImplementPlan(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.turnID = turn.ID
	rt.planImplement = true
	rt.mu.Unlock()

	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("ImplementPlan: %s %v", out, err)
	}
}

func TestScheduleTaskRejectedOnResumedImplementPlan(t *testing.T) {
	// occupy() is resume: it claims the leftover turn and wipes planImplement.
	// KindPlanImplemented is the durable mark. If we only trust the in-memory
	// flag, schedule_task sneaks through after a crash.
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: KindPlanImplemented, Text: planImplementedNotice,
	})

	rt := e.runtimeFor(th.ID)
	idle := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if !rt.occupy(nil, cancel, turn.ID, idle) {
		t.Fatal("occupy")
	}
	t.Cleanup(func() { rt.release(cancel, idle, turn.ID) })
	if rt.recordingPlanImplement() {
		t.Fatal("occupy must leave the in-memory plan flag off")
	}

	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("resumed implement turn must still block schedule_task: %s %v", out, err)
	}
	listed, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("rejected create still persisted: %+v", listed)
	}
}

func TestScheduleTaskArmsStandaloneOnAHumanTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60,"title":"job"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, out).ID
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != store.ScheduleStandalone || row.ThreadID != "" {
		t.Fatalf("standalone must not pin a target conversation: %+v", row)
	}
	if row.OriginThreadID != th.ID || row.CreatedBy != store.ScheduleCreatedManager {
		t.Fatalf("origin=%+v", row)
	}
}

func TestReportScheduleEmptyFindingsIsQuiet(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	tl := mustInvokable(t, ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}))
	out, err := tl.InvokableRun(context.Background(), `{"findings":""}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, out)
	if !got.Quiet {
		t.Fatalf("empty findings must be quiet: %s", out)
	}

	human := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err = ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, human.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":""}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("human turn: %s %v", out, err)
	}
}

func TestReportScheduleFindingsAreNotQuiet(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"something changed"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, out)
	if got.Quiet {
		t.Fatalf("non-empty findings must not be quiet: %s", out)
	}
}

func TestScheduleToolsRejectGarbageJSON(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	tools := []tool.InvokableTool{
		mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
			return e.scheduleWakeJSON(th.ID, turn.ID, args)
		})),
		mustInvokable(t, ScheduleTaskTool(func(args string) (string, error) {
			return e.scheduleTaskJSON(th.ID, turn.ID, args)
		})),
		mustInvokable(t, CancelScheduleTool(func(args string) (string, error) {
			return e.cancelScheduleJSON(th.ID, turn.ID, args)
		})),
		mustInvokable(t, ReportScheduleTool(func(args string) (string, error) {
			return e.reportScheduleJSON(th.ID, turn.ID, args)
		})),
	}
	for _, tl := range tools {
		out, err := tl.InvokableRun(context.Background(), `{`)
		if err != nil {
			t.Fatalf("InvokableRun must not return a Go error: %v", err)
		}
		if !strings.Contains(out, `"ok":false`) {
			t.Fatalf("garbage args: %s", out)
		}
	}
}

func TestScheduleToolNilHandler(t *testing.T) {
	for _, tl := range []tool.InvokableTool{
		mustInvokable(t, ScheduleWakeTool(nil)),
		mustInvokable(t, ScheduleTaskTool(nil)),
		mustInvokable(t, CancelScheduleTool(nil)),
		mustInvokable(t, ReportScheduleTool(nil)),
	} {
		out, err := tl.InvokableRun(context.Background(), `{}`)
		if err != nil || !strings.Contains(out, `"ok":false`) {
			t.Fatalf("unwired: %s %v", out, err)
		}
	}
	empty := ScheduleWakeTool(func(string) (string, error) { return "  ", nil })
	out, err := mustInvokable(t, empty).InvokableRun(context.Background(), "")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("blank result: %s %v", out, err)
	}
}

func TestCancelScheduleToolStopsTheWait(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	wake, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, wake).ID
	out, err := CancelScheduleTool(func(args string) (string, error) {
		return e.cancelScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"id":"`+id+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	mustToolOK(t, out)
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.ScheduleCancelled {
		t.Fatalf("status=%q", row.Status)
	}
}

func TestScheduleWakeRejectsAForeignId(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	foreign := mustCreateWake(t, e, other.ID)
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"id":"`+foreign.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("foreign id: %s %v", out, err)
	}
}

func TestReportScheduleRejectsABelowFloorRecadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"","next_in_s":1}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("next_in_s below floor: %s %v", out, err)
	}
}

func TestCancelScheduleToolNeedsAnId(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := CancelScheduleTool(func(args string) (string, error) {
		return e.cancelScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("missing id: %s %v", out, err)
	}
}

func TestScheduleWakeOptionalUntilAndMaxRuns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","delay_s":60,"max_runs":3,"until":"2030-01-02T03:04:05Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, out).ID
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.DelayS != 60 || row.MaxRuns != 3 || row.UntilAt == nil {
		t.Fatalf("optional fields: %+v", row)
	}
	if row.UntilAt.UTC().Format(time.RFC3339) != "2030-01-02T03:04:05Z" {
		t.Fatalf("until=%s", row.UntilAt)
	}
}

func TestScheduleWakeRejectsMissingCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("no cadence: %s %v", out, err)
	}
}

func TestWorkersCannotSchedule(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	reg := e.newTurnRegistry(func(role, id string) model.BaseChatModel { return nil }, set, nil)
	t.Cleanup(reg.Close)
	found := 0
	for _, bt := range reg.SubAgentTools {
		info, err := bt.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		switch info.Name {
		case ToolScheduleWake, ToolScheduleTask, ToolCancelSchedule, ToolReportSchedule:
		default:
			continue
		}
		inv, ok := bt.(tool.InvokableTool)
		if !ok {
			t.Fatalf("worker %s is not invokable", info.Name)
		}
		out, err := inv.InvokableRun(context.Background(), `{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60,"id":"sch_x"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "workers cannot schedule") {
			t.Fatalf("%s out=%s", info.Name, out)
		}
		found++
	}
	if found != 4 {
		t.Fatalf("workers must have the four schedule deny stubs, got %d", found)
	}
}

func TestManagerScheduleToolsAreWired(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	rt := e.runtimeFor(th.ID)
	got := rt.managerScheduleTools(turn)
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	want := []string{ToolScheduleWake, ToolScheduleTask, ToolCancelSchedule, ToolReportSchedule}
	for i, tl := range got {
		info, err := tl.Info(context.Background())
		if err != nil || info.Name != want[i] {
			t.Fatalf("tool %d: %+v %v", i, info, err)
		}
	}
	wake := mustInvokable(t, got[0])
	out, err := wake.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, out).ID
	task := mustInvokable(t, got[1])
	out, err = task.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("task on a scheduled turn: %s %v", out, err)
	}
	cancel := mustInvokable(t, got[2])
	out, err = cancel.InvokableRun(context.Background(), `{"id":"`+id+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	mustToolOK(t, out)
	report := mustInvokable(t, got[3])
	out, err = report.InvokableRun(context.Background(), `{"findings":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if !mustToolOK(t, out).Quiet {
		t.Fatalf("report: %s", out)
	}
	if n := len(rt.managerScheduleTools(nil)); n != 4 {
		t.Fatalf("nil turn still mounts the four tools, got %d", n)
	}
}

func TestScheduleWakeUnknownIdIsJSON(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"id":"sch_missing","prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("unknown id: %s %v", out, err)
	}
}

func TestScheduleWakeUpsertRejectsBelowFloor(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	tl := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}))
	first, err := tl.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, first).ID
	out, err := tl.InvokableRun(context.Background(),
		`{"id":"`+id+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":1}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("below floor upsert: %s %v", out, err)
	}
}

func TestScheduleWakeUpdatesAPausedWake(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	sch := mustCreateWake(t, e, th.ID)
	if _, err := e.PatchSchedule(sch.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"id":"`+sch.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":90,"title":"paused"}`)
	if err != nil {
		t.Fatal(err)
	}
	mustToolOK(t, out)
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.EveryS != 90 || row.Title != "paused" || row.Status != store.SchedulePaused {
		t.Fatalf("paused upsert=%+v", row)
	}
}

func TestScheduleTaskRejectsBelowFloor(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":1}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("below floor: %s %v", out, err)
	}
}

func TestReportScheduleMissingTurnIsJSON(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, "tn_missing", args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":""}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("missing turn: %s %v", out, err)
	}
}

func TestReportScheduleRejectedOnWrongThread(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, other.ID, store.Turn{ScheduleContinue: true})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":""}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("wrong thread: %s %v", out, err)
	}
}

func TestReportScheduleRecadenceWithoutARunIsStillOK(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"","next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !mustToolOK(t, out).Quiet {
		t.Fatalf("quiet: %s", out)
	}
}

func TestReportScheduleUnknownRunIsQuiet(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: "srun_missing"})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"","keep":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if !mustToolOK(t, out).Quiet {
		t.Fatalf("unknown run must not fail the report: %s", out)
	}
}

func TestScheduleWakeRejectsAStandaloneId(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"id":"`+sch.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("standalone id: %s %v", out, err)
	}
}

func TestCancelScheduleWhitespaceIdIsJSON(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := CancelScheduleTool(func(args string) (string, error) {
		return e.cancelScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"id":"  "}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("blank id: %s %v", out, err)
	}
}

func TestScheduleTaskMissingPrompt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("missing prompt: %s %v", out, err)
	}
}

func TestReportScheduleOrphanRunIsQuiet(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	run := &store.ScheduleRun{ScheduleID: "sch_ghost", ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"","next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !mustToolOK(t, out).Quiet {
		t.Fatalf("orphan run: %s", out)
	}
}

func mustInvokable(t *testing.T, tl tool.BaseTool) tool.InvokableTool {
	t.Helper()
	inv, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatal("schedule tool must be invokable")
	}
	return inv
}

type scheduleToolResult struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	Quiet bool   `json:"quiet"`
	Error string `json:"error"`
}

func mustToolOK(t *testing.T, raw string) scheduleToolResult {
	t.Helper()
	var got scheduleToolResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil || !got.OK {
		t.Fatalf("result=%s err=%v", raw, err)
	}
	return got
}

func TestScheduleWakeRejectsABadUntil(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60,"until":"not-a-time"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("bad until: %s %v", out, err)
	}
}

func TestScheduleWakeMissingPrompt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("missing prompt: %s %v", out, err)
	}
}

func TestScheduleTaskNeedsATurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, "tn_missing", args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("missing turn: %s %v", out, err)
	}
}

func TestScheduleToolHandlerErrorIsJSON(t *testing.T) {
	tl := mustInvokable(t, ScheduleWakeTool(func(string) (string, error) {
		return "", fmt.Errorf("boom")
	}))
	out, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "boom") {
		t.Fatalf("handler error: %s %v", out, err)
	}
}

func TestReportScheduleKeepFalseCancels(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"","keep":false}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, out)
	if !got.Quiet {
		t.Fatalf("empty findings: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.ScheduleCancelled {
		t.Fatalf("keep=false must cancel: %q", row.Status)
	}
}

func TestScheduleWakeCronCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","cron":"0 9 * * 1"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, out).ID
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Cron != "0 9 * * 1" || row.DelayS != 0 || row.EveryS != 0 {
		t.Fatalf("cron row=%+v", row)
	}
}

func TestCancelScheduleUnknownIdIsJSON(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := CancelScheduleTool(func(args string) (string, error) {
		return e.cancelScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"id":"sch_missing"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("unknown id: %s %v", out, err)
	}
}

func TestScheduleWakeUpsertClearsTheOtherCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	tl := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}))
	first, err := tl.InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	id := mustToolOK(t, first).ID
	_, err = tl.InvokableRun(context.Background(),
		`{"id":"`+id+`","prompt":"`+scheduleToolWaitPrompt+`","cron":"5 4 * * 0"}`)
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Cron != "5 4 * * 0" || row.EveryS != 0 || row.DelayS != 0 {
		t.Fatalf("cadence not replaced: %+v", row)
	}
}

func TestReportScheduleRecadenceUpdatesTheInterval(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch := mustCreateWake(t, e, th.ID)
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"note","next_in_s":120}`)
	if err != nil {
		t.Fatal(err)
	}
	got := mustToolOK(t, out)
	if got.Quiet {
		t.Fatalf("findings must not be quiet: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.EveryS != 120 || row.DelayS != 0 || row.Cron != "" {
		t.Fatalf("recadence=%+v", row)
	}
}

func TestReportScheduleNextInSRearmsAFiredDelay(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleToolWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{
		"status": store.ScheduleDone, "run_count": 1,
	}); err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	out, err := ReportScheduleTool(func(args string) (string, error) {
		return e.reportScheduleJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(), `{"findings":"note","next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !mustToolOK(t, out).OK {
		t.Fatalf("rearm: %s", out)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.ScheduleActive {
		t.Fatalf("status=%q, next_in_s must rearm a fired delay", row.Status)
	}
	if row.EveryS != 60 || row.DelayS != 0 {
		t.Fatalf("cadence=%+v", row)
	}
	if n := countThreadKind(t, e, th.ID, KindSchedule); n < 2 {
		t.Fatalf("armed chips=%d, recadence must record a wait", n)
	}
}

func TestScheduleWakeRejectsACancelledId(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	out, err := ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"id":"`+sch.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("cancelled id: %s %v", out, err)
	}
}

func TestScheduleTaskRejectedOnWrongThread(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, other.ID, store.Turn{})
	out, err := ScheduleTaskTool(func(args string) (string, error) {
		return e.scheduleTaskJSON(th.ID, turn.ID, args)
	}).(tool.InvokableTool).InvokableRun(context.Background(),
		`{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("wrong thread: %s %v", out, err)
	}
}

func mustStoredTurn(t *testing.T, e *Engine, threadID string, flags store.Turn) *store.Turn {
	t.Helper()
	turn := &store.Turn{
		ThreadID:         threadID,
		UserText:         "check current state",
		GoalContinue:     flags.GoalContinue,
		ScheduleContinue: flags.ScheduleContinue,
		ScheduleRunID:    flags.ScheduleRunID,
	}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	return turn
}
