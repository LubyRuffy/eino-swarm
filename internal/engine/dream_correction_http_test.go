package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const (
	dreamFixtureSkill = "local-label-check"
	dreamFixtureBad   = "For a local label check, write unchecked to result.txt."
	dreamFixtureGood  = "For a local label check, copy the exact contents of input.txt to result.txt."
	dreamFixtureValue = "verified fixture label\n"
	dreamFixtureOther = "archive-index"
	dreamArchiveOld   = "Record the manifest name."
	dreamArchiveNew   = "Record the manifest name and checksum."
)

// A successful turn can finish while the project memory gate is held. After
// the explicit correction lands, the next conversation must receive the
// corrected procedure, act on it, and be unable to put the old text back.
func TestDreamCorrectionIsUsedAndProtectedInTheNextConversation(t *testing.T) {
	var managerCalls, reviewerCalls atomic.Int32
	e := newDreamFixtureEngine(t, &reviewerCalls, func(w http.ResponseWriter, request scheduleCompletionRequest) {
		answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
		switch step := managerCalls.Add(1); step {
		case 1:
			answer("The correction was submitted.")
		case 2:
			writeDreamFixtureTool(t, w, request, "view", memory.ToolSkillView, map[string]string{"name": dreamFixtureSkill})
		case 3:
			var result struct {
				Success bool   `json:"success"`
				Body    string `json:"body"`
			}
			readDreamFixtureToolResult(t, request, "view", &result)
			if !result.Success || result.Body != dreamFixtureGood {
				t.Errorf("the next model request did not receive the corrected procedure: %+v", result)
				answer("The procedure could not be verified.")
				return
			}
			writeDreamFixtureTool(t, w, request, "execute", "exec", map[string]string{"command": "cat input.txt > result.txt"})
		case 4:
			var result struct {
				ExitCode *int `json:"exit_code"`
			}
			readDreamFixtureToolResult(t, request, "execute", &result)
			if result.ExitCode == nil || *result.ExitCode != 0 {
				t.Errorf("the corrected procedure did not execute successfully: %+v", result)
			}
			writeDreamFixtureTool(t, w, request, "recontaminate", memory.ToolSkillManage, map[string]string{
				"action": "patch", "name": dreamFixtureSkill, "old_text": dreamFixtureGood, "new_text": dreamFixtureBad,
			})
		case 5:
			var result struct {
				Success *bool  `json:"success"`
				Error   string `json:"error"`
			}
			readDreamFixtureToolResult(t, request, "recontaminate", &result)
			if result.Success == nil || *result.Success || !strings.Contains(result.Error, "protected by an accepted correction") {
				t.Errorf("the next model request did not observe protection of the corrected skill: %+v", result)
			}
			writeDreamFixtureTool(t, w, request, "ordinary-patch", memory.ToolSkillManage, map[string]string{
				"action": "patch", "name": dreamFixtureOther, "old_text": dreamArchiveOld, "new_text": dreamArchiveNew,
			})
		case 6:
			var result struct {
				Success bool `json:"success"`
			}
			readDreamFixtureToolResult(t, request, "ordinary-patch", &result)
			if !result.Success {
				t.Errorf("protection of the corrected skill blocked an ordinary unrelated patch: %+v", result)
			}
			answer("The label check finished.")
		default:
			t.Errorf("unexpected manager call %d", step)
			answer("Unexpected fixture request.")
		}
	})
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte(dreamFixtureValue), 0o600); err != nil {
		t.Fatal(err)
	}
	project, mem := seedDreamFixtureSkill(t, e, workspace)
	if _, err := mem.WriteSkill(dreamFixtureOther, "Indexes source archives.", dreamArchiveOld); err != nil {
		t.Fatal(err)
	}
	before, err := mem.ReadSkill(dreamFixtureSkill)
	if err != nil {
		t.Fatal(err)
	}
	release := holdDreamFixtureGate(t, e, project.ID)
	text := dreamFixtureCommand(t, dreamFixtureBad, dreamFixtureGood)
	turn := startDreamFixtureTurn(t, e, project.ID, text)
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("the foreground did not finish while memory was gated: %+v", got)
	}
	assertDreamFixtureBody(t, mem, dreamFixtureBad)
	release()
	review := waitForReview(t, e, turn.ID)
	assertDreamFixtureBody(t, mem, dreamFixtureGood)
	var outcome struct {
		Changed bool            `json:"changed"`
		Changes []memory.Change `json:"changes"`
		Dream   struct {
			Status    string `json:"status"`
			SourceSeq int64  `json:"source_seq"`
			SkillName string `json:"skill_name"`
		} `json:"dream"`
	}
	if err := json.Unmarshal([]byte(review.Text), &outcome); err != nil {
		t.Fatal(err)
	}
	if !outcome.Changed || outcome.Dream.Status != "applied" || outcome.Dream.SkillName != dreamFixtureSkill || outcome.Dream.SourceSeq <= 0 || len(outcome.Changes) != 1 {
		t.Fatalf("the durable review lacks the applied correction: %s", review.Text)
	}
	change := outcome.Changes[0]
	if change.Target != memory.ToolSkillManage || change.Action != "patch" || change.Name != dreamFixtureSkill || change.Before == nil || *change.Before != dreamFixtureBad || change.After == nil || *change.After != dreamFixtureGood {
		t.Fatalf("incorrect durable change receipt: %+v", change)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var originalSource, foregroundDone bool
	for _, event := range events {
		originalSource = originalSource || event.Seq == outcome.Dream.SourceSeq && event.Kind == KindUser && event.Text == text
		foregroundDone = foregroundDone || event.Kind == "done" && event.Seq < review.Seq
	}
	if !originalSource || !foregroundDone || reviewerCalls.Load() != 0 {
		t.Fatalf("missing original source or background ordering, or correction called the model reviewer: source=%t done=%t reviewer=%d", originalSource, foregroundDone, reviewerCalls.Load())
	}

	next := startDreamFixtureTurn(t, e, project.ID, "Run the recorded local label check and update the archive convention to include checksums.")
	if got := waitForTurn(t, e, next.ID); got.Status != store.TurnDone || got.Final != "The label check finished." {
		t.Fatalf("the next independent conversation did not complete the check: %+v", got)
	}
	waitForReview(t, e, next.ID)
	data, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(data) != dreamFixtureValue {
		t.Fatalf("the actual tool result was not delivered: data=%q err=%v", data, err)
	}
	assertDreamFixtureBody(t, mem, dreamFixtureGood)
	other, err := mem.ReadSkill(dreamFixtureOther)
	if err != nil || other.Body != dreamArchiveNew {
		t.Fatalf("ordinary archive update did not land: %+v, %v", other, err)
	}
	after, err := mem.ReadSkill(dreamFixtureSkill)
	if err != nil || after.Description != before.Description || after.Views != before.Views+1 {
		t.Fatalf("correction did not preserve the description and ordinary skill use: before=%+v after=%+v err=%v", before, after, err)
	}
}

func newDreamFixtureEngine(t *testing.T, reviewerCalls *atomic.Int32, manager func(http.ResponseWriter, scheduleCompletionRequest)) *Engine {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) == 0 {
			t.Error("unexpected local model request")
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if request.Messages[0].Content == predictionPrompt() {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Inspect the retained record."}, "stop")
			return
		}
		for _, tool := range request.Tools {
			if tool.Function.Name == memory.ToolApplyReview {
				reviewerCalls.Add(1)
				writeDreamFixtureTool(t, w, request, "review-noop", memory.ToolApplyReview, map[string]any{"changes": []any{}})
				return
			}
		}
		manager(w, request)
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Swarm.ComposerPredictions = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-dream-correction"
	e.pool = provider.New(e.cfg)
	return e
}

func writeDreamFixtureTool(t *testing.T, w http.ResponseWriter, request scheduleCompletionRequest, id, name string, args any) {
	t.Helper()
	var offered bool
	for _, tool := range request.Tools {
		offered = offered || tool.Function.Name == name
	}
	if !offered {
		t.Errorf("the actual model request does not offer %s", name)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Errorf("encode fixture tool call: %v", err)
		return
	}
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)},
	}}}, "tool_calls")
}

func readDreamFixtureToolResult(t *testing.T, request scheduleCompletionRequest, id string, result any) {
	t.Helper()
	for _, message := range request.Messages {
		if message.Role == schema.Tool && message.ToolCallID == id {
			if err := json.Unmarshal([]byte(message.Content), result); err != nil {
				t.Errorf("decode actual %s tool receipt: %v", id, err)
			}
			return
		}
	}
	t.Errorf("actual model request did not contain tool receipt %s", id)
}

func seedDreamFixtureSkill(t *testing.T, e *Engine, workspace string) (*store.Project, *memory.Store) {
	t.Helper()
	project, err := e.CreateProject("Local label fixture", "", workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	mem := e.ProjectMemory(project.ID)
	if _, err := mem.WriteSkill(dreamFixtureSkill, "Checks a local fixture label.", dreamFixtureBad); err != nil {
		t.Fatal(err)
	}
	return project, mem
}

func holdDreamFixtureGate(t *testing.T, e *Engine, projectID string) func() {
	t.Helper()
	gate := e.reviews.begin(projectID)
	if gate == nil {
		t.Fatal("memory gate is stopped")
	}
	gate <- struct{}{}
	var once sync.Once
	release := func() { once.Do(func() { <-gate; e.reviews.done() }) }
	t.Cleanup(release)
	return release
}

func dreamFixtureCommand(t *testing.T, before, after string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": dreamFixtureSkill, "before": before, "after": after})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("```memory-correction\n%s\n```", body)
}

func startDreamFixtureTurn(t *testing.T, e *Engine, projectID, text string) *store.Turn {
	t.Helper()
	thread, err := e.CreateThread("Local correction test", "", projectID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, text)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func assertDreamFixtureBody(t *testing.T, mem *memory.Store, want string) {
	t.Helper()
	skill, err := mem.ReadSkill(dreamFixtureSkill)
	if err != nil || skill.Body != want {
		t.Fatalf("stored procedure body = %q, want %q; read error = %v", skill.Body, want, err)
	}
}
