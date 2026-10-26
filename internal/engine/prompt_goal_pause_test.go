package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestPausedGoalPromptDoesNotAuthorizePursuit(t *testing.T) {
	for _, state := range []string{"capped", "blocked", "idle"} {
		t.Run(state, func(t *testing.T) {
			th := &store.Thread{Goal: t.Name()}
			th.GoalCapped = state == "capped"
			th.GoalBlocked = state == "blocked"
			th.GoalIdle = state == "idle"
			body := conversationExtra(th, nil, "")
			if !strings.Contains(body, "## Goal") || !strings.Contains(body, th.Goal) {
				t.Fatal("the paused objective must remain visible")
			}
			if strings.Contains(body, "Keep pursuing it across turns") || !strings.Contains(body, "paused") {
				t.Fatal("a paused objective must not authorize pursuit")
			}
		})
	}
}

// This checks the actual Registry/ADK/HTTP input, rather than constructing a
// prompt that the running manager might never receive.
func TestHumanTurnOnPausedGoalDeliversItsRequestToTheModel(t *testing.T) {
	for _, state := range []string{"capped", "blocked", "idle"} {
		t.Run(state, func(t *testing.T) {
			e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, req scheduleCompletionRequest) {
				writeContextFlowReply(w, req.Stream, map[string]any{"content": "The current state is available."}, "stop")
			})
			th, err := e.CreateThread("paused prompt", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, t.Name()); err != nil {
				t.Fatal(err)
			}
			if err := e.store.UpdateThread(th.ID, map[string]any{
				"goal_capped": state == "capped", "goal_blocked": state == "blocked", "goal_idle": state == "idle",
			}); err != nil {
				t.Fatal(err)
			}
			request := "Request identity: " + t.Name() + "\n\"quoted\" details"
			turn, err := e.StartTurn(th.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("turn: %+v", got)
			}
			waitSettled(t, e, th.ID)
			reqs := captured.snapshot()
			if len(reqs) != 1 {
				t.Fatalf("manager requests=%d", len(reqs))
			}
			var system string
			for _, m := range reqs[0].Messages {
				if m.Role == schema.System {
					system += m.Content
				}
			}
			encoded, _ := json.Marshal(request)
			for _, want := range []string{"## This human turn", `source="human"`, turn.ID, string(encoded), "deliverable", "Tools may be used", "Do not start workers"} {
				if !strings.Contains(system, want) {
					t.Errorf("missing paused-turn structure %q", want)
				}
			}
		})
	}
}

func TestOpenGoalAsksForCompletionCriteriaOncePerObjective(t *testing.T) {
	body := GoalPrompt(t.Name(), false)
	for _, want := range []string{"observable deliverable", "stopping condition", "ask_user", "same objective text", "do not ask again", "complete_goal"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing criteria rule %q", want)
		}
	}
}

func TestGoalDefaultsDoNotContainIncidentInputs(t *testing.T) {
	cfg := config.Default()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := ManagerPrompt(&tools.Set{}, cfg, "") + GoalPrompt("", false) + GoalContinueText() + string(raw)
	// Incident prose is allowed only as user data, never as a default policy.
	for _, leak := range []string{"继" + "续突破", "你为什么" + "停住，卡在哪里了？", "th_" + "665c1098b6d10e1e"} {
		if strings.Contains(body, leak) {
			t.Fatalf("incident input leaked into defaults: %q", leak)
		}
	}
}

func TestImplementPlanDoesNotBecomeAHumanQuestionOnAPausedGoal(t *testing.T) {
	e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, req scheduleCompletionRequest) {
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The requested plan was executed."}, "stop")
	})
	th, err := e.CreateThread("plan origin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal_capped": true, "plan_mode": true, "plan_markdown": "Inspect the existing artifact."}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.ImplementPlan(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn=%+v", got)
	}
	for _, req := range captured.snapshot() {
		for _, message := range req.Messages {
			if message.Role == schema.System && strings.Contains(message.Content, "## This human turn") {
				t.Fatal("a generated plan instruction must not be attributed as the latest human question")
			}
		}
	}
}

func TestPausedGoalInstructionsRespectTurnOriginAndGoalState(t *testing.T) {
	e := newTestEngine(t)
	paused := &store.Thread{Goal: t.Name(), GoalBlocked: true, GoalBlockReason: "external dependency"}
	if body := threadGoalSection(paused); !strings.Contains(body, paused.GoalBlockReason) {
		t.Fatal("a blocked pause must retain its reason")
	}
	if threadGoalSection(nil) != "" {
		t.Fatal("a missing conversation has no goal")
	}
	for _, turn := range []*store.Turn{nil, {GoalContinue: true}, {ScheduleContinue: true}} {
		if body, err := e.pausedGoalAnswerSection(paused, turn); body != "" || err != nil {
			t.Fatal("runtime turns must not be attributed to a new human request")
		}
	}
	for _, th := range []*store.Thread{nil, {}, {Goal: t.Name()}, {Goal: t.Name(), GoalComplete: true}} {
		if body, err := e.pausedGoalAnswerSection(th, &store.Turn{UserText: t.Name()}); body != "" || err != nil {
			t.Fatal("an active, absent or completed objective is not a paused human turn")
		}
		if th != nil && threadGoalSection(th) != goalSection(th.Goal, th.GoalComplete, th.GoalBlocked, th.GoalBlockReason) {
			t.Fatal("nonpaused goal instructions changed")
		}
	}
}

func TestPausedGoalOriginReadFailurePreventsMisattributedModelInput(t *testing.T) {
	e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, req scheduleCompletionRequest) {
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "This must not be requested."}, "stop")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	th, err := e.CreateThread("origin read failure", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal_capped": true}); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("input source unavailable")
	// Retain this callback until the private Store closes: terminal status is
	// not a join of all finalization actors.
	if err := e.store.DB().Callback().Row().Before("gorm:row").Register("test:goal_origin_failure", func(tx *gorm.DB) {
		query := tx.Statement.SQL.String()
		if strings.Contains(query, "SELECT CASE") && strings.Contains(query, "FROM messages") {
			_ = tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the recorded state.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	if got.Status != store.TurnError || !strings.Contains(got.Error, cause.Error()) || len(captured.snapshot()) != 0 {
		t.Fatalf("unverified origin reached the model: turn=%+v requests=%d", got, len(captured.snapshot()))
	}
	paused, err := e.store.GetThread(th.ID)
	if err != nil || !paused.GoalCapped || paused.GoalBlocked {
		t.Fatalf("origin read failure changed the existing pause: %+v %v", paused, err)
	}
}
