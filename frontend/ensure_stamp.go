package frontend

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func stampPath(root string) string {
	return filepath.Join(root, "dist", stampName)
}

func stale(root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, "dist", "index.html")); err != nil {
		return true, nil
	}
	fp, err := fingerprint(root)
	if err != nil {
		return false, err
	}
	got, err := os.ReadFile(stampPath(root))
	if err != nil || strings.TrimSpace(string(got)) != fp {
		return true, nil
	}
	return false, nil
}

func writeStamp(root string, fp string) error {
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(stampPath(root), []byte(fp+"\n"), 0o644)
}

// fingerprint is a hash of the files Vite actually reads. dist/ and
// node_modules are outputs; e2e and unit tests do not change the bundle,
// so editing those must not trigger a rebuild.
func fingerprint(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel != "." && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(d.Name()) {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", "dist", "e2e", "test-results", "playwright-report",
		"blob-report", "coverage":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func skipFile(name string) bool {
	switch {
	case name == ".DS_Store":
		return true
	case strings.HasSuffix(name, ".go"):
		return true
	case strings.HasSuffix(name, ".tsbuildinfo"):
		return true
	case strings.HasSuffix(name, ".test.ts"), strings.HasSuffix(name, ".test.tsx"):
		return true
	case strings.HasSuffix(name, ".spec.ts"), strings.HasSuffix(name, ".spec.tsx"):
		return true
	}
	return false
}
