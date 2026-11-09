package engine

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// The ADK tool consumer can finish the model stream before its UI consumer
// publishes the original call. That gap must retain both the call and its ID.
type delayedAskPublication struct {
	e        *Engine
	turn     *store.Turn
	rt       *runtime
	ctx      context.Context
	release  func()
	callID   string
	endpoint *restartReplayEndpoint
}

type restartAskEOFModel struct {
	model.BaseChatModel
	ended chan error
}

func (m *restartAskEOFModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	stream, err := m.BaseChatModel.Stream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderWithConvert(stream, func(msg *schema.Message) (*schema.Message, error) {
		return msg, nil
	}, schema.WithOnEOF(func() (any, error) {
		m.ended <- ctx.Err()
		return nil, io.EOF
	})), nil
}

func startDelayedAskPublication(t *testing.T, kind swarm.NotifyKind) *delayedAskPublication {
	t.Helper()
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("pending decision", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue after the decision")
	reg := swarm.NewRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	idle, done := make(chan struct{}), make(chan struct{})
	gate, reached := make(chan struct{}), make(chan struct{})
	ended, called := make(chan error, 1), make(chan string, 1)
	var once sync.Once
	started := false
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() {
		release()
		cancel()
		if started {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("original manager did not join after publication was released")
			}
		}
		reg.Close()
	})
	rt := e.runtimeFor(thread.ID)
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("could not occupy the original turn")
	}
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	var identityMu sync.Mutex
	generationID := ""
	var gateOnce sync.Once
	var previews atomic.Int32
	args := restartReplayAskArguments(t, "decision")
	started = true
	go func() {
		defer close(done)
		defer rt.release(cancel, idle, turn.ID)
		_, _ = reg.RunWith(ctx, swarm.RunConfig{
			Instruction: "Ask for the required decision.", Task: turn.UserText,
			Model: &restartAskEOFModel{BaseChatModel: &headerWaitModel{msg: restartReplayAskMessage("original-ask", args)}, ended: ended},
			ManagerTools: []tool.BaseTool{AskUserTool(func(toolCtx context.Context, questions []AskQuestion) (AskAnswers, error) {
				called <- compose.GetToolCallID(toolCtx)
				return rt.waitAsk(toolCtx, questions)
			})},
		}, swarm.Callback(func(n swarm.Notification) {
			if n.Kind == swarm.NotifyGenerationStarted && n.AgentID == swarm.DefaultManagerID {
				identityMu.Lock()
				generationID = n.MessageID
				identityMu.Unlock()
			}
			if n.Kind == kind && n.ToolCallID == "original-ask" {
				identityMu.Lock()
				identified := n.MessageID != "" && n.MessageID == generationID
				identityMu.Unlock()
				if n.MessageID == "" {
					previews.Add(1)
				}
				if identified {
					gateOnce.Do(func() { close(reached) })
					<-gate
				}
			}
			acc.onNotify(n)
		}))
	}()
	waitRestartState(t, func() bool {
		select {
		case <-reached:
			return rt.awaitingAnswer()
		default:
			return false
		}
	})
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("source had not completed before handoff: %v", err)
		}
	default:
		t.Fatal("real Ask started without a source EOF")
	}
	select {
	case id := <-called:
		if id != "original-ask" {
			t.Fatalf("actual ADK tool context lost its call ID: %q", id)
		}
		if kind == swarm.NotifyToolCallDelta && previews.Load() == 0 {
			t.Fatal("producer preview was not forwarded before the UI gate")
		}
		return &delayedAskPublication{e: e, turn: turn, rt: rt, ctx: ctx, release: release, callID: id, endpoint: endpoint}
	default:
		t.Fatal("real tool context was not observed")
		return nil
	}
}

func TestActualManagerAskKeepsItsContextIDBeforeCallPublication(t *testing.T) {
	f := startDelayedAskPublication(t, swarm.NotifyToolCallDelta)
	f.rt.mu.Lock()
	id := ""
	if f.rt.ask != nil {
		id = f.rt.ask.callID
	}
	f.rt.mu.Unlock()
	if id != f.callID {
		t.Fatalf("waiter lost actual call identity before publication: got %q want %q", id, f.callID)
	}
	events, err := f.e.Store().ListTurnEvents(f.turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == f.callID {
			t.Fatal("fixture published the call before checking the actual tool context")
		}
	}
}

func TestRestartKeepsAskAcrossDelayedGenerationPublication(t *testing.T) {
	for _, kind := range []swarm.NotifyKind{swarm.NotifyToolCallDelta, swarm.NotifyToolCall} {
		t.Run(kind.String(), func(t *testing.T) {
			f := startDelayedAskPublication(t, kind)
			prepareCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			done := make(chan error, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				status, err := f.e.PrepareRestart(prepareCtx)
				if err == nil && !status.Ready {
					err = context.DeadlineExceeded
				}
				done <- err
			}()
			t.Cleanup(func() {
				f.release()
				select {
				case <-joined:
				case <-time.After(2 * time.Second):
					t.Error("PrepareRestart caller did not join after publication was released")
				}
			})
			for f.e.checkpointForTurn(f.turn.ID) == nil || f.ctx.Err() == nil {
				select {
				case <-prepareCtx.Done():
					t.Fatal("checkpoint and cancellation did not precede publication")
				case <-time.After(time.Millisecond):
				}
			}
			f.release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-prepareCtx.Done():
				t.Fatal("handoff did not finish within its original context budget")
			}
			f.e.Shutdown()
			if err := f.e.Store().Close(); err != nil {
				t.Fatal(err)
			}
			next := openRestartReplayEngine(t, f.e.cfg)
			events, err := next.Store().ListTurnEvents(f.turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == "original-ask" {
					t.Fatalf("restart resolved the unanswered executor call: %+v", event)
				}
			}
			if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
				t.Fatalf("resume=%d err=%v", n, err)
			}
			waitRestartState(t, func() bool {
				return next.Status(f.turn.ThreadID).AwaitingAnswer || !next.Status(f.turn.ThreadID).Running
			})
			if !next.Status(f.turn.ThreadID).AwaitingAnswer || len(f.endpoint.requests()) != 0 {
				t.Fatal("restart lost the original unanswered Ask")
			}
			if err := next.AnswerTurn(f.turn.ThreadID, f.callID, AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
				t.Fatal(err)
			}
			if got := waitRestartReplayTurn(t, next, f.turn.ID); got.ID != f.turn.ID || got.Status != store.TurnDone {
				t.Fatal("the accepted answer did not finish its original turn")
			}
		})
	}
}
