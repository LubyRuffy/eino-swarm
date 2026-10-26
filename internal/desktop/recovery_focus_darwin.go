//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Cocoa -lproc
#include "recovery_focus_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var recoveryFocusOnMain = application.InvokeAsync
var recoveryFocusNative = activateRecoverySuccessor

func captureRecoverySuccessor(pid int, executable string) (recoverySuccessorBirth, error) {
	path := C.CString(executable)
	defer C.free(unsafe.Pointer(path))
	var birth C.zwaiRecoveryBirth
	if C.zwaiRecoveryCapture(C.int(pid), path, &birth) == 0 {
		return recoverySuccessorBirth{}, ErrRecoveryFocusIdentity
	}
	return recoverySuccessorBirth{uint64(birth.seconds), uint64(birth.microseconds)}, nil
}

func activateRecoverySuccessor(pid int, executable string, birth recoverySuccessorBirth) int {
	path := C.CString(executable)
	defer C.free(unsafe.Pointer(path))
	return int(C.zwaiRecoveryFocusSuccessor(C.int(pid), path, C.zwaiRecoveryBirth{seconds: C.uint64_t(birth.seconds), microseconds: C.uint64_t(birth.microseconds)}))
}

func requestRecoverySuccessorActivation(ctx context.Context, pid int, executable string, birth recoverySuccessorBirth) (int, error) {
	result := make(chan int, 1)
	recoveryFocusOnMain(func() {
		if ctx.Err() == nil {
			result <- recoveryFocusNative(pid, executable, birth)
		}
	})
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case status := <-result:
		return status, ctx.Err()
	}
}
