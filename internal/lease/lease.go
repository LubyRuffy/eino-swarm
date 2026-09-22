// Package lease is the one-engine-per-data-directory lock.
//
// Sequence numbers and the scheduler live in the process that has the
// database open. A second process must attach to that one, not open zwai.db
// itself. The lock file is the mutex; engine.json is how a later shell finds
// the URL. The kernel drops the lock when the process dies, including
// kill -9, which is what lets the next shell take over.
package lease

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	lockName = "engine.lock"
	fileName = "engine.json"
)

var (
	// ErrHeld means another live process already owns this data directory.
	ErrHeld = errors.New("lease: engine already running")
	// ErrNotRunning means there is no engine to attach to.
	ErrNotRunning = errors.New("lease: no engine")
	// ErrUnreachable means the recorded pid is alive but /api/meta did not
	// answer. The lock is still theirs; do not start a second engine.
	ErrUnreachable = errors.New("lease: engine process is alive but not answering")
	// ErrMockMismatch means the live engine was started with a different
	// --mock than this shell asked for.
	ErrMockMismatch = errors.New("lease: engine mock mode does not match")
)

// Record is the on-disk description of the engine a shell can attach to.
type Record struct {
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	Mock      bool      `json:"mock"`
	StartedAt time.Time `json:"started_at"`
}

// Hold is an exclusive lock on one data directory. Release on shutdown.
// The file stays open for the life of the hold: closing it is what unlocks.
type Hold struct {
	f   *os.File
	dir string
}

// Acquire takes the exclusive lock. The caller publishes the URL only after
// it is actually listening.
func Acquire(dataDir string) (*Hold, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("lease: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dataDir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lease: open lock: %w", err)
	}
	if err := lockExclusive(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Hold{f: f, dir: dataDir}, nil
}

// Release drops the lock and removes the published URL. A crash skips this;
// the kernel still drops the lock, and Discover treats a dead pid as stopped.
func (h *Hold) Release() {
	if h == nil {
		return
	}
	_ = os.Remove(filepath.Join(h.dir, fileName))
	_ = unlock(h.f)
	_ = h.f.Close()
}

// Publish writes the URL atomically so a reader never sees a half record.
func Publish(dataDir string, rec Record) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("lease: %w", err)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	final := filepath.Join(dataDir, fileName)
	tmp, err := os.CreateTemp(dataDir, "engine-*.json")
	if err != nil {
		return fmt.Errorf("lease: publish: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("lease: publish: %w", err)
	}
	return nil
}

type metaView struct {
	DataDir string `json:"data_dir"`
	Mock    bool   `json:"mock"`
}

// Discover reports the engine a shell should attach to.
func Discover(dataDir string, mock bool) (Record, error) {
	body, err := os.ReadFile(filepath.Join(dataDir, fileName))
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, ErrNotRunning
		}
		return Record{}, fmt.Errorf("lease: read: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(body, &rec); err != nil || rec.PID <= 0 || rec.URL == "" {
		return Record{}, ErrNotRunning
	}
	if !pidAlive(rec.PID) {
		return Record{}, ErrNotRunning
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(rec.URL + "/api/meta")
	if err != nil {
		return Record{}, ErrUnreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Record{}, ErrUnreachable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Record{}, ErrUnreachable
	}
	var meta metaView
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Record{}, ErrUnreachable
	}
	if filepath.Clean(meta.DataDir) != filepath.Clean(dataDir) {
		return Record{}, ErrNotRunning
	}
	if meta.Mock != mock {
		return Record{}, ErrMockMismatch
	}
	rec.Mock = meta.Mock
	return rec, nil
}
