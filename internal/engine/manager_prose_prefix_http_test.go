package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestManagerCommentaryCannotPublishASchedulingClaimSplitAcrossToolRounds(t *testing.T) {
	const proposal = "The plan would be a recurring 90-second check on this conversation."
	const assertion = "I've now enabled that."
	toolIDs := []string{"read-before-assertion", "read-after-assertion"}
	workspace := t.TempDir()
	for _, id := range toolIDs {
		if err := os.WriteFile(filepath.Join(workspace, id), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var managerCalls atomic.Int32
	var classifiedPrefix atomic.Bool
	nextManager := make(chan scheduleCompletionRequest, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode fixture request: %v", err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(input.Messages) == 2 && input.Messages[0].Content == scheduleAnswerPrompt {
			var candidate struct {
				Messages []string `json:"messages"`
			}
			if err := json.Unmarshal([]byte(input.Messages[1].Content), &candidate); err != nil {
				t.Errorf("decode policy candidate: %v", err)
				http.Error(w, "invalid policy candidate", http.StatusBadRequest)
				return
			}
			result := `{"claims":[]}`
			if slices.Equal(candidate.Messages, []string{proposal, assertion}) {
				classifiedPrefix.Store(true)
				result = `{"claims":[{"quote":"` + assertion + `","stance":"affirmed","temporal":"current","attribution":"assistant","scope":"unspecified","scope_quote":"","new":true,"every_s":90}]}`
			}
			writeContextFlowReply(w, false, map[string]any{"content": result}, "stop")
			return
		}
		step := int(managerCalls.Add(1)) - 1
		if step >= len(toolIDs) {
			select {
			case nextManager <- input:
			default:
			}
			// Keep the terminal candidate out of this observation. It sees
			// both parts already and cannot undo anything published early.
			<-r.Context().Done()
			return
		}
		id := toolIDs[step]
		args, _ := json.Marshal(map[string]string{"file_path": filepath.Join(workspace, id)})
		writeContextFlowReply(w, input.Stream, map[string]any{
			"content": []string{proposal, assertion}[step],
			"tool_calls": []any{map[string]any{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": "read", "arguments": string(args)},
			}},
		}, "tool_calls")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-manager-prose-prefix"
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Split scheduling claim", "", workspace, false, false)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("manager commentary prefix", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread.ID)
	defer sub.Close()
	turn, err := e.StartTurn(thread.ID, "Inspect these two local files.")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Interrupt(thread.ID) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var sawProposal, sawAssertion, sawSecondCall bool
	for !sawSecondCall {
		select {
		case event := <-sub.C:
			if event.AgentID != swarm.DefaultManagerID {
				continue
			}
			if event.Kind == "agent_message" || event.Kind == "delta" {
				sawProposal = sawProposal || event.Text == proposal
				sawAssertion = sawAssertion || event.Text == assertion
			}
			sawSecondCall = event.Kind == "tool_call" && event.ToolCallID == toolIDs[1]
		case <-deadline.C:
			t.Fatal("the actual second tool boundary did not arrive")
		}
	}
	if !sawProposal || sawAssertion || !classifiedPrefix.Load() {
		t.Errorf("the split claim was not held at the live boundary: proposal=%t assertion=%t classified prefix=%t",
			sawProposal, sawAssertion, classifiedPrefix.Load())
	}
	select {
	case input := <-nextManager:
		if strings.Contains(joinedContent(input.Messages), assertion) || !hasToolResult(input.Messages, toolIDs[1]) {
			t.Error("the next manager request leaked the split claim or lost the actual tool result")
		}
	case <-deadline.C:
		t.Fatal("the manager did not request its next response after the actual tools")
	}
	if err := e.Interrupt(thread.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "agent_message" && event.Text == assertion {
			t.Error("the split scheduling claim was stored as an admitted paragraph")
		}
	}
	if strings.Contains(replayDump(t, e, thread.ID), assertion) {
		t.Error("the split scheduling claim survived cold replay after Stop")
	}
}
