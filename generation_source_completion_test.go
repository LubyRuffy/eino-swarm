package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type sourceCompletionModel struct {
	model.BaseChatModel
	message *schema.Message
	err     error
}

func (m *sourceCompletionModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reader, writer := schema.Pipe[*schema.Message](2)
	writer.Send(m.message, nil)
	if m.err != nil {
		writer.Send(nil, m.err)
	}
	writer.Close()
	return reader, nil
}

func consumeGenerationSource(reader *schema.StreamReader[*schema.Message]) error {
	for {
		if _, err := reader.Recv(); err != nil {
			return err
		}
	}
}

// A late UI consumer must retain completed provider output, without turning a
// real source error, truncated call, or earlier cancellation into completion.
func TestDelayedGenerationConsumerRetainsSourceTerminalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, finish, arguments, reason string
		sourceError                     error
		cancelBeforeEOF                 bool
		calls                           int
	}{
		{name: "clean source before cancellation", arguments: `{}`, calls: 1},
		{name: "source failure before cancellation", arguments: `{}`, sourceError: io.ErrUnexpectedEOF, reason: "model_error"},
		{name: "cancel before source EOF", arguments: `{}`, cancelBeforeEOF: true, reason: "cancelled"},
		{name: "explicit finish survives cancellation", arguments: `{}`, finish: "stop", cancelBeforeEOF: true, calls: 1},
		{name: "truncated call stays rejected", arguments: `{"path":"`, finish: "length", reason: "output_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reg := NewRegistry()
			defer reg.Close()
			rec := &recorder{}
			reg.SetHostNotify(rec.cb())
			acc := &streamAcc{reg: reg, agentID: DefaultManagerID}
			wrapped := &generationModel{acc: acc, BaseChatModel: &sourceCompletionModel{
				message: &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "inspection", Function: schema.FunctionCall{Name: "inspect", Arguments: tc.arguments}}}, ResponseMeta: &schema.ResponseMeta{FinishReason: tc.finish}}, err: tc.sourceError}}
			stream, err := wrapped.Stream(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			readers := stream.Copy(2)
			defer readers[0].Close()
			defer readers[1].Close()
			if tc.cancelBeforeEOF {
				cancel()
			}
			sourceErr := consumeGenerationSource(readers[0])
			if tc.sourceError != nil && !errors.Is(sourceErr, tc.sourceError) {
				t.Fatalf("source error changed: %v", sourceErr)
			}
			cancel()
			_, calls, _ := acc.drain(readers[1])
			if len(calls) != tc.calls || len(rec.ofKind(NotifyToolCall)) != tc.calls {
				t.Fatalf("wrong publication after delayed consumption: calls=%d want=%d", len(calls), tc.calls)
			}
			interrupted, completed := rec.ofKind(NotifyGenerationInterrupted), rec.ofKind(NotifyGenerationCompleted)
			if tc.reason == "" {
				if len(interrupted) != 0 || len(completed) != 1 {
					t.Fatal("completed source was rewritten as an interrupted generation")
				}
			} else {
				var cause GenerationInterruption
				if len(interrupted) != 1 || json.Unmarshal([]byte(interrupted[0].Text), &cause) != nil || cause.Reason != tc.reason || len(completed) != 0 {
					t.Fatalf("source terminal cause changed: %+v", interrupted)
				}
			}
		})
	}
}

// The accumulator is reused across manager generations. An old EOF token
// cannot make a later cancelled generation look successfully completed.
func TestSourceCompletionBelongsToOnlyItsOwnGeneration(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	rec := &recorder{}
	reg.SetHostNotify(rec.cb())
	acc := &streamAcc{reg: reg, agentID: DefaultManagerID}
	wrapped := &generationModel{acc: acc, BaseChatModel: &sourceCompletionModel{message: schema.AssistantMessage("verified output", nil)}}
	for index := 0; index < 2; index++ {
		ctx, cancel := context.WithCancel(t.Context())
		stream, err := wrapped.Stream(ctx, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		readers := stream.Copy(2)
		if index == 1 {
			cancel()
		}
		if err := consumeGenerationSource(readers[0]); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		cancel()
		_, _, _ = acc.drain(readers[1])
		readers[0].Close()
		readers[1].Close()
	}
	started, completed, interrupted := rec.ofKind(NotifyGenerationStarted), rec.ofKind(NotifyGenerationCompleted), rec.ofKind(NotifyGenerationInterrupted)
	if len(started) != 2 || started[0].MessageID == started[1].MessageID || len(completed) != 1 || completed[0].MessageID != started[0].MessageID || len(interrupted) != 1 || interrupted[0].MessageID != started[1].MessageID {
		t.Fatal("a source completion was dropped or reused for a different generation")
	}
}
