package main

import (
	"errors"
	"os"
	"runtime"
	"syscall"

	"github.com/LubyRuffy/eino-swarm/internal/update"
	"golang.org/x/sys/unix"
)

// Only the portable Linux release builder stamps this value.
var linuxArchiveBuild string

var newLinuxUpdateFeed = func() *update.LinuxFeed {
	return &update.LinuxFeed{Current: version, Arch: runtime.GOARCH}
}

func inheritedLinuxUpdatePipe(fd int) (*os.File, error) {
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), "Linux update control")
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		_ = file.Close()
		return nil, errors.New("Linux update process requires an inherited private pipe")
	}
	return file, nil
}

func ownedLinuxFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncLinuxDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
