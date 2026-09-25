//go:build darwin

// Command pack builds the macOS desktop zip attached to a GitHub Release.
//
//	go run ./internal/desktop/pack -version 1.2.3 -o bin
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/update"
)

func main() {
	version := flag.String("version", "", "release version, major.minor.patch")
	outDir := flag.String("o", "bin", "directory for the zip")
	flag.Parse()
	if err := run(*version, *outDir, runtime.GOARCH); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(version, outDir, arch string) error {
	canonical := update.Canonical(version)
	if canonical == "" {
		return fmt.Errorf("desktop release needs a numeric version, got %q", version)
	}
	if arch != "arm64" && arch != "amd64" {
		return fmt.Errorf("desktop release arch must be arm64 or amd64, got %q", arch)
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "zwai-desktop-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	exe := filepath.Join(stage, "zwai")
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+canonical, "-o", exe, "./cmd/zwai")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOOS=darwin", "GOARCH="+arch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build zwai: %w", err)
	}
	app := filepath.Join(stage, "zwai.app")
	if err := desktop.WriteMacApp(exe, app, canonical); err != nil {
		return err
	}
	sign := exec.Command("codesign", "--force", "--sign", "-", "--identifier", "app.zwai.desktop", app)
	sign.Stdout = os.Stdout
	sign.Stderr = os.Stderr
	if err := sign.Run(); err != nil {
		return fmt.Errorf("codesign: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(outDir, update.DarwinAssetName(canonical, arch))
	if err := update.ZipApp(app, dest); err != nil {
		return err
	}
	fmt.Println(dest)
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
