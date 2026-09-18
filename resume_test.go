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

// resume_agent starts a new run under the same id, so the spawned event has
// to carry the new Instruction — the chrome would otherwise keep showing the
// first task.
func TestResumeReemitsSpawnedWithTheNewInstruction(t *testing.T) {
	reg := NewRegistry()
	reg.WorkerPreamble = "OS: testhost"
	reg.ModelBuilder = oneShot("first")
	var got []string
	reg.setHooks(func(role, agentID, instruction string) {
		got = append(got, instruction)
	}, nil)
	h, err := reg.Spawn(context.Background(), "w", "first task", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}

	reg.ModelBuilder = oneShot("second")
	nh, err := reg.Resume(context.Background(), h.ID, "continue", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-nh.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("resumed worker did not finish")
	}
	if len(got) != 2 {
		t.Fatalf("want spawn then resume, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "first task") || !strings.Contains(got[0], "OS: testhost") {
		t.Fatalf("first instruction=%q", got[0])
	}
	if !strings.Contains(got[1], "continue") || strings.Contains(got[1], "first task") {
		t.Fatalf("resume must send the new task, got %q", got[1])
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
	out, err := resumeT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"task":"more"}`, h.ID))
	assertCtlRefuse(t, out, err, "still running")
}

func TestResumeUnknownRejected(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	resumeT := invokable(t, reg.Tools()[4])
	out, err := resumeT.InvokableRun(context.Background(), `{"agent_id":"ghost","task":"more"}`)
	assertCtlRefuse(t, out, err, "unknown agent")
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
	resumeT := invokable(t, reg.Tools()[4])
	out, err := resumeT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"task":"continue"}`, h.ID))
	assertCtlRefuse(t, out, err, "closed")
}

func TestResumeRequiresIDandTask(t *testing.T) {
	reg := NewRegistry()
	resumeT := invokable(t, reg.Tools()[4])
	out, err := resumeT.InvokableRun(context.Background(), `{"task":"x"}`)
	assertCtlRefuse(t, out, err, "agent_id is required")
	out, err = resumeT.InvokableRun(context.Background(), `{"agent_id":"w-1"}`)
	assertCtlRefuse(t, out, err, "task is required")
	out, err = resumeT.InvokableRun(context.Background(), `{"agent_id":"w-1","task":"   "}`)
	assertCtlRefuse(t, out, err, "task is required")
	out, err = resumeT.InvokableRun(context.Background(), `{`)
	assertCtlRefuse(t, out, err, "could not read the arguments")
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

func TestReusableFinishedIDSkipsEmptyAndRunning(t *testing.T) {
	reg := NewRegistry()
	if got := reg.reusableFinishedID(""); got != "" {
		t.Fatalf("empty role must not match, got %q", got)
	}

	doneCh := make(chan struct{})
	running := &Handle{ID: "w-1", Role: "w", done: make(chan struct{})}
	reg.mu.Lock()
	reg.agents = map[string]*Handle{"w-1": running}
	reg.past = map[string]*agentPast{"w-1": {ID: "w-1", Role: "w"}}
	reg.pastIDs = []string{"w-1"}
	reg.mu.Unlock()
	if got := reg.reusableFinishedID("w"); got != "" {
		t.Fatalf("a running worker is not reusable, got %q", got)
	}

	close(running.done)
	if got := reg.reusableFinishedID("w"); got != "w-1" {
		t.Fatalf("finished archive entry should be reusable, got %q", got)
	}

	reg.mu.Lock()
	reg.past = nil
	reg.pastIDs = nil
	reg.agents["w-1"] = &Handle{ID: "w-1", Role: "w", done: doneCh}
	reg.mu.Unlock()
	close(doneCh)
	if got := reg.reusableFinishedID("w"); got != "w-1" {
		t.Fatalf("finished handle still in the live map must be reusable, got %q", got)
	}
}

func TestRunningIDPicksTheOldestLiveWorker(t *testing.T) {
	reg := NewRegistry()
	if got := reg.runningID(""); got != "" {
		t.Fatalf("empty role must not match, got %q", got)
	}
	older := &Handle{ID: "w-1", Role: "w", done: make(chan struct{}), spawned: time.Unix(1, 0)}
	newer := &Handle{ID: "w-2", Role: "w", done: make(chan struct{}), spawned: time.Unix(2, 0)}
	doneCh := make(chan struct{})
	close(doneCh)
	finished := &Handle{ID: "w-0", Role: "w", done: doneCh, spawned: time.Unix(0, 0)}
	other := &Handle{ID: "x-1", Role: "x", done: make(chan struct{}), spawned: time.Unix(0, 0)}
	reg.mu.Lock()
	reg.agents = map[string]*Handle{"w-0": finished, "w-1": older, "w-2": newer, "x-1": other}
	reg.mu.Unlock()
	if got := reg.runningID("w"); got != "w-1" {
		t.Fatalf("runningID should pick the oldest live worker, got %q", got)
	}
	if got := reg.runningID("x"); got != "x-1" {
		t.Fatalf("runningID should see the other role, got %q", got)
	}
}

func TestSpawnAgentRequiresATask(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		t.Fatal("must not mint a worker without a task")
		return nil
	}
	spawnT := invokable(t, reg.Tools()[0])
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"w"}`)
	assertCtlRefuse(t, out, err, "task is required")
	out, err = spawnT.InvokableRun(context.Background(), `{"role":"w","task":"   "}`)
	assertCtlRefuse(t, out, err, "task is required")
	out, err = spawnT.InvokableRun(context.Background(), `{"task":"x"}`)
	assertCtlRefuse(t, out, err, "role is required")
	out, err = spawnT.InvokableRun(context.Background(), `{`)
	assertCtlRefuse(t, out, err, "could not read the arguments")
}

func TestSpawnOnAClosedRegistryDoesNotKillTheCaller(t *testing.T) {
	reg := NewRegistry()
	reg.Close()
	spawnT := invokable(t, reg.Tools()[0])
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"x"}`)
	assertCtlRefuse(t, out, err, "closed")
}

func TestSpawnReuseWhenResumeFailsDoesNotKillTheCaller(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("x")
	h, err := reg.Spawn(context.Background(), "w", "first", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	reg.mu.Lock()
	reg.closed = true
	reg.mu.Unlock()
	spawnT := invokable(t, reg.Tools()[0])
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"second"}`)
	assertCtlRefuse(t, out, err, "closed")
}

// A missing spawn_agent task used to return a Go error. eino's ToolNode wraps
// that as NodeRunError and kills the manager — a pursuing /goal then blocks.
// The model omitted a field; that is a retry, not a crash.
func TestSpawnWithoutATaskDoesNotKillTheManager(t *testing.T) {
	var minted int
	var sawRefuse bool
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{calls: []schema.ToolCall{
					rawCall("s1", "spawn_agent", `{"role":"reader"}`),
				}},
				{inspect: func(msgs []*schema.Message) {
					for _, m := range msgs {
						if m != nil && m.Role == schema.Tool && strings.Contains(m.Content, "task is required") {
							sawRefuse = true
						}
					}
				}, calls: []schema.ToolCall{
					rawCall("s2", "spawn_agent", `{"role":"reader","task":"read it"}`),
				}},
				{calls: []schema.ToolCall{
					rawCall("w1", "wait_agents", `{"agent_ids":["reader-1"],"timeout_s":5}`),
				}},
				{content: []string{"done"}},
			}}
		}
		minted++
		return &chunkedModel{turns: []turnScript{{content: []string{"ok"}}}}
	}
	if _, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "delegate", Task: "go"}, nil); err != nil {
		t.Fatalf("a missing spawn task must be a tool result, not a NodeRunError: %v", err)
	}
	if !sawRefuse {
		t.Fatal("the retry turn must see the refused spawn")
	}
	if minted != 1 {
		t.Fatalf("missing task must not mint; the retry must mint once, got %d", minted)
	}
}

func TestLifecycleToolsDescribeACallerMistakeAsNotFatal(t *testing.T) {
	for _, name := range []string{"spawn_agent", "close_agent", "resume_agent"} {
		var info *schema.ToolInfo
		for _, bt := range NewRegistry().Tools() {
			got, err := bt.Info(context.Background())
			if err != nil || got == nil {
				t.Fatal(err)
			}
			if got.Name == name {
				info = got
				break
			}
		}
		if info == nil {
			t.Fatalf("missing %s", name)
		}
		if !strings.Contains(info.Desc, "does not fail the turn") {
			t.Fatalf("%s must say a caller mistake is not a crashed turn, got %q", name, info.Desc)
		}
		for _, leak := range []string{"reviewer", "notes.md", "NodeRunError"} {
			if strings.Contains(info.Desc, leak) {
				t.Fatalf("%s desc leaked %q", name, leak)
			}
		}
	}
}

func TestSpawnAgentSteersARunningWorkerWithTheSameRole(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	block := make(chan struct{})
	var once sync.Once
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				once.Do(func() { close(started) })
				<-block
				return schema.AssistantMessage("ok", nil)
			},
		}}
	}
	spawnT := invokable(t, reg.Tools()[0])
	out1, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	out2, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	close(block)
	var a, b struct {
		AgentID     string `json:"agent_id"`
		ResumedFrom string `json:"resumed_from"`
		Steered     string `json:"steered"`
	}
	if err := json.Unmarshal([]byte(out1), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out2), &b); err != nil {
		t.Fatal(err)
	}
	if a.AgentID == "" || a.AgentID != b.AgentID || b.Steered != "true" {
		t.Fatalf("a running same-role spawn must steer in place, first=%+v second=%+v", a, b)
	}
	if b.ResumedFrom != "" {
		t.Fatalf("a still-running worker is steered, not resumed, got %+v", b)
	}
	h, ok := reg.get(a.AgentID)
	if !ok {
		t.Fatal("worker vanished")
	}
	waitDone(t, h)
	if leftover := h.leftover(); len(leftover) != 1 || leftover[0] != "b" {
		t.Fatalf("the new task should queue as steering, leftover=%v", leftover)
	}
}

func TestSpawnAgentSameRoleInParallelSharesOneID(t *testing.T) {
	reg := NewRegistry()
	block := make(chan struct{})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				<-block
				return schema.AssistantMessage("ok", nil)
			},
		}}
	}
	spawnT := invokable(t, reg.Tools()[0])
	out := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			out[i], errs[i] = spawnT.InvokableRun(context.Background(),
				fmt.Sprintf(`{"role":"w","task":"t%d"}`, i))
		}(i)
	}
	wg.Wait()
	close(block)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
	}
	var first, second struct {
		AgentID string `json:"agent_id"`
		Steered string `json:"steered"`
	}
	if err := json.Unmarshal([]byte(out[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first.AgentID == "" || first.AgentID != second.AgentID {
		t.Fatalf("parallel same-role spawns must share one id, got %q %q", first.AgentID, second.AgentID)
	}
	if (first.Steered == "true") == (second.Steered == "true") {
		t.Fatalf("exactly one of the parallel calls should steer, got %+v %+v", first, second)
	}
}

func TestSpawnAgentReusesAFinishedWorkerWithTheSameRole(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("ok")
	spawnT := invokable(t, reg.Tools()[0])
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"first"}`)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}
	h, ok := reg.get(first.AgentID)
	if !ok {
		t.Fatal("first spawn vanished")
	}
	waitDone(t, h)

	out, err = spawnT.InvokableRun(context.Background(), `{"role":"w","task":"second"}`)
	if err != nil {
		t.Fatal(err)
	}
	var second struct {
		AgentID     string `json:"agent_id"`
		ResumedFrom string `json:"resumed_from"`
	}
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	if second.AgentID != first.AgentID || second.ResumedFrom != first.AgentID {
		t.Fatalf("same-role spawn after stop should continue in place, first=%q second=%+v", first.AgentID, second)
	}
}

func TestForkContextDoesNotMintATwinForAnExistingRole(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("ok")
	spawnT := invokable(t, reg.Tools()[0])
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"w","task":"first"}`)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}
	h, ok := reg.get(first.AgentID)
	if !ok {
		t.Fatal("first spawn vanished")
	}
	waitDone(t, h)

	out, err = spawnT.InvokableRun(context.Background(), `{"role":"w","task":"forked","fork_context":true}`)
	if err != nil {
		t.Fatal(err)
	}
	var second struct {
		AgentID     string `json:"agent_id"`
		ResumedFrom string `json:"resumed_from"`
		Forked      string `json:"forked"`
	}
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	if second.AgentID != first.AgentID || second.ResumedFrom != first.AgentID || second.Forked != "" {
		t.Fatalf("fork_context must not mint a twin for a finished role, first=%q second=%+v", first.AgentID, second)
	}
}
