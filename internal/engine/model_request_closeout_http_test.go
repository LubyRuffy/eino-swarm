package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The loopback model follows the closeout guidance only after the provider
// reports a real low budget. The test executes the edit, verification and
// delivery through the ordinary manager tools.
func TestManagerRequestBudgetMakesRoomForVerifiedDelivery(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec requires a supported POSIX process owner")
	}
	const final = "The corrected artifact works at the requested destination."
	const cuePrefix = "Model requests remaining:"
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("managed=%t", enabled), func(t *testing.T) {
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, req scheduleCompletionRequest) {
				w.Header().Set("X-Zwai-Model-Request-Limit", "20")
				w.Header().Set("X-Zwai-Model-Requests-Remaining", strconv.Itoa(5-step))
				var cues []string
				for _, msg := range req.Messages {
					if msg.Role == schema.User && strings.HasPrefix(msg.Content, cuePrefix) {
						cues = append(cues, msg.Content)
					}
				}
				if enabled && step >= 2 {
					if len(cues) != 1 || !strings.HasPrefix(cues[0], fmt.Sprintf("%s %d of 20.", cuePrefix, 6-step)) ||
						!strings.Contains(cues[0], "required verification and delivery") {
						t.Errorf("request %d has no current provider closeout guidance: %q", step, cues)
						writeContextFlowReply(w, req.Stream, map[string]any{"content": "The failure still needs attention."}, "stop")
						return
					}
				} else if len(cues) != 0 {
					t.Errorf("unexpected budget guidance on request %d: %q", step, cues)
				}
				switch step {
				case 1:
					writeManagedExecHTTPCall(w, req, "verify-source", `{"command":"set -o pipefail; grep -x READY source.txt | tail -n 1"}`)
				case 2:
					var result struct {
						ExitCode *int `json:"exit_code"`
					}
					for _, msg := range req.Messages {
						if msg.Role == schema.Tool && msg.ToolCallID == "verify-source" {
							if err := json.Unmarshal([]byte(msg.Content), &result); err != nil {
								t.Error(err)
							}
						}
					}
					if result.ExitCode == nil || *result.ExitCode != 1 {
						t.Errorf("pipeline did not report its first failed step: %+v", result)
					}
					writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
						"index": 0, "id": "correct-source", "type": "function",
						"function": map[string]any{"name": "edit", "arguments": `{"file_path":"source.txt","search_block":"STALE","replace_block":"READY"}`},
					}}}, "tool_calls")
				case 3:
					writeManagedExecHTTPCall(w, req, "deliver-source", `{"command":"grep -x READY source.txt && mkdir -p delivered && cp source.txt delivered/result.txt && cd delivered && grep -x READY result.txt"}`)
				case 4:
					writeContextFlowReply(w, req.Stream, map[string]any{"content": final}, "stop")
				default:
					t.Errorf("unexpected manager request %d", step)
					writeContextFlowReply(w, req.Stream, map[string]any{"content": final}, "stop")
				}
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			if enabled {
				e.cfg.Tools.Enabled = []string{"exec_session"}
			}
			thread, err := e.CreateThread("Delivery fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			ws := e.WorkspaceDir(thread.ID)
			if err := os.MkdirAll(ws, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ws, "source.txt"), []byte("STALE\n"), 0600); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Correct the local artifact, deliver it to the destination, and verify it there.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			out, err := os.ReadFile(filepath.Join(ws, "delivered", "result.txt"))
			if err != nil || string(out) != "READY\n" || got.Status != store.TurnDone || got.Final != final || len(fixture.snapshot()) != 4 {
				t.Fatalf("required delivery missing: result=%q error=%v status=%s final=%q requests=%d", out, err, got.Status, got.Final, len(fixture.snapshot()))
			}
		})
	}
}
