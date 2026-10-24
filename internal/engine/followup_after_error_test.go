package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Hold the first real manager HTTP request until human queue rows are durable.
// Later calls return a fixed completed inspection, never task-word matching.
type followupErrorHTTP struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	inputs  []string
	timeout bool
}

func (f *followupErrorHTTP) unblock() { f.once.Do(func() { close(f.release) }) }

func (f *followupErrorHTTP) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.inputs...)
}

func newFollowupErrorHTTP(t *testing.T, timeout bool) (*Engine, *followupErrorHTTP) {
	t.Helper()
	f := &followupErrorHTTP{entered: make(chan struct{}), release: make(chan struct{}), timeout: timeout}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected offline request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request struct {
			Tools    []json.RawMessage `json:"tools"`
			Stream   bool              `json:"stream"`
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if len(request.Tools) == 0 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The completed inspection is retained."}, "stop")
			return
		}
		last := ""
		for _, message := range request.Messages {
			if message.Role == "user" {
				last = message.Content
			}
		}
		f.mu.Lock()
		f.inputs = append(f.inputs, last)
		n := len(f.inputs)
		f.mu.Unlock()
		if n == 1 {
			close(f.entered)
			select {
			case <-f.release:
			case <-r.Context().Done():
				return
			}
		}
		// The real adapter permits one header reconnect in the same model
		// call. Both requests must fail to reproduce a terminal first-byte error.
		if f.timeout && n <= 2 {
			<-r.Context().Done()
			return
		}
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"message":"fixture request rejected","type":"invalid_request_error","code":"invalid_request"}}`)
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The inspection completed."}, "stop")
	}))
	t.Cleanup(server.Close)
	t.Cleanup(f.unblock)
	e := newTestEngine(t)
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Models.Default = "queue-fixture"
		c.Models.Providers = []config.Provider{{ID: "queue-fixture", BaseURL: server.URL, Model: "offline", TimeoutSeconds: 4, FirstByteTimeoutSeconds: 1}}
		c.Swarm.AutoTitle = false
		c.Memory.Enabled, c.Memory.AutoReview = false, false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.pool = provider.New(e.cfg)
	return e, f
}

func followupErrorAwaitRequest(t *testing.T, fixture *followupErrorHTTP) {
	t.Helper()
	select {
	case <-fixture.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the actual Registry did not enter the offline HTTP model")
	}
}

func followupErrorTurns(t *testing.T, e *Engine, threadID string, want int, wait time.Duration) []store.Turn {
	t.Helper()
	until := time.Now().Add(wait)
	for time.Now().Before(until) {
		turns, err := e.Store().ListTurns(threadID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) >= want {
			return turns
		}
		time.Sleep(5 * time.Millisecond)
	}
	turns, _ := e.Store().ListTurns(threadID)
	queue, _ := e.ListFollowups(threadID)
	t.Fatalf("human queue did not start after the real failed turn: turns=%+v queue=%+v", turns, queue)
	return nil
}

func followupErrorTrace(t *testing.T, e *Engine, turn *store.Turn, fragment string) []store.Event {
	t.Helper()
	if turn.Status != store.TurnError || !strings.Contains(turn.Error, fragment) {
		t.Fatalf("the first request did not retain its actual model failure: %+v", turn)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyError.String() {
			terminal++
			if event.Err != turn.Error || event.TurnID != turn.ID || event.Seq <= 0 {
				t.Fatalf("original failure Trace changed: %+v", event)
			}
		}
		if event.Kind == swarm.NotifyDone.String() || event.Kind == KindModelRetry {
			t.Fatalf("a nonretryable failure became success or an automatic model retry: %+v", event)
		}
	}
	if terminal != 1 {
		t.Fatalf("failed turn has %d terminal error events", terminal)
	}
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	managers := 0
	for _, call := range calls {
		if call.AgentID == swarm.DefaultManagerID {
			managers++
			if !strings.Contains(call.Err, fragment) || call.TurnID != turn.ID {
				t.Fatalf("actual failed model-call receipt lost: %+v", call)
			}
		}
	}
	if managers != 1 {
		t.Fatalf("failed manager HTTP calls=%d, want one", managers)
	}
	return events
}

func followupErrorFIFO(t *testing.T, e *Engine, f *followupErrorHTTP, first *store.Turn, queue []string) {
	t.Helper()
	wait, failedHTTP := 3*time.Second, 1
	if f.timeout {
		wait += 2 * config.DefaultModelBackoffInitial
		failedHTTP = 2
	}
	turns := followupErrorTurns(t, e, first.ThreadID, len(queue)+1, wait)
	for i, text := range queue {
		turn := waitForTurn(t, e, turns[i+1].ID)
		if turn.UserText != text || turn.Status != store.TurnDone || turn.ScheduleContinue || turn.GoalContinue || turn.ScheduleRunID != "" {
			t.Fatalf("queued human input did not run independently in FIFO: %+v", turn)
		}
	}
	waitSettled(t, e, first.ThreadID)
	left, err := e.ListFollowups(first.ThreadID)
	if err != nil || len(left) != 0 {
		t.Fatalf("queue not drained: %+v %v", left, err)
	}
	got, err := e.Store().GetTurn(first.ID)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatalf("human follow-up rewrote the failed turn: before=%+v after=%+v err=%v", first, got, err)
	}
	requests := f.requests()
	if len(requests) != len(queue)+failedHTTP {
		t.Fatalf("manager HTTP requests=%d, want %d failed attempts plus %d human turns: %q", len(requests), failedHTTP, len(queue), requests)
	}
	for i, text := range queue {
		if requests[i+failedHTTP] != text {
			t.Fatalf("actual model input FIFO changed: %q", requests)
		}
	}
}

func TestFollowupAfterErrorOrdinaryHumanFIFO(t *testing.T) {
	e, fixture := newFollowupErrorHTTP(t, false)
	thread, err := e.CreateThread("queued inspection", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(thread.ID, "Inspect the initial input.")
	if err != nil {
		t.Fatal(err)
	}
	followupErrorAwaitRequest(t, fixture)
	queue := []string{"Inspect the next input.", "Inspect the final input."}
	for _, text := range queue {
		if _, err := e.EnqueueFollowup(thread.ID, text); err != nil {
			t.Fatal(err)
		}
	}
	fixture.unblock()
	first = waitForTurn(t, e, first.ID)
	events := followupErrorTrace(t, e, first, "fixture request rejected")
	followupErrorFIFO(t, e, fixture, first, queue)
	after, _ := e.Store().ListTurnEvents(first.ID)
	if !reflect.DeepEqual(events, after) {
		t.Fatal("human FIFO continuation changed the original failure Trace")
	}
}

// RunScheduleNow really claims and finishes the scheduled fire. The later human
// queue is not a retry of that failed check, and cannot cancel its future wake.
func TestFollowupAfterErrorScheduledTimeoutKeepsFailedRunAndFutureWake(t *testing.T) {
	e, fixture := newFollowupErrorHTTP(t, true)
	thread, err := e.CreateThread("scheduled inspection", "", "")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID,
		Title: "Recurring inspection", Prompt: "Inspect the current input.", EveryS: 3600, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.RunScheduleNow(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	followupErrorAwaitRequest(t, fixture)
	wake, err := e.GetSchedule(schedule.ID)
	if err != nil || wake.Status != store.ScheduleActive || !wake.NextRunAt.After(time.Now()) {
		t.Fatalf("the actual scheduled start lost its future wake: %+v %v", wake, err)
	}
	if schedule.TitleAuto {
		t.Fatal("lifecycle fixture must not start an unrelated title model task")
	}
	queue := []string{"Inspect a separate input.", "Report a separate result."}
	for _, text := range queue {
		if _, err := e.EnqueueFollowup(thread.ID, text); err != nil {
			t.Fatal(err)
		}
	}
	fixture.unblock()
	first = waitForTurn(t, e, first.ID)
	events := followupErrorTrace(t, e, first, "within 1s")
	run := waitForScheduleRun(t, e, first.ScheduleRunID)
	if !first.ScheduleContinue || run.Status != store.ScheduleRunError || !run.Unread || run.Summary != first.Error || run.TurnID != first.ID {
		t.Fatalf("the actual failed schedule/run lost its error and unread evidence: turn=%+v run=%+v", first, run)
	}
	followupErrorFIFO(t, e, fixture, first, queue)
	afterRun, err := e.Store().GetRun(run.ID)
	if err != nil || !reflect.DeepEqual(afterRun, run) {
		t.Fatalf("human FIFO rewrote the failed run: before=%+v after=%+v err=%v", run, afterRun, err)
	}
	afterWake, err := e.GetSchedule(wake.ID)
	if err != nil || afterWake.Status != store.ScheduleActive || !afterWake.NextRunAt.Equal(wake.NextRunAt) {
		t.Fatalf("human FIFO changed the future scheduled wake: before=%+v after=%+v err=%v", wake, afterWake, err)
	}
	afterEvents, _ := e.Store().ListTurnEvents(first.ID)
	if !reflect.DeepEqual(events, afterEvents) {
		t.Fatal("human FIFO rewrote the failed schedule Trace")
	}
}

func TestFollowupAfterErrorWithoutHumanQueueDoesNotStartAnotherTurn(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprint("scheduled=", scheduled), func(t *testing.T) {
			e, fixture := newFollowupErrorHTTP(t, false)
			thread, err := e.CreateThread("inspection without a queue", "", "")
			if err != nil {
				t.Fatal(err)
			}
			var first *store.Turn
			if scheduled {
				wake, createErr := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID,
					Title: "Future inspection", Prompt: "Inspect the current input.", EveryS: 3600, CreatedBy: store.ScheduleCreatedHuman})
				if createErr != nil {
					t.Fatal(createErr)
				}
				first, err = e.RunScheduleNow(wake.ID)
			} else {
				if err := e.SetThreadGoal(thread.ID, "Inspect the fixture state."); err != nil {
					t.Fatal(err)
				}
				first, err = e.StartTurn(thread.ID, "Inspect the current input.")
			}
			if err != nil {
				t.Fatal(err)
			}
			followupErrorAwaitRequest(t, fixture)
			fixture.unblock()
			first = waitForTurn(t, e, first.ID)
			followupErrorTrace(t, e, first, "fixture request rejected")
			waitSettled(t, e, thread.ID)
			turns, err := e.Store().ListTurns(thread.ID)
			if err != nil || len(turns) != 1 || len(fixture.requests()) != 1 {
				t.Fatalf("an empty queue restarted failed work: %+v requests=%q err=%v", turns, fixture.requests(), err)
			}
			if scheduled {
				if status := e.Status(thread.ID); status.Running || !status.Waiting {
					t.Fatalf("future wake should remain waiting without starting another check: %+v", status)
				}
			} else if got, err := e.Store().GetThread(thread.ID); err != nil || !got.GoalBlocked {
				t.Fatalf("empty human queue bypassed the existing blocked-goal decision: %+v %v", got, err)
			}
		})
	}
}

func TestFollowupAfterErrorStopAndRestartLeaveHumanQueueUntouched(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint("restart=", restart), func(t *testing.T) {
			e, fixture := newFollowupErrorHTTP(t, false)
			thread, err := e.CreateThread("interrupted inspection", "", "")
			if err != nil {
				t.Fatal(err)
			}
			first, err := e.StartTurn(thread.ID, "Inspect the current input.")
			if err != nil {
				t.Fatal(err)
			}
			followupErrorAwaitRequest(t, fixture)
			one, err := e.EnqueueFollowup(thread.ID, "Inspect another input.")
			if err != nil {
				t.Fatal(err)
			}
			two, err := e.EnqueueFollowup(thread.ID, "Inspect the remaining input.")
			if err != nil {
				t.Fatal(err)
			}
			if restart {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				status, err := e.PrepareRestart(ctx)
				if err != nil || !status.Ready || status.ThreadsCheckpointed != 1 {
					t.Fatalf("actual restart checkpoint did not finish: %+v %v", status, err)
				}
				got, err := e.Store().GetTurn(first.ID)
				if err != nil || got.Status != store.TurnRunning || e.checkpointForTurn(first.ID) == nil {
					t.Fatalf("restart failed to preserve the real running turn: %+v %v", got, err)
				}
			} else {
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				if got := waitForTurn(t, e, first.ID); got.Status != store.TurnCancelled {
					t.Fatalf("Stop did not cancel the actual model run: %+v", got)
				}
			}
			fixture.unblock()
			waitSettled(t, e, thread.ID)
			assertFollowupRows(t, e, thread.ID, one, two)
			turns, err := e.Store().ListTurns(thread.ID)
			if err != nil || len(turns) != 1 || len(fixture.requests()) != 1 {
				t.Fatalf("Stop/restart consumed queued work: %+v requests=%q err=%v", turns, fixture.requests(), err)
			}
		})
	}
}

// Release two manual sends at the same real FinishTurn boundary as automatic
// FIFO delivery. The callback only controls interleaving after the DB commit;
// it never invents an error turn, calls flush, or substitutes a model result.
func TestFollowupAfterErrorConcurrentManualDeliveryDoesNotDuplicate(t *testing.T) {
	e, fixture := newFollowupErrorHTTP(t, false)
	thread, err := e.CreateThread("concurrent delivery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(thread.ID, "Inspect the current input.")
	if err != nil {
		t.Fatal(err)
	}
	followupErrorAwaitRequest(t, fixture)
	queued, err := e.EnqueueFollowup(thread.ID, "Inspect the explicitly queued input.")
	if err != nil {
		t.Fatal(err)
	}
	boundary, race := make(chan struct{}), make(chan struct{})
	var reached, release sync.Once
	unblock := func() { release.Do(func() { close(race) }) }
	name := "test:followup-after-error-committed-turn"
	callback := e.Store().DB().Callback().Update()
	if err := callback.After("gorm:commit_or_rollback_transaction").Register(name, func(tx *gorm.DB) {
		fields, ok := tx.Statement.Dest.(map[string]any)
		if !ok || tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "turns" || fields["status"] != store.TurnError {
			return
		}
		reached.Do(func() {
			close(boundary)
			select {
			case <-race:
			case <-time.After(5 * time.Second):
				t.Error("manual delivery never released the committed finish boundary")
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = callback.Remove(name) })
	t.Cleanup(unblock)
	fixture.unblock()
	select {
	case <-boundary:
	case <-time.After(5 * time.Second):
		t.Fatal("the actual failed FinishTurn did not reach its committed boundary")
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-race
			_, err := e.SteerFollowup(thread.ID, queued.ID)
			results <- err
		}()
	}
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, ErrBusy) {
				t.Fatalf("manual queue claim returned an unexpected failure: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent manual queue claim did not return")
		}
	}
	turns := followupErrorTurns(t, e, thread.ID, 2, 3*time.Second)
	waitForTurn(t, e, turns[1].ID)
	waitSettled(t, e, thread.ID)
	turns, err = e.Store().ListTurns(thread.ID)
	if err != nil || len(turns) != 2 || turns[1].UserText != queued.Text || turns[1].Status != store.TurnDone {
		t.Fatalf("automatic/manual delivery duplicated or lost the queued request: %+v %v", turns, err)
	}
	assertFollowupRows(t, e, thread.ID)
	requests := fixture.requests()
	if len(requests) != 2 || requests[1] != queued.Text {
		t.Fatalf("queued text reached the actual model more than once: %q", requests)
	}
	failed, err := e.Store().GetTurn(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	followupErrorTrace(t, e, failed, "fixture request rejected")
}
