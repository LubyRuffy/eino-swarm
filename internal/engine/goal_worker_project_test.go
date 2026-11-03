package engine

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Repeated Play clicks must preserve the original worker's recovery evidence
// while its saved owner differs, then restore the same worker after repair.
func TestGoalResumeWaitsForTaggedWorkerProjectRepair(t *testing.T) {
	for _, fromProject := range []bool{true, false} {
		name := "unassigned to project"
		if fromProject {
			name = "project to project"
		}
		t.Run(name, func(t *testing.T) {
			const workerID = "observer-4"
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					nuwaValidationTool(w, request, "repair-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, workerID))
				case 2:
					nuwaValidationTool(w, request, "repair-complete", ToolCompleteGoal, `{"summary":"Recovered and collected the original worker."}`)
				default:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recovery finished."}, "stop")
				}
			})
			original := ""
			if fromProject {
				project, err := e.CreateProject("Original project", "", "", false)
				if err != nil {
					t.Fatal(err)
				}
				original = project.ID
			}
			destination, err := e.CreateProject("Destination project", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit project recovery fixture", "", original)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(thread.ID, "Collect the retained worker's findings."); err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: original, ProviderID: thread.ProviderID, Model: thread.Model,
				GoalContinue: true, UserText: GoalContinueText()}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			appendParkedCheckpoint(t, e, thread.ID, turn.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
				ID: workerID, Role: "observer", Generation: 3, ConfigTag: turn.ID,
				Task: "Check the retained findings.",
			}}})
			if err := e.MoveThread(thread.ID, destination.ID); err != nil {
				t.Fatal(err)
			}
			// Reproduce the durable state left by a previous engine that had
			// already attempted recovery and blocked the goal.
			const reason = "restore worker configuration: worker configuration's project owner changed"
			if err := e.store.FinishTurn(turn.ID, store.TurnError, "", reason); err != nil {
				t.Fatal(err)
			}
			if err := e.BlockThreadGoal(thread.ID, reason); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				if next, err := e.ResumeThreadGoal(thread.ID); next != nil || err == nil || !strings.Contains(err.Error(), "project owner changed") {
					t.Fatalf("attempt %d started while ownership differed: next=%+v err=%v", i+1, next, err)
				}
			}
			turns, err := e.store.ListTurns(thread.ID)
			if err != nil || len(turns) != 1 || turns[0].Status != store.TurnError || turns[0].Error != reason {
				t.Fatalf("refusals changed turn history: %+v err=%v", turns, err)
			}
			blocked, err := e.store.GetThread(thread.ID)
			if err != nil || !blocked.GoalBlocked || blocked.GoalBlockReason != reason || hasKind(t, e, thread.ID, KindGoalResumed) || len(fixture.snapshot()) != 0 {
				t.Fatalf("refusals changed goal state or reached the model: %+v err=%v", blocked, err)
			}
			if err := e.MoveThread(thread.ID, original); err != nil {
				t.Fatal(err)
			}
			next, err := e.ResumeThreadGoal(thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, next.ID); got.Status != store.TurnDone || got.ProjectID != original {
				t.Fatalf("repair did not restore pursuit under the original owner: %+v", got)
			}
			_, finished := e.workersFromThread(thread.ID)
			if len(finished) != 1 || finished[0].ID != workerID || finished[0].Generation != 3 || finished[0].ConfigTag != turn.ID ||
				finished[0].Err != nil || finished[0].Result != "The stored evidence is unchanged." || !finished[0].Collected {
				t.Fatalf("repaired continuation did not collect the original HTTP worker: %+v", finished)
			}
		})
	}
}

func TestGoalResumeNeedsRecoveryEvidenceOnlyForFreshRegistry(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	thread, err := e.CreateThread("Explicit recovery evidence fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("checkpoint lookup unavailable")
	if err := e.store.DB().Callback().Row().Before("gorm:row").Register("fail_goal_recovery_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			_ = tx.AddError(readErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(thread.ID)
	if err := rt.checkGoalResumeWorkerProjects(); !errors.Is(err, readErr) {
		t.Fatalf("fresh registry was admitted without readable recovery evidence: %v", err)
	}
	rt.parkRegistry(swarm.NewRegistry())
	if err := rt.checkGoalResumeWorkerProjects(); err != nil {
		t.Fatalf("reusing the parked registry unnecessarily read a cold checkpoint: %v", err)
	}
}
