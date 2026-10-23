package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type moveResultTool struct {
	result string
	err    error
}

func (*moveResultTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: memory.ToolMemoryReclassify}, nil
}

func (t *moveResultTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return t.result, t.err
}

// A validation refusal remains feedback for the curator. A storage failure
// after a real write must stop it and become the final visible tidy error.
func TestTidyStopsOnPartialWritesAndPreservesRepairableRefusals(t *testing.T) {
	upstream := errors.New("the underlying storage operation failed")
	for _, tc := range []struct {
		name, result, cause string
		upstream            error
		wantError           bool
	}{
		{"saved move", `{"success":true,"changed":true}`, "", nil, false},
		{"already in destination", `{"success":true,"changed":false}`, "", nil, false},
		{"validation refusal", `{"success":false,"changed":false,"error":"the selected source is ambiguous"}`, "", nil, false},
		{"saved destination then failure", `{"success":false,"changed":true,"error":"the source could not be removed"}`, "the source could not be removed", nil, true},
		{"invalid result", `{broken`, "decode move result", nil, true},
		{"invalid success type", `{"success":"yes","changed":true}`, "decode move result", nil, true},
		{"upstream failure", `{"success":false}`, "underlying storage operation failed", upstream, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := &tidyReclassifyTool{InvokableTool: &moveResultTool{result: tc.result, err: tc.upstream}}
			out, err := wrapped.InvokableRun(context.Background(), `{"action":"promote"}`)
			if out != tc.result || (err != nil) != tc.wantError {
				t.Fatalf("result changed or wrong failure semantics: out=%q err=%v", out, err)
			}
			if tc.cause != "" && !strings.Contains(err.Error(), tc.cause) {
				t.Fatalf("failure lost its cause: %v", err)
			}
			if tc.upstream != nil && !errors.Is(err, tc.upstream) {
				t.Fatalf("failure lost its category: %v", err)
			}
		})
	}
}

func TestOnlyRequestedProjectTidyReceivesTheWrappedMoveTool(t *testing.T) {
	if tidyReclassifyTools(nil, nil) != nil {
		t.Fatal("a missing store advertised a move tool")
	}
	mem := memory.NewLimited(t.TempDir(), 1000, 200)
	const source = "Preserve every selected source record before replacing durable state."
	if _, _, err := mem.Add(source); err != nil {
		t.Fatal(err)
	}
	for name, tools := range map[string][]tool.BaseTool{
		"foreground": memory.Tools(mem, nil),
		"worker":     memory.ViewTools(mem),
		"library":    memory.CatalogTools(mem, nil),
	} {
		for _, tl := range tools {
			info, err := tl.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.Name == memory.ToolMemoryReclassify {
				t.Fatalf("%s received the project-tidy-only tool", name)
			}
		}
	}
	var changes []memory.Change
	tools := tidyReclassifyTools(mem, func(change memory.Change) { changes = append(changes, change) })
	if len(tools) != 1 {
		t.Fatalf("tidy move tools=%d", len(tools))
	}
	info, err := tools[0].Info(context.Background())
	if err != nil || info.Name != memory.ToolMemoryReclassify {
		t.Fatalf("wrapped schema=%+v err=%v", info, err)
	}
	args, _ := json.Marshal(map[string]string{
		"action": "promote", "old_text": source, "name": "source-retention",
		"description": "when replacing durable state", "content": source,
	})
	out, err := tools[0].(tool.InvokableTool).InvokableRun(context.Background(), string(args))
	if err != nil || !strings.Contains(out, `"success":true`) {
		t.Fatalf("the authorized move failed: %s %v", out, err)
	}
	snap, err := mem.Read()
	if err != nil || len(snap.Entries) != 0 || len(changes) != 1 || changes[0].Action != "promote" {
		t.Fatalf("the wrapped tool lost persistence or receipt: %+v changes=%+v err=%v", snap, changes, err)
	}
	if skill, err := mem.ReadSkill("source-retention"); err != nil || skill.Body != source {
		t.Fatalf("the wrapped move lost the original facts: %+v %v", skill, err)
	}
}
