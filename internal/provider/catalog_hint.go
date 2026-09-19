package provider

import (
	"errors"
	"net"
	"runtime"
	"syscall"
)

// localNetworkHint is the extra sentence macOS users need when a LAN
// endpoint is reachable from Terminal but not from the desktop window.
// Sequoia+ treats a Wails GUI as its own app; without Local Network
// permission the kernel returns EHOSTUNREACH ("no route to host").
func localNetworkHint(err error) string {
	if runtime.GOOS != "darwin" || !isNoRoute(err) {
		return ""
	}
	return "; macOS blocked local-network access for this app — allow zwai under System Settings → Privacy & Security → Local Network"
}

func isNoRoute(err error) bool {
	if err == nil {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && errors.Is(op.Err, syscall.EHOSTUNREACH) {
		return true
	}
	return errors.Is(err, syscall.EHOSTUNREACH)
}
