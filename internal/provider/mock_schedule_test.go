package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

const scheduledCheckUser = "This turn is a scheduled check. Do the check in the instruction below, then call report_schedule. Empty findings archive the run. Cancel the schedule when the wait is over.\n\nContinue the wait."

func TestMockAutoReportsAScheduledTurn(t *testing.T) {
	msg := managerScript(1, []*schema.Message{schema.UserMessage(scheduledCheckUser)})
	if msg == nil || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != reportScheduleToolName {
		t.Fatalf("default mock must report a scheduled check, got %+v", msg)
	}
	if !strings.Contains(msg.ToolCalls[0].Function.Arguments, `"findings":"`+mockScheduleFindingsText+`"`) {
		t.Fatalf("args=%s", msg.ToolCalls[0].Function.Arguments)
	}
	assertNoScheduleLeak(t, msg.ToolCalls[0].Function.Arguments)

	second := managerScript(2, []*schema.Message{
		schema.UserMessage(scheduledCheckUser),
		msg,
		schema.ToolMessage(`{"ok":true,"quiet":false}`, msg.ToolCalls[0].ID),
	})
	if second == nil || len(second.ToolCalls) != 0 {
		t.Fatalf("after auto-report the mock must finish, got %+v", second)
	}
}

func TestMockScheduleSpawnOptInStillFansOut(t *testing.T) {
	SetMockScheduleSpawn(true)
	t.Cleanup(func() { SetMockScheduleSpawn(false) })

	msg := managerScript(1, []*schema.Message{schema.UserMessage(scheduledCheckUser)})
	if msg == nil || len(msg.ToolCalls) == 0 || msg.ToolCalls[0].Function.Name != "spawn_agent" {
		t.Fatalf("spawn opt-in must still fan out, got %+v", msg)
	}
}

func TestMockScheduleQuietEnvReportsEmptyFindings(t *testing.T) {
	t.Setenv("ZWAI_MOCK_SCHEDULE_QUIET", "1")

	first := managerScript(1, []*schema.Message{schema.UserMessage(scheduledCheckUser)})
	if first == nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != reportScheduleToolName {
		t.Fatalf("quiet env must report, got %+v", first)
	}
	if !strings.Contains(first.ToolCalls[0].Function.Arguments, `"findings":""`) {
		t.Fatalf("args=%s", first.ToolCalls[0].Function.Arguments)
	}
	assertNoScheduleLeak(t, first.ToolCalls[0].Function.Arguments)
}

func TestMockScheduleWakeEnvArmsAMinIntervalWait(t *testing.T) {
	t.Setenv("ZWAI_MOCK_SCHEDULE_WAKE", "1")

	task := "Continue the wait."
	first := managerScript(1, []*schema.Message{schema.UserMessage(task)})
	if first == nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != scheduleWakeToolName {
		t.Fatalf("wake env must arm on the first manager step, got %+v", first)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(first.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["cron"] != nil && args["cron"] != "" {
		t.Fatalf("do not invent a cron sample: %s", first.ToolCalls[0].Function.Arguments)
	}
	every, _ := args["every_s"].(float64)
	if int(every) != config.DefaultScheduleMinIntervalSeconds {
		t.Fatalf("every_s=%v want min interval %d", args["every_s"], config.DefaultScheduleMinIntervalSeconds)
	}
	if args["prompt"] != task {
		t.Fatalf("prompt=%v want last user text", args["prompt"])
	}
	assertNoScheduleLeak(t, first.ToolCalls[0].Function.Arguments)

	second := managerScript(2, []*schema.Message{
		schema.UserMessage(task),
		first,
		schema.ToolMessage(`{"ok":true,"id":"sch_1"}`, first.ToolCalls[0].ID),
	})
	if second == nil || len(second.ToolCalls) != 0 {
		t.Fatalf("after wake the mock must finish, not spawn, got %+v", second)
	}
}

func TestMockScheduleWakeEnvStaysOffByDefault(t *testing.T) {
	got := managerScript(1, []*schema.Message{schema.UserMessage("Continue the wait.")})
	if got == nil || len(got.ToolCalls) == 0 || got.ToolCalls[0].Function.Name != "spawn_agent" {
		t.Fatalf("wake env default off must still spawn, got %+v", got)
	}
}

func TestMockScheduleFlagsWinOverAutoReport(t *testing.T) {
	SetMockScheduleSilent(true)
	t.Cleanup(func() { SetMockScheduleSilent(false) })

	got := managerScript(1, []*schema.Message{schema.UserMessage(scheduledCheckUser)})
	if got == nil || strings.TrimSpace(got.Content) != "" || len(got.ToolCalls) != 0 {
		t.Fatalf("silent flag must win over auto-report, got %+v", got)
	}
}

func TestMockScheduleFlagsWinOverWakeEnv(t *testing.T) {
	SetMockScheduleFindings(true)
	t.Cleanup(func() { SetMockScheduleFindings(false) })
	t.Setenv("ZWAI_MOCK_SCHEDULE_WAKE", "1")

	got := managerScript(1, []*schema.Message{schema.UserMessage("Continue the wait.")})
	if got == nil || len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != reportScheduleToolName {
		t.Fatalf("findings flag must win over wake env, got %+v", got)
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
	assertNoScheduleLeak(t, first.ToolCalls[0].Function.Arguments)

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

func assertNoScheduleLeak(t *testing.T, s string) {
	t.Helper()
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(s, w) {
			t.Fatalf("leaked %q", w)
		}
	}
}
