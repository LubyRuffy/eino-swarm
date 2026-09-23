package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestScheduleWakeSchemaHasNoId(t *testing.T) {
	// An optional id is a name the model invents. The first arm has nothing
	// to copy, so the field must not be on the tool.
	info, err := ScheduleWakeTool(nil).Info(context.Background())
	if err != nil || info == nil || info.ParamsOneOf == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || js == nil || js.Properties == nil {
		t.Fatalf("schema: %+v %v", js, err)
	}
	if prop, ok := js.Properties.Get("id"); ok || prop != nil {
		t.Fatalf("schedule_wake must not offer an id, got %+v", prop)
	}
	if strings.Contains(info.Desc, "pass id") {
		t.Fatalf("wake desc still asks for an id:\n%s", info.Desc)
	}
}

func TestScheduleWakeLabelIdStillArms(t *testing.T) {
	// The missing-row rule is "not a stored id", not "does not look like sch_".
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	out, err := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	})).InvokableRun(context.Background(),
		`{"id":"label-not-a-row","prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "store: not found") {
		t.Fatalf("store sentinel leaked to the caller: %s", out)
	}
	if mustToolOK(t, out).ID == "" {
		t.Fatalf("label id did not arm: %s", out)
	}
}

func TestScheduleWakeUnknownIdReplacesTheOpenWake(t *testing.T) {
	// A label must not mint a second wait beside the one already open.
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
	out, err := tl.InvokableRun(context.Background(),
		`{"id":"label-not-a-row","prompt":"`+scheduleToolWaitPrompt+`","delay_s":90,"title":"later"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "store: not found") {
		t.Fatalf("store sentinel leaked to the caller: %s", out)
	}
	if got := mustToolOK(t, out).ID; got != id {
		t.Fatalf("label id minted %q beside %q", got, id)
	}
	listed, err := e.ListSchedules()
	if err != nil || len(listed) != 1 {
		t.Fatalf("wakes=%d err=%v", len(listed), err)
	}
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.DelayS != 90 || row.EveryS != 0 || row.Title != "later" {
		t.Fatalf("replaced spec=%+v", row)
	}
}

func TestScheduleWakeLabelBelowFloorDoesNotMintASecond(t *testing.T) {
	// A label still has to pass the cadence check. Failing that check on
	// the open wake must not fall through into a second row.
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
	out, err := tl.InvokableRun(context.Background(),
		`{"id":"label-not-a-row","prompt":"`+scheduleToolWaitPrompt+`","every_s":1}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("below floor: %s %v", out, err)
	}
	if strings.Contains(out, "store: not found") {
		t.Fatalf("cadence failure leaked as a missing row: %s", out)
	}
	if n := wakesOn(t, e, th.ID); n != 1 {
		t.Fatalf("wakes=%d", n)
	}
	row, err := e.Store().GetSchedule(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.EveryS != 60 || row.Title != "wake" {
		t.Fatalf("open wake changed: %+v", row)
	}
}

func TestScheduleWakeIdOnAnotherConversationDoesNotArmHere(t *testing.T) {
	e := newTestEngine(t)
	other, _ := e.CreateThread("", "", "")
	here, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, here.ID, store.Turn{})
	foreign := mustCreateWake(t, e, other.ID)
	out, err := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(here.ID, turn.ID, args)
	})).InvokableRun(context.Background(),
		`{"id":"`+foreign.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":90}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("foreign id: %s %v", out, err)
	}
	if n := wakesOn(t, e, here.ID); n != 0 {
		t.Fatalf("this conversation armed %d waits from another conversation's id", n)
	}
	row, err := e.Store().GetSchedule(foreign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ThreadID != other.ID || row.EveryS != 60 {
		t.Fatalf("foreign row changed: %+v", row)
	}
}

func TestScheduleWakeFinishedIdDoesNotArmAnother(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	sch := mustCreateWake(t, e, th.ID)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	out, err := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	})).InvokableRun(context.Background(),
		`{"id":"`+sch.ID+`","prompt":"`+scheduleToolWaitPrompt+`","every_s":90}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("finished id: %s %v", out, err)
	}
	if n := wakesOn(t, e, th.ID); n != 1 {
		t.Fatalf("wakes=%d, a finished id must not arm another", n)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.ScheduleCancelled {
		t.Fatalf("status=%s", row.Status)
	}
}

func wakesOn(t *testing.T, e *Engine, threadID string) int {
	t.Helper()
	rows, err := e.ListSchedules()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range rows {
		if row.ThreadID == threadID {
			n++
		}
	}
	return n
}
