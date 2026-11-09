//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
)

type observedExecReceipt struct {
	SessionID           string `json:"session_id"`
	Status              string `json:"status"`
	Code                string `json:"code"`
	Stdout              string `json:"stdout"`
	Stderr              string `json:"stderr"`
	Error               string `json:"error"`
	ExitCode            *int   `json:"exit_code"`
	TimeoutMS           int64  `json:"timeout_ms"`
	ObservationTimedOut bool   `json:"observation_timed_out"`
	Truncated           bool   `json:"truncated"`
}

func managedExecObserversForTest(t *testing.T, owner *ManagedExecSessions) map[string]tool.InvokableTool {
	t.Helper()
	factory, ok := any(owner).(interface{ ObservationTools() []tool.BaseTool })
	if !ok {
		t.Fatal("the managed exec owner cannot expose ID-based observations")
	}
	observers := make(map[string]tool.InvokableTool)
	for _, observer := range factory.ObservationTools() {
		info, err := observer.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		observers[info.Name] = observer.(tool.InvokableTool)
	}
	if len(observers) != 2 || observers["exec_status"] == nil || observers["wait_exec"] == nil {
		t.Fatalf("wrong observer tools: %v", observers)
	}
	return observers
}

func invokeExecObserver(ctx context.Context, observer tool.InvokableTool, args string) (observedExecReceipt, error) {
	out, err := observer.InvokableRun(ctx, args)
	var receipt observedExecReceipt
	if err == nil {
		err = json.Unmarshal([]byte(out), &receipt)
	}
	return receipt, err
}

func launchExecToObserve(t *testing.T, owner *ManagedExecSessions, base tool.BaseTool, callID, command string, timeoutMS int) string {
	t.Helper()
	exec := ManagedExec(base, owner).(tool.InvokableTool)
	ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, callID)
	args, err := json.Marshal(map[string]any{"command": command, "max_output_kb": 1, "timeout_ms": timeoutMS})
	if err != nil {
		t.Fatal(err)
	}
	result, err := invokeExecObserver(ctx, exec, string(args))
	if err != nil || result.Status != "running" || !strings.HasPrefix(result.SessionID, "exec-") {
		t.Fatalf("original command did not yield: %+v, %v", result, err)
	}
	return result.SessionID
}

type managedWaitEntryContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *managedWaitEntryContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func writeExecObservationMarker(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestManagedExecObservationFollowsOutputAndRetainsTerminalResult(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	observers := managedExecObserversForTest(t, owner)
	id := launchExecToObserve(t, owner, workspaceExecForTest(t, dir), "observed-call", `printf first; while [ ! -e emit ]; do sleep 0.02; done; printf '%08000d' 1; printf recent; printf warning >&2; while [ ! -e finish ]; do sleep 0.02; done; printf settled; exit 7`, 0)
	statusArgs, _ := json.Marshal(map[string]any{"session_id": id})
	waitArgs, _ := json.Marshal(map[string]any{"session_id": id, "idle_timeout_ms": 900})
	status, err := invokeExecObserver(t.Context(), observers["exec_status"], string(statusArgs))
	if err != nil || status.Status != "running" || status.SessionID != id || status.Stdout != "first" || status.ExitCode != nil {
		t.Fatalf("status did not show original running output: %+v, %v", status, err)
	}

	type outcome struct {
		receipt observedExecReceipt
		err     error
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entry := &managedWaitEntryContext{Context: ctx, entered: make(chan struct{})}
	observed := make(chan outcome, 2)
	go func() {
		got, err := invokeExecObserver(entry, observers["wait_exec"], string(waitArgs))
		observed <- outcome{got, err}
	}()
	select {
	case <-entry.entered:
	case <-ctx.Done():
		t.Fatal("wait tool did not enter its observation")
	}
	began := time.Now()
	time.Sleep(450 * time.Millisecond)
	writeExecObservationMarker(t, dir, "emit")
	select {
	case got := <-observed:
		if got.err != nil || got.receipt.Status != "running" || !got.receipt.ObservationTimedOut ||
			!got.receipt.Truncated || !strings.HasSuffix(got.receipt.Stdout, "recent") ||
			got.receipt.Stderr != "warning" || got.receipt.ExitCode != nil || time.Since(began) < 1150*time.Millisecond {
			t.Fatalf("idle budget was not renewed by real recent output: %+v, %v, elapsed %v", got.receipt, got.err, time.Since(began))
		}
	case <-ctx.Done():
		t.Fatal("quiet command did not return a bounded observation")
	}
	if !owner.Pending() {
		t.Fatal("observation expiry canceled or consumed the original command")
	}

	preempt, interrupt := context.WithCancel(t.Context())
	interrupted := &managedWaitEntryContext{Context: preempt, entered: make(chan struct{})}
	go func() {
		got, err := invokeExecObserver(interrupted, observers["wait_exec"], string(waitArgs))
		observed <- outcome{got, err}
	}()
	select {
	case <-interrupted.entered:
		interrupt()
	case <-ctx.Done():
		interrupt()
		t.Fatal("second wait did not enter its observation")
	}
	got := <-observed
	if !errors.Is(got.err, context.Canceled) || !owner.Pending() {
		t.Fatalf("preempt did not return promptly or canceled the process: %+v, %v", got.receipt, got.err)
	}

	for range 2 {
		go func() {
			receipt, err := invokeExecObserver(ctx, observers["wait_exec"], string(statusArgs))
			observed <- outcome{receipt, err}
		}()
	}
	writeExecObservationMarker(t, dir, "finish")
	for range 2 {
		got := <-observed
		if got.err != nil || got.receipt.SessionID != id || got.receipt.Status != "exited" ||
			got.receipt.ExitCode == nil || *got.receipt.ExitCode != 7 || got.receipt.Error == "" ||
			!strings.HasSuffix(got.receipt.Stdout, "recentsettled") || got.receipt.ObservationTimedOut {
			t.Fatalf("terminal observation did not describe original process: %+v, %v", got.receipt, got.err)
		}
	}
	original, err := owner.Wait(ctx)
	if err != nil || len(original) != 1 || original[0].SessionID != id || original[0].CallID != "observed-call" {
		t.Fatalf("observers consumed original terminal delivery: %+v, %v", original, err)
	}
	status, err = invokeExecObserver(ctx, observers["exec_status"], string(statusArgs))
	if err != nil || status.Status != "exited" || status.ExitCode == nil || *status.ExitCode != 7 ||
		!strings.HasSuffix(status.Stdout, "recentsettled") {
		t.Fatalf("terminal lookup disappeared after original delivery: %+v, %v", status, err)
	}

	other := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = other.Close() })
	second := launchExecToObserve(t, other, workspaceExecForTest(t, dir), "observed-call", `while [ ! -e release-other ]; do sleep 0.02; done`, 0)
	if second == id {
		t.Fatal("fresh turn reused a previous session ID")
	}
	otherTools := managedExecObserversForTest(t, other)
	foreign, err := invokeExecObserver(t.Context(), otherTools["exec_status"], string(statusArgs))
	if err != nil || foreign.Status != "unknown" || foreign.Code != "unknown_exec_session" || foreign.Stdout != "" {
		t.Fatalf("foreign session lookup did not fail closed: %+v, %v", foreign, err)
	}
}

func TestManagedExecObservationKeepsExplicitProcessDeadline(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	observers := managedExecObserversForTest(t, owner)
	const timeoutMS = 2200
	id := launchExecToObserve(t, owner, workspaceExecForTest(t, dir), "observed-call", `while true; do printf heartbeat; sleep 0.08; done`, timeoutMS)
	args, _ := json.Marshal(map[string]any{"session_id": id, "idle_timeout_ms": 550})
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	receipt, err := invokeExecObserver(ctx, observers["wait_exec"], string(args))
	if err != nil || receipt.Status != "timed_out" || receipt.TimeoutMS != timeoutMS ||
		receipt.ObservationTimedOut || receipt.ExitCode == nil || *receipt.ExitCode == 0 ||
		!strings.Contains(receipt.Error, "deadline") || !strings.Contains(receipt.Stdout, "heartbeat") {
		t.Fatalf("renewing the observation altered an explicit command deadline: %+v, %v", receipt, err)
	}
}

func TestManagedExecObservationRejectsInvalidRequests(t *testing.T) {
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	observers := managedExecObserversForTest(t, owner)
	for _, input := range []struct {
		tool, args, code string
	}{
		{"exec_status", `{`, "invalid_exec_observation"},
		{"exec_status", `{"session_id":" "}`, "invalid_exec_observation"},
		{"wait_exec", `{"session_id":"exec-unknown","idle_timeout_ms":-1}`, "invalid_exec_observation"},
		{"wait_exec", `{"session_id":"exec-unknown","idle_timeout_ms":1.5}`, "invalid_exec_observation"},
		{"wait_exec", `{"session_id":"exec-unknown","idle_timeout_ms":9223372036855}`, "invalid_exec_observation"},
		{"wait_exec", `{"session_id":"exec-unknown","idle_timeout_ms":0}`, "unknown_exec_session"},
	} {
		receipt, err := invokeExecObserver(t.Context(), observers[input.tool], input.args)
		if err != nil || receipt.Code != input.code || receipt.Error == "" {
			t.Errorf("%s %s gave %+v, %v", input.tool, input.args, receipt, err)
		}
	}
}

// A targeted wait keeps the requested identity, but another owned completion
// must return control so its original terminal result can be collected.
func TestManagedExecObservationWaitsForTheRequestedProcess(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	base := workspaceExecForTest(t, dir)
	observers := managedExecObserversForTest(t, owner)
	target := launchExecToObserve(t, owner, base, "target", `while [ ! -e target ]; do sleep 0.02; done; printf target`, 0)
	other := launchExecToObserve(t, owner, base, "other", `while [ ! -e other ]; do sleep 0.02; done; printf other`, 0)
	args, _ := json.Marshal(map[string]any{"session_id": target, "idle_timeout_ms": 60000})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	entry := &managedWaitEntryContext{Context: ctx, entered: make(chan struct{})}
	type outcome struct {
		receipt observedExecReceipt
		err     error
	}
	awaited := make(chan outcome, 1)
	go func() {
		receipt, err := invokeExecObserver(entry, observers["wait_exec"], string(args))
		awaited <- outcome{receipt, err}
	}()
	select {
	case <-entry.entered:
	case <-ctx.Done():
		t.Fatal("targeted wait did not enter")
	}
	writeExecObservationMarker(t, dir, "other")
	var got outcome
	select {
	case got = <-awaited:
	case <-ctx.Done():
		t.Fatal("peer completion did not wake the targeted wait")
	}
	if got.err != nil || got.receipt.SessionID != target || got.receipt.Status != "running" || got.receipt.Code != "peer_exec_settled" || got.receipt.ObservationTimedOut || got.receipt.ExitCode != nil {
		t.Fatalf("peer wake changed target identity or outcome: %+v, %v", got.receipt, got.err)
	}
	ready, err := owner.TakeReady()
	if err != nil || len(ready) != 1 || ready[0].SessionID != other || ready[0].CallID != "other" {
		t.Fatalf("lost peer original result: %+v, %v", ready, err)
	}
	if again, err := owner.TakeReady(); err != nil || len(again) != 0 {
		t.Fatalf("peer collected twice: %+v, %v", again, err)
	}
	live, err := invokeExecObserver(ctx, observers["exec_status"], string(args))
	if err != nil || live.Status != "running" || live.SessionID != target {
		t.Fatalf("peer wake stopped target: %+v, %v", live, err)
	}
	writeExecObservationMarker(t, dir, "target")
	terminal, err := invokeExecObserver(ctx, observers["wait_exec"], string(args))
	if err != nil || terminal.Status != "exited" || terminal.SessionID != target || terminal.Stdout != "target" || terminal.Code != "" {
		t.Fatalf("wrong target terminal receipt: %+v, %v", terminal, err)
	}
}

func TestManagedExecObservationCapsRecentOutputWithOriginalToolBudget(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Tools.ResultMaxChars = 800
	set, err := Build(t.Context(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	var base tool.BaseTool
	for i, name := range set.Names {
		if name == einoexec.ToolName {
			base = set.Tools[i]
			break
		}
	}
	if base == nil {
		t.Fatal("missing workspace exec")
	}
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	observers := managedExecObserversForTest(t, owner)
	id := launchExecToObserve(t, owner, base, "budget",
		`printf '界%.0s' {1..2500}; printf latest; while [ ! -e finish ]; do sleep 0.02; done; exit 7`, 0)
	args, _ := json.Marshal(map[string]any{"session_id": id})
	for _, name := range []string{"exec_status", "wait_exec"} {
		if name == "wait_exec" {
			writeExecObservationMarker(t, dir, "finish")
		}
		out, err := observers[name].InvokableRun(t.Context(), string(args))
		var result observedExecReceipt
		if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.SessionID != id ||
			utf8.RuneCountInString(out) > cfg.Tools.ResultMaxChars || !result.Truncated ||
			!strings.HasSuffix(result.Stdout, "latest") || strings.ContainsRune(result.Stdout, utf8.RuneError) {
			t.Fatalf("%s lost recent output or original budget: %s, %v", name, out, err)
		}
		if name == "wait_exec" && (result.ExitCode == nil || *result.ExitCode != 7 || result.Status != "exited") {
			t.Fatalf("capped terminal result lost original exit: %+v", result)
		}
	}
}
