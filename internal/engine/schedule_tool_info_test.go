package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestScheduleToolNamesAreStable(t *testing.T) {
	// These names sit in transcripts and Trace. Renaming one is a protocol change.
	if ToolScheduleWake != "schedule_wake" || ToolScheduleTask != "schedule_task" ||
		ToolCancelSchedule != "cancel_schedule" || ToolReportSchedule != "report_schedule" {
		t.Fatalf("names drifted: %q %q %q %q",
			ToolScheduleWake, ToolScheduleTask, ToolCancelSchedule, ToolReportSchedule)
	}
}

func TestScheduleToolInfoStaysGeneric(t *testing.T) {
	wake, err := ScheduleWakeTool(nil).Info(context.Background())
	if err != nil || wake == nil {
		t.Fatalf("wake info: %+v %v", wake, err)
	}
	if !strings.Contains(wake.Desc, "about a third") {
		t.Fatalf("wake desc must bias short of remaining estimates:\n%s", wake.Desc)
	}
	report, err := ReportScheduleTool(nil).Info(context.Background())
	if err != nil || report == nil {
		t.Fatalf("report info: %+v %v", report, err)
	}
	if !strings.Contains(report.Desc, "never stretch") {
		t.Fatalf("report desc must not stretch a remaining-time interval:\n%s", report.Desc)
	}
	task, err := ScheduleTaskTool(nil).Info(context.Background())
	if err != nil || task == nil {
		t.Fatalf("task info: %+v %v", task, err)
	}
	if strings.Contains(task.Desc, "about a third") {
		t.Fatal("a human-specified job must not inherit the estimate bias")
	}
	if !strings.Contains(scheduleWakeCadenceParams()["delay_s"].Desc, "about a third") {
		t.Fatal("wake delay_s must bias short of a remaining-time estimate")
	}
	if strings.Contains(scheduleCadenceParams()["delay_s"].Desc, "about a third") {
		t.Fatal("shared cadence params must not bias a human-specified job")
	}
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
