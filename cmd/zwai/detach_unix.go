//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the engine in its own session so the shell that spawned it can
// exit without delivering SIGHUP. That is the whole point: closing the window
// must not kill the process later shells are attached to.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
