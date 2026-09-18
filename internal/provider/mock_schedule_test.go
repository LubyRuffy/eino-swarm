package provider

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMockDoesNotAutoDetectAScheduledTurn(t *testing.T) {
	msg := managerScript(1, []*schema.Message{
		schema.UserMessage("This turn is a scheduled check. Do the check in the instruction below, then call report_schedule. Empty findings archive the run. Cancel the schedule when the wait is over.\n\nContinue the wait."),
	})
	if msg == nil || len(msg.ToolCalls) == 0 || msg.ToolCalls[0].Function.Name != "spawn_agent" {
		t.Fatalf("default mock must still spawn, got %+v", msg)
	}
}

func TestMockScheduleQuietReportsEmptyFindingsBeforeSpawn(t *testing.T) {
	SetMockScheduleQuiet(true)
	t.Cleanup(func() { SetMockScheduleQuiet(false) })

	first := managerScript(1, []*schema.Message{schema.UserMessage("Continue the wait.")})
	if first == nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != reportScheduleToolName {
		t.Fatalf("quiet mock must report before spawn, got %+v", first)
	}
	if !strings.Contains(first.ToolCalls[0].Function.Arguments, `"findings":""`) {
		t.Fatalf("args=%s", first.ToolCalls[0].Function.Arguments)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(first.ToolCalls[0].Function.Arguments, w) {
			t.Fatalf("leaked %q", w)
		}
	}

	second := managerScript(2, []*schema.Message{
		schema.UserMessage("Continue the wait."),
		first,
		schema.ToolMessage(`{"ok":true,"quiet":true}`, first.ToolCalls[0].ID),
	})
	if second == nil || len(second.ToolCalls) != 0 {
		t.Fatalf("after report, quiet mock must finish, got %+v", second)
	}
}

func TestMockScheduleSilentSkipsSpawnAndReport(t *testing.T) {
	SetMockScheduleSilent(true)
	t.Cleanup(func() { SetMockScheduleSilent(false) })

	got := managerScript(1, []*schema.Message{schema.UserMessage("Continue the wait.")})
	if got == nil || strings.TrimSpace(got.Content) != "" || len(got.ToolCalls) != 0 {
		t.Fatalf("silent mock must be an empty finish, got %+v", got)
	}
}

func TestMockScheduleFindingsReportsAChange(t *testing.T) {
	SetMockScheduleFindings(true)
	t.Cleanup(func() { SetMockScheduleFindings(false) })

	first := managerScript(1, []*schema.Message{schema.UserMessage("Continue the wait.")})
	if first == nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != reportScheduleToolName {
		t.Fatalf("findings mock must report, got %+v", first)
	}
	if !strings.Contains(first.ToolCalls[0].Function.Arguments, `"findings":"`+mockScheduleFindingsText+`"`) {
		t.Fatalf("args=%s", first.ToolCalls[0].Function.Arguments)
	}

	second := managerScript(2, []*schema.Message{
		schema.UserMessage("Continue the wait."),
		first,
		schema.ToolMessage(`{"ok":true,"quiet":false}`, first.ToolCalls[0].ID),
	})
	if second == nil || len(second.ToolCalls) != 0 {
		t.Fatalf("after report, findings mock must finish, got %+v", second)
	}
}
