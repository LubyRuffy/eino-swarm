package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// errNotDirectory is a directory listing asked to read a file or a symlink.
var errNotDirectory = errors.New("not a directory")

// listDirectory reads one directory completely. rel is empty for the
// workspace root and is otherwise an OS path relative to it. Dot-directories,
// node_modules and vendor are omitted; every other direct child is returned.
// A symlink is not followed.
func listDirectory(workspace, rel string) ([]FileEntry, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	name := "."
	prefix := ""
	if rel != "" {
		name = rel
		prefix = filepath.ToSlash(rel) + "/"
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errNotDirectory
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ents, err := f.ReadDir(-1)
	if err != nil {
		// A short read is not a directory listing.
		return nil, err
	}
	out := make([]FileEntry, 0, len(ents))
	for _, ent := range ents {
		if ent.IsDir() && skipListedDir(ent.Name()) {
			continue
		}
		item := FileEntry{
			Path: prefix + ent.Name(),
			Name: ent.Name(),
			Dir:  ent.IsDir(),
		}
		if info, err := ent.Info(); err == nil {
			item.Size = info.Size()
			item.Modified = info.ModTime()
		}
		out = append(out, item)
	}
	return out, nil
}

// searchWorkspace returns every non-skipped entry whose slash path contains
// query, case-insensitively. Cancellation returns no partial result.
func searchWorkspace(ctx context.Context, workspace, query string) ([]FileEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	needle := strings.ToLower(query)
	out := make([]FileEntry, 0)
	err := filepath.WalkDir(workspace, func(abs string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if abs == workspace {
			return nil
		}
		if d.IsDir() && skipListedDir(d.Name()) {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(workspace, abs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(strings.ToLower(rel), needle) {
			return nil
		}
		item := FileEntry{Path: rel, Name: d.Name(), Dir: d.IsDir()}
		if info, err := d.Info(); err == nil {
			item.Size = info.Size()
			item.Modified = info.ModTime()
		}
		out = append(out, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func skipListedDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor":
		return true
	default:
		return false
	}
}

// ListFiles lists the immediate children of one workspace directory. An empty
// dir is the workspace root. The result is that directory's complete direct
// listing, not a sample of the tree.
func (e *Engine) ListFiles(threadID, dir string) ([]FileEntry, error) {
	root := e.WorkspaceDir(threadID)
	rel := ""
	if strings.TrimSpace(dir) != "" {
		name, err := workspaceRelName(dir)
		if err != nil {
			return nil, err
		}
		rel = name
	} else if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("engine: workspace: %w", err)
	}
	entries, err := listDirectory(root, rel)
	if err != nil {
		if errors.Is(err, errNotDirectory) {
			return nil, fmt.Errorf("engine: %q is not a directory", dir)
		}
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("engine: list %q: %w", dir, store.ErrNotFound)
		}
		return nil, fmt.Errorf("engine: list %q: %w", dir, err)
	}
	return entries, nil
}

// SearchFiles returns every listed workspace entry whose path contains query.
// It does not return a truncated sample: failure yields no rows.
func (e *Engine) SearchFiles(ctx context.Context, threadID, query string) ([]FileEntry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("engine: empty file search")
	}
	root := e.WorkspaceDir(threadID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("engine: workspace: %w", err)
	}
	entries, err := searchWorkspace(ctx, root, query)
	if err != nil {
		return nil, fmt.Errorf("engine: search workspace: %w", err)
	}
	return entries, nil
}
