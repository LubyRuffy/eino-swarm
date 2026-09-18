package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// reportScheduleToolName is the wire name for a scheduled-check report.
const reportScheduleToolName = "report_schedule"

// scheduleWakeToolName is the wire name for arming a wait on this conversation.
const scheduleWakeToolName = "schedule_wake"

const mockScheduleFindingsText = "something changed"

// scheduledCheckMarker is the engine wrapper prefix a fired wait puts on
// the last user text. Sniffing the whole durable prompt would bake a task
// into the mock.
const scheduledCheckMarker = "This turn is a scheduled check."

type mockScheduleKind int

const (
	mockScheduleOff mockScheduleKind = iota
	mockScheduleQuiet
	mockScheduleSilent
	mockScheduleFindings
)

var (
	mockScheduleMu    sync.Mutex
	mockScheduleMode  mockScheduleKind
	mockScheduleSpawn bool
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

// SetMockScheduleSpawn restores the old fan-out on a scheduled check.
// Restore false in Cleanup so the default auto-report stays on.
func SetMockScheduleSpawn(v bool) {
	mockScheduleMu.Lock()
	defer mockScheduleMu.Unlock()
	mockScheduleSpawn = v
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

func mockScheduleSpawnOptIn() bool {
	mockScheduleMu.Lock()
	defer mockScheduleMu.Unlock()
	return mockScheduleSpawn
}

func mockEnvOn(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) == "1"
}

func mockLooksLikeScheduledTurn(msgs []*schema.Message) bool {
	return strings.Contains(lastUserText(msgs), scheduledCheckMarker)
}

func mockScheduleScript(turn int, msgs []*schema.Message) *schema.Message {
	// Explicit test flags beat auto-detect and the env knobs.
	switch currentMockScheduleMode() {
	case mockScheduleSilent:
		return schema.AssistantMessage("", nil)
	case mockScheduleQuiet:
		return mockScheduleReportMessage(turn, msgs, "")
	case mockScheduleFindings:
		return mockScheduleReportMessage(turn, msgs, mockScheduleFindingsText)
	}
	if mockScheduleSpawnOptIn() {
		return nil
	}
	if mockLooksLikeScheduledTurn(msgs) {
		findings := mockScheduleFindingsText
		if mockEnvOn("ZWAI_MOCK_SCHEDULE_QUIET") {
			findings = ""
		}
		return mockScheduleReportMessage(turn, msgs, findings)
	}
	if mockEnvOn("ZWAI_MOCK_SCHEDULE_WAKE") {
		if alreadyCalledTool(msgs, scheduleWakeToolName) {
			return schema.AssistantMessage("ok\n", nil)
		}
		return mockScheduleWakeMessage(turn, msgs)
	}
	return nil
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

func mockScheduleWakeMessage(turn int, msgs []*schema.Message) *schema.Message {
	args, _ := json.Marshal(map[string]any{
		"every_s": config.DefaultScheduleMinIntervalSeconds,
		"prompt":  lastUserText(msgs),
	})
	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			call(fmt.Sprintf("mock-wake-%d", turn), scheduleWakeToolName, string(args)),
		},
	}
}
