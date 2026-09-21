//go:build !windows

package wakeup

import "errors"

func startWindows() (func(), error) {
	return nil, errors.New("wakeup: windows inhibit is not available")
}
