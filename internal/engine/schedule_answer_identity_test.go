package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Generic job/cron wording cannot establish a separate-conversation claim.
// The original failure repeated a real scheduling action to repair prose.
func TestGenericScheduleReferencePublishesWithoutRearmingThroughRegistryHTTP(t *testing.T) {
	for _, withID := range []bool{false, true} {
		t.Run(fmt.Sprint(withID), func(t *testing.T) {
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			th, _ := e.CreateThread("schedule identity", "", "")
			row, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Title: "scheduled inspection", Cron: "15 21 * * *", Prompt: "inspect current work"})
			if err != nil {
				t.Fatal(err)
			}
			text := "The recurring inspection remains armed."
			if withID {
				text = fmt.Sprintf("The recurring inspection %s remains armed.", row.ID)
			}
			var managers, checks atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if req.Messages[0].Content == scheduleAnswerPrompt {
					checks.Add(1)
					claim := map[string]any{"quote": text, "stance": "affirmed", "temporal": "current", "attribution": "assistant", "scope": "unspecified", "new": false, "cron": row.Cron}
					if withID {
						claim["schedule_id"] = row.ID
					}
					body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				if strings.Contains(req.Messages[0].Content, "## Waiting") {
					managers.Add(1)
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
			}))
			defer endpoint.Close()
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-identity"
			e.pool = provider.New(e.cfg)
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Report the current inspection status.")
			if err != nil {
				t.Fatal(err)
			}
			// Fail promptly on an incompatible policy protocol, preserving its
			// actual refusal rather than waiting through repeated invalid output.
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			for running := true; running; {
				select {
				case ev := <-sub.C:
					if ev.Kind == "model_backoff" {
						_ = e.Interrupt(th.ID)
						t.Fatalf("valid reference sent to classification recovery: %s", ev.Text)
					}
					if ev.Kind == "done" || ev.Kind == "error" {
						running = false
					}
				case <-deadline.C:
					_ = e.Interrupt(th.ID)
					t.Fatal("reference did not finish")
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			if got.Status != store.TurnDone || got.Final != text || managers.Load() != 1 || checks.Load() != 1 {
				t.Fatalf("real reference refused/repeated: turn=%+v managers=%d checks=%d", got, managers.Load(), checks.Load())
			}
			current, _ := e.store.GetSchedule(row.ID)
			if !current.UpdatedAt.Equal(row.UpdatedAt) || !current.NextRunAt.Equal(row.NextRunAt) || current.ArmedTurnID != row.ArmedTurnID {
				t.Fatalf("prose changed wait: before=%+v after=%+v", row, current)
			}
		})
	}
}

func TestScheduleClaimScopeAndIdentityMustBeGrounded(t *testing.T) {
	for _, tc := range []struct {
		name, scope, witness, id string
		valid                    bool
	}{
		{"generic", "unspecified", "", "wake", true},
		{"explicit", "thread", "this conversation", "wake", true},
		{"missing-scope-witness", "independent", "", "wake", false},
		{"invented-scope-witness", "thread", "another conversation", "wake", false},
		{"generic-with-scope-witness", "unspecified", "this conversation", "wake", false},
		{"invented-id", "unspecified", "", "unmentioned", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quote := "The check wake on this conversation remains armed."
			body, _ := json.Marshal(map[string]any{"claims": []any{map[string]any{"quote": quote, "stance": "affirmed", "temporal": "current", "attribution": "assistant", "scope": tc.scope, "scope_quote": tc.witness, "schedule_id": tc.id, "new": false}}})
			_, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{quote})
			if (err == nil) != tc.valid {
				t.Fatalf("grounding valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

// An unqualified exact ID may resolve either owned kind, never another owner.
func TestUnqualifiedScheduleIdentityRetainsOwnershipAndReceiptGuards(t *testing.T) {
	turn := &store.Turn{ID: "current", ThreadID: "owner"}
	existing, fresh := false, true
	at := time.Now().UTC()
	for _, tc := range []struct {
		name, kind, thread, origin, armedTurn, status string
		newClaim, receipt, want                       bool
	}{
		{"owned-wake", store.ScheduleThread, "owner", "owner", "old", store.ScheduleActive, false, false, true},
		{"owned-job", store.ScheduleStandalone, "", "owner", "old", store.ScheduleActive, false, false, true},
		{"foreign-wake", store.ScheduleThread, "other", "owner", "old", store.ScheduleActive, false, false, false},
		{"foreign-job", store.ScheduleStandalone, "", "other", "old", store.ScheduleActive, false, false, false},
		{"unknown-kind", "unknown", "owner", "owner", "old", store.ScheduleActive, false, false, false},
		{"cancelled", store.ScheduleThread, "owner", "owner", "old", store.ScheduleCancelled, false, false, false},
		{"missing-receipt", store.ScheduleThread, "owner", "owner", "current", store.ScheduleActive, true, false, false},
		{"old-turn", store.ScheduleThread, "owner", "owner", "old", store.ScheduleActive, true, true, false},
		{"fresh", store.ScheduleThread, "owner", "owner", "current", store.ScheduleActive, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := scheduleAnswerClaim{Quote: "wake remains armed", Scope: "unspecified", New: &existing, ScheduleID: "wake"}
			if tc.newClaim {
				claim.New = &fresh
			}
			row := store.Schedule{ID: "wake", Kind: tc.kind, ThreadID: tc.thread, OriginThreadID: tc.origin, ArmedTurnID: tc.armedTurn, Status: tc.status, NextRunAt: at}
			if got := scheduleClaimMatches(claim, row, turn, map[string]bool{"wake": tc.receipt}); got != tc.want {
				t.Fatalf("matched=%v want=%v", got, tc.want)
			}
		})
	}
}
