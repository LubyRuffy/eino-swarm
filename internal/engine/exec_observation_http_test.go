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
	"github.com/cloudwego/eino/schema"
)

// The tools must reach the real manager adapter without changing the default
// toolset. Reading or waiting on a session leaves its original receipt intact.
func TestManagerExecObservationUsesSameSessionOverHTTP(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const command = `{"command":"printf x >> starts; printf 'phase one'; printf 'initial diagnostic' >&2; while [ ! -f release ]; do sleep 0.02; done; printf ' phase two'; printf ' and final' >&2; exit 7"}`
	const statusCall = "exec-observe-live"
	const shortWait = "exec-observe-idle"
	const finishWait = "exec-observe-finish"
	const finishedStatus = "exec-observe-completed"
	const final = "The command completed with the recorded exit status."
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("opted-in=%t", enabled), func(t *testing.T) {
			schemaSeen := make(chan bool, 1)
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					names := make(map[string]int)
					for _, spec := range request.Tools {
						names[spec.Function.Name]++
						if spec.Function.Name == "exec_status" || spec.Function.Name == "wait_exec" {
							if _, ok := spec.Function.Parameters.Properties["session_id"]; !ok {
								t.Errorf("%s omitted session_id", spec.Function.Name)
							}
						}
						if spec.Function.Name == "wait_exec" {
							if _, ok := spec.Function.Parameters.Properties["idle_timeout_ms"]; !ok {
								t.Error("wait_exec omitted idle_timeout_ms")
							}
						}
					}
					wantCount := 0
					if enabled {
						wantCount = 1
					}
					var managesObservation bool
					for _, message := range request.Messages {
						if message.Role == schema.System &&
							strings.Contains(message.Content, "This manager also has exec_status and wait_exec") &&
							strings.Contains(message.Content, "call wait_exec with that session ID") {
							managesObservation = true
						}
					}
					valid := names["exec_status"] == wantCount && names["wait_exec"] == wantCount &&
						managesObservation == enabled
					schemaSeen <- valid
					if !valid || !enabled {
						writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
						return
					}
					writeScheduleCompletionTool(w, request, "exec", command)
					return
				}
				status := "running"
				if step >= 5 {
					status = "exited"
				}
				original := assertManagedExecHTTPStatus(t, request, managedExecHTTPCall, status)
				args := map[string]any{"session_id": original.SessionID}
				var id, name string
				switch step {
				case 2:
					id, name = statusCall, "exec_status"
				case 3:
					got := execObserverHTTPResult(t, request, statusCall)
					if got.SessionID != original.SessionID || got.Status != "running" ||
						got.Stdout != "phase one" || got.Stderr != "initial diagnostic" || got.ExitCode != nil {
						t.Errorf("running status lost progress or session identity: %+v", got)
					}
					id, name = shortWait, "wait_exec"
					args["idle_timeout_ms"] = 70
				case 4:
					got := execObserverHTTPResult(t, request, shortWait)
					if got.SessionID != original.SessionID || got.Status != "running" ||
						!got.ObservationTimedOut || got.ExitCode != nil || got.Stdout != "" || got.Stderr != "" ||
						got.OutputFromToolCallID != statusCall {
						t.Errorf("idle observation did not reference the same running output: %+v", got)
					}
					id, name = finishWait, "wait_exec"
					args["idle_timeout_ms"] = 4000
				case 5, 6:
					witness := finishWait
					if step == 6 {
						witness = finishedStatus
					}
					got := execObserverHTTPResult(t, request, witness)
					if got.SessionID != original.SessionID || got.Status != "exited" || got.ObservationTimedOut ||
						got.ExitCode == nil || *got.ExitCode != 7 || original.ExitCode == nil || *original.ExitCode != 7 ||
						got.Stdout != "" || got.Stderr != "" || got.OutputFromToolCallID != managedExecHTTPCall ||
						original.Stdout != "phase one phase two" || original.Stderr != "initial diagnostic and final" {
						t.Errorf("model input did not keep exactly one terminal output body: observed=%+v original=%+v", got, original)
					}
					if step == 6 {
						writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
						return
					}
					id, name = finishedStatus, "exec_status"
				default:
					t.Errorf("unexpected extra model request %d", step)
					writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
					return
				}
				encoded, err := json.Marshal(args)
				if err != nil {
					t.Errorf("marshal observation arguments: %v", err)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": id, "type": "function",
					"function": map[string]any{"name": name, "arguments": string(encoded)},
				}}}, "tool_calls")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			if enabled {
				e.cfg.Tools.Enabled = []string{"exec_session"}
			}
			thread, err := e.CreateThread("Exec observation", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Observe the process and verify its terminal result.")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case valid := <-schemaSeen:
				if !valid {
					t.Fatalf("exec observers availability differs from opt-in %t", enabled)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("manager did not receive the initial tool schema")
			}
			wantRequests := 1
			if enabled {
				wantRequests = 6
				ordinaryEventually(t, func() bool {
					events, err := e.store.ListTurnEvents(turn.ID)
					if err != nil {
						return false
					}
					for _, event := range events {
						if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == finishWait {
							return true
						}
					}
					return false
				}, "manager did not wait on the same session after an idle observation")
				workspace := e.WorkspaceDir(thread.ID)
				if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(filepath.Join(workspace, "starts")); err != nil || string(data) != "x" {
					t.Fatalf("status or waits did not preserve the one original process: %q, %v", data, err)
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != wantRequests {
				t.Fatalf("turn status=%s final=%q requests=%d, want %d", got.Status, got.Final, len(fixture.snapshot()), wantRequests)
			}
			if enabled {
				assertManagedExecTraceReceipt(t, e, turn.ID)
				assertExecObserverTrace(t, e, turn.ID, map[string]string{
					statusCall: "exec_status", shortWait: "wait_exec",
					finishWait: "wait_exec", finishedStatus: "exec_status",
				}, shortWait, finishWait, finishedStatus)
			}
		})
	}
}

type execObserverReceipt struct {
	managedExecHTTPReceipt
	ObservationTimedOut  bool   `json:"observation_timed_out"`
	OutputFromToolCallID string `json:"output_from_tool_call_id"`
}

func execObserverHTTPResult(t *testing.T, request scheduleCompletionRequest, id string) execObserverReceipt {
	t.Helper()
	var result execObserverReceipt
	var count int
	for _, message := range request.Messages {
		if message.Role != schema.Tool || message.ToolCallID != id {
			continue
		}
		count++
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			t.Error(err)
		}
	}
	if count != 1 {
		t.Errorf("expected one tool result for %s, got %d", id, count)
	}
	return result
}

func assertExecObserverTrace(t *testing.T, e *Engine, turnID string, expected map[string]string, idleID string, terminalIDs ...string) {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	calls, results := make(map[string]int), make(map[string]int)
	for _, event := range events {
		name, ok := expected[event.ToolCallID]
		if !ok {
			continue
		}
		switch event.Kind {
		case swarm.NotifyToolCall.String():
			actual, _ := splitToolCallText(event.Text)
			if actual != name {
				t.Errorf("trace call %s named %s, want %s", event.ToolCallID, actual, name)
			}
			calls[event.ToolCallID]++
		case swarm.NotifyToolResult.String():
			results[event.ToolCallID]++
			if event.ToolCallID == idleID {
				var receipt execObserverReceipt
				if err := json.Unmarshal([]byte(event.Text), &receipt); err != nil || !receipt.ObservationTimedOut ||
					receipt.Stdout != "phase one" || receipt.Stderr != "initial diagnostic" ||
					receipt.OutputFromToolCallID != "" {
					t.Errorf("stored observer lost independent running evidence: %+v, %v", receipt, err)
				}
			}
			for _, terminalID := range terminalIDs {
				if event.ToolCallID != terminalID {
					continue
				}
				var receipt execObserverReceipt
				if err := json.Unmarshal([]byte(event.Text), &receipt); err != nil || receipt.ExitCode == nil ||
					*receipt.ExitCode != 7 || receipt.Stdout != "phase one phase two" ||
					receipt.Stderr != "initial diagnostic and final" || receipt.OutputFromToolCallID != "" {
					t.Errorf("stored observer lost independent terminal evidence: %+v, %v", receipt, err)
				}
			}
		}
	}
	for id := range expected {
		if calls[id] != 1 || results[id] != 1 {
			t.Errorf("trace %s has %d calls and %d results", id, calls[id], results[id])
		}
	}
}

// The projection is rebuilt from the wire input every time. After history
// folding, an observer can still carry the only copy of command output.
func TestManagedExecObservationProjectionKeepsIndependentEvidence(t *testing.T) {
	call := func(id, name string) *schema.Message {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: "{}"},
		}})
	}
	const original = `{"session_id":"exec-owned","status":"exited","exit_code":7,"stdout":"first","stderr":"details"}`
	const richer = `{"session_id":"exec-owned","status":"exited","exit_code":7,"stdout":"last","stderr":"details","truncated":true}`
	const mismatch = `{"session_id":"exec-owned","status":"timed_out","exit_code":-1,"stdout":"first","stderr":"details"}`
	const running = `{"session_id":"exec-owned","status":"running","revision":3,"stdout":"working","stderr":""}`
	const next = `{"session_id":"exec-owned","status":"running","revision":4,"stdout":"working","stderr":""}`
	for _, tc := range []struct {
		name, sourceTool, source, observation, reference string
	}{
		{name: "duplicate terminal", sourceTool: "exec", source: original, observation: original, reference: "source"},
		{name: "original folded away", observation: original},
		{name: "different terminal outcome", sourceTool: "exec", source: original, observation: mismatch},
		{name: "conflicting original call", sourceTool: "exec", source: original,
			observation: strings.TrimSuffix(original, "}") + `,"original_call_id":"other-call"}`},
		{name: "newer bytes than original", sourceTool: "exec", source: original, observation: richer},
		{name: "untrusted same-shaped tool", sourceTool: "other", source: original, observation: original},
		{name: "duplicate running revision", sourceTool: "wait_exec", source: running, observation: running, reference: "source"},
		{name: "new running revision", sourceTool: "wait_exec", source: running, observation: next},
		{name: "malformed observer", sourceTool: "exec", source: original, observation: "not JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []*schema.Message{nil, schema.SystemMessage("Instruction")}
			if tc.sourceTool != "" {
				input = append(input, call("source", tc.sourceTool), schema.ToolMessage(tc.source, "source"))
			}
			observation := schema.ToolMessage(tc.observation, "observation")
			input = append(input, call("observation", "exec_status"), observation)
			projected := referenceManagedExecOutput(input)
			if len(projected) != len(input) || input[len(input)-1] != observation || observation.Content != tc.observation {
				t.Fatalf("projection mutated canonical input: %+v", input)
			}
			for i, message := range projected[:len(projected)-1] {
				if input[i] != message {
					t.Errorf("projection changed unrelated message %d", i)
				}
			}
			wire := projected[len(projected)-1]
			if tc.reference == "" {
				if wire != observation {
					t.Errorf("projection lost independent evidence: %s", wire.Content)
				}
				return
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal([]byte(wire.Content), &got); err != nil {
				t.Fatal(err)
			}
			if wire == observation || wire.ToolCallID != observation.ToolCallID || got["stdout"] != nil || got["stderr"] != nil ||
				string(got["output_from_tool_call_id"]) != `"`+tc.reference+`"` {
				t.Errorf("projection missed a verified duplicate: %s", wire.Content)
			}
		})
	}
}
