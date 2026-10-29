package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestParkedRegistryIsStillOwnedWhenItsSuccessorEvidenceIsSaved(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("Atomic Registry admission", "", "")
	first := mustStoredTurn(t, e, th.ID, store.Turn{})
	next := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: first.ID, Kind: KindHistoryReadStarted})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	rt.parked, rt.turnID = reg, first.ID
	savedWhileOwned := false
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("registry-admission-owner", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_registry_inherited" {
			// The callback runs synchronously inside claim's rt.mu critical section.
			savedWhileOwned = rt.parked == reg && rt.turnID == first.ID && !rt.running
		}
	}); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	ctx, cancel := context.WithCancel(t.Context())
	idle := make(chan struct{})
	defer rt.release(cancel, idle, next.ID)
	got, reused, err := rt.claimRegistry(ctx, next, cancel, idle, func() (*swarm.Registry, error) {
		t.Fatal("parked Registry was replaced")
		return nil, nil
	}, func(current *swarm.Registry) {
		if current != reg || rt.running || rt.turnID != first.ID {
			t.Fatal("Registry was reconfigured after occupation")
		}
	})
	if err != nil || got != reg || !reused || !savedWhileOwned {
		t.Fatalf("claim lost its original owner: reg=%p reused=%v saved=%v err=%v", got, reused, savedWhileOwned, err)
	}
	if read, err := e.store.TurnHasHistoryQuery(th.ID, next.ID); err != nil || !read {
		t.Fatalf("successor did not inherit the actual earlier read: %v %v", read, err)
	}
	select {
	case event := <-sub.C:
		if event.Kind != "history_registry_inherited" || event.TurnID != next.ID || event.SourceTurnID != first.ID || event.Seq <= 0 {
			t.Fatalf("wrong live inheritance receipt: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("durable inheritance event was not broadcast")
	}
	if err := e.recordHistoryRegistryInheritance(ctx, th.ID, next.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-sub.C:
		t.Fatalf("idempotent inheritance rebroadcast: %+v", event)
	default:
	}
}

func TestRejectedRegistryAdmissionDoesNotDetachOrReplaceItsOwner(t *testing.T) {
	for _, refusal := range []string{"storage", "dropped", "cancelled", "fresh preparation"} {
		t.Run(refusal, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("Refused Registry admission", "", "")
			first := mustStoredTurn(t, e, th.ID, store.Turn{})
			next := mustStoredTurn(t, e, th.ID, store.Turn{})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			rt.parked, rt.turnID = reg, first.ID
			fault := errors.New("Registry admission refused")
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			switch refusal {
			case "storage":
				if err := e.store.DB().Callback().Create().Before("gorm:create").Register("registry-admission-storage", func(tx *gorm.DB) {
					if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_registry_inherited" {
						_ = tx.AddError(fault)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "dropped":
				e.droppedTurns[first.ID] = struct{}{}
				fault = store.ErrNotFound
			case "cancelled":
				cancel(fault)
			case "fresh preparation":
				rt.parked = nil
			}
			got, reused, err := rt.claimRegistry(ctx, next, func() {}, make(chan struct{}), func() (*swarm.Registry, error) {
				return nil, fault
			}, nil)
			if !errors.Is(err, fault) || got != nil || reused || rt.running || rt.turnID != first.ID {
				t.Fatalf("refused claim mutated runtime: reg=%p reused=%v status=%+v err=%v", got, reused, rt.status(), err)
			}
			if refusal != "fresh preparation" && rt.parked != reg {
				t.Fatal("refused claim detached the old Registry")
			}
		})
	}
}

func TestConcurrentSuccessorsCannotTakeTheSameParkedRegistry(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("Concurrent Registry claims", "", "")
	first := mustStoredTurn(t, e, th.ID, store.Turn{})
	turns := []*store.Turn{mustStoredTurn(t, e, th.ID, store.Turn{}), mustStoredTurn(t, e, th.ID, store.Turn{})}
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	rt.parked, rt.turnID = reg, first.ID
	t.Cleanup(func() { rt.release(nil, rt.idle, rt.turnID) })
	var wg sync.WaitGroup
	var wins, busy int
	var resultMu sync.Mutex
	for _, turn := range turns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, reused, err := rt.claimRegistry(t.Context(), turn, func() {}, make(chan struct{}), func() (*swarm.Registry, error) {
				t.Error("a concurrent claim created a second Registry")
				return nil, errors.New("unexpected fresh Registry")
			}, nil)
			resultMu.Lock()
			defer resultMu.Unlock()
			if err == nil && reused && got == reg {
				wins++
			} else if errors.Is(err, ErrBusy) && got == nil && !reused {
				busy++
			} else {
				t.Errorf("unexpected concurrent claim: %p %v %v", got, reused, err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || busy != 1 || rt.parked != nil || rt.reg != reg {
		t.Fatalf("claims wins=%d busy=%d parked=%p current=%p", wins, busy, rt.parked, rt.reg)
	}
}

func TestFreshRegistryIsOccupiedOnlyAfterItsRestorePreparationSucceeds(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("Fresh Registry admission", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	idle := make(chan struct{})
	defer rt.release(nil, idle, turn.ID)
	got, reused, err := rt.claimRegistry(t.Context(), turn, func() {}, idle, func() (*swarm.Registry, error) {
		if rt.running || rt.reg != nil {
			t.Fatal("fresh Registry was occupied before preparation")
		}
		return reg, nil
	}, nil)
	if err != nil || reused || got != reg || !rt.running || rt.reg != reg || rt.turnID != turn.ID {
		t.Fatalf("fresh claim failed: reg=%p reused=%v err=%v", got, reused, err)
	}
}
