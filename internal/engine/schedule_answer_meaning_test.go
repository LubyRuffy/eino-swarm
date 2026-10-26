package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The classifier must preserve the complete assertion's meaning. Merely
// finding an affirmative fragment inside a denial or quotation is insufficient.
func TestScheduleAnswerMeaningExcludesNonCurrentOrReportedAssertions(t *testing.T) {
	for _, tc := range []struct{ name, stance, temporal, attribution string }{
		{"current", "affirmed", "current", "assistant"},
		{"denial", "negated", "current", "assistant"},
		{"hypothetical", "hypothetical", "current", "assistant"},
		{"ended", "affirmed", "historical", "assistant"},
		{"proposal", "affirmed", "intended", "assistant"},
		{"human-policy", "affirmed", "current", "reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := map[string]any{"quote": "complete assertion", "scope": "thread", "new": true,
				"stance": tc.stance, "temporal": tc.temporal, "attribution": tc.attribution}
			body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
			claims, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{"complete assertion"})
			want := 0
			if tc.name == "current" {
				want = 1
			}
			if err != nil || len(claims) != want {
				t.Fatalf("meaning became an armed claim: claims=%+v err=%v want=%d", claims, err, want)
			}
		})
	}
	for _, field := range []string{"stance", "temporal", "attribution"} {
		for _, invalid := range []any{nil, "unknown", true} {
			claim := map[string]any{"quote": "complete assertion", "scope": "thread", "new": true,
				"stance": "affirmed", "temporal": "current", "attribution": "assistant"}
			claim[field] = invalid
			body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
			if _, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{"complete assertion"}); err == nil {
				t.Fatalf("unknown %s=%v silently granted admission", field, invalid)
			}
		}
	}
}

func TestNonClaimMeaningPublishesThroughRegistryAndHTTPWithoutGoalBlock(t *testing.T) {
	for _, tc := range []struct{ name, text, stance, temporal, attribution string }{
		{"denial", "No automatic check was arranged.", "negated", "current", "assistant"},
		{"status", "The operation remains in progress.", "", "", ""},
		{"human-policy", "The human forbids parking the conversation for an automatic check.", "affirmed", "current", "reported"},
		{"ended", "The earlier automatic check finished.", "affirmed", "historical", "assistant"},
		{"cancelled", "The earlier automatic check was cancelled.", "affirmed", "historical", "assistant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var managers, policies atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
					Tools    []any             `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if req.Messages[0].Content == scheduleAnswerPrompt {
					policies.Add(1)
					if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 {
						t.Errorf("policy escaped read-only boundary: %+v", req)
					}
					var input struct {
						Messages []string `json:"messages"`
					}
					if json.Unmarshal([]byte(req.Messages[1].Content), &input) != nil || len(input.Messages) != 1 || input.Messages[0] != tc.text {
						t.Error("classifier lost full candidate context")
					}
					claims := []any{}
					if tc.stance != "" {
						claims = append(claims, map[string]any{"quote": tc.text, "scope": "thread", "new": true,
							"stance": tc.stance, "temporal": tc.temporal, "attribution": tc.attribution})
					}
					body, _ := json.Marshal(map[string]any{"claims": claims})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				if strings.Contains(req.Messages[0].Content, "## Waiting") {
					managers.Add(1)
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": tc.text, "reasoning_content": "Inspect current state before reporting."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-claim-meaning"
			e.pool = provider.New(e.cfg)
			th, err := e.CreateThread("nonclaim admission", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "finish the assigned work"); err != nil {
				t.Fatal(err)
			}
			// A no-tool continuation uses the normal idle hold after publication;
			// that unrelated policy must neither clear its budget nor block it.
			if err := e.store.UpdateThread(th.ID, map[string]any{"goal_auto_turns": 2}); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurnInput(th.ID, UserInput{Text: "Inspect current state.", ContinueGoal: true})
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			if got.Status != store.TurnDone || got.Final != tc.text || managers.Load() != 1 || policies.Load() != 1 {
				t.Fatalf("nonclaim withheld/replayed: turn=%+v managers=%d policies=%d", got, managers.Load(), policies.Load())
			}
			updated, err := e.store.GetThread(th.ID)
			if err != nil || updated.GoalBlocked || updated.GoalBlockReason != "" || updated.GoalAutoTurns != 2 {
				t.Fatalf("admission changed goal ownership/budget: thread=%+v err=%v", updated, err)
			}
			if scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
				t.Fatal("nonclaim consumed correction")
			}
			rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil || len(rows) != 1 || rows[0].State != "accepted" || rows[0].Payload != "" {
				t.Fatalf("terminal candidate payload/decision mismatch: rows=%+v err=%v", rows, err)
			}
		})
	}
}
