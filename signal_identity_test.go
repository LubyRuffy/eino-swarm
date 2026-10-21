package swarm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func notificationMessageID(t *testing.T, n Notification) string {
	t.Helper()
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	return wire.MessageID
}

func TestCanceledOutputAndResumedWorkersReceiveNewMessageIdentities(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.SetHostNotify(rec.cb())
	acc := &streamAcc{reg: reg, agentID: "interrupted", role: "inspector"}
	for range 2 {
		r, w := schema.Pipe[*schema.Message](2)
		w.Send(&schema.Message{Role: schema.Assistant, ReasoningContent: "checking"}, nil)
		w.Send(nil, context.Canceled)
		w.Close()
		if _, _, err := acc.drain(r); err != nil {
			t.Fatal(err)
		}
	}
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{reasoning: []string{"checking"}, content: []string{"complete"}}}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var first string
	for _, role := range []string{"inspector", "reviewer"} {
		h, err := reg.Spawn(ctx, role, "inspect the current state", reg.ModelBuilder)
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = h.ID
		}
	}
	if err := reg.Wait(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resume(ctx, first, "verify the latest state", reg.ModelBuilder); err != nil {
		t.Fatal(err)
	}
	if err := reg.Wait(ctx, 0); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	latest := map[string]string{}
	for _, n := range rec.all() {
		id := notificationMessageID(t, n)
		switch n.Kind {
		case NotifyTurn:
			if id == "" || seen[id] {
				t.Fatalf("new output reused an old identity: %+v", n)
			}
			seen[id], latest[n.AgentID] = true, id
		case NotifyReasoningDelta, NotifyDelta, NotifyAgentMessage:
			if id == "" || id != latest[n.AgentID] {
				t.Fatalf("output mixed identities between agents or resumes: %+v", n)
			}
			if n.Kind == NotifyAgentMessage && n.AgentID == "interrupted" {
				t.Fatalf("cancellation fabricated a completed output: %+v", n)
			}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("want two canceled outputs, two workers and one resume; got %d", len(seen))
	}
}

func TestNonstreamingNotificationsDoNotInventMessageIdentity(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	reg.emitComplete("inspector", "worker", adk.EventFromMessage(schema.AssistantMessage("complete", nil), nil, schema.Assistant, ""))
	for _, n := range rec.all() {
		if notificationMessageID(t, n) != "" {
			t.Fatalf("nonstream output received a fabricated identity: %+v", n)
		}
	}
	if len(rec.all()) != 1 {
		t.Fatalf("nonstream output disappeared: %+v", rec.all())
	}
}

// Identical text from a new model output must remain a separate message. The
// identity belongs to the output, rather than an agent, run, or text prefix.
func TestStreamMessageIdentityChangesAcrossModelRoundsAndNewRuns(t *testing.T) {
	seen := map[string]bool{}
	for range 2 {
		reg := NewRegistry()
		reg.ModelBuilder = func(string, string) model.BaseChatModel {
			return &chunkedModel{turns: []turnScript{
				{reasoning: []string{"check "}, content: []string{"checking"}, calls: []schema.ToolCall{rawCall("c1", "inspect", "{}")}},
				{reasoning: []string{"check "}, content: []string{"checking"}},
			}}
		}
		tools := []tool.BaseTool{&fnTool{name: "inspect", fn: func(context.Context, string) (string, error) {
			return "verified", nil
		}}}
		rec := &recorder{}
		if _, err := reg.RunWith(context.Background(), RunConfig{Task: "verify the current state", ManagerTools: tools}, rec.cb()); err != nil {
			t.Fatal(err)
		}
		current := ""
		calls, results := 0, 0
		for _, n := range rec.all() {
			switch n.Kind {
			case NotifyTurn:
				current = notificationMessageID(t, n)
				if current == "" || seen[current] {
					t.Fatalf("new model output reused or omitted its identity: %+v", n)
				}
				seen[current] = true
			case NotifyReasoningDelta, NotifyDelta, NotifyToolCall, NotifyAgentMessage:
				if got := notificationMessageID(t, n); got != current || got == "" {
					t.Fatalf("output identity changed within one stream: %q -> %q, %+v", current, got, n)
				}
				if n.Kind == NotifyToolCall {
					calls++
					if n.ToolCallID != "c1" {
						t.Fatalf("tool call identity changed: %+v", n)
					}
				}
			case NotifyToolResult:
				results++
				if n.ToolCallID != "c1" || n.Text != "verified" {
					t.Fatalf("tool pairing changed: %+v", n)
				}
			}
		}
		if calls != 1 || results != 1 {
			t.Fatalf("model output identity dropped or duplicated tool events: calls=%d results=%d", calls, results)
		}
		reg.Close()
	}
	if len(seen) != 4 {
		t.Fatalf("want four distinct model outputs, got %d", len(seen))
	}
}
