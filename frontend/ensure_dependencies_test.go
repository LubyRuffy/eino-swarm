package frontend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeBuildDependencyTree(t *testing.T) string {
	t.Helper()
	root := writeTree(t)
	mustWrite(t, filepath.Join(root, "package.json"), `{"name":"ui","scripts":{"build":"tsc -b && vite build"},"dependencies":{"react":"*"},"devDependencies":{"typescript":"*","vite":"*"}}`)
	installBuildDependencyFixtures(t, root)
	return root
}

func installBuildDependencyFixtures(t *testing.T, root string) {
	t.Helper()
	for name, manifest := range map[string]string{
		"react":      `{"name":"react"}`,
		"typescript": `{"name":"typescript","bin":{"tsc":"bin/tsc"}}`,
		"vite":       `{"name":"vite","bin":{"vite":"bin/vite.js"}}`,
	} {
		mustWrite(t, filepath.Join(root, "node_modules", name, "package.json"), manifest)
	}
	for _, name := range []string{"tsc", "vite"} {
		path := fixtureBinPath(root, name)
		_ = os.RemoveAll(path)
		mustWrite(t, path, "fixture executable\n")
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func fixtureBinPath(root, name string) string {
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	return filepath.Join(root, "node_modules", ".bin", name)
}

// node_modules timestamps cannot establish whether an interrupted/production-
// only install actually left the packages and compiler needed by the build.
func TestEnsureRepairsIncompleteModulesEvenWhenTheirTimestampIsNewer(t *testing.T) {
	for _, name := range []string{"empty tree", "partial tree", "missing production package", "missing development package", "missing tsc", "broken vite"} {
		t.Run(name, func(t *testing.T) {
			root := writeBuildDependencyTree(t)
			switch name {
			case "empty tree":
				if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "partial tree":
				_ = os.RemoveAll(filepath.Join(root, "node_modules", "typescript"))
				_ = os.RemoveAll(filepath.Join(root, "node_modules", "vite"))
			case "missing production package":
				_ = os.RemoveAll(filepath.Join(root, "node_modules", "react"))
			case "missing development package":
				_ = os.RemoveAll(filepath.Join(root, "node_modules", "typescript"))
			case "missing tsc":
				_ = os.Remove(fixtureBinPath(root, "tsc"))
			case "broken vite":
				_ = os.Remove(fixtureBinPath(root, "vite"))
				if err := os.Symlink(filepath.Join(root, "missing-vite"), fixtureBinPath(root, "vite")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			newer := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(root, "node_modules"), newer, newer); err != nil {
				t.Fatal(err)
			}
			run := &scriptRunner{writeIndex: true, afterInstall: func(root string) error {
				installBuildDependencyFixtures(t, root)
				return nil
			}}
			if err := ensureBundle(t.Context(), root, run); err != nil {
				t.Fatal(err)
			}
			if !hasCall(run.calls, npmInstallArg) || !hasCall(run.calls, "--include=dev") || !hasCall(run.calls, npmBuildScript) {
				t.Fatalf("incomplete packages were not repaired with development tools before building: %v", run.calls)
			}
			if needsInstall(root) {
				t.Fatal("successful repair left an unusable dependency tree")
			}
			if _, err := os.Stat(stampPath(root)); err != nil {
				t.Fatal("the repaired build did not produce a source stamp")
			}
		})
	}
}

func TestEnsureRejectsAnInstallThatDidNotRestoreBuildDependencies(t *testing.T) {
	root := writeBuildDependencyTree(t)
	if err := os.Remove(fixtureBinPath(root, "tsc")); err != nil {
		t.Fatal(err)
	}
	run := &scriptRunner{writeIndex: true}
	err := ensureBundle(t.Context(), root, run)
	if err == nil || !strings.Contains(err.Error(), "npm install") || !strings.Contains(err.Error(), "tsc") {
		t.Fatalf("an apparently successful installation still omitted the compiler: %v", err)
	}
	if !hasCall(run.calls, npmInstallArg) || hasCall(run.calls, npmBuildScript) {
		t.Fatalf("an incomplete installation reached the build: %v", run.calls)
	}
	if _, err := os.Stat(stampPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an incomplete installation was stamped: %v", err)
	}
}

func TestFreshBundleNeedsNeitherNpmNorInstalledPackages(t *testing.T) {
	root := writeBuildDependencyTree(t)
	if err := ensureBundle(t.Context(), root, &scriptRunner{writeIndex: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	run := &scriptRunner{lookPathErr: errors.New("Node is unavailable")}
	if err := ensureBundle(context.Background(), root, run); err != nil || len(run.calls) != 0 {
		t.Fatalf("a fresh bundle unnecessarily required Node: err=%v calls=%v", err, run.calls)
	}
}

func TestInstalledBuildPackagesAcceptsObjectStringScopedAndAbsentBins(t *testing.T) {
	for _, tc := range []struct {
		name string
		bin  string
		exec string
	}{
		{"compiler", `{"compile":"cli.js"}`, "compile"},
		{"compiler", `"cli.js"`, "compiler"},
		{"@tools/compiler", `"cli.js"`, "compiler"},
		{"compiler", `null`, ""},
	} {
		t.Run(tc.name+tc.bin, func(t *testing.T) {
			root := writeTree(t)
			mustWrite(t, filepath.Join(root, "package.json"), `{"devDependencies":{"`+tc.name+`":"*"}}`)
			mustWrite(t, filepath.Join(root, "node_modules", tc.name, "package.json"), `{"bin":`+tc.bin+`}`)
			if tc.exec != "" {
				bin := fixtureBinPath(root, tc.exec)
				mustWrite(t, bin, "fixture executable")
				if err := os.Chmod(bin, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := checkInstalledBuildPackages(root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInstalledBuildPackagesReportsUnreadableOrMalformedMetadata(t *testing.T) {
	for _, name := range []string{"missing manifest", "invalid manifest", "invalid installed manifest", "invalid bin", "non-executable bin", "directory bin"} {
		t.Run(name, func(t *testing.T) {
			root := writeBuildDependencyTree(t)
			switch name {
			case "missing manifest":
				_ = os.Remove(filepath.Join(root, "package.json"))
			case "invalid manifest":
				mustWrite(t, filepath.Join(root, "package.json"), "{")
			case "invalid installed manifest":
				mustWrite(t, filepath.Join(root, "node_modules", "typescript", "package.json"), "{")
			case "invalid bin":
				mustWrite(t, filepath.Join(root, "node_modules", "typescript", "package.json"), `{"bin":17}`)
			case "non-executable bin":
				if runtime.GOOS == "windows" {
					t.Skip("Windows executable checks use the command extension")
				}
				if err := os.Chmod(fixtureBinPath(root, "tsc"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory bin":
				_ = os.Remove(fixtureBinPath(root, "vite"))
				if err := os.Mkdir(fixtureBinPath(root, "vite"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := checkInstalledBuildPackages(root); err == nil {
				t.Fatal("unusable build metadata or executable was accepted")
			}
		})
	}
}

func TestEnsureRejectsAnUnreadableSourceBeforeRunningNpm(t *testing.T) {
	root := writeBuildDependencyTree(t)
	mustWrite(t, filepath.Join(root, "dist", "index.html"), "existing bundle")
	if err := os.Symlink(filepath.Join(root, "missing-source"), filepath.Join(root, "src", "broken.ts")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(t.Context(), root, run); !errors.Is(err, os.ErrNotExist) || len(run.calls) != 0 {
		t.Fatalf("an unreadable source entered npm or lost its cause: err=%v calls=%v", err, run.calls)
	}
}
