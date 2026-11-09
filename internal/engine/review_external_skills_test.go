package engine

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestLoadedExternalPackageDoesNotBecomeLearnedEvidence(t *testing.T) {
	const external = "instructions from an installed package"
	const local = "independently observed local result"
	call := swarm.NotifyToolCall.String()
	result := swarm.NotifyToolResult.String()
	events := []store.Event{
		{Seq: 1, Kind: call, AgentID: "worker", ToolCallID: "same", Text: `load_skill({"name":"check-files"})`},
		{Seq: 2, Kind: call, AgentID: "manager", ToolCallID: "same", Text: `read({"file_path":"local.md"})`},
		{Seq: 3, Kind: result, AgentID: "worker", ToolCallID: "same", Text: external},
		{Seq: 4, Kind: result, AgentID: "manager", ToolCallID: "same", Text: local},
	}
	input := selectReviewInput(events, "", "")
	if strings.Contains(input.Transcript, external) || strings.Contains(input.Transcript, "load_skill(") ||
		!strings.Contains(input.Transcript, local) {
		t.Fatalf("review transcript mixed the two tool pairs: %s", input.Transcript)
	}
	sources := reviewEvidenceSources(events, input.Events)
	if _, ok := sources[3]; ok || sources[4].Text != local {
		t.Fatalf("review evidence=%v", sources)
	}
}
