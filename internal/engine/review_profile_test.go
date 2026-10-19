package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Review policy is independent of the original task. Blank configuration
// preserves the task's wire behavior, including omitting an unset effort.
func TestReviewProfileControlsOnlyTheReviewHTTPReasoningEffort(t *testing.T) {
	for _, tc := range []struct{ name, override, inherited, want string }{
		{"explicit low", "low", "high", "low"},
		{"explicit low from unset", "low", "", "low"},
		{"default inherits", "", "high", "high"},
		{"default stays unset", "", "", ""},
		{"medium normalizes", " MEDIUM ", "low", "medium"},
		{"high normalizes", "HIGH", "low", "high"},
		{"invalid inherits", "unsupported", "high", "high"},
		{"disable is not invented", "off", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
			setReviewProfile(t, e, tc.override)
			th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Retain the explicitly stated project convention."}})
			turn.ReasoningEffort = tc.inherited
			e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
			settleReviewProtocol(t, e)
			requests := fixture.snapshot()
			if len(requests) != 1 {
				t.Fatalf("review requests=%d", len(requests))
			}
			assertReviewProfileRequest(t, requests[0], tc.want, e.cfg.Swarm.CompletionTokenLimit())
			if out := decodeReview(t, waitForReview(t, e, turn.ID)); out.Err != "" {
				t.Fatalf("profile prevented a complete review: %+v", out)
			}
			if turn.ReasoningEffort != tc.inherited {
				t.Fatal("review profile rewrote the original turn")
			}
		})
	}
}

func TestReviewProfileLeavesManagerAndCatalogTidyHTTPRequestsUnchanged(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "The fixture operation is complete.", nil })
	setReviewProfile(t, e, "low")
	e.cfg.Memory.AutoReview = false
	project, thread := projectThread(t, e)
	if err := e.SetThreadReasoning(thread.ID, "high"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Confirm the fixture state.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || finished.ReasoningEffort != "high" {
		t.Fatalf("manager profile changed: %+v", finished)
	}
	if _, err := e.ProjectMemory(project.ID).WriteSkill("fixture-procedure", "Verify the fixture state.", "Inspect, execute, verify."); err != nil {
		t.Fatal(err)
	}
	if _, err := e.FoldProjectSkills(project.ID); err != nil {
		t.Fatal(err)
	}
	requests := fixture.snapshot()
	if len(requests) != 2 {
		t.Fatalf("expected manager and tidy requests, got %d", len(requests))
	}
	for _, request := range requests {
		assertReviewProfileRequest(t, request, "high", e.cfg.Swarm.CompletionTokenLimit())
	}
}

func setReviewProfile(t *testing.T, e *Engine, value string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"review_reasoning_effort": value})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &e.cfg.Memory); err != nil {
		t.Fatal(err)
	}
}

func assertReviewProfileRequest(t *testing.T, raw json.RawMessage, want string, maxTokens int) {
	t.Helper()
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(request["reasoning_effort"], &got); len(request["reasoning_effort"]) > 0 && err != nil {
		t.Fatal(err)
	}
	if got != want || (want == "" && request["reasoning_effort"] != nil) {
		t.Fatalf("reasoning effort=%q want=%q request=%s", got, want, raw)
	}
	var cap int
	if err := json.Unmarshal(request["max_tokens"], &cap); err != nil || cap != maxTokens {
		t.Fatalf("profile changed completion budget: %s err=%v", request["max_tokens"], err)
	}
	if request["thinking"] != nil || request["max_completion_tokens"] != nil || strings.Contains(string(raw), `"thinking":{"type":"disabled"}`) {
		t.Fatal("review profile invented a thinking mode or another output-budget field")
	}
}
