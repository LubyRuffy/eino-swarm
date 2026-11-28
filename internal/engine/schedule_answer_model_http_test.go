package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Verify request routing through the actual manager and HTTP adapters, and keep
// a possible claim private until the existing persisted-evidence check passes.
func TestScheduleAnswerCheckModelRoutesRealRequests(t *testing.T) {
	for _, tc := range []struct {
		name, mode, outcome     string
		enabled, mounted, claim bool
		decisions, extractions  int32
	}{
		{name: "automatic with Gateway", mode: config.ScheduleAnswerAuto, enabled: true, mounted: true, decisions: 1},
		{name: "automatic without Gateway role", mode: config.ScheduleAnswerAuto, enabled: true, extractions: 1},
		{name: "follow chat despite Gateway role", mode: config.ScheduleAnswerChat, enabled: true, mounted: true, extractions: 1},
		{name: "explicit decision model", mode: config.ScheduleAnswerDecision, enabled: true, mounted: true, decisions: 1},
		{name: "explicit decision role is missing", mode: config.ScheduleAnswerDecision, enabled: true, outcome: "decision_model_unavailable"},
		{name: "possible unsupported claim stays private", mode: config.ScheduleAnswerDecision, enabled: true, mounted: true, claim: true, decisions: 2, extractions: 2, outcome: "no_matching_schedule"},
		{name: "disabled still publishes the manager answer", mode: config.ScheduleAnswerDecision, mounted: true, claim: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, probability := timingFixtureAnswer, "0.01"
			if tc.claim {
				answer, probability = fixtureScheduleClaim, "0.1"
			}
			e, decisions, extractions := newScheduleAnswerModelFixture(t, tc.mode, tc.enabled, tc.mounted, answer, probability)
			th, err := e.CreateThread("Configure answer checking", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(th.ID, "Describe the current state.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			if tc.outcome == "" {
				if got.Status != store.TurnDone || got.Final != answer {
					t.Fatalf("expected the candidate answer, got %+v", got)
				}
			} else if got.Status != store.TurnError || got.Final != "" || !strings.Contains(got.Error, tc.outcome) || strings.Contains(replayDump(t, e, th.ID), answer) {
				t.Fatalf("expected %s with the answer withheld, got %+v", tc.outcome, got)
			}
			if decisions.Load() != tc.decisions || extractions.Load() != tc.extractions {
				t.Fatalf("decisions=%d extractions=%d, want %d/%d", decisions.Load(), extractions.Load(), tc.decisions, tc.extractions)
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			var traced int32
			for _, call := range calls {
				if call.AgentID == scheduleAnswerAgentID && call.Model == "fixture-decision" && call.ProviderID == accountCatalogID && call.Err == "" {
					traced++
				}
			}
			if traced != tc.decisions {
				t.Fatalf("recorded %d decision calls in Trace, want %d", traced, tc.decisions)
			}
		})
	}
}

func TestScheduleAnswerDecisionErrorsUseTheExistingRetryBudget(t *testing.T) {
	for _, tc := range []struct{ name, probability, code, detail string }{
		{"invalid probability", "null", "classification_invalid", "decision_response"},
		{"HTTP failure", "", "classification_unavailable", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, decisions, extractions := newScheduleAnswerModelFixture(t, config.ScheduleAnswerAuto, true, true, timingFixtureAnswer, tc.probability)
			e.answerRecoveryClock = newBackoffFixtureClock(t)
			th, err := e.CreateThread("Decision retry budget", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(th.ID, "Describe the current state.")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Interrupt(th.ID) }()
			waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue })
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			var retries, deferrals int
			for _, event := range events {
				if event.Kind != KindModelBackoff || event.AgentID != scheduleAnswerAgentID {
					continue
				}
				var body struct {
					Code, Detail string
					LastFailure  *struct{ Code, Detail string } `json:"last_failure"`
				}
				if err := json.Unmarshal([]byte(event.Text), &body); err != nil {
					t.Fatal(err)
				}
				switch body.Code {
				case tc.code:
					if body.Detail != tc.detail {
						t.Fatalf("wrong retry reason: %s", event.Text)
					}
					retries++
				case "classification_deferred":
					if body.LastFailure == nil || body.LastFailure.Code != tc.code || body.LastFailure.Detail != tc.detail {
						t.Fatalf("wrong final failure reason: %s", event.Text)
					}
					deferrals++
				}
			}
			if decisions.Load() != 3 || extractions.Load() != 0 || retries != 2 || deferrals != 1 || strings.Contains(replayDump(t, e, th.ID), timingFixtureAnswer) {
				t.Fatalf("bad decision response changed recovery or published the answer: decisions=%d extractions=%d retries=%d deferrals=%d", decisions.Load(), extractions.Load(), retries, deferrals)
			}
		})
	}
}

func newScheduleAnswerModelFixture(t *testing.T, mode string, enabled, mounted bool, answer, probability string) (*Engine, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	decisions, extractions := new(atomic.Int32), new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/decisions":
			decisions.Add(1)
			var req struct {
				Model, Input string
				Questions    []struct{ Type, Name, Instructions string }
			}
			var input struct{ Messages []string }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || json.Unmarshal([]byte(req.Input), &input) != nil ||
				req.Model != "fixture-decision" || len(input.Messages) != 1 || input.Messages[0] != answer ||
				len(req.Questions) != 1 || req.Questions[0].Type != "predicate" || req.Questions[0].Name != scheduleAnswerDecisionName ||
				req.Questions[0].Instructions != scheduleAnswerDecisionQuestion || r.Header.Get("Authorization") != "Bearer token" {
				t.Error("unexpected Gateway predicate wire request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if probability == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"answers":[{"name":%q,"probability":%s}]}`, scheduleAnswerDecisionName, probability)
		case "/v1/chat/completions":
			var req struct {
				Model    string
				Messages []*schema.Message
				Stream   bool
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 || req.Model != "fixture-chat" {
				t.Error("unexpected chat model wire request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if req.Messages[0].Content == scheduleAnswerPrompt {
				extractions.Add(1)
				claims := `{"claims":[]}`
				if answer == fixtureScheduleClaim {
					claims = `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","scope_quote":"on this conversation","new":true,"every_s":90}]}`
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": claims}, "stop")
				return
			}
			writeContextFlowReply(w, req.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected Gateway endpoint %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	e := newAccountEngine(t, upstream.URL+"/v1", "fixture-chat")
	if err := e.cfg.Update(func(cfg *config.Config) error {
		cfg.Swarm.ScheduleAnswerModel, cfg.Swarm.ScheduleAnswerCheck = mode, enabled
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := accountCatalog(t, upstream.URL+"/v1", "fixture-chat", "fixture-decision")
	source.SetModelPolicy(true, map[string]string{"conversation": "fixture-chat", "decisions": "fixture-decision"})
	source.SetCapabilities(&provider.SourceCapabilities{Authorized: true, Available: true, Configurable: true, Capabilities: map[string]provider.RoleCapability{
		"conversation": {Available: true, Supported: true, Configurable: true, Endpoints: []string{"chat_completions"}},
		"decisions":    {Available: mounted, Supported: mounted, Endpoints: []string{"decisions"}},
	}})
	e.pool.SetAccountSource(source)
	return e, decisions, extractions
}
