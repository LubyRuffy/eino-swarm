package engine

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A wait-shaped exec must block the next model request until a real job
// exits. A later job can wake it before the earlier job has settled.
func TestManagerExecPollAwaitsOriginalExitInsteadOfStartingSleep(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const first = `{"command":"while [ ! -f release-first ]; do sleep 0.02; done; printf first"}`
	const second = `{"command":"while [ ! -f release-second ]; do sleep 0.02; done; printf second; exit 7"}`
	const pollID = "background-poll"
	const poll = `{"command":"sleep 30; echo observed","timeout_ms":60000}`
	const final = "Verified both command results."
	type pollReceipt struct {
		Waited bool   `json:"waited_for_background"`
		Stdout string `json:"stdout"`
	}
	pollStarted, wake := make(chan struct{}), make(chan struct{})
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			writeScheduleCompletionTool(w, request, "exec", first)
		case 2:
			assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			writeManagedExecHTTPCall(w, request, parallelExecHTTPCall, second)
		case 3:
			assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			assertManagedExecHTTPStatus(t, request, parallelExecHTTPCall, "running")
			writeManagedExecHTTPCall(w, request, pollID, poll)
			close(pollStarted)
		case 4:
			assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			result := assertManagedExecHTTPStatus(t, request, parallelExecHTTPCall, "exited")
			if result.Stdout != "second" || result.ExitCode == nil || *result.ExitCode != 7 {
				t.Errorf("wake lost the original result: %+v", result)
			}
			var receipt pollReceipt
			var count int
			for _, message := range request.Messages {
				if message.Role == schema.Tool && message.ToolCallID == pollID {
					count++
					if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil {
						t.Error(err)
					}
				}
			}
			if count != 1 || !receipt.Waited || receipt.Stdout != "observed\n" {
				t.Errorf("expected one engine wait receipt and the real probe output, got count=%d %+v", count, receipt)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Waiting for the first command."}, "stop")
			close(wake)
		case 5:
			if result := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "exited"); result.Stdout != "first" {
				t.Errorf("first command result lost: %+v", result)
			}
			assertManagedExecHTTPStatus(t, request, parallelExecHTTPCall, "exited")
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
		default:
			t.Errorf("unexpected model request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Tools.Enabled = []string{"exec_session"}
	thread, err := e.CreateThread("Background poll fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Run both commands and verify their results.")
	if err != nil {
		t.Fatal(err)
	}
	workspace := e.WorkspaceDir(thread.ID)
	ordinaryAwait(t, pollStarted, "manager did not request the wait")
	hold := time.NewTimer(1250 * time.Millisecond)
	defer hold.Stop()
	select {
	case <-wake:
		t.Fatal("the polling exec caused a model request while both original commands were still running")
	case <-hold.C:
	}
	if err := os.WriteFile(filepath.Join(workspace, "release-second"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ordinaryAwait(t, wake, "the first available original exit did not wake the manager")
	if err := os.WriteFile(filepath.Join(workspace, "release-first"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 5 {
		t.Fatalf("unexpected terminal turn: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
	}
	count := 0
	for _, result := range e.managerToolResults(turn.ID) {
		if result.ToolCallID != pollID {
			continue
		}
		count++
		var receipt pollReceipt
		if err := json.Unmarshal([]byte(result.Text), &receipt); err != nil || !receipt.Waited {
			t.Errorf("durable wait receipt missing: %s", result.Text)
		}
	}
	if count != 1 {
		t.Fatalf("expected one durable result for the wait, got %d", count)
	}
}
