package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// HTTP 200 is not a successful verification when the classifier body cannot be
// decoded. Its finite budget must yield to the existing human continuation.
func TestMalformedScheduleClassificationYieldsAfterThreeSuccessfulHTTPRequests(t *testing.T) {
	fourth := make(chan struct{}, 1)
	e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, r *http.Request, n int32) {
		if n > 3 {
			fourth <- struct{}{}
			<-r.Context().Done()
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{`}, "stop")
	})
	useEngineBackoffClock(t, e)
	th, err := e.CreateThread("finite malformed classification", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Interrupt(th.ID) }()
	deadline := time.Now().Add(5 * time.Second)
	for !e.Status(th.ID).AwaitingContinue && time.Now().Before(deadline) {
		select {
		case <-fourth:
			t.Fatal("three malformed HTTP 200 classifications requested the same candidate a fourth time")
		default:
		}
		time.Sleep(time.Millisecond)
	}
	if !e.Status(th.ID).AwaitingContinue || managers.Load() != 1 || checks.Load() != 3 {
		t.Fatalf("invalid classification did not yield without replay: status=%+v manager=%d checks=%d", e.Status(th.ID), managers.Load(), checks.Load())
	}
	rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "deferred" || rows[0].Attempts != 3 || rows[0].ClassificationFailures != 0 {
		t.Fatalf("malformed candidate was not durably deferred: rows=%+v err=%v", rows, err)
	}
}

const invalidBudgetCandidate = "The independent inspection has been arranged; its private result is awaiting verification."

// A completed schedule_task must survive unavailable verification without any
// replay of its side effect or publication of the withheld manager prose.
func TestMalformedClassifierBudgetsPreserveCommittedSchedulingAndPrivateProse(t *testing.T) {
	for _, tc := range []struct {
		name, detail string
		outcomes     []string
	}{
		{"consecutive-invalid", "invalid", []string{"invalid", "invalid", "invalid"}},
		{"alternating-invalid-first", "attempts", []string{"invalid", "unavailable", "invalid", "unavailable", "invalid", "unavailable"}},
		{"alternating-unavailable-first", "attempts", []string{"unavailable", "invalid", "unavailable", "invalid", "unavailable", "invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, managers, checks, snapshot := newScheduleAnswerInvalidBudgetFixture(t, tc.outcomes)
			th, err := e.CreateThread("bounded task verification", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Arrange an independent recurring inspection.")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Interrupt(th.ID) }()
			waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue || checks.Load() > int32(len(tc.outcomes)) })
			if !e.Status(th.ID).AwaitingContinue || managers.Load() != 2 || checks.Load() != int32(len(tc.outcomes)) {
				t.Fatalf("classification exceeded finite budget or replayed manager/tools: status=%+v manager=%d checks=%d", e.Status(th.ID), managers.Load(), checks.Load())
			}
			rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil || len(rows) != 1 || rows[0].State != "deferred" || rows[0].Attempts != len(tc.outcomes) || !strings.Contains(rows[0].Payload, invalidBudgetCandidate) {
				t.Fatalf("original private candidate not retained: %+v error=%v", rows, err)
			}
			before := <-snapshot
			after, err := e.store.ListSchedules()
			if err != nil || len(after) != 1 || !reflect.DeepEqual(before, after[0]) {
				t.Fatalf("classifier changed the committed task: before=%+v after=%+v error=%v", before, after, err)
			}
			if scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
				t.Fatal("invalid classifier consumed schedule_claim_unverified correction allowance")
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			deferred, taskCalls := 0, 0
			for _, event := range events {
				if strings.Contains(event.Text, invalidBudgetCandidate) || strings.Contains(event.Text, "private-policy-fragment") {
					t.Fatalf("private candidate or decoder error entered event/Trace: %+v", event)
				}
				if event.Kind == "tool_call" && toolCallName(event.Text) == ToolScheduleTask {
					taskCalls++
				}
				if event.Kind != KindModelBackoff || event.AgentID != scheduleAnswerAgentID {
					continue
				}
				var body struct {
					Code, Detail, CandidateID string
					WaitMS                    int64 `json:"wait_ms"`
				}
				if err := json.Unmarshal([]byte(event.Text), &body); err != nil {
					t.Fatal(err)
				}
				if body.Code == "classification_deferred" {
					deferred++
					if body.Detail != tc.detail || body.WaitMS != 0 {
						t.Fatalf("deferral omitted the exhausted budget: %+v", event)
					}
				}
				if body.Code == "classification_invalid" && body.Detail != "json" {
					t.Fatalf("invalid classifier omitted its safe decoder category: %+v", event)
				}
			}
			if deferred != 1 || taskCalls != 1 {
				t.Fatalf("duplicate deferral or scheduling side effect: deferred=%d tasks=%d", deferred, taskCalls)
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			trace, _ := json.Marshal(calls)
			if strings.Contains(string(trace), invalidBudgetCandidate) || strings.Contains(string(trace), "private-policy-fragment") || strings.Contains(replayDump(t, e, th.ID), invalidBudgetCandidate) {
				t.Fatal("private classifier content entered Trace or model replay")
			}
			for {
				select {
				case event := <-sub.C:
					if strings.Contains(event.Text, invalidBudgetCandidate) || strings.Contains(event.Text, "private-policy-fragment") {
						t.Fatalf("private classifier content was broadcast: %+v", event)
					}
				default:
					return
				}
			}
		})
	}
}

func TestTwoInvalidClassificationsCanPublishTheOriginalAnswerOnTheThirdRequest(t *testing.T) {
	e, managers, checks, snapshot := newScheduleAnswerInvalidBudgetFixture(t, []string{"invalid", "invalid", "valid"})
	th, err := e.CreateThread("recovered invalid classification", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Arrange an independent recurring inspection.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != invalidBudgetCandidate || managers.Load() != 2 || checks.Load() != 3 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
		t.Fatalf("valid third classification did not publish original answer: turn=%+v managers=%d checks=%d", got, managers.Load(), checks.Load())
	}
	before := <-snapshot
	after, err := e.store.ListSchedules()
	if err != nil || len(after) != 1 || !reflect.DeepEqual(before, after[0]) {
		t.Fatalf("classification retries changed the task: before=%+v after=%+v error=%v", before, after, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, event := range events {
		if event.Kind == "agent_message" && event.Text == invalidBudgetCandidate {
			answers++
		}
		if event.Kind == KindModelBackoff && strings.Contains(event.Text, "classification_deferred") {
			t.Fatalf("valid third response was already deferred: %+v", event)
		}
	}
	if answers != 1 {
		t.Fatalf("original answer publications=%d", answers)
	}
	assertFixtureBackoffWaits(t, e, 5*time.Second, 10*time.Second)
}

func TestClassifierFailureKindsKeepSeparateDurableConsecutiveBudgets(t *testing.T) {
	for _, outcomes := range [][]string{{"unavailable", "invalid"}, {"invalid", "unavailable"}} {
		t.Run(strings.Join(outcomes, "-then-"), func(t *testing.T) {
			e, managers, checks, _ := newScheduleAnswerInvalidBudgetFixture(t, outcomes)
			th, err := e.CreateThread("distinct classifier budgets", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(th.ID, "Arrange an independent recurring inspection.")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Interrupt(th.ID) }()
			// The third real HTTP request stays blocked so both failed attempts
			// have been persisted without a successful response erasing the row.
			waitRestartState(t, func() bool { return checks.Load() == 3 })
			rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil || len(rows) != 1 {
				t.Fatalf("missing private retry row: %+v %v", rows, err)
			}
			body, _ := json.Marshal(rows[0])
			var counters struct {
				Unavailable int `json:"classification_failures"`
				Invalid     int `json:"classification_invalids"`
			}
			if err := json.Unmarshal(body, &counters); err != nil {
				t.Fatal(err)
			}
			wantUnavailable, wantInvalid := 0, 1
			if outcomes[1] == "unavailable" {
				wantUnavailable, wantInvalid = 1, 0
			}
			if counters.Unavailable != wantUnavailable || counters.Invalid != wantInvalid || rows[0].Attempts != 2 || rows[0].State != "pending" || e.Status(th.ID).AwaitingContinue || managers.Load() != 2 {
				t.Fatalf("failure kinds shared or lost a durable budget: row=%s manager=%d", body, managers.Load())
			}
		})
	}
}

// These are real provider responses rather than direct decoder calls: each
// structural refusal must cross the same public diagnostic boundary safely.
func TestInvalidScheduleClassificationDetailsCrossTheActualHTTPBoundary(t *testing.T) {
	claimBody := func(edit func(map[string]any)) string {
		claim := map[string]any{"stance": "affirmed", "temporal": "current", "attribution": "assistant", "quote": timingFixtureAnswer, "scope": "unspecified", "new": true}
		edit(claim)
		body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
		return string(body)
	}
	for _, tc := range []struct {
		detail, content string
		tool            bool
	}{
		{detail: "missing_message", tool: true},
		{detail: "missing_message", content: ""},
		{detail: "missing_message", content: " \n\t "},
		{detail: "json", content: `{"claims":[private-policy-fragment`},
		{detail: "trailing", content: `{"claims":[]} private-policy-fragment`},
		{detail: "claim_list", content: `{"claims":null}`},
		{detail: "claim_meaning", content: claimBody(func(c map[string]any) { c["stance"] = "unknown" })},
		{detail: "claim_shape", content: claimBody(func(c map[string]any) { delete(c, "new") })},
		{detail: "unspecified_scope_quote", content: claimBody(func(c map[string]any) { c["scope_quote"] = "current state" })},
		{detail: "scope_quote", content: claimBody(func(c map[string]any) { c["scope"] = "thread"; c["scope_quote"] = "private-policy-fragment" })},
		{detail: "schedule_id", content: claimBody(func(c map[string]any) { c["schedule_id"] = "private-policy-fragment" })},
		{detail: "quote_not_in_candidate", content: claimBody(func(c map[string]any) { c["quote"] = "private-policy-fragment" })},
		{detail: "next_run_at", content: claimBody(func(c map[string]any) { c["next_run_at"] = "private-policy-fragment" })},
	} {
		t.Run(tc.detail, func(t *testing.T) {
			e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 40, func(w http.ResponseWriter, r *http.Request, n int32) {
				if n > 1 {
					<-r.Context().Done()
					return
				}
				message := map[string]any{"content": tc.content}
				if tc.tool {
					message["tool_calls"] = []any{map[string]any{"id": "forbidden-classifier-tool", "type": "function", "function": map[string]any{"name": "unavailable_fixture_tool", "arguments": `{}`}}}
				}
				writeContextFlowReply(w, false, message, "stop")
			})
			useEngineBackoffClock(t, e)
			th, err := e.CreateThread("closed classification diagnostics", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(th.ID, "Inspect the current state.")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Interrupt(th.ID) }()
			waitRestartState(t, func() bool { return checks.Load() == 2 })
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			details := 0
			for _, event := range events {
				if strings.Contains(event.Text, timingFixtureAnswer) || strings.Contains(event.Text, "private-policy-fragment") {
					t.Fatalf("structural diagnosis leaked model/candidate text: %+v", event)
				}
				if event.Kind != KindModelBackoff || event.AgentID != scheduleAnswerAgentID {
					continue
				}
				var body struct{ Code, Detail string }
				if json.Unmarshal([]byte(event.Text), &body) != nil || body.Code != "classification_invalid" || body.Detail != tc.detail {
					t.Fatalf("invalid structural diagnostic: %+v", event)
				}
				details++
			}
			if details != 1 || managers.Load() != 1 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
				t.Fatalf("diagnostic replayed manager or changed allowance: details=%d managers=%d", details, managers.Load())
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			trace, _ := json.Marshal(calls)
			if strings.Contains(string(trace), timingFixtureAnswer) || strings.Contains(string(trace), "private-policy-fragment") {
				t.Fatal("structural diagnostic leaked into Trace")
			}
		})
	}
}

func newScheduleAnswerInvalidBudgetFixture(t *testing.T, outcomes []string) (*Engine, *atomic.Int32, *atomic.Int32, <-chan store.Schedule) {
	t.Helper()
	managers, checks := &atomic.Int32{}, &atomic.Int32{}
	snapshot := make(chan store.Schedule, 1)
	var e *Engine
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []any             `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) == 0 {
			http.Error(w, "invalid fixture protocol", http.StatusBadRequest)
			return
		}
		if req.Messages[0].Content == scheduleAnswerPrompt {
			var candidate struct {
				Messages []string `json:"messages"`
			}
			if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 || json.Unmarshal([]byte(req.Messages[1].Content), &candidate) != nil || len(candidate.Messages) != 1 || candidate.Messages[0] != invalidBudgetCandidate {
				http.Error(w, "wrong candidate or classifier protocol", http.StatusBadRequest)
				return
			}
			n := int(checks.Add(1))
			if n > len(outcomes) {
				<-r.Context().Done()
				return
			}
			switch outcomes[n-1] {
			case "unavailable":
				w.WriteHeader(http.StatusServiceUnavailable)
			case "invalid":
				writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[private-policy-fragment`}, "stop")
			case "valid":
				writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			default:
				http.Error(w, "unknown fixture result", http.StatusBadRequest)
			}
			return
		}
		if !strings.Contains(req.Messages[0].Content, "## Waiting") {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "The task inspection is pending."}, "stop")
			return
		}
		switch managers.Add(1) {
		case 1:
			writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "arm-independent-inspection", "type": "function", "function": map[string]any{
					"name": ToolScheduleTask, "arguments": `{"every_s":3600,"title":"Independent inspection","prompt":"Inspect current status."}`,
				},
			}}}, "tool_calls")
		case 2:
			rows, err := e.store.ListSchedules()
			if err != nil || len(rows) != 1 || rows[0].Kind != store.ScheduleStandalone || rows[0].TitleAuto {
				t.Errorf("real schedule_task did not commit its explicit-title row: %+v %v", rows, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			snapshot <- rows[0]
			writeContextFlowReply(w, req.Stream, map[string]any{"content": invalidBudgetCandidate}, "stop")
		default:
			t.Error("classifier retry replayed the manager")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-invalid-schedule-classifier"
	e.cfg.Models.Providers[0].FirstByteTimeoutSeconds = 40
	e.cfg.Models.Providers[0].TimeoutSeconds = 40
	e.pool = provider.New(e.cfg)
	useEngineBackoffClock(t, e)
	return e, managers, checks, snapshot
}
