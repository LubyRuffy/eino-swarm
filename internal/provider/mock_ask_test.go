package provider

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMockAskExercisesAnOptionWithoutAnID(t *testing.T) {
	msg := mockAskMessage(1)
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != askUserToolName {
		t.Fatalf("want one ask_user call, got %+v", msg)
	}
	var args struct {
		Questions []struct {
			Options []map[string]json.RawMessage `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if len(args.Questions) != 1 || len(args.Questions[0].Options) != 2 {
		t.Fatalf("want two choices, got %+v", args)
	}
	options := args.Questions[0].Options
	if string(options[0]["id"]) != `"safer"` {
		t.Fatalf("the explicit-id choice changed: %s", options[0]["id"])
	}
	if _, exists := options[1]["id"]; exists {
		t.Fatal("the label-derived choice must omit id so E2E keeps exercising host normalization")
	}
	if string(options[1]["label"]) != `"Prefer the faster path"` {
		t.Fatalf("the label-derived choice changed: %s", options[1]["label"])
	}
}

func TestMockPlanningAsksBeforeItProposes(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("You are planning. Implementation tools are not mounted."),
		schema.UserMessage("draft the approach"),
	}
	first := mockAskOrPlan(1, msgs)
	if first == nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != askUserToolName {
		t.Fatalf("planning must ask first, got %+v", first)
	}
	msgs = append(msgs, first, schema.ToolMessage(`{"ok":true}`, first.ToolCalls[0].ID))
	second := mockAskOrPlan(2, msgs)
	if second == nil || len(second.ToolCalls) != 1 || second.ToolCalls[0].Function.Name != proposePlanToolName {
		t.Fatalf("after the answer, planning must propose, got %+v", second)
	}
}

func TestMockAskStaysOffUnlessEnabled(t *testing.T) {
	msgs := []*schema.Message{schema.UserMessage("do the work")}
	if got := mockAskOrPlan(1, msgs); got != nil {
		t.Fatalf("a normal mock run must not ask, got %+v", got)
	}
}
