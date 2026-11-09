package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	managedExecHTTPCall  = "scheduled-completion-call"
	parallelExecHTTPCall = "parallel-exec-call"
)

// A slow exec yields the same process to the manager turn. The model can run
// another tool, but waits for terminal evidence before its answer is published.
func TestManagerExecYieldsOneProcessWithoutModelPolling(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = "printf 'once\n' >> starts; printf 'phase one\n'; while [ ! -f release ]; do sleep 0.02; done; printf 'phase two\n'; printf 'warning\n' >&2"
	const final = "Both phases completed."
	const premature = "This command is already complete."
	for _, test := range []struct {
		name    string
		enabled bool
	}{
		{name: "default-off"},
		{name: "opted-in", enabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				assertManagerExecTool(t, request)
				if step == 1 {
					var system string
					for _, message := range request.Messages {
						if message.Role == schema.System {
							system += message.Content
						}
					}
					oldPollCue := strings.Contains(system, "sleep in another before")
					newWaitCue := strings.Contains(system, "wakes you as soon as an owned command settles") &&
						strings.Contains(system, "Use command sleep")
					if oldPollCue == test.enabled || newWaitCue != test.enabled {
						t.Errorf("wrong wait guidance for managed=%t: poll=%t engine=%t", test.enabled, oldPollCue, newWaitCue)
					}
					args, err := json.Marshal(map[string]any{"command": command, "timeout_ms": 600000})
					if err != nil {
						t.Error(err)
					}
					writeScheduleCompletionTool(w, request, "exec", string(args))
					return
				}
				if test.enabled && step == 2 {
					var running struct {
						Status    string `json:"status"`
						SessionID string `json:"session_id"`
						ExitCode  *int   `json:"exit_code"`
					}
					for _, m := range request.Messages {
						if m.Role == schema.Tool && m.ToolCallID == managedExecHTTPCall {
							if err := json.Unmarshal([]byte(m.Content), &running); err != nil {
								t.Error(err)
							}
						}
					}
					if running.Status != "running" || running.SessionID == "" || running.ExitCode != nil {
						t.Errorf("long exec did not yield a live session: %+v", running)
					}
					calls := []any{
						map[string]any{"index": 0, "id": parallelExecHTTPCall, "type": "function",
							"function": map[string]any{"name": "exec", "arguments": `{"command":"printf verified > independent"}`}},
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": calls}, "tool_calls")
					return
				}
				if test.enabled && step == 3 {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": premature}, "stop")
					return
				}
				var result struct {
					Stdout    string `json:"stdout"`
					Stderr    string `json:"stderr"`
					ExitCode  *int   `json:"exit_code"`
					TimeoutMS int    `json:"timeout_ms"`
				}
				found, independent := 0, false
				for _, message := range request.Messages {
					if message.Role == schema.Assistant && strings.Contains(message.Content, premature) {
						t.Error("unverified answer was sent back as a completed assistant message")
					}
					if message.Role != schema.Tool {
						continue
					}
					if message.ToolCallID == parallelExecHTTPCall {
						independent = true
					}
					if message.ToolCallID != managedExecHTTPCall {
						continue
					}
					found++
					if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
						t.Error(err)
					}
				}
				if found != 1 || result.ExitCode == nil || *result.ExitCode != 0 || result.Stdout != "phase one\nphase two\n" || result.Stderr != "warning\n" {
					t.Errorf("original exec result did not contain terminal status and both streams: %+v", result)
				}
				if test.enabled && (result.TimeoutMS != 600000 || !independent) {
					t.Errorf("managed exec effective timeout = %d, parallel result = %t", result.TimeoutMS, independent)
				}
				if !test.enabled && result.TimeoutMS != 0 {
					t.Error("default-off exec added a managed deadline receipt")
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			if test.enabled {
				e.cfg.Tools.Enabled = []string{"exec_session"}
			}
			thread, err := e.CreateThread("Managed exec fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace := e.WorkspaceDir(thread.ID)
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			turn, err := e.StartTurn(thread.ID, "Run and verify the two phases.")
			if err != nil {
				t.Fatal(err)
			}
			live := waitLive(t, sub, swarm.NotifyToolDelta.String(), 10*time.Second)
			if live.TurnID != turn.ID || live.ToolCallID != managedExecHTTPCall || !strings.Contains(live.Text, "phase one") {
				t.Fatalf("first phase did not stream from the original call: %+v", live)
			}
			wantWhileRunning := 1
			if test.enabled {
				wantWhileRunning = 3
				ordinaryEventually(t, func() bool {
					data, err := os.ReadFile(filepath.Join(workspace, "independent"))
					return err == nil && string(data) == "verified" && len(fixture.snapshot()) >= 3
				}, "a new model round could not run independent work while the original exec was still active")
			}
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			for waiting := true; waiting; {
				select {
				case event := <-sub.C:
					if event.Kind == swarm.NotifyAgentMessage.String() && strings.Contains(event.Text, premature) {
						t.Fatalf("premature completion reached the transcript: %+v", event)
					}
					if event.ToolCallID == managedExecHTTPCall {
						if event.Kind == swarm.NotifyToolResult.String() {
							t.Fatalf("live session was presented as a finished tool result: %+v", event)
						}
						if event.Kind == swarm.NotifyToolDelta.String() && !strings.Contains(event.Text, "phase one") {
							t.Fatalf("backgrounding overwrote live process output: %+v", event)
						}
					}
				case <-timer.C:
					waiting = false
				}
			}
			if got := len(fixture.snapshot()); got != wantWhileRunning {
				t.Fatalf("silent process caused %d manager requests, want %d", got, wantWhileRunning)
			}
			if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != wantWhileRunning+1 {
				t.Fatalf("command did not reach one terminal model round: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
			}
			data, err := os.ReadFile(filepath.Join(workspace, "starts"))
			if err != nil || string(data) != "once\n" {
				t.Fatalf("expected a single process launch: %q, %v", data, err)
			}
			if test.enabled {
				results := e.managerToolResults(turn.ID)
				var statuses []string
				for _, result := range results {
					if result.ToolCallID == managedExecHTTPCall {
						var fields struct {
							Status string `json:"status"`
						}
						if err := json.Unmarshal([]byte(result.Text), &fields); err != nil {
							t.Fatal(err)
						}
						statuses = append(statuses, fields.Status)
					}
				}
				if len(statuses) != 1 || statuses[0] == "running" {
					t.Fatalf("expected just one durable terminal receipt for the original exec call, got %q", statuses)
				}
			}
		})
	}
}

// Completion during a model request must reach the next request even while
// the manager continues using independent tools.
func TestManagerBackgroundExecDeliversExitToNextModelRequest(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = `{"command":"echo $$ > leader; printf 'started'; while [ ! -f release ]; do sleep 0.02; done; printf 'completed'; printf 'warning' >&2; exit 7"}`
	const independent = `{"command":"pid=$(cat leader); for attempt in 1 2 3 4 5; do if ! kill -0 \"$pid\" 2>/dev/null; then sleep 0.1; printf 'independent'; exit; fi; sleep 0.05; done; exit 1"}`
	const final = "The original command exited with status seven."
	for _, action := range []string{"finish", "stop", "handoff"} {
		t.Run(action, func(t *testing.T) {
			var workspace string
			receiptDelivered := make(chan struct{})
			allowFinal := make(chan struct{})
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					writeScheduleCompletionTool(w, request, "exec", command)
				case 2:
					assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
					if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
						t.Error(err)
					}
					writeManagedExecHTTPCall(w, request, parallelExecHTTPCall, independent)
				default:
					result := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "exited")
					if result.ExitCode == nil || *result.ExitCode != 7 || result.Stdout != "startedcompleted" || result.Stderr != "warning" {
						t.Errorf("next request did not receive the exact terminal output: %+v", result)
					}
					var foundIndependent bool
					for _, message := range request.Messages {
						if message.Role == schema.Tool && message.ToolCallID == parallelExecHTTPCall {
							foundIndependent = strings.Contains(message.Content, `"stdout":"independent"`)
						}
					}
					if !foundIndependent {
						t.Error("next model request lost independent tool output or did not observe the first process exit")
					}
					if step == 3 {
						close(receiptDelivered)
						select {
						case <-r.Context().Done():
							return
						case <-allowFinal:
						}
						writeManagedExecHTTPCall(w, request, "confirmation-exec-call", `{"command":"printf checked"}`)
						return
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
				}
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			thread, err := e.CreateThread("Background completion delivery fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace = e.WorkspaceDir(thread.ID)
			turn, err := e.StartTurn(thread.ID, "Run the command and verify its exit status.")
			if err != nil {
				t.Fatal(err)
			}
			ordinaryAwait(t, receiptDelivered, "model did not receive the next request after the command exited")
			got, err := e.store.GetTurn(turn.ID)
			if err != nil || got.Status != store.TurnRunning || got.Final != "" {
				t.Errorf("the command result prematurely settled the turn: %+v, %v", got, err)
			}
			assertManagedExecTraceReceipt(t, e, turn.ID)
			wantRequests, wantStatus, wantFinal := 3, store.TurnDone, final
			switch action {
			case "finish":
				close(allowFinal)
				wantRequests++
			case "stop":
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				wantStatus, wantFinal = store.TurnCancelled, ""
			case "handoff":
				e = coldRestartReplayEngine(t, e)
				if restored, err := e.ResumeOrphanedTurns(); err != nil || restored != 1 {
					t.Fatalf("replacement did not resume the original turn: %d, %v", restored, err)
				}
				wantRequests++
			}
			got = waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != wantStatus || got.Final != wantFinal || len(fixture.snapshot()) != wantRequests {
				t.Fatalf("turn after %s: status=%s final=%q requests=%d", action, got.Status, got.Final, len(fixture.snapshot()))
			}
			assertManagedExecTraceReceipt(t, e, turn.ID)
		})
	}
}

func assertManagedExecTraceReceipt(t *testing.T, e *Engine, turnID string) {
	t.Helper()
	var receipts []string
	for _, result := range e.managerToolResults(turnID) {
		if result.ToolCallID == managedExecHTTPCall {
			receipts = append(receipts, result.Text)
		}
	}
	if len(receipts) != 1 || !strings.Contains(receipts[0], `"exit_code":7`) {
		t.Errorf("Trace did not receive one terminal result for the original call: %q", receipts)
	}
}

type managedExecHTTPReceipt struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  *int   `json:"exit_code"`
}

func assertManagedExecHTTPStatus(t *testing.T, request scheduleCompletionRequest, id, status string) managedExecHTTPReceipt {
	t.Helper()
	var result managedExecHTTPReceipt
	var calls, results int
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if call.ID == id && call.Function.Name == "exec" {
				calls++
			}
		}
		if message.Role == schema.Tool && message.ToolCallID == id {
			results++
			if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
				t.Error(err)
			}
		}
	}
	if calls != 1 || results != 1 || result.Status != status || result.SessionID == "" {
		t.Errorf("expected one %s result for call %s: calls=%d results=%d result=%+v", status, id, calls, results, result)
	}
	return result
}

func writeManagedExecHTTPCall(w http.ResponseWriter, request scheduleCompletionRequest, id, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function",
		"function": map[string]any{"name": "exec", "arguments": args},
	}}}, "tool_calls")
}

// If the manager has no independent work, the first exit wakes it even when
// an earlier-launched command is still alive.
func TestManagerWaitingForBackgroundExecWakesOnFirstExit(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const first = `{"command":"while [ ! -f release-first ]; do sleep 0.02; done; printf first"}`
	const second = `{"command":"while [ ! -f release-second ]; do sleep 0.02; done; printf second"}`
	const waiting = "Both processes have already exited."
	const final = "Verified both commands."
	bothRunning := make(chan struct{})
	secondExited := make(chan struct{})
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
			writeContextFlowReply(w, request.Stream, map[string]any{"content": waiting}, "stop")
			close(bothRunning)
		case 4:
			assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "running")
			if result := assertManagedExecHTTPStatus(t, request, parallelExecHTTPCall, "exited"); result.Stdout != "second" {
				t.Errorf("second command output missing: %+v", result)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": waiting}, "stop")
			close(secondExited)
		case 5:
			if result := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, "exited"); result.Stdout != "first" {
				t.Errorf("first command output missing: %+v", result)
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
	thread, err := e.CreateThread("Background exit order fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Verify both command outcomes.")
	if err != nil {
		t.Fatal(err)
	}
	workspace := e.WorkspaceDir(thread.ID)
	ordinaryAwait(t, bothRunning, "manager did not reach the wait while both commands were running")
	if err := os.WriteFile(filepath.Join(workspace, "release-second"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ordinaryAwait(t, secondExited, "the later-launched command exited but the manager is still waiting for the first")
	if err := os.WriteFile(filepath.Join(workspace, "release-first"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 5 {
		t.Fatalf("commands did not settle in exit order: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
	}
	for _, request := range fixture.snapshot() {
		for _, message := range request.Messages {
			if message.Role == schema.Assistant && strings.Contains(message.Content, waiting) {
				t.Error("premature completion was retained in the model transcript")
			}
		}
	}
}

func assertManagerExecTool(t *testing.T, request scheduleCompletionRequest) {
	t.Helper()
	var count int
	for _, spec := range request.Tools {
		switch spec.Function.Name {
		case "exec":
			count++
			for _, arg := range []string{"command", "cwd", "timeout_ms", "env", "stdin", "max_output_kb"} {
				if _, ok := spec.Function.Parameters.Properties[arg]; !ok {
					t.Errorf("exec lost advertised argument %s", arg)
				}
			}
		case "exec_session":
			t.Error("model was offered a separate exec_session tool")
		}
	}
	if count != 1 {
		t.Errorf("got %d original exec tools, want exactly one", count)
	}
}

func TestManagerManagedExecReapsBeforeStopSteerOrHandoff(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = `{"command":"(while [ ! -f release ]; do sleep 0.02; done; printf leaked > leaked) & printf 'child ready'; wait","timeout_ms":600000}`
	const steer = "Check the state of the interrupted command."
	const final = "The original command was interrupted."
	for _, action := range []string{"steer", "stop", "handoff"} {
		t.Run(action, func(t *testing.T) {
			var workspace string
			checkChildReaped := func(t *testing.T) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
					t.Error(err)
					return
				}
				time.Sleep(120 * time.Millisecond)
				if _, err := os.Stat(filepath.Join(workspace, "leaked")); !os.IsNotExist(err) {
					t.Errorf("an owned child kept working after cancellation: %v", err)
				}
			}
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					writeScheduleCompletionTool(w, request, "exec", command)
					return
				}
				switch action {
				case "stop":
					t.Error("Stop caused another manager request")
				case "steer":
					if !hasUserContent(request.Messages, "[steer] "+steer) {
						t.Error("next manager request missed the human steer")
					}
					checkChildReaped(t)
				case "handoff":
					var unknown bool
					for _, message := range request.Messages {
						unknown = unknown || message.Role == schema.Tool && message.ToolCallID == managedExecHTTPCall && message.Content == swarm.HandoffUnknownToolResult
						unknown = unknown || message.Role == schema.User && strings.Contains(message.Content, "This tool has an unknown outcome after restart")
					}
					if !unknown {
						t.Error("recovery did not preserve the original command's unknown outcome")
					}
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			thread, err := e.CreateThread("Managed exec cancellation fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace = e.WorkspaceDir(thread.ID)
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			turn, err := e.StartTurn(thread.ID, "Run the command and check its result.")
			if err != nil {
				t.Fatal(err)
			}
			live := waitLive(t, sub, swarm.NotifyToolDelta.String(), 10*time.Second)
			if live.TurnID != turn.ID || !strings.Contains(live.Text, "child ready") {
				t.Fatalf("command did not start before cancellation: %+v", live)
			}
			switch action {
			case "stop":
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
			case "steer":
				if err := e.Steer(thread.ID, steer); err != nil {
					t.Fatal(err)
				}
				if err := e.Preempt(thread.ID); err != nil {
					t.Fatal(err)
				}
			case "handoff":
				e = coldRestartReplayEngine(t, e)
				checkChildReaped(t)
				if restored, err := e.ResumeOrphanedTurns(); err != nil || restored != 1 {
					t.Fatalf("replacement did not resume the original turn: %d, %v", restored, err)
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if action == "stop" {
				if got.Status != store.TurnCancelled || len(fixture.snapshot()) != 1 {
					t.Fatalf("Stop was not terminal: status=%s requests=%d", got.Status, len(fixture.snapshot()))
				}
				checkChildReaped(t)
				return
			}
			if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 2 {
				t.Fatalf("interruption did not resume the original turn: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
			}
		})
	}
}

func TestManagerBackgroundExecSurvivesSteerAndReapsOnStopOrHandoff(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = `{"command":"(while [ ! -f release ]; do sleep 0.02; done; printf completed > child-done) & printf 'child ready'; wait; printf 'original finished'"}`
	const steer = "Check the state of the original command."
	const final = "Verified the command outcome."
	for _, action := range []string{"steer", "stop", "handoff"} {
		t.Run(action, func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch {
				case step == 1:
					writeScheduleCompletionTool(w, request, "exec", command)
					return
				case step == 2 || action == "steer" && step == 3:
					if action == "steer" && step == 3 && !hasUserContent(request.Messages, "[steer] "+steer) {
						t.Error("steering did not interrupt the engine's background wait")
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The command is finished."}, "stop")
					return
				case action == "steer" && step == 4:
					terminal := false
					for _, m := range request.Messages {
						terminal = terminal || m.Role == schema.Tool && m.ToolCallID == managedExecHTTPCall && strings.Contains(m.Content, "original finished")
					}
					if !terminal {
						t.Error("the original exec did not survive steering")
					}
				case action == "handoff" && step == 3:
					unknown := false
					for _, m := range request.Messages {
						unknown = unknown || m.Role == schema.User && strings.Contains(m.Content, "This tool has an unknown outcome after restart")
						if m.Role == schema.Tool && m.ToolCallID == managedExecHTTPCall {
							unknown = unknown || m.Content == swarm.HandoffUnknownToolResult
							if strings.Contains(m.Content, `"status":"running"`) {
								t.Error("handoff sent the old running process as if it were still owned")
							}
						}
					}
					if !unknown {
						t.Error("handoff did not preserve the yielded command's unknown effects")
					}
				default:
					t.Errorf("unexpected model request %d during %s", step, action)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			thread, err := e.CreateThread("Managed exec background fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace := e.WorkspaceDir(thread.ID)
			turn, err := e.StartTurn(thread.ID, "Run the command and check its result.")
			if err != nil {
				t.Fatal(err)
			}
			ordinaryEventually(t, func() bool { return len(fixture.snapshot()) >= 2 }, "original exec did not yield")
			switch action {
			case "stop":
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
			case "steer":
				if err := e.Steer(thread.ID, steer); err != nil {
					t.Fatal(err)
				}
				if err := e.Preempt(thread.ID); err != nil {
					t.Fatal(err)
				}
				ordinaryEventually(t, func() bool { return len(fixture.snapshot()) >= 3 }, "preempt did not wake the manager from background waiting")
				if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "handoff":
				e = coldRestartReplayEngine(t, e)
				if restored, err := e.ResumeOrphanedTurns(); err != nil || restored != 1 {
					t.Fatalf("replacement did not resume the original turn: %d, %v", restored, err)
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if action == "stop" {
				if got.Status != store.TurnCancelled || len(fixture.snapshot()) != 2 {
					t.Fatalf("Stop did not end the turn: status=%s requests=%d", got.Status, len(fixture.snapshot()))
				}
			} else {
				wantRequests := 3
				if action == "steer" {
					wantRequests = 4
				}
				if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != wantRequests {
					t.Fatalf("background exec did not settle correctly: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
				}
			}
			if action != "steer" {
				if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				time.Sleep(120 * time.Millisecond)
				if _, err := os.Stat(filepath.Join(workspace, "child-done")); !os.IsNotExist(err) {
					t.Fatalf("background child kept working after %s: %v", action, err)
				}
			}
		})
	}
}

func TestManagerBackgroundExecReceiptKeepsTheModelAllowance(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = `{"command":"printf started; while [ ! -f release ]; do sleep 0.02; done; printf completed"}`
	const final = "Verified after continuing."
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprintf("limit-%d", limit), func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					writeScheduleCompletionTool(w, request, "exec", command)
					return
				}
				if step == 2 && limit == 2 {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The command has already finished."}, "stop")
					return
				}
				terminal := false
				for _, m := range request.Messages {
					terminal = terminal || m.Role == schema.Tool && m.ToolCallID == managedExecHTTPCall && strings.Contains(m.Content, `"stdout":"startedcompleted"`)
				}
				if !terminal {
					t.Error("continuation did not receive the original terminal tool result")
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			e.cfg.Swarm.ManagerMaxIterations = limit
			thread, err := e.CreateThread("Background allowance fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			turn, err := e.StartTurn(thread.ID, "Run the command and check the result.")
			if err != nil {
				t.Fatal(err)
			}
			waitLive(t, sub, swarm.NotifyToolDelta.String(), 5*time.Second)
			rt := e.runtimeFor(thread.ID)
			ordinaryEventually(t, func() bool {
				rt.mu.Lock()
				reg := rt.reg
				rt.mu.Unlock()
				if reg == nil {
					return false
				}
				for _, m := range reg.ManagerHistory() {
					if m != nil && m.Role == schema.Tool && m.ToolCallID == managedExecHTTPCall && strings.Contains(m.Content, `"status":"running"`) {
						return true
					}
				}
				return false
			}, "the original command did not yield before its deadline")
			if limit == 2 {
				ordinaryEventually(t, func() bool { return len(fixture.snapshot()) == limit }, "the provisional model response did not arrive")
			}
			if err := os.WriteFile(filepath.Join(e.WorkspaceDir(thread.ID), "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			waitLive(t, sub, KindMaxIterations, 5*time.Second)
			if requests := len(fixture.snapshot()); requests != limit {
				t.Fatalf("terminal receipt refreshed the allowance: requests=%d want=%d", requests, limit)
			}
			if err := e.ContinueTurn(thread.ID, true); err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != limit+1 {
				t.Fatalf("the admitted continuation did not settle: status=%s final=%q requests=%d", got.Status, got.Final, len(fixture.snapshot()))
			}
		})
	}
}

func TestBackgroundExecRestoresAuthenticToolPairAfterCompaction(t *testing.T) {
	const id = "test-exec"
	const args = `{"command":"printf completed"}`
	const result = `{"status":"exited","stdout":"completed"}`
	user := schema.UserMessage("Continue the original request.")
	input := []adk.Message{user, schema.ToolMessage(`{"status":"running"}`, id)}
	got := spliceManagedExecReceipts(input, []tools.ManagedExecReceipt{{CallID: id, Arguments: args, Result: result}})
	if len(got) != 3 || got[0] != user || got[1].Role != schema.Assistant || len(got[1].ToolCalls) != 1 ||
		got[1].ToolCalls[0].ID != id || got[1].ToolCalls[0].Function.Name != "exec" || got[1].ToolCalls[0].Function.Arguments != args ||
		got[2].Role != schema.Tool || got[2].ToolCallID != id || got[2].Content != result {
		t.Fatalf("original tool evidence did not survive compaction: %+v", got)
	}
	if unchanged := spliceManagedExecReceipts(got, nil); len(unchanged) != len(got) || unchanged[2] != got[2] {
		t.Fatal("a wait without terminal evidence modified the transcript")
	}
}

func TestRestoredExecSessionUsesActualEffectsInsteadOfAnOldHandle(t *testing.T) {
	running := store.Event{Kind: swarm.NotifyToolResult.String(), ToolCallID: "start", Text: `{"session_id":"old","status":"running"}`}
	terminal := []store.Event{
		{Kind: swarm.NotifyToolCall.String(), ToolCallID: "poll", Text: `exec_session({"session_id":"old"})`},
		{Kind: swarm.NotifyToolResult.String(), ToolCallID: "poll", Text: `{"session_id":"old","status":"exited","exit_code":0}`},
	}
	for _, tc := range []struct {
		name    string
		results []store.Event
		wantCue bool
	}{
		{name: "running", results: []store.Event{running}, wantCue: true},
		{name: "terminal", results: append([]store.Event{running}, terminal...)},
		{name: "terminal-before-stale-running", results: append(append([]store.Event(nil), terminal...), running)},
		{name: "no-first-result", wantCue: true},
		{name: "second-restart-after-synthetic-result", results: []store.Event{
			{Kind: swarm.NotifyToolResult.String(), ToolCallID: "start", Text: resumeToolStopped, Err: resumeToolStopped},
		}, wantCue: true},
		{name: "rejected-start", results: []store.Event{
			{Kind: swarm.NotifyToolResult.String(), ToolCallID: "start", Text: `{"status":"error","failed":true,"error":"command did not start"}`},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Workspace verification is needed."}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			thread, err := e.CreateThread("Managed exec recovery fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, thread.ID, "Check the original job.")
			events := append([]store.Event{
				{Kind: swarm.NotifyToolCall.String(), ToolCallID: "start", Text: `exec_session({"command":"inspect"})`},
			}, tc.results...)
			for _, event := range events {
				event.ThreadID, event.TurnID, event.AgentID = thread.ID, turn.ID, swarm.DefaultManagerID
				if err := e.store.AppendEvent(&event); err != nil {
					t.Fatal(err)
				}
			}
			if resumed, err := e.ResumeOrphanedTurns(); err != nil || resumed != 1 {
				t.Fatalf("resume count=%d err=%v", resumed, err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("restored turn did not finish: %+v", got)
			}
			waitSettled(t, e, thread.ID)
			requests := fixture.snapshot()
			if len(requests) != 1 {
				t.Fatalf("restored manager sent %d requests", len(requests))
			}
			for _, spec := range requests[0].Tools {
				if spec.Function.Name == "exec_session" {
					t.Fatal("an old turn was allowed to start a new managed session")
				}
			}
			hasCue := false
			for _, msg := range requests[0].Messages {
				hasCue = hasCue || msg.Role == schema.User && msg.Content == managedExecResumeCue
			}
			if hasCue != tc.wantCue {
				t.Fatalf("recovery cue did not reflect the recorded command outcome: cue=%t want=%t", hasCue, tc.wantCue)
			}
		})
	}
}
