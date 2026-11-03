package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNuwaColdRestoreCannotExposeFinishedFormerProjectWorker(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		moved, collected, closed bool
	}{
		{name: "foreign uncollected obligation", moved: true},
		{name: "foreign collected archive", moved: true, collected: true},
		{name: "foreign discarded archive", moved: true, closed: true},
		{name: "original project can collect", moved: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const workerID = "source-reader-4"
			const marker = "Former project only: checkpoint-only Worker result."
			waitID := func(step int) string { return fmt.Sprintf("cold-former-owner-wait-%d", step) }
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, req scheduleCompletionRequest) {
				if step%2 == 1 {
					for _, msg := range req.Messages {
						if strings.Contains(msg.Content, marker) {
							t.Error("fixture marker reached manager before the worker tool call")
						}
					}
					nuwaValidationTool(w, req, waitID(step), "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, workerID))
				} else {
					writeContextFlowReply(w, req.Stream, map[string]any{"content": "The destination check ended."}, "stop")
				}
			})
			e.cfg.Memory.AutoReview = false
			a, err := e.CreateProject("Original project", "", t.TempDir(), false, true)
			if err != nil {
				t.Fatal(err)
			}
			b, err := e.CreateProject("Destination project", "", t.TempDir(), false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Moved conversation", "", a.ID)
			if err != nil {
				t.Fatal(err)
			}
			origin := &store.Turn{ThreadID: thread.ID, ProjectID: a.ID, NuwaEnabled: true,
				UserText: "Inspect public instructions.", ProviderID: thread.ProviderID, Model: thread.Model}
			if err := e.store.CreateTurn(origin); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(origin.ID, store.TurnDone, "Prior check ended.", ""); err != nil {
				t.Fatal(err)
			}
			appendParkedCheckpoint(t, e, thread.ID, origin.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
				ID: workerID, Role: "source-reader", ConfigTag: origin.ID, Generation: 2, Finished: true,
				Collected: tc.collected, CloseRequested: tc.closed, Result: marker,
			}}})
			rt := e.runtimeFor(thread.ID)
			rt.mu.Lock()
			hadLiveRegistry := rt.parked != nil || rt.reg != nil
			rt.mu.Unlock()
			if hadLiveRegistry {
				t.Fatal("fixture should have only durable cold Worker state")
			}
			runWait := func(projectID, expectedStatus string) {
				t.Helper()
				before := len(fixture.snapshot())
				next, err := e.StartTurn(thread.ID, "Check the recorded Worker result.")
				if err != nil {
					t.Fatal(err)
				}
				next = waitForTurn(t, e, next.ID)
				waitSettled(t, e, thread.ID)
				if next.Status != store.TurnDone || next.ProjectID != projectID {
					t.Fatalf("turn did not complete under the expected project: %+v", next)
				}
				requests := fixture.snapshot()
				if len(requests) != before+2 {
					t.Fatalf("expected two actual manager requests, got %d", len(requests)-before)
				}
				callID := waitID(before + 1)
				reply := nuwaValidationReceipt(requests[before+1], callID)
				receipt, err := awaitNuwaToolResult(t.Context(), e.store, callID)
				if err != nil || receipt.TurnID != next.ID || receipt.Text != reply {
					t.Fatalf("actual manager wait was not durably observed: %+v, %v", receipt, err)
				}
				var wait toolScopeWait
				if err := json.Unmarshal([]byte(reply), &wait); err != nil || wait.TimedOut || len(wait.Agents) != 1 ||
					wait.Agents[0].AgentID != workerID || wait.Agents[0].Status != expectedStatus ||
					(expectedStatus == "done" && wait.Agents[0].Generation != 2) ||
					strings.Contains(reply, marker) != (expectedStatus == "done") {
					t.Fatalf("worker outcome status=%s wait_seq=%d marker=%t response=%q err=%v",
						expectedStatus, receipt.Seq, strings.Contains(reply, marker), reply, err)
				}
			}
			if !tc.moved {
				runWait(a.ID, "done")
				return
			}
			if err := e.MoveThread(thread.ID, b.ID); err != nil {
				t.Fatal(err)
			}
			if tc.collected || tc.closed {
				runWait(b.ID, "unknown")
			} else if next, err := e.StartTurn(thread.ID, "Continue in the destination."); next != nil ||
				!errors.Is(err, errTurnProjectOwnerChanged) || len(fixture.snapshot()) != 0 {
				t.Fatalf("foreign uncollected Worker reached admission: turn=%+v requests=%d err=%v", next, len(fixture.snapshot()), err)
			}
			if checkpoint := e.checkpointForTurn(origin.ID, nil); checkpoint == nil || len(checkpoint.Workers) != 1 ||
				checkpoint.Workers[0].Result != marker || checkpoint.Workers[0].Collected != tc.collected {
				t.Fatalf("destination attempt modified the original durable checkpoint: %+v", checkpoint)
			}
			if err := e.MoveThread(thread.ID, a.ID); err != nil {
				t.Fatal(err)
			}
			if !tc.closed {
				runWait(a.ID, "done")
			}
		})
	}
}
