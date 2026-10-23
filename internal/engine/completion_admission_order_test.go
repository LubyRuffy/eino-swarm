package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Scheduling eligibility now precedes answer admission. A storage failure
// at that boundary must discard the candidate before finalization persists it.
func TestScheduledEligibilityReadFailureCannotPublishUnadmittedAnswer(t *testing.T) {
	const candidate = "The scheduled inspection result is available."
	var armed atomic.Bool
	var managers, policies atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/chat/completions" {
			t.Error("completion admission fixture escaped keyless loopback")
		}
		var input ordinaryLifecycleInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if len(input.Messages) > 0 && input.Messages[0].Role == schema.System && input.Messages[0].Content == scheduleAnswerPrompt {
			if input.Stream || len(input.Tools) != 0 || input.ToolChoice != "none" || len(input.Messages) != 2 {
				t.Error("policy request was not separate and tool-free")
			}
			policies.Add(1)
			armed.Store(true)
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		managers.Add(1)
		writeContextFlowReply(w, input.Stream, map[string]any{"content": candidate}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL, e.cfg.Models.Providers[0].APIKey = endpoint.URL, ""
	e.cfg.Models.Providers[0].Model = "offline-completion-admission"
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("completion admission ordering", "", "")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Prompt: "Inspect the assigned state.", EveryS: 3600, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("fixture event evidence unavailable")
	const callback = "test:scheduled-eligibility-evidence-unavailable"
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if armed.Load() && tx.Statement.Table == "events" {
			tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// GORM callback registration is process-local configuration; stop
		// background briefing queries before changing its callback slice.
		e.Shutdown()
		_ = e.store.DB().Callback().Query().Remove(callback)
	})
	turn, err := e.RunScheduleNow(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	armed.Store(false)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "read scheduled check evidence") || managers.Load() != 1 || policies.Load() != 1 {
		t.Fatalf("fixture did not reach the actual eligibility storage boundary: status=%s error=%q managers=%d policies=%d", finished.Status, finished.Error, managers.Load(), policies.Load())
	}
	if finished.Final != "" {
		t.Fatal("scheduled eligibility read failure durably published a candidate that never passed answer admission")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(event.Text, candidate) {
			t.Fatalf("unadmitted candidate reached a durable event: kind=%s seq=%d", event.Kind, event.Seq)
		}
	}
	if strings.Contains(replayDump(t, e, th.ID), candidate) {
		t.Fatal("unadmitted candidate survived assistant history replay")
	}
}
