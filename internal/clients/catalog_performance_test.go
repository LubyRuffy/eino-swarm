package clients

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// A growing transcript archive must not spend another full parse on each idle poll.
func TestUnchangedClientCatalogDoesNotRereadTranscripts(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeClaude(t, root, "idle", "first request", now)
	writeCodex(t, root, "idle", "first request", now, "task_started")
	writeCursor(t, root, "idle", "first request", now, "")
	cfg := config.ClientsConfig{Enabled: true, ClaudeDir: root, CodexDir: root, CursorDir: root, RecentDays: 3, RunningStaleSeconds: 90}
	reads := 0
	prev := catalogRead
	catalogRead = func(path string) (hit, bool) { reads++; return prev(path) }
	defer func() { catalogRead = prev }()
	first := List(cfg, now, 0)
	if len(first.Tools) != 3 || reads != 3 {
		t.Fatalf("initial catalog: groups=%d reads=%d", len(first.Tools), reads)
	}
	again := List(cfg, now.Add(100*time.Second), 0)
	if reads != 3 {
		t.Fatalf("unchanged idle catalog reread transcripts: got %d reads, want 3", reads)
	}
	if again.Tools[0].Tasks[0].Status != StatusDone || again.Tools[1].Tasks[0].Status != StatusRunning || again.Tools[2].Tasks[0].Status != StatusDone {
		t.Fatalf("cached status stopped following tool semantics: %+v", again)
	}
}

func orderedCatalog(n int) []Task {
	tasks := make([]Task, n)
	now := time.Now()
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprint(i), UpdatedAt: now.Add(time.Duration(i) * time.Millisecond)}
	}
	return tasks
}

// This is a loose ten-second budget, not a UI-frame timing assertion. The old
// insertion sort needs billions of comparisons for this chronological archive.
func TestLargeChronologicalCatalogSortHasBoundedWork(t *testing.T) {
	tasks := orderedCatalog(100000)
	done := make(chan struct{})
	go func() { sortTasks(tasks); close(done) }()
	select {
	case <-done:
		if tasks[0].ID != "99999" || tasks[len(tasks)-1].ID != "0" {
			t.Fatal("catalog no longer sorts newest first")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sorting a chronological archive exceeded ten seconds; catalog sort must not be quadratic")
	}
}

func TestCatalogSortKeepsEqualTimestampOrder(t *testing.T) {
	now := time.Now()
	tasks := []Task{{ID: "old", UpdatedAt: now.Add(-time.Hour)}, {ID: "first", UpdatedAt: now}, {ID: "second", UpdatedAt: now}}
	sortTasks(tasks)
	if got := []string{tasks[0].ID, tasks[1].ID, tasks[2].ID}; !reflect.DeepEqual(got, []string{"first", "second", "old"}) {
		t.Fatalf("order=%v", got)
	}
	sortTasks(nil)
	sortTasks(tasks[:1])
}

func BenchmarkChronologicalCatalogSort(b *testing.B) {
	original := orderedCatalog(7749)
	tasks := make([]Task, len(original))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(tasks, original)
		sortTasks(tasks)
	}
}
