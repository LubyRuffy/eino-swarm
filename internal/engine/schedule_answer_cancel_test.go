package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestStopCancelsTheActualScheduleAnswerPolicyRequest(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	var policyCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.Messages[0].Content, "## Schedule answer claims") {
			if policyCalls.Add(1) == 1 {
				close(entered)
			}
			<-r.Context().Done()
			close(cancelled)
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": fixtureScheduleClaim}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-blocked-answer-policy"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("stop policy", "", "")
	turn, err := e.StartTurn(th.ID, "Inspect the pending work.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("policy did not enter actual HTTP request")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnCancelled || got.Final != "" || got.Error != "interrupted" || policyCalls.Load() != 1 {
		t.Fatalf("Stop became a policy failure/retry: %+v calls=%d", got, policyCalls.Load())
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop left policy HTTP request running")
	}
	if strings.Contains(replayDump(t, e, th.ID), fixtureScheduleClaim) {
		t.Fatal("Stop exposed pending answer")
	}
}

func TestAnswerAdmissionHoldsOwnershipUntilItsPublicationCommits(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("admission ordering", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := swarm.NewRegistry()
	defer reg.Close()
	idle := make(chan struct{})
	rt.occupy(reg, cancel, turn.ID, idle)
	defer rt.release(cancel, idle, turn.ID)
	entered, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	callback := "test:schedule-answer-publication"
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Event" {
			first.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer e.store.DB().Callback().Create().Remove(callback)
	admitted := make(chan *scheduleAnswerError, 1)
	go func() {
		admitted <- e.admitScheduleAnswer(th.ID, turn.ID, nil, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: "The current inspection finished."}})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("answer did not reach atomic publication")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- e.Interrupt(th.ID) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop bypassed an admitted publication: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("Stop did not cancel after the ordered publication")
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	if len(events) < 2 || events[0].Kind != "agent_message" || events[1].Kind != KindUserStopRequested {
		t.Fatalf("publication and Stop receipts reordered: %+v", events)
	}
	if failure := e.admitScheduleAnswer(th.ID, turn.ID, nil, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: "late answer"}}); failure == nil || failure.Code != "answer_owner_stopped" {
		t.Fatalf("post-Stop admission succeeded: %+v", failure)
	}
}
