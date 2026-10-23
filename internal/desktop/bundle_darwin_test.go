//go:build darwin

package desktop

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	if err := writeAppBundle(src, app, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(plist)
	for _, need := range []string{
		macBundleID,
		"<string>1.2.3</string>",
		"<key>CFBundleIconFile</key>",
		"<string>AppIcon.icns</string>",
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
	icon, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "AppIcon.icns"))
	if err != nil {
		t.Fatal("Finder cannot show the app mark without a bundled icon:", err)
	}
	if len(icon) < 8 || string(icon[:4]) != "icns" {
		t.Fatal("Finder icon is not an ICNS file")
	}
}

func TestFinderIconMatchesTheRuntimeDockMark(t *testing.T) {
	dir := t.TempDir()
	icns := filepath.Join(dir, "AppIcon.icns")
	decoded := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(icns, macAppIcon, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("sips", "-s", "format", "png", "-z", "640", "640", icns, "--out", decoded).CombinedOutput(); err != nil {
		t.Fatalf("macOS cannot decode the Finder icon: %v: %s", err, output)
	}
	gotFile, err := os.Open(decoded)
	if err != nil {
		t.Fatal(err)
	}
	defer gotFile.Close()
	got, err := png.Decode(gotFile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := png.Decode(bytes.NewReader(dockIcon()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("Finder icon bounds %v, Dock mark bounds %v", got.Bounds(), want.Bounds())
	}
	var difference uint64
	for y := 0; y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			g := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			w := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			for _, pair := range [][2]uint8{{g.R, w.R}, {g.G, w.G}, {g.B, w.B}, {g.A, w.A}} {
				if pair[0] > pair[1] {
					difference += uint64(pair[0] - pair[1])
				} else {
					difference += uint64(pair[1] - pair[0])
				}
			}
		}
	}
	mean := float64(difference) / float64(got.Bounds().Dx()*got.Bounds().Dy()*4)
	if mean > 5 {
		t.Fatalf("Finder icon has drifted from the Dock mark: mean channel difference %.2f", mean)
	}
}

func TestMacInfoPlistIsValidEnoughToParse(t *testing.T) {
	body := macInfoPlist("")
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
	want := filepath.Join(dir, "zwai", "bundles") + string(filepath.Separator)
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, filepath.Join("zwai.app", "Contents", "MacOS", macBundleName)) {
		t.Fatalf("dest %q is not an immutable cache bundle under %q", got, want)
	}
}

func TestPreparingAnExecutingBundleNeverRewritesOrResignsIt(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.WriteFile(src, []byte("#!/bin/sh\nprintf 'ready\\n'\nread input\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path, err := prepareBundle(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	bytesBefore, _ := os.ReadFile(path)
	command := exec.Command(path)
	stdout, _ := command.StdoutPipe()
	stdin, _ := command.StdinPipe()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = command.Wait() }()
	ready := make([]byte, 6)
	if _, err := io.ReadFull(stdout, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("bundle child not running: %q %v", ready, err)
	}
	time.Sleep(5 * time.Millisecond)
	again, err := prepareBundle(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	bytesAfter, _ := os.ReadFile(path)
	if again != path || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("preparing the same source mutated a live bundle")
	}
}

func TestDifferentExecutablesAndConcurrentPreparationHaveIndependentImmutableBundles(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "one"), filepath.Join(dir, "two")
	_ = os.WriteFile(first, []byte("first"), 0755)
	_ = os.WriteFile(second, []byte("second"), 0755)
	var group sync.WaitGroup
	paths := make(chan string, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); path, err := prepareBundle(first, dir); paths <- path; failures <- err }()
	}
	group.Wait()
	close(paths)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var same string
	for path := range paths {
		if same != "" && same != path {
			t.Fatal("same source created competing bundles")
		}
		same = path
	}
	other, err := prepareBundle(second, dir)
	if err != nil {
		t.Fatal(err)
	}
	if same == other {
		t.Fatal("a different executable replaced the first bundle")
	}
	if body, _ := os.ReadFile(same); string(body) != "first" {
		t.Fatal("another preparation overwrote the old executable")
	}
}

func TestFailedBundlePreparationNeverPublishesAReceiptOrLeavesTemporaryState(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	_ = os.WriteFile(src, []byte("fixture"), 0755)
	previous := writeCacheBundle
	writeCacheBundle = func(_, path, _ string) error {
		_ = os.MkdirAll(path, 0755)
		return fmt.Errorf("fixture interrupted packaging")
	}
	defer func() { writeCacheBundle = previous }()
	if _, err := prepareBundle(src, dir); err == nil {
		t.Fatal("failed packaging published a bundle")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "zwai", "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatalf("partial bundle survived: %s", entry.Name())
		}
	}
}

func TestImmutableBundleRejectsIncompleteOrChangedCacheWithoutRepairingIt(t *testing.T) {
	for _, damage := range []string{"not_directory", "receipt_missing", "receipt_invalid", "source_mismatch", "executable_missing", "executable_changed"} {
		t.Run(damage, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "source")
			_ = os.WriteFile(src, []byte("immutable executable"), 0755)
			path, err := prepareBundle(src, dir)
			if err != nil {
				t.Fatal(err)
			}
			final := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))
			source, _ := bundleFileHash(src)
			switch damage {
			case "not_directory":
				_ = os.RemoveAll(final)
				_ = os.WriteFile(final, []byte("blocked"), 0600)
			case "receipt_missing":
				_ = os.Remove(filepath.Join(final, "complete.json"))
			case "receipt_invalid":
				_ = os.WriteFile(filepath.Join(final, "complete.json"), []byte("{"), 0600)
			case "source_mismatch":
				source = "different"
			case "executable_missing":
				_ = os.Remove(path)
			case "executable_changed":
				_ = os.WriteFile(path, []byte("different executable"), 0755)
			}
			if _, err := verifyCachedBundle(final, source); err == nil {
				t.Fatal("invalid immutable bundle was accepted")
			}
		})
	}
}

func TestImmutableBundleReportsBlockedPathsAndChangedSourceBeforePublication(t *testing.T) {
	for _, failure := range []string{"blocked_root", "blocked_lock", "source_removed", "source_changed", "files_missing", "receipt_blocked", "publish_conflict"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "source")
			_ = os.WriteFile(src, []byte("fixture"), 0755)
			source, _ := bundleFileHash(src)
			root := filepath.Join(dir, "zwai", "bundles")
			if failure == "blocked_root" {
				_ = os.WriteFile(filepath.Join(dir, "zwai"), []byte("blocked"), 0600)
			}
			if failure == "blocked_lock" {
				_ = os.MkdirAll(filepath.Join(root, source+".lock"), 0700)
			}
			previous := writeCacheBundle
			defer func() { writeCacheBundle = previous }()
			if failure != "blocked_root" && failure != "blocked_lock" {
				writeCacheBundle = func(executable, app, version string) error {
					if failure == "files_missing" {
						return nil
					}
					if err := writeAppBundle(executable, app, version); err != nil {
						return err
					}
					switch failure {
					case "source_removed":
						return os.Remove(executable)
					case "source_changed":
						return os.WriteFile(executable, []byte("changed"), 0755)
					case "receipt_blocked":
						return os.Mkdir(filepath.Join(filepath.Dir(app), "complete.json"), 0700)
					case "publish_conflict":
						return os.MkdirAll(filepath.Join(root, source, "occupied"), 0700)
					}
					return nil
				}
			}
			if _, err := prepareBundle(src, dir); err == nil {
				t.Fatalf("preparation hid %s", failure)
			}
		})
	}
	if _, err := bundleFileHash(t.TempDir()); err == nil {
		t.Fatal("directory accepted as executable")
	}
	file := filepath.Join(t.TempDir(), "unreadable")
	_ = os.WriteFile(file, []byte("no access"), 0000)
	if _, err := bundleFileHash(file); err == nil {
		t.Fatal("unreadable executable accepted")
	}
}

func TestPrepareBundleFailsWhenSourceIsMissing(t *testing.T) {
	if _, err := prepareBundle(filepath.Join(t.TempDir(), "nope"), t.TempDir()); err == nil {
		t.Fatal("missing source must fail")
	}
}

func TestWriteAppBundleFailsWhenSourceIsMissing(t *testing.T) {
	err := writeAppBundle(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "zwai.app"), "")
	if err == nil {
		t.Fatal("a missing executable must fail")
	}
}

func TestWriteAppBundleRejectsBlockedIconResources(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.WriteFile(src, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "zwai.app")
	contents := filepath.Join(app, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contents, "Resources"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAppBundle(src, app, "1.2.3"); err == nil {
		t.Fatal("missing Finder icon resources must fail packaging")
	}
}

func TestWriteAppBundleReportsBlockedBundlePaths(t *testing.T) {
	for _, blocked := range []string{"MacOS", "Info.plist", "AppIcon.icns"} {
		t.Run(blocked, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "source")
			if err := os.WriteFile(src, []byte("app"), 0o755); err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(dir, "zwai.app")
			var path string
			switch blocked {
			case "MacOS":
				path = filepath.Join(app, "Contents", "MacOS")
			case "Info.plist":
				path = filepath.Join(app, "Contents", "Info.plist")
			default:
				path = filepath.Join(app, "Contents", "Resources", "AppIcon.icns")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if blocked == "MacOS" {
				if err := os.WriteFile(path, []byte("blocked"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := writeAppBundle(src, app, "1.2.3"); err == nil {
				t.Fatal("packaging must report a blocked bundle path")
			}
		})
	}
}

func TestWriteAppBundleRejectsInvalidVersion(t *testing.T) {
	if err := writeAppBundle("unused", filepath.Join(t.TempDir(), "zwai.app"), "1.2.3-dirty"); err == nil {
		t.Fatal("an app bundle must not claim a dirty release version")
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
