package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/schema"
)

func TestUnverifiedManagerProseDoesNotReachSameRoundForkHTTPInput(t *testing.T) {
	const userTask = "Review the retained evidence before drawing any conclusions."
	const workerTask = "Check the evidence assigned to the reviewer."
	const spawnID = "fork-after-unverified-prose"
	managerInput := make(chan scheduleCompletionRequest, 1)
	workerInput := make(chan scheduleCompletionRequest, 1)
	var managerCalls, workerCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode fixture request: %v", err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(input.Messages) == 2 && input.Messages[0].Content == scheduleAnswerPrompt {
			result := `{"claims":[]}`
			if strings.Contains(input.Messages[1].Content, fixtureScheduleClaim) {
				result = `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","scope_quote":"on this conversation","new":true,"every_s":90}]}`
			}
			writeContextFlowReply(w, false, map[string]any{"content": result}, "stop")
			return
		}
		for _, offered := range input.Tools {
			if offered.Function.Name != "spawn_agent" {
				continue
			}
			if managerCalls.Add(1) == 1 {
				args, _ := json.Marshal(map[string]any{"role": "reviewer", "task": workerTask, "fork_context": true})
				writeContextFlowReply(w, input.Stream, map[string]any{
					"content": fixtureScheduleClaim,
					"tool_calls": []any{map[string]any{
						"index": 0, "id": spawnID, "type": "function",
						"function": map[string]any{"name": "spawn_agent", "arguments": string(args)},
					}},
				}, "tool_calls")
				return
			}
			select {
			case managerInput <- input:
			default:
			}
			// Observe the exact next manager request before final-answer policy
			// or completion logic could hide a leak into that request.
			<-r.Context().Done()
			return
		}
		if workerCalls.Add(1) == 1 {
			workerInput <- input
		}
		writeContextFlowReply(w, input.Stream, map[string]any{"content": "The worker checked the assigned evidence."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-manager-prose-fork"
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("manager prose fork admission", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, userTask)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Interrupt(thread.ID) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var worker, manager scheduleCompletionRequest
	select {
	case worker = <-workerInput:
	case <-deadline.C:
		t.Fatal("the real spawned worker never requested a model response")
	}
	select {
	case manager = <-managerInput:
	case <-deadline.C:
		t.Fatal("the real manager did not continue after its spawn call")
	}

	workerPayload := joinedContent(worker.Messages)
	for _, text := range []string{"historical context", userTask, workerTask} {
		if !strings.Contains(workerPayload, text) {
			t.Errorf("the fork did not inherit its real context marker %q", text)
		}
	}
	if strings.Contains(workerPayload, fixtureScheduleClaim) {
		t.Error("unverified manager scheduling prose reached the same-round forked worker HTTP request")
	}
	if strings.Contains(joinedContent(manager.Messages), fixtureScheduleClaim) {
		t.Error("unverified manager scheduling prose reached the next manager HTTP request")
	}
	var spawnCall, spawnReceipt bool
	for _, message := range manager.Messages {
		for _, call := range message.ToolCalls {
			spawnCall = spawnCall || (call.ID == spawnID && call.Function.Name == "spawn_agent")
		}
		if message.Role == schema.Tool && message.ToolCallID == spawnID {
			var receipt struct {
				AgentID string `json:"agent_id"`
			}
			spawnReceipt = json.Unmarshal([]byte(message.Content), &receipt) == nil && receipt.AgentID != ""
		}
	}
	if !spawnCall || !spawnReceipt || managerCalls.Load() != 2 || workerCalls.Load() != 1 {
		t.Errorf("redaction removed or repeated the actual delegation: call=%t receipt=%t manager requests=%d worker requests=%d",
			spawnCall, spawnReceipt, managerCalls.Load(), workerCalls.Load())
	}
	if err := e.Interrupt(thread.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if strings.Contains(replayDump(t, e, thread.ID), fixtureScheduleClaim) {
		t.Error("unverified manager scheduling prose survived cold replay after Stop")
	}
}
