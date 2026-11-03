package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func waitForPrediction(t *testing.T, e *Engine, turnID string) store.Event {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := e.Store().ListTurnEvents(turnID)
		if err != nil {
			t.Fatalf("ListTurnEvents: %v", err)
		}
		for _, ev := range events {
			if ev.Kind == KindComposerPrediction && ev.Text != "" && ev.Err == "" {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn %s never recorded a next-step suggestion", turnID)
	return store.Event{}
}

func predictionEvents(t *testing.T, e *Engine, turnID string) []store.Event {
	t.Helper()
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	var out []store.Event
	for _, ev := range events {
		if ev.Kind == KindComposerPrediction {
			out = append(out, ev)
		}
	}
	return out
}

func TestNextStepStaysOffDuringOrdinaryTests(t *testing.T) {
	e := newTestEngine(t)
	if e.composerPredictionsOn() {
		t.Fatal("go test must not generate a next step unless that engine opts in")
	}
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if got := predictionEvents(t, e, turn.ID); len(got) != 0 {
		t.Fatalf("an ordinary test turn recorded a suggestion: %+v", got)
	}
}

func TestAFinishedReplyOffersANextStep(t *testing.T) {
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	ev := waitForPrediction(t, e, turn.ID)
	if ev.Text != provider.MockComposerPrediction || ev.AgentID != ComposerPredictionAgentID {
		t.Fatalf("suggestion = %+v", ev)
	}
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, ev.Text) {
			t.Fatalf("the suggestion entered the next prompt's history: %+v", msg)
		}
	}
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range calls {
		if call.AgentID == ComposerPredictionAgentID {
			found = true
		}
	}
	if !found {
		t.Fatal("the suggestion call is missing from the turn's trace")
	}
}

func TestNextStepSettingOffSkipsTheCall(t *testing.T) {
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	e.Config().Swarm.ComposerPredictions = false
	if e.composerPredictionsOn() {
		t.Fatal("an explicit off switch still generated suggestions")
	}
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if got := predictionEvents(t, e, turn.ID); len(got) != 0 {
		t.Fatalf("setting off still recorded a suggestion: %+v", got)
	}
}

func TestADeclinedNextStepClearsThePlaceholder(t *testing.T) {
	orig := predictionGenerate
	t.Cleanup(func() { predictionGenerate = orig })
	predictionGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return schema.AssistantMessage("NONE", nil), nil
	}
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	deadline := time.Now().Add(30 * time.Second)
	var got store.Event
	for time.Now().Before(deadline) {
		events := predictionEvents(t, e, turn.ID)
		if len(events) > 0 {
			got = events[len(events)-1]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Kind == "" {
		t.Fatal("a decline never reached Trace")
	}
	if got.Text != "" || got.Err == "" {
		t.Fatalf("NONE should not become a placeholder: %+v", got)
	}
}

func TestNextStepModelErrorIsTraceOnly(t *testing.T) {
	orig := predictionGenerate
	t.Cleanup(func() { predictionGenerate = orig })
	predictionGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("endpoint unavailable")
	}
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	deadline := time.Now().Add(30 * time.Second)
	var got store.Event
	for time.Now().Before(deadline) {
		events := predictionEvents(t, e, turn.ID)
		if len(events) > 0 {
			got = events[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Err == "" || got.Text != "" {
		t.Fatalf("model error = %+v", got)
	}
	if gotTurn, err := e.Store().GetTurn(turn.ID); err != nil || gotTurn.Status != store.TurnDone {
		t.Fatalf("a suggestion failure changed the turn: %+v %v", gotTurn, err)
	}
}

func TestNextStepPromptIsGeneric(t *testing.T) {
	prompt := predictionPrompt()
	for _, leak := range []string{"notes.md", "example", "e.g.", "run the tests", provider.MockComposerPrediction} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("prompt contains %q", leak)
		}
	}
	if !strings.Contains(prompt, "NONE") || !strings.Contains(prompt, "human's language") {
		t.Fatalf("prompt: %s", prompt)
	}
	orig := predictionGenerate
	t.Cleanup(func() { predictionGenerate = orig })
	var mu sync.Mutex
	var seen string
	predictionGenerate = func(_ context.Context, _ model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		if len(msgs) > 0 && msgs[0] != nil {
			mu.Lock()
			seen = msgs[0].Content
			mu.Unlock()
		}
		return schema.AssistantMessage("NONE", nil), nil
	}
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	deadline := time.Now().Add(30 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		mu.Lock()
		got = seen
		mu.Unlock()
		if got != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != prompt {
		t.Fatalf("the call did not use the suggestion prompt: %q", got)
	}
}

func TestANewTurnDropsAnInFlightSuggestion(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gen, ok := e.predictions.begin(th.ID)
	if !ok {
		t.Fatal("begin")
	}
	defer e.predictions.done()
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if e.predictions.current(th.ID, gen) {
		t.Fatal("occupying the conversation left the old suggestion current")
	}
	waitForTurn(t, e, turn.ID)
	if e.predictions.current(th.ID, gen) {
		t.Fatal("occupying the conversation left the old suggestion current")
	}
}

func TestAStaleSuggestionIsNotRecorded(t *testing.T) {
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	gen, ok := e.predictions.begin(th.ID)
	if !ok {
		t.Fatal("begin")
	}
	defer e.predictions.done()
	e.predictions.invalidate(th.ID)
	e.finishPrediction(th.ID, turn.ID, gen, provider.MockComposerPrediction, "")
	for _, ev := range predictionEvents(t, e, turn.ID) {
		if ev.Text == provider.MockComposerPrediction {
			t.Fatal("a stale suggestion was recorded")
		}
	}
}

func TestSuggestionSkipsARunningConversation(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ComposerPredictions = false
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	e.Config().Swarm.ComposerPredictions = true
	e.mu.Lock()
	rt := e.runtimes[th.ID]
	e.mu.Unlock()
	// The finished turn's defer still closes its original idle channel.
	// Reusing that turn id and marking it running makes the defer close
	// the channel a second time. A later turn id leaves that defer alone.
	replaced := make(chan struct{})
	rt.mu.Lock()
	rt.turnID = turn.ID + "-later"
	rt.idle = replaced
	rt.running = true
	rt.mu.Unlock()
	t.Cleanup(func() {
		rt.mu.Lock()
		rt.running = false
		rt.mu.Unlock()
		select {
		case <-replaced:
		default:
			close(replaced)
		}
	})
	gen, ok := e.predictions.begin(th.ID)
	if !ok {
		t.Fatal("begin")
	}
	defer e.predictions.done()
	e.runComposerPrediction(th.ID, turn.ID, turn.ProviderID, turn.Model, "hello", "answer", gen)
	if got := predictionEvents(t, e, turn.ID); len(got) != 0 {
		t.Fatalf("a running conversation recorded a suggestion: %+v", got)
	}
}

func TestSuggestionRecordsAMissingEndpoint(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ComposerPredictions = false
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	e.Config().Swarm.ComposerPredictions = true
	gen, ok := e.predictions.begin(th.ID)
	if !ok {
		t.Fatal("begin")
	}
	defer e.predictions.done()
	e.runComposerPrediction(th.ID, turn.ID, "missing-provider", "missing-model", "hello", "answer", gen)
	got := predictionEvents(t, e, turn.ID)
	if len(got) != 1 || got[0].Err == "" || got[0].Text != "" {
		t.Fatalf("missing endpoint = %+v", got)
	}
}

func TestScheduleSkipsATurnThatIsNotIdle(t *testing.T) {
	var absent *Engine
	absent.scheduleComposerPrediction("th", &store.Turn{}, store.TurnDone, false, false, "answer")

	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	turn := &store.Turn{ID: "tn", ThreadID: "th"}
	e.scheduleComposerPrediction("th", nil, store.TurnDone, false, false, "answer")
	e.scheduleComposerPrediction("th", turn, store.TurnCancelled, false, false, "answer")
	e.scheduleComposerPrediction("th", turn, store.TurnDone, true, false, "answer")
	e.scheduleComposerPrediction("th", turn, store.TurnDone, false, true, "answer")
	e.scheduleComposerPrediction("th", turn, store.TurnDone, false, false, " \n")
	if !e.predictions.stop(time.Second) {
		t.Fatal("stop")
	}
	e.scheduleComposerPrediction("th", turn, store.TurnDone, false, false, "answer")
	if got := predictionEvents(t, e, turn.ID); len(got) != 0 {
		t.Fatalf("a skipped suggestion was recorded: %+v", got)
	}
}

func TestNextStepPanicStaysInsideTheSuggestion(t *testing.T) {
	orig := predictionGenerate
	t.Cleanup(func() { predictionGenerate = orig })
	started := make(chan struct{})
	predictionGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		close(started)
		panic("suggestion failed")
	}
	e := newTestEngine(t)
	e.composerPredictionsInTest.Store(true)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the suggestion call never started")
	}
	if !e.predictions.stop(5 * time.Second) {
		t.Fatal("a panicked suggestion did not finish")
	}
	for _, ev := range predictionEvents(t, e, turn.ID) {
		if ev.Text != "" {
			t.Fatalf("a panic became a placeholder: %+v", ev)
		}
	}
	if got, err := e.Store().GetTurn(turn.ID); err != nil || got.Status != store.TurnDone {
		t.Fatalf("panic changed the turn: %+v %v", got, err)
	}
}

func TestPredictionPoolStopTimesOutWhileASuggestionRuns(t *testing.T) {
	var pool predictionPool
	if _, ok := pool.begin("th"); !ok {
		t.Fatal("begin")
	}
	if pool.stop(time.Millisecond) {
		t.Fatal("shutdown returned before the suggestion finished")
	}
	pool.done()
}

func TestPredictionPoolStopsCleanly(t *testing.T) {
	var pool predictionPool
	gen, ok := pool.begin("th")
	if !ok || gen == 0 || !pool.current("th", gen) {
		t.Fatalf("begin gen=%d ok=%v", gen, ok)
	}
	pool.invalidate("th")
	if pool.current("th", gen) {
		t.Fatal("invalidate left the generation current")
	}
	pool.done()
	if !pool.stop(time.Second) {
		t.Fatal("stop")
	}
	if _, ok := pool.begin("th"); ok {
		t.Fatal("begin after stop")
	}
	var engine *Engine
	engine.noteTurnOccupied("th")
	var zero Engine
	zero.noteTurnOccupied("th")
}

func TestSanitizePredictionKeepsOneSendableLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  NONE  ", ""},
		{"none.", ""},
		{"\"NONE\"", ""},
		{"  “keep the change”  \nmore", "keep the change"},
		{"a   b\tc", "a b c"},
		{strings.Repeat("字", predictionMaxRunes+5), strings.Repeat("字", predictionMaxRunes)},
	}
	for _, tc := range cases {
		if got := sanitizePrediction(tc.in); got != tc.want {
			t.Fatalf("sanitize %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPredictionInputUsesTheVisibleExchange(t *testing.T) {
	events := []store.Event{
		{Kind: "tool_result", Text: "secret tool output"},
		{Kind: KindComposerPrediction, Text: "old suggestion"},
		{Kind: KindUser, Text: "<user_request>\nkeep going\n</user_request>"},
		{Kind: "done", Text: "finished the step"},
	}
	got := predictionInput(events, "", "")
	if strings.Contains(got, "secret") || strings.Contains(got, "old suggestion") {
		t.Fatalf("input kept non-visible text: %s", got)
	}
	if !strings.Contains(got, "Human: keep going") || !strings.Contains(got, "Assistant: finished the step") {
		t.Fatalf("input: %s", got)
	}
	fallback := predictionInput(nil, "hello", "done now")
	if !strings.Contains(fallback, "Human: hello") || !strings.Contains(fallback, "Assistant: done now") {
		t.Fatalf("fallback: %s", fallback)
	}
	var long []store.Event
	for i := 0; i < 40; i++ {
		long = append(long, store.Event{Kind: KindUser, Text: strings.Repeat("a", 180)})
	}
	long = append(long, store.Event{Kind: "done", Text: "LASTMARKER"})
	bounded := predictionInput(long, "", "")
	if len([]rune(bounded)) > predictionContextRunes {
		t.Fatalf("context %d runes", len([]rune(bounded)))
	}
	if !strings.Contains(bounded, "LASTMARKER") {
		t.Fatal("the newest line was dropped")
	}
}
