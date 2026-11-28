package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	// ErrHostUpdateInput denotes an invalid operation identity or version.
	ErrHostUpdateInput = errors.New("invalid host update operation or version")
	// ErrHostUpdateNotFound means there is no receipt for this operation.
	ErrHostUpdateNotFound = errors.New("host update operation not found")
	hostOperationID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// HostUpdate is the selected host's private installation receipt. The phone
// projection contains only the operation identity, stage and download counts.
type HostUpdate struct {
	OperationID     string `json:"operation_id"`
	Version         string `json:"version"`
	Stage           string `json:"stage"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Error           string `json:"error,omitempty"`
}

// HostJob owns one attempt's receipt until it transfers installer ownership
// to a replacement coordinator. At most one process writes it at a time.
type HostJob struct {
	path string
	mu   sync.Mutex
	info HostUpdate
	peer string
}

// Peer identifies the encrypted phone that confirmed a protected update.
func (j *HostJob) Peer() string { return j.peer }

func hostJobPath(dataDir, id string) (string, error) {
	id = strings.ToLower(id)
	if !hostOperationID.MatchString(id) {
		return "", ErrHostUpdateInput
	}
	return filepath.Join(dataDir, "host-updates", id+".json"), nil
}

// OpenHostJob reads the last durable state, including after engine replacement.
func OpenHostJob(dataDir, id string) (*HostJob, error) {
	path, err := hostJobPath(dataDir, id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrHostUpdateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read host update receipt: %w", err)
	}
	var info HostUpdate
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("decode host update receipt: %w", err)
	}
	if info.OperationID != strings.ToLower(id) || Canonical(info.Version) != info.Version ||
		info.Version == "" || !hostUpdateStage(info.Stage) {
		return nil, errors.New("invalid host update receipt")
	}
	return &HostJob{path: path, info: info}, nil
}

// Snapshot returns independent current progress.
func (j *HostJob) Snapshot() HostUpdate {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.info
}

// ReportDownload updates measured bytes for live readers without issuing a
// disk write for every buffer. The next stage durably captures the counts.
func (j *HostJob) ReportDownload(downloaded, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.info.DownloadedBytes, j.info.TotalBytes = downloaded, total
}

// SetStage durably changes the stage before publishing it to readers.
func (j *HostJob) SetStage(stage string) error {
	return j.SetError(stage, nil)
}

// SetError durably changes the stage and records a phone-safe failure reason.
// Callers log underlying system errors on the host.
func (j *HostJob) SetError(stage string, reason error) error {
	if !hostUpdateStage(stage) {
		return errors.New("invalid host update stage")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	next := j.info
	next.Stage, next.Error = stage, ""
	if reason != nil {
		next.Error = reason.Error()
	}
	if err := writeHostJob(j.path, next); err != nil {
		return err
	}
	j.info = next
	return nil
}

func hostUpdateStage(stage string) bool {
	switch stage {
	case "checking", "downloading", "installing", "saving", "restarting",
		"restoring", "completed", "failed", "pending":
		return true
	default:
		return false
	}
}

func writeHostJob(path string, info HostUpdate) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()
	tmp, err := os.CreateTemp(dir, ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // only ever this attempt's private temporary file
	writeErr := json.NewEncoder(tmp).Encode(info)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return dirFile.Sync()
}
