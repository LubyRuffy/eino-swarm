package provider

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestLocalNetworkHintNamesTheMacPermission(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EHOSTUNREACH}
	got := localNetworkHint(err)
	if runtime.GOOS == "darwin" {
		if !strings.Contains(got, "Local Network") {
			t.Fatalf("a no-route dial on macOS must name Local Network permission, got %q", got)
		}
	} else if got != "" {
		t.Fatalf("non-darwin must not blame Local Network: %q", got)
	}
	if isNoRoute(nil) {
		t.Fatal("nil is not a route error")
	}
	if localNetworkHint(errors.New("connection refused")) != "" {
		t.Fatal("a refused connection is not a privacy block")
	}
	wrapped := fmt.Errorf("dial tcp: %w", syscall.EHOSTUNREACH)
	if runtime.GOOS == "darwin" && !strings.Contains(localNetworkHint(wrapped), "Local Network") {
		t.Fatal("a wrapped EHOSTUNREACH must still name the permission")
	}
}
