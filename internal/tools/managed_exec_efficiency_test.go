//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
)

func TestManagedExecYieldWindowIsIndependentFromCommandDeadline(t *testing.T) {
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	exec := ManagedExec(workspaceExecForTest(t, t.TempDir()), owner).(tool.InvokableTool)

	for _, tc := range []struct {
		name, args, output string
		exit               int
	}{
		{"short operation stays foreground", `{"command":"sleep 1.2; printf ready","yield_after_ms":6000}`, "ready", 0},
		{"hard deadline wins", `{"command":"command sleep 10","timeout_ms":150,"yield_after_ms":6000}`, "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, tc.name)
			receipt, err := invokeExecObserver(ctx, exec, tc.args)
			if err != nil || receipt.SessionID != "" || receipt.ExitCode == nil ||
				*receipt.ExitCode != tc.exit || receipt.Stdout != tc.output {
				t.Fatalf("unexpected foreground terminal receipt: %+v, %v", receipt, err)
			}
			if tc.exit != 0 && receipt.TimeoutMS != 150 {
				t.Fatalf("original deadline changed: %+v", receipt)
			}
		})
	}
}

func TestManagedExecPipefailIsExplicit(t *testing.T) {
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	exec := ManagedExec(workspaceExecForTest(t, t.TempDir()), owner).(tool.InvokableTool)
	for _, tc := range []struct {
		name, args string
		exit       int
	}{
		{"default", `{"command":"false | cat"}`, 0},
		{"explicit default", `{"command":"false | cat","pipefail":false}`, 0},
		{"strict", `{"command":"false | cat","pipefail":true}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, tc.name)
			receipt, err := invokeExecObserver(ctx, exec, tc.args)
			if err != nil || receipt.ExitCode == nil || *receipt.ExitCode != tc.exit {
				t.Fatalf("unexpected pipeline exit: %+v, %v", receipt, err)
			}
		})
	}
}

func TestManagedExecObservationCursorPreservesChangesAndTerminal(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	observers := managedExecObserversForTest(t, owner)
	id := launchExecToObserve(t, owner, workspaceExecForTest(t, dir), "cursor-call",
		`printf first; while [ ! -e emit ]; do sleep 0.02; done; printf second; while [ ! -e finish ]; do sleep 0.02; done; exit 7`, 0)
	type observation struct {
		observedExecReceipt
		Revision  uint64  `json:"revision"`
		Original  string  `json:"original_call_id"`
		Unchanged bool    `json:"unchanged"`
		Output    *string `json:"stdout"`
		Errors    *string `json:"stderr"`
	}
	observe := func(name, extra string) observation {
		t.Helper()
		args := fmt.Sprintf(`{"session_id":%q%s}`, id, extra)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		got, err := observers[name].InvokableRun(ctx, args)
		var result observation
		if err != nil || json.Unmarshal([]byte(got), &result) != nil {
			t.Fatalf("observe %s: %s, %v", name, got, err)
		}
		return result
	}
	first := observe("exec_status", "")
	if first.Revision == 0 || first.Original != "cursor-call" || first.Output == nil || *first.Output != "first" {
		t.Fatalf("first full snapshot lacks a cursor or original evidence: %+v", first)
	}
	extra := fmt.Sprintf(`,"after_revision":%d`, first.Revision)
	for _, name := range []string{"exec_status", "wait_exec"} {
		got := observe(name, extra+`,"idle_timeout_ms":60`)
		if got.Status != "running" || got.Revision != first.Revision || !got.Unchanged ||
			got.Output != nil || got.Errors != nil || got.ObservationTimedOut != (name == "wait_exec") {
			t.Fatalf("%s repeated unchanged output or lost status: %+v", name, got)
		}
	}
	writeExecObservationMarker(t, dir, "emit")
	deadline := time.Now().Add(2 * time.Second)
	for {
		next := observe("exec_status", extra)
		if next.Revision > first.Revision && !next.Unchanged && next.Output != nil && *next.Output == "firstsecond" {
			extra = fmt.Sprintf(`,"after_revision":%d`, next.Revision)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new output not visible: %+v", next)
		}
		time.Sleep(10 * time.Millisecond)
	}
	writeExecObservationMarker(t, dir, "finish")
	terminal := observe("wait_exec", extra)
	if terminal.Status != "exited" || terminal.Unchanged || terminal.ExitCode == nil || *terminal.ExitCode != 7 ||
		terminal.Output == nil || *terminal.Output != "firstsecond" {
		t.Fatalf("terminal transition without new output was hidden: %+v", terminal)
	}
	same := observe("exec_status", fmt.Sprintf(`,"after_revision":%d`, terminal.Revision))
	if same.Status != "exited" || !same.Unchanged || same.ExitCode == nil || *same.ExitCode != 7 ||
		same.Output != nil || same.Original != "cursor-call" {
		t.Fatalf("unchanged terminal metadata lost or repeated its stream: %+v", same)
	}
	full := observe("exec_status", "")
	if full.Output == nil || *full.Output != "firstsecond" || full.Unchanged {
		t.Fatalf("explicit full terminal snapshot not preserved: %+v", full)
	}
}

func TestManagedExecRejectsInvalidEfficiencyOptionsBeforeLaunch(t *testing.T) {
	dir := t.TempDir()
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	exec := ManagedExec(workspaceExecForTest(t, dir), owner).(tool.InvokableTool)
	for i, option := range []string{`"yield_after_ms":-1`, `"yield_after_ms":30001`, `"yield_after_ms":1.5`, `"pipefail":"true"`} {
		ctx := BindExecOutput(t.Context(), func(string) {}, einoexec.ToolName, fmt.Sprint(i))
		receipt, err := invokeExecObserver(ctx, exec, `{"command":"touch launched",`+option+`}`)
		if err != nil || receipt.ExitCode == nil || *receipt.ExitCode != -1 {
			t.Fatalf("%s was not rejected: %+v, %v", option, receipt, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "launched")); !os.IsNotExist(err) {
		t.Fatalf("invalid options started the command: %v", err)
	}
}

func TestManagedExecEfficiencyOptionsAreAdvertisedOnlyOnManagedTools(t *testing.T) {
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	legacy := workspaceExecForTest(t, t.TempDir())
	for _, tc := range []struct {
		tool tool.BaseTool
		want map[string]string
	}{
		{ManagedExec(legacy, owner), map[string]string{"yield_after_ms": "integer", "pipefail": "boolean"}},
		{legacy, map[string]string{"yield_after_ms": "", "pipefail": ""}},
	} {
		info, err := tc.tool.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		params, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		for field, kind := range tc.want {
			prop, ok := params.Properties.Get(field)
			if ok != (kind != "") || (ok && prop.Type != kind) {
				t.Fatalf("%s schema = %+v, want type %q", field, prop, kind)
			}
		}
	}
	for _, observer := range owner.ObservationTools() {
		info, err := observer.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		params, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		if prop, ok := params.Properties.Get("after_revision"); !ok || prop.Type != "integer" {
			t.Fatalf("%s did not advertise integer after_revision: %+v", info.Name, prop)
		}
	}
}

func TestManagedExecObservationRejectsInvalidCursor(t *testing.T) {
	owner := NewManagedExecSessions(t.Context())
	t.Cleanup(func() { _ = owner.Close() })
	observers := managedExecObserversForTest(t, owner)
	for _, invalid := range []string{"0", "-1", "1.5"} {
		args := `{"session_id":"exec-unknown","after_revision":` + invalid + `}`
		receipt, err := invokeExecObserver(t.Context(), observers["exec_status"], args)
		if err != nil || receipt.Code != "invalid_exec_observation" || receipt.Error == "" {
			t.Fatalf("invalid cursor %s produced %+v, %v", invalid, receipt, err)
		}
	}
}
