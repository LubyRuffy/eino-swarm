//go:build windows

package desktop

import "golang.org/x/sys/windows"

func presentStartupAlert(title, message string, failure bool) {
	flags := uint32(windows.MB_OK | windows.MB_SETFOREGROUND | windows.MB_TOPMOST | windows.MB_SYSTEMMODAL)
	if failure {
		flags |= windows.MB_ICONERROR
	} else {
		flags |= windows.MB_ICONWARNING
	}
	_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr(message), windows.StringToUTF16Ptr(title), flags)
}
