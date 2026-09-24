//go:build windows

package lease

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
	procOpenProcess  = kernel32.NewProc("OpenProcess")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
)

const (
	lockfileExclusive      = 0x00000002
	lockfileFailNow        = 0x00000001
	processQueryLimitedInf = 0x1000
)

func lockExclusive(f *os.File) error {
	// One byte at the start of the file. The overlapped struct has to stay
	// alive for the call; a stack value is enough because we do not wait.
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusive|lockfileFailNow),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r == 0 {
		if err == syscall.Errno(33) { // ERROR_LOCK_VIOLATION
			return ErrHeld
		}
		return err
	}
	return nil
}

func unlock(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}

func fileIdentity(path string) (uint64, uint64) { return 0, 0 }

func signalStop(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInf), 0, uintptr(pid))
	if h == 0 {
		return false
	}
	_, _, _ = procCloseHandle.Call(h)
	return true
}
