package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestCompleteGoalToolRecordsTheObjective(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	tl, ok := CompleteGoalTool(func(summary string) (string, error) {
		return e.completeGoalJSON(th.ID, summary)
	}).(tool.InvokableTool)
	if !ok {
		t.Fatal("complete_goal must be invokable")
	}
	info, err := CompleteGoalTool(nil).Info(context.Background())
	if err != nil || info.Name != ToolCompleteGoal {
		t.Fatalf("info: %+v %v", info, err)
	}
	out, err := tl.InvokableRun(context.Background(), `{"summary":"satisfied"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ok"] != true {
		t.Fatalf("result=%s err=%v", out, err)
	}
	th, _ = e.Store().GetThread(th.ID)
	if !th.GoalComplete {
		t.Fatal("tool must persist completion")
	}
	again, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again, `"ok":true`) {
		t.Fatalf("idempotent complete: %s", again)
	}
}

func TestCompleteGoalToolRejectsGarbageAndAMissingGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	tl := CompleteGoalTool(func(summary string) (string, error) {
		return e.completeGoalJSON(th.ID, summary)
	}).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("garbage args: %s %v", out, err)
	}
	out, err = tl.InvokableRun(context.Background(), `{"summary":"x"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("no goal: %s %v", out, err)
	}
}

func TestCompleteGoalToolNilHandlerAndEmptyResult(t *testing.T) {
	tl := CompleteGoalTool(nil).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("unwired: %s %v", out, err)
	}
	empty := CompleteGoalTool(func(string) (string, error) { return "  ", nil }).(tool.InvokableTool)
	out, err = empty.InvokableRun(context.Background(), "")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("blank result: %s %v", out, err)
	}
}

func TestBlockGoalToolRecordsTheBlock(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	tl, ok := BlockGoalTool(func(reason string) (string, error) {
		return e.blockGoalJSON(th.ID, reason)
	}).(tool.InvokableTool)
	if !ok {
		t.Fatal("block_goal must be invokable")
	}
	info, err := BlockGoalTool(nil).Info(context.Background())
	if err != nil || info.Name != ToolBlockGoal {
		t.Fatalf("info: %+v %v", info, err)
	}
	out, err := tl.InvokableRun(context.Background(), `{"reason":"needs an external change"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ok"] != true {
		t.Fatalf("result=%s err=%v", out, err)
	}
	th, _ = e.Store().GetThread(th.ID)
	if !th.GoalBlocked || th.GoalBlockReason != "needs an external change" {
		t.Fatalf("tool must persist the block: %+v", th)
	}
	again, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(again, `"ok":true`) {
		t.Fatalf("idempotent block: %s %v", again, err)
	}
}

func TestBlockGoalToolRejectsGarbageAndAMissingGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	tl := BlockGoalTool(func(reason string) (string, error) {
		return e.blockGoalJSON(th.ID, reason)
	}).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("garbage args: %s %v", out, err)
	}
	out, err = tl.InvokableRun(context.Background(), `{"reason":"x"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("no goal: %s %v", out, err)
	}
}

func TestBlockGoalToolNilHandlerAndEmptyResult(t *testing.T) {
	tl := BlockGoalTool(nil).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("unwired: %s %v", out, err)
	}
	empty := BlockGoalTool(func(string) (string, error) { return "  ", nil }).(tool.InvokableTool)
	out, err = empty.InvokableRun(context.Background(), "")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("blank result: %s %v", out, err)
	}
}

func TestBlockGoalToolInfoStaysGeneric(t *testing.T) {
	info, err := BlockGoalTool(nil).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"notes.md", "researcher", "elasticsearch"} {
		if strings.Contains(strings.ToLower(info.Desc), leak) {
			t.Fatalf("%q leaked into block_goal: %s", leak, info.Desc)
		}
	}
	if !strings.Contains(info.Desc, "three consecutive") {
		t.Fatal("block_goal must require a repeated blocker")
	}
}

func TestCompleteGoalToolInfoStaysGeneric(t *testing.T) {
	info, err := CompleteGoalTool(nil).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"notes.md", "researcher", "elasticsearch"} {
		if strings.Contains(strings.ToLower(info.Desc), leak) {
			t.Fatalf("%q leaked into complete_goal: %s", leak, info.Desc)
		}
	}
}
