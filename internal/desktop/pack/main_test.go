//go:build darwin

package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/update"
)

func TestRunRejectsAVersionThatIsNotARelease(t *testing.T) {
	if err := run("dev", t.TempDir(), "arm64"); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatal(err)
	}
	if err := run("1.2.3-dirty", t.TempDir(), "arm64"); err == nil {
		t.Fatal("a dirty describe is not a release")
	}
	if err := run("1.2.3", t.TempDir(), "386"); err == nil || !strings.Contains(err.Error(), "arch") {
		t.Fatal(err)
	}
}

func TestModuleRootFindsThisCheckout(t *testing.T) {
	root, err := moduleRoot()
	if err != nil || root == "" {
		t.Fatal(err)
	}
}

func TestMacReleaseZipContainsFinderIcon(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "source")
	if err := os.WriteFile(exe, []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "zwai.app")
	if err := desktop.WriteMacApp(exe, app, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "release.zip")
	if err := update.ZipApp(app, archive); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, name := range []string{"zwai.app/Contents/Info.plist", "zwai.app/Contents/Resources/AppIcon.icns"} {
		f, err := zr.Open(name)
		if err != nil {
			t.Fatalf("installable ZIP misses %s: %v", name, err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".plist") && !strings.Contains(string(data), "<string>AppIcon.icns</string>") {
			t.Fatal("Finder icon is not selected by Info.plist")
		}
		if strings.HasSuffix(name, ".icns") && (len(data) < 8 || string(data[:4]) != "icns") {
			t.Fatal("installable ZIP lacks a valid ICNS header")
		}
	}
}
