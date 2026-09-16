package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func waitDone(t *testing.T, h *Handle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
}

func TestRestoreKeepsTheGivenIDAndRuns(t *testing.T) {
	reg := NewRegistry()
	var seenID string
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		seenID = agentID
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("continued", nil)
			},
		}}
	}
	h, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "worker-7", Role: "worker", Instruction: "do the assigned work",
		Task: "continue from the workspace",
	}, reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "worker-7" {
		t.Fatalf("id=%q, want the id we restored", h.ID)
	}
	waitDone(t, h)
	if seenID != "worker-7" {
		t.Fatalf("model built for %q, want worker-7", seenID)
	}
	result, err, done := h.Result()
	if !done || err != nil || result != "continued" {
		t.Fatalf("result=%q err=%v done=%v", result, err, done)
	}
}

func TestRestoreBumpsSeqSoTheNextSpawnDoesNotCollide(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("ok")
	if _, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "worker-4", Role: "worker", Task: "continue",
	}, reg.ModelBuilder); err != nil {
		t.Fatal(err)
	}
	h, err := reg.Spawn(context.Background(), "worker", "next", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "worker-5" {
		t.Fatalf("next spawn id=%q, would collide with the restored worker", h.ID)
	}
	waitDone(t, h)
}

func TestRestoreRejectsARunningID(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	live, err := reg.Spawn(context.Background(), "slow", "long", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.Cancel)
	_, err = reg.Restore(context.Background(), RestoredWorker{
		ID: live.ID, Role: live.Role, Task: "again",
	}, reg.ModelBuilder)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("restore of a live worker should fail, got %v", err)
	}
}

func TestRestoreRejectsAClosedRegistry(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	reg.Close()
	_, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "worker-1", Role: "worker", Task: "continue",
	}, reg.ModelBuilder)
	if err == nil {
		t.Fatal("restore after Close must fail")
	}
}

func TestPlantFinishedIsVisibleToWaitAgents(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	if err := reg.PlantFinished(FinishedWorker{
		ID: "worker-1", Role: "worker", Result: "already done",
	}); err != nil {
		t.Fatal(err)
	}
	waitT := invokable(t, reg.Tools()[2])
	out, err := waitT.InvokableRun(context.Background(), `{"agent_ids":["worker-1"],"timeout_s":1}`)
	if err != nil {
		t.Fatal(err)
	}
	var report waitReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Agents) != 1 || report.Agents[0].Status != "done" || report.Agents[0].Result != "already done" {
		t.Fatalf("planted worker missing from wait_agents: %s", out)
	}
}

func TestRunWithRestoresWorkersAfterHooksSoSpawnedFires(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(turn int, msgs []*schema.Message) *schema.Message {
					ids, _ := json.Marshal(map[string]any{"agent_ids": []string{"worker-1"}, "timeout_s": 5})
					return schema.AssistantMessage("waiting", []schema.ToolCall{
						rawCall("w", "wait_agents", string(ids)),
					})
				},
				func(turn int, msgs []*schema.Message) *schema.Message {
					return schema.AssistantMessage("collected", nil)
				},
			}}
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("worker done", nil)
			},
		}}
	}
	var spawned []string
	_, err := reg.RunWith(context.Background(), RunConfig{
		Instruction: "manage",
		Messages:    []adk.Message{schema.UserMessage("continue")},
		RestoreWorkers: []RestoredWorker{{
			ID: "worker-1", Role: "worker", Task: "continue",
		}},
	}, func(n Notification) {
		if n.Kind == NotifySpawned {
			spawned = append(spawned, n.AgentID)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 1 || spawned[0] != "worker-1" {
		t.Fatalf("restored worker never appeared on the roster: %v", spawned)
	}
}

func TestRestoreAndPlantFinishedRejectBadInput(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")

	for _, w := range []RestoredWorker{
		{Role: "worker", Task: "continue"},
		{ID: "worker-1", Task: "continue"},
		{ID: "worker-1", Role: "worker"},
	} {
		if _, err := reg.Restore(context.Background(), w, nil); err == nil {
			t.Fatalf("restore accepted %+v", w)
		}
	}
	if err := reg.PlantFinished(FinishedWorker{}); err == nil {
		t.Fatal("plant with no id must fail")
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "worker-1"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "worker-1", Result: "again"}); err != nil {
		t.Fatal(err)
	}
	revived, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "worker-1", Role: "worker", Task: "continue",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, revived)

	closed := NewRegistry()
	closed.ModelBuilder = oneShot("x")
	closed.Close()
	if err := closed.PlantFinished(FinishedWorker{ID: "worker-1"}); err == nil {
		t.Fatal("plant after Close must fail")
	}
	if _, err := closed.Restore(context.Background(), RestoredWorker{
		ID: "worker-2", Role: "worker", Task: "continue",
	}, nil); err == nil {
		t.Fatal("restore after Close must fail")
	}

	live, err := reg.Spawn(context.Background(), "slow", "long", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.Cancel)
	if err := reg.PlantFinished(FinishedWorker{ID: live.ID}); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("plant of a live worker should fail, got %v", err)
	}

	done, err := reg.Spawn(context.Background(), "done", "quick", oneShot("done"))
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, done)
	h, err := reg.Restore(context.Background(), RestoredWorker{
		ID: done.ID, Role: done.Role, Task: "continue",
	}, oneShot("again"))
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h)

	if trailingSeq("plain") != 0 || trailingSeq("worker-x") != 0 || trailingSeq("worker-") != 0 {
		t.Fatal("trailingSeq should ignore ids without a numeric suffix")
	}
}

func TestRestorePushesSeedBeforeFirstModelCall(t *testing.T) {
	const marker = "conversation-so-far-marker"
	var seen string
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if strings.Contains(m.Content, marker) {
						seen = m.Content
					}
				}
				return schema.AssistantMessage("continued", nil)
			},
		}}
	}
	h, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "worker-1", Role: "worker", Task: "continue", Seed: marker,
	}, reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, h)
	if !strings.Contains(seen, marker) {
		t.Fatalf("first model call missed the restored conversation: %q", seen)
	}
}

func TestApplyRunWorkersSurfacesRestoreErrors(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	if err := reg.applyRunWorkers(context.Background(), RunConfig{
		FinishedWorkers: []FinishedWorker{{}},
	}); err == nil {
		t.Fatal("empty planted id must fail the run")
	}
	if err := reg.applyRunWorkers(context.Background(), RunConfig{
		RestoreWorkers: []RestoredWorker{{ID: "worker-1", Role: "worker"}},
	}); err == nil {
		t.Fatal("restore without a task must fail the run")
	}
}

func TestRunWithFailsWhenRestoreFails(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	_, err := reg.RunWith(context.Background(), RunConfig{
		Instruction: "manage",
		Messages:    []adk.Message{schema.UserMessage("continue")},
		RestoreWorkers: []RestoredWorker{{
			ID: "worker-1", Role: "worker",
		}},
	}, nil)
	if err == nil {
		t.Fatal("a broken restore must fail the run, not start the manager anyway")
	}
}
