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

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The model must get the original failure and one chance to change its
// approach. A stubborn loop may not keep starting shells or spending requests.
func TestManagerManagedExecStopsIdenticalFailureLoop(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires supported process ownership")
	}
	const command = `printf x >> "$LAUNCH_LOG"; ls package_marker`
	const final = "The corrected directory contains the package marker."
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprintf("changes_directory=%t", recover), func(t *testing.T) {
			var launchLog string
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step > 1 {
					var result struct {
						Code     string `json:"code"`
						ExitCode *int   `json:"exit_code"`
						Stderr   string `json:"stderr"`
						Stdout   string `json:"stdout"`
					}
					found := false
					for _, m := range request.Messages {
						if m.Role != schema.Tool || m.ToolCallID != fmt.Sprintf("failed-exec-%d", step-1) {
							continue
						}
						found = true
						if err := json.Unmarshal([]byte(m.Content), &result); err != nil {
							t.Errorf("invalid tool result at request %d: %v", step, err)
						}
					}
					if !found || result.ExitCode == nil {
						t.Errorf("request %d missed the original exec receipt", step)
					} else if step <= 4 && (*result.ExitCode == 0 || !strings.Contains(result.Stderr, "package_marker")) {
						t.Errorf("request %d lost the real path error: %+v", step, result)
					}
					if step == 4 && result.Code != "repeated_exec_failure" {
						t.Errorf("third call did not return a retry refusal: %+v", result)
					}
					if step == 5 && recover && (result.ExitCode == nil || *result.ExitCode != 0 || !strings.Contains(result.Stdout, "package_marker")) {
						t.Errorf("corrected cwd did not execute successfully: %+v", result)
					}
				}
				if step >= 5 {
					if !recover {
						t.Errorf("stubborn command caused another model request: %d", step)
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
					return
				}
				args := map[string]any{"command": command, "env": map[string]string{"LAUNCH_LOG": launchLog}}
				if recover && step == 4 {
					args["cwd"] = "correct"
				}
				encoded, err := json.Marshal(args)
				if err != nil {
					t.Error(err)
					return
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": fmt.Sprintf("failed-exec-%d", step), "type": "function",
					"function": map[string]any{"name": "exec", "arguments": string(encoded)},
				}}}, "tool_calls")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Tools.Enabled = []string{"exec_session"}
			thread, err := e.CreateThread("Repeated exec fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			workspace := e.WorkspaceDir(thread.ID)
			launchLog = filepath.Join(workspace, "launches")
			correct := filepath.Join(workspace, "correct")
			if err := os.MkdirAll(correct, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(correct, "package_marker"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Locate the package marker.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			marker, err := os.ReadFile(launchLog)
			if err != nil {
				t.Fatal(err)
			}
			requests := len(fixture.snapshot())
			if recover {
				if got.Status != store.TurnDone || got.Final != final || requests != 5 || string(marker) != "xxx" {
					t.Fatalf("corrected command did not settle the turn: %+v requests=%d launches=%q", got, requests, marker)
				}
			} else if got.Status != store.TurnError || !strings.Contains(strings.ToLower(got.Error), "repeat") || requests != 4 || string(marker) != "xx" {
				t.Fatalf("repeating commands kept executing or polling: %+v requests=%d launches=%q", got, requests, marker)
			}
		})
	}
}
