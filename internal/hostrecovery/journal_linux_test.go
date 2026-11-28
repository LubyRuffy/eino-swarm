package hostrecovery

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func createJournalImage(t *testing.T, path, version, bytes string) Image {
	t.Helper()
	if err := os.WriteFile(path, []byte(bytes), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	image, err := ImageAt(path, version)
	if err != nil {
		t.Fatal(err)
	}
	expect := fmt.Sprintf("%x", sha256.Sum256([]byte(bytes)))
	if image.Path != path || image.Version != version || image.SHA256 != expect {
		t.Fatalf("image identity = %+v; want SHA256 %s", image, expect)
	}
	return image
}

func requireRecoveryLock(t *testing.T, state string) *os.File {
	t.Helper()
	lock, err := Lock(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	return lock
}

func TestRecoveryJournalSeparatesRequestProofAndDurablePromotion(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	state := StateDir(data)
	old := createJournalImage(t, filepath.Join(dir, "old"), "1.0.0", "previous image")
	candidate := createJournalImage(t, filepath.Join(dir, "candidate"), "1.0.1", "candidate image")
	if Managed(data) {
		t.Fatal("new data unexpectedly appears managed")
	}
	lock := requireRecoveryLock(t, state)
	if !Managed(data) {
		t.Fatal("ordinary boot must notice the durable boot owner")
	}
	request := Request{
		OperationID: "ace029d4-1748-48a1-9aed-ef2ed6821220",
		Nonce:       Nonce(), Peer: "initiating paired client", Candidate: candidate,
	}
	previous := Journal{Active: old}
	if err := SaveJournal(state, previous); err != nil {
		t.Fatal(err)
	}
	if err := SaveRequest(state, request); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	requireRecoveryLock(t, state)
	loadedRequest, err := LoadRequest(state)
	if err != nil || loadedRequest != request {
		t.Fatalf("supervisor handover lost the confirmed request: %+v, %v", loadedRequest, err)
	}
	if boot, err := LoadJournal(state); err != nil || boot != previous {
		t.Fatalf("a queued request changed the active image: %+v, %v", boot, err)
	}
	dataPair := DataIdentity{
		DataDir: data, SnapshotDir: filepath.Join(dir, "previous"),
		Original: FileID{Dev: 1, Ino: 10, Token: Nonce()},
		Trial:    FileID{Dev: 1, Ino: 11, Token: Nonce()},
	}
	trial := Journal{Active: old, Trial: &Trial{Request: request, Data: &dataPair}}
	if err := SaveJournal(state, trial); err != nil {
		t.Fatal(err)
	}
	proof := Proof{OperationID: request.OperationID, Nonce: request.Nonce, Peer: request.Peer}
	if err := SaveProof(state, proof); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadProof(state); err != nil || got != proof {
		t.Fatalf("candidate proof lost identity: %+v, %v", got, err)
	}
	if boot, err := LoadJournal(state); err != nil || !reflect.DeepEqual(boot, trial) {
		t.Fatalf("transport proof promoted data before a durable decision: %+v, %v", boot, err)
	}
	committed := Journal{Active: candidate, Retired: &dataPair, Completed: request.OperationID}
	if err := SaveJournal(state, committed); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RemoveRequest(state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadRequest(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("promoted operation could replay on boot: %v", err)
	}
	if boot, err := LoadJournal(state); err != nil || !reflect.DeepEqual(boot, committed) {
		t.Fatalf("promotion was not durable: %+v, %v", boot, err)
	}
	for _, name := range []string{journalName, proofName} {
		info, err := os.Stat(filepath.Join(state, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private record %s: %+v, %v", name, info, err)
		}
	}
}

func TestRecoveryJournalReadersNeverObserveTornPromotion(t *testing.T) {
	state := StateDir(filepath.Join(t.TempDir(), "data"))
	requireRecoveryLock(t, state)
	first := Journal{Active: Image{Path: "/candidate-one", Version: "1.0.0", SHA256: Nonce()}, Completed: "first"}
	second := Journal{Active: Image{Path: "/candidate-two", Version: "1.0.1", SHA256: Nonce()}, Completed: "second"}
	if err := SaveJournal(state, first); err != nil {
		t.Fatal(err)
	}
	// A crash can leave an unpublished temporary file. Boot readers still
	// see only the last complete decision at the canonical path.
	if err := os.WriteFile(filepath.Join(state, ".record-torn"), []byte("{truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(3)
	read := make(chan struct{}, 3)
	for range 3 {
		go func() {
			defer readers.Done()
			for i := 0; ; i++ {
				got, err := LoadJournal(state)
				if err != nil || got != first && got != second {
					t.Errorf("reader saw a torn decision %+v, %v", got, err)
					if i == 0 {
						read <- struct{}{}
					}
					return
				}
				if i == 0 {
					read <- struct{}{}
				}
				select {
				case <-done:
					return
				default:
				}
			}
		}()
	}
	defer func() { close(done); readers.Wait() }()
	for range 3 {
		<-read
	}
	for i := range 24 {
		next := first
		if i%2 == 0 {
			next = second
		}
		if err := SaveJournal(state, next); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryJournalRejectsUnreadableUnsafeAndMalformedRecords(t *testing.T) {
	for _, test := range []struct {
		name string
		put  func(string, string) error
	}{
		{"torn JSON", func(path, _ string) error { return os.WriteFile(path, []byte(`{"Active":`), 0o600) }},
		{"non-record JSON", func(path, _ string) error { return os.WriteFile(path, []byte(`["foreign"]`), 0o600) }},
		{"oversized", func(path, _ string) error { return os.WriteFile(path, []byte(strings.Repeat(" ", (64<<10)+1)), 0o600) }},
		{"world readable", func(path, _ string) error {
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		}},
		{"symlink", func(path, target string) error {
			if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
				return err
			}
			return os.Symlink(target, path)
		}},
		{"pipe", func(path, _ string) error { return unix.Mkfifo(path, 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			state := StateDir(filepath.Join(dir, "data"))
			requireRecoveryLock(t, state)
			if err := test.put(filepath.Join(state, journalName), filepath.Join(dir, "foreign")); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadJournal(state); err == nil {
				t.Fatal("boot accepted an invalid canonical decision")
			}
		})
	}
}

func TestRecoveryJournalWriteFailurePreservesTheActiveDecision(t *testing.T) {
	state := StateDir(filepath.Join(t.TempDir(), "data"))
	requireRecoveryLock(t, state)
	old := Journal{Active: Image{Path: "/known-good", Version: "1.0.0", SHA256: Nonce()}}
	if err := SaveJournal(state, old); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(state, requestName)
	if err := os.Mkdir(requestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(requestPath, "unrelated"), []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveRequest(state, Request{}); err == nil {
		t.Fatal("a directory was mistaken for the canonical request file")
	}
	if err := RemoveRequest(state); err == nil {
		t.Fatal("a bad request path was silently treated as retired")
	}
	if err := SaveProof(filepath.Join(state, "missing"), Proof{}); err == nil {
		t.Fatal("a proof without a state directory was accepted")
	}
	if got, err := LoadJournal(state); err != nil || got != old {
		t.Fatalf("failed state write changed the active image: %+v, %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(requestPath, "unrelated"))
	if err != nil || string(raw) != "preserved" {
		t.Fatalf("foreign path content lost: %q, %v", raw, err)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 2 {
		t.Fatalf("failed write leaked an unpublished state file: %v, %v", entries, err)
	}
}

func TestRecoverySharedAndExclusiveGuardCrossProcess(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	state := StateDir(data)
	ordinary, err := Guard(data, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ordinary.Close() })
	if _, err := Lock(state); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatalf("an ordinary engine failed to exclude supervisor: %v", err)
	}
	if Managed(data) {
		t.Fatal("failed supervisor acquisition marked the data as managed")
	}
	runGuardChild(t, data, "shared")
	runGuardChild(t, data, "exclusive-blocked")
	if err := ordinary.Close(); err != nil {
		t.Fatal(err)
	}
	sup := requireRecoveryLock(t, state)
	runGuardChild(t, data, "shared-blocked")
	runGuardChild(t, data, "exclusive-blocked")
	if err := sup.Close(); err != nil {
		t.Fatal(err)
	}
	runGuardChild(t, data, "exclusive")
}

func TestRecoveryGuardRefusesUntrustedStatePaths(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	state := StateDir(data)
	if _, err := Lock(filepath.Join(dir, "other")); err == nil {
		t.Fatal("unrelated state path acquired a supervisor lock")
	}
	unrelated := filepath.Join(dir, "unrelated")
	if err := os.WriteFile(unrelated, []byte("outside state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unrelated, state+".guard"); err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(state); err == nil {
		t.Fatal("guard followed an unrelated symlink")
	}
	if err := os.Remove(state + ".guard"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state+".guard", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state+".guard", 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Guard(data, true); err == nil {
		t.Fatal("nonprivate guard path was accepted")
	}
	if err := os.Chmod(state+".guard", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(state); err == nil {
		t.Fatal("nonprivate deployment directory was accepted")
	}
	runGuardChild(t, data, "exclusive")
}

func runGuardChild(t *testing.T, data, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, path, "-test.run", "^TestRecoveryGuardChild$")
	child.Env = append(os.Environ(), "ZWAI_TEST_GUARD_DATA="+data, "ZWAI_TEST_GUARD_MODE="+mode)
	if raw, err := child.CombinedOutput(); err != nil {
		t.Fatalf("foreign process %s: %v\n%s", mode, err, raw)
	}
}

func TestRecoveryGuardChild(t *testing.T) {
	data, mode := os.Getenv("ZWAI_TEST_GUARD_DATA"), os.Getenv("ZWAI_TEST_GUARD_MODE")
	if data == "" {
		return
	}
	guard, err := Guard(data, strings.HasPrefix(mode, "exclusive"))
	if guard != nil {
		defer guard.Close()
	}
	if strings.HasSuffix(mode, "blocked") {
		if !errors.Is(err, unix.EWOULDBLOCK) {
			t.Fatalf("guard admitted a conflicting owner: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryImageProofBindsExecutableContentsNotFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zwai")
	image := createJournalImage(t, path, "1.0.0", "first image")
	if err := VerifyImage(image); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other image"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyImage(image); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("same length replacement had accepted identity: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageAt(path, "1.0.0"); err == nil {
		t.Fatal("an image without executable mode passed admission")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageAt(alias, "1.0.0"); err == nil {
		t.Fatal("followed a substituted image symlink")
	}
	aliased := image
	aliased.Path = alias
	if err := VerifyImage(aliased); err == nil {
		t.Fatal("a verified image reference followed a substituted symlink")
	}
	if _, err := ImageAt("relative", "1.0.0"); err == nil {
		t.Fatal("resolved an image relative to the working directory")
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageAt(empty, "1.0.0"); err == nil {
		t.Fatal("accepted an empty executable")
	}
}
