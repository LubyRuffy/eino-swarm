package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-tools/read"
	"github.com/cloudwego/eino/schema"
)

func TestMicroCompactClearsOldReplayableResultsAndKeepsTheTail(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("go"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "c1", Function: schema.FunctionCall{Name: read.ToolName}},
			{ID: "c2", Function: schema.FunctionCall{Name: read.ToolName}},
			{ID: "c3", Function: schema.FunctionCall{Name: read.ToolName}},
			{ID: "c4", Function: schema.FunctionCall{Name: read.ToolName}},
		}},
		{Role: schema.Tool, ToolCallID: "c1", Content: "body-one"},
		{Role: schema.Tool, ToolCallID: "c2", Content: "body-two"},
		{Role: schema.Tool, ToolCallID: "c3", Content: "body-three"},
		{Role: schema.Tool, ToolCallID: "c4", Content: "body-four"},
	}
	got := microCompact(msgs, 2)
	if got[2].Content != microcompactPlaceholder || got[3].Content != microcompactPlaceholder {
		t.Fatalf("oldest replayable results must clear: %+v %+v", got[2], got[3])
	}
	if got[4].Content != "body-three" || got[5].Content != "body-four" {
		t.Fatalf("the kept tail vanished: %+v %+v", got[4], got[5])
	}
	if msgs[2].Content != "body-one" {
		t.Fatal("microCompact must not mutate the original messages")
	}
	if got[1].ToolCalls[0].ID != "c1" {
		t.Fatal("tool_use/tool_result pairing must stay intact")
	}
}

func TestMicroCompactLeavesLifecycleAndMemoryResults(t *testing.T) {
	msgs := []*schema.Message{
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "s1", Function: schema.FunctionCall{Name: spawnAgentToolName}},
			{ID: "m1", Function: schema.FunctionCall{Name: "memory"}},
		}},
		{Role: schema.Tool, ToolCallID: "s1", Content: "spawned worker-1"},
		{Role: schema.Tool, ToolCallID: "m1", Content: "stored a note"},
	}
	got := microCompact(msgs, 0)
	if got[1].Content != "spawned worker-1" || got[2].Content != "stored a note" {
		t.Fatalf("non-replayable results were cleared: %+v", got)
	}
}

func TestMicroCompactIsANoopWhenNothingIsOld(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("hi"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "c1", Function: schema.FunctionCall{Name: read.ToolName}},
		}},
		{Role: schema.Tool, ToolCallID: "c1", Content: "short"},
	}
	got := microCompact(msgs, 3)
	if !sameMsgSlice(msgs, got) {
		t.Fatal("a short tail must not be copied for nothing")
	}
}

func sameMsgSlice(a, b []*schema.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMicroCompactChangedDetectsAClearedBody(t *testing.T) {
	a := []*schema.Message{{Role: schema.Tool, Content: "x"}}
	b := microCompact([]*schema.Message{
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "c", Function: schema.FunctionCall{Name: read.ToolName}},
		}},
		{Role: schema.Tool, ToolCallID: "c", Content: "x"},
	}, 0)
	if !microCompactChanged(a, []*schema.Message{{Role: schema.Tool, Content: "y"}}) {
		t.Fatal("different bodies must count as a change")
	}
	if microCompactChanged(b, b) {
		t.Fatal("the same slice is not a change")
	}
	if !strings.Contains(strings.Join([]string{b[1].Content}, ""), microcompactPlaceholder) {
		t.Fatalf("keep=0 must clear the only replayable result: %+v", b)
	}
}

func TestMicroCompactEmptyAndNegativeKeepAreNoopsOrClear(t *testing.T) {
	if got := microCompact(nil, 3); got != nil {
		t.Fatalf("nil in=%v", got)
	}
	if got := microCompact([]*schema.Message{}, -1); len(got) != 0 {
		t.Fatalf("empty=%v", got)
	}
	msgs := []*schema.Message{
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "c", Function: schema.FunctionCall{Name: read.ToolName}},
		}},
		{Role: schema.Tool, ToolCallID: "c", Content: microcompactPlaceholder},
	}
	if !sameMsgSlice(msgs, microCompact(msgs, 0)) {
		t.Fatal("an already-cleared result must not be copied again")
	}
}

func TestMicroCompactChangedNilAndLength(t *testing.T) {
	if !microCompactChanged(nil, []*schema.Message{{}}) {
		t.Fatal("different lengths must count")
	}
	if !microCompactChanged([]*schema.Message{nil}, []*schema.Message{{Role: schema.Tool}}) {
		t.Fatal("nil vs a message must count")
	}
}

func TestMicroCompactSkipsNilMessages(t *testing.T) {
	msgs := []*schema.Message{
		nil,
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			{ID: "c", Function: schema.FunctionCall{Name: read.ToolName}},
		}},
		{Role: schema.Tool, ToolCallID: "c", Content: "body"},
	}
	got := microCompact(msgs, 0)
	if got[0] != nil {
		t.Fatal("a nil slot must stay a nil slot")
	}
	if got[2].Content != microcompactPlaceholder {
		t.Fatalf("the replayable body next to a nil slot must still clear: %+v", got[2])
	}
}
