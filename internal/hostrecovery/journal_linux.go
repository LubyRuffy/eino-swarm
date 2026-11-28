package hostrecovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Image identifies the executable before it is allowed to open a data slot.
type Image struct {
	Path, Version, SHA256 string
}

// Request is written only after the predecessor durably checkpoints its work.
type Request struct {
	OperationID, Peer, Nonce string
	Candidate                Image
}

// Trial keeps the original data identity through a possibly interrupted swap.
type Trial struct {
	Request Request
	Data    *DataIdentity
}

// Preparing identifies a reserved adjacent clone while the old data remains live.
type Preparing struct {
	Request Request
	Data    DataPreparation
}

// Journal is written by the stable supervisor, never by a candidate.
type Journal struct {
	Active       Image
	Preparing    *Preparing `json:",omitempty"`
	Trial        *Trial
	Retired      *DataIdentity
	RetiredImage *Image
	Completed    string
}

// Launch binds an inherited child to the stable supervisor and one attempt.
type Launch struct {
	ParentPID      int
	DataDir, Nonce string
	Unit           string
	ListenerFD     int
	Trial          *Request
	Protected      bool
}

// Proof is generated only by the candidate's current encrypted phone link
// after the initiating phone returns its candidate-specific challenge.
type Proof struct {
	OperationID, Nonce, Peer string
}

const (
	journalName = "deployment.json"
	requestName = "request.json"
	proofName   = "proof.json"
)

// StateDir is outside the replaceable data directory and executable slots.
func StateDir(dataDir string) string { return filepath.Clean(dataDir) + ".host-recovery" }

// Nonce returns a single-attempt 256-bit identity.
func Nonce() string {
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err) // crypto/rand cannot continue safely if the OS RNG fails.
	}
	return hex.EncodeToString(id[:])
}

// Managed fails closed for unreadable or incomplete deployment state.
func Managed(dataDir string) bool {
	_, err := os.Lstat(StateDir(dataDir))
	return !errors.Is(err, os.ErrNotExist)
}

// Lock admits exactly one supervisor for this stable data path.
func Lock(dir string) (*os.File, error) {
	dataDir, ok := strings.CutSuffix(dir, ".host-recovery")
	if !ok || dataDir == "" || StateDir(dataDir) != dir {
		return nil, errors.New("invalid host recovery directory")
	}
	lock, err := Guard(dataDir, true)
	if err != nil {
		return nil, err
	}
	if err := initializeStateDir(dir); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func initializeStateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !owned(info) {
		return errors.New("the host recovery directory must be private and locally owned")
	}
	return syncDirectory(filepath.Dir(dir))
}

// Guard is shared by ordinary engines and exclusive to the supervisor. The
// guard lives outside the data image so it cannot move during a trial swap.
func Guard(dataDir string, exclusive bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(dataDir), 0o700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(StateDir(dataDir)+".guard", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "host recovery lock")
	if err := checkPrivateFile(file); err != nil {
		file.Close()
		return nil, err
	}
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	if err := unix.Flock(fd, mode|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another launch owns this host data directory: %w", err)
	}
	return file, nil
}

// ImageAt computes the immutable identity of a local executable.
func ImageAt(path, version string) (Image, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || version == "" {
		return Image{}, errors.New("a host image needs an absolute path and version")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return Image{}, err
	}
	file := os.NewFile(uintptr(fd), "host image")
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Image{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !owned(info) {
		return Image{}, errors.New("the host image must be an owned executable (0755)")
	}
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(file, (512<<20)+1))
	if err != nil {
		return Image{}, err
	}
	if n <= 0 || n > 512<<20 || n != info.Size() {
		return Image{}, errors.New("invalid host image size")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || !after.ModTime().Equal(info.ModTime()) || after.Size() != n {
		return Image{}, errors.Join(errors.New("the host image changed during verification"), err)
	}
	return Image{Path: path, Version: version, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// VerifyImage refuses a corrupted or substituted executable.
func VerifyImage(image Image) error {
	observed, err := ImageAt(image.Path, image.Version)
	if err != nil {
		return err
	}
	if observed.SHA256 != image.SHA256 {
		return errors.New("the host image changed after verification")
	}
	return nil
}

// LoadJournal reads the last committed supervisor decision.
func LoadJournal(dir string) (Journal, error) {
	journal, err := readRecord[Journal](dir, journalName)
	if err == nil {
		err = journal.validatePhase()
	}
	return journal, err
}

// SaveJournal persists a decision before the supervisor may send the child its
// activation acknowledgment.
func SaveJournal(dir string, journal Journal) error {
	if err := journal.validatePhase(); err != nil {
		return err
	}
	return writeRecord(dir, journalName, journal)
}

func (journal Journal) validatePhase() error {
	if (journal.Preparing != nil && journal.Trial != nil) ||
		(journal.Completed != "" && (journal.Preparing != nil || journal.Trial != nil)) {
		return errors.New("the host recovery journal contains conflicting phases")
	}
	return nil
}

// LoadRequest reads the predecessor's submitted operation.
func LoadRequest(dir string) (Request, error) { return readRecord[Request](dir, requestName) }

// SaveRequest publishes one checkpointed operation for the stable owner.
func SaveRequest(dir string, request Request) error { return writeRecord(dir, requestName, request) }

// RemoveRequest retires the submitted operation without replay on reboot.
func RemoveRequest(dir string) error { return removeRecord(dir, requestName) }

// ReadProof reads the candidate's round-trip receipt.
func ReadProof(dir string) (Proof, error) { return readRecord[Proof](dir, proofName) }

// SaveProof persists the candidate's round-trip receipt.
func SaveProof(dir string, proof Proof) error { return writeRecord(dir, proofName, proof) }

func readRecord[T any](dir, name string) (T, error) {
	var record T
	fd, err := unix.Open(filepath.Join(dir, name), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return record, err
	}
	file := os.NewFile(uintptr(fd), "host recovery record")
	defer file.Close()
	if err := checkPrivateFile(file); err != nil {
		return record, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return record, err
	}
	if len(raw) > 64<<10 {
		return record, errors.New("host recovery record exceeded its size limit")
	}
	return record, json.Unmarshal(raw, &record)
}

func writeRecord(dir, name string, record any) error {
	stage, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	err = json.NewEncoder(stage).Encode(record)
	if err == nil {
		err = stage.Sync()
	}
	if err := errors.Join(err, stage.Close()); err != nil {
		return err
	}
	if err := os.Rename(stage.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func removeRecord(dir, name string) error {
	err := os.Remove(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(dir)
}

func checkPrivateFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !owned(info) || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("invalid host recovery file ownership or mode")
	}
	return nil
}

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
