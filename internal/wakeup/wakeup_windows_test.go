//go:build windows

package wakeup

import (
	"errors"
	"testing"
)

func TestWindowsHoldAndRelease(t *testing.T) {
	orig := setExecState
	t.Cleanup(func() { setExecState = orig })
	var flags []uint32
	setExecState = func(f uint32) error {
		flags = append(flags, f)
		return nil
	}
	stop, err := startWindows()
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if len(flags) != 2 {
		t.Fatalf("flags %v", flags)
	}
	if flags[0] != esContinuous|esSystemRequired {
		t.Fatalf("hold %x", flags[0])
	}
	if flags[1] != esContinuous {
		t.Fatalf("release %x", flags[1])
	}
}

func TestWindowsHoldFailure(t *testing.T) {
	orig := setExecState
	t.Cleanup(func() { setExecState = orig })
	setExecState = func(uint32) error { return errors.New("denied") }
	if _, err := startWindows(); err == nil {
		t.Fatal("expected error")
	}
}
