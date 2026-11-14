//go:build windows

package lease

import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWindowsLeasePublicationSurvivesAnOpenDiscoveryReader(t *testing.T) {
	dir := t.TempDir()
	before := Record{PID: os.Getpid(), URL: "http://127.0.0.1", Startup: "restoring"}
	if err := Publish(dir, before); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fileName)
	f, err := openRecordFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	after := before
	after.Startup = "ready"
	// The desktop can be reading the first lease exactly when startup publishes
	// readiness. Its open handle must not abort the only new engine.
	if err := Publish(dir, after); err != nil {
		t.Fatalf("publishing readiness while discovery reads the old record: %v", err)
	}
	oldBody, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	newBody, err := readRecordFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body []byte
		want string
	}{{oldBody, "restoring"}, {newBody, "ready"}} {
		var record Record
		if err := json.Unmarshal(tc.body, &record); err != nil || record.Startup != tc.want {
			t.Fatalf("record=%q, want complete %s record, err=%v", tc.body, tc.want, err)
		}
	}
}

func TestWindowsOrdinaryFileReaderReproducesTheStartupFailure(t *testing.T) {
	dir := t.TempDir()
	if err := Publish(dir, Record{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	source := filepath.Join(dir, "replacement")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	err = os.Rename(source, filepath.Join(dir, fileName))
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32) /* ERROR_SHARING_VIOLATION */) {
		t.Fatalf("ordinary Windows read handle should reproduce the replacement refusal: %v", err)
	}
}

func TestWindowsLeaseReaderPreservesFileErrors(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), "invalid\x00path"} {
		_, err := openRecordFile(path)
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) {
			t.Fatalf("opening %q returned %v, want the original file error", path, err)
		}
	}
}

func TestWindowsLeaseReplacementRetainsTheOldRecordOnError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, tc := range []struct{ source, target string }{
		{"invalid\x00path", target}, {source, "invalid\x00path"},
		{filepath.Join(dir, "missing"), target}, {source, target},
	} {
		err := replaceRecordFile(tc.source, tc.target)
		var linkErr *os.LinkError
		if !errors.As(err, &linkErr) {
			t.Fatalf("replacement %q -> %q: %v", tc.source, tc.target, err)
		}
		body, err := os.ReadFile(target)
		if err != nil || string(body) != "old" {
			t.Fatalf("failed replacement changed old record: %q, %v", body, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatalf("replacement leaked source handle: %v", err)
	}
}

func TestWindowsLeasePathsKeepLongAndNetworkIdentities(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\lease\engine.json`, `\\?\C:\lease\engine.json`},
		{`\\server\share\engine.json`, `\\?\UNC\server\share\engine.json`},
		{`\\?\C:\lease\engine.json`, `\\?\C:\lease\engine.json`},
		{`\\.\C:\lease\engine.json`, `\\.\C:\lease\engine.json`},
	} {
		got, err := recordPath(tc.path)
		if err != nil || windows.UTF16PtrToString(got) != tc.want {
			t.Fatalf("path %q => %v, %v", tc.path, got, err)
		}
	}
	dir := filepath.Join(t.TempDir(), strings.Repeat("long-path-", 12), strings.Repeat("nested-", 16))
	if err := Publish(dir, Record{PID: os.Getpid(), Startup: "restoring"}); err != nil {
		t.Fatal(err)
	}
	f, err := openRecordFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := Publish(dir, Record{PID: os.Getpid(), Startup: "ready"}); err != nil {
		t.Fatal(err)
	}
	body, err := readRecordFile(filepath.Join(dir, fileName))
	if err != nil || !strings.Contains(string(body), `"startup":"ready"`) {
		t.Fatalf("long path record=%q, err=%v", body, err)
	}
}
