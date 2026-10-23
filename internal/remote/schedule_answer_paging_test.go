package remote

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneEarlierKeepsVisibleRetryNoticesAndSkipsInvisiblePayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"workers_uncollected", `{"reason":"workers_uncollected"}`, true},
		{"schedule_claim_unverified", `{"reason":"schedule_claim_unverified"}`, true},
		{"context_overflow", `{"reason":"context_overflow"}`, true},
		{"invalid_tool_json", `{"reason":"invalid_tool_json"}`, true},
		{"rate_limited", `{"reason":"rate_limited"}`, true},
		{"model_unavailable", `{"reason":"model_unavailable"}`, true},
		{"model_response_invalid", `{"reason":"model_response_invalid"}`, true},
		{"model_transport", `{"reason":"model_transport"}`, true},
		{"model_idle", `{"reason":"model_idle"}`, true},
		{"unknown", `{"reason":"future_reason"}`, false},
		{"missing", `{"attempt":1}`, false},
		{"malformed", `{"reason":`, false},
		{"wrong_type", `{"reason":5}`, false},
		{"null", `null`, false},
		{"ordinary_text", "legacy retry envelope", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testEngine(t)
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			// The newest Earlier page has only a retry notice and an orphaned
			// tool result. Returning older text loses a visible correction; an
			// invisible retry must instead keep walking to that older text.
			for _, ev := range []store.Event{
				{ThreadID: th.ID, Kind: "agent_message", AgentID: "manager", Text: "earlier answer"},
				{ThreadID: th.ID, Kind: "model_retry", AgentID: "manager", Text: tc.text},
				{ThreadID: th.ID, Kind: "tool_result", AgentID: "manager", Text: `{"ok":false}`, ToolCallID: "hidden"},
				{ThreadID: th.ID, Kind: "user_message", Text: "later task"},
			} {
				if err := e.Store().AppendEvent(&ev); err != nil {
					t.Fatal(err)
				}
			}
			response := Handle(e, config.RemoteConfig{WatchEvents: 2}, Request{ID: "earlier", Op: OpLog, ThreadID: th.ID, Before: 4}, "relay", "session")
			if !response.OK {
				t.Fatalf("Earlier request failed: %+v", response)
			}
			if tc.want {
				if len(response.Events) != 2 || response.Events[0].Kind != "model_retry" || response.Events[0].Text != tc.text || response.Seq != 2 || !response.More {
					t.Fatalf("Earlier skipped the visible retry notice: %+v", response)
				}
			} else if len(response.Events) != 1 || response.Events[0].Text != "earlier answer" || response.Seq != 1 || response.More {
				t.Fatalf("Earlier stopped on invisible retry payload: %+v", response)
			}
		})
	}
}
