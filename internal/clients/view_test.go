package clients

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestViewReturnsWhileTheWalkIsStillRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	prev := viewWalk
	viewMu.Lock()
	viewSnap = viewState{}
	viewBusy = false
	viewMu.Unlock()
	viewWalk = func(config.ClientsConfig, time.Time) ([]Task, []Task, []Task) {
		close(started)
		<-release
		return []Task{{ID: "late", Title: "after the walk", UpdatedAt: time.Now(), Status: StatusDone}}, nil, nil
	}
	t.Cleanup(func() {
		viewWalk = prev
		select {
		case <-release:
		default:
			close(release)
		}
		viewMu.Lock()
		viewSnap = viewState{}
		viewBusy = false
		viewMu.Unlock()
	})

	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: t.TempDir(),
		CodexDir: t.TempDir(), CursorDir: t.TempDir(),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	now := time.Now()
	first := View(cfg, now, 0)
	if !first.Enabled || !first.Pending || len(first.Tools) != 0 {
		t.Fatalf("a cold read waited: %+v", first)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the walk never started")
	}
	again := View(cfg, now, 0)
	if !again.Pending {
		t.Fatal("a second read published a snapshot the walk had not finished")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := View(cfg, time.Now(), 0)
		if got.Pending {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if len(got.Tools) != 3 || len(got.Tools[0].Tasks) != 1 || got.Tools[0].Tasks[0].Title != "after the walk" {
			t.Fatalf("published = %+v", got.Tools)
		}
		return
	}
	t.Fatal("the finished walk never became readable")
}
