package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
)

func runForegroundWakeFixture(t *testing.T, owner *ManagedExecSessions, base tool.BaseTool, callID, command string, exit int) {
	t.Helper()
	exec := ManagedExec(base, owner).(tool.InvokableTool)
	ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, callID)
	args, _ := json.Marshal(map[string]any{"command": command, "yield_after_ms": 30000})
	out, err := exec.InvokableRun(ctx, string(args))
	var result struct {
		SessionID string `json:"session_id"`
		ExitCode  *int   `json:"exit_code"`
		Stdout    string `json:"stdout"`
	}
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.SessionID != "" || result.ExitCode == nil || *result.ExitCode != exit || result.Stdout != "foreground" {
		t.Fatalf("expected one ordinary foreground result: %s, %v", out, err)
	}
}

func TestManagedExecForegroundWakeIsScopedToCurrentModelBatch(t *testing.T) {
	for _, order := range []string{"foreground-first", "wait-first"} {
		t.Run(order, func(t *testing.T) {
			dir := t.TempDir()
			owner := NewManagedExecSessions(t.Context())
			t.Cleanup(func() { _ = owner.Close() })
			base := workspaceExecForTest(t, dir)
			owner.AdvanceModelRequest()
			target := launchExecToObserve(t, owner, base, "target", `while [ ! -f release ]; do sleep 0.02; done; printf target`, 0)
			owner.AdvanceModelRequest()
			observer := managedExecObserversForTest(t, owner)["wait_exec"]
			args, _ := json.Marshal(map[string]any{"session_id": target, "idle_timeout_ms": 60000})
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			type outcome struct {
				receipt observedExecReceipt
				err     error
			}
			got := make(chan outcome, 1)
			if order == "wait-first" {
				entry := &managedWaitEntryContext{Context: ctx, entered: make(chan struct{})}
				go func() {
					receipt, err := invokeExecObserver(entry, observer, string(args))
					got <- outcome{receipt, err}
				}()
				select {
				case <-entry.entered:
				case <-ctx.Done():
					t.Fatal("wait did not subscribe")
				}
			}
			runForegroundWakeFixture(t, owner, base, "foreground", "printf foreground; exit 7", 7)
			if order == "foreground-first" {
				receipt, err := invokeExecObserver(ctx, observer, string(args))
				got <- outcome{receipt, err}
			}
			var result outcome
			select {
			case result = <-got:
			case <-ctx.Done():
				t.Fatal("foreground completion did not wake the current batch")
			}
			if result.err != nil || result.receipt.SessionID != target || result.receipt.Status != "running" || result.receipt.Code != "peer_exec_settled" || result.receipt.ObservationTimedOut || result.receipt.ExitCode != nil {
				t.Fatalf("foreground completion changed target: %+v, %v", result.receipt, result.err)
			}
			if ready, err := owner.TakeReady(); err != nil || len(ready) != 0 {
				t.Fatalf("foreground was queued a second time: %+v, %v", ready, err)
			}
			owner.AdvanceModelRequest()
			short, _ := json.Marshal(map[string]any{"session_id": target, "idle_timeout_ms": 20})
			for i := 0; i < 2; i++ {
				idle, err := invokeExecObserver(ctx, observer, string(short))
				if err != nil || idle.Status != "running" || !idle.ObservationTimedOut || idle.Code != "" {
					t.Fatalf("old batch caused an immediate wake: %+v, %v", idle, err)
				}
			}
			foreign := NewManagedExecSessions(t.Context())
			defer foreign.Close()
			foreign.AdvanceModelRequest()
			runForegroundWakeFixture(t, foreign, base, "foreign", "printf foreground; exit 0", 0)
			idle, err := invokeExecObserver(ctx, observer, string(short))
			if err != nil || !idle.ObservationTimedOut || idle.Code != "" {
				t.Fatalf("foreign owner woke this target: %+v, %v", idle, err)
			}
			runForegroundWakeFixture(t, owner, base, "foreground-success", "printf foreground; exit 0", 0)
			writeExecObservationMarker(t, dir, "release")
			ready, err := owner.Wait(ctx)
			if err != nil || len(ready) != 1 || ready[0].CallID != "target" {
				t.Fatalf("target did not finish once: %+v, %v", ready, err)
			}
			terminal, err := invokeExecObserver(ctx, observer, string(args))
			if err != nil || terminal.Status != "exited" || terminal.Code != "" || terminal.ExitCode == nil || *terminal.ExitCode != 0 || terminal.Stdout != "target" {
				t.Fatalf("foreground flag hid target terminal: %+v, %v", terminal, err)
			}
		})
	}
}

func TestManagedExecDirectOwnerWithoutModelBoundaryDoesNotRetainForegroundWake(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	base := workspaceExecForTest(t, dir)
	target := launchExecToObserve(t, owner, base, "direct-target", `while [ ! -f release ]; do sleep 0.02; done`, 0)
	runForegroundWakeFixture(t, owner, base, "direct-foreground", "printf foreground; exit 7", 7)
	args, _ := json.Marshal(map[string]any{"session_id": target, "idle_timeout_ms": 20})
	observer := managedExecObserversForTest(t, owner)["wait_exec"]
	for i := 0; i < 3; i++ {
		result, err := invokeExecObserver(t.Context(), observer, string(args))
		if err != nil || result.Status != "running" || !result.ObservationTimedOut || result.Code != "" {
			t.Fatalf("direct API retained a batch flag without Advance: %+v, %v", result, err)
		}
	}
	if owner.request != 0 {
		t.Fatalf("test must exercise the non-engine owner: %d", owner.request)
	}
}

func TestManagedExecForegroundWakeDoesNotOverrideWaitCancellation(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	base := workspaceExecForTest(t, dir)
	owner.AdvanceModelRequest()
	target := launchExecToObserve(t, owner, base, "cancel-target", `while [ ! -f release ]; do sleep 0.02; done`, 0)
	owner.AdvanceModelRequest()
	runForegroundWakeFixture(t, owner, base, "cancel-foreground", "printf foreground; exit 7", 7)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	args, _ := json.Marshal(map[string]any{"session_id": target})
	observers := managedExecObserversForTest(t, owner)
	_, err := invokeExecObserver(ctx, observers["wait_exec"], string(args))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("foreground readiness masked cancellation: %v", err)
	}
	live, err := invokeExecObserver(t.Context(), observers["exec_status"], string(args))
	if err != nil || live.Status != "running" || live.SessionID != target {
		t.Fatalf("wait cancellation stopped its target: %+v, %v", live, err)
	}
}
