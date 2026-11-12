package engine

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

// ErrBackupBusy means a turn, a schedule fire, a restart, or another backup
// still owns the data directory. Nothing was written or replaced.
// A backup can be retried with SkipActive when BackupBusy.Skippable is set:
// those conversations are left out of the archive and keep running.
// Restore, restart, and another transfer cannot be skipped.
var ErrBackupBusy = errors.New("engine: a backup or restore is already running, or a task is still active")

// BackupBusy is ErrBackupBusy plus the conversations a backup can leave out.
type BackupBusy struct {
	Active []backup.OmittedThread
	// Skippable is true only when the blocker is those conversations.
	// A restart, a restore, or another transfer leaves it false.
	Skippable bool
}

func (e *BackupBusy) Error() string { return ErrBackupBusy.Error() }

func (e *BackupBusy) Unwrap() error { return ErrBackupBusy }

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
// skipActive lets a backup proceed while conversations are still running.
// Those conversations are returned so the snapshot can leave them out.
// Restore passes false: a full restore would otherwise delete them.
func (e *Engine) holdBackup(skipActive bool) (func(), []backup.OmittedThread, error) {
	if err := e.RequireReady(); err != nil {
		return nil, nil, err
	}
	if busy, err := e.backupTransferBusy(); busy || err != nil {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, &BackupBusy{}
	}
	e.mu.Lock()
	held := e.backupRunning
	e.mu.Unlock()
	if held {
		return nil, nil, &BackupBusy{}
	}
	if active := e.backupActiveThreads(); len(active) > 0 && !skipActive {
		return nil, nil, &BackupBusy{Active: active, Skippable: true}
	}
	e.mu.Lock()
	if e.backupRunning || e.restart.freezing || e.startupRestoring || e.restart.status.Stage != "" {
		e.mu.Unlock()
		return nil, nil, &BackupBusy{}
	}
	// New turns fail at beginLifecycleAdmission. An admission that already
	// entered is only listing or starting work, so wait for that call to
	// leave and then look again. A schedule tick is not a task.
	e.backupRunning = true
	e.mu.Unlock()
	if err := e.waitBackupAdmissions(); err != nil {
		e.finishBackup(false)
		return nil, nil, err
	}
	if busy, err := e.backupTransferBusy(); busy || err != nil {
		e.finishBackup(false)
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, &BackupBusy{}
	}
	active := e.backupActiveThreads()
	if len(active) > 0 && !skipActive {
		e.finishBackup(false)
		return nil, nil, &BackupBusy{Active: active, Skippable: true}
	}

	e.schedMu.Lock()
	wasRunning := e.schedStop != nil
	e.schedMu.Unlock()
	if wasRunning {
		e.StopScheduler()
	}
	var omitted []backup.OmittedThread
	if skipActive {
		omitted = active
	}
	return func() { e.finishBackup(wasRunning) }, omitted, nil
}

// backupTransferBusy is a restart, a restore, or startup recovery.
// Those cannot be left out of a snapshot: the process is already replacing
// the data directory. backupRunning is not included; the caller holds it.
func (e *Engine) backupTransferBusy() (bool, error) {
	if err := e.RequireReady(); err != nil {
		return false, err
	}
	e.mu.Lock()
	busy := e.restart.freezing || e.startupRestoring || e.restart.status.Stage != ""
	e.mu.Unlock()
	return busy, nil
}

// backupActiveThreads is every conversation whose turn or workers are still
// in this process. A future wake is not one of them. The list is what a
// backup can leave out; it is not a reason to stop the conversation.
func (e *Engine) backupActiveThreads() []backup.OmittedThread {
	var out []backup.OmittedThread
	for _, rt := range e.snapshotRuntimes() {
		st := rt.status()
		if !st.Running && st.Workers == 0 {
			continue
		}
		reason := backup.ReasonRunning
		if st.AwaitingAnswer {
			reason = backup.ReasonAsking
		} else if !st.Running {
			reason = backup.ReasonWorkers
		}
		out = append(out, backup.OmittedThread{
			ID:     st.ThreadID,
			Title:  e.backupThreadTitle(st.ThreadID),
			Reason: reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (e *Engine) backupThreadTitle(id string) string {
	title := id
	if e.store != nil {
		if thread, err := e.store.GetThread(id); err == nil {
			title = strings.Join(strings.Fields(thread.Title), " ")
		}
	}
	if title == "" {
		return id
	}
	runes := []rune(title)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return title
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
	release, omitted, err := e.holdBackup(opt.SkipActive)
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
	// The caller names a choice, not a list of conversations. Only the
	// threads still running inside this hold are left out of the snapshot.
	if opt.SkipActive {
		opt.OmitThreads = omitted
	} else {
		opt.OmitThreads = nil
	}
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
	release, _, err := e.holdBackup(false)
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
