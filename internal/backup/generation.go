package backup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Generation reads the monotonic counter stored beside the archive contents.
// A missing file is generation 0. The file is not part of a backup, so restoring
// an older archive cannot move the counter backwards.
func Generation(dataDir string) (int64, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, generationFile))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, invalidf("data generation")
	}
	if n < 0 {
		return 0, invalidf("data generation")
	}
	return n, nil
}

// AdvanceGeneration adds one after a restore has committed. Backup does not
// call it: writing an archive does not change the data the apps are showing.
func AdvanceGeneration(dataDir string) (int64, error) {
	if err := failNext(); err != nil {
		return 0, err
	}
	n, err := Generation(dataDir)
	if err != nil {
		return 0, err
	}
	n++
	path := filepath.Join(dataDir, generationFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(n, 10)), 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return n, nil
}
