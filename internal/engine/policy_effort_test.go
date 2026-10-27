package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type policyEffortRequest struct{ classifier, effort string }

// newPolicyEffortEngine answers both policy classifiers over real HTTP and
// records which reasoning_effort each request carried. rejectEffort makes
// the endpoint behave like one without reasoning control.
func newPolicyEffortEngine(t *testing.T, rejectEffort bool) (*Engine, func() []policyEffortRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []policyEffortRequest
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
			Effort   string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) != 2 {
			t.Errorf("classifier request: %+v err=%v", req, err)
			return
		}
		classifier, content := "", ""
		switch {
		case req.Messages[0].Content == scheduleAnswerPrompt:
			classifier, content = "schedule-answer", `{"claims":[]}`
		case strings.Contains(req.Messages[0].Content, "## Thread wake authorization classification"):
			classifier, content = "thread-wake", `{"decision":"unchanged","quote":""}`
		default:
			t.Errorf("unexpected request system prompt")
			return
		}
		mu.Lock()
		seen = append(seen, policyEffortRequest{classifier, req.Effort})
		mu.Unlock()
		if rejectEffort && req.Effort != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"reasoning_effort is not supported","type":"invalid_request_error"}}`))
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": content}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-policy-effort-fixture"
	e.pool = provider.New(e.cfg)
	return e, func() []policyEffortRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]policyEffortRequest(nil), seen...)
	}
}

func runPolicyClassifiers(t *testing.T, e *Engine) {
	t.Helper()
	th, err := e.CreateThread("policy effort", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadReasoning(th.ID, "high"); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if _, failure := e.verifyScheduleAnswer(context.Background(), turn, []string{"The inspected work is complete."}); failure != nil {
		t.Fatalf("schedule-answer classification failed: %+v", failure)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Continue with the inspection."})
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	last := events[len(events)-1]
	if err := e.classifyThreadWakeInput(context.Background(), th.ID, turn.ID, store.WakePolicyInput{Seq: last.Seq, Kind: last.Kind, Text: last.Text}); err != nil {
		t.Fatalf("thread-wake classification failed: %v", err)
	}
}

// The schedule-answer classifier holds the finished answer until it decides;
// thinking at the conversation's level cost one deferred check 57s and 10K
// reasoning tokens on a yes/no question. Both classifiers take the policy
// level, independent of the conversation's, unless set to inherit.
func TestPolicyClassifiersThinkAtThePolicyLevelNotTheConversations(t *testing.T) {
	for _, tc := range []struct{ setting, want string }{
		{"", "low"}, {"medium", "medium"}, {"inherit", "high"},
	} {
		t.Run("setting="+tc.setting, func(t *testing.T) {
			e, seen := newPolicyEffortEngine(t, false)
			e.cfg.Swarm.PolicyReasoningEffort = tc.setting
			runPolicyClassifiers(t, e)
			got := seen()
			if len(got) != 2 || got[0] != (policyEffortRequest{"schedule-answer", tc.want}) || got[1] != (policyEffortRequest{"thread-wake", tc.want}) {
				t.Fatalf("requests=%+v want both at %q", got, tc.want)
			}
		})
	}
}

// An endpoint without reasoning control rejects the default policy level.
// That must not make every finished answer unverifiable.
func TestPolicyClassifiersSurviveAnEndpointWithoutReasoningControl(t *testing.T) {
	e, seen := newPolicyEffortEngine(t, true)
	runPolicyClassifiers(t, e)
	want := []policyEffortRequest{{"schedule-answer", "low"}, {"schedule-answer", ""}, {"thread-wake", "low"}, {"thread-wake", ""}}
	got := seen()
	if len(got) != len(want) {
		t.Fatalf("requests=%+v want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("requests=%+v want %+v", got, want)
		}
	}
}

// A level the human chose is not quietly dropped: the endpoint's refusal
// stays a classification failure, as for a chosen review level.
func TestChosenPolicyLevelKeepsTheEndpointsRejection(t *testing.T) {
	e, seen := newPolicyEffortEngine(t, true)
	e.cfg.Swarm.PolicyReasoningEffort = "medium"
	th, _ := e.CreateThread("chosen level", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	_, failure := e.verifyScheduleAnswer(context.Background(), turn, []string{"The inspected work is complete."})
	if failure == nil || failure.Code != "classification_unavailable" || provider.HTTPStatus(failure.Cause) != http.StatusBadRequest {
		t.Fatalf("failure=%+v", failure)
	}
	if got := seen(); len(got) != 1 || got[0].effort != "medium" {
		t.Fatalf("requests=%+v, a chosen level must not be retried without it", got)
	}
}

// A refused claim used to leave only a count and a hash in Trace, so a false
// refusal could not be told from a real unsupported claim. The receipt now
// names the claim's shape and why no current row proved it, but still never
// the claim's words.
func TestUnmatchedScheduleClaimIsDiagnosableInTraceWithoutItsQuote(t *testing.T) {
	e, _, _ := newScheduleAnswerFixture(t, "no-call", false)
	th, err := e.CreateThread("refusal diagnosis", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnError || !strings.Contains(got.Error, "no_matching_schedule") {
		t.Fatalf("turn=%+v", got)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		var body struct {
			Reason    string                 `json:"reason"`
			Unmatched *scheduleClaimMismatch `json:"unmatched"`
		}
		if ev.Kind != KindModelRetry || json.Unmarshal([]byte(ev.Text), &body) != nil || body.Reason != "schedule_claim_unverified" {
			continue
		}
		found = true
		u := body.Unmatched
		if u == nil || u.Scope != "thread" || !u.New || len(u.Stated) != 1 || u.Stated[0] != "every_s" || u.Candidates != 0 || u.Closest != "" {
			t.Fatalf("receipt lacks a structural diagnosis: %s", ev.Text)
		}
		if strings.Contains(ev.Text, fixtureScheduleClaim) || strings.Contains(ev.Text, "90 seconds") {
			t.Fatalf("receipt copied the claim text: %s", ev.Text)
		}
	}
	if !found {
		t.Fatal("no schedule_claim_unverified receipt in Trace")
	}
}

func TestScheduleClaimDiagnosisNamesTheClosestRowsMismatches(t *testing.T) {
	turn := &store.Turn{ID: "turn-now", ThreadID: "thread-a"}
	yes := true
	next := time.Now().Add(time.Minute)
	rows := []store.Schedule{
		{ID: "sch_other_thread", Kind: store.ScheduleThread, ThreadID: "thread-b", Status: store.ScheduleActive, NextRunAt: next, EveryS: 90},
		{ID: "sch_paused", Kind: store.ScheduleThread, ThreadID: "thread-a", Status: store.SchedulePaused, NextRunAt: next, EveryS: 90},
		{ID: "sch_far", Kind: store.ScheduleThread, ThreadID: "thread-a", Status: store.ScheduleActive, NextRunAt: next, DelayS: 30, ArmedTurnID: "turn-old"},
		{ID: "sch_near", Kind: store.ScheduleThread, ThreadID: "thread-a", Status: store.ScheduleActive, NextRunAt: next, EveryS: 60, ArmedTurnID: "turn-now"},
	}
	claim := scheduleAnswerClaim{Scope: "thread", New: &yes, EveryS: 90}
	d := diagnoseScheduleClaim(claim, rows, turn, map[string]bool{"sch_near": true})
	if d.Candidates != 2 || d.Closest != "sch_near" || len(d.Mismatched) != 1 || d.Mismatched[0] != "every_s" {
		t.Fatalf("diagnosis=%+v", d)
	}
	full := scheduleAnswerClaim{Scope: "independent", New: &yes, ScheduleID: "sch_x", DelayS: 5, Cron: "* * * * *", NextRunAt: "bad"}
	d = diagnoseScheduleClaim(full, rows, turn, nil)
	if strings.Join(d.Stated, ",") != "cron,delay_s,next_run_at,schedule_id" || d.Candidates != 0 {
		t.Fatalf("diagnosis=%+v", d)
	}
	standalone := store.Schedule{ID: "sch_job", Kind: store.ScheduleStandalone, OriginThreadID: "thread-a", Status: store.ScheduleActive, NextRunAt: next, Cron: "0 * * * *"}
	miss := scheduleClaimMismatches(full, standalone, turn, nil)
	if strings.Join(miss, ",") != "schedule_id,not_armed_this_turn,delay_s,cron,next_run_at" {
		t.Fatalf("mismatches=%v", miss)
	}
}

// A refused claim named only the fields it stated, so a mismatch on delay_s
// could not be told apart from the classifier reading a job's duration as the
// wait. The numbers on each side are structure, not the claim's words.
func TestScheduleClaimDiagnosisGivesTheComparedCadenceNumbers(t *testing.T) {
	turn := &store.Turn{ID: "turn-now", ThreadID: "thread-a"}
	yes := true
	next := time.Now().Add(time.Minute)
	rows := []store.Schedule{
		{ID: "sch_near", Kind: store.ScheduleThread, ThreadID: "thread-a", Status: store.ScheduleActive, NextRunAt: next, DelayS: 30, ArmedTurnID: "turn-now"},
	}
	claim := scheduleAnswerClaim{Scope: "thread", New: &yes, DelayS: 90, Quote: "never copied"}
	d := diagnoseScheduleClaim(claim, rows, turn, map[string]bool{"sch_near": true})
	if d.Claimed["delay_s"] != 90 || len(d.Claimed) != 1 || d.Armed["delay_s"] != 30 || len(d.Armed) != 1 {
		t.Fatalf("diagnosis lacks the compared numbers: %+v", d)
	}
	body, _ := json.Marshal(d)
	if strings.Contains(string(body), "never copied") {
		t.Fatalf("diagnosis copied the quote: %s", body)
	}

	none := diagnoseScheduleClaim(scheduleAnswerClaim{Scope: "thread", New: &yes, DelayS: 90}, nil, turn, nil)
	if none.Armed != nil {
		t.Fatalf("no candidate row, nothing armed to report: %+v", none)
	}
}
