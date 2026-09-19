package frontend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedLockfilesMatchThePublicNpmRegistry(t *testing.T) {
	// A fresh clone runs npm install against the default registry.
	// Mirror hosts in the lockfile are what made npm 12 exit EALLOWREMOTE.
	root := sourceRoot()
	if root == "" {
		t.Fatal("frontend checkout missing")
	}
	files := []string{
		filepath.Join(root, "package-lock.json"),
		filepath.Join(root, "..", "mobile", "package-lock.json"),
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		hosts, err := lockfileForeignHosts(data)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(hosts) > 0 {
			t.Errorf("%s resolves against %s; a clone with the default npm registry will fail install", path, strings.Join(hosts, ", "))
		}
	}
}

func TestProjectNpmrcPinsThePublicRegistry(t *testing.T) {
	// A user-level mirror rewrites resolved URLs on the next npm install.
	// The project file has to pin the public registry or the lockfile
	// test above is a revolving door.
	root := sourceRoot()
	if root == "" {
		t.Fatal("frontend checkout missing")
	}
	want := "registry=https://" + publicNpmRegistryHost + "/"
	files := []string{
		filepath.Join(root, ".npmrc"),
		filepath.Join(root, "..", "mobile", ".npmrc"),
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !strings.Contains(string(body), want) {
			t.Errorf("%s must pin %s", path, want)
		}
	}
}

func TestLockfileForeignHostsReportsEveryNonPublicRegistry(t *testing.T) {
	hosts, err := lockfileForeignHosts([]byte(`{"packages":{
		"node_modules/a":{"resolved":"https://registry.npmmirror.com/a/-/a-1.0.0.tgz"},
		"node_modules/b":{"resolved":"https://registry.npmjs.org/b/-/b-1.0.0.tgz"},
		"node_modules/c":{"resolved":"https://pkgs.example.test/c/-/c-1.0.0.tgz"},
		"":{"name":"ui"}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(hosts, ",")
	if got != "pkgs.example.test,registry.npmmirror.com" {
		t.Fatalf("got %q", got)
	}
}

func TestLockfileForeignHostsRejectsInvalidJSON(t *testing.T) {
	if _, err := lockfileForeignHosts([]byte(`{`)); err == nil {
		t.Fatal("corrupt lockfile must fail closed")
	}
}

func TestLockfileForeignHostsRejectsUnparseableResolved(t *testing.T) {
	if _, err := lockfileForeignHosts([]byte(`{"packages":{"node_modules/a":{"resolved":"http://["}}}`)); err == nil {
		t.Fatal("a resolved value that is not a URL must fail closed")
	}
}

func TestCheckLockfileRegistryAllowsAMissingLockfile(t *testing.T) {
	if err := checkLockfileRegistry(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLockfileRegistryAllowsThePublicHost(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "package-lock.json"), `{"packages":{"node_modules/a":{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz"}}}`)
	if err := checkLockfileRegistry(dir); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorLockfileFailsBeforeNpmInstall(t *testing.T) {
	dir := writeTree(t)
	mustWrite(t, filepath.Join(dir, "package-lock.json"), `{"packages":{"node_modules/a":{"resolved":"https://registry.npmmirror.com/a/-/a-1.0.0.tgz"}}}`)
	run := &scriptRunner{writeIndex: true}
	err := ensureBundle(context.Background(), dir, run)
	if err == nil || !strings.Contains(err.Error(), "EALLOWREMOTE") || !strings.Contains(err.Error(), "registry.npmmirror.com") {
		t.Fatalf("got %v", err)
	}
	if len(run.calls) != 0 {
		t.Fatalf("must not spawn npm on a known-bad lockfile: %v", run.calls)
	}
}

func TestCheckLockfileRegistryRejectsALockfileDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "package-lock.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkLockfileRegistry(dir); err == nil {
		t.Fatal("a directory named package-lock.json must fail")
	}
}

func TestUnhostedResolvedCountsAsForeign(t *testing.T) {
	hosts, err := lockfileForeignHosts([]byte(`{"packages":{"node_modules/a":{"resolved":"file:vendor/a.tgz"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "file:vendor/a.tgz" {
		t.Fatalf("got %v", hosts)
	}
}
