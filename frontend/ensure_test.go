package frontend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptRunner struct {
	lookPath    string
	lookPathErr error
	calls       [][]string
	runErr      error
	installErr  error
	buildErr    error
	writeIndex  bool
}

func (s *scriptRunner) LookPath(file string) (string, error) {
	if file != npmCommand {
		return "", errors.New("unexpected LookPath " + file)
	}
	if s.lookPathErr != nil {
		return "", s.lookPathErr
	}
	if s.lookPath != "" {
		return s.lookPath, nil
	}
	return "/bin/npm", nil
}

func (s *scriptRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.calls = append(s.calls, append([]string{name}, args...))
	if s.runErr != nil {
		return s.runErr
	}
	if len(args) > 0 && args[0] == npmInstallArg {
		if s.installErr != nil {
			return s.installErr
		}
		return nil
	}
	if len(args) >= 2 && args[0] == npmRunArg && args[1] == npmBuildScript {
		if s.buildErr != nil {
			return s.buildErr
		}
		if s.writeIndex {
			if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "dist", "index.html"), []byte("<!doctype html>"), 0o644)
		}
		return nil
	}
	return errors.New("unexpected npm args " + strings.Join(args, " "))
}

func writeTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "package.json"), `{"name":"ui"}`)
	mustWrite(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3}`)
	mustWrite(t, filepath.Join(dir, "index.html"), `<!doctype html><div id="root"></div>`)
	mustWrite(t, filepath.Join(dir, "vite.config.ts"), `export default {}`)
	mustWrite(t, filepath.Join(dir, "src", "main.tsx"), `export {}`)
	mustWrite(t, filepath.Join(dir, "node_modules", ".package-lock.json"), `{}`)
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func swapEnsure(t *testing.T, root string, run commandRunner) {
	t.Helper()
	prevRoot, prevRun, prevOut := findRoot, runCmd, ensureOut
	t.Cleanup(func() {
		findRoot = prevRoot
		runCmd = prevRun
		ensureOut = prevOut
	})
	findRoot = func() string { return root }
	runCmd = run
	ensureOut = io.Discard
}

func TestLoadUsesTheEmbedDuringGoTest(t *testing.T) {
	got, err := Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Assets()
	if (got == nil) != (want == nil) {
		t.Fatal("Load under go test must match Assets so the suite does not spawn npm")
	}
	if got == nil {
		return
	}
	if _, err := fs.Stat(got, "index.html"); err != nil {
		t.Fatalf("Load embed: %v", err)
	}
}

func TestSourceRootFindsTheCheckout(t *testing.T) {
	root := sourceRoot()
	if root == "" {
		t.Fatal("go test of this package runs from the module; package.json must be next to ensure.go")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRootWithoutThisFile(t *testing.T) {
	prev := thisFile
	t.Cleanup(func() { thisFile = prev })
	thisFile = ""
	if sourceRoot() != "" {
		t.Fatal("an empty compile path is not a checkout")
	}
}

func TestSourceRootWithoutPackageJSON(t *testing.T) {
	if rootFromFile(filepath.Join(t.TempDir(), "ensure.go")) != "" {
		t.Fatal("a random directory is not the frontend checkout")
	}
}

func TestLoadWithoutACheckoutUsesTheEmbed(t *testing.T) {
	got, err := load(context.Background(), "", &scriptRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if (got == nil) != (Assets() == nil) {
		t.Fatal("an installed binary with no checkout must serve the embed")
	}
}

func TestEnsureWithoutACheckoutFails(t *testing.T) {
	swapEnsure(t, "", &scriptRunner{})
	if err := Ensure(context.Background()); !errors.Is(err, errNoSources) {
		t.Fatalf("got %v, want errNoSources", err)
	}
}

func TestEnsureBuildsAFreshCloneOnce(t *testing.T) {
	dir := writeTree(t)
	if err := os.RemoveAll(filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	run := &scriptRunner{writeIndex: true}
	swapEnsure(t, dir, run)
	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "index.html")); err != nil {
		t.Fatal("a clone with no dist must end up with index.html, or the window is a 404")
	}
	if _, err := os.Stat(stampPath(dir)); err != nil {
		t.Fatal(err)
	}
	if !hasCall(run.calls, npmInstallArg) || !hasCall(run.calls, npmBuildScript) {
		t.Fatalf("first build calls=%v", run.calls)
	}

	run.calls = nil
	if err := Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(run.calls) != 0 {
		t.Fatalf("unchanged sources must not rebuild, calls=%v", run.calls)
	}
}

func TestLoadServesTheDiskBundleAfterABuild(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	got, err := load(context.Background(), dir, run)
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(got, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("<!doctype html>")) {
		t.Fatalf("served %q", body)
	}
}

func TestSourceChangeRebuildsWithoutReinstalling(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	run.calls = nil
	mustWrite(t, filepath.Join(dir, "src", "main.tsx"), `export const n = 1`)
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if hasCall(run.calls, npmInstallArg) {
		t.Fatalf("modules were already there: %v", run.calls)
	}
	if !hasCall(run.calls, npmBuildScript) {
		t.Fatalf("changed src must rebuild: %v", run.calls)
	}
}

func TestNoiseFilesDoNotRebuild(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	run.calls = nil
	mustWrite(t, filepath.Join(dir, "src", "main.test.tsx"), `export {}`)
	mustWrite(t, filepath.Join(dir, "e2e", "flow.spec.ts"), `export {}`)
	mustWrite(t, filepath.Join(dir, "dist", "extra.js"), `{}`)
	mustWrite(t, filepath.Join(dir, "node_modules", "pkg", "index.js"), `{}`)
	mustWrite(t, filepath.Join(dir, "embed.go"), `package frontend`)
	mustWrite(t, filepath.Join(dir, ".vite", "cache"), `x`)
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if len(run.calls) != 0 {
		t.Fatalf("tests, e2e, dist, node_modules and Go files must not rebuild: %v", run.calls)
	}
}

func TestMissingNpmIsAStartupError(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{lookPathErr: errors.New("not found")}
	err := ensureBundle(context.Background(), dir, run)
	if err == nil || !strings.Contains(err.Error(), npmCommand) {
		t.Fatalf("got %v", err)
	}
}

func TestNpmInstallFailureStopsTheBuild(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "package.json"), `{"name":"ui"}`)
	mustWrite(t, filepath.Join(dir, "src", "main.tsx"), `export {}`)
	run := &scriptRunner{installErr: errors.New("network")}
	err := ensureBundle(context.Background(), dir, run)
	if err == nil || !strings.Contains(err.Error(), "npm install") {
		t.Fatalf("got %v", err)
	}
}

func TestNpmBuildFailureLeavesNoStamp(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{buildErr: errors.New("tsc")}
	err := ensureBundle(context.Background(), dir, run)
	if err == nil || !strings.Contains(err.Error(), "npm run build") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(stampPath(dir)); err == nil {
		t.Fatal("a failed build must not stamp, or the next start would skip a broken dist")
	}
}

func TestBuildWithoutIndexHTMLFails(t *testing.T) {
	dir := writeTree(t)
	_, err := load(context.Background(), dir, &scriptRunner{writeIndex: false})
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("got %v", err)
	}
}

func TestCancelledBuildStopsNpm(t *testing.T) {
	dir := writeTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensureBundle(ctx, dir, &scriptRunner{writeIndex: true})
	if err == nil {
		t.Fatal("a cancelled first-run build must stop")
	}
}

func TestLockfileNewerThanModulesReinstalls(t *testing.T) {
	dir := writeTree(t)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "node_modules"), past, past); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3,"x":1}`)
	if !needsInstall(dir) {
		t.Fatal("a newer lockfile must reinstall, or the bundle builds against the old tree")
	}
}

func TestNeedsInstallWhenNodeModulesIsAFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "node_modules"), "nope")
	if !needsInstall(dir) {
		t.Fatal("a file named node_modules is not an install")
	}
}

func TestFingerprintIsStableAndWalkErrors(t *testing.T) {
	dir := writeTree(t)
	a, err := fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a == "" {
		t.Fatalf("a=%s b=%s", a, b)
	}
	if _, err := fingerprint(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("want an error for a tree that cannot be walked")
	}
}

func TestStaleWhenIndexGoesMissing(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "dist", "index.html")); err != nil {
		t.Fatal(err)
	}
	need, err := stale(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !need {
		t.Fatal("a wiped dist must rebuild even if the stamp file is still there")
	}
}

func TestStampMismatchRebuilds(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stampPath(dir), []byte("not-the-hash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run.calls = nil
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if !hasCall(run.calls, npmBuildScript) {
		t.Fatalf("mismatch stamp must rebuild: %v", run.calls)
	}
}

func TestWriteStampCreatesDist(t *testing.T) {
	dir := t.TempDir()
	if err := writeStamp(dir, "abc"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(stampPath(dir))
	if err != nil || strings.TrimSpace(string(got)) != "abc" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestWriteStampRejectsAFileNamedDist(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dist"), "nope")
	if err := writeStamp(dir, "abc"); err == nil {
		t.Fatal("dist as a file must not be treated as the bundle directory")
	}
}

func TestConfigChangeRebuilds(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	run.calls = nil
	mustWrite(t, filepath.Join(dir, "vite.config.ts"), `export default { base: "./" }`)
	if err := ensureBundle(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if !hasCall(run.calls, npmBuildScript) {
		t.Fatalf("vite config is an input: %v", run.calls)
	}
}

func TestInstallLogOnAClone(t *testing.T) {
	dir := writeTree(t)
	if err := os.RemoveAll(filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	prev := ensureOut
	ensureOut = &buf
	t.Cleanup(func() { ensureOut = prev })
	if err := ensureBundle(context.Background(), dir, &scriptRunner{writeIndex: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "installing frontend packages") {
		t.Fatalf("log=%q", buf.String())
	}
}

func TestSkipHelpers(t *testing.T) {
	for _, name := range []string{"node_modules", "dist", "e2e", ".vite", "test-results", "playwright-report", "blob-report", "coverage"} {
		if !skipDir(name) {
			t.Fatalf("skipDir(%q)", name)
		}
	}
	if skipDir("src") {
		t.Fatal("src is input")
	}
	for _, name := range []string{".DS_Store", "x.go", "a.test.ts", "b.test.tsx", "c.spec.ts", "e.spec.tsx", "d.tsbuildinfo"} {
		if !skipFile(name) {
			t.Fatalf("skipFile(%q)", name)
		}
	}
	if skipFile("main.tsx") {
		t.Fatal("main.tsx is input")
	}
}

func TestMtimeMissingIsZero(t *testing.T) {
	if !mtime(filepath.Join(t.TempDir(), "nope")).IsZero() {
		t.Fatal("missing file")
	}
}

func TestLookPathUsesConfiguredName(t *testing.T) {
	dir := writeTree(t)
	run := &scriptRunner{lookPath: "/custom/npm", writeIndex: true}
	if err := installAndBuild(context.Background(), dir, run); err != nil {
		t.Fatal(err)
	}
	if len(run.calls) == 0 || run.calls[0][0] != "/custom/npm" {
		t.Fatalf("calls=%v", run.calls)
	}
}

func TestEnsureLogsThenStamps(t *testing.T) {
	dir := writeTree(t)
	var buf bytes.Buffer
	prev := ensureOut
	ensureOut = &buf
	t.Cleanup(func() { ensureOut = prev })
	if err := ensureBundle(context.Background(), dir, &scriptRunner{writeIndex: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "building frontend") {
		t.Fatalf("log=%q", buf.String())
	}
}

func TestExecRunnerLookPathAndRun(t *testing.T) {
	r := execRunner{}
	if _, err := r.LookPath("zwai-npm-not-on-path-xyz"); err == nil {
		t.Fatal("a missing binary must fail LookPath")
	}
	if err := r.Run(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "nope"), "x"); err == nil {
		t.Fatal("a missing binary must fail Run")
	}
	path, err := exec.LookPath("true")
	if err != nil {
		t.Skip(err)
	}
	if err := r.Run(context.Background(), t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
}

func TestPackageJSONNewerThanModulesReinstalls(t *testing.T) {
	dir := writeTree(t)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "node_modules"), past, past); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "package.json"), `{"name":"ui","x":1}`)
	if !needsInstall(dir) {
		t.Fatal("a newer package.json must reinstall")
	}
}

func hasCall(calls [][]string, needle string) bool {
	for _, c := range calls {
		for _, a := range c {
			if a == needle {
				return true
			}
		}
	}
	return false
}
