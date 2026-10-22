package engine

import (
	"errors"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestDeletionWaitsForTheGenerationAppendBeforeRemovingItsOwner(t *testing.T) {
	for _, projectDelete := range []bool{false, true} {
		t.Run(map[bool]string{false: "thread", true: "project"}[projectDelete], func(t *testing.T) {
			e := newTestEngine(t)
			projectID := ""
			if projectDelete {
				p, err := e.CreateProject("generation order", "", "", false)
				if err != nil {
					t.Fatal(err)
				}
				projectID = p.ID
			}
			th, err := e.CreateThread("", "", projectID)
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
			entered, released := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(released) }) }
			t.Cleanup(release)
			const hook = "generation-append-before-owner-deletion"
			if err := e.store.DB().Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
				if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == "generation_interrupted" {
					close(entered)
					<-released
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = e.store.DB().Callback().Create().Remove(hook) })
			appended := make(chan struct{})
			go func() {
				acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "output", Text: `{"reason":"model_error","detail":"known cause","recovery":"none"}`})
				close(appended)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("append did not reach its storage boundary")
			}
			deleted := make(chan error, 1)
			go func() {
				if projectDelete {
					deleted <- e.DeleteProject(projectID)
				} else {
					deleted <- e.DeleteThread(th.ID)
				}
			}()
			select {
			case err := <-deleted:
				t.Fatalf("deletion passed an unfinished generation append: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			release()
			select {
			case <-appended:
			case <-time.After(5 * time.Second):
				t.Fatal("generation append did not finish")
			}
			select {
			case err := <-deleted:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("deletion did not finish after append")
			}
			events, _ := e.store.ListGenerationEvents(th.ID)
			e.generations.Lock()
			_, cached := e.generations.threads[th.ID]
			e.generations.Unlock()
			if len(events) != 0 || cached {
				t.Fatalf("deletion retained a generation appended before it: events=%+v cached=%v", events, cached)
			}
		})
	}
}

func TestFailedDeletionKeepsGenerationCacheUsable(t *testing.T) {
	for _, projectDelete := range []bool{false, true} {
		t.Run(map[bool]string{false: "thread", true: "project"}[projectDelete], func(t *testing.T) {
			e := newTestEngine(t)
			projectID := ""
			if projectDelete {
				p, err := e.CreateProject("generation delete failure", "", "", false)
				if err != nil {
					t.Fatal(err)
				}
				projectID = p.ID
			}
			th, err := e.CreateThread("", "", projectID)
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
			failure := errors.New("injected delete failure")
			const hook = "generation-delete-storage-failure"
			if err := e.store.DB().Callback().Delete().Before("gorm:delete").Register(hook, func(tx *gorm.DB) { _ = tx.AddError(failure) }); err != nil {
				t.Fatal(err)
			}
			if projectDelete {
				err = e.DeleteProject(projectID)
			} else {
				err = e.DeleteThread(th.ID)
			}
			_ = e.store.DB().Callback().Delete().Remove(hook)
			if !errors.Is(err, failure) {
				t.Fatalf("deletion hid its storage failure: %v", err)
			}
			e.generations.Lock()
			_, cached := e.generations.threads[th.ID]
			e.generations.Unlock()
			if !cached {
				t.Fatal("failed deletion lost the live generation cache")
			}
			done := make(chan struct{})
			go func() {
				acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationCompleted, AgentID: "worker", MessageID: "output"})
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("failed deletion leaked the generation lock")
			}
			events, _ := e.store.ListGenerationEvents(th.ID)
			if len(events) != 2 || events[1].Kind != "generation_completed" {
				t.Fatalf("generation did not continue after failed deletion: %+v", events)
			}
		})
	}
}

func TestGenerationHistoryReadFailureCannotMoveAnOldOutputIntoTheCurrentTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	origin := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
	current := plantUnfinishedTurn(t, e, th.ID, "Continue the check.")
	e.record(store.Event{ThreadID: th.ID, TurnID: origin.ID, Kind: "generation_started", AgentID: "worker", MessageID: "old-output"})
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	failure := errors.New("injected generation history read failure")
	failedQueries := 0
	const hook = "generation-history-query-failure"
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]store.Event); ok {
			failedQueries++
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.store.DB().Callback().Query().Remove(hook) })
	if _, err := e.store.GetThread(th.ID); err != nil {
		t.Fatalf("generation-query failure affected the owner lookup: %v", err)
	}
	acc := newAccumulator(e, th.ID, current.ID, 0)
	receipt := swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "old-output", Text: `{"reason":"model_error","detail":"known cause","recovery":"none"}`}
	acc.onNotify(receipt)
	_ = e.store.DB().Callback().Query().Remove(hook)
	if failedQueries != 1 {
		t.Fatalf("want only the generation history query to fail, got %d", failedQueries)
	}
	e.generations.Lock()
	_, cached := e.generations.threads[th.ID]
	e.generations.Unlock()
	events, err := e.store.ListGenerationEvents(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cached || len(events) != 1 {
		t.Fatalf("unavailable history fabricated generation ownership: cached=%v events=%+v", cached, events)
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("unavailable history published a fabricated receipt: %+v", ev)
	default:
	}
	acc.onNotify(receipt)
	events, err = e.store.ListGenerationEvents(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != "generation_interrupted" || events[1].TurnID != origin.ID || events[1].MessageID != "old-output" {
		t.Fatalf("restored history did not preserve the original output: %+v", events)
	}
	e.generations.Lock()
	log := e.generations.threads[th.ID]
	rebuilt := log != nil && log.pending["old-output"].TurnID == origin.ID
	e.generations.Unlock()
	if !rebuilt {
		t.Fatal("restored query did not reconstruct durable generation ownership")
	}
}
