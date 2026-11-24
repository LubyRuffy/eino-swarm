package engine

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Refusals retain the original body and never redirect an explicit frame to
// the old model reviewer. A quoted example or model text is only normal input.
func TestDreamCorrectionRefusesUnownedAndStaleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, status            string
		managerOnly, quoted     bool
		stale, malformed        bool
		planAtSend, planAtApply bool
		dreamOff, reviewOff     bool
		wantLegacyReview        bool
	}{
		{name: "stale before-image", stale: true, status: "refused"},
		{name: "malformed explicit frame", malformed: true, status: "refused"},
		{name: "embedded example", quoted: true, wantLegacyReview: true},
		{name: "assistant text", managerOnly: true, wantLegacyReview: true},
		{name: "plan at admission then disabled", planAtSend: true},
		{name: "plan entered before correction commit", planAtApply: true},
		{name: "Dream explicitly off", dreamOff: true},
		{name: "automatic memory review off", reviewOff: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reviewerCalls atomic.Int32
			frame := dreamFixtureCommand(t, dreamFixtureBad, dreamFixtureGood)
			text, final := frame, "The request finished."
			switch {
			case tc.stale:
				text = dreamFixtureCommand(t, "An older, different procedure.", dreamFixtureGood)
			case tc.malformed:
				text = "```memory-correction\n{\"name\":\"local-label-check\"}\n```"
			case tc.quoted:
				text = "Explain this example without applying it:\n\n" + frame
			case tc.managerOnly:
				text, final = "Show an example of the record format.", frame
			}
			e := newDreamFixtureEngine(t, &reviewerCalls, func(w http.ResponseWriter, request scheduleCompletionRequest) {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.AutoDream = !tc.dreamOff
			e.cfg.Memory.AutoReview = !tc.reviewOff
			// Prediction is scheduled after the correction and review dispatch.
			// Its durable event lets a negative assertion join that actual work
			// without making engine shutdown itself suppress the tested action.
			e.cfg.Swarm.ComposerPredictions = true
			e.composerPredictionsInTest.Store(true)
			project, mem := seedDreamFixtureSkill(t, e, t.TempDir())
			thread, err := e.CreateThread("Local correction test", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.planAtSend {
				if err := e.SetPlanMode(thread.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			release := holdDreamFixtureGate(t, e, project.ID)
			turn, err := e.StartTurn(thread.ID, text)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("the foreground fixture failed: %+v", got)
			}
			waitForPrediction(t, e, turn.ID)
			if tc.planAtApply || tc.planAtSend {
				if err := e.SetPlanMode(thread.ID, tc.planAtApply); err != nil {
					t.Fatal(err)
				}
			}
			release()
			waitPostTurnRoutingWork(t, e)
			assertDreamFixtureBody(t, mem, dreamFixtureBad)
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			var legacy, dream int
			for _, event := range events {
				if event.Kind != KindMemoryReview {
					continue
				}
				out := decodeReview(t, event)
				if out.Changed || len(out.Changes) != 0 {
					t.Fatalf("refused correction recorded a write: %s", event.Text)
				}
				if out.Dream == nil {
					legacy++
					continue
				}
				dream++
				if out.Dream.Status != tc.status || out.Dream.SourceSeq <= 0 || event.Err == "" {
					t.Fatalf("unexpected correction receipt: %s", event.Text)
				}
			}
			if (dream == 1) != (tc.status != "") || dream > 1 || (legacy == 1) != tc.wantLegacyReview || legacy > 1 || reviewerCalls.Load() != int32(legacy) {
				t.Fatalf("unexpected post-turn work: Dream receipts=%d legacy receipts=%d review POSTs=%d", dream, legacy, reviewerCalls.Load())
			}
		})
	}
}

func TestDreamCorrectionFreshRequestWithStaleBeforeDoesNotRewrite(t *testing.T) {
	var reviewerCalls atomic.Int32
	e := newDreamFixtureEngine(t, &reviewerCalls, func(w http.ResponseWriter, request scheduleCompletionRequest) {
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The request finished."}, "stop")
	})
	project, mem := seedDreamFixtureSkill(t, e, t.TempDir())
	text := dreamFixtureCommand(t, dreamFixtureBad, dreamFixtureGood)
	path := filepath.Join(mem.Dir(), memory.SkillsDir, dreamFixtureSkill, "SKILL.md")
	var before []byte
	var beforeInfo os.FileInfo
	for i, status := range []string{"applied", "refused"} {
		turn := startDreamFixtureTurn(t, e, project.ID, text)
		if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
			t.Fatalf("the foreground fixture failed: %+v", got)
		}
		review := waitForReview(t, e, turn.ID)
		out := decodeReview(t, review)
		if out.Dream == nil || out.Dream.Status != status || out.Dream.SourceSeq <= 0 || out.Changed != (i == 0) || (i == 1 && len(out.Changes) != 0) {
			t.Fatalf("attempt %d did not record %s: %s", i+1, status, review.Text)
		}
		assertDreamFixtureBody(t, mem, dreamFixtureGood)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			before, beforeInfo = raw, info
		} else if string(raw) != string(before) || !os.SameFile(beforeInfo, info) {
			t.Fatal("a fresh request with the stale before-image rewrote the skill or protection marker")
		}
	}
	if reviewerCalls.Load() != 0 {
		t.Fatalf("explicit correction or stale request invoked the model reviewer: %d", reviewerCalls.Load())
	}
}

// A correction waiting at the real project gate must recheck its ownership
// and settings after the human changes them. It must never resurrect data.
func TestDreamCorrectionQueuedWorkRespectsRevokedOwnership(t *testing.T) {
	for _, action := range []string{"delete-source", "delete-project", "move-source", "turn-off-Dream"} {
		t.Run(action, func(t *testing.T) {
			var reviewerCalls atomic.Int32
			e := newDreamFixtureEngine(t, &reviewerCalls, func(w http.ResponseWriter, request scheduleCompletionRequest) {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The request finished."}, "stop")
			})
			e.cfg.Swarm.ComposerPredictions = true
			e.composerPredictionsInTest.Store(true)
			project, mem := seedDreamFixtureSkill(t, e, t.TempDir())
			destination, other := seedDreamFixtureSkill(t, e, t.TempDir())
			release := holdDreamFixtureGate(t, e, project.ID)
			turn := startDreamFixtureTurn(t, e, project.ID, dreamFixtureCommand(t, dreamFixtureBad, dreamFixtureGood))
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("the foreground fixture failed: %+v", got)
			}
			waitForPrediction(t, e, turn.ID)
			assertDreamFixtureBody(t, mem, dreamFixtureBad)
			var err error
			switch action {
			case "delete-source":
				err = e.DeleteThread(turn.ThreadID)
			case "delete-project":
				err = e.DeleteProject(project.ID)
			case "move-source":
				err = e.MoveThread(turn.ThreadID, destination.ID)
			case "turn-off-Dream":
				err = e.cfg.Update(func(next *config.Config) error {
					next.Memory.AutoDream = false
					return nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			release()
			waitPostTurnRoutingWork(t, e)
			assertDreamFixtureBody(t, other, dreamFixtureBad)
			if action == "delete-project" {
				if _, err := e.GetProject(project.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("deleted project returned: %v", err)
				}
				if _, err := os.Stat(mem.Dir()); !os.IsNotExist(err) {
					t.Fatalf("deleted project memory was recreated: %v", err)
				}
			} else {
				assertDreamFixtureBody(t, mem, dreamFixtureBad)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if action == "delete-source" || action == "delete-project" {
				if _, err := e.Store().GetThread(turn.ThreadID); !errors.Is(err, store.ErrNotFound) || len(events) != 0 {
					t.Fatalf("deleted source was recreated or left orphan events: err=%v events=%+v", err, events)
				}
			}
			if action == "move-source" {
				thread, err := e.Store().GetThread(turn.ThreadID)
				if err != nil || thread.ProjectID != destination.ID {
					t.Fatalf("the original conversation did not stay moved: %+v, %v", thread, err)
				}
			}
			for _, event := range events {
				if event.Kind == KindMemoryReview {
					t.Fatalf("revoked queued work left a memory receipt: %s", event.Text)
				}
			}
			if reviewerCalls.Load() != 0 {
				t.Fatalf("revoked queued work invoked the model reviewer: %d", reviewerCalls.Load())
			}
		})
	}
}
