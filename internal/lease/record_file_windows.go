//go:build windows

package lease

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// recordPath preserves long drive and UNC paths for the native file APIs.
func recordPath(path string) (*uint16, error) {
	full, err := windows.FullPath(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(full, `\\?\`) && !strings.HasPrefix(full, `\\.\`) {
		if strings.HasPrefix(full, `\\`) {
			full = `\\?\UNC\` + full[2:]
		} else {
			full = `\\?\` + full
		}
	}
	return windows.UTF16PtrFromString(full)
}

func openRecordFile(path string) (*os.File, error) {
	name, err := recordPath(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func replaceRecordFile(source, target string) error {
	src, err := recordPath(source)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
	}
	dst, err := recordPath(target)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
	}
	h, err := windows.CreateFile(src, windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
	}
	defer windows.CloseHandle(h)
	// MoveFileEx (os.Rename) refuses an open destination even when its reader
	// shares deletion. POSIX replacement keeps that reader on the old complete
	// file and atomically directs new opens to the new one. Never delete first.
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	name := windows.UTF16PtrToString(dst)
	units, _ := windows.UTF16FromString(name) // recordPath already validated UTF-16.
	offset := unsafe.Offsetof(renameInfo{}.Name)
	buf := make([]byte, int(offset)+2*len(units))
	info := (*renameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.Length = uint32(2 * (len(units) - 1))
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset])), len(units)), units)
	if err := windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buf[0], uint32(len(buf))); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
	}
	return nil
}
