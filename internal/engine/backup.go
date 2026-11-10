package engine

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

// ErrBackupBusy means a turn, a schedule fire, a restart, or another backup
// still owns the data directory. The caller retries after that work finishes;
// nothing was written or replaced.
var ErrBackupBusy = errors.New("engine: a backup or restore is already running, or a task is still active")

// RejectIfBackup refuses a mutation while a backup or restore holds the
// data directory. Startup recovery has its own error.
func (e *Engine) RejectIfBackup() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.backupRunning {
		return ErrBackupBusy
	}
	return nil
}

// DataGeneration is the counter clients watch. It moves only after a restore
// commits, and a missing file is zero.
func (e *Engine) DataGeneration() (int64, error) {
	return backup.Generation(e.cfg.DataDir())
}

// holdBackup is the exclusive section for writing or replacing data.
// The scheduler is paused only when it was already running, and it stays
// paused until release so parked-worker recovery finishes first.
func (e *Engine) holdBackup() (func(), error) {
	if e.ActiveWork() > 0 {
		return nil, ErrBackupBusy
	}
	if err := e.RequireReady(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.backupRunning || e.restart.freezing || e.startupRestoring {
		e.mu.Unlock()
		return nil, ErrBackupBusy
	}
	// New turns fail at beginLifecycleAdmission. An admission that already
	// entered is only listing or starting work, so wait for that call to
	// leave and then look again. A schedule tick is not a task.
	e.backupRunning = true
	e.mu.Unlock()
	if err := e.waitBackupAdmissions(); err != nil {
		e.finishBackup(false)
		return nil, err
	}
	if e.ActiveWork() > 0 {
		e.finishBackup(false)
		return nil, ErrBackupBusy
	}

	e.schedMu.Lock()
	wasRunning := e.schedStop != nil
	e.schedMu.Unlock()
	if wasRunning {
		e.StopScheduler()
	}
	return func() { e.finishBackup(wasRunning) }, nil
}

func (e *Engine) waitBackupAdmissions() error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.mu.Lock()
		pending := e.restart.admissions
		e.mu.Unlock()
		if pending == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrBackupBusy
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *Engine) finishBackup(resumeScheduler bool) {
	e.mu.Lock()
	e.backupRunning = false
	e.mu.Unlock()
	if resumeScheduler {
		e.StartScheduler()
	}
}

func (e *Engine) openPool() (*sql.DB, error) {
	if e.store == nil || e.store.DB() == nil {
		return nil, errors.New("engine: no database")
	}
	return e.store.DB().DB()
}

// BackupData writes one archive through the engine's open database.
func (e *Engine) BackupData(ctx context.Context, opt backup.Options) (backup.Manifest, error) {
	release, err := e.holdBackup()
	if err != nil {
		return backup.Manifest{}, err
	}
	defer release()
	db, err := e.openPool()
	if err != nil {
		return backup.Manifest{}, err
	}
	opt.DataDir = e.cfg.DataDir()
	opt.DB = db
	return backup.Create(ctx, opt)
}

// PreviewBackup reports what a restore would do. It does not take the
// exclusive hold and it does not change files.
func (e *Engine) PreviewBackup(ctx context.Context, path string) (backup.Report, error) {
	if err := e.RejectIfBackup(); err != nil {
		return backup.Report{}, err
	}
	if err := e.RequireReady(); err != nil {
		return backup.Report{}, err
	}
	db, err := e.openPool()
	if err != nil {
		return backup.Report{}, err
	}
	return backup.Preview(ctx, e.cfg.DataDir(), db, path)
}

// RestoreBackup replaces all data or adds/replaces one project, then reloads
// what the process cached from the old files. A parked-worker recovery that
// cannot resume is logged; the restored data stays.
func (e *Engine) RestoreBackup(ctx context.Context, opt backup.RestoreOptions) (backup.Manifest, error) {
	release, err := e.holdBackup()
	if err != nil {
		return backup.Manifest{}, err
	}
	defer release()
	db, err := e.openPool()
	if err != nil {
		return backup.Manifest{}, err
	}
	opt.DataDir = e.cfg.DataDir()
	opt.DB = db
	manifest, err := backup.Restore(ctx, opt)
	if err != nil {
		return manifest, err
	}
	e.reloadAfterRestore(ctx, manifest)
	return manifest, nil
}

func (e *Engine) reloadAfterRestore(ctx context.Context, manifest backup.Manifest) {
	e.configUpdateMu.Lock()
	loaded, err := config.Load(e.cfg.DataDir())
	if err != nil {
		e.log.Warn("could not reload configuration after restore", "err", err)
	} else if err := e.cfg.Replace(loaded); err != nil {
		e.log.Warn("could not apply restored configuration", "err", err)
	}
	if e.mcp != nil {
		e.mcp.Reload(e.cfg.Snapshot().MCP.Servers)
	}
	if e.pool != nil {
		e.pool.Invalidate()
	}
	e.configUpdateMu.Unlock()
	e.ApplyLiveSwarmLimits()
	e.forgetCachedMemory()
	e.closeAllRuntimes()
	if e.store != nil {
		e.store.ResetSequences()
		e.rebuildRestoredSearch(manifest)
	}
	if err := e.Restore(ctx, nil); err != nil {
		e.log.Warn("parked workers were not resumed after restore", "err", err)
	}
}

func (e *Engine) rebuildRestoredSearch(manifest backup.Manifest) {
	if manifest.Kind == backup.KindFull {
		if err := e.store.RebuildSearch(); err != nil {
			e.log.Warn("could not rebuild search after restore", "err", err)
		}
		return
	}
	if manifest.ProjectID == "" {
		return
	}
	threads, err := e.store.ListThreads(true, manifest.ProjectID)
	if err != nil {
		e.log.Warn("could not list restored conversations", "err", err)
		return
	}
	for _, th := range threads {
		if err := e.store.IndexThread(th.ID); err != nil {
			e.log.Warn("could not index a restored conversation", "thread", th.ID, "err", err)
		}
	}
}

func (e *Engine) forgetCachedMemory() {
	e.memory.mu.Lock()
	defer e.memory.mu.Unlock()
	e.memory.stores = map[string]*memory.Store{}
	for _, catalog := range e.memory.external {
		if catalog != nil {
			catalog.Invalidate()
		}
	}
	e.memory.external = nil
}

func (e *Engine) closeAllRuntimes() {
	e.mu.Lock()
	rts := make([]*runtime, 0, len(e.runtimes))
	for id, rt := range e.runtimes {
		rts = append(rts, rt)
		delete(e.runtimes, id)
	}
	e.mu.Unlock()
	for _, rt := range rts {
		rt.close()
	}
}
