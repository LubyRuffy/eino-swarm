package clients

import (
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// View is the catalog a sidebar poll or a phone may read without waiting.
// The disk walk runs beside that call. Pending means this key has no
// finished snapshot yet; Tools is empty and must not replace a list the
// caller already painted. A finished snapshot is returned even when a
// refresh is due, and one walk is started in the background.
func View(cfg config.ClientsConfig, now time.Time, beforeMs int64) Catalog {
	if !cfg.Enabled {
		return Catalog{Tools: []Group{}}
	}
	key := scanKey(cfg)
	kick(cfg, now, key)
	viewMu.Lock()
	defer viewMu.Unlock()
	if viewSnap.key != key || viewSnap.at.IsZero() {
		return Catalog{Enabled: true, Pending: true}
	}
	windowStart := now.Add(-cfg.RecentWindow())
	var before time.Time
	if beforeMs > 0 {
		before = time.UnixMilli(beforeMs)
	}
	return Catalog{
		Enabled: true,
		Tools: []Group{
			page(ToolClaude, append([]Task(nil), viewSnap.claude...), windowStart, before),
			page(ToolCodex, append([]Task(nil), viewSnap.codex...), windowStart, before),
			page(ToolCursor, append([]Task(nil), viewSnap.cursor...), windowStart, before),
		},
	}
}

func scanKey(cfg config.ClientsConfig) string {
	return cfg.ClaudeDir + "\x00" + cfg.CodexDir + "\x00" + cfg.CursorDir
}

type viewState struct {
	key                   string
	at                    time.Time
	claude, codex, cursor []Task
}

var (
	viewMu   sync.Mutex
	viewSnap viewState
	viewBusy bool
	// viewWalk is the disk walk. Tests park it so a reader can observe
	// that View returned while the walk was still running.
	viewWalk = walkDirs
)

func walkDirs(cfg config.ClientsConfig, now time.Time) (claude, codex, cursor []Task) {
	stale := cfg.RunningStale()
	return scanClaude(cfg.ClaudeDir, now, stale),
		scanCodex(cfg.CodexDir, now, stale),
		scanCursor(cfg.CursorDir, now, stale)
}

func kick(cfg config.ClientsConfig, now time.Time, key string) {
	viewMu.Lock()
	defer viewMu.Unlock()
	if viewBusy {
		return
	}
	if viewSnap.key == key && !viewSnap.at.IsZero() &&
		now.Sub(viewSnap.at) < scanFresh && !now.Before(viewSnap.at) {
		return
	}
	viewBusy = true
	go func() {
		claude, codex, cursor := viewWalk(cfg, now)
		viewMu.Lock()
		viewSnap = viewState{key: key, at: time.Now(), claude: claude, codex: codex, cursor: cursor}
		viewBusy = false
		viewMu.Unlock()
	}()
}
