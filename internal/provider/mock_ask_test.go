package provider

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

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
