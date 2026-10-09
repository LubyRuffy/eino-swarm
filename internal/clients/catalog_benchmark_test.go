package clients

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func BenchmarkIdleTranscriptRefresh(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	// Representative edge reads from long transcripts, in an isolated directory.
	body := strings.Repeat(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"`+strings.Repeat("x", 900)+`"}]}}`+"\n", 140)
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("rollout-%04d.jsonl", i)), []byte(body), 0600); err != nil {
			b.Fatal(err)
		}
	}
	now := time.Now()
	scanCodex(root, now, time.Minute)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := scanCodex(root, now, time.Minute)
		if len(tasks) != 1000 {
			b.Fatalf("rows=%d", len(tasks))
		}
	}
}

func BenchmarkCachedClientView(b *testing.B) {
	cfg := config.ClientsConfig{Enabled: true, ClaudeDir: b.TempDir(), RecentDays: 3}
	now := time.Now()
	tasks := make([]Task, 7749)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprint(i), UpdatedAt: now.Add(-time.Duration(i) * time.Millisecond)}
	}
	viewMu.Lock()
	previous, busy := viewSnap, viewBusy
	viewSnap = viewState{key: scanKey(cfg), at: now, codex: tasks}
	viewBusy = false
	viewMu.Unlock()
	defer func() { viewMu.Lock(); viewSnap, viewBusy = previous, busy; viewMu.Unlock() }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := View(cfg, now, 0); len(got.Tools[1].Tasks) != 5 {
			b.Fatal("view lost first page")
		}
	}
}
