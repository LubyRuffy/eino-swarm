//go:build windows

package wakeup

import "syscall"

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setExecState = windowsSetExecState

func startWindows() (func(), error) {
	if err := setExecState(esContinuous | esSystemRequired); err != nil {
		return nil, err
	}
	return func() {
		_ = setExecState(esContinuous)
	}, nil
}

func windowsSetExecState(flags uint32) error {
	r, _, err := procSetThreadExecutionState.Call(uintptr(flags))
	if r == 0 {
		return err
	}
	return nil
}

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)
