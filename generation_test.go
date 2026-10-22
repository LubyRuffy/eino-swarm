package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestInterruptedGenerationKeepsCauseAndIdentityWithoutFinishingWorker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		finish string
		reason string
	}{
		{"cancelled", context.Canceled, "", "cancelled"},
		{"deadline", context.DeadlineExceeded, "", "timeout"},
		{"transport", io.ErrUnexpectedEOF, "", "model_error"},
		{"output limit", nil, "length", "output_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			reg := NewRegistry()
			reg.SetHostNotify(rec.cb())
			acc := &streamAcc{reg: reg, agentID: "reader-1", role: "reader"}
			r, w := schema.Pipe[*schema.Message](3)
			w.Send(&schema.Message{Role: schema.Assistant, ReasoningContent: "checking the current state", ResponseMeta: &schema.ResponseMeta{FinishReason: tc.finish}}, nil)
			if tc.err != nil {
				w.Send(nil, tc.err)
			}
			w.Close()
			_, _, _ = acc.drain(r)
			var interrupted []Notification
			for _, n := range rec.all() {
				if n.Kind.String() == "generation_interrupted" {
					interrupted = append(interrupted, n)
				}
				if n.Kind == NotifyFinished || n.Kind == NotifyAgentMessage {
					t.Fatalf("interruption fabricated completion: %+v", n)
				}
			}
			if len(interrupted) != 1 {
				t.Fatalf("want one interruption receipt, got %+v", rec.all())
			}
			n := interrupted[0]
			var body struct{ Reason, Detail, Recovery string }
			if err := json.Unmarshal([]byte(n.Text), &body); err != nil {
				t.Fatal(err)
			}
			if n.MessageID == "" || n.MessageID != rec.ofKind(NotifyTurn)[0].MessageID || body.Reason != tc.reason || body.Detail == "" || body.Recovery != "none" {
				t.Fatalf("receipt lost identity or cause: %+v %+v", n, body)
			}
		})
	}
}

func TestNormalEmptyGenerationIsCompletedInsteadOfInterrupted(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	r, w := schema.Pipe[*schema.Message](1)
	w.Close()
	_, _, err := (&streamAcc{reg: reg, agentID: "worker"}).drain(r)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	count := 0
	for _, n := range rec.all() {
		if n.Kind.String() == "generation_interrupted" {
			t.Fatalf("normal empty stop was interrupted: %+v", n)
		}
		if n.Kind.String() == "generation_completed" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("normal completion has no durable generation receipt: %+v", rec.all())
	}
}

func TestPartialOutputLimitKeepsTextAndNeverExecutesTruncatedToolArguments(t *testing.T) {
	for _, calls := range [][]schema.ToolCall{nil, {{ID: "partial", Function: schema.FunctionCall{Name: "inspect", Arguments: `{"path":"`}}}} {
		rec := &recorder{}
		reg := NewRegistry()
		reg.SetHostNotify(rec.cb())
		r, w := schema.Pipe[*schema.Message](1)
		w.Send(&schema.Message{Role: schema.Assistant, Content: "The current result is partial", ToolCalls: calls, ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, nil)
		w.Close()
		answer, got, err := (&streamAcc{reg: reg, agentID: "reader"}).drain(r)
		if answer != "The current result is partial" || len(got) != 0 || len(rec.ofKind(NotifyGenerationInterrupted)) != 1 || len(rec.ofKind(NotifyToolCall)) != 0 {
			t.Fatalf("partial output was lost or executed: answer=%q calls=%v events=%+v", answer, got, rec.all())
		}
		if len(calls) > 0 && !errors.Is(err, errOutputBudget) {
			t.Fatalf("truncated call accepted: %v", err)
		}
	}
}

func TestGenerationModelReportsFailureBeforeStreamAndNormalGenerate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream bool
		err    error
		out    *schema.Message
		reason string
	}{
		{name: "before stream", stream: true, err: errors.New("HTTP request rejected"), reason: "model_error"},
		{name: "generate failure", err: context.Canceled, reason: "cancelled"},
		{name: "normal", out: schema.AssistantMessage("done", nil)},
		{name: "normal nil"},
		{name: "nonstream output cap", out: &schema.Message{Role: schema.Assistant, ReasoningContent: "checking", ResponseMeta: &schema.ResponseMeta{FinishReason: "max_tokens"}}, reason: "output_limit"},
		{name: "nonstream partial answer cap", out: &schema.Message{Role: schema.Assistant, Content: "partial answer", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, reason: "output_limit"},
		{name: "nonstream partial call cap", out: &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "inspect", Arguments: `{`}}}, ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, reason: "output_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			reg := NewRegistry()
			reg.SetHostNotify(rec.cb())
			acc := &streamAcc{reg: reg, agentID: "reader"}
			m := &generationModel{BaseChatModel: &generationTestModel{out: tc.out, err: tc.err}, acc: acc}
			if tc.stream {
				_, _ = m.Stream(context.Background(), nil)
			} else {
				_, _ = m.Generate(context.Background(), nil)
			}
			started := rec.ofKind(NotifyGenerationStarted)
			if len(started) != 1 || started[0].MessageID == "" {
				t.Fatalf("request never started: %+v", rec.all())
			}
			if tc.reason == "" {
				if len(rec.ofKind(NotifyGenerationCompleted)) != 1 || len(rec.ofKind(NotifyGenerationInterrupted)) != 0 {
					t.Fatalf("normal generate lost completion: %+v", rec.all())
				}
			} else {
				interrupted := rec.ofKind(NotifyGenerationInterrupted)
				if len(interrupted) != 1 || interrupted[0].MessageID != started[0].MessageID || !strings.Contains(interrupted[0].Text, `"reason":"`+tc.reason+`"`) {
					t.Fatalf("early failure lost cause: %+v", rec.all())
				}
			}
			acc.interruptGeneration(context.Canceled)
			acc.completeGeneration()
		})
	}
}

type generationTestModel struct {
	out *schema.Message
	err error
}

func (m *generationTestModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.out, m.err
}
func (m *generationTestModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, m.err
}

func TestGenerationCancellationRetainsHandoffAndWatchdogCause(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handoff bool
		ctx     context.Context
		want    string
	}{
		{name: "handoff", handoff: true, ctx: context.Background(), want: "engine_restart"},
		{name: "watchdog", ctx: expiredGenerationContext(), want: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			reg := NewRegistry()
			reg.SetHostNotify(rec.cb())
			h := &Handle{}
			h.handoff.cancelled = tc.handoff
			acc := &streamAcc{reg: reg, agentID: "reader", handle: h, ctx: tc.ctx}
			acc.beginTurn()
			acc.interruptGeneration(context.Canceled)
			acc.interruptGeneration(context.Canceled)
			n := rec.ofKind(NotifyGenerationInterrupted)
			if len(n) != 1 || !strings.Contains(n[0].Text, `"reason":"`+tc.want+`"`) {
				t.Fatalf("wrong cancellation cause: %+v", n)
			}
		})
	}
}
func expiredGenerationContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	return ctx
}

func TestGenerationModelQueuesActualOutputIdentitiesAndAbortedPendingRequests(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	acc := &streamAcc{reg: reg, agentID: "worker"}
	m := &generationModel{BaseChatModel: &chunkedModel{turns: []turnScript{{content: []string{"verified"}}}}, acc: acc}
	r, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	started := rec.ofKind(NotifyGenerationStarted)[0]
	if _, _, err := acc.drain(r); err != nil {
		t.Fatal(err)
	}
	if n := rec.ofKind(NotifyAgentMessage); len(n) != 1 || n[0].MessageID != started.MessageID {
		t.Fatalf("request identity did not reach output: %+v", rec.all())
	}
	r, err = m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	acc.interruptPending(context.Canceled)
	acc.interruptPending(context.Canceled)
	n := rec.ofKind(NotifyGenerationInterrupted)
	if len(n) != 1 || n[0].MessageID != rec.ofKind(NotifyGenerationStarted)[1].MessageID {
		t.Fatalf("undrained output lost interruption: %+v", rec.all())
	}
}

func TestProviderCancellationWordsCannotMasqueradeAsAnActualCancellation(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	acc := &streamAcc{reg: reg, agentID: "worker", ctx: context.Background()}
	acc.beginTurn()
	acc.interruptGeneration(errors.New("provider response mentions context canceled while decoding"))
	got := rec.ofKind(NotifyGenerationInterrupted)
	if len(got) != 1 || !strings.Contains(got[0].Text, `"reason":"model_error"`) {
		t.Fatalf("provider words fabricated cancellation: %+v", got)
	}
}

func TestPrefetchedModelStreamsKeepRequestIdentitiesInFIFOOrder(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	acc := &streamAcc{reg: reg, agentID: "worker"}
	m := &generationModel{BaseChatModel: &chunkedModel{turns: []turnScript{{calls: []schema.ToolCall{{ID: "inspect", Function: schema.FunctionCall{Name: "inspect", Arguments: "{}"}}}}, {content: []string{"verified"}}}}, acc: acc}
	first, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(acc.pending) != 2 {
		t.Fatalf("fixture did not prefetch two streams: %+v", acc.pending)
	}
	if _, calls, err := acc.drain(first); err != nil || len(calls) != 1 {
		t.Fatalf("first generation=%v %v", calls, err)
	}
	if text, _, err := acc.drain(second); err != nil || text != "verified" {
		t.Fatalf("second generation=%q %v", text, err)
	}
	starts, turns := rec.ofKind(NotifyGenerationStarted), rec.ofKind(NotifyTurn)
	if len(starts) != 2 || len(turns) != 2 || starts[0].MessageID != turns[0].MessageID || starts[1].MessageID != turns[1].MessageID || starts[0].MessageID == starts[1].MessageID {
		t.Fatalf("prefetch mixed output identities: %+v", rec.all())
	}
}

func TestTruncatedToolArgumentsCannotReachTheActualToolExecutor(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	var executed atomic.Int32
	reg.ModelBuilder = func(string, string) model.BaseChatModel { return &generationBudgetToolModel{} }
	_, _ = reg.RunWith(context.Background(), RunConfig{Task: "Inspect inputs.", MaxIterations: 1, ManagerTools: []tool.BaseTool{&fnTool{name: "inspect", fn: func(context.Context, string) (string, error) { executed.Add(1); return "must not execute", nil }}}}, nil)
	if executed.Load() != 0 {
		t.Fatalf("the model's truncated arguments reached the actual tool executor %d times", executed.Load())
	}
}

type generationBudgetToolModel struct{}

func (*generationBudgetToolModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("streaming required")
}
func (*generationBudgetToolModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r, w := schema.Pipe[*schema.Message](1)
	w.Send(&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "truncated", Function: schema.FunctionCall{Name: "inspect", Arguments: `{"path":"`}}}, ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, nil)
	w.Close()
	return r, nil
}

func TestGenerationGuardCloseImmediatelyClosesItsSource(t *testing.T) {
	source, writer := schema.Pipe[*schema.Message](0)
	defer writer.Close()
	guarded := guardGenerationTools(source)
	guarded.Close()
	if !writer.Send(schema.AssistantMessage("late output", nil), nil) {
		t.Fatal("closing the generation guard left the source reader open")
	}
}

func TestTruncatedToolFramePreservesItsPartialTextBeforeTheGuardRejectsIt(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	rec := &recorder{}
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &generationFrameModel{frames: []*schema.Message{{Role: schema.Assistant, Content: "partial answer", ReasoningContent: "partial thought", ToolCalls: []schema.ToolCall{{ID: "broken", Function: schema.FunctionCall{Name: "inspect", Arguments: `{"path":"`}}}, ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}}}
	}
	var invoked atomic.Int32
	_, _ = reg.RunWith(context.Background(), RunConfig{Task: "Inspect inputs.", MaxIterations: 1, ManagerTools: []tool.BaseTool{&fnTool{name: "inspect", fn: func(context.Context, string) (string, error) { invoked.Add(1); return "not allowed", nil }}}}, rec.cb())
	if invoked.Load() != 0 {
		t.Fatal("incomplete arguments executed")
	}
	reason, answer := rec.ofKind(NotifyReasoningDelta), rec.ofKind(NotifyDelta)
	if len(reason) != 1 || reason[0].Text != "partial thought" || len(answer) != 1 || answer[0].Text != "partial answer" {
		t.Fatalf("same-frame partial text was lost: %+v", rec.all())
	}
}

func TestLateHandoffCannotReplaceAnAlreadyKnownGenerationFailure(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{{errOutputBudget, "output_limit"}, {errors.New("model rejected request"), "model_error"}, {context.DeadlineExceeded, "timeout"}} {
		rec := &recorder{}
		reg := NewRegistry()
		reg.SetHostNotify(rec.cb())
		h := &Handle{}
		h.handoff.cancelled = true
		acc := &streamAcc{reg: reg, handle: h, agentID: "worker", ctx: context.Background()}
		acc.beginTurn()
		acc.interruptGeneration(tc.err)
		got := rec.ofKind(NotifyGenerationInterrupted)
		if len(got) != 1 || !strings.Contains(got[0].Text, `"reason":"`+tc.reason+`"`) {
			t.Fatalf("late handoff replaced the actual cause: %+v", got)
		}
	}
}

type generationFrameModel struct{ frames []*schema.Message }

func (*generationFrameModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("streaming required")
}
func (m *generationFrameModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray(m.frames), nil
}

func TestGenerationGuardAdmitsCompleteToolBatchesAndRefusesAnyTruncatedMember(t *testing.T) {
	valid := schema.ToolCall{ID: "first", Function: schema.FunctionCall{Name: "inspect", Arguments: `{}`}}
	another := schema.ToolCall{ID: "second", Function: schema.FunctionCall{Name: "inspect", Arguments: `{}`}}
	invalid := another
	invalid.Function.Arguments = `{"path":"`
	for _, tc := range []struct {
		name, finish string
		calls        []schema.ToolCall
		want         int
	}{{"normal stop", "stop", []schema.ToolCall{valid}, 1}, {"normal eof", "", []schema.ToolCall{valid}, 1}, {"valid length", "length", []schema.ToolCall{valid}, 1}, {"two valid length", "length", []schema.ToolCall{valid, another}, 2}, {"partial member length", "length", []schema.ToolCall{valid, invalid}, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			defer reg.Close()
			rec := &recorder{}
			reg.ModelBuilder = func(string, string) model.BaseChatModel {
				return &generationFrameModel{frames: []*schema.Message{{Role: schema.Assistant, ToolCalls: tc.calls}, {Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{FinishReason: tc.finish}}}}
			}
			var invoked atomic.Int32
			_, _ = reg.RunWith(context.Background(), RunConfig{Task: "Inspect inputs.", MaxIterations: 1, ManagerTools: []tool.BaseTool{&fnTool{name: "inspect", fn: func(context.Context, string) (string, error) { invoked.Add(1); return "verified", nil }}}}, rec.cb())
			if int(invoked.Load()) != tc.want {
				t.Fatalf("wrong tool admission: executed=%d want=%d events=%+v", invoked.Load(), tc.want, rec.all())
			}
			if tc.finish == "length" {
				if got := rec.ofKind(NotifyGenerationInterrupted); len(got) != 1 || !strings.Contains(got[0].Text, `"reason":"output_limit"`) {
					t.Fatalf("output cap lost its cause: %+v", rec.all())
				}
			}
		})
	}
}

func TestLateContextCancellationCannotReplaceAnExplicitModelFailure(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{cancelled, expiredGenerationContext()} {
		rec := &recorder{}
		reg := NewRegistry()
		reg.SetHostNotify(rec.cb())
		acc := &streamAcc{reg: reg, agentID: "worker", ctx: ctx}
		acc.beginTurn()
		acc.interruptGeneration(errors.New("model returned an explicit HTTP rejection"))
		got := rec.ofKind(NotifyGenerationInterrupted)
		if len(got) != 1 || !strings.Contains(got[0].Text, `"reason":"model_error"`) {
			t.Fatalf("late context state replaced explicit model failure: %+v", got)
		}
	}
}

func TestGenerationGuardPreservesKnownOutputLimitOverLateSourceCancellation(t *testing.T) {
	source, writer := schema.Pipe[*schema.Message](2)
	writer.Send(&schema.Message{Role: schema.Assistant, Content: "partial answer", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, nil)
	writer.Send(nil, context.Canceled)
	writer.Close()
	guarded := guardGenerationTools(source)
	defer guarded.Close()
	msg, err := guarded.Recv()
	if err != nil || msg.Content != "partial answer" {
		t.Fatalf("partial output lost: %+v %v", msg, err)
	}
	if _, err := guarded.Recv(); !errors.Is(err, errOutputBudget) {
		t.Fatalf("late source cancellation replaced the known output cap: %v", err)
	}
}

func TestTerminalFinishEvidenceSurvivesALaterContextCancellation(t *testing.T) {
	for _, finish := range []string{"length", "stop"} {
		t.Run(finish, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rec := &recorder{}
			reg := NewRegistry()
			reg.SetHostNotify(rec.cb())
			source := schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "visible output", ResponseMeta: &schema.ResponseMeta{FinishReason: finish}}})
			stream := schema.StreamReaderWithConvert(source, func(msg *schema.Message) (*schema.Message, error) { cancel(); return msg, nil })
			_, _, _ = (&streamAcc{reg: reg, agentID: "worker", ctx: ctx}).drain(stream)
			interrupted, complete := rec.ofKind(NotifyGenerationInterrupted), rec.ofKind(NotifyGenerationCompleted)
			if finish == "stop" {
				if len(interrupted) != 0 || len(complete) != 1 {
					t.Fatalf("late cancellation changed a completed output: %+v", rec.all())
				}
			} else if len(interrupted) != 1 || !strings.Contains(interrupted[0].Text, `"reason":"output_limit"`) || len(complete) != 0 {
				t.Fatalf("late cancellation erased the output cap: %+v", rec.all())
			}
		})
	}
}
