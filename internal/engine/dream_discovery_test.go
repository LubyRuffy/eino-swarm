package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"gorm.io/gorm"
)

func TestDreamDiscoveryBindsViewedSkillToPriorReviewAndActualObservation(t *testing.T) {
	e := newTestEngine(t)
	project, originalThread := projectThread(t, e)
	triggerThread, err := e.CreateThread("Independent verification", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	mem := e.ProjectMemory(project.ID)
	const name, description = "check-generated-artifact", "Verify generated files"
	if _, err := mem.WriteSkill(name, description, "Verify the result from the recorded manifest."); err != nil {
		t.Fatal(err)
	}
	original := dreamDiscoveryTurn(t, e, project.ID, originalThread.ID)
	originalCall, originalResult := dreamDiscoveryExec(t, e, original, "original", "check manifest", "result unavailable")
	dreamDiscoveryFinish(t, e, original)
	var change memory.Change
	memoryTools := memory.Tools(mem, func(c memory.Change) { change = c })
	if result, err := memoryTools[2].(tool.InvokableTool).InvokableRun(t.Context(), `{"action":"patch","name":"check-generated-artifact","old_text":"recorded manifest","new_text":"initial status"}`); err != nil || !strings.Contains(result, `"success":true`) {
		t.Fatalf("actual patch did not run: %s, %v", result, err)
	}
	review := reviewOutcome{
		ProjectID: project.ID, Changed: true, StopReason: "completed", Changes: []memory.Change{{Target: memory.ToolMemory, Action: "add"}, change},
		Evidence: []reviewEvidence{{Target: memory.ToolMemory, Action: "add"}, {Target: memory.ToolSkillManage, Action: "patch", Name: name, Sources: []int64{originalResult.Seq}}},
	}
	e.recordReview(original.ThreadID, original.ID, review)
	e.recordReview(original.ThreadID, original.ID, reviewOutcome{ProjectID: project.ID, Dream: &dreamReceipt{Skill: name, Status: "uncertain"}})
	trigger := dreamDiscoveryTurn(t, e, project.ID, triggerThread.ID)
	view, err := memoryTools[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"check-generated-artifact"}`)
	if err != nil {
		t.Fatal(err)
	}
	readCall, readResult := dreamDiscoveryPair(t, e, trigger, "read", "skill_view", `{"name":"check-generated-artifact"}`, view)
	laterCall, laterResult := dreamDiscoveryExec(t, e, trigger, "later", "verify artifact", "complete receipt")
	dreamDiscoveryExec(t, e, trigger, "verify", "inspect receipt", "verified bytes")
	dreamDiscoveryFinish(t, e, trigger)
	events, err := e.Store().ListTurnEvents(trigger.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := e.discoverDream(t.Context(), trigger, project.ID, mem, events)
	if err != nil || candidate == nil {
		t.Fatalf("owned paired observations did not identify a candidate: %+v, %v", candidate, err)
	}
	if candidate.Patch.ThreadID != original.ThreadID || candidate.Patch.TurnID != original.ID || candidate.Patch.ID == 0 || candidate.Change.Name != name ||
		candidate.Read.Call.ID != readCall.ID || candidate.Read.Result.ID != readResult.ID || len(candidate.Original) != 1 || len(candidate.Sources) != 2 || len(candidate.Attempts) != 1 ||
		candidate.Original[0].Call.ID != originalCall.ID || candidate.Original[0].Result.ID != originalResult.ID ||
		candidate.Sources[0].Call.ID != laterCall.ID || candidate.Sources[0].Result.ID != laterResult.ID {
		t.Fatalf("incorrect durable evidence binding: %+v", candidate)
	}

	var historyQueries atomic.Int32
	if err := e.Store().DB().Callback().Row().Before("gorm:row").Register("dream_capture_history_queries", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "idx_evt_thread_kind_seq") {
			historyQueries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, events); got == nil || err != nil || historyQueries.Swap(0) == 0 {
		t.Fatalf("history query observation missing for eligible trigger: %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*store.Event)
	}{
		{"malformed native call", func(ev *store.Event) {
			if ev.ID == laterCall.ID {
				ev.Text = "exec"
			}
		}},
		{"unknown tool", func(ev *store.Event) {
			if ev.ID == laterCall.ID {
				ev.Text = "mcp_external({})"
			}
		}},
		{"wrong process", func(ev *store.Event) {
			if ev.ID == laterCall.ID {
				ev.Text = `exec({"command":"other command"})`
			}
		}},
		{"stdin is private", func(ev *store.Event) {
			if ev.ID == laterCall.ID {
				ev.Text = `exec({"command":"verify artifact","stdin":"private"})`
			}
		}},
		{"environment is private", func(ev *store.Event) {
			if ev.ID == laterCall.ID {
				ev.Text = `exec({"command":"verify artifact","env":{"TOKEN":"private"}})`
			}
		}},
		{"truncated output", func(ev *store.Event) {
			if ev.ID == laterResult.ID {
				ev.Text = strings.Replace(ev.Text, `"truncated":false`, `"truncated":true`, 1)
			}
		}},
		{"failed transport", func(ev *store.Event) {
			if ev.ID == laterResult.ID {
				ev.Err = "transport failed"
			}
		}},
		{"forwarded result", func(ev *store.Event) {
			if ev.ID == laterResult.ID {
				ev.SourceThreadID = original.ThreadID
			}
		}},
		{"unpaired reused ID", func(ev *store.Event) {
			if ev.ID == laterResult.ID {
				ev.ToolReceiptScope = "different process"
			}
		}},
		{"other worker generation", func(ev *store.Event) {
			if ev.ID == laterResult.ID || ev.ID == laterCall.ID {
				ev.WorkerGeneration = 2
			}
		}},
		{"mismatched skill view", func(ev *store.Event) {
			if ev.ID == readCall.ID {
				ev.Text = `skill_view({"name":"different-skill"})`
			}
		}},
		{"old skill body", func(ev *store.Event) {
			if ev.ID == readResult.ID {
				ev.Text = strings.Replace(ev.Text, "initial status", "recorded manifest", 1)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modified := slices.Clone(events)
			for i := range modified {
				tc.mutate(&modified[i])
			}
			if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, modified); got != nil || err != nil {
				t.Fatalf("ineligible observation created a candidate: %+v, %v", got, err)
			}
		})
	}
	if got, err := e.discoverDream(t.Context(), nil, project.ID, mem, events); got != nil || err != nil {
		t.Fatalf("missing turn entered discovery: %+v, %v", got, err)
	}
	duplicate := append(slices.Clone(events), laterResult)
	if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, duplicate); got != nil || err != nil {
		t.Fatalf("duplicate result created a candidate: %+v, %v", got, err)
	}
	if calls := historyQueries.Load(); calls != 0 {
		t.Fatalf("ineligible trigger queried history %d times", calls)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := e.discoverDream(ctx, trigger, project.ID, mem, events); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled discovery did not propagate cancellation: %+v, %v", got, err)
	}
	if got, err := e.discoverDream(t.Context(), trigger, project.ID, memory.New(t.TempDir(), 2200), events); got != nil || err != nil {
		t.Fatalf("removed skill was selected: %+v, %v", got, err)
	}

	patch := candidate.Patch
	updateReview := func(out reviewOutcome) {
		t.Helper()
		body, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Store().DB().Model(&store.Event{}).Where("id=?", patch.ID).Update("text", string(body)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*reviewOutcome)
	}{
		{"unmarked original project", func(out *reviewOutcome) { out.ProjectID = "" }},
		{"other original project", func(out *reviewOutcome) { out.ProjectID = "unrelated" }},
		{"shared library write", func(out *reviewOutcome) { out.Library = true }},
		{"partially committed review", func(out *reviewOutcome) { out.StopReason = "error"; out.Err = "some changes failed" }},
		{"no complete before image", func(out *reviewOutcome) { out.Changes[1].Before = nil }},
		{"duplicate matching patches", func(out *reviewOutcome) { out.Changes = append(out.Changes, out.Changes[1]) }},
		{"no complete after image", func(out *reviewOutcome) { out.Changes[1].After = nil }},
		{"citation was not a result", func(out *reviewOutcome) { out.Evidence[1].Sources = []int64{originalCall.Seq} }},
		{"citation absent", func(out *reviewOutcome) { out.Evidence[1].Sources = nil }},
		{"citation is duplicated", func(out *reviewOutcome) { out.Evidence[1].Sources = []int64{originalResult.Seq, originalResult.Seq} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modified := review
			modified.Changes = slices.Clone(review.Changes)
			modified.Evidence = slices.Clone(review.Evidence)
			tc.mutate(&modified)
			updateReview(modified)
			if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, events); got != nil || err != nil {
				t.Fatalf("ineligible historical review created a candidate: %+v, %v", got, err)
			}
		})
	}
	updateReview(review)
	duplicateReview := patch
	duplicateReview.ID = 0
	duplicateReview.Seq = 0
	// Preserve the actual time order for an earlier duplicate patch receipt.
	if err := e.Store().AppendEvent(&duplicateReview); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().DB().Model(&store.Event{}).Where("id=?", duplicateReview.ID).Updates(map[string]any{"created_at": patch.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	// The new row's ID is after the view even when the clock is set earlier,
	// so it cannot become an eligible second patch for this trigger.
	if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, events); got == nil || err != nil {
		t.Fatalf("post-view review replaced the authentic earlier candidate: %+v, %v", got, err)
	}
	if err := e.DeleteThread(original.ThreadID); err != nil {
		t.Fatal(err)
	}
	if got, err := e.discoverDream(t.Context(), trigger, project.ID, mem, events); got != nil || err != nil {
		t.Fatalf("removed source was still discovered: %+v, %v", got, err)
	}
}

func dreamDiscoveryTurn(t *testing.T, e *Engine, projectID, threadID string) *store.Turn {
	t.Helper()
	turn := &store.Turn{ProjectID: projectID, ThreadID: threadID, ProviderID: "test-route"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	return turn
}

func dreamDiscoveryFinish(t *testing.T, e *Engine, turn *store.Turn) {
	t.Helper()
	if err := e.Store().FinishTurn(turn.ID, store.TurnDone, "finished", ""); err != nil {
		t.Fatal(err)
	}
	turn.Status = store.TurnDone
}

func dreamDiscoveryExec(t *testing.T, e *Engine, turn *store.Turn, id, command, output string) (store.Event, store.Event) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"command": command})
	result, _ := json.Marshal(map[string]any{"full_command": command, "workdir": "/workspace", "exit_code": 0, "failed": false, "truncated": false, "stdout": output, "stderr": ""})
	return dreamDiscoveryPair(t, e, turn, id, "exec", string(args), string(result))
}

func dreamDiscoveryPair(t *testing.T, e *Engine, turn *store.Turn, id, name, args, output string) (store.Event, store.Event) {
	t.Helper()
	appendEvent := func(kind, text string) store.Event {
		ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: "manager", ToolCallID: id, Kind: kind, Text: text}
		if err := e.Store().AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	call := appendEvent("tool_call", name+"("+args+")")
	return call, appendEvent("tool_result", output)
}
