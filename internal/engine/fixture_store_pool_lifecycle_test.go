package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type storeFixtureProbe struct {
	closed    atomic.Int32
	removed   atomic.Int32
	closeErr  error
	removeErr error
}

func (s *storeFixtureProbe) fixture(id string) *preparedEngineStore {
	return &preparedEngineStore{dir: id,
		close:  func() error { s.closed.Add(1); return s.closeErr },
		remove: func(string) error { s.removed.Add(1); return s.removeErr },
	}
}

func awaitStoreFixture[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("owned Store preparation did not reach its synchronization point")
		var zero T
		return zero
	}
}

// Closing a pool never disposes a resource already handed to a running test.
func TestPreparedStoresHaveOneOwnerAndAreNotReused(t *testing.T) {
	var next atomic.Int32
	var mu sync.Mutex
	probes := map[*preparedEngineStore]*storeFixtureProbe{}
	pool := newEngineStorePool(2, 2, func() (*preparedEngineStore, error) {
		probe := &storeFixtureProbe{}
		fixture := probe.fixture(fmt.Sprint(next.Add(1)))
		mu.Lock()
		probes[fixture] = probe
		mu.Unlock()
		return fixture, nil
	})
	first, err := pool.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.release(first); err != nil {
		t.Fatal(err)
	}
	second, err := pool.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first.dir == second.dir {
		t.Fatal("a used Store was recycled into another test")
	}
	if err := pool.close(); err == nil || !strings.Contains(err.Error(), "still claimed") {
		t.Fatalf("Close accepted a test-owned Store: %v", err)
	}
	mu.Lock()
	secondProbe := probes[second]
	mu.Unlock()
	if secondProbe.closed.Load() != 0 {
		t.Fatal("pool closed an active test's Store")
	}
	if _, err := pool.get(context.Background()); !errors.Is(err, errEngineStorePoolClosed) {
		t.Fatalf("Get after Close: %v", err)
	}
	if err := pool.release(second); err != nil {
		t.Fatal(err)
	}
	if err := pool.release(second); err == nil {
		t.Fatal("one claim accepted two releases")
	}
	// An earlier ownership refusal remains visible even after its owner releases.
	if err := pool.close(); err == nil {
		t.Fatal("repeated Close erased its original refusal")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, probe := range probes {
		if probe.closed.Load() != 1 || probe.removed.Load() != 1 {
			t.Fatalf("Store cleanup counts close=%d remove=%d", probe.closed.Load(), probe.removed.Load())
		}
	}
}

func TestStorePoolStopJoinsItsAlreadyStartedPreparations(t *testing.T) {
	entered := make(chan struct{}, 2)
	finish := make(chan struct{})
	var opens, closes atomic.Int32
	pool := newEngineStorePool(2, 1, func() (*preparedEngineStore, error) {
		opens.Add(1)
		entered <- struct{}{}
		<-finish
		return &preparedEngineStore{dir: "owned", close: func() error { closes.Add(1); return nil }, remove: func(string) error { return nil }}, nil
	})
	pool.start()
	awaitStoreFixture(t, entered)
	awaitStoreFixture(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- pool.close() }()
	awaitStoreFixture(t, pool.ctx.Done())
	select {
	case err := <-closed:
		t.Fatalf("Close abandoned a started migration: %v", err)
	default:
	}
	close(finish)
	if err := awaitStoreFixture(t, closed); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 2 || closes.Load() != 2 {
		t.Fatalf("after Stop opens=%d closes=%d", opens.Load(), closes.Load())
	}
}

func TestStorePoolCancelledGetDoesNotAbandonItsPreparation(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	probe := &storeFixtureProbe{}
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) {
		close(entered)
		<-finish
		return probe.fixture("cancelled"), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	go func() { _, err := pool.get(ctx); got <- err }()
	awaitStoreFixture(t, entered)
	cancel()
	if err := awaitStoreFixture(t, got); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get cause=%v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- pool.close() }()
	awaitStoreFixture(t, pool.ctx.Done())
	close(finish)
	if err := awaitStoreFixture(t, closed); err != nil {
		t.Fatal(err)
	}
	if probe.closed.Load() != 1 || probe.removed.Load() != 1 {
		t.Fatal("cancelled Get leaked its unclaimed Store")
	}
	if _, err := pool.get(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled Get=%v", err)
	}
}

func TestStorePoolCloseRacingGetDoesNotHandOutStoppedResources(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	probe := &storeFixtureProbe{}
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) {
		close(entered)
		<-finish
		return probe.fixture("racing"), nil
	})
	got := make(chan error, 1)
	go func() { _, err := pool.get(context.Background()); got <- err }()
	awaitStoreFixture(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- pool.close() }()
	awaitStoreFixture(t, pool.ctx.Done())
	close(finish)
	if err := awaitStoreFixture(t, got); !errors.Is(err, errEngineStorePoolClosed) {
		t.Fatalf("racing Get=%v", err)
	}
	if err := awaitStoreFixture(t, closed); err != nil {
		t.Fatal(err)
	}
	if probe.closed.Load() != 1 || probe.removed.Load() != 1 {
		t.Fatal("racing Get/Close did not clean exactly once")
	}
}

func TestStorePoolPreparationFailureIsNotHiddenByFallback(t *testing.T) {
	want := errors.New("migration failed")
	for _, prepareErr := range []error{want, nil} {
		t.Run(fmt.Sprint(prepareErr), func(t *testing.T) {
			pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) { return nil, prepareErr })
			if _, err := pool.get(context.Background()); err == nil || (prepareErr != nil && !errors.Is(err, want)) {
				t.Fatalf("failed prepare Get=%v", err)
			}
			if err := pool.close(); err == nil {
				t.Fatal("failed preparation accepted at pool exit")
			}
		})
	}
}

func TestStoreFixtureCleanupKeepsDataWhenCloseFails(t *testing.T) {
	closeErr, removeErr := errors.New("close failed"), errors.New("remove failed")
	for _, test := range []struct {
		name                string
		closeErr, removeErr error
		removed             int32
	}{
		{"close", closeErr, nil, 0}, {"remove", nil, removeErr, 1}, {"ok", nil, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &storeFixtureProbe{closeErr: test.closeErr, removeErr: test.removeErr}
			fixture := probe.fixture("private")
			for range 2 {
				err := fixture.dispose()
				if test.closeErr != nil && !errors.Is(err, closeErr) {
					t.Fatalf("lost close error: %v", err)
				}
				if test.removeErr != nil && !errors.Is(err, removeErr) {
					t.Fatalf("lost remove error: %v", err)
				}
			}
			if probe.closed.Load() != 1 || probe.removed.Load() != test.removed {
				t.Fatal("cleanup repeated or deleted data before successful Close")
			}
		})
	}
}

func TestStorePoolReportsUnusedStoreCleanupFailure(t *testing.T) {
	want := errors.New("unused close failed")
	entered, finish := make(chan struct{}), make(chan struct{})
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) {
		close(entered)
		<-finish
		return (&storeFixtureProbe{closeErr: want}).fixture("retained"), nil
	})
	pool.start()
	awaitStoreFixture(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- pool.close() }()
	awaitStoreFixture(t, pool.ctx.Done())
	close(finish)
	if err := awaitStoreFixture(t, closed); !errors.Is(err, want) {
		t.Fatalf("unused error lost: %v", err)
	}
}

func TestEngineTestExitIncludesPoolJoinAndCleanup(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			entered, finish := make(chan struct{}), make(chan struct{})
			want := errors.New("test exit cleanup")
			pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) {
				close(entered)
				<-finish
				return (&storeFixtureProbe{closeErr: want}).fixture("retained"), nil
			})
			var reported error
			result := make(chan int, 1)
			go func() {
				result <- runEngineTests(func() int { pool.start(); <-entered; return code }, pool, func(err error) { reported = err })
			}()
			awaitStoreFixture(t, entered)
			awaitStoreFixture(t, pool.ctx.Done())
			select {
			case <-result:
				t.Fatal("test process exited before its Store worker joined")
			default:
			}
			close(finish)
			got := awaitStoreFixture(t, result)
			if (code == 0 && got != 1) || (code != 0 && got != code) || !errors.Is(reported, want) {
				t.Fatalf("exit=%d original=%d report=%v", got, code, reported)
			}
		})
	}
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) { t.Fatal("unused pool started"); return nil, nil })
	if got := runEngineTests(func() int { return 7 }, pool, func(error) { t.Fatal("unused pool failed") }); got != 7 {
		t.Fatalf("original exit=%d", got)
	}
}

func TestStorePoolRejectsConfigurationThatCannotProduceResources(t *testing.T) {
	for _, test := range []struct {
		workers, capacity int
		prepare           func() (*preparedEngineStore, error)
	}{
		{0, 1, func() (*preparedEngineStore, error) { return nil, nil }},
		{1, 0, func() (*preparedEngineStore, error) { return nil, nil }},
		{1, 1, nil},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid Store pool would leave Get waiting forever")
				}
			}()
			_ = newEngineStorePool(test.workers, test.capacity, test.prepare)
		}()
	}
}
