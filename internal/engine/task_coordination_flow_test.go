package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Two independent managers must negotiate through the real tool graph, not
// through a test-only direct call or the sub-agent registry's send_message.
func TestIndependentManagersDiscoverAndNegotiate(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("shared work", "", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := e.CreateThread("first task", "", "")
	b, _ := e.CreateThread("second task", "", "")
	for _, th := range []*store.Thread{a, b} {
		if err := e.MoveThread(th.ID, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	ra, rb := e.runtimeFor(a.ID), e.runtimeFor(b.ID)
	turn := &store.Turn{ID: "sender-turn", ThreadID: a.ID}
	peerTurn := &store.Turn{ID: "peer-turn", ThreadID: b.ID}
	for _, row := range []*store.Turn{turn, peerTurn} {
		if err := e.Store().CreateTurn(row); err != nil {
			t.Fatal(err)
		}
	}
	peerRegistry := swarm.NewRegistry()
	t.Cleanup(peerRegistry.Close)
	rb.occupy(peerRegistry, func() {}, "peer-turn", make(chan struct{}))
	defer rb.release(func() {}, rb.idle, "peer-turn")
	set := buildTestToolset(t, e, a.ID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func(msgs []*schema.Message) *schema.Message {
			if !strings.Contains(msgs[0].Content, taskCoordinationPrompt) {
				t.Error("host manager lost the guidance for its mounted peer tools")
			}
			return coordinationCall("discover", "list_tasks", `{}`)
		},
		func(msgs []*schema.Message) *schema.Message {
			out := msgs[len(msgs)-1].Content
			if !strings.Contains(out, b.ID) || !strings.Contains(out, p.Workdir) {
				t.Errorf("active peer/workspace missing: %s", out)
			}
			args, _ := json.Marshal(map[string]string{"thread_id": b.ID, "text": "I can wait for your acknowledgement before changing shared resources."})
			return coordinationCall("negotiate", "send_task_message", string(args))
		},
		func(msgs []*schema.Message) *schema.Message {
			out := msgs[len(msgs)-1].Content
			if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"seq":`) {
				t.Errorf("no durable receipt: %s", out)
			}
			return schema.AssistantMessage("Negotiation request queued; no agreement yet.", nil)
		},
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	t.Cleanup(reg.Close)
	acc := newAccumulator(e, a.ID, turn.ID, 0)
	_, err = ra.runOwnedTestManager(context.Background(), turn, reg, acc, set, nil, []adk.Message{schema.UserMessage("coordinate this task")})
	acc.flushAll()
	if err != nil {
		t.Fatalf("manager cannot discover/communicate with an independent task: %v", err)
	}
	if cm.next != 3 {
		t.Fatalf("negotiation never reached its receipt: calls=%d", cm.next)
	}
	// Receiver reads the stored message and replies through its own manager.
	setB := buildTestToolset(t, e, b.ID)
	cmB := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func(_ []*schema.Message) *schema.Message { return coordinationCall("read", "read_task_messages", `{}`) },
		func(msgs []*schema.Message) *schema.Message {
			out := msgs[len(msgs)-1].Content
			if !strings.Contains(out, a.ID) || !strings.Contains(out, "shared resources") {
				t.Errorf("peer inbox lost source/body: %s", out)
			}
			args, _ := json.Marshal(map[string]string{"thread_id": a.ID, "text": "Acknowledged; I will finish first and send a completion notice."})
			return coordinationCall("reply", "send_task_message", string(args))
		},
		func(_ []*schema.Message) *schema.Message { return schema.AssistantMessage("Agreement recorded.", nil) },
	}}
	regB := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cmB }, setB, nil)
	t.Cleanup(regB.Close)
	accB := newAccumulator(e, b.ID, "peer-turn", 0)
	_, err = rb.runOwnedTestManager(context.Background(), peerTurn, regB, accB, setB, nil, []adk.Message{schema.UserMessage("coordinate this task")})
	accB.flushAll()
	if err != nil {
		t.Fatalf("receiver cannot negotiate: %v", err)
	}
	for _, id := range []string{a.ID, b.ID} {
		events, err := e.Store().ListEvents(id, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range events {
			if ev.Kind == "task_message" {
				found = true
			}
		}
		if !found {
			t.Fatalf("peer message absent from recoverable timeline for %s", id)
		}
	}
}

type coordinationModel struct {
	steps []func([]*schema.Message) *schema.Message
	next  int
}

func (m *coordinationModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	i := m.next
	m.next++
	if i >= len(m.steps) {
		return schema.AssistantMessage("done", nil), nil
	}
	return m.steps[i](in), nil
}

func (m *coordinationModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	r, w := schema.Pipe[*schema.Message](1)
	w.Send(msg, nil)
	w.Close()
	return r, nil
}

func coordinationCall(id, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
