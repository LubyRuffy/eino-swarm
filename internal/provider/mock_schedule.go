package provider

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/schema"
)

// reportScheduleToolName is the wire name for a scheduled-check report.
// Tests opt in; the default mock must not sniff a scheduled turn and call it.
const reportScheduleToolName = "report_schedule"

const mockScheduleFindingsText = "something changed"

type mockScheduleKind int

const (
	mockScheduleOff mockScheduleKind = iota
	mockScheduleQuiet
	mockScheduleSilent
	mockScheduleFindings
)

var (
	mockScheduleMu   sync.Mutex
	mockScheduleMode mockScheduleKind
)

// SetMockScheduleQuiet makes the first generate call report_schedule with
// empty findings, then finish short — before any spawn. Restore false in Cleanup.
func SetMockScheduleQuiet(v bool) {
	setMockScheduleMode(mockScheduleQuiet, v)
}

// SetMockScheduleSilent skips spawn, leaves assistant content empty, and
// never calls report_schedule. Restore false in Cleanup.
func SetMockScheduleSilent(v bool) {
	setMockScheduleMode(mockScheduleSilent, v)
}

// SetMockScheduleFindings makes the first generate call report_schedule with
// generic findings, then finish. Restore false in Cleanup.
func SetMockScheduleFindings(v bool) {
	setMockScheduleMode(mockScheduleFindings, v)
}

func setMockScheduleMode(kind mockScheduleKind, on bool) {
	mockScheduleMu.Lock()
	defer mockScheduleMu.Unlock()
	if on {
		mockScheduleMode = kind
		return
	}
	if mockScheduleMode == kind {
		mockScheduleMode = mockScheduleOff
	}
}

func currentMockScheduleMode() mockScheduleKind {
	mockScheduleMu.Lock()
	defer mockScheduleMu.Unlock()
	return mockScheduleMode
}

func mockScheduleScript(turn int, msgs []*schema.Message) *schema.Message {
	switch currentMockScheduleMode() {
	case mockScheduleSilent:
		return schema.AssistantMessage("", nil)
	case mockScheduleQuiet:
		return mockScheduleReportMessage(turn, msgs, "")
	case mockScheduleFindings:
		return mockScheduleReportMessage(turn, msgs, mockScheduleFindingsText)
	default:
		return nil
	}
}

func mockScheduleReportMessage(turn int, msgs []*schema.Message, findings string) *schema.Message {
	if alreadyCalledTool(msgs, reportScheduleToolName) {
		if findings == "" {
			return schema.AssistantMessage("ok\n", nil)
		}
		return schema.AssistantMessage("", nil)
	}
	args, _ := json.Marshal(map[string]any{"findings": findings})
	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			call(fmt.Sprintf("mock-report-%d", turn), reportScheduleToolName, string(args)),
		},
	}
}
