//go:build darwin

package desktop

import (
	"testing"
	"unsafe"
)

func TestPositionTrafficLightsNilWindow(t *testing.T) {
	if got := positionTrafficLights(nil); got != 0 {
		t.Fatalf("nil window = %v, want 0", got)
	}
}

func TestPositionTrafficLightsHopsToTheMainThread(t *testing.T) {
	// A worker-goroutine setFrame is an NSInternalInconsistencyException and
	// a SIGABRT, not a log line.
	var hopped bool
	prev := runOnMain
	t.Cleanup(func() { runOnMain = prev })
	runOnMain = func(fn func() float64) float64 {
		hopped = true
		return 0
	}
	var dummy int
	_ = positionTrafficLights(unsafe.Pointer(&dummy))
	if !hopped {
		t.Fatal("a live window pointer reached AppKit without hopping to the main thread")
	}
}
