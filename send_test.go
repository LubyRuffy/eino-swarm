package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func waitAgent(t *testing.T, h *Handle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not finish")
	}
}

func sendJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("send_message returned non-JSON %q: %v", out, err)
	}
	return got
}

func assertNoKnownRoster(t *testing.T, out string) {
	t.Helper()
	if _, ok := sendJSON(t, out)["known"]; ok {
		t.Fatalf("a miss must not return a roster that invites retry: %s", out)
	}
}

// A guessed sibling id used to return a Go error. eino's ToolNode turns that
// into NodeRunError and kills the worker — after the real work already landed.
func TestSendToUnknownAgentDoesNotFailTheCaller(t *testing.T) {
	reg := NewRegistry()
	sendT := invokable(t, reg.SendTool())
	out, err := sendT.InvokableRun(context.Background(), `{"agent_id":"ghost","text":"hello"}`)
	if err != nil {
		t.Fatalf("unknown agent must not be a tool error (that kills the worker): %v", err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != false {
		t.Fatalf("unknown agent was delivered: %s", out)
	}
	reason, _ := got["reason"].(string)
	if !strings.Contains(reason, "unknown agent") {
		t.Fatalf("miss is unusable without a reason: %s", out)
	}
	if got["notified"] != nil {
		t.Fatalf("the host calling send_message already has the tool result; do not echo it as a steer: %s", out)
	}
	assertNoKnownRoster(t, out)

	empty, err := sendT.InvokableRun(context.Background(), `{"agent_id":"","text":"hello"}`)
	if err != nil {
		t.Fatalf("empty agent_id must not be a tool error: %v", err)
	}
	got = sendJSON(t, empty)
	reason, _ = got["reason"].(string)
	if got["delivered"] != false || !strings.Contains(reason, "agent_id is required") {
		t.Fatalf("empty agent_id miss is unusable: %s", empty)
	}
}

// An invented suffix is a miss, not a silent delivery to a similarly named
// worker. Recovery is notifying the host, not guessing another id.
func TestInventedSiblingIdIsAMissNotAGuess(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
		}}
	}
	peer, err := reg.Spawn(context.Background(), "peer", "long work",
		reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Cancel() })

	out, err := invokable(t, reg.SendTool()).InvokableRun(context.Background(),
		`{"agent_id":"peer-99","text":"nudge"}`)
	if err != nil {
		t.Fatalf("invented id must not fail the tool: %v", err)
	}
	if sendJSON(t, out)["delivered"] != false {
		t.Fatalf("invented suffix was delivered: %s", out)
	}
	assertNoKnownRoster(t, out)
	if _, _, finished := peer.Result(); finished {
		t.Fatal("a miss must not be delivered to a similarly named worker")
	}
}

// One worker per role: addressing the role is the same as addressing the id
// spawn_agent returned. Models mix those two strings constantly.
func TestSendByRoleReachesTheRunningWorker(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting work", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(_ int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if m.Role == schema.User && strings.HasPrefix(m.Content, "[steer] ") {
						return schema.AssistantMessage("STEERED(1)", nil)
					}
				}
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "peer", "do work",
		reg.ModelBuilder, &workTool{d: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	sendT := invokable(t, reg.SendTool())
	out, err := sendT.InvokableRun(context.Background(), `{"agent_id":"peer","text":"priority: fast path"}`)
	if err != nil {
		t.Fatalf("send by role: %v", err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != true {
		t.Fatalf("role should resolve to the running worker: %s", out)
	}
	if got["resolved_id"] != h.ID {
		t.Fatalf("role resolution must name the real id, got %s", out)
	}
	waitAgent(t, h)
	res, err, finished := h.Result()
	if !finished || err != nil {
		t.Fatalf("worker failed: finished=%v err=%v", finished, err)
	}
	if !strings.Contains(res, "STEERED(1)") {
		t.Fatalf("role send was not queued: %s", res)
	}
}

func TestSendByRoleToFinishedWorkerReportsAlreadyFinished(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "peer", "short work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	sendT := invokable(t, reg.SendTool())
	out, err := sendT.InvokableRun(context.Background(), `{"agent_id":"peer","text":"too late"}`)
	if err != nil {
		t.Fatalf("send to a finished role: %v", err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != false {
		t.Fatalf("finished role was delivered: %s", out)
	}
	reason, _ := got["reason"].(string)
	if !strings.Contains(reason, "already finished") {
		t.Fatalf("want already finished, got %s", out)
	}
	if got["resolved_id"] != h.ID {
		t.Fatalf("finished role should still name the id: %s", out)
	}
	if got["notified"] != nil {
		t.Fatalf("the host already has this result; do not steer it again: %s", out)
	}
}

func TestSendByRoleSurvivesStatsPruningTheHandle(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "peer", "short work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	reg.Stats()
	out, err := invokable(t, reg.SendTool()).InvokableRun(context.Background(),
		`{"agent_id":"peer","text":"too late"}`)
	if err != nil {
		t.Fatalf("pruned finished role: %v", err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != false {
		t.Fatalf("pruned finished role was delivered: %s", out)
	}
	reason, _ := got["reason"].(string)
	if !strings.Contains(reason, "already finished") {
		t.Fatalf("pruned finished role must not look unknown: %s", out)
	}
	if got["resolved_id"] != h.ID {
		t.Fatalf("pruned role should still name the id: %s", out)
	}
}

// The actual product: a worker that cannot reach a sibling must finish as
// done, and the host must get the text so it can spawn or resume whoever is
// next. A roster to retry against is not that.
func TestWorkerHandoffNotifiesTheManagerAndFinishes(t *testing.T) {
	reg := NewRegistry()
	var sawNotified bool
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("handing off", []schema.ToolCall{
					rawCall("s1", "send_message", `{"agent_id":"ghost","text":"please continue"}`),
				})
			},
			func(_ int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if m.Role != schema.Tool {
						continue
					}
					if strings.Contains(strings.ToLower(m.Content), "noderunerror") {
						t.Fatalf("the miss leaked as a graph error: %s", m.Content)
					}
					got := sendJSON(t, m.Content)
					if got["notified"] == DefaultManagerID && got["delivered"] == false {
						sawNotified = true
					}
					if _, ok := got["known"]; ok {
						t.Fatalf("notified:manager is the recovery; a roster invites another invented id: %s", m.Content)
					}
				}
				return schema.AssistantMessage("own work is done", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "peer", "finish the task",
		reg.ModelBuilder, reg.SendTool())
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	res, err, finished := h.Result()
	if !finished {
		t.Fatal("worker is still running")
	}
	if err != nil {
		t.Fatalf("a missed send_message killed the worker: %v", err)
	}
	if !sawNotified {
		t.Fatal("the worker was not told the host has the handoff")
	}
	if !strings.Contains(res, "own work is done") {
		t.Fatalf("worker never got to answer: %q", res)
	}

	steers := reg.TakePendingSteers()
	if len(steers) != 1 || !strings.Contains(steers[0], "please continue") {
		t.Fatalf("the host never received the handoff text: %q", steers)
	}
	if !strings.Contains(steers[0], h.ID) {
		t.Fatalf("the host must know which worker handed off: %q", steers)
	}

	waitT := invokable(t, reg.Tools()[2])
	rep, err := waitT.InvokableRun(context.Background(), `{"agent_ids":["`+h.ID+`"],"timeout_s":1}`)
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
	if !strings.Contains(rep, `"status":"done"`) {
		t.Fatalf("roster should show done, not failed: %s", rep)
	}
	if strings.Contains(rep, `"status":"failed"`) {
		t.Fatalf("a recovered miss must not look like a failed worker: %s", rep)
	}
}

func TestSendToManagerFromAWorker(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("reporting", []schema.ToolCall{
					rawCall("s1", "send_message", `{"agent_id":"manager","text":"work is ready"}`),
				})
			},
			func(_ int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if m.Role != schema.Tool {
						continue
					}
					got := sendJSON(t, m.Content)
					if got["delivered"] != true || got["agent_id"] != DefaultManagerID {
						t.Fatalf("send_message to manager must deliver: %s", m.Content)
					}
				}
				return schema.AssistantMessage("own work is done", nil)
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "peer", "finish the task",
		reg.ModelBuilder, reg.SendTool())
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	if _, err, finished := h.Result(); !finished || err != nil {
		t.Fatalf("worker failed: finished=%v err=%v", finished, err)
	}
	steers := reg.TakePendingSteers()
	if len(steers) != 1 || !strings.Contains(steers[0], "work is ready") {
		t.Fatalf("manager never got the report: %q", steers)
	}
	if !strings.Contains(steers[0], h.ID) {
		t.Fatalf("the report must name the worker: %q", steers)
	}
}

func TestSendAfterCloseIsAMissNotAnError(t *testing.T) {
	reg := NewRegistry()
	sendT := invokable(t, reg.SendTool())
	reg.Close()
	out, err := sendT.InvokableRun(context.Background(), `{"agent_id":"ghost","text":"hello"}`)
	if err != nil {
		t.Fatalf("a closed registry must not turn send_message into a tool error: %v", err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != false {
		t.Fatalf("closed registry delivered a steer: %s", out)
	}
	out, err = sendT.InvokableRun(context.Background(), `{"agent_id":"manager","text":"hello"}`)
	if err != nil {
		t.Fatalf("send to manager on a closed registry: %v", err)
	}
	got = sendJSON(t, out)
	if got["delivered"] != false {
		t.Fatalf("closed registry delivered to manager: %s", out)
	}
}

func TestSendMessageToolDescribesAMissAsNotFatal(t *testing.T) {
	info, err := NewRegistry().SendTool().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || !strings.Contains(info.Desc, "notified:manager") {
		t.Fatalf("workers only see the tool desc; it must say a miss notifies the host, got %q", info.Desc)
	}
	if !strings.Contains(info.Desc, "finish with a final answer") {
		t.Fatalf("the desc must tell the worker to finish, not retry: %s", info.Desc)
	}
	if strings.Contains(strings.ToLower(info.Desc), "reviewer") || strings.Contains(info.Desc, "1342") {
		t.Fatalf("tool desc leaked a sample: %s", info.Desc)
	}
}

// The manager is blocked in wait_agents. The worker's invented sibling must
// still land on the manager's next model call, and the worker must show done.
func TestMissedHandoffReachesTheWaitingManager(t *testing.T) {
	reg := NewRegistry()
	var sawSteer, sawDone bool
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(int, []*schema.Message) *schema.Message {
					return schema.AssistantMessage("delegating", []schema.ToolCall{
						rawCall("s1", "spawn_agent", `{"role":"peer","task":"finish the task"}`),
					})
				},
				func(int, []*schema.Message) *schema.Message {
					return schema.AssistantMessage("waiting", []schema.ToolCall{
						rawCall("w1", "wait_agents", `{"agent_ids":["peer-1"],"timeout_s":5}`),
					})
				},
				func(_ int, msgs []*schema.Message) *schema.Message {
					for _, m := range msgs {
						if m.Role == schema.User && strings.Contains(m.Content, "please continue") {
							sawSteer = true
						}
						if m.Role == schema.Tool && strings.Contains(m.Content, `"status":"done"`) &&
							strings.Contains(m.Content, "own work is done") {
							sawDone = true
						}
					}
					return schema.AssistantMessage("handoff received", nil)
				},
			}}
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("handing off", []schema.ToolCall{
					rawCall("s1", "send_message", `{"agent_id":"ghost","text":"please continue"}`),
				})
			},
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("own work is done", nil)
			},
		}}
	}
	res, err := reg.RunWith(context.Background(), RunConfig{
		Instruction: "delegate",
		Task:        "go",
	}, nil)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !sawSteer {
		t.Fatal("the waiting manager never saw the worker's handoff text")
	}
	if !sawDone {
		t.Fatal("wait_agents did not report the worker done with its answer")
	}
	if !strings.Contains(res.Final, "handoff received") {
		t.Fatalf("manager never closed the loop: %q", res.Final)
	}
}

func TestWorkerHandoffToFinishedSiblingNotifiesManager(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == "peer" {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(int, []*schema.Message) *schema.Message {
					return schema.AssistantMessage("DONE", nil)
				},
			}}
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("handing off", []schema.ToolCall{
					rawCall("s1", "send_message", `{"agent_id":"peer","text":"please continue"}`),
				})
			},
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("own work is done", nil)
			},
		}}
	}
	peer, err := reg.Spawn(context.Background(), "peer", "short work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, peer)
	other, err := reg.Spawn(context.Background(), "other", "finish the task",
		reg.ModelBuilder, reg.SendTool())
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, other)
	if _, err, finished := other.Result(); !finished || err != nil {
		t.Fatalf("worker failed: finished=%v err=%v", finished, err)
	}
	steers := reg.TakePendingSteers()
	if len(steers) != 1 || !strings.Contains(steers[0], "please continue") {
		t.Fatalf("finished sibling must escalate to the host: %q", steers)
	}
}

func TestEmptyHandoffDoesNotSteerTheManager(t *testing.T) {
	reg := NewRegistry()
	out, err := invokable(t, reg.sendToolFor("peer-1", "peer")).InvokableRun(
		context.Background(), `{"agent_id":"ghost","text":"   "}`)
	if err != nil {
		t.Fatal(err)
	}
	got := sendJSON(t, out)
	if got["notified"] != nil {
		t.Fatalf("empty text must not invent a host notification: %s", out)
	}
	if steers := reg.TakePendingSteers(); len(steers) != 0 {
		t.Fatalf("empty text steered the manager: %q", steers)
	}
}

func TestSendToManagerFromTheHost(t *testing.T) {
	reg := NewRegistry()
	out, err := invokable(t, reg.SendTool()).InvokableRun(context.Background(),
		`{"agent_id":"manager","text":"note to self"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := sendJSON(t, out)
	if got["delivered"] != true {
		t.Fatalf("host send_message to manager must deliver: %s", out)
	}
	steers := reg.TakePendingSteers()
	if len(steers) != 1 || steers[0] != "note to self" {
		t.Fatalf("host-to-manager must not wrap a worker prefix: %q", steers)
	}
}

func TestSendToManagerWithoutARolePrefix(t *testing.T) {
	reg := NewRegistry()
	out, err := invokable(t, reg.sendToolFor("peer-1", "")).InvokableRun(
		context.Background(), `{"agent_id":"manager","text":"work is ready"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sendJSON(t, out)["delivered"] != true {
		t.Fatalf("delivered: %s", out)
	}
	steers := reg.TakePendingSteers()
	if len(steers) != 1 || steers[0] != "from peer-1: work is ready" {
		t.Fatalf("missing from-prefix without a role: %q", steers)
	}
}

func TestClosedRegistryDoesNotNotifyFromAWorker(t *testing.T) {
	reg := NewRegistry()
	sendT := invokable(t, reg.sendToolFor("peer-1", "peer"))
	reg.Close()
	out, err := sendT.InvokableRun(context.Background(), `{"agent_id":"ghost","text":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sendJSON(t, out)["notified"] != nil {
		t.Fatalf("a closed registry has no host to notify: %s", out)
	}
	if steers := reg.TakePendingSteers(); len(steers) != 0 {
		t.Fatalf("closed registry queued a steer: %q", steers)
	}
}

func TestManagerCallerDoesNotEscalateAMiss(t *testing.T) {
	reg := NewRegistry()
	out, err := invokable(t, reg.sendToolFor(DefaultManagerID, "manager")).InvokableRun(
		context.Background(), `{"agent_id":"ghost","text":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sendJSON(t, out)["notified"] != nil {
		t.Fatalf("the host already has the tool result: %s", out)
	}
	if steers := reg.TakePendingSteers(); len(steers) != 0 {
		t.Fatalf("host miss echoed as a steer: %q", steers)
	}
}

func TestBindSendCallerSkipsWhenTheWorkerHasNoId(t *testing.T) {
	reg := NewRegistry()
	orig := []tool.BaseTool{reg.SendTool()}
	got := reg.bindSendCaller(orig, "", "peer")
	if len(got) != 1 || got[0] != orig[0] {
		t.Fatal("an empty id must not wrap send_message")
	}
	if toolName(nil) != "" {
		t.Fatal("nil tool has no name")
	}
	if toolName(stubInfo{err: errors.New("nope")}) != "" || toolName(stubInfo{}) != "" {
		t.Fatal("a tool with no name must not look like send_message")
	}
}

type stubInfo struct {
	info *schema.ToolInfo
	err  error
}

func (s stubInfo) Info(context.Context) (*schema.ToolInfo, error) { return s.info, s.err }
