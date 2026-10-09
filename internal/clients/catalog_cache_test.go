package clients

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCatalogCacheTracksChangedDeletedAndReplacedFiles(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	path := filepath.Join(root, "projects", "work", "row.jsonl")
	writeClaude(t, root, "row", "original", now)
	if got := scanClaude(root, now, time.Minute); len(got) != 1 || got[0].Title != "original" {
		t.Fatalf("first=%+v", got)
	}
	writeClaude(t, root, "row", "appended request", now.Add(time.Second))
	if got := scanClaude(root, now, time.Minute); len(got) != 1 || got[0].Title != "appended request" {
		t.Fatalf("changed=%+v", got)
	}
	// Replacing the inode must invalidate even identical size/mtime metadata.
	replacement := filepath.Join(root, "replace.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(before), "appended request", "replaced request", 1)
	writeFileTime(t, replacement, body, now.Add(time.Second))
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if got := scanClaude(root, now, time.Minute); len(got) != 1 || got[0].Title != "replaced request" {
		t.Fatalf("replacement=%+v", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := scanClaude(root, now, time.Minute); len(got) != 0 {
		t.Fatalf("deleted row remained: %+v", got)
	}
	catalogMu.Lock()
	kept := len(catalogSnapshots[ToolClaude].current)
	catalogMu.Unlock()
	if kept != 0 {
		t.Fatalf("deleted parsed rows retained: %d", kept)
	}
}

func TestCatalogCacheRechecksEmptyRowsAndRootChanges(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	path := filepath.Join(root, "projects", "work", "row.jsonl")
	writeFileTime(t, path, "{\"type\":\"mode\"}\n", now)
	prev := catalogRead
	reads := 0
	catalogRead = func(path string) (hit, bool) { reads++; return prev(path) }
	defer func() { catalogRead = prev }()
	for i := 0; i < 2; i++ {
		if got := scanClaude(root, now, time.Minute); len(got) != 0 {
			t.Fatal("empty session listed")
		}
	}
	if reads != 1 {
		t.Fatalf("empty transcript reread %d times", reads)
	}
	writeClaude(t, root, "row", "now visible", now.Add(time.Second))
	if got := scanClaude(root, now, time.Minute); len(got) != 1 {
		t.Fatalf("new transcript hidden: %+v", got)
	}
	other := t.TempDir()
	writeClaude(t, other, "different", "new root", now)
	if got := scanClaude(other, now, time.Minute); len(got) != 1 || got[0].Title != "new root" {
		t.Fatalf("root change=%+v", got)
	}
	catalogMu.Lock()
	kept := len(catalogSnapshots[ToolClaude].current)
	catalogMu.Unlock()
	if kept != 1 {
		t.Fatalf("prior root cache retained: %d", kept)
	}
}

func TestCatalogCacheDoesNotRetainFailedOrNonRegularReads(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeClaude(t, root, "row", "request", now)
	prev := catalogRead
	catalogRead = func(string) (hit, bool) { return hit{}, false }
	defer func() { catalogRead = prev }()
	scan := beginSessionScan(ToolClaude, root)
	if _, ok := scan.read(root, now, time.Minute); ok {
		t.Fatal("directory listed")
	}
	if _, ok := scan.read(filepath.Join(root, "absent"), now, time.Minute); ok {
		t.Fatal("missing file listed")
	}
	if _, ok := scan.read(filepath.Join(root, "projects", "work", "row.jsonl"), now, time.Minute); ok {
		t.Fatal("failed read listed")
	}
	scan.finish()
	catalogRead = prev
	if got := scanClaude(root, now, time.Minute); len(got) != 1 {
		t.Fatal("failed read prevented retry")
	}
}

func TestCatalogMetadataFallbacksAndReadOnlyFiles(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for _, tool := range []string{ToolClaude, ToolCodex, ToolCursor} {
		body := map[string]string{ToolClaude: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read"}]}}`, ToolCodex: `{"type":"response_item","payload":{"type":"function_call","name":"read"}}`, ToolCursor: `{"role":"assistant","message":{"content":"reply"}}`}[tool]
		path := filepath.Join(root, tool+".jsonl")
		writeFileTime(t, path, body+"\n", now)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		scan := beginSessionScan(tool, root)
		task, ok := scan.read(path, now, time.Minute)
		scan.finish()
		if !ok || task.Title != tool || task.ID != tool+":"+tool {
			t.Fatalf("fallback for %s=%+v ok=%v", tool, task, ok)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("catalog modified foreign transcript")
		}
	}
}

func TestConcurrentListPagesDoNotMutateCachedRows(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for i := 0; i < 10; i++ {
		writeClaude(t, root, string(rune('a'+i)), "row", now.Add(time.Duration(i)*time.Millisecond))
	}
	cfg := claudeCfg(root)
	first := List(cfg, now, 0)
	original := first.Tools[0].Tasks[0].ID
	first.Tools[0].Tasks[0].ID = "changed by caller"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cat := List(cfg, now, 0)
			if cat.Tools[0].Tasks[0].ID != original {
				t.Error("reader mutated shared cache")
			}
		}()
	}
	wg.Wait()
}

func TestCatalogIgnoresNonSessionEntriesAndMissingTranscripts(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeClaude(t, root, "visible", "visible task", now)
	writeCodex(t, root, "visible", "visible task", now, "task_complete")
	writeCursor(t, root, "visible", "visible task", now, "turn_ended")
	writeFileTime(t, filepath.Join(root, "projects", "not-a-project"), "ignore", now)
	writeFileTime(t, filepath.Join(root, "sessions", "not-a-session.jsonl"), "ignore", now)
	writeFileTime(t, filepath.Join(root, "sessions", "rollout-not-json.txt"), "ignore", now)
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "sessions", "rollout-missing.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "no-transcripts"), 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "projects", "invalid", "agent-transcripts")
	writeFileTime(t, filepath.Join(dir, "not-a-session-directory"), "ignore", now)
	if err := os.MkdirAll(filepath.Join(dir, "missing-file"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFileTime(t, filepath.Join(dir, "empty", "empty.jsonl"), "{\"type\":\"mode\"}\n", now)
	for tool, scan := range map[string]func(string, time.Time, time.Duration) []Task{ToolClaude: scanClaude, ToolCodex: scanCodex, ToolCursor: scanCursor} {
		got := scan(root, now, time.Minute)
		if len(got) != 1 || got[0].Title != "visible task" {
			t.Fatalf("%s listed non-session entries: %+v", tool, got)
		}
	}
}
