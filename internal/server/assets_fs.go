package server

import (
	"io"
	"io/fs"
	"path"
	"time"
)

// snapFS is a read-only file tree copied at server start. A later Vite
// rebuild of frontend/dist must not be able to delete hashed files out
// from under an already-open window.
type snapFS map[string][]byte

func (s snapFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	name = path.Clean(name)
	if data, ok := s[name]; ok {
		return &snapFile{name: name, data: data}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type snapFile struct {
	name   string
	data   []byte
	offset int64
}

func (f *snapFile) Stat() (fs.FileInfo, error) {
	return snapInfo{name: path.Base(f.name), size: int64(len(f.data))}, nil
}

func (f *snapFile) Read(p []byte) (int, error) {
	if f.offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *snapFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.offset + offset
	case io.SeekEnd:
		abs = int64(len(f.data)) + offset
	default:
		return 0, fs.ErrInvalid
	}
	if abs < 0 {
		return 0, fs.ErrInvalid
	}
	f.offset = abs
	return abs, nil
}

func (f *snapFile) Close() error { return nil }

type snapInfo struct {
	name string
	size int64
}

func (i snapInfo) Name() string       { return i.name }
func (i snapInfo) Size() int64        { return i.size }
func (i snapInfo) Mode() fs.FileMode  { return 0o444 }
func (i snapInfo) ModTime() time.Time { return time.Time{} }
func (i snapInfo) IsDir() bool        { return false }
func (i snapInfo) Sys() any           { return nil }
