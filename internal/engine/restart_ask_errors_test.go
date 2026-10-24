package engine

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Failure to read the durable question must not silently authorize a model run.
func TestRestartQuestionHistoryReadFailureIsReturned(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("unavailable history", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	messages := []adk.Message{schema.UserMessage(turn.UserText)}
	rt := e.runtimeFor(thread.ID)
	got, err := rt.resolveOrphanedAsk(t.Context(), turn, messages)
	if err == nil || len(got) != len(messages) || rt.awaitingAnswer() {
		t.Fatalf("unreadable history became a runnable recovery: messages=%+v err=%v", got, err)
	}
}

func TestRestartQuestionWithoutARuntimeOrTurnDoesNotCreateAWait(t *testing.T) {
	var rt *runtime
	if got, err := rt.resolveOrphanedAsk(t.Context(), nil, nil); err != nil || got != nil {
		t.Fatalf("missing runtime: messages=%+v err=%v", got, err)
	}
	rt = &runtime{}
	if got, err := rt.resolveOrphanedAsk(t.Context(), nil, nil); err != nil || got != nil {
		t.Fatalf("missing turn: messages=%+v err=%v", got, err)
	}
}
