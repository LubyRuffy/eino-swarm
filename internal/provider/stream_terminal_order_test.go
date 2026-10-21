package provider

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Session shutdown may close storage as soon as its stream consumer returns.
// A blocked recorder must therefore also hold back the terminal observation.
func TestStreamTerminalWaitsForItsCallRecord(t *testing.T) {
	for _, failure := range []string{"receive-error", "caller-cancel", "truncated", "eof"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cause := errors.New("offline stream broke")
				switch failure {
				case "caller-cancel":
					cause = context.Canceled
				case "truncated":
					cause = io.ErrUnexpectedEOF
				case "eof":
					cause = io.EOF
				}
				entered, persist := make(chan struct{}), make(chan struct{})
				var records []CallRecord
				rec := func(row CallRecord) {
					close(entered)
					<-persist
					records = append(records, row)
				}
				inner := &terminalStreamModel{cause: cause, waitCancel: failure == "caller-cancel"}
				m := wrap(inner, "session-memory", config.Provider{ID: "selected", Model: "offline"}, "", 0, rec)
				stream, err := m.Stream(ctx, []*schema.Message{schema.UserMessage("retain the verified state")})
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				first, err := stream.Recv()
				if err != nil || first.Content != "partial output" {
					t.Fatalf("partial stream was changed: %+v, %v", first, err)
				}
				terminal := make(chan error, 1)
				go func() { _, err := stream.Recv(); terminal <- err }()
				cancel()
				<-entered
				// Wait establishes whether the consumer has received the terminal
				// or is blocked behind persistence, without a timing assumption.
				synctest.Wait()
				var got error
				early := false
				select {
				case got = <-terminal:
					early = true
				default:
				}
				close(persist)
				synctest.Wait()
				if !early {
					got = <-terminal
				}
				if early {
					t.Fatal("the stream exposed its terminal before its call record was persisted")
				}
				if !errors.Is(got, cause) {
					t.Fatalf("stream cause was lost: got %v want %v", got, cause)
				}
				if len(records) != 1 {
					t.Fatalf("one physical stream must record once: %+v", records)
				}
				row := records[0]
				if row.AgentID != "session-memory" || row.ProviderID != "selected" || row.Model != "offline" ||
					row.InputMsgs != 1 || row.OutputChars != len("partial output")+len("thinking") ||
					row.PromptTokens != 9 || row.CompletionTokens != 2 || row.TotalTokens != 11 ||
					row.CachedTokens != 4 || row.ReasoningTokens != 1 {
					t.Fatalf("the terminal record lost attribution or partial usage: %+v", row)
				}
				if failure == "eof" {
					if row.Err != nil {
						t.Fatalf("normal EOF became a failed call: %v", row.Err)
					}
				} else {
					var modelError *CallError
					if !errors.As(got, &modelError) || row.Err != cause {
						t.Fatalf("failure origin changed: consumer=%v record=%v", got, row.Err)
					}
				}
			})
		})
	}
}

func TestClosingTheConsumerDuringPersistenceDoesNotDuplicateTheFailedCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("offline terminal after partial usage")
		entered, persist := make(chan struct{}), make(chan struct{})
		var records []CallRecord
		m := wrap(&terminalStreamModel{cause: cause}, "worker-1", config.Provider{ID: "selected"}, "", 0,
			func(row CallRecord) {
				close(entered)
				<-persist
				records = append(records, row)
			})
		stream, err := m.Stream(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		<-entered
		stream.Close()
		close(persist)
		synctest.Wait()
		if len(records) != 1 || records[0].AgentID != "worker-1" || records[0].Err != cause || records[0].TotalTokens != 11 {
			t.Fatalf("a closed consumer must retain one failed call and its last usage: %+v", records)
		}
	})
}

type terminalStreamModel struct {
	model.BaseChatModel
	cause      error
	waitCancel bool
}

func (m *terminalStreamModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](2)
	go func() {
		defer sw.Close()
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "partial output", ReasoningContent: "thinking",
			ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
				PromptTokens: 9, CompletionTokens: 2, TotalTokens: 11,
				PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 4},
				CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 1},
			}}}, nil)
		if m.waitCancel {
			<-ctx.Done()
		}
		if !errors.Is(m.cause, io.EOF) {
			sw.Send(nil, m.cause)
		}
	}()
	return sr, nil
}
