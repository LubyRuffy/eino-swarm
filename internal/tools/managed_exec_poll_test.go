//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
)

type managedPollOutcome struct {
	text string
	err  error
}

func runManagedPollTool(ctx context.Context, tl tool.InvokableTool, callID, command string, observationMS ...int) managedPollOutcome {
	ctx = BindExecOutput(ctx, func(string) {}, einoexec.ToolName, callID)
	params := map[string]any{"command": command}
	if len(observationMS) > 0 {
		params["yield_after_ms"] = observationMS[0]
	}
	args, _ := json.Marshal(params)
	out, err := tl.InvokableRun(ctx, string(args))
	return managedPollOutcome{text: out, err: err}
}

func TestManagedWorkspaceExecPollsWakeOnFirstExitWithoutStartingSleep(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	probe := "#!/bin/sh\nprintf probe >> probes\nprintf 'observed\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "pip"), []byte(probe), 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	for _, id := range []string{"slow", "fast"} {
		got := runManagedPollTool(t.Context(), tl, id, "while [ ! -e release-"+id+" ]; do sleep 0.02; done; printf "+id)
		if got.err != nil || !sessions.IsProvisional(id, got.text) {
			t.Fatalf("owned command %s did not yield: %s, %v", id, got.text, got.err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	type poll struct {
		id, command, code, stdout string
	}
	polls := []poll{
		{id: "bare", command: "sleep 31", code: "background_exec_ready"},
		{id: "probe", command: "echo observing; sleep 37; pip list 2>/dev/null | head -4", stdout: "observing\nobserved\n"},
	}
	output := make([]chan managedPollOutcome, len(polls))
	for i, p := range polls {
		output[i] = make(chan managedPollOutcome, 1)
		// This assertion needs the probe's terminal result. The normal one-second
		// observation may truthfully yield a running session on a busy host.
		// Keep the same three-second context deadline, which still forbids sleep.
		go func() { output[i] <- runManagedPollTool(ctx, tl, p.id, p.command, 3000) }()
	}
	// Neither polling invocation should return a timer or run the probe while
	// the original commands are both still running.
	select {
	case got := <-output[0]:
		t.Fatalf("bare wait returned without a background result: %+v", got)
	case got := <-output[1]:
		t.Fatalf("probe wait returned without a background result: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dir, "probes")); !os.IsNotExist(err) {
		t.Fatalf("probe ran before completion: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release-fast"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, p := range polls {
		select {
		case got := <-output[i]:
			var receipt struct {
				Waited   bool   `json:"waited_for_background"`
				WaitMS   *int64 `json:"background_wait_ms"`
				Code     string `json:"code"`
				ExitCode *int   `json:"exit_code"`
				Stdout   string `json:"stdout"`
				Command  string `json:"full_command"`
			}
			if got.err != nil || json.Unmarshal([]byte(got.text), &receipt) != nil ||
				!receipt.Waited || receipt.WaitMS == nil || *receipt.WaitMS < 0 || receipt.Command != p.command ||
				receipt.Code != p.code || receipt.Stdout != p.stdout ||
				(p.code != "" && receipt.ExitCode != nil) ||
				(p.code == "" && (receipt.ExitCode == nil || *receipt.ExitCode != 0)) {
				t.Fatalf("wait %s did not return a truthful completion receipt: %s, %v", p.id, got.text, got.err)
			}
		case <-ctx.Done():
			t.Fatalf("wait %s did not wake on the first actual exit", p.id)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "probes")); err != nil || string(data) != "probe" {
		t.Fatalf("read-only probe did not execute exactly once: %q, %v", data, err)
	}
	sessions.mu.Lock()
	launches := sessions.next
	sessions.mu.Unlock()
	if launches != 3 {
		t.Fatalf("polls should launch only the probe in addition to the two owned commands: %d launches", launches)
	}
	receipts, err := sessions.TakeReady()
	if err != nil || len(receipts) != 1 || receipts[0].CallID != "fast" || !strings.Contains(receipts[0].Result, `"stdout":"fast"`) || !sessions.Pending() {
		t.Fatalf("polls consumed the original terminal or stopped the slower job: %+v, %v", receipts, err)
	}
}

func TestManagedWorkspaceExecCanceledPollDoesNotStartFollowup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "pip3"), []byte("#!/bin/sh\ntouch probe-ran\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	got := runManagedPollTool(t.Context(), tl, "real", "while [ ! -e release ]; do sleep 0.02; done")
	if got.err != nil || !sessions.IsProvisional("real", got.text) {
		t.Fatalf("owned command did not yield: %s, %v", got.text, got.err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	got = runManagedPollTool(ctx, tl, "poll", "sleep 34; pip3 list 2>/dev/null")
	if got.err != nil || !strings.Contains(got.text, "context deadline exceeded") || strings.Contains(got.text, "waited_for_background") {
		t.Fatalf("canceled poll claimed completion: %s, %v", got.text, got.err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	if ready, err := sessions.Wait(wait); err != nil || len(ready) != 1 || ready[0].CallID != "real" {
		t.Fatalf("canceling poll lost the original process result: %+v, %v", ready, err)
	}
	sessions.mu.Lock()
	launches := sessions.next
	sessions.mu.Unlock()
	if launches != 1 {
		t.Fatalf("canceled poll launched a shell: %d launches", launches)
	}
	if _, err := os.Stat(filepath.Join(dir, "probe-ran")); !os.IsNotExist(err) {
		t.Fatalf("canceled poll ran its follow-up probe: %v", err)
	}
}

func TestManagedWorkspaceExecPreservesLiteralSleepAndLegacyBias(t *testing.T) {
	dir := t.TempDir()
	legacy := workspaceExecForTest(t, dir).(tool.InvokableTool)
	managed := ManagedExec(legacy).(tool.InvokableTool)
	const args = `{"command":"sleep 5; touch marker","timeout_ms":2600}`
	got, err := managed.InvokableRun(t.Context(), args)
	if err != nil || !strings.Contains(got, "context deadline exceeded") {
		t.Fatalf("managed exec shortened a real delay: %s, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
		t.Fatalf("real delay passed before its deadline: %v", err)
	}
	got, err = legacy.InvokableRun(t.Context(), args)
	if err != nil || !strings.Contains(got, `"exit_code":0`) {
		t.Fatalf("default-off exec lost its existing sleep behavior: %s, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Fatalf("default-off exec did not run the original follow-up: %v", err)
	}
}

func TestManagedExecPollRequiresEntireStaticCommand(t *testing.T) {
	for _, tc := range []struct{ input, followup string }{
		{"sleep 5", ""},
		{"sleep 5.25 && echo marker", ": && echo marker"},
		{"echo before ; sleep 18 ; echo after", "echo before ; : ; echo after"},
		{"sleep 8; echo checked; pip3 list 2> /dev/null | tail -12", ":; echo checked; pip3 list 2> /dev/null | tail -12"},
	} {
		if got, ok := managedExecPollFollowup(tc.input); !ok || got != tc.followup {
			t.Errorf("static poll %q produced %q, %t, want %q", tc.input, got, ok, tc.followup)
		}
	}
	for _, literal := range []string{
		"sleep 4.9", "sleep 30s", "command sleep 5", "sleep 5; touch marker", "sleep 5 && curl endpoint",
		"sleep 5 || echo fallback", "sleep 5; sleep 5", "sleep 5; echo marker > file",
		"sleep \"5\"; echo marker", "sleep 5; echo \"$(touch file)\"", "echo 'sleep 5'",
		"sleep 5; pip list 2>/dev/null > file", "sleep 5; pip list 2>/dev/null | head -4 > file",
		"sleep 5\necho marker", "sleep ${DELAY}; echo marker", "sleep 5; pip install package",
		"sleep 99999999999999999999999999999999999",
	} {
		if got, ok := managedExecPollFollowup(literal); ok {
			t.Errorf("literal command %q was rewritten as %q", literal, got)
		}
	}
}

func TestManagedWorkspaceExecPollRequiresPendingCommandAndOrdinaryInputs(t *testing.T) {
	dir := t.TempDir()
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	literal := runManagedPollTool(t.Context(), tl, "sole-command", "sleep 6 && echo delayed")
	if literal.err != nil || !sessions.IsProvisional("sole-command", literal.text) {
		t.Fatalf("sole command did not start its actual delay: %+v", literal)
	}
	t.Run("shell startup file", func(t *testing.T) {
		startup := filepath.Join(dir, "bash-env")
		if err := os.WriteFile(startup, []byte("sleep() { printf customized; }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BASH_ENV", startup)
		got := runManagedPollTool(t.Context(), tl, "startup-file", "sleep 7")
		if got.err != nil || !strings.Contains(got.text, `"stdout":"customized"`) ||
			strings.Contains(got.text, "waited_for_background") {
			t.Fatalf("inherited shell startup changed semantics during poll detection: %+v", got)
		}
	})
	for _, tc := range []struct{ name, extra string }{
		{"environment", `,"env":{"TEST_VALUE":"explicit"}`},
		{"stdin", `,"stdin":"explicit"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions.mu.Lock()
			before := sessions.next
			sessions.mu.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
			defer cancel()
			ctx = BindExecOutput(ctx, func(string) {}, einoexec.ToolName, tc.name)
			result, err := tl.InvokableRun(ctx, `{"command":"sleep 7 && echo delayed"`+tc.extra+`}`)
			var receipt struct {
				Command string `json:"full_command"`
				Error   string `json:"error"`
				Waited  bool   `json:"waited_for_background"`
			}
			if err != nil || json.Unmarshal([]byte(result), &receipt) != nil ||
				!strings.Contains(receipt.Error, "command timeout or canceled") ||
				receipt.Waited || receipt.Command != "sleep 7 && echo delayed" {
				t.Fatalf("explicit input did not keep a real unmodified delay: %s, %v", result, err)
			}
			sessions.mu.Lock()
			launched := sessions.next
			sessions.mu.Unlock()
			if launched != before+1 {
				t.Fatalf("explicit input did not start a real shell: %d → %d", before, launched)
			}
		})
	}
}

func TestManagedWorkspaceExecPollDeadlineDoesNotStopOriginalCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "pip"), []byte("#!/bin/sh\ntouch probe-ran\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	got := runManagedPollTool(t.Context(), tl, "real", "while [ ! -e release ]; do sleep 0.02; done; printf result")
	if got.err != nil || !sessions.IsProvisional("real", got.text) {
		t.Fatalf("owned command did not yield: %+v", got)
	}
	ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, "poll")
	out, err := tl.InvokableRun(ctx, `{"command":"sleep 31; pip list 2>/dev/null","timeout_ms":130}`)
	var receipt struct {
		Code     string `json:"code"`
		Waited   bool   `json:"waited_for_background"`
		Failed   bool   `json:"failed"`
		Timeout  int64  `json:"timeout_ms"`
		ExitCode *int   `json:"exit_code"`
	}
	if err != nil || json.Unmarshal([]byte(out), &receipt) != nil || receipt.Code != "background_exec_wait_timeout" ||
		!receipt.Waited || !receipt.Failed || receipt.Timeout != 130 || receipt.ExitCode != nil {
		t.Fatalf("hard wait deadline did not yield an honest refusal: %s, %v", out, err)
	}
	sessions.mu.Lock()
	launches := sessions.next
	sessions.mu.Unlock()
	if launches != 1 {
		t.Fatalf("timed out wait started an extra shell: %d", launches)
	}
	if _, err := os.Stat(filepath.Join(dir, "probe-ran")); !os.IsNotExist(err) {
		t.Fatalf("timed out wait ran the post-exit probe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	gotReceipts, err := sessions.Wait(wait)
	if err != nil || len(gotReceipts) != 1 || gotReceipts[0].CallID != "real" ||
		!strings.Contains(gotReceipts[0].Result, `"stdout":"result"`) {
		t.Fatalf("poll deadline canceled the original command: %+v, %v", gotReceipts, err)
	}
}

func TestManagedWorkspaceExecPollAndProbeShareOneDeadline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "pip"), []byte("#!/bin/sh\nprintf started\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	got := runManagedPollTool(t.Context(), tl, "real", "while [ ! -e release ]; do sleep 0.02; done")
	if got.err != nil || !sessions.IsProvisional("real", got.text) {
		t.Fatalf("owned command did not yield: %+v", got)
	}
	done := make(chan managedPollOutcome, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ctx = BindExecOutput(ctx, func(string) {}, einoexec.ToolName, "poll")
	go func() {
		out, err := tl.InvokableRun(ctx, `{"command":"sleep 31; pip list 2>/dev/null","timeout_ms":1200}`)
		done <- managedPollOutcome{text: out, err: err}
	}()
	select {
	case out := <-done:
		t.Fatalf("wait finished before command exit: %+v", out)
	case <-time.After(600 * time.Millisecond):
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		var receipt struct {
			Waited  bool   `json:"waited_for_background"`
			Timeout int64  `json:"timeout_ms"`
			WaitMS  int64  `json:"background_wait_ms"`
			RunMS   int64  `json:"elapsed_ms"`
			Stdout  string `json:"stdout"`
			Error   string `json:"error"`
		}
		if out.err != nil || json.Unmarshal([]byte(out.text), &receipt) != nil || !receipt.Waited ||
			receipt.Timeout != 1200 || receipt.WaitMS < 400 || receipt.RunMS >= 1000 ||
			receipt.Stdout != "started" || !strings.Contains(receipt.Error, "context deadline exceeded") {
			t.Fatalf("probe reset the hard deadline or lost its actual output: %s, %v", out.text, out.err)
		}
	case <-ctx.Done():
		t.Fatal("probe failed to honor the shared deadline")
	}
}
