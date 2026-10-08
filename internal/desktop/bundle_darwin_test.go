//go:build darwin

package desktop

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"os/exec"
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
