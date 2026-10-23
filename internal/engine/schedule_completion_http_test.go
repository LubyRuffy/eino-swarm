package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

type scheduleCompletionRequest struct {
	Stream     bool              `json:"stream"`
	ToolChoice string            `json:"tool_choice"`
	Messages   []*schema.Message `json:"messages"`
	Tools      []struct {
		Function struct{ Name string } `json:"function"`
	} `json:"tools"`
}

type scheduleCompletionHTTP struct {
	mu       sync.Mutex
	requests []scheduleCompletionRequest
}

func (f *scheduleCompletionHTTP) snapshot() []scheduleCompletionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scheduleCompletionRequest(nil), f.requests...)
}

// The actual Registry, ADK and HTTP adapter must allow a terminal text reply
// while tools remain available; removing the tools would hide a forced choice.
func newScheduleCompletionHTTPEngine(t *testing.T, reply func(http.ResponseWriter, *http.Request, int, scheduleCompletionRequest)) (*Engine, *scheduleCompletionHTTP) {
	t.Helper()
	f := &scheduleCompletionHTTP{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode offline request: %v", err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The stored evidence is unchanged."}, "stop")
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, request)
		step := len(f.requests)
		f.mu.Unlock()
		reply(w, r, step, request)
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-scheduled-completion"
	e.pool = provider.New(e.cfg)
	return e, f
}

func writeScheduleCompletionTool(w http.ResponseWriter, request scheduleCompletionRequest, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": "scheduled-completion-call", "type": "function",
		"function": map[string]any{"name": name, "arguments": args},
	}}}, "tool_calls")
}

func assertScheduleRequestsKeepToolsOptional(t *testing.T, requests []scheduleCompletionRequest) {
	t.Helper()
	for i, request := range requests {
		if request.ToolChoice != "" && request.ToolChoice != "auto" {
			t.Errorf("request %d forced a tool choice: %q", i+1, request.ToolChoice)
		}
		reportAvailable := false
		for _, tool := range request.Tools {
			reportAvailable = reportAvailable || tool.Function.Name == ToolReportSchedule
		}
		if !reportAvailable {
			t.Errorf("request %d removed the optional report tool", i+1)
		}
	}
}

func startScheduleCompletionTurn(t *testing.T, e *Engine, trigger string) (*store.Schedule, *store.Turn) {
	t.Helper()
	clk := newScheduleClock()
	e.now = clk.Now
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	schedule := armDueWake(t, e, clk, thread.ID, 60)
	if trigger == "automatic" {
		e.fireDueSchedules()
		return schedule, onlyContextFlowTurn(t, e, thread.ID)
	}
	turn, err := e.RunScheduleNow(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	return schedule, turn
}

func TestScheduledTextRepliesDoNotRequireTools(t *testing.T) {
	for _, trigger := range []string{"run now", "automatic"} {
		for _, answer := range []string{"Done.", "状态已确认。", strings.Repeat("The recorded result is available. ", 100)} {
			name := "long text"
			if len(answer) < 100 {
				name = answer
			}
			t.Run(trigger+"/"+name, func(t *testing.T) {
				e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
				})
				schedule, turn := startScheduleCompletionTurn(t, e, trigger)
				got := waitForTurn(t, e, turn.ID)
				run := waitForScheduleRun(t, e, turn.ScheduleRunID)
				if got.Status != store.TurnDone || run.Status != store.ScheduleRunFindings || !run.Unread || run.Summary != strings.TrimSpace(answer) {
					t.Fatalf("a terminal text result was rejected: turn=%+v run=%+v", got, run)
				}
				requests := fixture.snapshot()
				assertScheduleRequestsKeepToolsOptional(t, requests)
				if len(requests) != 1 {
					t.Fatalf("a terminal text result was requested again: requests=%d", len(requests))
				}
				events, err := e.Store().ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					if event.Kind == KindModelRetry || event.Kind == KindScheduleReport || event.Kind == swarm.NotifyToolResult.String() {
						t.Fatalf("a text-only completion caused extra activity: %+v", event)
					}
				}
				if len(scheduleRuns(t, e, schedule.ID)) != 1 || onlyContextFlowTurn(t, e, turn.ThreadID).ID != turn.ID {
					t.Fatal("terminal text created another turn or schedule run")
				}
			})
		}
	}
}

func TestScheduledEmptyReplyRecoveryAcceptsTextOrRemainsBounded(t *testing.T) {
	for _, trigger := range []string{"run now", "automatic"} {
		for _, answer := range []string{"The stored result is available.", ""} {
			name := "terminal text"
			if answer == "" {
				name = "empty again"
			}
			t.Run(trigger+"/"+name, func(t *testing.T) {
				e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
					content := " \n\t"
					if step > 1 {
						content = answer
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": content}, "stop")
				})
				schedule, turn := startScheduleCompletionTurn(t, e, trigger)
				got := waitForTurn(t, e, turn.ID)
				run := waitForScheduleRun(t, e, turn.ScheduleRunID)
				if answer != "" {
					if got.Status != store.TurnDone || run.Status != store.ScheduleRunFindings || !run.Unread || run.Summary != answer {
						t.Fatalf("same-turn text recovery failed: turn=%+v run=%+v", got, run)
					}
				} else if got.Status != store.TurnError || got.Error != "scheduled check ended without returning text, executing a tool or reporting its result" || run.Status != store.ScheduleRunError || !run.Unread {
					t.Fatalf("empty responses did not fail visibly: turn=%+v run=%+v", got, run)
				}
				requests := fixture.snapshot()
				assertScheduleRequestsKeepToolsOptional(t, requests)
				if len(requests) != 2 {
					t.Fatalf("empty reply recovery requests=%d, want two", len(requests))
				}
				if !strings.Contains(requests[1].Messages[len(requests[1].Messages)-1].Content, scheduleCompletionCue) {
					t.Fatal("recovery lost its same-turn instruction")
				}
				events, err := e.Store().ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				retries := 0
				for _, event := range events {
					if event.Kind == KindModelRetry && strings.Contains(event.Text, "scheduled_no_activity") {
						retries++
					}
				}
				if retries != 1 || len(scheduleRuns(t, e, schedule.ID)) != 1 || onlyContextFlowTurn(t, e, turn.ThreadID).ID != turn.ID {
					t.Fatalf("recovery duplicated the check: retries=%d", retries)
				}
			})
		}
	}
}

func TestScheduledExplicitQuietReportRemainsSuccessfulWithAutoTools(t *testing.T) {
	for _, trigger := range []string{"run now", "automatic"} {
		t.Run(trigger, func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					writeScheduleCompletionTool(w, request, ToolReportSchedule, `{"findings":""}`)
					return
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The check has nothing new to surface."}, "stop")
			})
			_, turn := startScheduleCompletionTurn(t, e, trigger)
			waitForTurn(t, e, turn.ID)
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			// Quiet is stamped after FinishTurn/FinishRun. Wait for that
			// persisted field rather than treating an idle runtime as the write.
			deadline := time.Now().Add(5 * time.Second)
			got, err := e.Store().GetTurn(turn.ID)
			for err == nil && !got.Quiet && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
				got, err = e.Store().GetTurn(turn.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != store.TurnDone || !got.Quiet || run.Status != store.ScheduleRunQuiet || run.Unread {
				t.Fatalf("explicit quiet report was retried or rejected: turn=%+v run=%+v", got, run)
			}
			requests := fixture.snapshot()
			assertScheduleRequestsKeepToolsOptional(t, requests)
			if len(requests) != 2 {
				t.Fatalf("quiet tool round requests=%d, want two", len(requests))
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Kind == KindModelRetry {
					t.Fatalf("explicit quiet report retried: %+v", event)
				}
			}
		})
	}
}

func TestStopCancelsTheScheduledEmptyReplyRecoveryHTTPRequest(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": ""}, "stop")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})
	_, turn := startScheduleCompletionTurn(t, e, "run now")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not reach HTTP")
	}
	if err := e.Interrupt(turn.ThreadID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop left the recovery request running")
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("Stop became a failed scheduled check: %+v", got)
	}
	requests := fixture.snapshot()
	assertScheduleRequestsKeepToolsOptional(t, requests)
	if len(requests) != 2 {
		t.Fatalf("Stop restarted recovery: requests=%d", len(requests))
	}
}
