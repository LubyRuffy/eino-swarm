//go:build unix && !linux

package frontend

import (
	"errors"
	"syscall"
)

func buildGroupActive(pgid int) (bool, error) {
	err := signalBuildGroup(-pgid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	// Darwin can report EPERM while a killed orphan is being reaped.
	// It still identifies an existing owned group, so keep waiting.
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return err == nil, err
}
