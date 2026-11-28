package hostrecovery

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	dataTokenName     = ".zwai-host-recovery-identity"
	dataTrialPrefix   = ".zwai-data-trial-"
	dataDiscardPrefix = ".zwai-data-discard-"
	dataTokenLength   = 64
	maxDataTreeDepth  = 96
)

// FileID identifies a data directory across atomic exchanges and reboots.
// Dev describes the mount when prepared. The inode and token also survive
// mount renumbering on boot.
type FileID struct {
	Dev   uint64 `json:"dev"`
	Ino   uint64 `json:"ino"`
	Token string `json:"token"`
}

// DataIdentity is the durable pair a supervisor records before enabling a
// candidate to open the application's real data path.
type DataIdentity struct {
	DataDir     string `json:"data_dir"`
	SnapshotDir string `json:"snapshot_dir"`
	Original    FileID `json:"original"`
	Trial       FileID `json:"trial"`
}

// DataPreparation reserves a single adjacent trial name. The supervisor
// persists it before creating the directory or copying any data.
type DataPreparation struct {
	DataDir    string `json:"data_dir"`
	Original   FileID `json:"original"`
	TrialToken string `json:"trial_token"`
}

// CheckDataSupport validates the current data slot and its parent without
// creating, renaming or writing any files. PrepareData rechecks the tree
// after acquiring exclusive launch admission.
func CheckDataSupport(dataDir string) error {
	parent, name, err := openDataParent(dataDir)
	if err != nil {
		return err
	}
	defer parent.Close()
	data, _, err := openDataDir(parent, name)
	if err != nil {
		return err
	}
	defer data.Close()
	for _, dir := range []*os.File{parent, data} {
		if err := unix.Faccessat(int(dir.Fd()), ".", unix.W_OK|unix.X_OK, unix.AT_EACCESS); err != nil {
			return fmt.Errorf("host data path is not writable by its supervisor: %w", err)
		}
	}
	if _, err := readDataToken(data); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not prove the current host data identity: %w", err)
	}
	return nil
}

// PlanData identifies the original directory and reserves a fresh trial name
// without creating its directory. The caller must hold exclusive launch
// admission after the previous engine has exited.
func PlanData(dataDir string) (DataPreparation, error) {
	parent, name, err := openDataParent(dataDir)
	if err != nil {
		return DataPreparation{}, err
	}
	defer parent.Close()
	old, oldStat, err := openDataDir(parent, name)
	if err != nil {
		return DataPreparation{}, err
	}
	defer old.Close()
	oldToken, err := readDataToken(old)
	if errors.Is(err, os.ErrNotExist) {
		oldToken = Nonce()
		err = writeDataToken(old, oldToken, Nonce())
	}
	if err != nil {
		return DataPreparation{}, fmt.Errorf("identify original data: %w", err)
	}
	if err := parent.Sync(); err != nil {
		return DataPreparation{}, fmt.Errorf("persist original data slot: %w", err)
	}
	return DataPreparation{
		DataDir: dataDir, TrialToken: Nonce(),
		Original: FileID{Dev: uint64(oldStat.Dev), Ino: oldStat.Ino, Token: oldToken},
	}, nil
}

// ClonePreparedData seals the reserved sibling before copying any app bytes.
// The caller must persist the plan and continue holding exclusive admission.
// An error can leave recoverable data in the reserved sibling.
func ClonePreparedData(ctx context.Context, plan DataPreparation) (DataIdentity, error) {
	if err := ctx.Err(); err != nil {
		return DataIdentity{}, err
	}
	parent, name, trialName, err := openDataPreparation(plan)
	if err != nil {
		return DataIdentity{}, err
	}
	defer parent.Close()
	old, oldStat, err := openDataDir(parent, name)
	if err != nil {
		return DataIdentity{}, err
	}
	defer old.Close()
	oldToken, err := readDataToken(old)
	if err != nil || !sameDataID(FileID{Ino: oldStat.Ino, Token: oldToken}, plan.Original) {
		return DataIdentity{}, errors.Join(errors.New("prepared original data slot no longer matches"), err)
	}
	if err := unix.Mkdirat(int(parent.Fd()), trialName, 0o700); err != nil {
		return DataIdentity{}, fmt.Errorf("create private data trial: %w", err)
	}
	trial, trialStat, err := openDataDir(parent, trialName)
	if err != nil {
		return DataIdentity{}, err
	}
	defer trial.Close()
	if err := writeDataToken(trial, plan.TrialToken, plan.TrialToken); err != nil {
		return DataIdentity{}, err
	}
	if err := parent.Sync(); err != nil {
		return DataIdentity{}, fmt.Errorf("persist the reserved data slot before copying: %w", err)
	}
	if err := cloneDataTree(ctx, old, trial, 0); err != nil {
		return DataIdentity{}, fmt.Errorf("prepare isolated data trial: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return DataIdentity{}, err
	}
	return DataIdentity{
		DataDir: plan.DataDir, SnapshotDir: filepath.Join(filepath.Dir(plan.DataDir), trialName),
		Original: plan.Original,
		Trial:    FileID{Dev: uint64(trialStat.Dev), Ino: trialStat.Ino, Token: plan.TrialToken},
	}, nil
}

// DiscardUnreadyData removes only the prepared sibling while the exact original
// still occupies the live name. The supervisor may run this after restarting
// the original engine, with its launch admission still held.
func DiscardUnreadyData(plan DataPreparation) error {
	parent, live, name, err := openDataPreparation(plan)
	if err != nil {
		return err
	}
	defer parent.Close()
	active, err := readDataID(parent, live)
	if err != nil || !sameDataID(active, plan.Original) {
		return errors.Join(errors.New("refusing to discard preparation without its original data in service"), err)
	}
	trial, _, err := openDataDir(parent, name)
	if errors.Is(err, os.ErrNotExist) {
		return parent.Sync()
	}
	if err != nil {
		return err
	}
	defer trial.Close()
	token, err := readDataToken(trial)
	switch {
	case err == nil && token == plan.TrialToken:
		if err := removeSealedDataContents(trial); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		// Before the seal the directory can contain only the private token's
		// write-in-progress file. Never recursively remove an unsealed tree.
		if err := removeUnsealedDataToken(trial, plan.TrialToken); err != nil {
			return err
		}
	default:
		return errors.Join(errors.New("refusing to discard a foreign prepared data slot"), err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove reserved data slot: %w", err)
	}
	return parent.Sync()
}

func openDataPreparation(plan DataPreparation) (*os.File, string, string, error) {
	if plan.Original.Ino == 0 || !validDataToken(plan.Original.Token) ||
		!validDataToken(plan.TrialToken) || plan.Original.Token == plan.TrialToken {
		return nil, "", "", errors.New("invalid original or trial token in data preparation")
	}
	trial := dataTrialPrefix + plan.TrialToken
	if filepath.Base(plan.DataDir) == trial {
		return nil, "", "", errors.New("prepared trial name overlaps the live data path")
	}
	parent, live, err := openDataParent(plan.DataDir)
	return parent, live, trial, err
}

func removeSealedDataContents(dir *os.File) error {
	if err := verifyDiscardTree(dir, 0); err != nil {
		return err
	}
	entries, err := os.ReadDir(procFile(dir))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != dataTokenName {
			if err := os.RemoveAll(filepath.Join(procFile(dir), entry.Name())); err != nil {
				return err
			}
		}
	}
	// Persist child removal while the seal is still recoverable.
	if err := dir.Sync(); err != nil {
		return err
	}
	return unix.Unlinkat(int(dir.Fd()), dataTokenName, 0)
}

func removeUnsealedDataToken(dir *os.File, token string) error {
	tmp := dataTokenName + "-" + token
	file, err := openDataEntry(dir, tmp, unix.O_PATH, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := dataStat(file)
	if err != nil || st.Mode != unix.S_IFREG|0o600 || st.Nlink != 1 ||
		st.Uid != uint32(os.Geteuid()) || st.Size > dataTokenLength {
		return errors.Join(errors.New("refusing to remove an unknown temporary data token"), err)
	}
	return unix.Unlinkat(int(dir.Fd()), tmp, 0)
}

// PrepareData is the immediate prepare operation used for local testing.
// Supervised updates persist the plan before calling ClonePreparedData.
func PrepareData(ctx context.Context, dataDir string) (DataIdentity, error) {
	if err := ctx.Err(); err != nil {
		return DataIdentity{}, err
	}
	plan, err := PlanData(dataDir)
	if err != nil {
		return DataIdentity{}, err
	}
	id, err := ClonePreparedData(ctx, plan)
	if err != nil {
		return DataIdentity{}, errors.Join(err, DiscardUnreadyData(plan))
	}
	return id, nil
}

// ActivateData exposes the trial at the logical data path with a single
// filesystem exchange. Callers durably journal both identities beforehand.
func ActivateData(id DataIdentity) error { return orientData(id, id.Trial, id.Original, false) }

// RestoreData returns the exact previous data tree to the logical path. It
// may be called again after a crash or an uncertain fsync result.
func RestoreData(id DataIdentity) error { return orientData(id, id.Original, id.Trial, true) }

func orientData(id DataIdentity, wantLive, wantOther FileID, allowDiscarded bool) error {
	parent, live, other, err := openDataSlots(id)
	if err != nil {
		return err
	}
	defer parent.Close()
	liveID, err := readDataID(parent, live)
	if err != nil {
		return err
	}
	otherID, err := readDataID(parent, other)
	if allowDiscarded && sameDataID(liveID, wantLive) && errors.Is(err, os.ErrNotExist) {
		return parent.Sync()
	}
	if err != nil {
		return err
	}
	switch {
	case sameDataID(liveID, wantLive) && sameDataID(otherID, wantOther):
		// The prior process may have exchanged successfully and died before
		// reporting fsync. Sync again even when no exchange is necessary.
	case sameDataID(liveID, wantOther) && sameDataID(otherID, wantLive):
		if err := unix.Renameat2(int(parent.Fd()), live, int(parent.Fd()), other, unix.RENAME_EXCHANGE); err != nil {
			return fmt.Errorf("atomically exchange host data: %w", err)
		}
	default:
		return errors.New("host data slots do not match their durable identities")
	}
	return parent.Sync()
}

// DiscardData removes a failed trial after RestoreData has put the original
// back. It never deletes the original slot, including after promotion.
func DiscardData(id DataIdentity) error {
	parent, live, other, err := openDataSlots(id)
	if err != nil {
		return err
	}
	defer parent.Close()
	liveID, err := readDataID(parent, live)
	if err != nil || !sameDataID(liveID, id.Original) {
		return errors.Join(errors.New("refusing to discard data without the original in service"), err)
	}
	return discardDataSlot(parent, other, id.Trial)
}

// DiscardRetiredData removes an exact older backup after a later promotion.
// The caller keeps the more recent committed pair as its recovery record.
func DiscardRetiredData(id DataIdentity) error {
	parent, live, other, err := openDataSlots(id)
	if err != nil {
		return err
	}
	defer parent.Close()
	liveID, err := readDataID(parent, live)
	if err != nil || sameDataID(liveID, id.Original) {
		return errors.Join(errors.New("refusing to retire the active previous data"), err)
	}
	return discardDataSlot(parent, other, id.Original)
}

func discardDataSlot(parent *os.File, name string, expected FileID) error {
	tombstone := dataDiscardPrefix + expected.Token
	observed, err := readDataID(parent, name)
	switch {
	case err == nil && sameDataID(observed, expected):
		if err := validateDiscardSlot(parent, name); err != nil {
			return err
		}
		if err := unix.Renameat2(int(parent.Fd()), name, int(parent.Fd()), tombstone, unix.RENAME_NOREPLACE); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		// An interrupted cleanup may already have detached this private
		// slot. The logical path and journal can recover independently.
	default:
		return errors.Join(errors.New("refusing to discard an unrecognized data slot"), err)
	}
	if err := parent.Sync(); err != nil {
		return err
	}
	observed, err = readDataID(parent, tombstone)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !sameDataID(observed, expected) {
		return errors.Join(errors.New("refusing to remove an unrecognized detached data slot"), err)
	}
	if err := validateDiscardSlot(parent, tombstone); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(procFile(parent), tombstone)); err != nil {
		return err
	}
	return parent.Sync()
}

func validateDiscardSlot(parent *os.File, name string) error {
	trial, _, err := openDataDir(parent, name)
	if err != nil {
		return err
	}
	return errors.Join(verifyDiscardTree(trial, 0), trial.Close())
}

func openDataSlots(id DataIdentity) (*os.File, string, string, error) {
	other := filepath.Base(id.SnapshotDir)
	if id.DataDir == id.SnapshotDir || filepath.Dir(id.SnapshotDir) != filepath.Dir(id.DataDir) ||
		filepath.Clean(id.SnapshotDir) != id.SnapshotDir || id.Original.Ino == 0 || id.Trial.Ino == 0 ||
		!validDataToken(id.Original.Token) || !validDataToken(id.Trial.Token) || id.Original.Token == id.Trial.Token ||
		other != dataTrialPrefix+id.Trial.Token {
		return nil, "", "", errors.New("invalid durable host data pair")
	}
	parent, live, err := openDataParent(id.DataDir)
	return parent, live, other, err
}

func openDataParent(dataDir string) (*os.File, string, error) {
	if !filepath.IsAbs(dataDir) || dataDir != filepath.Clean(dataDir) || dataDir == "/" {
		return nil, "", errors.New("a canonical absolute host data path is required")
	}
	dir := filepath.Dir(dataDir)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, "", errors.Join(errors.New("host data parent must resolve without symlinks"), err)
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", err
	}
	parent := os.NewFile(uintptr(fd), dir)
	stat, err := dataStat(parent)
	if err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o022 != 0 {
		parent.Close()
		return nil, "", errors.Join(errors.New("host data parent must be controlled by the supervisor user"), err)
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil {
		parent.Close()
		return nil, "", err
	}
	if fs.Type != unix.EXT4_SUPER_MAGIC && fs.Type != unix.BTRFS_SUPER_MAGIC {
		parent.Close()
		return nil, "", errors.New("host data trial requires a local ext4 or Btrfs filesystem")
	}
	return parent, filepath.Base(dataDir), nil
}

func openDataDir(parent *os.File, name string) (*os.File, unix.Stat_t, error) {
	f, err := openDataEntry(parent, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, unix.Stat_t{}, err
	}
	stat, err := dataStat(f)
	if err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o022 != 0 {
		f.Close()
		return nil, unix.Stat_t{}, errors.Join(errors.New("the host data slot is not privately owned"), err)
	}
	return f, stat, nil
}

func openDataEntry(dir *os.File, name string, flags int, mode uint32) (*os.File, error) {
	fd, err := unix.Openat2(int(dir.Fd()), name, &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return nil, &os.PathError{Op: "open private data path", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func dataStat(file *os.File) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstat(int(file.Fd()), &st)
	return st, err
}

func validDataToken(token string) bool {
	if len(token) != dataTokenLength || token != strings.ToLower(token) {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func readDataToken(dir *os.File) (string, error) {
	f, err := openDataEntry(dir, dataTokenName, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOATIME, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	stat, err := dataStat(f)
	if err != nil || stat.Mode != unix.S_IFREG|0o600 || stat.Nlink != 1 ||
		stat.Uid != uint32(os.Geteuid()) || stat.Size != dataTokenLength {
		return "", errors.Join(errors.New("invalid private data slot token file"), err)
	}
	var raw [dataTokenLength]byte
	if _, err := io.ReadFull(f, raw[:]); err != nil {
		return "", err
	}
	token := string(raw[:])
	if !validDataToken(token) {
		return "", errors.New("invalid private data slot token")
	}
	return token, nil
}

func writeDataToken(dir *os.File, token, temporarySuffix string) error {
	tmp := dataTokenName + "-" + temporarySuffix
	f, err := openDataEntry(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(dir.Fd()), tmp, 0)
	_, err = f.WriteString(token)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err := unix.Renameat2(int(dir.Fd()), tmp, int(dir.Fd()), dataTokenName, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return dir.Sync()
}

func readDataID(parent *os.File, name string) (FileID, error) {
	dir, st, err := openDataDir(parent, name)
	if err != nil {
		return FileID{}, err
	}
	defer dir.Close()
	token, err := readDataToken(dir)
	if err != nil {
		// An existing directory without its token is corrupt, not absent.
		return FileID{}, fmt.Errorf("could not identify existing data slot %q: %v", name, err)
	}
	return FileID{Dev: uint64(st.Dev), Ino: st.Ino, Token: token}, nil
}

func sameDataID(actual, recorded FileID) bool {
	return actual.Ino == recorded.Ino && actual.Token == recorded.Token
}

func procFile(file *os.File) string {
	return "/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10)
}

func cloneDataTree(ctx context.Context, src, dst *os.File, depth int) error {
	if depth >= maxDataTreeDepth {
		return errors.New("host data tree is too deep for a safe clone")
	}
	before, err := dataStat(src)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := src.ReadDir(128)
		for _, entry := range entries {
			if depth == 0 && entry.Name() == dataTokenName {
				continue
			}
			if err := cloneDataEntry(ctx, src, dst, entry.Name(), depth+1); err != nil {
				return fmt.Errorf("clone %q: %w", entry.Name(), err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return finishDataCopy(src, dst, before)
}

func cloneDataEntry(ctx context.Context, parent, out *os.File, name string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pin, err := openDataEntry(parent, name, unix.O_PATH, 0)
	if err != nil {
		return err
	}
	defer pin.Close()
	stat, err := dataStat(pin)
	if err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return errors.Join(errors.New("unsafe ownership or permissions in the host data tree"), err)
	}
	isDir := stat.Mode&unix.S_IFMT == unix.S_IFDIR
	if isDir && depth >= maxDataTreeDepth {
		return errors.New("host data tree is too deep for a safe clone")
	}
	if !isDir && (stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1) {
		return errors.New("host data must use ordinary files and directories without links")
	}
	// Opening the pinned inode by descriptor avoids a path replacement
	// between the type check and opening it for data or directory reads.
	source, err := os.Open(procFile(pin))
	if err != nil {
		return err
	}
	defer source.Close()
	var target *os.File
	if isDir {
		if err := unix.Mkdirat(int(out.Fd()), name, 0o700); err != nil {
			return err
		}
		target, err = openDataEntry(out, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	} else {
		target, err = openDataEntry(out, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	}
	if err != nil {
		return err
	}
	defer target.Close()
	if isDir {
		return cloneDataTree(ctx, source, target, depth)
	}
	err = unix.IoctlFileClone(int(target.Fd()), int(source.Fd()))
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EXDEV) {
		var copied int64
		copied, err = io.Copy(target, dataCopyReader{ctx: ctx, Reader: source})
		if err == nil && copied != stat.Size {
			return errors.New("the host data copy does not match the source size")
		}
	}
	if err != nil {
		return err
	}
	staged, err := dataStat(target)
	if err != nil || staged.Size != stat.Size {
		return errors.Join(errors.New("the staged file does not match the source size"), err)
	}
	return finishDataCopy(source, target, stat)
}

type dataCopyReader struct {
	ctx context.Context
	io.Reader
}

func (r dataCopyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func finishDataCopy(src, dst *os.File, before unix.Stat_t) error {
	after, err := dataStat(src)
	if err != nil {
		return err
	}
	if after.Mtim != before.Mtim || after.Ctim != before.Ctim || after.Size != before.Size {
		return errors.New("the previous engine's data changed while cloning")
	}
	if err := unix.Fchown(int(dst.Fd()), int(before.Uid), int(before.Gid)); err != nil {
		return err
	}
	if err := dst.Chmod(os.FileMode(before.Mode & 0o777)); err != nil {
		return err
	}
	if err := cloneDataAttributes(int(src.Fd()), int(dst.Fd())); err != nil {
		return err
	}
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, procFile(dst), []unix.Timespec{before.Atim, before.Mtim}, 0); err != nil {
		return err
	}
	return errors.Join(src.Sync(), dst.Sync())
}

func cloneDataAttributes(src, dst int) error {
	source, err := listDataAttributes(src)
	if err != nil {
		return err
	}
	target, err := listDataAttributes(dst)
	if err != nil {
		return err
	}
	for key := range target {
		if _, keep := source[key]; !keep {
			if err := unix.Fremovexattr(dst, key); err != nil {
				return err
			}
		}
	}
	for key, value := range source {
		if targetValue, exists := target[key]; exists && bytes.Equal(targetValue, value) {
			continue
		}
		if err := unix.Fsetxattr(dst, key, value, 0); err != nil {
			return err
		}
	}
	return nil
}

func listDataAttributes(fd int) (map[string][]byte, error) {
	length, err := unix.Flistxattr(fd, nil)
	if err != nil || length == 0 {
		return nil, err
	}
	raw := make([]byte, length)
	length, err = unix.Flistxattr(fd, raw)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]byte)
	for _, key := range strings.Split(strings.TrimSuffix(string(raw[:length]), "\x00"), "\x00") {
		size, err := unix.Fgetxattr(fd, key, nil)
		if err != nil {
			return nil, err
		}
		value := make([]byte, size)
		got, err := unix.Fgetxattr(fd, key, value)
		if err != nil || size != got {
			return nil, errors.Join(errors.New("host data attributes changed while cloning"), err)
		}
		result[key] = value
	}
	return result, nil
}

func verifyDiscardTree(dir *os.File, depth int) error {
	if depth >= maxDataTreeDepth {
		return errors.New("refusing to remove an excessively nested host data trial")
	}
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			subdir, err := openDataEntry(dir, entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if err != nil {
				return err
			}
			err = verifyDiscardTree(subdir, depth+1)
			if err := errors.Join(err, subdir.Close()); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
