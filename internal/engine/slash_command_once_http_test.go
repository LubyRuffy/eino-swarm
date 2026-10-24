package engine

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A command's argument is data. Reinterpreting it at the next dispatcher can
// silently change the requested mode and the model's actual input/tool table.
func TestFirstSlashCommandOwnsItsArgumentAtTheModelBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   UserInput
		text string
		goal string
		plan bool
	}{
		{"goal owns a plan argument", UserInput{Text: "/goal /plan Inspect the operation"}, "/plan Inspect the operation", "/plan Inspect the operation", false},
		{"goal owns an IME plan argument", UserInput{Text: "／goal 、plan检查当前范围"}, "、plan检查当前范围", "、plan检查当前范围", false},
		{"goal owns another goal argument", UserInput{Text: "/goal /goal Inspect the operation"}, "/goal Inspect the operation", "/goal Inspect the operation", false},
		{"ordinary goal", UserInput{Text: "/goal Inspect the operation"}, "Inspect the operation", "Inspect the operation", false},
		{"ordinary plan", UserInput{Text: "/plan Inspect the operation"}, "Inspect the operation", "", true},
		{"plan owns a trailing goal argument", UserInput{Text: "/plan Inspect the operation /goal"}, "Inspect the operation /goal", "", true},
		{"plan owns a leading goal argument", UserInput{Text: "/plan /goal Inspect the operation"}, "/goal Inspect the operation", "", true},
		{"goal continuation keeps literal input", UserInput{Text: "/goal /plan Inspect the operation", ContinueGoal: true}, "/goal /plan Inspect the operation", "", false},
		{"schedule continuation keeps literal input", UserInput{Text: "/goal /plan Inspect the operation", ContinueSchedule: true}, "/goal /plan Inspect the operation", "", false},
		{"implementation keeps literal plan input", UserInput{Text: "/plan Inspect the operation", ImplementPlan: true}, "/plan Inspect the operation", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					for _, tool := range request.Tools {
						if tool.Function.Name == ToolCompleteGoal {
							writeScheduleCompletionTool(w, request, ToolCompleteGoal, `{"summary":"The assigned operation is verified."}`)
							return
						}
					}
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned operation is verified."}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			thread, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurnInput(thread.ID, tc.in)
			if err != nil || turn == nil {
				t.Fatalf("start turn=%+v err=%v", turn, err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if finished.Status != store.TurnDone || finished.UserText != tc.text || finished.GoalContinue != tc.in.ContinueGoal || finished.ScheduleContinue != tc.in.ContinueSchedule {
				t.Errorf("stored turn=%+v; want text=%q and original continuation flags", finished, tc.text)
			}
			got, err := e.Store().GetThread(thread.ID)
			if err != nil || got.Goal != tc.goal || got.PlanMode != tc.plan {
				t.Errorf("thread=%+v err=%v; want goal=%q plan=%v", got, err, tc.goal, tc.plan)
			}
			requests := calls.snapshot()
			if len(requests) == 0 {
				t.Fatal("the real model HTTP boundary was not reached")
			}
			request := requests[0]
			var userText string
			for _, message := range request.Messages {
				if message.Role == schema.User {
					userText = message.Content
				}
			}
			if userText != tc.text {
				t.Errorf("model input=%q; want the first command's complete argument %q", userText, tc.text)
			}
			planningTools := false
			for _, tool := range request.Tools {
				planningTools = planningTools || tool.Function.Name == ToolProposePlan
			}
			if planningTools != tc.plan {
				t.Errorf("model planning tools=%v; want %v", planningTools, tc.plan)
			}
			if hasKind(t, e, thread.ID, KindPlan) != tc.plan {
				t.Errorf("plan lifecycle changed; want plan=%v", tc.plan)
			}
			turns, err := e.Store().ListTurns(thread.ID)
			if err != nil || len(turns) != 1 {
				t.Errorf("command started extra turns: %+v err=%v", turns, err)
			}
		})
	}
}

func TestLeadingPlanSteerKeepsItsTrailingGoalLiteralAtTheModelBoundary(t *testing.T) {
	entered, steered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			// An actual registry tool forces another model boundary after the
			// queued human steer, without starting an unrelated worker.
			writeScheduleCompletionTool(w, request, "wait_agents", `{"agent_ids":[],"timeout_s":1}`)
			return
		}
		if step == 2 {
			close(steered)
		}
		<-r.Context().Done()
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(thread.ID, "The previous objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(thread.ID, "Previous work verified."); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the current operation.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the manager never entered its actual model request")
	}
	const steering = "/plan Inspect the next operation /goal"
	if err := e.SteerInput(thread.ID, UserInput{Text: steering}); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetThread(thread.ID)
	if err != nil || got.Goal != "The previous objective" || !got.GoalComplete || got.PlanMode {
		t.Errorf("literal plan steer changed persistent goal/mode: %+v err=%v", got, err)
	}
	if e.Status(thread.ID).TurnID != turn.ID {
		t.Error("steering replaced the running turn")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-steered:
	case <-time.After(10 * time.Second):
		t.Fatal("the original turn did not read the steer at its next model request")
	}
	requests := calls.snapshot()
	seen := false
	for _, message := range requests[1].Messages {
		seen = seen || message.Role == schema.User && message.Content == "[steer] "+steering
	}
	if !seen {
		t.Errorf("the actual model lost or reinterpreted the steer: %+v", requests[1].Messages)
	}
	events, err := e.Replay(thread.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	goals, literalSteers := 0, 0
	for _, event := range events {
		if event.Kind == KindGoal {
			goals++
		}
		if event.Kind == KindSteer && event.TurnID == turn.ID && event.Text == steering {
			literalSteers++
		}
	}
	if goals != 1 || literalSteers != 1 {
		t.Errorf("steer lifecycle goals=%d literalSteers=%d; want original goal and one literal steer", goals, literalSteers)
	}
	if err := e.Interrupt(thread.ID); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, thread.ID)
}
