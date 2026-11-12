package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestBackupRoundTripRestoresTheProjectName(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngineAt(t, dir)
	project, err := e.CreateProject("kept", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	if _, err := e.BackupData(context.Background(), backup.Options{Kind: backup.KindFull, Output: out}); err != nil {
		t.Fatal(err)
	}
	changed := "changed"
	if _, err := e.UpdateProject(project.ID, &changed, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RestoreBackup(context.Background(), backup.RestoreOptions{Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "kept" {
		t.Fatalf("name = %q", got.Name)
	}
	gen, err := e.DataGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if gen != 1 {
		t.Fatalf("generation = %d", gen)
	}
}

func TestProjectBackupJoinsAnotherDataDirectory(t *testing.T) {
	source := newTestEngineAt(t, t.TempDir())
	project, err := source.CreateProject("carried", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	if _, err := source.BackupData(context.Background(), backup.Options{Kind: backup.KindProject, ProjectID: project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	dest := newTestEngineAt(t, t.TempDir())
	if _, err := dest.CreateProject("already", "", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.RestoreBackup(context.Background(), backup.RestoreOptions{Path: out, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Store().GetProject(project.ID); err != nil {
		t.Fatal(err)
	}
	projects, err := dest.Store().ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %d", len(projects))
	}
	gen, err := dest.DataGeneration()
	if err != nil || gen != 1 {
		t.Fatalf("generation = %d err=%v", gen, err)
	}
}

func TestBackupRefusesWhileATurnIsRunning(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngineAt(t, dir)
	marker := filepath.Join(dir, "zwai.db")
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor("th_busy")
	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()
	out := filepath.Join(t.TempDir(), "busy.zwai-backup")
	_, err = e.BackupData(context.Background(), backup.Options{Kind: backup.KindFull, Output: out})
	if !errors.Is(err, ErrBackupBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatal("backup wrote a file while a turn was running")
	}
	after, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("database bytes changed while backup was refused")
	}
	if _, err := e.RestoreBackup(context.Background(), backup.RestoreOptions{Path: out, Confirm: true, Replace: true}); !errors.Is(err, ErrBackupBusy) {
		t.Fatalf("restore err = %v", err)
	}
}

func TestBackupHoldBlocksNewWorkAndPausesTheScheduler(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	e.cfg.Swarm.ScheduleTickMS = 60_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.mu.Lock()
		busy := e.restart.admissions > 0
		e.mu.Unlock()
		e.schedMu.Lock()
		started := e.schedStop != nil
		e.schedMu.Unlock()
		if started && !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not settle")
		}
		time.Sleep(5 * time.Millisecond)
	}
	release, _, err := e.holdBackup(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.beginLifecycleAdmission(); !errors.Is(err, ErrBackupBusy) {
		t.Fatalf("admission err = %v", err)
	}
	e.schedMu.Lock()
	paused := e.schedStop == nil
	e.schedMu.Unlock()
	if !paused {
		t.Fatal("scheduler kept ticking during backup")
	}
	release()
	e.schedMu.Lock()
	resumed := e.schedStop != nil
	e.schedMu.Unlock()
	if !resumed {
		t.Fatal("scheduler stayed stopped after backup")
	}
	done, err := e.beginLifecycleAdmission()
	if err != nil {
		t.Fatal(err)
	}
	done()
}

func TestBackupCanLeaveOutARunningConversation(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	kept, err := e.CreateThread("kept", "", "")
	if err != nil {
		t.Fatal(err)
	}
	busyThread, err := e.CreateThread("busy conversation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(e.cfg.WorkspaceDir(busyThread.ID), "note.txt")
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("still-here"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(busyThread.ID)
	rt.mu.Lock()
	rt.running = true
	rt.ask = &pendingAsk{}
	rt.mu.Unlock()

	out := filepath.Join(t.TempDir(), "skip.zwai-backup")
	_, err = e.BackupData(context.Background(), backup.Options{Kind: backup.KindFull, Output: out})
	var busy *BackupBusy
	if !errors.As(err, &busy) || busy == nil || !busy.Skippable || len(busy.Active) != 1 {
		t.Fatalf("busy = %+v err=%v", busy, err)
	}
	if busy.Active[0].ID != busyThread.ID || busy.Active[0].Title != "busy conversation" || busy.Active[0].Reason != backup.ReasonAsking {
		t.Fatalf("active = %+v", busy.Active[0])
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatal("refused backup wrote a file")
	}

	manifest, err := e.BackupData(context.Background(), backup.Options{Kind: backup.KindFull, Output: out, SkipActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.Threads != 1 || len(manifest.OmittedThreads) != 1 || manifest.OmittedThreads[0].ID != busyThread.ID {
		t.Fatalf("manifest %+v", manifest)
	}
	if _, err := e.Store().GetThread(busyThread.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "still-here" {
		t.Fatalf("live workspace = %q err=%v", got, err)
	}
	// Leaving the conversation out of the file must not answer it or stop it.
	if !rt.status().Running || rt.status().AwaitingAnswer != true {
		t.Fatal("skipping the backup stopped the conversation")
	}

	dest := newTestEngineAt(t, t.TempDir())
	if _, err := dest.RestoreBackup(context.Background(), backup.RestoreOptions{Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Store().GetThread(busyThread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("omitted thread restored: %v", err)
	}
	if _, err := dest.Store().GetThread(kept.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest.Config().WorkspaceDir(busyThread.ID), "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("omitted workspace restored: %v", err)
	}
}

func TestBackupDoesNotSkipARestart(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	rt := e.runtimeFor("th_busy")
	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()
	e.mu.Lock()
	e.restart.freezing = true
	e.mu.Unlock()
	out := filepath.Join(t.TempDir(), "restart.zwai-backup")
	_, err := e.BackupData(context.Background(), backup.Options{Kind: backup.KindFull, Output: out, SkipActive: true})
	var busy *BackupBusy
	if !errors.As(err, &busy) || busy == nil || busy.Skippable {
		t.Fatalf("err = %#v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatal("backup wrote a file during restart")
	}
}
