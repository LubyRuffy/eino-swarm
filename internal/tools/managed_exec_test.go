//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
)

func workspaceExecForTest(t *testing.T, dir string) tool.BaseTool {
	t.Helper()
	set, err := Build(t.Context(), config.Default(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range set.Names {
		if name == einoexec.ToolName {
			return set.Tools[i]
		}
	}
	t.Fatal("workspace exec missing")
	return nil
}

func TestManagedWorkspaceExecSoftYieldsAndKeepsOriginalProcess(t *testing.T) {
	dir := t.TempDir()
	sessions := NewManagedExecSessions(t.Context())
	defer func() {
		if err := sessions.Close(); err != nil {
			t.Error(err)
		}
	}()
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	const callID = "background-call"
	live := make(chan string, 10)
	ctx = BindExecOutput(ctx, func(text string) { live <- text }, einoexec.ToolName, callID)
	const args = `{"command":"printf 'once\\n' >> launches; while [ ! -e release ]; do sleep 0.02; done; printf finished","timeout_ms":3600000}`
	type outcome struct {
		text string
		err  error
	}
	result := make(chan outcome, 1)
	go func() {
		text, err := tl.InvokableRun(ctx, args)
		result <- outcome{text, err}
	}()
	var receipt struct {
		Status    string `json:"status"`
		SessionID string `json:"session_id"`
		ExitCode  *int   `json:"exit_code"`
	}
	select {
	case got := <-result:
		if got.err != nil || json.Unmarshal([]byte(got.text), &receipt) != nil ||
			receipt.Status != "running" || receipt.SessionID == "" || receipt.ExitCode != nil {
			t.Fatalf("soft wait did not yield an owned running command: %s, %v", got.text, got.err)
		}
		if !sessions.IsProvisional(callID, got.text) || sessions.IsProvisional("other", got.text) || !sessions.Pending() {
			t.Fatalf("running observation lost its original owner: %s", got.text)
		}
	case <-time.After(2500 * time.Millisecond):
		cancel()
		<-result
		t.Fatal("tool kept blocking after the soft observation window")
	}
	cancel() // Once returned, the invocation may end while the child continues.
	if got, err := sessions.Wait(ctx); !errors.Is(err, context.Canceled) || len(got) != 0 || !sessions.Pending() {
		t.Fatalf("interrupted observation consumed or canceled the running job: %+v, %v", got, err)
	}
	repeat := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, callID)
	if got, err := tl.InvokableRun(repeat, args); err != nil || !strings.Contains(got, "already owns a command") {
		t.Fatalf("repeated invocation was not refused before launch: %s, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitCtx, stopWaiting := context.WithTimeout(t.Context(), time.Second)
	defer stopWaiting()
	got, err := sessions.Wait(waitCtx)
	if err != nil || len(got) != 1 || sessions.Pending() {
		t.Fatalf("missing terminal receipt: %+v, %v", got, err)
	}
	var terminal struct {
		Stdout    string `json:"stdout"`
		ExitCode  *int   `json:"exit_code"`
		Status    string `json:"status"`
		SessionID string `json:"session_id"`
		TimeoutMS int64  `json:"timeout_ms"`
	}
	if err := json.Unmarshal([]byte(got[0].Result), &terminal); err != nil || got[0].CallID != callID ||
		got[0].SessionID != receipt.SessionID || got[0].Arguments != args || terminal.SessionID != receipt.SessionID ||
		terminal.Stdout != "finished" || terminal.ExitCode == nil || *terminal.ExitCode != 0 ||
		terminal.Status != "exited" || terminal.TimeoutMS != 3600000 {
		t.Fatalf("terminal receipt lost original command identity or result: %+v, %v", got[0], err)
	}
	if got, err := tl.InvokableRun(repeat, args); err != nil || !strings.Contains(got, "already owns a command") {
		t.Fatalf("collected call replay was not refused before launch: %s, %v", got, err)
	}
	select {
	case text := <-live:
		if !strings.Contains(text, `"stdout":"finished"`) {
			t.Fatalf("unexpected live background output: %s", text)
		}
	default:
		t.Fatal("background output did not use the original live stream")
	}
	if text, err := os.ReadFile(filepath.Join(dir, "launches")); err != nil || string(text) != "once\n" {
		t.Fatalf("command started more or fewer than once: %q, %v", text, err)
	}
}

func TestManagedWorkspaceExecTakesReadyResultWithoutWaitingForOtherCommands(t *testing.T) {
	dir := t.TempDir()
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	run := func(id string) string {
		t.Helper()
		ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, id)
		args := `{"command":"while [ ! -e release-` + id + ` ]; do sleep 0.02; done; printf ` + id + `"}`
		out, err := tl.InvokableRun(ctx, args)
		if err != nil || !sessions.IsProvisional(id, out) {
			t.Fatalf("command did not yield: %s, %v", out, err)
		}
		return out
	}
	take, ok := any(sessions).(interface {
		TakeReady() ([]ManagedExecReceipt, error)
	})
	if !ok {
		t.Fatal("session owner cannot collect an already-finished command without waiting")
	}
	run("first")
	second := run("second")
	if got, err := take.TakeReady(); err != nil || len(got) != 0 || !sessions.Pending() {
		t.Fatalf("collection waited for or consumed still-running commands: %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release-second"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var secondReceipt []ManagedExecReceipt
	deadline := time.Now().Add(time.Second)
	for len(secondReceipt) == 0 && time.Now().Before(deadline) {
		var err error
		secondReceipt, err = take.TakeReady()
		if err != nil {
			t.Fatal(err)
		}
		if len(secondReceipt) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(secondReceipt) != 1 || secondReceipt[0].CallID != "second" ||
		!strings.Contains(secondReceipt[0].Result, `"stdout":"second"`) || !sessions.Pending() {
		t.Fatalf("ready command was blocked behind earlier running command: %+v", secondReceipt)
	}
	if !sessions.IsProvisional("second", second) {
		t.Fatal("late live notifications can no longer recognize the collected running receipt")
	}
	if err := os.WriteFile(filepath.Join(dir, "release-first"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	got, err := sessions.Wait(ctx)
	if err != nil || len(got) != 1 || got[0].CallID != "first" || sessions.Pending() {
		t.Fatalf("waiting collected the wrong terminal receipts: %+v, %v", got, err)
	}
	if got, err := take.TakeReady(); err != nil || len(got) != 0 {
		t.Fatalf("already-collected receipt was returned again: %+v, %v", got, err)
	}
}

func TestManagedWorkspaceExecStopsRepeatingUnchangedFailure(t *testing.T) {
	for _, recovery := range []struct {
		name string
		args string
	}{
		{name: "cwd", args: `"cwd":"fixed"`},
		{name: "env", args: `"env":{"REPEAT_TEST_INPUT":"fixed"}`},
		{name: "stdin", args: `"stdin":"fixed"`},
		{name: "different command"},
		{name: "intervening execution"},
	} {
		t.Run(recovery.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "fixed"), 0o700); err != nil {
				t.Fatal(err)
			}
			sessions := NewManagedExecSessions(t.Context())
			t.Cleanup(func() { _ = sessions.Close() })
			tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
			call := 0
			run := func(args string) (string, error) {
				call++
				sessions.AdvanceModelRequest()
				ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, "call-"+strconv.Itoa(call))
				return tl.InvokableRun(ctx, args)
			}
			command := `"command":"printf once >> launches; printf 'diagnostic 42' >&2; exit 2"`
			args := "{" + command + "}"
			for i := range 2 {
				out, err := run(args)
				if err != nil || !strings.Contains(out, `"exit_code":2`) || strings.Contains(out, "repeated_exec_failure") {
					t.Fatalf("execution %d was refused or lost its failure: %s, %v", i+1, out, err)
				}
			}
			out, err := run(args)
			var result struct {
				Code     string `json:"code"`
				ExitCode int    `json:"exit_code"`
				Stderr   string `json:"stderr"`
				Error    string `json:"error"`
			}
			if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Code != "repeated_exec_failure" ||
				result.ExitCode != 2 || result.Stderr != "diagnostic 42" || !strings.Contains(strings.ToLower(result.Error), "change") {
				t.Fatalf("repeated invocation did not keep its error and recovery cue: %s, %v", out, err)
			}
			if _, err := run(args); !errors.Is(err, ErrRepeatedManagedExecFailure) || !strings.Contains(err.Error(), "diagnostic 42") {
				t.Fatalf("unchanged retry after refusal did not terminate with the cause: %v", err)
			}
			if launches, err := os.ReadFile(filepath.Join(dir, "launches")); err != nil || string(launches) != "onceonce" {
				t.Fatalf("repeated refusal launched a process: %q, %v", launches, err)
			}
			switch recovery.name {
			case "different command":
				args = `{"command":"printf changed"}`
			case "intervening execution":
				out, err = run(`{"command":"printf repaired"}`)
				if err != nil || !strings.Contains(out, `"stdout":"repaired"`) {
					t.Fatalf("intervening execution was not allowed: %s, %v", out, err)
				}
			default:
				args = "{" + command + "," + recovery.args + "}"
			}
			if out, err := run(args); err != nil || strings.Contains(out, "repeated_exec_failure") || strings.Contains(out, "did not run") {
				t.Fatalf("changed inputs did not permit a new execution: %s, %v", out, err)
			}
		})
	}
}

func TestManagedWorkspaceExecLetsParallelRetriesReceiveRefusalBeforeStopping(t *testing.T) {
	dir := t.TempDir()
	sessions := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = sessions.Close() })
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	run := func(id string) (string, error) {
		ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, id)
		return tl.InvokableRun(ctx, `{"command":"printf x >> launches; printf 'actionable failure' >&2; exit 2"}`)
	}
	for _, id := range []string{"first", "second"} {
		sessions.AdvanceModelRequest()
		if out, err := run(id); err != nil || !strings.Contains(out, `"exit_code":2`) {
			t.Fatalf("command did not finish with the fixture failure: %s, %v", out, err)
		}
	}
	type outcome struct {
		text string
		err  error
	}
	results := make(chan outcome, 2)
	sessions.AdvanceModelRequest()
	for _, id := range []string{"parallel-one", "parallel-two"} {
		go func() {
			text, err := run(id)
			results <- outcome{text, err}
		}()
	}
	for range 2 {
		got := <-results
		if got.err != nil || !strings.Contains(got.text, `"code":"repeated_exec_failure"`) {
			t.Fatalf("model could not receive the correction cue before a terminal failure: %s, %v", got.text, got.err)
		}
	}
	sessions.AdvanceModelRequest()
	if out, err := run("parallel-one"); err != nil || !strings.Contains(out, `"code":"repeated_exec_failure"`) {
		t.Fatalf("replaying a soft refusal became a new retry: %s, %v", out, err)
	}
	if _, err := run("later-retry"); !errors.Is(err, ErrRepeatedManagedExecFailure) {
		t.Fatalf("new request ignored the previous refusal without stopping: %v", err)
	}
	if launches, err := os.ReadFile(filepath.Join(dir, "launches")); err != nil || string(launches) != "xx" {
		t.Fatalf("parallel refusal launched an extra process: %q, %v", launches, err)
	}
}

func TestManagedWorkspaceExecDoesNotGuardUncertainOrChangingFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{name: "different stderr", args: `{"command":"printf x >> marker; wc -c < marker >&2; exit 2"}`, want: `"exit_code":2`},
		{name: "different stdout", args: `{"command":"printf x >> marker; wc -c < marker; printf failed >&2; exit 2"}`, want: `"exit_code":2`},
		{name: "truncated", args: `{"command":"printf x >> marker; printf '%02048d' 0 >&2; exit 2","max_output_kb":1}`, want: `"truncated":true`},
		{name: "hard deadline", args: `{"command":"printf x >> marker; exec sleep 90","timeout_ms":200}`, want: "context deadline exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sessions := NewManagedExecSessions(t.Context())
			t.Cleanup(func() { _ = sessions.Close() })
			tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
			for i := range 3 {
				ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, "call-"+strconv.Itoa(i))
				if out, err := tl.InvokableRun(ctx, tc.args); err != nil || !strings.Contains(out, tc.want) || strings.Contains(out, "repeated_exec_failure") {
					t.Fatalf("execution %d lost its own evidence: %s, %v", i+1, out, err)
				}
			}
			if launches, err := os.ReadFile(filepath.Join(dir, "marker")); err != nil || string(launches) != "xxx" {
				t.Fatalf("uncertain or changing result blocked actual execution: %q, %v", launches, err)
			}
		})
	}
}

func TestManagedWorkspaceExecBoundsPendingJobsAndJoinsOwnerClose(t *testing.T) {
	dir := t.TempDir()
	sessions := NewManagedExecSessions(t.Context())
	defer func() {
		if err := sessions.Close(); err != nil {
			t.Error(err)
		}
	}()
	tl := ManagedExec(workspaceExecForTest(t, dir), sessions).(tool.InvokableTool)
	bind := func(id string) context.Context {
		return BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, id)
	}
	results := make(chan string, maxManagedExecSessions)
	for i := range maxManagedExecSessions {
		go func() {
			out, err := tl.InvokableRun(bind("call-"+strconv.Itoa(i)), `{"command":"exec sleep 90"}`)
			if err != nil {
				out = err.Error()
			}
			results <- out
		}()
	}
	for range maxManagedExecSessions {
		select {
		case out := <-results:
			if !strings.Contains(out, `"status":"running"`) {
				t.Fatalf("unexpected pending receipt: %s", out)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("pending command did not yield")
		}
	}
	out, err := tl.InvokableRun(bind("overflow"), `{"command":"touch forbidden"}`)
	if err != nil || !strings.Contains(out, "pending commands") {
		t.Fatalf("command beyond pending bound was not refused: %s, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(err) {
		t.Fatalf("command beyond pending bound started: %v", err)
	}
	if err := sessions.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.Wait(t.Context())
	if err != nil || len(got) != maxManagedExecSessions || sessions.Pending() {
		t.Fatalf("owner close did not join all commands: %d results, %v", len(got), err)
	}
	for _, receipt := range got {
		if !strings.Contains(receipt.Result, `"status":"cancelled"`) {
			t.Fatalf("owner-close result: %s", receipt.Result)
		}
	}
	out, err = tl.InvokableRun(bind("closed"), `{"command":"touch forbidden"}`)
	if err != nil || !strings.Contains(out, "context canceled") {
		t.Fatalf("closed turn accepted new command: %s, %v", out, err)
	}
}

func TestManagedWorkspaceExecReportsTerminalBeforeOrAfterSoftYield(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
	}{
		{name: "quick", args: `{"command":"printf ready"}`},
		{name: "foreground preempt", args: `{"command":"printf ready; while :; do sleep 0.02; done"}`},
		{name: "background hard deadline", args: `{"command":"printf ready; while :; do sleep 0.02; done","timeout_ms":1400}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := NewManagedExecSessions(t.Context())
			defer func() {
				if err := sessions.Close(); err != nil {
					t.Error(err)
				}
			}()
			tl := ManagedExec(workspaceExecForTest(t, t.TempDir()), sessions).(tool.InvokableTool)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = BindExecOutput(ctx, func(string) {
				if tc.name == "foreground preempt" {
					cancel()
				}
			}, einoexec.ToolName, "call")
			out, err := tl.InvokableRun(ctx, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.name {
			case "quick":
				if !strings.Contains(out, `"exit_code":0`) || strings.Contains(out, `"session_id"`) || sessions.Pending() {
					t.Fatalf("quick exec lost the original terminal shape: %s", out)
				}
			case "foreground preempt":
				if !strings.Contains(out, "context canceled") || strings.Contains(out, `"session_id"`) || sessions.Pending() {
					t.Fatalf("foreground cancellation left an unowned command: %s", out)
				}
			case "background hard deadline":
				if !sessions.IsProvisional("call", out) {
					t.Fatalf("hard deadline became a soft wait: %s", out)
				}
				receipt, err := sessions.Wait(t.Context())
				if err != nil || len(receipt) != 1 || !strings.Contains(receipt[0].Result, `"status":"timed_out"`) {
					t.Fatalf("background lost its hard deadline: %+v, %v", receipt, err)
				}
			}
		})
	}
}

func TestManagedExecTerminalStatusPreservesProvenStallOrUncertainCleanup(t *testing.T) {
	if got := managedExecTerminalStatus(errManagedExecStalled); got != "stalled" {
		t.Fatalf("proven stall status = %q", got)
	}
	if got := managedExecTerminalStatus(errors.Join(errManagedExecStalled, errManagedExecCleanup)); got != "cleanup_failed" {
		t.Fatalf("uncertain cleanup status = %q", got)
	}
}

func TestManagedWorkspaceExecWaitsForOneProcessAndStreamsLiveOutput(t *testing.T) {
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "sub")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tl := ManagedExec(workspaceExecForTest(t, t.TempDir())).(tool.InvokableTool)
	info, err := tl.Info(t.Context())
	if err != nil || info.Name != einoexec.ToolName {
		t.Fatalf("managed exec schema = %+v, %v", info, err)
	}
	cmd := "printf 'run\\n' >> launches; printf ready; while [ ! -f release ]; do sleep 0.02; done; IFS= read -r line; printf ':%s:%s' \"$ZWAI_TEST_EXEC_VAR\" \"$line\"; printf err >&2; exit 7"
	args, err := json.Marshal(map[string]any{
		"command": cmd, "timeout_ms": "600000", "cwd": "sub",
		"base_dir": baseDir, "env": map[string]any{" ZWAI_TEST_EXEC_VAR ": "env", "ignored": 1},
		"stdin": "stdin\n", "max_output_kb": 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	live := make(chan string, 10)
	ctx = BindExecOutput(ctx, func(text string) {
		select {
		case live <- text:
		default:
		}
	}, einoexec.ToolName, "same-call")
	type outcome struct {
		text string
		err  error
	}
	result := make(chan outcome, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		text, err := tl.InvokableRun(ctx, string(args))
		result <- outcome{text, err}
	}()
	t.Cleanup(func() {
		cancel()
		<-joined
	})
	select {
	case got := <-live:
		if !strings.Contains(got, `"stdout":"ready"`) {
			t.Fatalf("live stdout = %s", got)
		}
	case <-ctx.Done():
		t.Fatal("command did not stream live stdout")
	}
	select {
	case got := <-result:
		t.Fatalf("tool returned before its process exited: %+v", got)
	default:
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		var reply struct {
			Stdout       string `json:"stdout"`
			Stderr       string `json:"stderr"`
			FullCommand  string `json:"full_command"`
			ResolvedExec string `json:"resolved_exec"`
			Workdir      string `json:"workdir"`
			ExitCode     int    `json:"exit_code"`
			TimeoutMS    int    `json:"timeout_ms"`
			Failed       bool   `json:"failed"`
		}
		if got.err != nil {
			t.Fatal(got.err)
		}
		if err := json.Unmarshal([]byte(got.text), &reply); err != nil {
			t.Fatalf("invalid result %q: %v", got.text, err)
		}
		if reply.Stdout != "ready:env:stdin" || reply.Stderr != "err" || reply.ExitCode != 7 || !reply.Failed ||
			reply.TimeoutMS != 600_000 || reply.Workdir != dir || reply.ResolvedExec == "" || reply.FullCommand != cmd {
			t.Fatalf("managed receipt lost exec compatibility or effective deadline: %+v", reply)
		}
	case <-ctx.Done():
		t.Fatal("command did not return after release")
	}
	if text, err := os.ReadFile(filepath.Join(dir, "launches")); err != nil || string(text) != "run\n" {
		t.Fatalf("command launched more or fewer than once: %q, %v", text, err)
	}
}

func TestManagedWorkspaceExecInheritsOwnerOrHonorsLongExplicitDeadline(t *testing.T) {
	dir := t.TempDir()
	tl := ManagedExec(workspaceExecForTest(t, dir)).(tool.InvokableTool)
	for _, tc := range []struct {
		name string
		arg  string
		want int64
	}{
		{name: "omitted"},
		{name: "zero", arg: `,"timeout_ms":0`},
		{name: "zero string", arg: `,"timeout_ms":"0"`},
		{name: "long number", arg: `,"timeout_ms":3600000`, want: 3_600_000},
		{name: "long string", arg: `,"timeout_ms":"3600000"`, want: 3_600_000},
		{name: "long exponent", arg: `,"timeout_ms":3.6e6`, want: 3_600_000},
		{name: "maximum representable", arg: `,"timeout_ms":9223372036854`, want: 9_223_372_036_854},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tl.InvokableRun(t.Context(), `{"command":"printf ran"`+tc.arg+`}`)
			var receipt struct {
				Stdout    string `json:"stdout"`
				TimeoutMS *int64 `json:"timeout_ms"`
				Failed    bool   `json:"failed"`
			}
			if err != nil || json.Unmarshal([]byte(got), &receipt) != nil || receipt.TimeoutMS == nil ||
				*receipt.TimeoutMS != tc.want || receipt.Stdout != "ran" || receipt.Failed {
				t.Fatalf("original exec deadline receipt=%s, want timeout_ms=%d and successful launch, error=%v", got, tc.want, err)
			}
		})
	}
}

func TestManagedWorkspaceExecLongCommandObeysEarlierOwnerDeadline(t *testing.T) {
	tl := ManagedExec(workspaceExecForTest(t, t.TempDir())).(tool.InvokableTool)
	for _, tc := range []struct {
		name string
		arg  string
		want int64
	}{
		{name: "inherited"},
		{name: "explicit", arg: `,"timeout_ms":3600000`, want: 3_600_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			got, err := tl.InvokableRun(ctx, `{"command":"printf started; while :; do sleep 0.05; done"`+tc.arg+`}`)
			var receipt struct {
				Stdout    string `json:"stdout"`
				TimeoutMS *int64 `json:"timeout_ms"`
				ExitCode  int    `json:"exit_code"`
				Failed    bool   `json:"failed"`
				Error     string `json:"error"`
			}
			if err != nil || json.Unmarshal([]byte(got), &receipt) != nil || receipt.Stdout != "started" ||
				receipt.TimeoutMS == nil || *receipt.TimeoutMS != tc.want || !receipt.Failed || receipt.ExitCode != -1 ||
				!strings.Contains(receipt.Error, "context deadline exceeded") {
				t.Fatalf("earlier owner deadline did not stop the original long command: %s, error=%v", got, err)
			}
		})
	}
}

func TestManagedWorkspaceExecAdvertisesOnlyItsEffectiveDeadline(t *testing.T) {
	legacy := workspaceExecForTest(t, t.TempDir())
	managed := ManagedExec(legacy)
	for _, tc := range []struct {
		tool tool.BaseTool
		want string
	}{
		{legacy, "10000"},
		{managed, "omit"},
		{legacy, "10000"},
	} {
		info, err := tc.tool.Info(t.Context())
		if err != nil || info.Name != einoexec.ToolName {
			t.Fatalf("exec schema = %+v, %v", info, err)
		}
		args, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := args.Properties.Get("base_dir"); ok {
			t.Fatal("historical base_dir is not part of the advertised schema")
		}
		timeout, ok := args.Properties.Get("timeout_ms")
		if !ok || !strings.Contains(strings.ToLower(timeout.Description), tc.want) {
			t.Fatalf("effective timeout description = %+v, want %s", timeout, tc.want)
		}
	}
	guarded, err := einoexec.New(einoexec.Config{DefaultBaseDir: t.TempDir(), ProtectedDomains: protectedExecDomain{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ManagedExec(guarded); got != guarded {
		t.Fatal("workspace routing replaced a separately configured exec")
	}
}

type protectedExecDomain struct{}

func (protectedExecDomain) Mark(string)          {}
func (protectedExecDomain) Contains(string) bool { return true }

func TestManagedWorkspaceExecReportsTimeoutAndOwnsPreemptCleanup(t *testing.T) {
	dir := t.TempDir()
	tl := ManagedExec(workspaceExecForTest(t, dir)).(tool.InvokableTool)
	t.Run("deadline", func(t *testing.T) {
		got, err := tl.InvokableRun(t.Context(), `{"command":"sleep 0.5","timeout_ms":25}`)
		if err != nil || !strings.Contains(got, "context deadline exceeded") || !strings.Contains(got, `"timeout_ms":25`) {
			t.Fatalf("deadline result=%s error=%v", got, err)
		}
	})
	t.Run("preempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan string, 1)
		go func() {
			result, err := tl.InvokableRun(ctx, `{"command":"sleep 30 & echo $! > child; wait","timeout_ms":600000}`)
			if err != nil {
				done <- err.Error()
				return
			}
			done <- result
		}()
		child := readManagedExecTestChild(t, dir)
		cancel()
		select {
		case got := <-done:
			if !strings.Contains(got, "context canceled") || !strings.Contains(got, `"exit_code":-1`) {
				t.Fatalf("preempt result=%s", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("preempt did not settle command")
		}
		assertManagedExecChildStopped(t, child)
	})
}

func TestManagedWorkspaceExecUsesExactWorkspaceAndCapsResult(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "workspace ")
	subdir := filepath.Join(dir, "sub ")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "workspace", "sub "), 0o700); err != nil {
		t.Fatal(err)
	}
	tl := ManagedExec(workspaceExecForTest(t, dir)).(tool.InvokableTool)
	got, err := tl.InvokableRun(t.Context(), `{"command":"touch launched; printf '%02048d' 0","cwd":"sub ","max_output_kb":1}`)
	var receipt struct {
		Stdout    string `json:"stdout"`
		Workdir   string `json:"workdir"`
		TimeoutMS int    `json:"timeout_ms"`
		Truncated bool   `json:"truncated"`
	}
	if err != nil || json.Unmarshal([]byte(got), &receipt) != nil || len(receipt.Stdout) != 1024 ||
		receipt.Workdir != subdir || receipt.TimeoutMS != 0 || !receipt.Truncated {
		t.Fatalf("bounded result lost exact path or timeout: %+v, error=%v", receipt, err)
	}
	if _, err := os.Stat(filepath.Join(subdir, "launched")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "workspace", "sub ", "launched")); !os.IsNotExist(err) {
		t.Fatalf("trimmed workspace sibling got command: %v", err)
	}
}

func TestManagedWorkspaceExecReportsBadArgumentsWithoutLaunching(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tl := ManagedExec(workspaceExecForTest(t, dir)).(tool.InvokableTool)
	for _, tc := range []struct{ args, want string }{
		{`{`, "failed to parse arguments"},
		{`{}`, "command is required"},
		{`{"command":"touch launched","cwd":"missing"}`, "cwd"},
		{`{"command":"touch launched","base_dir":"missing"}`, "base_dir not accessible"},
		{`{"command":"touch launched","timeout_ms":-1}`, "timeout_ms"},
		{`{"command":"touch launched","timeout_ms":-0.5}`, "timeout_ms"},
		{`{"command":"touch launched","timeout_ms":"-1"}`, "timeout_ms"},
		{`{"command":"touch launched","timeout_ms":9223372036855}`, "timeout_ms"},
		{`{"command":"touch launched","timeout_ms":1e30}`, "timeout_ms"},
		{`{"command":"touch launched","timeout_ms":"99999999999999999999999999"}`, "timeout_ms"},
		{`{"command":"touch launched","base_dir":` + strconv.Quote(file) + `}`, "base_dir is not a directory"},
	} {
		result, err := tl.InvokableRun(t.Context(), tc.args)
		if err != nil || !strings.Contains(result, tc.want) || !strings.Contains(result, `"failed":true`) {
			t.Errorf("bad args %s returned %s, %v", tc.args, result, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "launched")); !os.IsNotExist(err) {
		t.Fatalf("bad arguments launched a command: %v", err)
	}
}
