package frontend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const (
	npmCommand     = "npm"
	npmInstallArg  = "install"
	npmRunArg      = "run"
	npmBuildScript = "build"
	stampName      = ".source-stamp"
)

// errNoSources is Ensure when this binary was not compiled next to the
// frontend checkout. Load falls back to the embed instead of failing.
var errNoSources = errors.New("frontend sources were not found; run this from a git checkout")

type commandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, dir, name string, args ...string) error
}

type execRunner struct{}

func (execRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = ensureOut
	cmd.Stderr = ensureOut
	return cmd.Run()
}

// findRoot / runCmd / ensureOut are swapped in tests so Ensure does not
// spawn a real npm against this checkout.
var (
	findRoot                = sourceRoot
	runCmd    commandRunner = execRunner{}
	ensureOut io.Writer     = os.Stderr
	thisFile  string
)

func init() {
	// Caller here is this file, not whoever invoked sourceRoot — inlining
	// would otherwise make a fresh clone look like it has no checkout.
	_, thisFile, _, _ = runtime.Caller(0)
}

// Load returns the UI the HTTP server should serve. From a checkout it
// rebuilds dist/ when the sources changed and returns that directory —
// go:embed cannot see a bundle written after this process was compiled,
// which is exactly the fresh-clone `go run` case. Under `go test` it
// returns the embed so `go test ./...` does not require Node.
func Load(ctx context.Context) (fs.FS, error) {
	if testing.Testing() {
		return Assets(), nil
	}
	return load(ctx, findRoot(), runCmd)
}

// Ensure rebuilds dist/ when the checkout's frontend sources changed.
// go:generate and `make frontend` call this; it is an error when the
// sources are not on disk (an installed binary should use the embed).
func Ensure(ctx context.Context) error {
	root := findRoot()
	if root == "" {
		return errNoSources
	}
	return ensureBundle(ctx, root, runCmd)
}

func load(ctx context.Context, root string, run commandRunner) (fs.FS, error) {
	if root == "" {
		return Assets(), nil
	}
	if err := ensureBundle(ctx, root, run); err != nil {
		return nil, err
	}
	distFS := os.DirFS(filepath.Join(root, "dist"))
	if _, err := fs.Stat(distFS, "index.html"); err != nil {
		return nil, fmt.Errorf("frontend dist has no index.html after the build")
	}
	return distFS, nil
}

func ensureBundle(ctx context.Context, root string, run commandRunner) error {
	need, err := stale(root)
	if err != nil {
		return err
	}
	if !need {
		return nil
	}
	if err := installAndBuild(ctx, root, run); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "index.html")); err != nil {
		return fmt.Errorf("frontend dist has no index.html after the build")
	}
	fp, err := fingerprint(root)
	if err != nil {
		return err
	}
	return writeStamp(root, fp)
}

func installAndBuild(ctx context.Context, root string, run commandRunner) error {
	npm, err := run.LookPath(npmCommand)
	if err != nil {
		return fmt.Errorf("the UI bundle is missing or stale, and %s was not found on PATH; install Node.js", npmCommand)
	}
	if needsInstall(root) {
		if err := checkLockfileRegistry(root); err != nil {
			return err
		}
		fmt.Fprintln(ensureOut, "zwai: installing frontend packages")
		if err := run.Run(ctx, root, npm, npmInstallArg); err != nil {
			return fmt.Errorf("npm install: %w", err)
		}
	}
	fmt.Fprintln(ensureOut, "zwai: building frontend")
	if err := run.Run(ctx, root, npm, npmRunArg, npmBuildScript); err != nil {
		return fmt.Errorf("npm run build: %w", err)
	}
	return nil
}

func needsInstall(root string) bool {
	info, err := os.Stat(filepath.Join(root, "node_modules"))
	if err != nil || !info.IsDir() {
		return true
	}
	nmTime := info.ModTime()
	for _, name := range []string{"package.json", "package-lock.json"} {
		if mt := mtime(filepath.Join(root, name)); mt.After(nmTime) {
			return true
		}
	}
	return false
}

func mtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func sourceRoot() string {
	return rootFromFile(thisFile)
}

func rootFromFile(file string) string {
	if file == "" {
		return ""
	}
	dir := filepath.Dir(file)
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return ""
	}
	return dir
}
