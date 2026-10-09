package clients

import (
	"strconv"
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

func TestViewPublishesSortedImmutablePagesFromRealWalk(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for i := 0; i < 6; i++ {
		writeClaude(t, root, string(rune('a'+i)), "row", now.Add(-time.Duration(6-i)*time.Second))
	}
	cfg := claudeCfg(root)
	deadline := time.Now().Add(3 * time.Second)
	var got Catalog
	for time.Now().Before(deadline) {
		got = View(cfg, time.Now(), 0)
		if !got.Pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got.Pending || len(got.Tools) != 3 || len(got.Tools[0].Tasks) != 5 || got.Tools[0].Tasks[0].ID != "claude:f" {
		t.Fatalf("snapshot=%+v", got)
	}
	next, err := strconv.ParseInt(got.Tools[0].Next, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	got.Tools[0].Tasks[0].ID = "caller modified row"
	again := View(cfg, time.Now(), 0)
	if again.Tools[0].Tasks[0].ID != "claude:f" {
		t.Fatal("caller changed immutable snapshot")
	}
	older := View(cfg, time.Now(), next)
	if len(older.Tools[0].Tasks) != 1 || older.Tools[0].Tasks[0].ID != "claude:a" {
		t.Fatalf("older=%+v", older)
	}
	cfg.Enabled = false
	if off := View(cfg, time.Now(), 0); off.Enabled || len(off.Tools) != 0 {
		t.Fatal("disabled catalog returned rows")
	}
}
