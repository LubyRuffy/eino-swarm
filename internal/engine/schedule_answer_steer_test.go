package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// The candidate text can be identical before and after guidance. Cancellation
// must retire its identity and HTTP request, not compare the answer's words.
func TestSteerCancelsActualClassifierAndSupersedesItsCandidate(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	e, managers, policies := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			close(entered)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, err := e.CreateThread("supersede a pending classifier", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original classification request did not start")
	}
	before, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil || len(before) != 1 || before[0].State != "pending" {
		t.Fatalf("missing original pending identity: rows=%+v err=%v", before, err)
	}
	var original pendingScheduleAnswer
	if err := json.Unmarshal([]byte(before[0].Payload), &original); err != nil || len(original.Events) != 1 || original.Events[0].MessageID == "" {
		t.Fatalf("missing original output identity: payload=%+v err=%v", original, err)
	}
	assertPendingScheduleAnswerIsPrivate(t, e, th.ID, turn.ID)
	if err := e.Steer(th.ID, "Use the updated constraint."); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("steer did not cancel the original classifier HTTP request")
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 2 || policies.Load() != 2 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
		t.Fatalf("guidance lost the original turn or spent a claim correction: turn=%+v managers=%d policies=%d", got, managers.Load(), policies.Load())
	}
	after, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil || len(after) != 2 || after[0].ID != before[0].ID || after[0].State != "rejected" || after[1].State != "accepted" || after[1].ID == before[0].ID {
		t.Fatalf("superseded identity was reused: rows=%+v err=%v", after, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, event := range events {
		if event.Kind == "agent_message" && event.Text == timingFixtureAnswer {
			answers++
			if event.MessageID == "" || event.MessageID == original.Events[0].MessageID {
				t.Fatalf("old candidate was published: %+v", event)
			}
		}
	}
	if answers != 1 {
		t.Fatalf("expected exactly one verified answer, got %d", answers)
	}
}

// The final ownership query is later than the initial preempt check. Human
// guidance arriving there is neither a false claim nor another correction.
func TestLateSteerAfterClassificationDoesNotSpendAnAlreadyUsedCorrection(t *testing.T) {
	const stale = "The previous inspection is ready."
	const updated = "The updated inspection is ready."
	var managers, policies atomic.Int32
	var waitBeforeAdmission atomic.Bool
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
			n := policies.Add(1)
			claims := `{"claims":[]}`
			if n == 1 {
				claims = `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","new":true,"every_s":90}]}`
			}
			if n == 2 {
				waitBeforeAdmission.Store(true)
			}
			writeContextFlowReply(w, false, map[string]any{"content": claims}, "stop")
			return
		}
		if !strings.Contains(req.Messages[0].Content, "## Waiting") {
			http.Error(w, "unexpected fixture role", 400)
			return
		}
		text := fixtureScheduleClaim
		switch n := managers.Add(1); n {
		case 2:
			text = stale
		case 3:
			text = updated
			found := false
			for _, m := range req.Messages {
				found = found || m.Role == schema.User && strings.Contains(m.Content, "Use the updated constraint.")
				if m.Role == schema.Assistant && strings.Contains(m.Content, stale) {
					t.Error("superseded candidate reached another manager request")
				}
			}
			if !found {
				t.Error("late steer was lost")
			}
		default:
			if n != 1 {
				t.Errorf("unexpected manager replay %d", n)
			}
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-late-admission-steer"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("late admission guidance", "", "")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register("late_admission_steer", func(tx *gorm.DB) {
		if tx.Statement.Table == "threads" && waitBeforeAdmission.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	// The private DB owns this one-shot hook until Close. Removing GORM's
	// callback while the turn's finalization still reads it would itself race.
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("did not reach the post-classification ownership query")
	}
	if scheduleAnswerRetryCount(t, e, turn.ID) != 1 {
		t.Fatal("fixture did not consume its original real-claim correction")
	}
	if err := e.Steer(th.ID, "Use the updated constraint."); err != nil {
		t.Fatal(err)
	}
	unblock()
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != updated || managers.Load() != 3 || policies.Load() != 3 || scheduleAnswerRetryCount(t, e, turn.ID) != 1 {
		t.Fatalf("late steer consumed false-claim correction or ended task: turn=%+v managers=%d policies=%d", got, managers.Load(), policies.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "agent_message" && (strings.Contains(event.Text, stale) || strings.Contains(event.Text, fixtureScheduleClaim)) {
			t.Fatalf("superseded prose became public: %+v", event)
		}
	}
}
