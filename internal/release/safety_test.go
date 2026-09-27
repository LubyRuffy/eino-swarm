package release

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExistingAssetsAreNeverOverwritten(t *testing.T) {
	dir := writePair(t, "1.2.3")
	mutated := false
	err := Publish(Options{Version: "1.2.3", Dir: dir, LookPath: func(string) (string, error) { return "gh", nil }, Run: func(_ string, args ...string) (string, error) {
		if len(args) > 1 && (args[1] == "upload" || args[1] == "create") {
			mutated = true
			return "", nil
		}
		return `{"tagName":"v1.2.3","assets":[{"name":"zwai-1.2.3-android.apk","digest":"sha256:different"}]}`, nil
	}})
	if err == nil || mutated || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("err=%v mutation=%v", err, mutated)
	}
}

func TestSameDigestRetrySkipsUpload(t *testing.T) {
	dir := writePair(t, "1.2.3")
	err := Publish(Options{Version: "1.2.3", Dir: dir, LookPath: func(string) (string, error) { return "gh", nil }, Run: func(_ string, args ...string) (string, error) {
		if len(args) > 1 && args[1] != "view" {
			t.Fatal("retry attempted mutation")
		}
		return `{"tagName":"v1.2.3","assets":[{"name":"zwai-1.2.3-android.apk","digest":"sha256:2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"},{"name":"zwai-1.2.3-darwin-arm64.zip","digest":"sha256:2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"}]}`, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlatformAssetsAllowIndependentDelivery(t *testing.T) {
	dir := writePair(t, "1.2.3")
	for _, platform := range []string{"android", "macos"} {
		files, err := platformAssets(dir, "1.2.3", platform)
		if err != nil || len(files) != 1 {
			t.Fatalf("%s: %v %v", platform, files, err)
		}
	}
	for _, args := range [][3]string{{dir, "bad", "android"}, {dir, "1.2.3", "ios"}, {t.TempDir(), "1.2.3", "android"}, {t.TempDir(), "1.2.3", "macos"}} {
		if _, err := platformAssets(args[0], args[1], args[2]); err == nil {
			t.Fatal(args)
		}
	}
}

func TestPartialCreatePinsSourceAndVerifiesDigest(t *testing.T) {
	dir := writePair(t, "1.2.3")
	calls := 0
	err := Publish(Options{Version: "1.2.3", Dir: dir, Platform: "android", Target: "source", LookPath: func(string) (string, error) { return "gh", nil }, Run: func(_ string, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "not found", fmt.Errorf("missing")
		}
		if args[1] == "create" {
			if !strings.Contains(strings.Join(args, " "), "--target source") {
				t.Fatal(args)
			}
			return "", nil
		}
		return `{"tagName":"v1.2.3","assets":[{"name":"zwai-1.2.3-android.apk","digest":"sha256:2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"}]}`, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssetVerificationRejectsWrongTagAndLocalReadFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.apk")
	for _, body := range []string{`{"tagName":"v9.0.0"}`, `{"tagName":"v1.2.3","assets":[{"name":"missing.apk","digest":"sha256:x"}]}`} {
		err := verify(func(string, ...string) (string, error) { return body, nil }, "v1.2.3", []string{file})
		if err == nil {
			t.Fatal(body)
		}
	}
	err := verify(func(string, ...string) (string, error) { return "", fmt.Errorf("network") }, "v1.2.3", []string{file})
	if err == nil {
		t.Fatal("missing transport error")
	}
}
