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
			// Keep a concrete cadence so this tests meaning independently of
			// the rule that ignores assertions with no verifiable fields.
			const text = "complete assertion every 90 seconds"
			claim := map[string]any{"quote": text, "scope": "unspecified", "every_s": 90,
				"stance": tc.stance, "temporal": tc.temporal, "attribution": tc.attribution}
			if tc.name == "current" {
				claim["new"] = true
			}
			body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
			claims, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{text})
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
			claim := map[string]any{"quote": "complete assertion", "scope": "unspecified", "new": true,
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
	for _, tc := range []struct {
		name, text, stance, temporal, attribution string
		externalScopeQuote                        string
		mixed                                     bool
	}{
		{name: "denial", text: "No automatic check was arranged.", stance: "negated", temporal: "current", attribution: "assistant"},
		{name: "status", text: "The operation remains in progress."},
		{name: "human-policy", text: "The human forbids parking the conversation for an automatic check.", stance: "affirmed", temporal: "current", attribution: "reported"},
		{name: "ended", text: "The earlier automatic check finished.", stance: "affirmed", temporal: "historical", attribution: "assistant"},
		{name: "cancelled", text: "The earlier automatic check was cancelled.", stance: "affirmed", temporal: "historical", attribution: "assistant"},
		{name: "external-process", text: "观察到备份服务每 45 秒检查一次队列。", stance: "affirmed", temporal: "current", attribution: "assistant", externalScopeQuote: "备份服务"},
		{name: "external-and-local", text: "The backup service checks its queue every 45 seconds.", stance: "affirmed", temporal: "current", attribution: "assistant", externalScopeQuote: "backup service", mixed: true},
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
					expected := tc.text
					if tc.mixed && managers.Load() == 1 {
						expected += " " + fixtureScheduleClaim
					}
					var input struct {
						Messages []string `json:"messages"`
					}
					if json.Unmarshal([]byte(req.Messages[1].Content), &input) != nil || len(input.Messages) != 1 || input.Messages[0] != expected {
						t.Error("classifier lost full candidate context")
					}
					claims := []any{}
					if tc.stance != "" {
						claim := map[string]any{"quote": tc.text, "scope": "unspecified", "new": true,
							"stance": tc.stance, "temporal": tc.temporal, "attribution": tc.attribution}
						if tc.externalScopeQuote != "" {
							claim["scope"], claim["scope_quote"], claim["every_s"] = "external", tc.externalScopeQuote, 45
							delete(claim, "new")
						}
						claims = append(claims, claim)
					}
					if tc.mixed && managers.Load() == 1 {
						claims = append(claims, map[string]any{"quote": fixtureScheduleClaim, "scope": "unspecified", "new": true, "every_s": 90,
							"stance": "affirmed", "temporal": "current", "attribution": "assistant"})
					}
					body, _ := json.Marshal(map[string]any{"claims": claims})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				answer := tc.text
				if strings.Contains(req.Messages[0].Content, "## Waiting") {
					step := managers.Add(1)
					if tc.mixed && step == 1 {
						answer += " " + fixtureScheduleClaim
					}
					if tc.mixed && step == 2 && !strings.Contains(req.Messages[len(req.Messages)-1].Content, "identify the external owner") {
						t.Error("correction did not tell the manager how to preserve the external finding")
					}
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": answer, "reasoning_content": "Inspect current state before reporting."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-claim-meaning"
			e.pool = provider.New(e.cfg)
			useEngineBackoffClock(t, e)
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
			defer func() { _ = e.Interrupt(th.ID) }()
			if tc.externalScopeQuote != "" {
				waitRestartState(t, func() bool {
					return !e.Status(th.ID).Running || policies.Load() > 2
				})
				if policies.Load() > 2 {
					t.Fatal("the classifier retried an external scope quote")
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			want := int32(1)
			if tc.mixed {
				want++
			}
			if got.Status != store.TurnDone || got.Final != tc.text || managers.Load() != want || policies.Load() != want {
				t.Fatalf("nonclaim withheld/replayed: turn=%+v managers=%d policies=%d", got, managers.Load(), policies.Load())
			}
			updated, err := e.store.GetThread(th.ID)
			if err != nil || updated.GoalBlocked || updated.GoalBlockReason != "" || updated.GoalAutoTurns != 2 {
				t.Fatalf("admission changed goal ownership/budget: thread=%+v err=%v", updated, err)
			}
			if scheduleAnswerRetryCount(t, e, turn.ID) != int(want-1) {
				t.Fatal("correction was not limited to the unsupported local claim")
			}
			if tc.externalScopeQuote != "" {
				if replay := replayDump(t, e, th.ID); !strings.Contains(replay, tc.text) || strings.Contains(replay, fixtureScheduleClaim) {
					t.Fatal("replay lost the external finding or admitted an unverified local claim")
				}
				if schedules, err := e.store.ListSchedules(); err != nil || len(schedules) != 0 {
					t.Fatalf("reporting an external cadence created a local wait: %+v, %v", schedules, err)
				}
			}
			rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil || len(rows) != int(want) || rows[len(rows)-1].State != "accepted" || rows[len(rows)-1].Payload != "" {
				t.Fatalf("terminal candidate payload/decision mismatch: rows=%+v err=%v", rows, err)
			}
			if tc.mixed && (rows[0].State != "rejected" || rows[0].Payload != "") {
				t.Fatalf("unsupported local candidate was retained: %+v", rows[0])
			}
		})
	}
}

func TestExternalObservationDoesNotRelaxLocalClaimValidation(t *testing.T) {
	const external = "The archive service polls its queue every 45 seconds."
	const local = "The scheduled check runs every 60 seconds."
	externalItem := `{"quote":"` + external + `","scope":"external","scope_quote":"The archive service","stance":"affirmed","temporal":"current","attribution":"assistant","every_s":45}`
	localItem := `{"quote":"` + local + `","scope":"unspecified","stance":"affirmed","temporal":"current","attribution":"assistant","every_s":60`
	body := []string{external, local}
	for _, tc := range []struct {
		name, items string
		want        int
		detail      string
	}{
		{"external-only", externalItem, 0, ""},
		{"mixed", externalItem + "," + localItem + `,"new":true}`, 1, ""},
		{"local-still-requires-new", externalItem + "," + localItem + "}", 0, "claim_shape"},
		{"local-still-requires-scope-quote", externalItem + "," + strings.Replace(localItem, `"unspecified"`, `"thread"`, 1) + `,"new":true,"scope_quote":"The archive service"}`, 0, "scope_quote"},
		{"local-still-requires-exact-quote", externalItem + "," + strings.Replace(localItem, local, "A paraphrased local check", 1) + `,"new":true}`, 0, "quote_not_in_candidate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := decodeScheduleAnswer(schema.AssistantMessage(`{"claims":[`+tc.items+`]}`, nil), body)
			if tc.detail != "" {
				if failure, ok := err.(*scheduleAnswerError); !ok || failure.Detail != tc.detail || len(claims) != 0 {
					t.Fatalf("invalid local claim passed validation: claims=%+v err=%v", claims, err)
				}
				return
			}
			if err != nil || len(claims) != tc.want {
				t.Fatalf("external observation depended on app arming metadata: claims=%+v err=%v", claims, err)
			}
			if tc.want == 1 && (claims[0].Quote != local || claims[0].New == nil || !*claims[0].New || claims[0].EveryS != 60) {
				t.Fatalf("mixed candidate lost its separate local claim: %+v", claims[0])
			}
		})
	}
}

func TestExternalScheduleClaimRequiresAnOperatorInItsQuoteAndNoAppID(t *testing.T) {
	const text = "The backup service checks its queue every 45 seconds. An app check is armed as sch_local."
	for _, tc := range []struct{ name, quote, witness, scheduleID, wantDetail string }{
		{"grounded", "The backup service checks its queue every 45 seconds.", "backup service", "", ""},
		{"missing", "The backup service checks its queue every 45 seconds.", "", "", "scope_quote"},
		{"out-of-quote", "The backup service checks its queue every 45 seconds.", "An app check", "", "scope_quote"},
		{"absent-from-candidate", "The backup service checks its queue every 45 seconds.", "invented operator", "", "scope_quote"},
		{"quote-not-grounded", "Another service checks its queue every 45 seconds.", "Another service", "", "quote_not_in_candidate"},
		{"app-id", text, "backup service", "sch_local", "schedule_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := map[string]any{"quote": tc.quote, "scope": "external", "scope_quote": tc.witness, "schedule_id": tc.scheduleID, "new": false,
				"stance": "affirmed", "temporal": "current", "attribution": "assistant"}
			body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
			claims, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{text})
			if tc.wantDetail == "" {
				if err != nil || len(claims) != 0 {
					t.Fatalf("external observation affected admission: claims=%+v err=%v", claims, err)
				}
				return
			}
			failure, ok := err.(*scheduleAnswerError)
			if !ok || failure.Detail != tc.wantDetail || len(claims) != 0 {
				t.Fatalf("invalid external evidence reached admission: claims=%+v diagnostic=%#v", claims, failure)
			}
		})
	}
}

func TestExternalScopeWithoutAWitnessCannotPublishALocalClaim(t *testing.T) {
	const claim = "I have armed this conversation to check again every 90 seconds."
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if len(req.Messages) > 0 && req.Messages[0].Content == scheduleAnswerPrompt {
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[{"quote":"` + claim + `","scope":"external","scope_quote":"","stance":"affirmed","temporal":"current","attribution":"assistant","every_s":90}]}`}, "stop")
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": claim}, "stop")
	}))
	defer endpoint.Close()
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-audit-external"
	e.pool = provider.New(e.cfg)
	useEngineBackoffClock(t, e)
	th, err := e.CreateThread("Schedule audit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Report current state.")
	if err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool { return !e.Status(th.ID).Running || e.Status(th.ID).AwaitingContinue })
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	invalidWitness := false
	for _, ev := range events {
		if ev.Kind == KindModelBackoff && strings.Contains(ev.Text, `"detail":"scope_quote"`) {
			invalidWitness = true
		}
		if ev.Kind == "agent_message" && strings.Contains(ev.Text, claim) {
			t.Fatal("unarmed local success claim reached the public timeline")
		}
	}
	if !e.Status(th.ID).AwaitingContinue || !invalidWitness {
		t.Fatal("the ungrounded external classification did not exhaust its validation budget and pause")
	}
}
