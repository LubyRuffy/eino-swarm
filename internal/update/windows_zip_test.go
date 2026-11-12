package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsZipContainsOnlyTheExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "source.exe")
	payload := []byte("MZ-not-validated-here")
	if err := os.WriteFile(exe, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.zip")
	if err := ZipWindowsExe(exe, dest); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "zwai.exe" {
		t.Fatalf("entries %+v", reader.File)
	}
	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got := make([]byte, len(payload)+1)
	n, err := file.Read(got)
	if n != len(payload) || string(got[:n]) != string(payload) {
		t.Fatalf("n=%d err=%v body=%q", n, err, got[:n])
	}
	if err := ZipWindowsExe(dir, filepath.Join(dir, "bad.zip")); err == nil {
		t.Fatal("a directory was accepted as the executable")
	}
}
