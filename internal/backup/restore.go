package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// beforeFileSwap is nil in production. A test sets it to fail after the
// database commit and before the files move, which is the same rollback a
// caller sees when the restore cannot finish.
var beforeFileSwap func() error

// Preview reads an archive and reports conflicts. It does not create a
// database that was not already there, and it does not replace any file.
func Preview(ctx context.Context, dataDir string, db *sql.DB, path string) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return Report{}, err
	}
	arc, err := openArchive(path)
	if err != nil {
		return Report{}, err
	}
	defer arc.close()
	out := Report{Manifest: arc.manifest, Conflicts: []Conflict{}, Action: ActionReplaceAll}
	if arc.manifest.Kind == KindFull {
		return out, nil
	}
	conn, cleanup, err := acquireExisting(ctx, dataDir, db)
	if err != nil {
		return Report{}, err
	}
	if conn == nil {
		out.Action = ActionAddProject
		return out, nil
	}
	defer cleanup()
	exists, conflicts, err := inspectProject(ctx, conn, arc.dbPath, arc.manifest.ProjectID)
	if err != nil {
		return Report{}, err
	}
	out.Conflicts = conflicts
	switch {
	case len(conflicts) > 0:
		out.Action = ActionBlocked
	case exists:
		out.Action = ActionReplaceProject
	default:
		out.Action = ActionAddProject
	}
	return out, nil
}

// Restore replaces the whole data directory or adds/replaces one project.
// Any failure after the database changes puts that database and the copied
// files back to the snapshot taken at the start.
func Restore(ctx context.Context, opt RestoreOptions) (manifest Manifest, err error) {
	if err = ctx.Err(); err != nil {
		return Manifest{}, err
	}
	opt.DataDir, err = filepath.Abs(opt.DataDir)
	if err != nil {
		return Manifest{}, err
	}
	arc, err := openArchive(opt.Path)
	if err != nil {
		return Manifest{}, err
	}
	defer arc.close()
	if !opt.Confirm {
		return arc.manifest, ErrConfirm
	}
	if arc.manifest.Kind == KindFull && !opt.Replace {
		return arc.manifest, ErrConfirm
	}
	conn, cleanup, err := acquire(ctx, opt.DataDir, opt.DB)
	if err != nil {
		return arc.manifest, err
	}
	defer cleanup()
	if err = checkpoint(ctx, conn); err != nil {
		return arc.manifest, err
	}
	if arc.manifest.Kind == KindFull {
		err = restoreFull(ctx, conn, opt.DataDir, arc)
	} else {
		err = restoreOneProject(ctx, conn, opt.DataDir, arc, opt.ReplaceProject)
	}
	if err != nil {
		return arc.manifest, err
	}
	return arc.manifest, nil
}

func restoreFull(ctx context.Context, conn *sql.Conn, dataDir string, arc *openedArchive) (err error) {
	snapDB, err := tempFile(dataDir, ".backup-vacuum-*")
	if err != nil {
		return err
	}
	defer os.Remove(snapDB)
	if err = vacuumInto(ctx, conn, snapDB); err != nil {
		return err
	}
	rels := append([]string{}, fullTrees...)
	if arc.manifest.IncludesSecrets {
		rels = append(rels, "remote")
	}
	files, err := snapshotPaths(dataDir, rels)
	if err != nil {
		return err
	}
	defer files.remove()
	var mutated, swapped bool
	defer func() {
		if err == nil {
			return
		}
		undo := context.Background()
		if swapped {
			if rerr := files.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
		if mutated {
			if rerr := replaceAll(undo, conn, snapDB); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
	}()
	if err = replaceAll(ctx, conn, arc.dbPath); err != nil {
		return err
	}
	mutated = true
	if beforeFileSwap != nil {
		if err = beforeFileSwap(); err != nil {
			return err
		}
	}
	swapped = true
	if err = files.apply(arc.filesDir, arc.config); err != nil {
		return err
	}
	if !arc.manifest.IncludesSecrets {
		if err = keepDestinationSecrets(dataDir, files.config); err != nil {
			return err
		}
	}
	_, err = AdvanceGeneration(dataDir)
	return err
}

func restoreOneProject(ctx context.Context, conn *sql.Conn, dataDir string, arc *openedArchive, replace bool) (err error) {
	projectID := arc.manifest.ProjectID
	liveThreads, err := listThreads(ctx, conn, "main", projectID)
	if err != nil {
		return err
	}
	backupThreads, err := threadsIn(ctx, conn, arc.dbPath, projectID)
	if err != nil {
		return err
	}
	rels, err := projectRels(projectID, unionIDs(liveThreads, backupThreads))
	if err != nil {
		return err
	}
	snapDB, err := tempFile(dataDir, ".backup-project-*")
	if err != nil {
		return err
	}
	defer os.Remove(snapDB)
	if err = exportProjectRows(ctx, conn, projectID, snapDB, true); err != nil {
		return err
	}
	files, err := snapshotPaths(dataDir, rels)
	if err != nil {
		return err
	}
	defer files.remove()
	var mutated, swapped bool
	defer func() {
		if err == nil {
			return
		}
		undo := context.Background()
		if swapped {
			if rerr := files.restore(); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
		if mutated {
			if rerr := restoreProjectSnapshot(undo, conn, snapDB, projectID); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
	}()
	if err = applyProject(ctx, conn, arc.dbPath, projectID, replace, true); err != nil {
		return err
	}
	mutated = true
	if beforeFileSwap != nil {
		if err = beforeFileSwap(); err != nil {
			return err
		}
	}
	swapped = true
	if err = files.apply(arc.filesDir, nil); err != nil {
		return err
	}
	_, err = AdvanceGeneration(dataDir)
	return err
}

func keepDestinationSecrets(dataDir, snapConfig string) error {
	if err := failNext(); err != nil {
		return err
	}
	// A destination that has never saved a config has no credentials to put
	// back. An empty snapshot is not an error.
	if snapConfig == "" {
		return nil
	}
	previous, err := readConfigFile(snapConfig)
	if err != nil {
		return err
	}
	current, err := config.Load(dataDir)
	if err != nil {
		return err
	}
	overlayKeptSecrets(current, &previous)
	return current.Save()
}

func acquireExisting(ctx context.Context, dataDir string, db *sql.DB) (*sql.Conn, func(), error) {
	if db == nil {
		if _, err := os.Stat(filepath.Join(dataDir, "zwai.db")); os.IsNotExist(err) {
			return nil, nil, nil
		} else if err != nil {
			return nil, nil, err
		}
	}
	return acquire(ctx, dataDir, db)
}

func inspectProject(ctx context.Context, conn *sql.Conn, dbPath, projectID string) (bool, []Conflict, error) {
	if err := prepareExportDB(dbPath); err != nil {
		return false, nil, err
	}
	if err := attach(ctx, conn, "incoming", dbPath); err != nil {
		return false, nil, err
	}
	defer detach(context.Background(), conn, "incoming")
	exists, err := projectExists(ctx, conn, projectID)
	if err != nil {
		return false, nil, err
	}
	// Passing the existing project as the replace owner hides rows that a
	// replace would delete. Foreign identities stay in the list either way.
	conflicts, err := findConflicts(ctx, conn, projectID, exists)
	if err != nil {
		return false, nil, err
	}
	if conflicts == nil {
		conflicts = []Conflict{}
	}
	return exists, conflicts, nil
}

func threadsIn(ctx context.Context, conn *sql.Conn, dbPath, projectID string) ([]string, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	if err := prepareExportDB(dbPath); err != nil {
		return nil, err
	}
	if err := attach(ctx, conn, "incoming", dbPath); err != nil {
		return nil, err
	}
	defer detach(context.Background(), conn, "incoming")
	return listThreads(ctx, conn, "incoming", projectID)
}

func unionIDs(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for _, id := range append(append([]string{}, a...), b...) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
