package engine

import (
	"encoding/json"
	"errors"
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

// Preparing an uploaded attachment can outlive the turn selected by Steer.
// The final admission must refuse that stale owner before recording guidance.
func TestSteerCannotWriteToAReplacedTurnAfterInputPreparation(t *testing.T) {
	requests := make(chan int32, 2)
	first, second := make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce sync.Once
	finishFirst := func() { firstOnce.Do(func() { close(first) }) }
	finishSecond := func() { secondOnce.Do(func() { close(second) }) }
	var managers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
			t.Errorf("invalid owner admission fixture request: %v", err)
			return
		}
		if req.Messages[0].Content == scheduleAnswerPrompt {
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		if !strings.Contains(req.Messages[0].Content, "## Waiting") {
			t.Error("unexpected auxiliary request in owner admission fixture")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := managers.Add(1)
		if n > 2 {
			t.Errorf("stale steering started another manager request: %d", n)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- n
		release := first
		if n == 2 {
			release = second
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The inspection completed."}, "stop")
	}))
	t.Cleanup(func() { finishFirst(); finishSecond(); endpoint.Close() })
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-steer-owner-admission"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("steer owner admission", "", "")
	upload, err := e.SaveUpload(th.ID, "context.txt", writeUpload("The selected context is available."))
	if err != nil {
		t.Fatal(err)
	}
	oldTurn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
	case <-time.After(5 * time.Second):
		t.Fatal("the original turn did not reach its real model HTTP request")
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	oldRegistry := rt.reg
	rt.mu.Unlock()
	// Empty guidance must be refused while the target is genuinely running;
	// an idle-target error would not prove input validation or non-delivery.
	if err := e.Steer(th.ID, " \t "); err == nil || !strings.Contains(err.Error(), "nothing to steer") {
		t.Fatalf("busy empty guidance was accepted or misclassified: %v", err)
	}
	if oldRegistry.HasPendingSteers() || hasKind(t, e, th.ID, KindSteer) {
		t.Fatal("refused empty guidance changed the timeline or manager inbox")
	}
	hydrating, releaseHydration := make(chan struct{}), make(chan struct{})
	var hydrationOnce sync.Once
	unblock := func() { hydrationOnce.Do(func() { close(releaseHydration) }) }
	t.Cleanup(unblock)
	var held atomic.Bool
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register("steer_owner_preparation", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "attachments" && held.CompareAndSwap(false, true) {
			close(hydrating)
			<-releaseHydration
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Keep this one-shot hook on the private database until it is closed.
	// Turn-idle publication precedes the run goroutine's final owner lookup;
	// removing GORM callbacks there would race that real tail query.
	steered := make(chan error, 1)
	steerDone := make(chan struct{})
	go func() {
		defer close(steerDone)
		steered <- e.SteerInput(th.ID, UserInput{Text: "Use the selected context.", Files: []string{upload.RelPath}})
	}()
	t.Cleanup(func() {
		unblock()
		select {
		case <-steerDone:
		case <-time.After(5 * time.Second):
			t.Error("steering did not leave its storage callback before cleanup")
		}
	})
	select {
	case <-hydrating:
	case <-time.After(5 * time.Second):
		t.Fatal("steering did not reach actual uploaded-input preparation")
	}
	finishFirst()
	if got := waitForTurn(t, e, oldTurn.ID); got.Status != store.TurnDone {
		t.Fatalf("original turn did not finish while input was preparing: %+v", got)
	}
	waitSettled(t, e, th.ID)
	newTurn, err := e.StartTurn(th.ID, "Inspect the next state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-requests:
		if n != 2 {
			t.Fatalf("unexpected replacement model request: %d", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement turn did not reach its real model HTTP request")
	}
	rt.mu.Lock()
	newRegistry := rt.reg
	rt.mu.Unlock()
	if newRegistry == oldRegistry || newTurn.ID == oldTurn.ID {
		t.Fatal("fixture did not replace the captured turn and registry")
	}
	unblock()
	select {
	case err := <-steered:
		if !errors.Is(err, ErrIdle) {
			t.Fatalf("stale steering was not refused: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale steering did not return after input preparation")
	}
	if oldRegistry.HasPendingSteers() || newRegistry.HasPendingSteers() {
		t.Fatal("stale guidance reached an old or replacement manager inbox")
	}
	for _, turn := range []*store.Turn{oldTurn, newTurn} {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Kind == KindSteer {
				t.Fatalf("stale guidance was recorded against turn %s: %+v", turn.ID, event)
			}
		}
	}
	finishSecond()
	if got := waitForTurn(t, e, newTurn.ID); got.Status != store.TurnDone || managers.Load() != 2 {
		t.Fatalf("refused guidance changed replacement execution: turn=%+v requests=%d", got, managers.Load())
	}
}
