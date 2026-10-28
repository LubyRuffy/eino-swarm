package engine

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

var errEngineStorePoolClosed = errors.New("test Store preparation stopped")

// newTestEngine reads this only while m.Run is executing, after testing has
// parsed flags. It is never changed by a test or read by a background worker.
var engineStorePrefetch = flag.Bool("engine.store-prefetch", true, "prepare independent engine test Stores concurrently")

// A prepared Store has one lifetime and one eventual test owner. It is never
// reset or returned to the ready queue after a test has used it.
type preparedEngineStore struct {
	dir    string
	store  *store.Store
	close  func() error
	remove func(string) error

	once sync.Once
	err  error
}

func (f *preparedEngineStore) dispose() error {
	f.once.Do(func() {
		if err := f.close(); err != nil {
			f.err = fmt.Errorf("close test Store in %s (directory retained): %w", f.dir, err)
			return
		}
		if err := f.remove(f.dir); err != nil {
			f.err = fmt.Errorf("remove test Store directory %s: %w", f.dir, err)
		}
	})
	return f.err
}

// Only preparation runs concurrently: every item is a distinct disk database
// opened by the real Store constructor, including all migrations and FTS setup.
// Started preparation cannot be interrupted inside Store.Open. Stop prevents
// further admissions and Close joins those already admitted before returning.
type engineStorePool struct {
	prepare func() (*preparedEngineStore, error)
	ready   chan *preparedEngineStore
	workers int
	ctx     context.Context
	cancel  context.CancelFunc

	startOnce sync.Once
	closeOnce sync.Once
	preparing sync.WaitGroup
	getting   sync.WaitGroup
	mu        sync.Mutex
	stopped   bool
	err       error
	claimed   map[*preparedEngineStore]bool
	closeErr  error
}

func newEngineStorePool(workers, capacity int, prepare func() (*preparedEngineStore, error)) *engineStorePool {
	if workers < 1 || capacity < 1 || prepare == nil {
		panic("test Store pool requires positive bounds and a preparer")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &engineStorePool{
		prepare: prepare, ready: make(chan *preparedEngineStore, capacity), workers: workers,
		ctx: ctx, cancel: cancel, claimed: map[*preparedEngineStore]bool{},
	}
}

func (p *engineStorePool) start() {
	p.startOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.stopped {
			return
		}
		p.preparing.Add(p.workers)
		for range p.workers {
			go p.produce()
		}
	})
}

func (p *engineStorePool) produce() {
	defer p.preparing.Done()
	for {
		p.mu.Lock()
		admitted := !p.stopped
		p.mu.Unlock()
		if !admitted {
			return
		}
		fixture, err := p.prepare()
		if err != nil || fixture == nil {
			if fixture != nil {
				err = errors.Join(err, fixture.dispose())
			}
			if err == nil {
				err = errors.New("test Store preparer returned no Store")
			}
			p.mu.Lock()
			p.err = errors.Join(p.err, err)
			p.stopLocked()
			p.mu.Unlock()
			return
		}
		select {
		case p.ready <- fixture:
		case <-p.ctx.Done():
			p.recordError(fixture.dispose())
			return
		}
	}
}

func (p *engineStorePool) stopLocked() {
	if !p.stopped {
		p.stopped = true
		p.cancel()
	}
}

func (p *engineStorePool) recordError(err error) {
	p.mu.Lock()
	p.err = errors.Join(p.err, err)
	p.mu.Unlock()
}

func (p *engineStorePool) stoppedError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return errors.Join(errEngineStorePoolClosed, p.err)
}

func (p *engineStorePool) get(ctx context.Context) (*preparedEngineStore, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil, p.stoppedError()
	}
	// Stop takes the same mutex before Close waits, so no Add can race a
	// zero-count Wait. A cancelled Get still owns any item it has received.
	p.getting.Add(1)
	p.mu.Unlock()
	defer p.getting.Done()
	p.start()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, p.stoppedError()
	case fixture := <-p.ready:
		p.mu.Lock()
		stopped, cancelled := p.stopped, ctx.Err()
		if !stopped && cancelled == nil {
			p.claimed[fixture] = true
			p.mu.Unlock()
			return fixture, nil
		}
		p.mu.Unlock()
		disposeErr := fixture.dispose()
		p.recordError(disposeErr)
		if cancelled != nil {
			return nil, errors.Join(cancelled, disposeErr)
		}
		return nil, p.stoppedError()
	}
}

func (p *engineStorePool) release(fixture *preparedEngineStore) error {
	p.mu.Lock()
	owned := p.claimed[fixture]
	if owned {
		p.claimed[fixture] = false
	}
	p.mu.Unlock()
	if !owned {
		return errors.New("test Store release has no matching claim")
	}
	// Keep the claim through disposal: a concurrent Close must not report
	// success while a test still owns the Store or its cleanup is unfinished.
	err := fixture.dispose()
	p.mu.Lock()
	delete(p.claimed, fixture)
	p.err = errors.Join(p.err, err)
	p.mu.Unlock()
	return err
}

func (p *engineStorePool) close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.stopLocked()
		p.mu.Unlock()
		// Synchronize lazy start before waiting for workers. No Store.Open is
		// abandoned merely because cancellation arrived during migration.
		p.start()
		p.preparing.Wait()
		p.getting.Wait()
		close(p.ready)
		for fixture := range p.ready {
			p.recordError(fixture.dispose())
		}
		p.mu.Lock()
		p.closeErr = p.err
		if n := len(p.claimed); n != 0 {
			p.closeErr = errors.Join(p.closeErr, fmt.Errorf("%d test Stores still claimed; owners retain their directories", n))
		}
		p.mu.Unlock()
	})
	return p.closeErr
}

func diskEngineStorePreparer(base string, mkdir func(string, string) (string, error), open func(string) (*store.Store, error)) func() (*preparedEngineStore, error) {
	return func() (*preparedEngineStore, error) {
		return openDiskEngineStore(func(_ string, pattern string) (string, error) {
			return mkdir(base, pattern)
		}, open)
	}
}

func openDiskEngineStore(mkdir func(string, string) (string, error), open func(string) (*store.Store, error)) (*preparedEngineStore, error) {
	dir, err := mkdir("", "zwai-engine-store-")
	if err != nil {
		return nil, fmt.Errorf("create test Store directory: %w", err)
	}
	// Derive the filename from the actual Config contract without loading or
	// reading process environment in a background worker.
	dbPath := filepath.Join(dir, filepath.Base(config.Default().DBPath()))
	st, err := open(dbPath)
	if err != nil {
		// Open does not expose a partially initialized connection on failure.
		// Retain that private directory for diagnosis, never claim it was reaped.
		return nil, fmt.Errorf("open test Store in %s (directory retained): %w", dir, err)
	}
	if st == nil {
		return nil, fmt.Errorf("open test Store in %s returned nil (directory retained)", dir)
	}
	return &preparedEngineStore{dir: dir, store: st, close: st.Close, remove: os.RemoveAll}, nil
}

var engineTestStores *engineStorePool

func absoluteEngineStoreBase(base string) (string, error) {
	return filepath.Abs(base)
}

func TestMain(m *testing.M) {
	workers := min(4, stdruntime.GOMAXPROCS(0))
	// Resolve ambient TMPDIR before any test mutates its process environment.
	base, err := absoluteEngineStoreBase(os.TempDir())
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "resolve engine Store preparation directory:", err)
		os.Exit(1)
	}
	prepare := diskEngineStorePreparer(base, os.MkdirTemp, store.Open)
	engineTestStores = newEngineStorePool(workers, workers, prepare)
	os.Exit(runEngineTests(m.Run, engineTestStores, func(err error) {
		_, _ = fmt.Fprintln(os.Stderr, "engine Store preparation cleanup:", err)
	}))
}

func runEngineTests(run func() int, pool *engineStorePool, report func(error)) int {
	code := run()
	if err := pool.close(); err != nil {
		report(err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func clearTestEngineSeed(t *testing.T) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
}

func testEngineConfig(t *testing.T, dir string) *config.Config {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// The test swarm remains small, while its actual workers still run in parallel.
	cfg.Swarm.MaxConcurrent = 4
	cfg.Swarm.AgentTimeoutSeconds = 30
	return cfg
}

func buildTestEngine(t *testing.T, cfg *config.Config, st *store.Store) *Engine {
	t.Helper()
	e := New(cfg, st, provider.NewMock(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e
}
