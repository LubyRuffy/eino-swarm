package tui

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
)

func TestScheduleWakeSetsAOneLineNotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    `schedule_wake({"every_s":30,"prompt":"Continue the wait."})`,
	})
	if m.notice != "A wait is armed." {
		t.Fatalf("notice=%q", m.notice)
	}
	assertNoScheduleLeak(t, m.notice)
}

func TestCancelScheduleSetsAOneLineNotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    `cancel_schedule({"id":"sch_1"})`,
	})
	if m.notice != "A wait was cancelled." {
		t.Fatalf("notice=%q", m.notice)
	}
}

func TestReportScheduleWithFindingsSetsANotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    `report_schedule({"findings":"something changed"})`,
	})
	if m.notice != "something changed" {
		t.Fatalf("notice=%q", m.notice)
	}
	if strings.Contains(m.notice, "{") {
		t.Fatalf("dumped JSON: %q", m.notice)
	}
	assertNoScheduleLeak(t, m.notice)
}

func TestReportScheduleFallsBackWhenFindingsAreNotText(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    `report_schedule({"findings":{"note":"x"}})`,
	})
	if m.notice != "Scheduled check reported." {
		t.Fatalf("notice=%q", m.notice)
	}
	if strings.Contains(m.notice, "{") {
		t.Fatalf("dumped JSON: %q", m.notice)
	}
}

func TestQuietReportScheduleDoesNotSetANotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    `report_schedule({"findings":""})`,
	})
	if m.notice != "" {
		t.Fatalf("quiet report must stay silent, notice=%q", m.notice)
	}
}

func TestScheduleSkipIsNotANotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyAgentMessage,
		AgentID: swarm.DefaultManagerID,
		Text:    "the conversation is busy",
	})
	if m.notice != "" {
		t.Fatalf("skip is not a tool; notice=%q", m.notice)
	}
}

func TestARegularToolCallDoesNotSetAScheduleNotice(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{
		Kind:    swarm.NotifyToolCall,
		AgentID: swarm.DefaultManagerID,
		Text:    "read(notes.md)",
	})
	if m.notice != "" {
		t.Fatalf("ordinary tools must not steal the status line, notice=%q", m.notice)
	}
}

func TestScheduleWakeNoticeFromToolResult(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{
			Kind:       swarm.NotifyToolCall,
			AgentID:    swarm.DefaultManagerID,
			ToolCallID: "w1",
			Text:       `schedule_wake({"every_s":30,"prompt":"Continue the wait."})`,
		},
		swarm.Notification{
			Kind:       swarm.NotifyToolResult,
			AgentID:    swarm.DefaultManagerID,
			ToolCallID: "w1",
			Text:       `{"ok":true,"id":"sch_1"}`,
		},
	)
	if m.notice != "A wait is armed." {
		t.Fatalf("notice=%q", m.notice)
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
