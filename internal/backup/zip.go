package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxCompressRatio = 200

// The caps are variables so a test can lower them. Production values reject a
// huge or highly compressed archive without using the skill bundle's size cap.
var (
	maxZipEntries   = 100000
	maxArchiveBytes = int64(64 << 30)
)

// zipBombMinUncompressed avoids treating a tiny, highly compressible file as
// an attack. Tests lower it; production stays at 1 MiB.
var zipBombMinUncompressed int64 = 1 << 20

func safeZipPath(name string) (string, error) {
	if name == "" || strings.Contains(name, `\`) || strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
		return "", invalidf("path %q", name)
	}
	if name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || strings.HasSuffix(name, "/..") {
		return "", invalidf("path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", invalidf("path %q", name)
	}
	return clean, nil
}

func validateHeaders(files []*zip.File) error {
	if len(files) == 0 || len(files) > maxZipEntries {
		return invalidf("entry count")
	}
	seen := make(map[string]struct{}, len(files))
	var total int64
	for _, f := range files {
		name, err := safeZipPath(f.Name)
		if err != nil {
			return err
		}
		if _, ok := seen[name]; ok {
			return invalidf("duplicate path %s", name)
		}
		seen[name] = struct{}{}
		if f.Mode()&os.ModeSymlink != 0 || f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return invalidf("symlink %s", name)
		}
		if !f.FileInfo().Mode().IsRegular() && !f.FileInfo().IsDir() {
			return invalidf("unsupported entry %s", name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		size := int64(f.UncompressedSize64)
		total += size
		if total < 0 || total > maxArchiveBytes {
			return invalidf("uncompressed backup is too large")
		}
		comp := int64(f.CompressedSize64)
		if size > zipBombMinUncompressed && (comp == 0 || size/comp > maxCompressRatio) {
			return invalidf("compressed entry %s is not a backup", name)
		}
	}
	return nil
}

type zipItem struct {
	name string
	path string
	body []byte
	dir  bool
}

// progressStride is how often a large file moves the archive bar. Small
// files still move it once, when that file has been copied.
const progressStride = 256 << 10

func writeArchive(ctx context.Context, dest string, items []zipItem, manifest Manifest) (Manifest, error) {
	if err := failNext(); err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Manifest{}, err
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Manifest{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)
	hashes := make(map[string]string, len(items))
	total := archiveBytes(items)
	var done int64
	reportProgress(ctx, Progress{Phase: PhaseArchive, Total: total})
	for _, item := range items {
		name, err := safeZipPath(item.name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Manifest{}, err
		}
		size := itemBytes(item)
		base := done
		sum, err := writeItem(zw, name, item, func(copied int64) {
			if copied < size {
				reportProgress(ctx, Progress{Phase: PhaseArchive, Done: base + copied, Total: total})
			}
		})
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Manifest{}, err
		}
		done = base + size
		reportProgress(ctx, Progress{Phase: PhaseArchive, Done: done, Total: total})
		if !item.dir {
			hashes[name] = sum
		}
	}
	manifest.SHA256 = hashes
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Manifest{}, err
	}
	if _, err := writeItem(zw, manifestName, zipItem{name: manifestName, body: raw}, nil); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Manifest{}, err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return Manifest{}, err
	}
	if err := f.Close(); err != nil {
		return Manifest{}, err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return Manifest{}, err
	}
	ok = true
	return manifest, nil
}

func archiveBytes(items []zipItem) int64 {
	var total int64
	for _, item := range items {
		total += itemBytes(item)
	}
	return total
}

func itemBytes(item zipItem) int64 {
	if item.dir {
		return 0
	}
	if item.path == "" {
		return int64(len(item.body))
	}
	info, err := os.Stat(item.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

type copyCounter struct {
	w    io.Writer
	n    int64
	next int64
	on   func(int64)
}

func (c *copyCounter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.n += int64(n)
		if c.on != nil && c.n >= c.next {
			c.on(c.n)
			c.next = c.n + progressStride
		}
	}
	return n, err
}

func writeItem(zw *zip.Writer, name string, item zipItem, onCopy func(int64)) (string, error) {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now().UTC()}
	if item.dir {
		if !strings.HasSuffix(h.Name, "/") {
			h.Name += "/"
		}
		h.Method = zip.Store
		h.SetMode(os.ModeDir | 0o700)
		_, err := zw.CreateHeader(h)
		return "", err
	}
	h.SetMode(0o600)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	var dst io.Writer = io.MultiWriter(w, hash)
	if onCopy != nil {
		dst = &copyCounter{w: dst, next: progressStride, on: onCopy}
	}
	if item.path != "" {
		in, err := os.Open(item.path)
		if err != nil {
			return "", err
		}
		defer in.Close()
		if _, err := io.Copy(dst, in); err != nil {
			return "", err
		}
	} else if _, err := dst.Write(item.body); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type openedArchive struct {
	manifest Manifest
	root     string
	dbPath   string
	config   []byte
	filesDir string
}

func (a *openedArchive) close() {
	if a != nil && a.root != "" {
		_ = os.RemoveAll(a.root)
	}
}

func openArchive(path string) (*openedArchive, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, invalidf("open archive")
	}
	defer r.Close()
	if err := validateHeaders(r.File); err != nil {
		return nil, err
	}
	var manifestFile *zip.File
	for _, f := range r.File {
		name, err := safeZipPath(f.Name)
		if err != nil {
			return nil, err
		}
		if name == manifestName {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return nil, invalidf("missing manifest")
	}
	raw, err := readZipFile(manifestFile)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, invalidf("manifest")
	}
	if manifest.Format != FormatVersion || (manifest.Kind != KindFull && manifest.Kind != KindProject) {
		return nil, invalidf("manifest")
	}
	if manifest.Kind == KindProject && !validID(manifest.ProjectID) {
		return nil, invalidf("project id")
	}
	root, err := os.MkdirTemp("", "zwai-backup-*")
	if err != nil {
		return nil, err
	}
	arc := &openedArchive{manifest: manifest, root: root, filesDir: filepath.Join(root, "files")}
	if err := os.MkdirAll(arc.filesDir, 0o700); err != nil {
		arc.close()
		return nil, err
	}
	seen := map[string]struct{}{}
	var total int64
	for _, f := range r.File {
		name, err := safeZipPath(f.Name)
		if err != nil {
			arc.close()
			return nil, err
		}
		if name == manifestName || f.FileInfo().IsDir() {
			if f.FileInfo().IsDir() && strings.HasPrefix(name, filesPrefix) {
				rel := strings.TrimPrefix(name, filesPrefix)
				if err := extractRel(arc.filesDir, rel, nil, true); err != nil {
					arc.close()
					return nil, err
				}
			}
			continue
		}
		body, err := readZipFile(f)
		if err != nil {
			arc.close()
			return nil, err
		}
		total += int64(len(body))
		if total < 0 || total > maxArchiveBytes {
			arc.close()
			return nil, invalidf("uncompressed backup is too large")
		}
		sumBytes := sha256.Sum256(body)
		sum := hex.EncodeToString(sumBytes[:])
		if manifest.SHA256[name] != sum {
			arc.close()
			return nil, invalidf("checksum %s", name)
		}
		seen[name] = struct{}{}
		switch {
		case name == databaseName:
			arc.dbPath = filepath.Join(root, databaseName)
			if err := os.WriteFile(arc.dbPath, body, 0o600); err != nil {
				arc.close()
				return nil, err
			}
		case name == "config.yaml":
			arc.config = body
		case strings.HasPrefix(name, filesPrefix):
			rel := strings.TrimPrefix(name, filesPrefix)
			if err := extractRel(arc.filesDir, rel, body, false); err != nil {
				arc.close()
				return nil, err
			}
		default:
			arc.close()
			return nil, invalidf("unexpected entry %s", name)
		}
	}
	for name := range manifest.SHA256 {
		if _, ok := seen[name]; !ok {
			arc.close()
			return nil, invalidf("missing entry %s", name)
		}
	}
	if arc.dbPath == "" {
		arc.close()
		return nil, invalidf("missing database")
	}
	if manifest.Kind == KindFull && arc.config == nil {
		arc.close()
		return nil, invalidf("missing config")
	}
	if manifest.Kind == KindProject && arc.config != nil {
		arc.close()
		return nil, invalidf("a project backup has no config")
	}
	return arc, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, invalidf("read %s", f.Name)
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(r, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxArchiveBytes {
		return nil, invalidf("uncompressed backup is too large")
	}
	return body, nil
}

func extractRel(root, rel string, body []byte, dir bool) error {
	rel = path.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return invalidf("path %q", rel)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if !inside(root, target) {
		return invalidf("path %q", rel)
	}
	if dir {
		return os.MkdirAll(target, 0o700)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, body, 0o600)
}

func inside(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
