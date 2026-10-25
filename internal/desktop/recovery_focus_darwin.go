//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Cocoa
#include "recovery_focus_darwin.h"
*/
import "C"

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var recoveryFocusOnMain = application.InvokeAsync
var recoveryFocusNative = focusRecoveryWindow

func focusRecoveryWindow(window application.Window) {
	C.zwaiRecoveryFocus(window.NativeWindow())
}

func requestRecoverySuccessorFocus(ctx context.Context, window application.Window, parentDone <-chan struct{}) {
	recoveryFocusOnMain(func() {
		if window == nil || ctx.Err() != nil {
			return
		}
		select {
		case <-parentDone:
			return
		default:
		}
		recoveryFocusNative(window)
	})
}
