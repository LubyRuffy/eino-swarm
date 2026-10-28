package timing_test

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/timing"
)

func TestNilClockRetainsRealTimeAndTimerDelivery(t *testing.T) {
	clock := timing.OrReal(nil)
	before := time.Now()
	if got := clock.Now(); got.Before(before) || got.After(time.Now()) {
		t.Fatalf("default clock left real time: %s", got)
	}
	timer := clock.NewTimer(0)
	defer timer.Stop()
	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("default timer did not deliver")
	}
}

func TestClockOverrideAndRealTimerStopArePreserved(t *testing.T) {
	clock := timing.RealClock{}
	if timing.OrReal(clock) != clock {
		t.Fatal("an explicit instance clock was replaced")
	}
	timer := clock.NewTimer(time.Hour)
	if !timer.Stop() || timer.Stop() {
		t.Fatal("real timer lost its normal stop result")
	}
	select {
	case <-timer.C():
		t.Fatal("stopped timer delivered")
	default:
	}
}
