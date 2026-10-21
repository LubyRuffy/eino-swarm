//go:build unix

package frontend

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProcessActiveDistinguishesLiveAndUnreapedMembers(t *testing.T) {
	for _, tc := range []struct {
		stat   string
		active bool
		err    bool
	}{
		{"123 (node) Z 1 7 0", false, false},
		{"123 (node) X 1 7 0", false, false},
		{"123 (node) x 1 7 0", false, false},
		{"123 (node) S 1 7 0", true, false},
		{"123 (node) D 1 7 0", true, false},
		{"123 (node) R 1 8 0", false, false},
		{"123 (node worker) R 1 7 0", true, false},
		{"123 (node ) worker) R 1 7 0", true, false},
		{"broken stat", false, true},
		{"123 (node) R 1", false, true},
		{"123 (node) bad 1 7", false, true},
		{"123 (node) R 1 invalid", false, true},
	} {
		t.Run(tc.stat, func(t *testing.T) {
			active, err := buildProcessActive([]byte(tc.stat), 7)
			if active != tc.active || (err != nil) != tc.err {
				t.Fatalf("active=%v err=%v, want active=%v err=%v", active, err, tc.active, tc.err)
			}
		})
	}
}

func TestBuildGroupIgnoresUnreapedZombiesButWaitsForLiveWriters(t *testing.T) {
	proc := t.TempDir()
	mustWrite(t, filepath.Join(proc, "123", "stat"), "123 (npm) Z 1 7 0")
	mustWrite(t, filepath.Join(proc, "124", "stat"), "124 (node) Z 1 7 0")
	mustWrite(t, filepath.Join(proc, "125", "stat"), "125 (other) R 1 8 0")
	mustWrite(t, filepath.Join(proc, "not-a-pid", "stat"), "ignored")
	mustWrite(t, filepath.Join(proc, "126"), "not a directory")
	if err := os.Mkdir(filepath.Join(proc, "127"), 0o755); err != nil {
		t.Fatal(err)
	}
	if active, err := buildGroupHasLiveProcess(proc, 7); err != nil || active {
		t.Fatalf("unreaped zombies kept the build lock forever: active=%v err=%v", active, err)
	}
	mustWrite(t, filepath.Join(proc, "128", "stat"), "128 (node) S 1 7 0")
	if active, err := buildGroupHasLiveProcess(proc, 7); err != nil || !active {
		t.Fatalf("a remaining writer was accepted as stopped: active=%v err=%v", active, err)
	}
}

func TestBuildProcessScanReportsUnreadableOrInvalidState(t *testing.T) {
	if _, err := buildGroupHasLiveProcess(filepath.Join(t.TempDir(), "missing"), 7); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing proc root lost its error: %v", err)
	}
	proc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proc, "123", "stat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := buildGroupHasLiveProcess(proc, 7); err == nil {
		t.Fatal("an unreadable stat was mistaken for a stopped group")
	}
	if err := os.Remove(filepath.Join(proc, "123", "stat")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(proc, "123", "stat"), "invalid")
	if _, err := buildGroupHasLiveProcess(proc, 7); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("invalid state was mistaken for a stopped group: %v", err)
	}
}
