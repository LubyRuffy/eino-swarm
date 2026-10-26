package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Restart restores the original human pause. Its one approval must also
// authorize leaving the deferred candidate, rather than opening a second pause.
func TestRestartedDeferredAnswerNeedsOnlyOneHumanContinue(t *testing.T) {
	e, managers, checks := newScheduleAnswerTimingFixture(t, 1, 1, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, err := e.CreateThread("deferred answer continuation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "Inspect the current state.")
	const candidateID = "deferred-before-human-continue"
	const privateText = "The unpublished result was awaiting read-only verification."
	message := markPendingAnswer(schema.AssistantMessage(privateText, nil), candidateID)
	history := []*schema.Message{schema.UserMessage(turn.UserText), message}
	ready := make(chan struct{})
	restartReplayRuntime(t, e, turn, history, func(ctx context.Context, rt *runtime, reg *swarm.Registry) {
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		rt.waitToExtend(ctx, turn, 6, 3, reg)
	})
	row, failure := e.savePendingScheduleAnswer(turn, "answer", candidateID, pendingScheduleAnswer{
		AnswerID: candidateID, Text: []string{privateText}, Final: privateText, Transcript: history,
	})
	if failure != nil {
		t.Fatal(failure)
	}
	if err := e.store.SetScheduleAnswerRecovery(row.ID, 3, 3, time.Now().Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	close(ready)
	waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue })
	next := coldRestartReplayEngine(t, e)
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume=%d error=%v", count, err)
	}
	waitRestartState(t, func() bool { return next.Status(th.ID).AwaitingContinue })
	if managers.Load() != 0 || checks.Load() != 0 {
		t.Fatalf("restoring the pause requested a model: manager=%d classification=%d", managers.Load(), checks.Load())
	}
	if err := next.ContinueTurn(th.ID, true); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, next, turn.ID)
	if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 1 || checks.Load() != 1 {
		t.Fatalf("one Continue did not complete exactly one new answer: turn=%+v manager=%d classification=%d", got, managers.Load(), checks.Load())
	}
	events, err := next.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	pauses, approvals := 0, 0
	for _, event := range events {
		if event.Kind == KindMaxIterations {
			pauses++
		}
		if event.Kind == KindMaxIterationsContinued {
			approvals++
		}
		if strings.Contains(event.Text, privateText) {
			t.Fatalf("resuming a deferred answer exposed its prose: %+v", event)
		}
	}
	if pauses != 2 || approvals != 1 {
		t.Fatalf("original and restored pause must share one approval: pauses=%d approvals=%d", pauses, approvals)
	}
}

// A report reaches its failure limit before the answer itself is classified.
// Advancing work must durably reject that unexamined answer so restart cannot
// bring it back as the current pending output or replay the report tool.
func TestDeferredReportRejectsTheUnexaminedAnswerBeforeContinuingWork(t *testing.T) {
	const findings = "The inspection report is awaiting verification."
	const answer = "The inspection answer is awaiting verification."
	var managers, policies atomic.Int32
	receiptReached := make(chan struct{})
	releaseFinal := make(chan struct{})
	continued := make(chan []*schema.Message, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFinal) }) }
	t.Cleanup(release)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if request.Messages[0].Content == scheduleAnswerPrompt {
			policies.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch managers.Add(1) {
		case 1:
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "deferred-report", "type": "function", "function": map[string]any{
					"name": ToolReportSchedule, "arguments": `{"findings":"` + findings + `"}`,
				},
			}}}, "tool_calls")
		case 2:
			close(receiptReached)
			select {
			case <-releaseFinal:
			case <-r.Context().Done():
				return
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			continued <- request.Messages
			<-r.Context().Done()
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "deferred-report-fixture"
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("continue after report deferral", "", "")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 120, Title: "Deferred report inspection",
		Prompt: "Inspect remaining work.", CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Interrupt(th.ID) }()
	select {
	case <-receiptReached:
	case <-time.After(5 * time.Second):
		t.Fatal("report tool did not return its real receipt")
	}
	reportID := turn.ID + ":deferred-report"
	if err := e.store.SetScheduleAnswerRecovery(reportID, 2, 2, time.Time{}, false); err != nil {
		t.Fatal(err)
	}
	release()
	var messages []*schema.Message
	select {
	case messages = <-continued:
	case <-time.After(5 * time.Second):
		t.Fatal("deferred report blocked further authorized work")
	}
	if managers.Load() != 3 || policies.Load() != 1 || !e.Status(th.ID).Running {
		t.Fatalf("report deferral replayed or stopped work: managers=%d policies=%d status=%+v", managers.Load(), policies.Load(), e.Status(th.ID))
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), answer) || !strings.Contains(string(encoded), "deferred-report") || !strings.Contains(string(encoded), scheduleAnswerDeferredCue) {
		t.Fatalf("continuation retained unexamined prose or lost real report receipts: %s", encoded)
	}
	rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("private report and answer decisions=%+v error=%v", rows, err)
	}
	for _, row := range rows {
		switch row.Kind {
		case "report":
			if row.ID != reportID || row.State != "deferred" || row.ClassificationFailures != 3 {
				t.Fatalf("report did not retain its exhausted failure budget: %+v", row)
			}
		case "answer":
			if row.State != "rejected" || row.Attempts != 0 {
				t.Fatalf("unexamined answer can block recovery: %+v", row)
			}
		}
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	tools := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == "deferred-report" {
			tools++
		}
		if event.Kind == KindScheduleReport || strings.Contains(event.Text, answer) {
			t.Fatalf("deferral published unverified content: %+v", event)
		}
	}
	if tools != 1 {
		t.Fatalf("report tool was replayed: calls=%d", tools)
	}
}
