//go:build linux

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
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false, err
	}
	// Container PID 1 may retain orphan zombies. They cannot write, but
	// signal 0 still finds their group; waiting for ESRCH would never finish.
	return buildGroupHasLiveProcess("/proc", pgid)
}
