//go:build darwin

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlreadyBundledLooksAtTheMacOSPath(t *testing.T) {
	if alreadyBundled("/var/folders/xx/exe/zwai") {
		t.Fatal("a go-run temp binary is not a bundle")
	}
	if !alreadyBundled("/Users/me/Library/Caches/zwai/zwai.app/Contents/MacOS/zwai") {
		t.Fatal("a binary inside Contents/MacOS is the bundle")
	}
}

func TestWriteAppBundleInstallsPlistAndExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "zwai.app")
	if err := writeAppBundle(src, app); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(plist)
	for _, need := range []string{
		macBundleID,
		"NSLocalNetworkUsageDescription",
		"NSAllowsLocalNetworking",
		"zwai lists models and talks to endpoints on your local network.",
	} {
		if !strings.Contains(body, need) {
			t.Fatalf("Info.plist missing %q", need)
		}
	}
	// A user's LAN address must not leak into the permission prompt.
	if strings.Contains(body, "172.") || strings.Contains(strings.ToLower(body), "example") {
		t.Fatalf("plist must stay task-agnostic: %s", body)
	}
	got, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", macBundleName))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\n" {
		t.Fatalf("copied executable %q", got)
	}
}

func TestMacInfoPlistIsValidEnoughToParse(t *testing.T) {
	body := macInfoPlist()
	if !strings.HasPrefix(body, `<?xml version="1.0"`) {
		t.Fatal("plist must be XML")
	}
	if strings.Count(body, "<dict>") != strings.Count(body, "</dict>") {
		t.Fatal("unbalanced dict")
	}
}

func TestPrepareBundleNoopsInsideAnApp(t *testing.T) {
	got, err := prepareBundle("/tmp/zwai.app/Contents/MacOS/zwai", t.TempDir())
	if err != nil || got != "" {
		t.Fatalf("already bundled: dest=%q err=%v", got, err)
	}
}

func TestPrepareBundleWritesUnderCache(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := prepareBundle(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "zwai", "zwai.app", "Contents", "MacOS", macBundleName)
	if got != want {
		t.Fatalf("dest %q want %q", got, want)
	}
}

func TestPrepareBundleFailsWhenSourceIsMissing(t *testing.T) {
	if _, err := prepareBundle(filepath.Join(t.TempDir(), "nope"), t.TempDir()); err == nil {
		t.Fatal("missing source must fail")
	}
}

func TestWriteAppBundleFailsWhenSourceIsMissing(t *testing.T) {
	err := writeAppBundle(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "zwai.app"))
	if err == nil {
		t.Fatal("a missing executable must fail")
	}
}

func TestCopyFileReportsOpenAndCreateErrors(t *testing.T) {
	if err := copyFile(filepath.Join(t.TempDir(), "nope"), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("missing source")
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, filepath.Join(t.TempDir(), "no-dir", "out")); err == nil {
		t.Fatal("missing dest dir")
	}
}
