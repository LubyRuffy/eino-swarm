package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

func TestProjectDescriptionToolRequiresAnExplicitDescription(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("Instructions", "Keep this policy", "", false)
	if err != nil {
		t.Fatal(err)
	}
	thread := &store.Thread{Title: "Update instructions", ProjectID: project.ID}
	if err := e.store.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	writer := e.projectDescriptionTool(project, turn).(tool.InvokableTool)
	ctx := context.Background()
	for _, args := range []string{`{}`, `{"description":null}`, `{"description":3}`, `{"description":"partial"`} {
		out, err := writer.InvokableRun(ctx, args)
		if err != nil || !strings.Contains(out, `"ok":false`) {
			t.Fatalf("malformed input %q: %s %v", args, out, err)
		}
	}
	got, err := e.GetProject(project.ID)
	if err != nil || got.SystemPrompt != project.SystemPrompt {
		t.Fatalf("invalid inputs changed project: %+v %v", got, err)
	}

	out, err := writer.InvokableRun(ctx, `{"description":""}`)
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("explicit clear: %s %v", out, err)
	}
	got, err = e.GetProject(project.ID)
	if err != nil || got.SystemPrompt != "" {
		t.Fatalf("clear did not persist: %+v %v", got, err)
	}
	out, err = writer.InvokableRun(ctx, `{"description":"Overwrite from the stale snapshot"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("stale overwrite: %s %v", out, err)
	}
}
