package skills

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxSkillBundleEntries = 512
	maxSkillBundleBytes   = 32 << 20
	maxSkillEntryPath     = 1024
	maxSkillEntryDepth    = 32
)

type skillBundleEntry struct {
	file *zip.File
	path string
	dir  bool
}

func skillBundleEntries(archive *zip.Reader) ([]skillBundleEntry, error) {
	if archive == nil || len(archive.File) == 0 || len(archive.File) > maxSkillBundleEntries {
		return nil, fmt.Errorf("%w: ZIP must contain 1 to %d entries", ErrInvalidSkillBundle, maxSkillBundleEntries)
	}
	var total uint64
	paths := make(map[string]bundlePath, len(archive.File))
	entries := make([]skillBundleEntry, 0, len(archive.File))
	var manifest string
	for _, file := range archive.File {
		name := strings.TrimSuffix(file.Name, "/")
		mode := file.Mode().Type()
		dir := mode == fs.ModeDir && strings.HasSuffix(file.Name, "/")
		if !dir && (mode != 0 || strings.HasSuffix(file.Name, "/")) {
			return nil, fmt.Errorf("%w: ZIP entries must be regular files or directories", ErrInvalidSkillBundle)
		}
		if err := checkBundlePath(name, dir, paths); err != nil {
			return nil, err
		}
		if file.UncompressedSize64 > maxSkillBundleBytes-total {
			return nil, fmt.Errorf("%w: ZIP exceeds %d bytes uncompressed", ErrInvalidSkillBundle, maxSkillBundleBytes)
		}
		total += file.UncompressedSize64

		top, _, _ := strings.Cut(name, "/")
		if top == "__MACOSX" || name == ".DS_Store" {
			continue
		}
		if !dir && path.Base(name) == skillFile && strings.Count(name, "/") <= 1 {
			if manifest != "" {
				return nil, fmt.Errorf("%w: ZIP contains more than one SKILL.md", ErrInvalidSkillBundle)
			}
			manifest = name
		}
		entries = append(entries, skillBundleEntry{file: file, path: name, dir: dir})
	}
	if manifest == "" {
		return nil, fmt.Errorf("%w: SKILL.md is required at the ZIP root or inside one wrapper", ErrInvalidSkillBundle)
	}
	prefix := path.Dir(manifest)
	if prefix == "." {
		return entries, nil
	}
	var stripped []skillBundleEntry
	for _, entry := range entries {
		if entry.dir && entry.path == prefix {
			continue
		}
		name, ok := strings.CutPrefix(entry.path, prefix+"/")
		if !ok {
			return nil, fmt.Errorf("%w: ZIP must contain just one skill directory", ErrInvalidSkillBundle)
		}
		entry.path = name
		stripped = append(stripped, entry)
	}
	return stripped, nil
}

type bundlePath struct {
	name     string
	dir      bool
	explicit bool
}

func checkBundlePath(name string, dir bool, paths map[string]bundlePath) error {
	if len(name) > maxSkillEntryPath || strings.Count(name, "/") >= maxSkillEntryDepth ||
		name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, `\:`) ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: invalid ZIP entry path", ErrInvalidSkillBundle)
	}
	explicit := true
	for candidate := name; candidate != "."; candidate = path.Dir(candidate) {
		base := path.Base(candidate)
		if _, err := filepath.Localize(base); err != nil || strings.TrimRight(base, ". ") != base {
			return fmt.Errorf("%w: invalid ZIP entry name %q", ErrInvalidSkillBundle, base)
		}
		key := strings.ToLower(candidate)
		if previous, found := paths[key]; found {
			if candidate != previous.name || dir != previous.dir || explicit && previous.explicit {
				return fmt.Errorf("%w: colliding ZIP entry path %q", ErrInvalidSkillBundle, candidate)
			}
			explicit = explicit || previous.explicit
		}
		paths[key] = bundlePath{name: candidate, dir: dir, explicit: explicit}
		dir, explicit = true, false
	}
	return nil
}

func extractSkillBundle(ctx context.Context, stage string, entries []skillBundleEntry) error {
	// Root-relative extraction cannot recreate deleted project ancestors.
	root, err := os.OpenRoot(stage)
	if err != nil {
		return fmt.Errorf("skills: open bundle staging directory: %w", err)
	}
	defer root.Close()
	for _, entry := range entries {
		if err := entry.extract(ctx, root); err != nil {
			return err
		}
	}
	return nil
}

func (e skillBundleEntry) extract(ctx context.Context, stage *os.Root) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	destination := filepath.FromSlash(e.path)
	if e.dir {
		return stage.MkdirAll(destination, dirPerm)
	}
	if e.path == skillFile && e.file.UncompressedSize64 > maxSkillManifestBytes {
		return fmt.Errorf("%w: SKILL.md exceeds %d bytes", ErrInvalidSkillBundle, maxSkillManifestBytes)
	}
	if err := stage.MkdirAll(filepath.Dir(destination), dirPerm); err != nil {
		return fmt.Errorf("skills: stage resource directory: %w", err)
	}
	in, err := e.file.Open()
	if err != nil {
		return fmt.Errorf("%w: open ZIP entry %q: %v", ErrInvalidSkillBundle, e.path, err)
	}
	defer in.Close()
	mode := fs.FileMode(filePerm)
	if e.file.Mode().Perm()&0o111 != 0 {
		mode |= 0o100
	}
	out, err := stage.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: colliding ZIP entry path %q", ErrInvalidSkillBundle, e.path)
	}
	if err != nil {
		return fmt.Errorf("skills: stage resource %q: %w", e.path, err)
	}
	reader := skillZIPReader{ctx: ctx, reader: in}
	copied, copyErr := io.Copy(out, io.LimitReader(reader, int64(e.file.UncompressedSize64)+1))
	if err := errors.Join(copyErr, out.Close()); err != nil {
		return fmt.Errorf("skills: stage resource %q: %w", e.path, err)
	}
	if copied != int64(e.file.UncompressedSize64) {
		return fmt.Errorf("%w: ZIP entry size mismatch for %q", ErrInvalidSkillBundle, e.path)
	}
	return nil
}

type skillZIPReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r skillZIPReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("%w: decode ZIP entry: %v", ErrInvalidSkillBundle, err)
	}
	return n, err
}
