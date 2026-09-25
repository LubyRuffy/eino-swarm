package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightRejectsADirtyVersionBeforeAnyUpload(t *testing.T) {
	called := false
	err := Preflight(Options{
		Version: "1.2.3-dirty",
		LookPath: func(string) (string, error) {
			called = true
			return "gh", nil
		},
	})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if err := Preflight(Options{Version: "1.2.3", LookPath: func(string) (string, error) {
		return "", errors.New("missing")
	}}); err == nil || !strings.Contains(err.Error(), "gh") {
		t.Fatal(err)
	}
}

func TestAssetsRequireTheAPKAndAMacZip(t *testing.T) {
	dir := t.TempDir()
	if _, err := Assets(dir, "1.2.3"); err == nil || !strings.Contains(err.Error(), "apk") {
		t.Fatal(err)
	}
	apk := filepath.Join(dir, AndroidAPKName("v1.2.3"))
	if err := os.WriteFile(apk, []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Assets(dir, "1.2.3"); err == nil || !strings.Contains(err.Error(), "zip") {
		t.Fatal(err)
	}
	zip := filepath.Join(dir, "zwai-1.2.3-darwin-arm64.zip")
	if err := os.WriteFile(zip, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zwai-1.2.3-darwin-other.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Assets(dir, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != apk || got[1] != zip {
		t.Fatalf("assets %v", got)
	}
}

func TestPublishCreatesAReleaseWithBothFiles(t *testing.T) {
	dir := writePair(t, "1.2.3")
	var calls []string
	err := Publish(Options{
		Version:  "v1.2.3",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			if strings.Contains(calls[len(calls)-1], "release view") && len(calls) == 1 {
				return "release not found", errors.New("exit 1")
			}
			if strings.HasPrefix(calls[len(calls)-1], "release view") {
				return `{"tagName":"v1.2.3","assets":[{"name":"zwai-1.2.3-android.apk"},{"name":"zwai-1.2.3-darwin-arm64.zip"}]}`, nil
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "release create v1.2.3") || !strings.Contains(joined, "zwai-1.2.3-android.apk") || !strings.Contains(joined, "zwai-1.2.3-darwin-arm64.zip") {
		t.Fatalf("calls:\n%s", joined)
	}
	if strings.Contains(joined, "release delete") {
		t.Fatal("publish must not delete a release")
	}
}

func TestPublishUploadsOntoAnExistingReleaseAndFailsClosed(t *testing.T) {
	dir := writePair(t, "1.2.3")
	view := `{"tagName":"v1.2.3","assets":[{"name":"zwai-1.2.3-android.apk"}]}`
	err := Publish(Options{
		Version:  "1.2.3",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if strings.HasPrefix(line, "release view") && strings.Contains(line, "tagName") && !strings.Contains(line, "assets") {
				return `{"tagName":"v1.2.3"}`, nil
			}
			if strings.HasPrefix(line, "release upload") {
				return "", nil
			}
			if strings.Contains(line, "assets") {
				return view, nil
			}
			return "", errors.New(line)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatal(err)
	}
}

func TestPublishStopsWhenTheUploadFails(t *testing.T) {
	dir := writePair(t, "2.0.0")
	verified := false
	err := Publish(Options{
		Version:  "2.0.0",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if strings.Contains(line, "release delete") {
				t.Fatal("delete")
			}
			if strings.HasPrefix(line, "release view") && !strings.Contains(line, "assets") {
				return `{"tagName":"v2.0.0"}`, nil
			}
			if strings.HasPrefix(line, "release upload") {
				return "denied", errors.New("exit 1")
			}
			verified = true
			return "", nil
		},
	})
	if err == nil || verified {
		t.Fatalf("err=%v verified=%v", err, verified)
	}
}

func TestPublishDoesNotUploadWhenAFileIsMissing(t *testing.T) {
	err := Publish(Options{
		Version:  "1.2.3",
		Dir:      t.TempDir(),
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(string, ...string) (string, error) {
			t.Fatal("gh must not run")
			return "", nil
		},
	})
	if err == nil {
		t.Fatal("missing apk")
	}
}

func TestPublishReportsACreateFailureAndABadVerify(t *testing.T) {
	dir := writePair(t, "1.2.3")
	err := Publish(Options{
		Version:  "1.2.3",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if strings.Contains(line, "release view") {
				return "permission denied", errors.New("exit 1")
			}
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "view") {
		t.Fatal(err)
	}
	err = Publish(Options{
		Version:  "1.2.3",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if strings.HasPrefix(line, "release view") && !strings.Contains(line, "assets") {
				return "release not found", errors.New("exit 1")
			}
			if strings.HasPrefix(line, "release create") {
				return "nope", errors.New("exit 1")
			}
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "create") {
		t.Fatal(err)
	}
	err = Publish(Options{
		Version:  "1.2.3",
		Dir:      dir,
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(_ string, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if strings.Contains(line, "assets") {
				return "not-json", nil
			}
			if strings.HasPrefix(line, "release view") {
				return `{"tagName":"v1.2.3"}`, nil
			}
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatal(err)
	}
}

func TestRunCommandCapturesOutput(t *testing.T) {
	out, err := runCommand("sh", "-c", "echo ok")
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runCommand("sh", "-c", "echo no >&2; exit 1"); err == nil {
		t.Fatal("nonzero must fail")
	}
	if err := Publish(Options{Version: "1.2.3", Dir: t.TempDir()}); err == nil {
		t.Fatal("missing apk must fail before upload")
	}
}

func writePair(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{AndroidAPKName(version), "zwai-" + version + "-darwin-arm64.zip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
