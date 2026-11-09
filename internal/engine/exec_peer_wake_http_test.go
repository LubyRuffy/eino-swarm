package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// An explicit wait must let the manager collect an independently completed peer
// while the requested process continues to run under its original identity.
func TestManagerWaitExecWakesForPeerCompletionOverHTTP(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const peerID = "peer-owned-exec"
	const firstWait = "wait-target-before-peer"
	const finalWait = "wait-target-after-peer"
	const final = "Both process terminal results have been collected."
	const targetCommand = `{"command":"printf x >> target-starts; printf 'target started'; while [ ! -f release-target ]; do printf x >> target-heartbeat; sleep 0.02; done; printf ' target finished'"}`
	const peerCommand = `{"command":"printf x >> peer-starts; printf 'peer started'; while [ ! -f release-peer ]; do sleep 0.02; done; printf ' peer finished'; exit 7"}`
	peerDelivered := make(chan struct{}, 1)
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			writeScheduleCompletionTool(w, request, "exec", targetCommand)
			return
		}
		if step == 2 {
			assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			writeManagedExecHTTPCall(w, request, peerID, peerCommand)
			return
		}
		targetStatus := "running"
		if step == 5 {
			targetStatus = "exited"
		}
		target := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, targetStatus)
		switch step {
		case 3:
			assertManagedExecHTTPStatus(t, request, peerID, "running")
		case 4:
			peer := assertManagedExecHTTPStatus(t, request, peerID, "exited")
			if peer.ExitCode == nil || *peer.ExitCode != 7 || peer.Stdout != "peer started peer finished" {
				t.Errorf("peer terminal result lost its failure or output: %+v", peer)
			}
			observed := execObserverHTTPResult(t, request, firstWait)
			var wake struct {
				Code string `json:"code"`
			}
			for _, message := range request.Messages {
				if message.Role == schema.Tool && message.ToolCallID == firstWait {
					if err := json.Unmarshal([]byte(message.Content), &wake); err != nil {
						t.Error(err)
					}
				}
			}
			if observed.SessionID != target.SessionID || observed.Status != "running" ||
				observed.ExitCode != nil || observed.ObservationTimedOut || wake.Code != "peer_exec_settled" {
				t.Errorf("peer wake must retain a running target and an explicit reason: %+v code=%q", observed, wake.Code)
			}
			peerDelivered <- struct{}{}
		case 5:
			peer := assertManagedExecHTTPStatus(t, request, peerID, "exited")
			observed := execObserverHTTPResult(t, request, finalWait)
			if target.ExitCode == nil || *target.ExitCode != 0 || peer.ExitCode == nil || *peer.ExitCode != 7 ||
				observed.SessionID != target.SessionID || observed.Status != "exited" ||
				observed.ExitCode == nil || *observed.ExitCode != 0 || target.Stdout != "target started target finished" {
				t.Errorf("final model input lost independent terminal outcomes: target=%+v peer=%+v wait=%+v", target, peer, observed)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			return
		default:
			t.Errorf("unexpected model request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			return
		}
		id := firstWait
		if step == 4 {
			id = finalWait
		}
		args, err := json.Marshal(map[string]any{"session_id": target.SessionID, "idle_timeout_ms": 30000})
		if err != nil {
			t.Error(err)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": "wait_exec", "arguments": string(args)},
		}}}, "tool_calls")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Tools.Enabled = []string{"exec_session"}
	thread, err := e.CreateThread("Collect process results", "", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace := e.WorkspaceDir(thread.ID)
	release := func(name string) {
		if err := os.WriteFile(filepath.Join(workspace, name), nil, 0600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() {
		release("release-target")
		release("release-peer")
		if err := e.Interrupt(thread.ID); err != nil && !errors.Is(err, ErrIdle) {
			t.Error(err)
		}
		waitSettled(t, e, thread.ID)
	})
	turn, err := e.StartTurn(thread.ID, "Observe the running commands and collect their results.")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryEventually(t, func() bool {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == firstWait {
				return true
			}
		}
		return false
	}, "manager did not submit its explicit target wait")
	release("release-peer")
	select {
	case <-peerDelivered:
	case <-time.After(5 * time.Second):
		t.Fatal("peer exited but the manager remained blocked on the running target")
	}
	heartbeat := filepath.Join(workspace, "target-heartbeat")
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryEventually(t, func() bool {
		after, err := os.ReadFile(heartbeat)
		return err == nil && len(after) > len(before)
	}, "peer completion stopped the still-running target")
	release("release-target")
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 5 {
		t.Fatalf("turn=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
	}
	for _, name := range []string{"target-starts", "peer-starts"} {
		if data, err := os.ReadFile(filepath.Join(workspace, name)); err != nil || string(data) != "x" {
			t.Errorf("process was started more than once: %s=%q err=%v", name, data, err)
		}
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminals := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == peerID {
			var receipt managedExecHTTPReceipt
			if err := json.Unmarshal([]byte(event.Text), &receipt); err == nil && receipt.Status == "exited" {
				terminals++
			}
		}
	}
	if terminals != 1 {
		t.Errorf("peer original call has %d durable terminal receipts", terminals)
	}
}
