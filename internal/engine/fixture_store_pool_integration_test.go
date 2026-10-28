package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Every preparation opens a new disk file through the production constructor.
// This does not inherit another test's schema, callback list or business rows.
func TestPreparedStoresRunRealMigrationsAndKeepTheirDataIndependent(t *testing.T) {
	var mu sync.Mutex
	opens := map[string]int{}
	pool := newEngineStorePool(2, 2, func() (*preparedEngineStore, error) {
		return openDiskEngineStore(os.MkdirTemp, func(path string) (*store.Store, error) {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return nil, errors.New("Store preparation did not receive a fresh database path")
			}
			mu.Lock()
			opens[path]++
			mu.Unlock()
			return store.Open(path)
		})
	})
	var claimed []*preparedEngineStore
	t.Cleanup(func() {
		for _, fixture := range claimed {
			if err := pool.release(fixture); err != nil {
				t.Error(err)
			}
		}
		if err := pool.close(); err != nil {
			t.Error(err)
		}
	})
	for range 2 {
		fixture, err := pool.get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		claimed = append(claimed, fixture)
	}
	a, b := claimed[0], claimed[1]
	if a.dir == b.dir || a.store == b.store {
		t.Fatal("independent test Stores shared mutable state")
	}
	for _, fixture := range claimed {
		info, err := os.Stat(fixture.dir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("private Store directory: info=%v err=%v", info, err)
		}
		var journal string
		var foreign, busy int
		if err := fixture.store.DB().Raw("PRAGMA journal_mode").Scan(&journal).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.DB().Raw("PRAGMA foreign_keys").Scan(&foreign).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.DB().Raw("PRAGMA busy_timeout").Scan(&busy).Error; err != nil {
			t.Fatal(err)
		}
		if journal != "wal" || foreign != 1 || busy != 5000 {
			t.Fatalf("disk Store PRAGMAs journal=%s foreign=%d busy=%d", journal, foreign, busy)
		}
		for _, model := range []any{&store.Project{}, &store.Thread{}, &store.Message{}, &store.Turn{}, &store.Event{}, &store.LLMCall{}, &store.Attachment{}, &store.Followup{}, &store.Schedule{}, &store.ScheduleRun{}, &store.RemoteDevice{}, &store.ThreadWakeDecision{}, &store.ComposerDraft{}, &store.ComposerDraftBlob{}, &store.PendingScheduleAnswer{}} {
			if !fixture.store.DB().Migrator().HasTable(model) {
				t.Fatalf("real migration missing %T", model)
			}
		}
		for _, table := range []string{"search_docs", "search_chunks", "thread_fts"} {
			if !fixture.store.DB().Migrator().HasTable(table) {
				t.Fatalf("real search migration missing %s", table)
			}
		}
	}
	thread := &store.Thread{Title: "independent Store marker"}
	if err := a.store.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.GetThread(thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another fixture saw the first one's row: %v", err)
	}
	if err := a.store.DB().Exec("CREATE TABLE fixture_owned_marker (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if b.store.DB().Migrator().HasTable("fixture_owned_marker") {
		t.Fatal("schema changes leaked between fixture Stores")
	}
	var callbackRuns atomic.Int32
	if err := a.store.DB().Callback().Query().Before("gorm:query").Register("fixture-owned-query", func(*gorm.DB) { callbackRuns.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.GetThread(thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if callbackRuns.Load() != 0 {
		t.Fatal("a callback registered on one fixture affected another")
	}
	if _, err := a.store.GetThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if callbackRuns.Load() == 0 {
		t.Fatal("callback isolation assertion did not exercise the registered callback")
	}
	// The callback remains installed until this private Store is actually closed.
	if err := a.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(filepath.Join(a.dir, filepath.Base(config.Default().DBPath())))
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := reopened.GetThread(thread.ID)
	closeErr := reopened.Close()
	if readErr != nil || closeErr != nil || got.Title != thread.Title {
		t.Fatalf("real Close/reopen lost data: thread=%+v read=%v close=%v", got, readErr, closeErr)
	}
	// Release all owners before exit. The process-wide pool also follows this
	// order, so TestMain includes both unclaimed migration work and cleanup.
	for _, fixture := range claimed {
		if err := pool.release(fixture); err != nil {
			t.Fatal(err)
		}
	}
	claimed = nil
	if err := pool.close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opens) < 2 {
		t.Fatal("migration accounting missed a claimed fixture")
	}
	for path, count := range opens {
		if count != 1 {
			t.Fatalf("a prepared fixture ran its constructor %d times", count)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatalf("fixture directory remained after exit: %s err=%v", path, err)
		}
	}
}

func TestStorePreparationReportsDirectoryAndConstructorFailures(t *testing.T) {
	want := errors.New("fixture preparation failed")
	if _, err := openDiskEngineStore(func(string, string) (string, error) { return "", want }, func(string) (*store.Store, error) { t.Fatal("open after directory failure"); return nil, nil }); !errors.Is(err, want) {
		t.Fatalf("directory error=%v", err)
	}
	for _, openErr := range []error{want, nil} {
		t.Run(errorText(openErr), func(t *testing.T) {
			dir := t.TempDir()
			_, err := openDiskEngineStore(func(string, string) (string, error) { return dir, nil }, func(path string) (*store.Store, error) {
				if path != filepath.Join(dir, filepath.Base(config.Default().DBPath())) {
					t.Fatalf("wrong anchored path %s", path)
				}
				return nil, openErr
			})
			if err == nil || !strings.Contains(err.Error(), "retained") || (openErr != nil && !errors.Is(err, want)) {
				t.Fatalf("constructor error lost: %v", err)
			}
			if _, statErr := os.Stat(dir); statErr != nil {
				t.Fatalf("unconfirmed failed constructor directory was deleted: %v", statErr)
			}
		})
	}
}

func TestStorePreparationKeepsItsFrozenTemporaryBase(t *testing.T) {
	base := t.TempDir()
	want := errors.New("fake constructor boundary")
	var directory, database string
	prepare := diskEngineStorePreparer(base, func(dir, pattern string) (string, error) {
		directory = dir
		if pattern != "zwai-engine-store-" {
			t.Fatalf("unexpected private directory prefix %q", pattern)
		}
		return filepath.Join(dir, "prepared"), nil
	}, func(path string) (*store.Store, error) {
		database = path
		return nil, want
	})
	t.Setenv("TMPDIR", filepath.Join(base, "later-environment"))
	if _, err := prepare(); !errors.Is(err, want) {
		t.Fatalf("constructor boundary: %v", err)
	}
	if directory != base || !strings.HasPrefix(database, filepath.Join(base, "prepared")+string(filepath.Separator)) {
		t.Fatalf("preparation followed changed TMPDIR: directory=%s database=%s", directory, database)
	}
}

func TestStorePreparationAnchorsRelativeBaseBeforeDirectoryChanges(t *testing.T) {
	original, later := t.TempDir(), t.TempDir()
	t.Chdir(original)
	base, err := absoluteEngineStoreBase("relative-stores")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("fake constructor boundary")
	var directory string
	prepare := diskEngineStorePreparer(base, func(dir, _ string) (string, error) {
		directory = dir
		return filepath.Join(dir, "prepared"), nil
	}, func(string) (*store.Store, error) {
		return nil, want
	})
	t.Chdir(later)
	if _, err := prepare(); !errors.Is(err, want) {
		t.Fatalf("constructor boundary: %v", err)
	}
	if !filepath.IsAbs(directory) || directory != filepath.Join(original, "relative-stores") {
		t.Fatalf("relative preparation base followed later cwd: %s", directory)
	}
}

func errorText(err error) string {
	if err == nil {
		return "nil Store"
	}
	return err.Error()
}

func TestStorePoolRetainsAResourceReturnedAlongsidePreparationFailure(t *testing.T) {
	want := errors.New("prepare failed after allocation")
	probe := &storeFixtureProbe{}
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) { return probe.fixture("owned"), want })
	if _, err := pool.get(context.Background()); !errors.Is(err, want) {
		t.Fatalf("prepare error lost: %v", err)
	}
	if err := pool.close(); !errors.Is(err, want) {
		t.Fatalf("pool exit lost error: %v", err)
	}
	if probe.closed.Load() != 1 || probe.removed.Load() != 1 {
		t.Fatal("failed prepared item was leaked")
	}
}

func TestEngineFixtureChoicesKeepIndependentBusinessStores(t *testing.T) {
	base := t.TempDir()
	var preparations atomic.Int32
	prepare := diskEngineStorePreparer(base, os.MkdirTemp, store.Open)
	pool := newEngineStorePool(1, 1, func() (*preparedEngineStore, error) {
		preparations.Add(1)
		return prepare()
	})
	t.Cleanup(func() {
		if err := pool.close(); err != nil {
			t.Error(err)
		}
	})
	fresh := newTestEngineFromPool(t, pool, false)
	if preparations.Load() != 0 {
		t.Fatal("fresh fixture started background preparation")
	}
	prefetched := newTestEngineFromPool(t, pool, true)
	if fresh.Config().DataDir() == prefetched.Config().DataDir() || fresh.Store() == prefetched.Store() {
		t.Fatal("fixture choices shared mutable Store state")
	}
	for _, pair := range [][2]*Engine{{fresh, prefetched}, {prefetched, fresh}} {
		thread, err := pair[0].CreateThread("fixture choice", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pair[1].Store().GetThread(thread.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("business row crossed fixture choices: %v", err)
		}
	}
}
