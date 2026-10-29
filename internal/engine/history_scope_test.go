package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func scopedHistoryTool(t *testing.T, e *Engine, thread *store.Thread) *historyQueryTool {
	t.Helper()
	set, err := e.conversationTools(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range set.Tools {
		if reader, ok := base.(*historyQueryTool); ok {
			// Scope is enforced by the actual Reader; these isolated SQL tests
			// omit the execution-owner guard exercised at the HTTP boundary.
			reader.guard = nil
			return reader
		}
	}
	t.Fatal("history query is not mounted")
	return nil
}

func TestProjectToolBindsScopeBeforeTheModelChoosesItsSQL(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("Current project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.CreateProject("Other project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	current, err := e.CreateThread("Current", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.CreateThread("Foreign", "", foreign.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader := scopedHistoryTool(t, e, current)
	for _, request := range []map[string]any{
		{"sql": "SELECT id,project_id FROM threads ORDER BY id"},
		{"sql": "SELECT id,project_id FROM threads ORDER BY id", "params": []string{project.ID}},
		{"sql": "SELECT id,project_id FROM threads ORDER BY id", "project_id": foreign.ID},
	} {
		args, _ := json.Marshal(request)
		reply := historyToolReply(t, reader, context.Background(), string(args))
		rows := reply["rows"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["id"] != current.ID {
			t.Fatalf("actual tool returned another project: %#v", reply)
		}
	}
}

func TestMissingProjectToolDoesNotRestoreGlobalReads(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Current", "", "")
	if err != nil {
		t.Fatal(err)
	}
	thread.ProjectID = "missing-project"
	reply := historyToolReply(t, scopedHistoryTool(t, e, thread), context.Background(), `{"sql":"SELECT id FROM threads"}`)
	if reply["ok"] != false {
		t.Fatalf("missing project exposed global history: %#v", reply)
	}
}
