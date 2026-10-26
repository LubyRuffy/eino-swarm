package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestRuntimeCueSourcesKeepHumanQuotesThroughTheRealNextTurn(t *testing.T) {
	e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The current question is answered."}, "stop")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.CompactKeepMessages = 1
	th, err := e.CreateThread("runtime source fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	old := &store.Turn{ThreadID: th.ID, UserText: "An earlier assignment."}
	if err := e.store.CreateTurn(old); err != nil {
		t.Fatal(err)
	}
	cues := []string{GoalContinueText(), workerCollectionCorrectionCue, workerCollectionCue([]string{"worker-a", "worker-b"}), modelRetryCue, scheduleCompletionCue, scheduleAnswerCorrectionCue, resumeCue, resumeWorkersCue, parkedWorkersCue}
	rows := []store.Message{{Role: "user", Content: old.UserText}}
	for _, cue := range cues {
		rows = append(rows, store.Message{Role: "user", Content: cue})
	}
	rows = append(rows, store.Message{Role: "user", Content: "a marked runtime instruction", Source: store.MessageSourceRuntime})
	if err := e.store.AppendMessages(th.ID, old.ID, rows); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(old.ID, store.TurnDone, "Earlier results were checked.", ""); err != nil {
		t.Fatal(err)
	}
	// A pre-upgrade human can have quoted a complete cue. Its actual input
	// event preserves it while identically-worded runtime rows stay transient.
	quote := &store.Turn{ThreadID: th.ID, UserText: modelRetryCue}
	if err := e.store.CreateTurn(quote); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(th.ID, quote.ID, []store.Message{{Role: "user", Content: modelRetryCue}}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: quote.ID, Kind: KindUser, Text: modelRetryCue}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(quote.ID, store.TurnDone, "Quoted text retained.", ""); err != nil {
		t.Fatal(err)
	}
	next, err := e.StartTurn(th.ID, GoalContinueText())
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, next.ID); got.Status != store.TurnDone {
		t.Fatalf("next turn failed: %+v", got)
	}
	actual := requests.snapshot()
	if len(actual) != 1 {
		t.Fatalf("manager requests=%d", len(actual))
	}
	userCounts := map[string]int{}
	for _, message := range actual[0].Messages {
		if message.Role == schema.User {
			userCounts[message.Content]++
		}
	}
	for _, cue := range cues {
		want := 0
		if cue == GoalContinueText() || cue == modelRetryCue {
			want = 1
		}
		if userCounts[cue] != want {
			t.Errorf("source lost for cue %q: got %d want %d", cue, userCounts[cue], want)
		}
	}
	if userCounts["a marked runtime instruction"] != 0 {
		t.Fatal("a runtime source was replayed despite its explicit marker")
	}
	stored, err := e.store.ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundHuman := false
	for _, row := range stored {
		if row.TurnID == next.ID && row.Content == GoalContinueText() {
			foundHuman = row.Source == store.MessageSourceHuman
		}
	}
	if !foundHuman {
		t.Fatal("the real human send did not persist its authoritative source")
	}
	previousGenerate := compactGenerate
	t.Cleanup(func() { compactGenerate = previousGenerate })
	var compactBody string
	compactGenerate = func(_ context.Context, _ model.BaseChatModel, input []*schema.Message) (*schema.Message, error) {
		compactBody = input[len(input)-1].Content
		return schema.AssistantMessage("The human asked a question and the prior result was checked.", nil), nil
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	for _, cue := range cues {
		if cue == GoalContinueText() || cue == modelRetryCue {
			if strings.Count(compactBody, cue) != 1 {
				t.Errorf("compact lost or duplicated the real human quote %q", cue)
			}
		} else if strings.Contains(compactBody, cue) {
			t.Errorf("compact included an old runtime correction %q", cue)
		}
	}
	if strings.Contains(compactBody, "a marked runtime instruction") {
		t.Fatal("compact included a source-marked runtime instruction")
	}
}

func TestRuntimeCueMarkersControlSameTurnCompaction(t *testing.T) {
	for _, cue := range []string{GoalContinueText(), workerCollectionCue([]string{"worker-a"}), modelRetryCue, resumeCue, scheduleCompletionCue} {
		runtime := runtimeUserMessage(cue)
		human := withMessageSource(schema.UserMessage(cue), store.MessageSourceHuman)
		if !isRuntimeMessage(runtime) || isRuntimeMessage(human) || isRuntimeMessage(nil) {
			t.Fatal("message origin is not explicit")
		}
		if runtime.Content != cue {
			t.Fatal("the current execution lost its correction")
		}
		if got := compactInputFromADK("", []*schema.Message{runtime, human}); strings.Count(got, cue) != 1 {
			t.Fatalf("ADK compact must preserve only the human quote: %q", got)
		}
		if got := compactInput("", []store.Message{{Role: "user", Source: store.MessageSourceRuntime, Content: cue}, {Role: "user", Source: store.MessageSourceHuman, Content: cue}}); strings.Count(got, cue) != 1 {
			t.Fatalf("stored compact must preserve only the human quote: %q", got)
		}
	}
	briefing := sessionMemoryInput("", []store.Event{{Seq: 1, Kind: KindGoalContinued, Text: GoalContinueText()}, {Seq: 2, Kind: KindUser, Text: GoalContinueText()}})
	if strings.Count(briefing, GoalContinueText()) != 1 || strings.Contains(briefing, "runtime:") {
		t.Fatalf("briefing confused runtime instructions with human quotes: %s", briefing)
	}
}

func TestLegacyRuntimeCueMatchingRequiresTheWholeKnownCue(t *testing.T) {
	for _, cue := range []string{GoalContinueText(), workerCollectionCorrectionCue, workerCollectionCue([]string{"worker-a"}), modelRetryCue, resumeCue, resumeWorkersCue, parkedWorkersCue, scheduleCompletionCue, scheduleAnswerCorrectionCue, goalSessionWrapSteer(), "[steer] " + goalSessionWrapSteer()} {
		if !legacyRuntimeCue(cue) || legacyRuntimeCue("quoted: "+cue) || legacyRuntimeCue(cue+" human suffix") {
			t.Fatalf("legacy matching must use exact runtime structures: %q", cue)
		}
	}
	interrupted := formatInterruptedWorkersCue([]stoppedWorker{{ID: "worker-a"}, {ID: "worker-b"}})
	if !legacyRuntimeCue(interrupted) || legacyRuntimeCue(interrupted+"\nmalformed") || legacyRuntimeCue(formatInterruptedWorkersCue([]stoppedWorker{{ID: ""}})) {
		t.Fatal("legacy interrupted-worker cues require complete structured IDs")
	}
	if legacyRuntimeCue(workerCollectionCorrectionCue+"\nUncollected worker IDs: {broken") || legacyRuntimeCue("human prose") {
		t.Fatal("malformed or ordinary text was classified as runtime")
	}
	if withMessageSource(nil, "human") != nil || messageSource(nil) != "" {
		t.Fatal("a missing message invented a source")
	}
	plain := schema.UserMessage("plain")
	if withMessageSource(plain, "") != plain || messageSource(plain) != "" {
		t.Fatal("a source-free message was rewritten")
	}
	for _, test := range []struct {
		in     UserInput
		source string
	}{{UserInput{}, "human"}, {UserInput{ContinueGoal: true}, "runtime"}, {UserInput{ContinueSchedule: true}, "schedule"}, {UserInput{ImplementPlan: true}, "plan"}} {
		if got := inputMessageSource(test.in); got != test.source {
			t.Fatalf("input source=%q want=%q", got, test.source)
		}
	}
}

func TestBriefingKeepsHumanQuotesOfRuntimeCues(t *testing.T) {
	for _, kind := range []string{KindUser, KindSteer} {
		for _, text := range []string{goalSessionWrapSteer(), "This work session is ending. A human is quoting a log."} {
			body := sessionMemoryInput("", []store.Event{{Seq: 1, Kind: kind, Text: text}})
			if !strings.Contains(body, "human: "+text) {
				t.Fatalf("human input was discarded by its wording: kind=%s body=%q", kind, body)
			}
		}
	}
}

func TestUnreadableLegacyRuntimeSourceFailsWithoutDiscardingHumanText(t *testing.T) {
	e := newTestEngine(t)
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.withoutRuntimeRows([]store.Message{{Role: "user", Content: modelRetryCue}}); err == nil || !strings.Contains(err.Error(), "classify legacy transcript source") {
		t.Fatalf("source failure was hidden: %v", err)
	}
	rows, err := e.withoutRuntimeRows([]store.Message{{Role: "user", Content: modelRetryCue, Source: store.MessageSourceHuman}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("authoritative human source needed unavailable text inference: %+v %v", rows, err)
	}
}

func TestAutomaticCompactFallbackOmitsRuntimeCorrectionsAtTheSummarizer(t *testing.T) {
	cue := workerCollectionCue([]string{"worker-a"})
	stub := &scriptedChatModel{out: "The verified prior assignment remains unfinished."}
	state := []*schema.Message{
		schema.SystemMessage("coordinate the current assignment"),
		withMessageSource(schema.UserMessage(GoalContinueText()), store.MessageSourceHuman),
		runtimeUserMessage(GoalContinueText()), runtimeUserMessage(cue),
		schema.AssistantMessage("Earlier checks were recorded.", nil),
		schema.UserMessage("Answer the current question."), overBudgetAssistant("Current reasoning."),
	}
	if _, err := rewriteWithOurs(t, stub, state); err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	input := joinedContent(stub.in)
	stub.mu.Unlock()
	if strings.Count(input, GoalContinueText()) != 1 || strings.Contains(input, cue) {
		t.Fatalf("the actual automatic fallback leaked runtime input or lost the human quote: %s", input)
	}
}

func TestSessionBriefingUsesHumanEventsWithoutRuntimeContinuationInstructions(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("briefing source fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, goalSessionWrapSteer())
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindGoalContinued, Text: GoalContinueText()})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: goalSessionWrapSteer()})
	oldGenerate := compactGenerate
	t.Cleanup(func() { compactGenerate = oldGenerate })
	var body string
	compactGenerate = func(_ context.Context, _ model.BaseChatModel, input []*schema.Message) (*schema.Message, error) {
		body = input[len(input)-1].Content
		return schema.AssistantMessage("A human quoted a runtime instruction and requested an explanation.", nil), nil
	}
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, GoalContinueText()) || !strings.Contains(body, "human: "+goalSessionWrapSteer()) {
		t.Fatalf("the real briefing request confused runtime instructions with human input: %s", body)
	}
}

func TestLegacyExactHumanCueKeepsOnlyItsOriginalTranscriptRow(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("legacy exact quote fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: modelRetryCue}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: modelRetryCue})
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", Content: modelRetryCue}, {Role: "assistant", Content: "A provisional answer."}, {Role: "user", Content: modelRetryCue}}); err != nil {
		t.Fatal(err)
	}
	messages, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, m := range messages {
		if m.Role == schema.User && m.Content == modelRetryCue {
			users++
			if messageSource(m) != store.MessageSourceHuman {
				t.Fatal("legacy human quote lost its resolved source")
			}
		}
	}
	if users != 1 {
		t.Fatalf("human quote replayed %d times, want original row only", users)
	}
}
