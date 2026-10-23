package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestTraceFullRetainsFailedMemoryChangesAndStaleCandidates(t *testing.T) {
	const storageError = "the source directory could not be removed"
	payload := map[string]any{
		"changed": true,
		"notes":   map[string]int{"add": 1},
		"changes": []memory.Change{
			{Target: memory.ToolMemory, Action: "add", Text: "The existing source identity remains authoritative."},
			{Target: memory.ToolSkillManage, Action: "delete", Name: "retained-constraint"},
		},
		"stale_candidates": []string{"unviewed-procedure"},
		"err":              storageError,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	dir, turnID := storeTraceMemoryEvent(t, string(raw), storageError)
	out := captureStdout(t, func() {
		if err := runTrace([]string{turnID, "--full", "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{engine.KindMemoryReview, "error: " + storageError, string(raw), `"action":"add"`, `"action":"delete"`, `"stale_candidates":["unviewed-procedure"]`} {
		if !strings.Contains(out, want) {
			t.Fatalf("full trace lost failed memory diagnostics %q:\n%s", want, out)
		}
	}
}

func TestTraceFullShowsCompletedPromotionAndDemotionActions(t *testing.T) {
	for _, action := range []string{"promote", "demote"} {
		t.Run(action, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"changed": true,
				"skills":  []memory.Change{{Target: memory.ToolSkillManage, Action: action, Name: "recorded-constraint"}},
				"changes": []memory.Change{{Target: memory.ToolSkillManage, Action: action, Name: "recorded-constraint", Text: "The existing source identity remains authoritative."}},
			})
			if err != nil {
				t.Fatal(err)
			}
			dir, turnID := storeTraceMemoryEvent(t, string(raw), "")
			out := captureStdout(t, func() {
				if err := runTrace([]string{turnID, "--full", "--data-dir", dir}); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(out, string(raw)) || !strings.Contains(out, `"action":"`+action+`"`) {
				t.Fatalf("trace lost the completed %s receipt:\n%s", action, out)
			}
		})
	}
}

func TestMemoryTraceRetainsTheErrorWhenNoDiagnosticBodyExists(t *testing.T) {
	for _, text := range []string{"", " \n "} {
		if got := eventText(store.Event{Kind: engine.KindMemoryReview, Text: text, Err: "storage failed"}); got != "error: storage failed" {
			t.Fatalf("empty memory diagnostics changed error output: %q", got)
		}
	}
	if got := eventText(store.Event{Kind: "error", Text: "existing detail", Err: "storage failed"}); got != "error: storage failed" {
		t.Fatalf("unrelated event formatting changed: %q", got)
	}
}

func storeTraceMemoryEvent(t *testing.T, payload, cause string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	thread := &store.Thread{Title: "Recorded memory diagnostics"}
	if err := st.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, UserText: "Verify the recorded memory"}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTurn(turn.ID, store.TurnDone, "The request finished.", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(&store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: engine.KindMemoryReview, AgentID: engine.ReviewAgentID, Text: payload, Err: cause}); err != nil {
		t.Fatal(err)
	}
	return dir, turn.ID
}
