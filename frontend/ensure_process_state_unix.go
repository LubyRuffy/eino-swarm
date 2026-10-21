//go:build unix

package frontend

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func buildGroupHasLiveProcess(proc string, pgid int) (bool, error) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil || !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(proc, entry.Name(), "stat"))
		if errors.Is(err, os.ErrNotExist) {
			continue // The process exited after the directory listing.
		}
		if err != nil {
			return false, err
		}
		active, err := buildProcessActive(data, pgid)
		if err != nil || active {
			return active, err
		}
	}
	return false, nil
}

func buildProcessActive(stat []byte, pgid int) (bool, error) {
	// comm is parenthesized and can itself contain spaces and ')'. Every
	// field after its final ')' is whitespace-separated process metadata.
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return false, fmt.Errorf("frontend build process stat: %w", os.ErrInvalid)
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 3 || len(fields[0]) != 1 {
		return false, fmt.Errorf("frontend build process stat fields: %w", os.ErrInvalid)
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return false, fmt.Errorf("frontend build process group: %w", err)
	}
	state := fields[0]
	return group == pgid && state != "Z" && state != "X" && state != "x", nil
}
