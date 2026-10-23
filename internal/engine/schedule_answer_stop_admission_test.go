package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Stop and answer publication must share one ownership order. The real
// Interrupt call pauses while persisting its receipt, then the real offline
// classifier finishes: the answer cannot commit behind that receipt.
func TestActualStopPreventsAnswerPublicationAfterItsDurableIntent(t *testing.T) {
	entered, releaseClassification := make(chan struct{}), make(chan struct{})
	var classificationOnce, releaseClassificationOnce sync.Once
	releasePolicy := func() { releaseClassificationOnce.Do(func() { close(releaseClassification) }) }
	t.Cleanup(releasePolicy)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.HasPrefix(req.Messages[0].Content, "## Schedule answer claims") {
			classificationOnce.Do(func() { close(entered) })
			select {
			case <-releaseClassification:
			case <-r.Context().Done():
				return
			}
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "A verified ordinary answer."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-review"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("Stop review", "", "")
	turn, err := e.StartTurn(th.ID, "Inspect the state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("classification did not enter")
	}
	stopEntered, releaseStop := make(chan struct{}), make(chan struct{})
	var releaseStopOnce sync.Once
	unblockStop := func() { releaseStopOnce.Do(func() { close(releaseStop) }) }
	t.Cleanup(unblockStop)
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("stop_admission_barrier", func(tx *gorm.DB) {
		ev, ok := tx.Statement.Dest.(*store.Event)
		if ok && ev.Kind == KindUserStopRequested {
			close(stopEntered)
			<-releaseStop
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer e.store.DB().Callback().Create().Remove("stop_admission_barrier")
	stopped := make(chan error, 1)
	go func() { stopped <- e.Interrupt(th.ID) }()
	select {
	case <-stopEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not reach the real event boundary")
	}
	releasePolicy()
	rt := e.runtimeFor(th.ID)
	acquired := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rt.mu.TryLock() {
			rt.mu.Unlock()
			time.Sleep(time.Millisecond)
			continue
		}
		time.Sleep(10 * time.Millisecond)
		if rt.mu.TryLock() {
			rt.mu.Unlock()
			continue
		}
		acquired = true
		break
	}
	unblockStop()
	if !acquired {
		t.Fatal("Stop/admission did not hold runtime ownership during the persistence boundary")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked")
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnCancelled || finished.Final != "" || finished.Error != "interrupted" {
		t.Fatalf("Stop did not retain its terminal cause: %+v", finished)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	stopSeq := int64(0)
	for _, ev := range events {
		if ev.Kind == KindUserStopRequested {
			stopSeq = ev.Seq
		}
		if stopSeq > 0 && ev.Kind == "agent_message" {
			t.Fatalf("answer published after durable Stop: stop_seq=%d answer_seq=%d text=%q", stopSeq, ev.Seq, ev.Text)
		}
	}
	if stopSeq == 0 {
		t.Fatal("the actual Stop did not leave its durable intent")
	}
}
