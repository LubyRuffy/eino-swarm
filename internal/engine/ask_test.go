package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

func TestParseAskArgumentsRejectsJunk(t *testing.T) {
	if _, err := ParseAskArguments(""); err == nil {
		t.Fatal("empty args must fail")
	}
	if _, err := ParseAskArguments("{"); err == nil {
		t.Fatal("broken JSON must fail")
	}
	if _, err := ParseAskArguments(`{"questions":[]}`); err == nil {
		t.Fatal("zero questions must fail")
	}
}

func TestNormalizeAskQuestionsInjectsOther(t *testing.T) {
	got, err := NormalizeAskQuestions([]AskQuestion{{
		ID:     "approach",
		Prompt: "Which approach?",
		Options: []AskOption{
			{ID: "safer", Label: "Safer"},
			{ID: "faster", Label: "Faster"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Options) != 3 {
		t.Fatalf("want host Other appended, got %+v", got)
	}
	last := got[0].Options[len(got[0].Options)-1]
	if last.ID != askOtherID || last.Label != askOtherLabel {
		t.Fatalf("host option=%+v", last)
	}
}

func TestAskAnswerUsesNormalizedModelQuestionID(t *testing.T) {
	questions, err := NormalizeAskQuestions([]AskQuestion{{
		ID: "test-window", Prompt: "When?", Options: []AskOption{
			{ID: "now", Label: "Now"}, {ID: "later", Label: "Later"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if questions[0].ID != "test_window" {
		t.Fatalf("normalized id=%q", questions[0].ID)
	}
	if got := askIdent("test\uFEFF-window"); got != "test_window" {
		t.Fatalf("punctuation-normalized id=%q", got)
	}
	if _, err := resolveAskAnswers(questions, AskAnswers{"test-window": {Answers: []string{"Now"}}}); err == nil {
		t.Fatal("raw model id must not be accepted as an answer key")
	}
	if _, err := resolveAskAnswers(questions, AskAnswers{"test_window": {Answers: []string{"Now"}}}); err != nil {
		t.Fatalf("normalized answer: %v", err)
	}
}

func TestNormalizeAskQuestionsRejectsAModelOther(t *testing.T) {
	_, err := NormalizeAskQuestions([]AskQuestion{{
		ID:     "approach",
		Prompt: "Which approach?",
		Options: []AskOption{
			{ID: "safer", Label: "Safer"},
			{ID: "other", Label: "Something else"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "free-form") {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeAskQuestionsBounds(t *testing.T) {
	two := []AskOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	_, err := NormalizeAskQuestions(nil)
	if err == nil {
		t.Fatal("need at least one question")
	}
	qs := make([]AskQuestion, 4)
	for i := range qs {
		qs[i] = AskQuestion{ID: string(rune('a' + i)), Prompt: "q", Options: two}
	}
	if _, err := NormalizeAskQuestions(qs); err == nil {
		t.Fatal("four questions must fail")
	}
	if _, err := NormalizeAskQuestions([]AskQuestion{{
		ID: "q", Prompt: "p", Options: []AskOption{{ID: "a", Label: "A"}},
	}}); err == nil {
		t.Fatal("one option must fail")
	}
}

func TestFormatAskResultAndFreeText(t *testing.T) {
	qs := []AskQuestion{{ID: "approach", Prompt: "Which?", Options: []AskOption{
		{ID: "safer", Label: "Safer"}, {ID: askOtherID, Label: askOtherLabel},
	}}}
	raw := FreeTextAnswers(qs, "  typed line  ")
	if raw["approach"].Answers[0] != "typed line" {
		t.Fatalf("answers=%+v", raw)
	}
	body := FormatAskResult(raw)
	if !strings.Contains(body, `"answers"`) || !strings.Contains(body, "typed line") {
		t.Fatalf("body=%s", body)
	}
	if FormatAskResult(nil) != `{"answers":{}}` {
		t.Fatalf("nil answers=%s", FormatAskResult(nil))
	}
}

func TestAnswerTurnWhenIdleIsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	err := e.AnswerTurn(th.ID, "c1", AskAnswers{"q": {Answers: []string{"x"}}})
	if !errors.Is(err, ErrIdle) {
		t.Fatalf("err=%v", err)
	}
	if err := e.AnswerTurnText(th.ID, "nope"); !errors.Is(err, ErrIdle) {
		t.Fatalf("text err=%v", err)
	}
}

func TestAskUserPausesThisTurnUntilAnswered(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "choose an approach")
	if err != nil {
		t.Fatal(err)
	}
	call := waitAskToolCall(t, sub, 15*time.Second)
	waitAskArmed(t, e, th.ID)
	if ids := e.AwaitingAnswer(); len(ids) != 1 || ids[0] != th.ID {
		t.Fatalf("AwaitingAnswer=%v want [%s]", ids, th.ID)
	}
	if err := e.AnswerTurn(th.ID, "wrong-id", AskAnswers{"approach": {Answers: []string{"safer"}}}); !errors.Is(err, ErrAskMismatch) {
		t.Fatalf("wrong id err=%v", err)
	}
	if err := e.AnswerTurn(th.ID, call.ToolCallID, AskAnswers{"approach": {Answers: []string{"safer"}}}); err != nil {
		t.Fatal(err)
	}
	cleared := time.Now().Add(2 * time.Second)
	for time.Now().Before(cleared) {
		if len(e.AwaitingAnswer()) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ids := e.AwaitingAnswer(); len(ids) != 0 {
		t.Fatalf("an answered ask is still listed: %v", ids)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
	if !hasKind(t, e, th.ID, swarm.NotifyToolResult.String()) {
		t.Fatal("the answer never became a tool_result")
	}
	if !hasKind(t, e, th.ID, swarm.NotifySpawned.String()) {
		t.Fatal("the same turn must continue after the answer")
	}
}

func TestAskUserComposerLineAnswersEveryQuestion(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "choose an approach")
	if err != nil {
		t.Fatal(err)
	}
	call := waitAskToolCall(t, sub, 15*time.Second)
	if call.ToolCallID == "" || call.ThreadID != th.ID || call.TurnID != turn.ID {
		t.Fatalf("the questionnaire call does not belong to this turn: %+v", call)
	}
	// StartTurn is the armed-question compatibility path. The live UI instead
	// uses /answers, which can stash a reply after tool_call but before waitAsk.
	// Exercise this call's real waiter rather than racing that notification gap.
	rt := e.runtimeFor(th.ID)
	ready := time.Now().Add(2 * time.Second)
	for {
		rt.mu.Lock()
		// The waiter can precede the durable event, leaving its cached ID
		// empty; the answer path then resolves the current open call below.
		armed := rt.turnID == turn.ID && rt.ask != nil && rt.ask.ch != nil &&
			len(rt.ask.questions) > 0 && (rt.ask.callID == "" || rt.ask.callID == call.ToolCallID)
		rt.mu.Unlock()
		if armed {
			break
		}
		if !time.Now().Before(ready) {
			t.Fatalf("this questionnaire did not start waiting before the composer answer: call=%q status=%+v", call.ToolCallID, e.Status(th.ID))
		}
		time.Sleep(time.Millisecond)
	}
	if id := rt.askCallIDOnTurn(turn.ID); id != call.ToolCallID {
		t.Fatalf("the armed questionnaire belongs to another open call: got %q want %q", id, call.ToolCallID)
	}
	if !time.Now().Before(ready) {
		t.Fatal("questionnaire readiness exceeded its original 2s deadline")
	}
	got, err := e.StartTurn(th.ID, "use the existing layout")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != turn.ID {
		t.Fatalf("composer Enter must stay on this turn, got %+v", got)
	}
	waitForTurn(t, e, turn.ID)
}

func TestInterruptCancelsAWaitingAsk(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "choose an approach")
	if err != nil {
		t.Fatal(err)
	}
	call := waitAskToolCall(t, sub, 15*time.Second)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnCancelled {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == call.ToolCallID {
			result = ev.Text
		}
	}
	if !strings.Contains(result, "cancelled") {
		t.Fatalf("ask result=%q", result)
	}
}

func TestResumeReArmsAnOrphanedAskInsteadOfSwallowingIt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "choose an approach")
	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{{
			"id":     "approach",
			"prompt": "Which approach should this work take?",
			"options": []map[string]any{
				{"id": "safer", "label": "Safer"},
				{"id": "faster", "label": "Faster"},
			},
		}},
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: ToolAskUser + "(" + string(args) + ")", ToolCallID: "c-ask",
	})
	e.closeOrphanedToolCalls(turn)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == "c-ask" {
			t.Fatalf("closing the leftover ask swallowed the question: %+v", ev)
		}
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil || n != 1 {
		t.Fatalf("resumed %d err=%v", n, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).AwaitingAnswer {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !e.Status(th.ID).AwaitingAnswer {
		t.Fatal("the leftover question was not waiting after resume")
	}
	if err := e.AnswerTurn(th.ID, "c-ask", AskAnswers{"approach": {Answers: []string{"Safer"}}}); err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
}

func TestAskUserToolReturnsJSONOnBadInput(t *testing.T) {
	tl, ok := AskUserTool(nil).(tool.InvokableTool)
	if !ok {
		t.Fatal("ask_user must be invokable")
	}
	info, err := tl.Info(context.Background())
	if err != nil || info.Name != ToolAskUser {
		t.Fatalf("info: %+v %v", info, err)
	}
	out, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("out=%s", out)
	}
}

func TestWorkersGetADenyStubNotABlockingAsk(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	reg := e.newTurnRegistry(func(role, id string) model.BaseChatModel { return nil }, set, nil)
	t.Cleanup(reg.Close)
	var deny tool.InvokableTool
	for _, bt := range reg.SubAgentTools {
		info, err := bt.Info(context.Background())
		if err != nil || info == nil || info.Name != ToolAskUser {
			continue
		}
		inv, ok := bt.(tool.InvokableTool)
		if !ok {
			t.Fatal("worker ask_user is not invokable")
		}
		deny = inv
	}
	if deny == nil {
		t.Fatal("workers must have ask_user so a mistaken call fails in JSON")
	}
	out, err := deny.InvokableRun(context.Background(), `{"questions":[{"id":"q","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "workers cannot ask") {
		t.Fatalf("out=%s", out)
	}
}

func waitAskToolCall(t *testing.T, sub *Subscription, timeout time.Duration) store.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-sub.C:
			if ev.Kind == swarm.NotifyToolCall.String() && isAskUserToolText(ev.Text) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for ask_user")
		}
	}
}

// waitAskArmed returns once the executor is blocked on the human. The
// tool-call notification is published before waitAsk arms, and the race
// detector widens that gap enough to fail every assertion that treats the
// notification as "the question is now waiting": a steer or composer Enter in
// the gap is a different case from one sent during a pause.
func waitAskArmed(t *testing.T, e *Engine, threadID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !e.Status(threadID).AwaitingAnswer {
		if time.Now().After(deadline) {
			t.Fatal("ask_user never started waiting for the human")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSteerDuringAskStaysInTheInbox(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "choose an approach")
	if err != nil {
		t.Fatal(err)
	}
	call := waitAskToolCall(t, sub, 15*time.Second)
	rt, waiting, reg := waitSteerAskArmed(t, e, turn, call)
	if err := e.Steer(th.ID, "prefer the safer path"); err != nil {
		t.Fatal(err)
	}
	if !e.Status(th.ID).AwaitingAnswer {
		t.Fatal("a steer must not swallow the questionnaire")
	}
	rt.mu.Lock()
	sameAsk := rt.running && rt.turnID == turn.ID && rt.reg == reg && rt.ask == waiting
	rt.mu.Unlock()
	if !sameAsk {
		t.Fatal("steer replaced this turn's armed questionnaire")
	}
	if reg == nil || !reg.HasPendingSteers() {
		t.Fatal("steer vanished instead of sitting in the inbox")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
}

func TestLateAskAnswerDoesNotFillTheNextQuestion(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	release := holdTurn(t, e, th.ID)
	defer release()
	rt := e.runtimeFor(th.ID)
	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{{
			"id":     "approach",
			"prompt": "Which approach?",
			"options": []map[string]any{
				{"id": "safer", "label": "Safer"},
				{"id": "faster", "label": "Faster"},
			},
		}},
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: rt.currentTurnID(),
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: ToolAskUser + "(" + string(args) + ")", ToolCallID: "c-ask",
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := rt.waitAsk(ctx, []AskQuestion{{
			ID: "approach", Prompt: "Which approach?", Options: []AskOption{
				{ID: "safer", Label: "Safer"}, {ID: "faster", Label: "Faster"}, {ID: askOtherID, Label: askOtherLabel},
			},
		}})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		ready := rt.ask != nil
		rt.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitAsk=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitAsk did not return")
	}
	if err := e.AnswerTurnText(th.ID, "stale"); !errors.Is(err, ErrAskMismatch) {
		t.Fatalf("late answer err=%v", err)
	}
}
