package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Invalid tool calls from one answer must not become a second, enormous
// conversation. The manager still needs the refusal so it can correct task.
func TestInvalidSpawnBatchIsBoundedAndCanBeRepaired(t *testing.T) {
	var calls []schema.ToolCall
	for i := range 80 {
		calls = append(calls, schema.ToolCall{ID: fmt.Sprintf("bad-%d", i), Type: "function",
			Function: schema.FunctionCall{Name: "spawn_agent", Arguments: fmt.Sprintf(`{"role":"job-%d"}`, i)}})
	}
	calls = append(calls, schema.ToolCall{ID: "empty", Type: "function",
		Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{}`}})
	manager := &chunkedModel{turns: []turnScript{
		{calls: calls},
		{inspect: func(msgs []*schema.Message) {
			results := 0
			for _, msg := range msgs {
				if msg.Role != schema.Tool {
					continue
				}
				results++
				var refusal struct {
					Error    string
					Rejected int `json:"rejected_calls"`
				}
				if err := json.Unmarshal([]byte(msg.Content), &refusal); err != nil {
					t.Fatal(err)
				}
				want := 1
				if msg.ToolCallID == "bad-0" {
					want = 80
				}
				if refusal.Error == "" || refusal.Rejected != want {
					t.Errorf("refusal=%s", msg.Content)
				}
			}
			if results != 2 {
				t.Errorf("invalid batch produced %d tool results, want 2", results)
			}
		}, calls: []schema.ToolCall{{ID: "fixed", Type: "function",
			Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"writer","task":"draft a short report"}`}}}},
		{content: []string{"corrected"}},
	}}
	reg := NewRegistry()
	defer reg.Cleanup()
	reg.ModelBuilder = func(role, _ string) model.BaseChatModel {
		if role == DefaultManagerID {
			return manager
		}
		return &chunkedModel{turns: []turnScript{{content: []string{"report ready"}}}}
	}
	rec := &recorder{}
	res, err := reg.RunWith(context.Background(), RunConfig{Task: "prepare a report"}, rec.cb())
	if err != nil || res.Final != "corrected" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if n := len(rec.ofKind(NotifyToolCall)); n != 3 {
		t.Fatalf("trace has %d calls, want 3", n)
	}
	if n := len(rec.ofKind(NotifySpawned)); n != 1 {
		t.Fatalf("spawned %d workers, want only corrected call", n)
	}
	paired := map[string]bool{}
	for _, note := range rec.ofKind(NotifyToolCall) {
		paired[note.ToolCallID] = false
	}
	for _, note := range rec.ofKind(NotifyToolResult) {
		if _, ok := paired[note.ToolCallID]; !ok {
			t.Fatalf("unmatched result %s", note.ToolCallID)
		}
		paired[note.ToolCallID] = true
	}
	for id, found := range paired {
		if !found {
			t.Fatalf("missing result %s", id)
		}
	}
	for _, msg := range res.Transcript {
		if len(msg.ToolCalls) > 2 {
			t.Fatalf("unbounded transcript: %d calls", len(msg.ToolCalls))
		}
	}
}

func TestSpawnBatchKeepsValidCallsOtherToolsAndOriginalMessage(t *testing.T) {
	calls := []schema.ToolCall{
		rawCall("bad-json", "spawn_agent", `{`), rawCall("wrong-type", "spawn_agent", `{"task":12}`),
		rawCall("empty-task", "spawn_agent", `{"role":"alpha","task":"  "}`),
		rawCall("missing-task", "spawn_agent", `{"role":"beta"}`),
		rawCall("empty-role", "spawn_agent", `{"role":" ","task":"work"}`),
		rawCall("valid", "spawn_agent", `{"role":"alpha","task":"work","fork_context":true}`),
		rawCall("read", "read", `{"path":"report.txt"}`),
		rawCall("", "spawn_agent", `{}`),
	}
	original := schema.AssistantMessage("planning", calls)
	f := &spawnBatchFeedback{}
	out := f.filter(original)
	if len(out.ToolCalls) != 6 || len(original.ToolCalls) != 8 || out.Content != "planning" {
		t.Fatalf("filter=%+v original=%+v", out, original)
	}
	for _, id := range []string{"bad-json", "empty-task", "empty-role"} {
		text, ok := f.take(id)
		if !ok || !strings.Contains(text, "rejected_calls") {
			t.Fatalf("missing refusal %s: %s", id, text)
		}
		if _, ok := f.take(id); ok {
			t.Fatal("consumed refusal survived")
		}
	}
	if f.filter(nil) != nil {
		t.Fatal("nil message changed")
	}
	plain := schema.AssistantMessage("done", nil)
	if f.filter(plain) != plain {
		t.Fatal("plain answer changed")
	}
	a, problem := readSpawnArgs(`{"role":" alpha ","task":" work ","fork_context":true}`)
	if problem != "" || a.Role != "alpha" || a.Task != "work" || !a.ForkContext {
		t.Fatalf("args=%+v problem=%s", a, problem)
	}
}

type spawnStreamModel struct {
	chunks    []*schema.Message
	err       error
	immediate bool
}

func (m *spawnStreamModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return schema.AssistantMessage("", []schema.ToolCall{rawCall("bad", "spawn_agent", `{}`)}), nil
}
func (m *spawnStreamModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.immediate {
		return nil, m.err
	}
	sr, sw := schema.Pipe[*schema.Message](len(m.chunks) + 1)
	for _, msg := range m.chunks {
		sw.Send(msg, nil)
	}
	if m.err != nil {
		sw.Send(nil, m.err)
	}
	sw.Close()
	return sr, nil
}

func TestSpawnBatchStreamWaitsForCompleteArgumentsAndPreservesText(t *testing.T) {
	idx := 0
	inner := &spawnStreamModel{chunks: []*schema.Message{
		nil, {Role: schema.Assistant, Content: "live", ReasoningContent: "thinking", ToolCalls: []schema.ToolCall{{Index: &idx, ID: "split", Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"analyst",`}}}},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &idx, Function: schema.FunctionCall{Arguments: `"task":"check the report"}`}}}},
	}}
	m := &spawnBatchModel{inner: inner, feedback: &spawnBatchFeedback{}, reg: NewRegistry()}
	sr, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	first, err := sr.Recv()
	if err != nil || first.Content != "live" || first.ReasoningContent != "thinking" || len(first.ToolCalls) != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var calls []schema.ToolCall
	for {
		msg, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, msg.ToolCalls...)
	}
	if len(calls) != 1 || calls[0].ID != "split" || !strings.Contains(calls[0].Function.Arguments, "check the report") {
		t.Fatalf("calls=%+v", calls)
	}
	if _, ok := m.feedback.take("split"); ok {
		t.Fatal("complete fragmented call refused")
	}
}

func TestSpawnBatchPropagatesErrorsWithoutExecutingPartialCalls(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		} else {
			defer cancel()
		}
		boom := errors.New("stream lost")
		m := &spawnBatchModel{inner: &spawnStreamModel{err: boom, chunks: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{rawCall("partial", "spawn_agent", `{}`)})}}, feedback: &spawnBatchFeedback{}}
		if _, err := m.Generate(ctx, nil); !errors.Is(err, boom) {
			t.Fatalf("Generate error=%v", err)
		}
		sr, err := m.Stream(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for {
			msg, err := sr.Recv()
			if err != nil {
				want := boom
				if canceled {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
				break
			}
			if len(msg.ToolCalls) != 0 {
				t.Fatal("partial call escaped")
			}
		}
		sr.Close()
		m.inner = &spawnStreamModel{err: boom, immediate: true}
		if _, err := m.Stream(ctx, nil); !errors.Is(err, boom) {
			t.Fatalf("start error=%v", err)
		}
	}
	m := &spawnBatchModel{inner: &spawnStreamModel{}, feedback: &spawnBatchFeedback{}}
	msg, err := m.Generate(context.Background(), nil)
	if err != nil || len(msg.ToolCalls) != 1 {
		t.Fatalf("generate=%+v err=%v", msg, err)
	}
	if _, ok := m.feedback.take("bad"); !ok {
		t.Fatal("Generate missing feedback")
	}
}
