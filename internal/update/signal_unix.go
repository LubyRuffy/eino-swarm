//go:build !windows

package update

import (
	"os"
	"syscall"
)

func signalPID(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
}
