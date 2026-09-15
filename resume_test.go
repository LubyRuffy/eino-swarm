package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const priorContextMarker = "prior-context-marker"

func oneShot(answer string) ModelBuilder {
	return func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage(answer, nil)
			},
		}}
	}
}

// fork_context used to push inbox after the goroutine started, so the first
// model call could run with an empty seed. Seeding before go func() makes
// this deterministic rather than lucky.
func TestForkContextSeedsBeforeFirstModelCall(t *testing.T) {
	const marker = "manager-context-marker"
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
				return schema.AssistantMessage("FORK-OK", nil)
			},
		}}
	}
	h, err := reg.SpawnForked(context.Background(), "w", "do",
		reg.ModelBuilder, []adk.Message{schema.UserMessage(marker)})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	if !strings.Contains(seen, marker) {
		t.Fatalf("first model call missed inherited context: %q", seen)
	}
}

// send_message can queue after the last model call has already started; the
// worker then finishes without another turn. wait_agents has to admit the
// text never landed, or the manager thinks it steered someone who is gone.
func TestUndeliveredSteerSurfacesWhenTheAgentFinishes(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	var startOnce sync.Once
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				startOnce.Do(func() { close(started) })
				<-block
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "w", "do", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model call never started")
	}
	sendT := invokable(t, reg.Tools()[1])
	out, err := sendT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"text":"late-steer-text"}`, h.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"delivered":true`) {
		t.Fatalf("queued steer should report delivered (queued), got %s", out)
	}
	close(block)
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	waitT := invokable(t, reg.Tools()[2])
	out, err = waitT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":1}`, h.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "late-steer-text") {
		t.Fatalf("wait_agents hid leftover steering: %s", out)
	}
}

func TestResumeSeesFinishedWorkersHistory(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot(priorContextMarker)
	h, err := reg.Spawn(context.Background(), "w", "first task", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}

	var seen string
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if strings.Contains(m.Content, priorContextMarker) {
						seen = m.Content
					}
				}
				return schema.AssistantMessage("RESUMED-OK", nil)
			},
		}}
	}
	resumeT := invokable(t, reg.Tools()[4])
	out, err := resumeT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"task":"continue"}`, h.ID))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		AgentID     string `json:"agent_id"`
		ResumedFrom string `json:"resumed_from"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("resume json: %v (%s)", err, out)
	}
	if resp.ResumedFrom != h.ID || resp.AgentID != h.ID {
		t.Fatalf("resume should keep the same id: %s", out)
	}
	nh, ok := reg.get(resp.AgentID)
	if !ok {
		t.Fatal("resumed worker missing from registry")
	}
	select {
	case <-nh.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("resumed worker did not finish")
	}
	if !strings.Contains(seen, priorContextMarker) {
		t.Fatalf("resumed worker missed prior history: %q", seen)
	}
}

func TestResumeAfterFailureKeepsTheSameID(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("first")
	h, err := reg.Spawn(context.Background(), "w", "first", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled worker did not finish")
	}

	var sawID string
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		sawID = agentID
		return oneShot("second")(role, agentID)
	}
	nh, err := reg.Resume(context.Background(), h.ID, "retry", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	if nh.ID != h.ID {
		t.Fatalf("failed worker grew a twin: got %s want %s", nh.ID, h.ID)
	}
	select {
	case <-nh.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("resumed worker did not finish")
	}
	if sawID != h.ID {
		t.Fatalf("second run used a different id: %s", sawID)
	}
	if _, _, finished := h.Result(); !finished {
		t.Fatal("same handle should look finished after the second run")
	}
}

func TestResumeWhileRunningRejected(t *testing.T) {
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
	h, err := reg.Spawn(context.Background(), "slow", "long", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Cancel)
	resumeT := invokable(t, reg.Tools()[4])
	_, err = resumeT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"task":"more"}`, h.ID))
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("resume of a running worker should fail, got %v", err)
	}
}

func TestResumeUnknownRejected(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	resumeT := invokable(t, reg.Tools()[4])
	_, err := resumeT.InvokableRun(context.Background(), `{"agent_id":"ghost","task":"more"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("resume of a missing worker should fail, got %v", err)
	}
}

func TestResumeStillWorksAfterStatsPrune(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot(priorContextMarker)
	h, err := reg.Spawn(context.Background(), "w", "first", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	if _, finished := reg.Stats(); finished == 0 {
		t.Fatal("Stats should prune the finished handle")
	}
	if _, ok := reg.get(h.ID); ok {
		t.Fatal("Stats left the finished handle in the live map")
	}

	var seen string
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if strings.Contains(m.Content, priorContextMarker) {
						seen = m.Content
					}
				}
				return schema.AssistantMessage("AFTER-STATS", nil)
			},
		}}
	}
	nh, err := reg.Resume(context.Background(), h.ID, "continue", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-nh.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("resumed worker did not finish")
	}
	if !strings.Contains(seen, priorContextMarker) {
		t.Fatalf("resume after Stats missed history: %q", seen)
	}
	if nh.ID != h.ID {
		t.Fatalf("resume after prune must keep the id, got %s want %s", nh.ID, h.ID)
	}
}

func TestResumeRejectedAfterClose(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot(priorContextMarker)
	h, err := reg.Spawn(context.Background(), "w", "first", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	reg.Close()
	_, err = reg.Resume(context.Background(), h.ID, "continue", reg.ModelBuilder)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("resume after Close should fail, got %v", err)
	}
}

func TestResumeRequiresIDandTask(t *testing.T) {
	reg := NewRegistry()
	resumeT := invokable(t, reg.Tools()[4])
	if _, err := resumeT.InvokableRun(context.Background(), `{"task":"x"}`); err == nil {
		t.Fatal("missing agent_id should fail")
	}
	if _, err := resumeT.InvokableRun(context.Background(), `{"agent_id":"w-1"}`); err == nil {
		t.Fatal("missing task should fail")
	}
}

func TestFormatContextSkipsEmptyContent(t *testing.T) {
	got := formatContext("prefix", []adk.Message{
		schema.UserMessage(""),
		schema.AssistantMessage("kept", nil),
		nil,
	})
	if !strings.Contains(got, "kept") {
		t.Fatalf("expected kept content, got %q", got)
	}
	if strings.Contains(got, "user:") {
		t.Fatalf("empty user content leaked: %q", got)
	}
}

func TestArchiveEvictsOldestFinishedWorkers(t *testing.T) {
	reg := NewRegistry()
	reg.ArchiveLimit = 2
	reg.ModelBuilder = oneShot("x")
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		h, err := reg.Spawn(context.Background(), "w", "t", reg.ModelBuilder)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-h.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not finish")
		}
		ids[i] = h.ID
	}
	reg.Stats()
	if _, err := reg.Resume(context.Background(), ids[0], "continue", reg.ModelBuilder); err == nil {
		t.Fatal("the oldest archived worker should have been evicted")
	}
	h, err := reg.Resume(context.Background(), ids[2], "continue", oneShot("y"))
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
}

func TestHistoryTrimKeepsTheTail(t *testing.T) {
	msgs := make([]adk.Message, 0, 5)
	for i := 0; i < 5; i++ {
		msgs = append(msgs, schema.UserMessage(fmt.Sprintf("m%d", i)))
	}
	got := trimHistory(msgs, 2)
	if len(got) != 2 || got[0].Content != "m3" || got[1].Content != "m4" {
		t.Fatalf("trim should keep the tail, got %+v", got)
	}
	if n := len(trimHistory(msgs, 0)); n != 5 {
		t.Fatalf("limit 0 should fall back to the default cap, got %d", n)
	}
}

func TestHistoryCapUsesRegistryOverride(t *testing.T) {
	reg := NewRegistry()
	reg.HistoryLimit = 3
	if reg.historyCap() != 3 {
		t.Fatalf("HistoryLimit should win, got %d", reg.historyCap())
	}
	h := &Handle{}
	h.setHistory([]adk.Message{
		schema.UserMessage("a"),
		schema.UserMessage("b"),
		schema.UserMessage("c"),
		schema.UserMessage("d"),
	}, reg.historyCap())
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.history) != 3 || h.history[0].Content != "b" || h.history[2].Content != "d" {
		t.Fatalf("custom cap should keep the tail, got %+v", h.history)
	}
}

func TestClonePastNilAndToolCalls(t *testing.T) {
	if clonePast(nil) != nil {
		t.Fatal("nil past should stay nil")
	}
	if cloneMessages(nil) != nil {
		t.Fatal("empty messages should stay nil")
	}
	orig := schema.AssistantMessage("x", []schema.ToolCall{rawCall("1", "work", "{}")})
	got := cloneMessages([]adk.Message{orig, nil})
	if len(got) != 1 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("tool calls should be copied, got %+v", got)
	}
	got[0].ToolCalls[0].ID = "mutated"
	if orig.ToolCalls[0].ID == "mutated" {
		t.Fatal("cloned tool calls still share the original slice")
	}
}

func TestFormatContextEmptyInput(t *testing.T) {
	if got := formatContext("p", nil); got != "" {
		t.Fatalf("empty msgs: %q", got)
	}
	if got := formatContext("p", []adk.Message{schema.UserMessage("")}); got != "" {
		t.Fatalf("all-empty content: %q", got)
	}
}

func TestEnsureResultLockedSkipsEmptyAndDuplicate(t *testing.T) {
	h := &Handle{}
	h.ensureResultLocked()
	if h.history != nil {
		t.Fatal("empty result should not invent history")
	}
	h.result = "ans"
	h.history = []adk.Message{schema.AssistantMessage("ans", nil)}
	h.ensureResultLocked()
	if len(h.history) != 1 {
		t.Fatal("duplicate last answer should not be appended")
	}
}

func TestRememberIsIdempotentAndIgnoresClose(t *testing.T) {
	reg := NewRegistry()
	reg.past = nil
	h := &Handle{ID: "w-1", Role: "w", done: make(chan struct{}), result: "x"}
	close(h.done)
	h.history = []adk.Message{schema.AssistantMessage("first", nil)}
	reg.remember(h)
	h.history = []adk.Message{schema.AssistantMessage("second", nil)}
	reg.remember(h)
	if len(reg.pastIDs) != 1 {
		t.Fatalf("second remember of the same id should not grow the archive, got %v", reg.pastIDs)
	}
	if got := reg.past["w-1"].History[0].Content; got != "second" {
		t.Fatalf("remember should refresh the conversation, got %q", got)
	}
	closed := NewRegistry()
	closed.Close()
	closed.remember(&Handle{ID: "w-2", Role: "w", done: make(chan struct{})})
	if len(closed.past) != 0 {
		t.Fatal("remember after Close should drop the archive")
	}
}
