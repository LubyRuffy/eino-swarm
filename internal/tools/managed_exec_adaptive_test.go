//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestAdaptiveObservationUsesLearnedWaitWithoutMovingProcessOwnership(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	base := workspaceExecForTest(t, dir)
	managed := ManagedExec(base, owner)
	node, err := compose.NewToolNode(t.Context(), &compose.ToolsNodeConfig{
		Tools: append([]tool.BaseTool{managed}, owner.ObservationTools()...),
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(id, name, args string) observedExecReceipt {
		t.Helper()
		output, err := node.Invoke(t.Context(), schema.AssistantMessage("", []schema.ToolCall{{
			ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args},
		}}))
		if err != nil || len(output) != 1 || output[0].ToolCallID != id {
			t.Fatalf("invoke %s: outputs=%+v, err=%v", name, output, err)
		}
		var result observedExecReceipt
		if err := json.Unmarshal([]byte(output[0].Content), &result); err != nil {
			t.Fatalf("decode %s: %s, %v", name, output[0].Content, err)
		}
		return result
	}
	begin := func(id, marker string) string {
		t.Helper()
		owner.AdvanceModelRequest()
		owner.NoteStandaloneModelCall(id, false)
		cmd := fmt.Sprintf("while [ ! -f %s ]; do command sleep 0.02; done; printf %s", marker, marker)
		args := fmt.Sprintf(`{"command":%q}`, cmd)
		result := invoke(id, einoexec.ToolName, args)
		if result.Status != "running" || result.SessionID == "" {
			t.Fatalf("exec must still yield to its owner: %+v", result)
		}
		return result.SessionID
	}
	wait := func(id, session string) observedExecReceipt {
		t.Helper()
		return invoke(id, "wait_exec", fmt.Sprintf(`{"session_id":%q,"idle_timeout_ms":1000}`, session))
	}

	first := begin("first-exec", "first-release")
	writeExecObservationMarker(t, dir, "first-release")
	owner.AdvanceModelRequest()
	owner.NoteStandaloneModelCall("dependent-wait", true)
	witness := wait("dependent-wait", first)
	if witness.Status != "exited" || witness.Stdout != "first-release" {
		t.Fatalf("the dependent wait did not observe a real exit: %+v", witness)
	}
	if got, err := owner.TakeReady(); err != nil || len(got) != 1 || got[0].SessionID != first {
		t.Fatalf("learner disturbed first terminal delivery: %+v, %v", got, err)
	}

	second := begin("second-exec", "second-release")
	observation := &managedWaitEntryContext{Context: t.Context(), entered: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- owner.AwaitAdaptiveObservation(observation) }()
	select {
	case <-observation.entered:
	case err := <-result:
		t.Fatalf("learned observation returned before the command settled: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("adaptive wait did not observe the owned command")
	}
	writeExecObservationMarker(t, dir, "second-release")
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process exit did not wake adaptive observation")
	}
	if got, err := owner.TakeReady(); err != nil || len(got) != 1 || got[0].SessionID != second {
		t.Fatalf("adaptive wait did not leave the terminal receipt for delivery: %+v, %v", got, err)
	}

	third := begin("third-exec", "third-release")
	observeCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.AwaitAdaptiveObservation(observeCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation did not return its own context error: %v", err)
	}
	retryCtx, finishRetry := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer finishRetry()
	if err := owner.AwaitAdaptiveObservation(retryCtx); err != nil {
		t.Fatalf("retrying the same model boundary repeated a cancelled observation: %v", err)
	}
	status := invoke("status-after-cancel", "exec_status", fmt.Sprintf(`{"session_id":%q}`, third))
	if status.Status != "running" {
		t.Fatalf("observation cancellation killed its turn-owned process: %+v", status)
	}
	writeExecObservationMarker(t, dir, "third-release")
	last := wait("wait-after-cancel", third)
	if last.Status != "exited" || last.Stdout != "third-release" {
		t.Fatalf("cancelled observation lost the original process: %+v", last)
	}
}

func TestAdaptiveObservationRequiresARecentOwnedNormalExit(t *testing.T) {
	exit := 7
	view := managedExecView{OriginalCallID: "original", SessionID: "owned", Status: "exited",
		ExitCode: &exit, elapsed: managedExecAdaptiveWindow}
	for _, tc := range []struct {
		name        string
		view        managedExecView
		original    managedExecCall
		witness     managedExecStandaloneCall
		cancel      bool
		cancelOwner bool
		want        bool
	}{
		{name: "normal exit at the upper bound", view: view, want: true},
		{name: "below the millisecond report resolution still qualifies",
			view: managedExecView{OriginalCallID: "original", SessionID: "owned", Status: "exited",
				ExitCode: &exit, elapsed: managedExecObservationWindow + time.Nanosecond}, want: true},
		{name: "at the default observation", view: managedExecView{OriginalCallID: "original",
			SessionID: "owned", Status: "exited", ExitCode: &exit, elapsed: managedExecObservationWindow}},
		{name: "over the observation cap", view: managedExecView{OriginalCallID: "original",
			SessionID: "owned", Status: "exited", ExitCode: &exit, elapsed: managedExecAdaptiveWindow + time.Nanosecond}},
		{name: "idle observation timeout", view: managedExecView{OriginalCallID: "original",
			SessionID: "owned", Status: "running", ObservationTimedOut: true, elapsed: 2 * time.Second}},
		{name: "hard command timeout", view: managedExecView{OriginalCallID: "original",
			SessionID: "owned", Status: "timed_out", ExitCode: &exit, elapsed: 2 * time.Second}},
		{name: "missing exit code", view: managedExecView{OriginalCallID: "original",
			SessionID: "owned", Status: "exited", elapsed: 2 * time.Second}},
		{name: "foreign session", view: view, original: managedExecCall{sessionID: "other", standaloneRequest: 2}},
		{name: "older original", view: view, original: managedExecCall{sessionID: "owned", standaloneRequest: 1}},
		{name: "original was explicit or not alone", view: view, original: managedExecCall{sessionID: "owned"}},
		{name: "different wait call", view: view, witness: managedExecStandaloneCall{id: "other", wait: true}},
		{name: "status call never teaches", view: view, witness: managedExecStandaloneCall{id: "wait"}},
		{name: "wait canceled", view: view, cancel: true},
		{name: "owner canceled", view: view, cancelOwner: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := NewManagedExecSessions(t.Context())
			defer owner.cancel()
			owner.request = 3
			owner.calls["original"] = tc.original
			if tc.original.sessionID == "" {
				owner.calls["original"] = managedExecCall{sessionID: "owned", standaloneRequest: 2}
			}
			owner.standalone = tc.witness
			if tc.witness.id == "" {
				owner.standalone = managedExecStandaloneCall{id: "wait", wait: true}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			if tc.cancelOwner {
				owner.cancel()
			}
			owner.learnAdaptiveObservation(ctx, "wait", tc.view)
			if owner.adaptiveObservation != tc.want {
				t.Fatalf("observation preference=%v, want %v", owner.adaptiveObservation, tc.want)
			}
		})
	}
}

func TestAdaptiveObservationPreservesExplicitYieldAndOtherPendingWork(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	owner.adaptiveObservation = true
	managed := ManagedExec(workspaceExecForTest(t, dir), owner).(tool.InvokableTool)

	for _, tc := range []struct {
		name, explicit string
		note           bool
	}{
		{name: "explicit-fast", explicit: `,"yield_after_ms":25`, note: true},
		{name: "explicit-default", explicit: `,"yield_after_ms":0`, note: true},
		{name: "explicit-null", explicit: `,"yield_after_ms":null`, note: true},
		{name: "multi-tool-or-unmarked", explicit: "", note: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner.AdvanceModelRequest()
			if tc.note {
				owner.NoteStandaloneModelCall(tc.name, false)
			}
			ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, tc.name)
			cmd := "while [ ! -f " + tc.name + " ]; do command sleep 0.02; done"
			result, err := invokeExecObserver(ctx, managed, fmt.Sprintf(`{"command":%q%s}`, cmd, tc.explicit))
			if err != nil || result.Status != "running" {
				t.Fatalf("the owned process must yield with the requested observation: %+v, %v", result, err)
			}
			observation, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
			defer cancel()
			if err := owner.AwaitAdaptiveObservation(observation); err != nil {
				t.Fatalf("the process was held for an unrequested observation: %v", err)
			}
			writeExecObservationMarker(t, dir, tc.name)
			if got, err := owner.Wait(t.Context()); err != nil || len(got) != 1 || got[0].SessionID != result.SessionID {
				t.Fatalf("terminal receipt for the original command was lost: %+v, %v", got, err)
			}
		})
	}
	owner.AdvanceModelRequest()
	unrelated := launchExecToObserve(t, owner, workspaceExecForTest(t, dir), "unrelated",
		"while [ ! -f finish-both ]; do command sleep 0.02; done", 0)
	owner.AdvanceModelRequest()
	owner.NoteStandaloneModelCall("with-sibling", false)
	singleton := launchExecToObserve(t, owner, workspaceExecForTest(t, dir), "with-sibling",
		"while [ ! -f finish-both ]; do command sleep 0.02; done", 0)
	observation, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if err := owner.AwaitAdaptiveObservation(observation); err != nil {
		t.Fatalf("an unrelated command was pending when adaptive observation blocked: %v", err)
	}
	writeExecObservationMarker(t, dir, "finish-both")
	seen := make(map[string]bool)
	for len(seen) < 2 {
		got, err := owner.Wait(t.Context())
		if err != nil || len(got) == 0 {
			t.Fatalf("one of the original terminal receipts was lost: %+v, %v", got, err)
		}
		for _, receipt := range got {
			seen[receipt.SessionID] = true
		}
	}
	if !seen[unrelated] || !seen[singleton] {
		t.Fatalf("wrong terminal receipts: %v", seen)
	}
}

func TestAdaptiveObservationCapsWaitingAndReturnsToImmediateYield(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	owner.adaptiveObservation = true
	base := workspaceExecForTest(t, dir)
	begin := func(id, marker string) string {
		t.Helper()
		owner.AdvanceModelRequest()
		owner.NoteStandaloneModelCall(id, false)
		return launchExecToObserve(t, owner, base, id,
			"while [ ! -f "+marker+" ]; do command sleep 0.02; done", 0)
	}
	long := begin("long-command", "long-release")
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	if err := owner.AwaitAdaptiveObservation(ctx); err != nil {
		t.Fatalf("bounded observation did not return before the enclosing wait: %v", err)
	}
	if !owner.Pending() {
		t.Fatal("observation cap canceled the turn-owned process")
	}
	writeExecObservationMarker(t, dir, "long-release")
	if got, err := owner.Wait(t.Context()); err != nil || len(got) != 1 || got[0].SessionID != long {
		t.Fatalf("original process could not complete after observation capped: %+v, %v", got, err)
	}
	next := begin("next-command", "next-release")
	short, done := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer done()
	if err := owner.AwaitAdaptiveObservation(short); err != nil {
		t.Fatalf("a long-running observation did not reset the adaptive preference: %v", err)
	}
	writeExecObservationMarker(t, dir, "next-release")
	if got, err := owner.Wait(t.Context()); err != nil || len(got) != 1 || got[0].SessionID != next {
		t.Fatalf("next terminal receipt was lost: %+v, %v", got, err)
	}
}
