package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A model that guesses a working directory must be told the directory is
// missing and be allowed to correct itself. The exec guard once returned a Go
// error, and nothing between a tool and the model turns an error into a
// result: a scheduled check on a real model ended with "failed to stream tool
// call" and no answer. This runs the real tool graph, not the guard alone.
func TestMissingExecWorkDirReachesTheModelAsFeedbackNotATurnError(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	missing := filepath.Join(t.TempDir(), "never-created")
	const callID = "exec-missing-cwd"
	var managers atomic.Int32
	var seen atomic.Value
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []*schema.Message `json:"messages"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		if len(input.Messages) > 0 && input.Messages[0].Role == schema.System && input.Messages[0].Content == scheduleAnswerPrompt {
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		switch managers.Add(1) {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "touch " + marker, "cwd": missing})
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function",
				"function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		default:
			for _, m := range input.Messages {
				if m.Role == schema.Tool && m.ToolCallID == callID {
					seen.Store(m.Content)
				}
			}
			writeContextFlowReply(w, true, map[string]any{"content": "Checked."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)

	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-missing-cwd"
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("missing cwd", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Run the check.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != "Checked." {
		t.Fatalf("a missing cwd ended the turn: status=%s error=%q final=%q", got.Status, got.Error, got.Final)
	}
	result, _ := seen.Load().(string)
	for _, want := range []string{missing, "does not exist", "did not run", "Omit cwd"} {
		if !strings.Contains(result, want) {
			t.Fatalf("the model was not told %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "fork/exec") {
		t.Fatalf("the refusal blamed the shell instead of the directory:\n%s", result)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran although its directory is missing")
	}
}
