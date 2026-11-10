package backup

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// fullTrees are the directories a full backup copies. The data directory also
// holds the engine lease, logs, diagnostics and this process's generation
// counter; those stay put so a restore cannot rewind them or drop the lease.
var fullTrees = []string{"inputs", "plans", "workspaces", "library", "projects"}

func collectTree(dataDir, rel string) ([]zipItem, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, filepath.FromSlash(rel))
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, invalidf("symlink %s", rel)
	}
	var items []zipItem
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return invalidf("symlink %s", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return invalidf("unsupported file %s", path)
		}
		itemRel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		itemRel = filepath.ToSlash(itemRel)
		if itemRel == "." || strings.HasPrefix(itemRel, "../") {
			return invalidf("path %s", itemRel)
		}
		name := filesPrefix + itemRel
		if info.IsDir() {
			items = append(items, zipItem{name: name, dir: true})
			return nil
		}
		items = append(items, zipItem{name: name, path: path})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

type fileSnap struct {
	dataDir string
	rels    []string
	root    string
	config  string
}

func snapshotPaths(dataDir string, rels []string) (*fileSnap, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(dataDir, ".backup-snap-*")
	if err != nil {
		return nil, err
	}
	snap := &fileSnap{dataDir: dataDir, rels: rels, root: root}
	for _, rel := range rels {
		src := filepath.Join(dataDir, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			snap.remove()
			return nil, err
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := copyTree(src, dst); err != nil {
			snap.remove()
			return nil, err
		}
	}
	configPath := filepath.Join(dataDir, config.FileName)
	if _, err := os.Lstat(configPath); err == nil {
		snap.config = filepath.Join(root, config.FileName)
		if err := copyFile(configPath, snap.config, 0o600); err != nil {
			snap.remove()
			return nil, err
		}
	}
	return snap, nil
}

func (s *fileSnap) remove() {
	if s != nil && s.root != "" {
		_ = os.RemoveAll(s.root)
	}
}

func (s *fileSnap) apply(extracted string, configBytes []byte) error {
	if err := failNext(); err != nil {
		return err
	}
	for _, rel := range s.rels {
		live := filepath.Join(s.dataDir, filepath.FromSlash(rel))
		if err := os.RemoveAll(live); err != nil {
			return err
		}
		src := filepath.Join(extracted, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyTree(src, live); err != nil {
			return err
		}
	}
	if configBytes != nil {
		if err := os.WriteFile(filepath.Join(s.dataDir, config.FileName), configBytes, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s *fileSnap) restore() error {
	for _, rel := range s.rels {
		live := filepath.Join(s.dataDir, filepath.FromSlash(rel))
		if err := os.RemoveAll(live); err != nil {
			return err
		}
		src := filepath.Join(s.root, filepath.FromSlash(rel))
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyTree(src, live); err != nil {
			return err
		}
	}
	if s.config != "" {
		if err := copyFile(s.config, filepath.Join(s.dataDir, config.FileName), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return invalidf("unsupported file %s", src)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o600
	}
	return copyFile(src, dst, mode)
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func projectRels(projectID string, threads []string) ([]string, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	if !validID(projectID) {
		return nil, invalidf("project id")
	}
	rels := []string{filepath.ToSlash(filepath.Join("projects", projectID))}
	seen := map[string]struct{}{}
	for _, id := range threads {
		if !validID(id) {
			return nil, invalidf("thread id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		for _, parent := range []string{"inputs", "plans", "workspaces"} {
			rels = append(rels, filepath.ToSlash(filepath.Join(parent, id)))
		}
	}
	return rels, nil
}

func outsideDataDir(dataDir, output string) error {
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if inside(absData, absOut) {
		return invalidf("write the backup outside the data directory")
	}
	info, err := os.Lstat(absOut)
	if err == nil && info.IsDir() {
		return invalidf("backup output is a directory")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
