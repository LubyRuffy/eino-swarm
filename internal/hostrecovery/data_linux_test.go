package hostrecovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func trialTestDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var fs unix.Statfs_t
	if err := unix.Statfs(dir, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Type != unix.EXT4_SUPER_MAGIC && fs.Type != unix.BTRFS_SUPER_MAGIC {
		t.Skip("requires an ext4 or Btrfs scratch filesystem")
	}
	path := filepath.Join(dir, "data")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func putTrialData(t *testing.T, base, body string) {
	t.Helper()
	path := filepath.Join(base, "workspaces", "nested", "data")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

func requireTrialData(t *testing.T, base, expected string) {
	t.Helper()
	path := filepath.Join(base, "workspaces", "nested", "data")
	got, err := os.ReadFile(path)
	if err != nil || string(got) != expected {
		t.Fatalf("data at %s = %q, %v; want %q", base, got, err, expected)
	}
}

func TestCheckDataSupportDoesNotCreateOrChangeSlots(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "original")
	parent := filepath.Dir(data)
	for range 2 {
		if err := CheckDataSupport(data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(data, dataTokenName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight persisted a data identity: %v", err)
	}
	if err := CheckDataSupport(filepath.Join(parent, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight accepted a missing data slot: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "data" {
		t.Fatalf("preflight created neighboring paths: %v, %v", entries, err)
	}
	requireTrialData(t, data, "original")
	if os.Geteuid() != 0 {
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		if err := CheckDataSupport(data); !errors.Is(err, unix.EACCES) {
			t.Fatalf("preflight failed to reject a read-only parent: %v", err)
		}
		if err := os.Chmod(parent, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(data, dataTokenName), []byte("bad prior identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckDataSupport(data); err == nil {
		t.Fatal("preflight did not detect corrupted identity metadata")
	}
}

func TestPreparedDataCanDisposeAnInterruptedCloneWithoutTouchingTheOriginal(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "still active")
	plan, err := PlanData(data)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(data), dataTrialPrefix+plan.TrialToken)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning must precede any creation of the trial: %v", err)
	}
	if err := DiscardUnreadyData(plan); err != nil {
		t.Fatalf("recover from an interrupted mkdir: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := DiscardUnreadyData(plan); err != nil {
		t.Fatalf("recover before the trial seal: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, dataTokenName+"-"+plan.TrialToken), []byte(plan.TrialToken[:12]), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DiscardUnreadyData(plan); err != nil {
		t.Fatalf("recover after an interrupted seal write: %v", err)
	}
	// Copy cannot complete, but it will have sealed a private trial before
	// encountering an unsafe source entry. Recovery should dispose its bytes.
	if err := os.Symlink("outside", filepath.Join(data, "unsafe")); err != nil {
		t.Fatal(err)
	}
	if _, err := ClonePreparedData(t.Context(), plan); err == nil {
		t.Fatal("an incomplete clone was accepted")
	}
	putTrialData(t, path, "partially prepared clone")
	if err := DiscardUnreadyData(plan); err != nil {
		t.Fatal(err)
	}
	if err := DiscardUnreadyData(plan); err != nil {
		t.Fatalf("resuming cleanup of the same plan: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted clone remains: %v", err)
	}
	requireTrialData(t, data, "still active")
}

func TestPreparedDataRefusesForeignDiskBytesAndAnAlreadyActivatedTrial(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "original")
	plan, err := PlanData(data)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(data), dataTrialPrefix+plan.TrialToken)
	putTrialData(t, path, "foreign bytes without a token")
	if err := DiscardUnreadyData(plan); err == nil {
		t.Fatal("a reserved name alone authorized recursive removal")
	}
	requireTrialData(t, path, "foreign bytes without a token")
	if _, err := ClonePreparedData(t.Context(), plan); !errors.Is(err, os.ErrExist) {
		t.Fatalf("clone overwrote an unexpected directory: %v", err)
	}
	if err := os.Rename(path, filepath.Join(filepath.Dir(data), "foreign")); err != nil {
		t.Fatal(err)
	}
	wrong := plan
	wrong.Original.Ino++
	if _, err := ClonePreparedData(t.Context(), wrong); err == nil {
		t.Fatal("clone started using a different original identity")
	}
	id, err := ClonePreparedData(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDataID(id.Original, plan.Original) || id.Trial.Token != plan.TrialToken {
		t.Fatalf("clone failed to preserve prepared identities: %+v vs %+v", id, plan)
	}
	if err := ActivateData(id); err != nil {
		t.Fatal(err)
	}
	if err := DiscardUnreadyData(plan); err == nil {
		t.Fatal("pre-swap disposal was authorized after the exchange")
	}
	requireTrialData(t, id.SnapshotDir, "original")
	requireTrialData(t, filepath.Join(filepath.Dir(data), "foreign"), "foreign bytes without a token")
}

func TestDataTrialRollbackUsesOriginalContentsAfterRecovery(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "known good")
	source := filepath.Join(data, "workspaces", "nested", "data")
	modtime := time.Unix(900000300, 123)
	if err := os.Chtimes(source, modtime, modtime); err != nil {
		t.Fatal(err)
	}
	attrErr := unix.Setxattr(source, "user.zwai-test", []byte("original attribute"), 0)
	if attrErr != nil && !errors.Is(attrErr, unix.ENOTSUP) {
		t.Fatal(attrErr)
	}
	id, err := PrepareData(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if sameDataID(id.Original, id.Trial) {
		t.Fatal("candidate reused the original data identity")
	}
	requireTrialData(t, id.SnapshotDir, "known good")
	cloned := filepath.Join(id.SnapshotDir, "workspaces", "nested", "data")
	info, err := os.Stat(cloned)
	if err != nil || info.Mode().Perm() != 0o640 || !info.ModTime().Equal(modtime) {
		t.Fatalf("clone lost mode or modification time: %+v, %v", info, err)
	}
	if attrErr == nil {
		var raw [40]byte
		n, err := unix.Getxattr(cloned, "user.zwai-test", raw[:])
		if err != nil || string(raw[:n]) != "original attribute" {
			t.Fatalf("clone lost its file attribute: %q, %v", raw[:n], err)
		}
	}
	if err := ActivateData(id); err != nil {
		t.Fatal(err)
	}
	if err := ActivateData(id); err != nil {
		t.Fatalf("replay of activation refused live candidate: %v", err)
	}
	if err := DiscardData(id); err == nil {
		t.Fatal("active trial could discard the previous data")
	}
	putTrialData(t, data, "candidate wrote different bytes")
	requireTrialData(t, id.SnapshotDir, "known good")
	// A reread journal survives losing all in-memory exchange information.
	// A changed device number simulates re-enumeration after boot.
	raw, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var loaded DataIdentity
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	loaded.Original.Dev++
	loaded.Trial.Dev++
	if err := RestoreData(loaded); err != nil {
		t.Fatal(err)
	}
	requireTrialData(t, data, "known good")
	requireTrialData(t, id.SnapshotDir, "candidate wrote different bytes")
	if err := DiscardData(loaded); err != nil {
		t.Fatal(err)
	}
	if err := RestoreData(loaded); err != nil {
		t.Fatalf("could not reconcile after rolling back and discarding the trial: %v", err)
	}
	if err := DiscardData(loaded); err != nil {
		t.Fatalf("trial discard was not idempotent: %v", err)
	}
}

func TestDataTrialRefusesForeignSlotsAndRetiresOnlyPreviousBackup(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "original")
	id, err := PrepareData(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := DiscardRetiredData(id); err == nil {
		t.Fatal("could retire the active original")
	}
	for _, mutate := range []func(*DataIdentity){
		func(forged *DataIdentity) { forged.Original.Ino++ },
		func(forged *DataIdentity) { forged.Original.Token = Nonce() },
		func(forged *DataIdentity) { forged.SnapshotDir = data },
	} {
		forged := id
		mutate(&forged)
		if err := ActivateData(forged); err == nil {
			t.Fatal("foreign identity authorized an exchange")
		}
		requireTrialData(t, data, "original")
	}
	if err := ActivateData(id); err != nil {
		t.Fatal(err)
	}
	putTrialData(t, data, "candidate")
	// A partially cleaned detached directory must not affect which data
	// restart reconciles. Publish the detach through the real exchange FS.
	oldTomb := filepath.Join(filepath.Dir(data), dataDiscardPrefix+id.Original.Token)
	if err := os.Rename(id.SnapshotDir, oldTomb); err != nil {
		t.Fatal(err)
	}
	requireTrialData(t, oldTomb, "original")
	if err := DiscardRetiredData(id); err != nil {
		t.Fatal(err)
	}
	requireTrialData(t, data, "candidate")
	if _, err := os.Stat(oldTomb); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("detached previous backup was left behind: %v", err)
	}
}

func TestDataTrialRejectsUnsafeTreesWithoutChangingOriginal(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(data string) error
	}{
		{"symlink", func(data string) error { return os.Symlink("../../", filepath.Join(data, "escape")) }},
		{"hard link", func(data string) error {
			return os.Link(filepath.Join(data, "workspaces", "nested", "data"), filepath.Join(data, "hardlink"))
		}},
		{"named pipe", func(data string) error { return unix.Mkfifo(filepath.Join(data, "pipe"), 0o600) }},
		{"deep tree", func(data string) error {
			return os.MkdirAll(filepath.Join(data, strings.Repeat("d/", maxDataTreeDepth+1)), 0o755)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := trialTestDataDir(t)
			putTrialData(t, data, "untouched")
			if err := test.make(data); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareData(t.Context(), data); err == nil {
				t.Fatal("unsafe source tree was admitted")
			}
			requireTrialData(t, data, "untouched")
			entries, err := os.ReadDir(filepath.Dir(data))
			if err != nil || len(entries) != 1 || entries[0].Name() != "data" {
				t.Fatalf("failed prepare left a private staging directory: %v, %v", entries, err)
			}
		})
	}
}

func TestDataTrialRefusesCancelledPreparationAndMissingFallbackToken(t *testing.T) {
	data := trialTestDataDir(t)
	putTrialData(t, data, "known good")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PrepareData(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled prepare: %v", err)
	}
	id, err := PrepareData(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(id.SnapshotDir, dataTokenName)); err != nil {
		t.Fatal(err)
	}
	if err := RestoreData(id); err == nil {
		t.Fatal("misidentified an existing corrupt slot as already cleaned")
	}
	requireTrialData(t, data, "known good")
}

func TestDataTrialRefusesNestedMountEvenWithTheSameFilesystem(t *testing.T) {
	if os.Getenv("ZWAI_RECOVERY_TEST_PRIVATE_MOUNTS") != "1" {
		t.Skip("requires an explicitly isolated mount namespace with CAP_SYS_ADMIN")
	}
	data := trialTestDataDir(t)
	putTrialData(t, data, "known good")
	path := filepath.Join(data, "nested-mount")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	putTrialData(t, other, "unrelated")
	if err := unix.Mount(other, path, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(path, unix.MNT_DETACH); err != nil {
			t.Error(err)
		}
	})
	if _, err := PrepareData(t.Context(), data); !errors.Is(err, unix.EXDEV) {
		t.Fatalf("a bind mount must refuse data cloning: %v", err)
	}
	requireTrialData(t, data, "known good")
	requireTrialData(t, other, "unrelated")
}
