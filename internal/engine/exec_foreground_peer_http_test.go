package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A foreground exec result must not remain trapped behind a parallel ID wait.
func TestManagerWaitExecWakesForForegroundPeerInSameBatchOverHTTP(t *testing.T) {
	const peerID = "foreground-peer-call"
	const waitID = "parallel-target-wait"
	const idleWaitID = "wait-after-next-manager-advance"
	const finalWaitID = "wait-for-real-target-completion"
	const final = "Both independent terminal results have been collected."
	const target = `{"command":"printf x >> target-starts; while [ ! -f release-target ]; do printf x >> target-heartbeat; sleep 0.02; done; printf target-finished"}`
	const peer = `{"command":"printf x >> peer-starts; while [ ! -f release-peer ]; do sleep 0.02; done; printf foreground-finished; exit 7","yield_after_ms":30000}`
	arrived := make(chan struct{}, 1)
	var closing atomic.Bool
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if closing.Load() {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Fixture cleanup."}, "stop")
			return
		}
		if step == 1 {
			writeScheduleCompletionTool(w, request, "exec", target)
			return
		}
		if step == 2 {
			original := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			args, err := json.Marshal(map[string]any{"session_id": original.SessionID, "idle_timeout_ms": 60000})
			if err != nil {
				t.Error(err)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": waitID, "type": "function", "function": map[string]any{"name": "wait_exec", "arguments": string(args)}},
				map[string]any{"index": 1, "id": peerID, "type": "function", "function": map[string]any{"name": "exec", "arguments": peer}},
			}}, "tool_calls")
			return
		}
		if step < 3 || step > 5 {
			t.Errorf("unexpected model request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			return
		}
		count := 0
		for _, message := range request.Messages {
			if message.Role == schema.Tool && message.ToolCallID == peerID {
				var result struct {
					SessionID string `json:"session_id"`
					ExitCode  *int   `json:"exit_code"`
					Stdout    string `json:"stdout"`
				}
				if json.Unmarshal([]byte(message.Content), &result) != nil || result.SessionID != "" || result.ExitCode == nil || *result.ExitCode != 7 || result.Stdout != "foreground-finished" {
					t.Errorf("foreground result lost its identity or outcome: %s", message.Content)
				}
				count++
			}
		}
		if count != 1 {
			t.Errorf("foreground original result count=%d", count)
		}
		targetStatus := "running"
		if step == 5 {
			targetStatus = "exited"
		}
		original := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, targetStatus)
		observedID := waitID
		if step == 4 {
			observedID = idleWaitID
		}
		if step == 5 {
			observedID = finalWaitID
		}
		observed := execObserverHTTPResult(t, request, observedID)
		var code string
		for _, message := range request.Messages {
			if message.Role == schema.Tool && message.ToolCallID == observedID {
				var value struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal([]byte(message.Content), &value); err != nil {
					t.Error(err)
				}
				code = value.Code
			}
		}
		if observed.SessionID != original.SessionID {
			t.Errorf("wait switched target identity: observed=%+v original=%+v", observed, original)
		}
		switch step {
		case 3:
			if observed.Status != "running" || observed.ExitCode != nil || observed.ObservationTimedOut || code != "peer_exec_settled" {
				t.Errorf("foreground peer did not wake the same running target: %+v code=%q", observed, code)
			}
		case 4:
			if observed.Status != "running" || observed.ExitCode != nil || !observed.ObservationTimedOut || code != "" {
				t.Errorf("next manager Advance retained an already-collected peer wake: %+v code=%q", observed, code)
			}
			arrived <- struct{}{}
		case 5:
			if observed.Status != "exited" || observed.ExitCode == nil || *observed.ExitCode != 0 ||
				original.ExitCode == nil || *original.ExitCode != 0 || original.Stdout != "target-finished" || code != "" {
				t.Errorf("target did not return its genuine terminal result: observed=%+v original=%+v code=%q", observed, original, code)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			return
		}
		id, idleMS := idleWaitID, 60
		if step == 4 {
			id, idleMS = finalWaitID, 60000
		}
		args, err := json.Marshal(map[string]any{"session_id": original.SessionID, "idle_timeout_ms": idleMS})
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
	thread, err := e.CreateThread("Parallel result collection fixture", "", "")
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
		closing.Store(true)
		release("release-peer")
		release("release-target")
		if err := e.Interrupt(thread.ID); err != nil && !errors.Is(err, ErrIdle) {
			t.Error(err)
		}
		waitSettled(t, e, thread.ID)
	})
	turn, err := e.StartTurn(thread.ID, "Collect each process result when available.")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryEventually(t, func() bool {
		if _, err := os.Stat(filepath.Join(workspace, "peer-starts")); err != nil {
			return false
		}
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == waitID {
				return true
			}
		}
		return false
	}, "same-batch peer did not start while the ID wait was submitted")
	release("release-peer")
	var peerSeq, waitSeq int64
	ordinaryEventually(t, func() bool {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == waitID {
				waitSeq = event.Seq
			}
			if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == peerID {
				var result struct {
					SessionID string `json:"session_id"`
					ExitCode  *int   `json:"exit_code"`
					Stdout    string `json:"stdout"`
				}
				if json.Unmarshal([]byte(event.Text), &result) == nil && result.SessionID == "" && result.ExitCode != nil && *result.ExitCode == 7 && result.Stdout == "foreground-finished" {
					peerSeq = event.Seq
					return true
				}
			}
		}
		return false
	}, "foreground command did not produce its ordinary terminal receipt before yielding")
	t.Logf("FOREGROUND_BATCH_PROOF wait_call_seq=%d foreground_terminal_seq=%d foreground_session_id_empty=true exit_code=7 http_requests=%d", waitSeq, peerSeq, len(fixture.snapshot()))
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("foreground result was recorded, but the next manager Advance did not clear and idle-wait the target")
	}
	heartbeat := filepath.Join(workspace, "target-heartbeat")
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryEventually(t, func() bool {
		after, err := os.ReadFile(heartbeat)
		return err == nil && len(after) > len(before)
	}, "foreground peer completion stopped the running target")
	release("release-target")
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 5 {
		t.Fatalf("turn=%s final=%q model_requests=%d", got.Status, got.Final, len(fixture.snapshot()))
	}
	for _, name := range []string{"target-starts", "peer-starts"} {
		if data, err := os.ReadFile(filepath.Join(workspace, name)); err != nil || string(data) != "x" {
			t.Errorf("process start identity changed: %s=%q err=%v", name, data, err)
		}
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerResults := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == peerID {
			peerResults++
			if event.Seq != peerSeq {
				t.Error("foreground original terminal receipt was duplicated")
			}
		}
	}
	if peerResults != 1 {
		t.Errorf("foreground original result count=%d", peerResults)
	}
}
