package frontend

import (
	"context"
	"encoding/json"
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
	return runBuildCommand(ctx, cmd)
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
	release, err := lockBuild(ctx, root)
	if err != nil {
		return err
	}
	defer release()
	if need, err := stale(root); err != nil || !need {
		return err
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
		if err := run.Run(ctx, root, npm, npmInstallArg, "--include=dev"); err != nil {
			return fmt.Errorf("npm install: %w", err)
		}
		if err := checkInstalledBuildPackages(root); err != nil {
			return fmt.Errorf("npm install did not provide frontend build dependencies: %w", err)
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
	if err := checkInstalledBuildPackages(root); err != nil {
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

// Inspect only direct manifest dependencies. Package-directory timestamps do
// not prove an interrupted install left the build tools or their bin links.
func checkInstalledBuildPackages(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return err
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("package.json: %w", err)
	}
	modules := filepath.Join(root, "node_modules")
	for _, dependencies := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for name := range dependencies {
			data, err := os.ReadFile(filepath.Join(modules, name, "package.json"))
			if err != nil {
				return fmt.Errorf("frontend package %s: %w", name, err)
			}
			var pkg struct {
				Bin json.RawMessage `json:"bin"`
			}
			if err := json.Unmarshal(data, &pkg); err != nil {
				return fmt.Errorf("frontend package %s: %w", name, err)
			}
			if len(pkg.Bin) == 0 {
				continue
			}
			var bins map[string]string
			if err := json.Unmarshal(pkg.Bin, &bins); err != nil {
				var bin string
				if err := json.Unmarshal(pkg.Bin, &bin); err != nil {
					return fmt.Errorf("frontend package %s bin: %w", name, err)
				}
				bins = map[string]string{filepath.Base(name): bin}
			}
			for bin := range bins {
				if _, err := exec.LookPath(filepath.Join(modules, ".bin", bin)); err != nil {
					return fmt.Errorf("frontend build executable %s: %w", bin, err)
				}
			}
		}
	}
	return nil
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
