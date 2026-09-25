package tui

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
)

// A streamed tool call used to paint a second row when the real call arrived.
// The preview and the call are the same invocation.
func TestToolCallDeltaBecomesTheSameRow(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyToolCallDelta, AgentID: swarm.DefaultManagerID, ToolCallID: "c1", Text: "spawn_agent(8)"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, ToolCallID: "c1", Text: `spawn_agent({"role":"writer"})`},
	)
	tools := blocksOfKind(m.manager, blockTool)
	if len(tools) != 1 {
		t.Fatalf("a finished call must replace its preview, got %d tool rows", len(tools))
	}
	if tools[0].toolName != "spawn_agent" || !strings.Contains(tools[0].toolArgs, "writer") {
		t.Fatalf("row = %+v", tools[0])
	}
}
