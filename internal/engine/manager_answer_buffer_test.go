package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestManagerAnswerAdmissionRetainsIdentityWithoutMutatingSharedHistory(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("answer ownership", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	b := newManagerAnswerBuffer(newAccumulator(e, th.ID, turn.ID, 0))
	old := schema.AssistantMessage("identical text", nil)
	pending := markPendingAnswer(schema.AssistantMessage("identical text", nil), b.id)
	pending.Extra["provider_state"] = "opaque"
	res := swarm.RunResult{Final: pending.Content, Transcript: []adk.Message{nil, old, pending}}
	rejected := b.reject(res)
	if rejected.Final != "" || len(rejected.Transcript) != 2 || rejected.Transcript[1] != old || pending.Content != "identical text" {
		t.Fatalf("rejection matched text instead of output identity: %+v", rejected)
	}
	accepted := b.accept(res)
	if accepted.Transcript[2].Extra[pendingScheduleAnswerKey] != nil || pending.Extra[pendingScheduleAnswerKey] != b.id || accepted.Transcript[2].Extra["provider_state"] != "opaque" {
		t.Fatal("admission mutated a shared message/extra map")
	}
	if markPendingAnswer(nil, b.id) != nil {
		t.Fatal("nil output acquired fabricated identity")
	}
	wrapped, _ := b.WrapModel(context.Background(), &scriptedChatModel{out: "actual answer"}, nil)
	out, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("inspect")})
	if err != nil || out.Content != "actual answer" || out.Extra[pendingScheduleAnswerKey] != b.id {
		t.Fatalf("Generate lost admission identity: %+v %v", out, err)
	}
	if _, err = wrapped.Stream(context.Background(), nil); err == nil {
		t.Fatal("underlying stream failure was hidden")
	}
}

func TestManagerAnswerBufferKeepsWorkerAndToolActivityLive(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("live activity", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	reg := swarm.NewRegistry()
	defer reg.Close()
	_, release := ownManagerTestFixture(context.Background(), e.runtimeFor(th.ID), turn.ID, reg)
	defer release()
	b := newManagerAnswerBuffer(newAccumulator(e, th.ID, turn.ID, 0))
	b.onNotify(swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: "worker", Text: "worker result"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: "manager", Text: "inspect the actual state"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "manager", Text: "candidate commentary", MessageID: "commentary"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: "manager", Text: "exec({})", ToolCallID: "tool"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyTurn, AgentID: "manager"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "manager", Text: "partial interrupted answer"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "manager"})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: "manager", Text: "accepted answer", MessageID: "answer"})
	before, _ := e.store.ListTurnEvents(turn.ID)
	worker, tool, reason := false, false, false
	for _, ev := range before {
		worker = worker || ev.AgentID == "worker"
		tool = tool || ev.Kind == "tool_call"
		reason = reason || ev.Kind == KindReasoning
		if isManagerAgent(ev.AgentID) && ev.Kind == "agent_message" {
			t.Fatalf("candidate prose escaped buffer: %+v", ev)
		}
	}
	if !worker || !tool || !reason {
		t.Fatalf("buffer suppressed live activity: %+v", before)
	}
	if err := b.publish(nil); err != nil {
		t.Fatal(err)
	}
	after, _ := e.store.ListTurnEvents(turn.ID)
	var bodies []string
	for _, ev := range after {
		if isManagerAgent(ev.AgentID) && ev.Kind == "agent_message" {
			bodies = append(bodies, ev.Text)
		}
	}
	if strings.Join(bodies, "|") != "candidate commentary|accepted answer" {
		t.Fatalf("admitted wrong outputs: %v", bodies)
	}
	if err := b.publish(nil); err != nil {
		t.Fatalf("second publication should be empty: %v", err)
	}
}

func TestAnswerAdmissionCannotOutliveStopOrDeletedOwnership(t *testing.T) {
	for _, mode := range []string{"stop", "abandoned", "deleted", "state-change", "storage-failure", "ended", "replaced", "durable-stop"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("admission owner", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			defer reg.Close()
			_, release := ownManagerTestFixture(context.Background(), rt, turn.ID, reg)
			defer release()
			var evidence []store.Schedule
			want := "answer_owner_stopped"
			switch mode {
			case "ended":
				release()
				want = "answer_owner_ended"
			case "replaced":
				release()
				next := mustStoredTurn(t, e, th.ID, store.Turn{})
				nextReg := swarm.NewRegistry()
				defer nextReg.Close()
				_, end := ownManagerTestFixture(context.Background(), rt, next.ID, nextReg)
				defer end()
				want = "answer_owner_ended"
			case "durable-stop":
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUserStopRequested, AgentID: "manager"})
			case "stop":
				rt.userStopped = true
			case "abandoned":
				rt.abandoned = true
			case "deleted":
				e.droppedTurns[turn.ID] = struct{}{}
				want = "answer_owner_deleted"
			case "state-change":
				evidence = []store.Schedule{{ID: "missing-wake"}}
				want = "schedule_state_changed"
			case "storage-failure":
				want = "answer_storage_failed"
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_answer BEFORE INSERT ON events WHEN NEW.kind='agent_message' BEGIN SELECT RAISE(ABORT, 'answer unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			failure := e.admitScheduleAnswer(th.ID, turn.ID, evidence, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: "candidate"}})
			if failure == nil || failure.Code != want {
				t.Fatalf("failure=%+v want=%s", failure, want)
			}
			if mode == "storage-failure" && errors.Unwrap(failure) == nil {
				t.Fatal("admission failure lost its diagnostic cause")
			}
			messages, _ := e.store.ListMessages(th.ID)
			if len(messages) != 0 {
				t.Fatalf("refused answer survived in replay: %+v", messages)
			}
		})
	}
}

func TestPendingModelIdentityPreservesTheUnderlyingError(t *testing.T) {
	want := errors.New("offline model refused")
	m := &pendingAnswerModel{inner: errStreamModel{err: want}, id: "attempt"}
	if out, err := m.Generate(context.Background(), nil); out != nil || !errors.Is(err, want) {
		t.Fatalf("Generate error changed: %+v %v", out, err)
	}
	if out, err := m.Stream(context.Background(), nil); out != nil || !errors.Is(err, want) {
		t.Fatalf("Stream error changed: %+v %v", out, err)
	}
}

type nilFirstAnswerModel struct{}

func (nilFirstAnswerModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, nil
}
func (nilFirstAnswerModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{nil, schema.AssistantMessage("answer", nil)}), nil
}

func TestPendingStreamIdentityBeginsAtTheFirstActualChunk(t *testing.T) {
	m := &pendingAnswerModel{inner: nilFirstAnswerModel{}, id: "attempt"}
	reader, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if chunk, err := reader.Recv(); err != nil || chunk != nil {
		t.Fatalf("empty transport chunk changed: %+v %v", chunk, err)
	}
	chunk, err := reader.Recv()
	if err != nil || chunk.Content != "answer" || chunk.Extra[pendingScheduleAnswerKey] != "attempt" {
		t.Fatalf("first output lost identity: %+v %v", chunk, err)
	}
}

func TestUnadmittedProseIsRemovedBeforeActualAutomaticCompaction(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("compact admission", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	ac := e.newAutoCompact(th.ID, turn.ID, th)
	ac.force = true
	ac.keep = 1
	summarizer := &scriptedChatModel{out: "Previously inspected tool facts are retained."}
	ac.model = summarizer
	claim := markPendingAnswer(schema.AssistantMessage(fixtureScheduleClaim, []schema.ToolCall{{ID: "check", Function: schema.FunctionCall{Name: "exec", Arguments: `{}`}}}), "pending-attempt")
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("coordinate the task"), schema.UserMessage("inspect the work"), claim, schema.ToolMessage("actual observed result", "check"), schema.UserMessage("continue inspecting")}}
	_, next, err := ac.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joinedContent(summarizer.in), fixtureScheduleClaim) || !strings.Contains(joinedContent(summarizer.in), "exec") {
		t.Fatalf("summary source lost tool identity or admitted prose: %s", joinedContent(summarizer.in))
	}
	if strings.Contains(joinedContent(next.Messages), fixtureScheduleClaim) || claim.Content != fixtureScheduleClaim {
		t.Fatal("pending prose survived folding or shared source was mutated")
	}
	got, _ := e.store.GetThread(th.ID)
	if strings.Contains(got.CompactSummary, fixtureScheduleClaim) {
		t.Fatal("unadmitted claim became a durable compacted fact")
	}
}
