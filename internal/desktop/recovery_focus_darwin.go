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

func requestRecoverySuccessorFocus(ctx context.Context, window application.Window, parentDone <-chan struct{}, report func(string)) {
	report("queued")
	recoveryFocusOnMain(func() {
		report("main")
		if window == nil {
			report("window_missing")
			return
		}
		if ctx.Err() != nil {
			report("context_cancelled")
			return
		}
		select {
		case <-parentDone:
			report("parent_closed")
			return
		default:
		}
		recoveryFocusNative(window)
		report("native_returned")
	})
}
