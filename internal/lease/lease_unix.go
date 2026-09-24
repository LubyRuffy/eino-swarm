//go:build unix

package lease

import (
	"errors"
	"os"
	"syscall"
)

func lockExclusive(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrHeld
	}
	if err != nil {
		return err
	}
	return nil
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func fileIdentity(path string) (uint64, uint64) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}

func signalStop(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

func signalKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}
