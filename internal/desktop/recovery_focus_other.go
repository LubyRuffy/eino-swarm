//go:build !darwin

package desktop

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func requestRecoverySuccessorFocus(context.Context, application.Window, <-chan struct{}, func(string)) {
}
