package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Create writes one backup. It does not change the data generation: an archive
// is a copy, and opening it later must not rewind the apps that are watching.
func Create(ctx context.Context, opt Options) (Manifest, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	dataDir, err := filepath.Abs(opt.DataDir)
	if err != nil {
		return Manifest{}, err
	}
	output, err := filepath.Abs(opt.Output)
	if err != nil {
		return Manifest{}, err
	}
	if opt.Kind != KindFull && opt.Kind != KindProject {
		return Manifest{}, invalidf("kind")
	}
	if opt.Kind == KindProject && !validID(opt.ProjectID) {
		return Manifest{}, invalidf("project id")
	}
	if err := outsideDataDir(dataDir, output); err != nil {
		return Manifest{}, err
	}
	conn, cleanup, err := acquire(ctx, dataDir, opt.DB)
	if err != nil {
		return Manifest{}, err
	}
	defer cleanup()
	if err := checkpoint(ctx, conn); err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	var items []zipItem
	var removeTemp func()
	if opt.Kind == KindFull {
		manifest, items, removeTemp, err = buildFull(ctx, conn, dataDir, opt.OmitSecrets)
	} else {
		manifest, items, removeTemp, err = buildProject(ctx, conn, dataDir, opt.ProjectID)
	}
	if removeTemp != nil {
		defer removeTemp()
	}
	if err != nil {
		return Manifest{}, err
	}
	manifest.Format = FormatVersion
	manifest.Kind = opt.Kind
	manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	written, err := writeArchive(output, items, manifest)
	if err != nil {
		return Manifest{}, err
	}
	return written, nil
}

func buildFull(ctx context.Context, conn *sql.Conn, dataDir string, omit bool) (Manifest, []zipItem, func(), error) {
	db, err := tempFile(dataDir, ".backup-vacuum-*")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	cleanup := func() { _ = os.Remove(db) }
	if err := vacuumInto(ctx, conn, db); err != nil {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	if err := attach(ctx, conn, "snap", db); err != nil {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	counts, err := countSchema(ctx, conn, "snap")
	if err != nil {
		detach(context.Background(), conn, "snap")
		cleanup()
		return Manifest{}, nil, nil, err
	}
	external, err := listExternal(ctx, conn, "snap", "")
	detach(context.Background(), conn, "snap")
	if err != nil {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	cfg, err := config.Load(dataDir)
	if err != nil {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	raw, err := marshalConfig(cfg, omit)
	if err != nil {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	items := []zipItem{{name: databaseName, path: db}, {name: config.FileName, body: raw}}
	for _, rel := range fullTrees {
		part, err := collectTree(dataDir, rel)
		if err != nil {
			cleanup()
			return Manifest{}, nil, nil, err
		}
		items = append(items, part...)
	}
	if !omit {
		part, err := collectTree(dataDir, "remote")
		if err != nil {
			cleanup()
			return Manifest{}, nil, nil, err
		}
		items = append(items, part...)
	}
	return Manifest{IncludesSecrets: !omit, ExternalWorkdirs: external, Counts: counts}, items, cleanup, nil
}

func buildProject(ctx context.Context, conn *sql.Conn, dataDir, projectID string) (Manifest, []zipItem, func(), error) {
	db, err := tempFile(dataDir, ".backup-project-*")
	if err != nil {
		return Manifest{}, nil, nil, err
	}
	cleanup := func() { _ = os.Remove(db) }
	fail := func(err error) (Manifest, []zipItem, func(), error) {
		cleanup()
		return Manifest{}, nil, nil, err
	}
	if err := exportProject(ctx, conn, projectID, db); err != nil {
		return fail(err)
	}
	if err := attach(ctx, conn, "dst", db); err != nil {
		return fail(err)
	}
	defer detach(context.Background(), conn, "dst")
	counts, err := countSchema(ctx, conn, "dst")
	if err != nil {
		return fail(err)
	}
	external, err := listExternal(ctx, conn, "dst", projectID)
	if err != nil {
		return fail(err)
	}
	threads, err := listThreads(ctx, conn, "dst", projectID)
	if err != nil {
		return fail(err)
	}
	rels, err := projectRels(projectID, threads)
	if err != nil {
		return fail(err)
	}
	items := []zipItem{{name: databaseName, path: db}}
	for _, rel := range rels {
		part, err := collectTree(dataDir, rel)
		if err != nil {
			return fail(err)
		}
		items = append(items, part...)
	}
	return Manifest{ProjectID: projectID, IncludesSecrets: false, ExternalWorkdirs: external, Counts: counts}, items, cleanup, nil
}

func tempFile(dir, pattern string) (string, error) {
	if err := failNext(); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func acquire(ctx context.Context, dataDir string, db *sql.DB) (*sql.Conn, func(), error) {
	var closeStore func()
	if db == nil {
		st, err := store.Open(filepath.Join(dataDir, "zwai.db"))
		if err != nil {
			return nil, nil, err
		}
		sqlDB, err := st.DB().DB()
		if err != nil {
			_ = st.Close()
			return nil, nil, err
		}
		db = sqlDB
		closeStore = func() { _ = st.Close() }
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		if closeStore != nil {
			closeStore()
		}
		return nil, nil, err
	}
	return conn, func() {
		_ = conn.Close()
		if closeStore != nil {
			closeStore()
		}
	}, nil
}
