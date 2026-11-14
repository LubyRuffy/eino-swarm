package lease

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLeaseRecordReaderKeepsTheCompleteBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record")
	want := []byte(" {\"startup\":\"ready\"}\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readRecordFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read %q, want %q, err=%v", got, want, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("reader retained its file handle: %v", err)
	}
	if _, err := readRecordFile(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing lease error = %v", err)
	}
}
