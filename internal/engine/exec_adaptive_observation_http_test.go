package engine

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The manager's next actual HTTP request can carry the original terminal
// receipt after it showed that a short, isolated command needs its result.
func TestManagerAdaptiveExecObservationAvoidsAnotherWaitRequest(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const first = "adaptive-first"
	const second = "adaptive-second"
	const final = "Both commands were checked."
	const steer = "Keep using the original process."
	for _, name := range []string{"default", "adaptive", "explicit-fast", "parallel", "steer"} {
		t.Run(name, func(t *testing.T) {
			var workspace string
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					writeManagedExecHTTPCall(w, request, first, `{"command":"printf x >> launches; while [ ! -f release ]; do sleep 0.02; done; printf learned"}`)
					return
				case 2:
					original := assertManagedExecHTTPStatus(t, request, first, "running")
					if err := os.WriteFile(filepath.Join(workspace, "release"), nil, 0600); err != nil {
						t.Error(err)
					}
					writeAdaptiveWaitHTTP(t, w, request, "adaptive-witness", original.SessionID)
					return
				case 3:
					original := assertManagedExecHTTPStatus(t, request, first, "exited")
					if original.Stdout != "learned" {
						t.Errorf("learning command did not produce its receipt: %+v", original)
					}
					const secondCommand = `{"command":"printf x >> launches; sleep 2; printf verified"}`
					switch name {
					case "explicit-fast":
						writeManagedExecHTTPCall(w, request, second, `{"command":"printf x >> launches; sleep 2; printf verified","yield_after_ms":0}`)
					case "steer":
						writeManagedExecHTTPCall(w, request, second, `{"command":"printf x >> launches; while [ ! -f release-second ]; do sleep 0.02; done; printf verified"}`)
					case "parallel":
						writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{
							map[string]any{"index": 0, "id": second, "type": "function",
								"function": map[string]any{"name": "exec", "arguments": secondCommand}},
							map[string]any{"index": 1, "id": "adaptive-peer", "type": "function",
								"function": map[string]any{"name": "exec", "arguments": `{"command":"printf independent"}`}},
						}}, "tool_calls")
					default:
						writeManagedExecHTTPCall(w, request, second, secondCommand)
					}
					return
				case 4:
					if name != "adaptive" {
						if name == "parallel" {
							found := false
							for _, message := range request.Messages {
								if message.ToolCallID == "adaptive-peer" {
									var peer managedExecHTTPReceipt
									found = json.Unmarshal([]byte(message.Content), &peer) == nil &&
										peer.Stdout == "independent" && peer.ExitCode != nil && *peer.ExitCode == 0
								}
							}
							if !found {
								t.Error("the independent command did not complete in the same tool batch")
							}
						}
						original := assertManagedExecHTTPStatus(t, request, second, "running")
						if name == "steer" {
							found := false
							for _, message := range request.Messages {
								found = found || message.Content == "[steer] "+steer
							}
							if !found {
								t.Error("the next provider request omitted the steering message")
							}
							if err := os.WriteFile(filepath.Join(workspace, "release-second"), nil, 0600); err != nil {
								t.Error(err)
							}
						}
						writeAdaptiveWaitHTTP(t, w, request, "adaptive-fallback", original.SessionID)
						return
					}
					assertAdaptiveExecTerminal(t, request, second)
				case 5:
					if name == "adaptive" {
						t.Errorf("adaptive observation consumed an extra model request")
					}
					assertAdaptiveExecTerminal(t, request, second)
				default:
					t.Errorf("unexpected model request %d", step)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			if name != "default" {
				e.cfg.Tools.Enabled = append(e.cfg.Tools.Enabled, "exec_adaptive_observation")
			}
			thread, err := e.CreateThread("Adaptive command observation", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace = e.WorkspaceDir(thread.ID)
			turn, err := e.StartTurn(thread.ID, "Run two operations and verify both results.")
			if err != nil {
				t.Fatal(err)
			}
			if name == "steer" {
				ordinaryEventually(t, func() bool {
					data, err := os.ReadFile(filepath.Join(workspace, "launches"))
					return err == nil && string(data) == "xx"
				}, "the second command did not launch")
				time.Sleep(1400 * time.Millisecond)
				if got := len(fixture.snapshot()); got != 3 {
					t.Errorf("provider had %d requests before steer, want 3 while observing", got)
				}
				if err := e.Steer(thread.ID, steer); err != nil {
					t.Fatal(err)
				}
				if err := e.Preempt(thread.ID); err != nil {
					t.Fatal(err)
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			wantRequests := 4
			if name != "adaptive" {
				wantRequests = 5
			}
			if got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != wantRequests {
				t.Fatalf("turn status=%s final=%q requests=%d, want %d", got.Status, got.Final, len(fixture.snapshot()), wantRequests)
			}
			if data, err := os.ReadFile(filepath.Join(workspace, "launches")); err != nil || string(data) != "xx" {
				t.Fatalf("commands did not each launch exactly once: %q, %v", data, err)
			}
		})
	}
}

func assertAdaptiveExecTerminal(t *testing.T, request scheduleCompletionRequest, id string) {
	t.Helper()
	receipt := assertManagedExecHTTPStatus(t, request, id, "exited")
	if receipt.ExitCode == nil || *receipt.ExitCode != 0 || receipt.Stdout != "verified" {
		t.Errorf("next request is missing original command evidence: %+v", receipt)
	}
}

func writeAdaptiveWaitHTTP(t *testing.T, w http.ResponseWriter, request scheduleCompletionRequest, id, session string) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"session_id": session})
	if err != nil {
		t.Error(err)
	}
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function",
		"function": map[string]any{"name": "wait_exec", "arguments": string(args)},
	}}}, "tool_calls")
}
