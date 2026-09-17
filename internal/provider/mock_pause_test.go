package provider

import (
	"testing"
	"time"
)

func TestMockWorkerPauseReadsMilliseconds(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "15")
	if got := mockWorkerPause(); got != 15*time.Millisecond {
		t.Fatalf("got %s, want 15ms", got)
	}
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "")
	if got := mockWorkerPause(); got != 0 {
		t.Fatalf("blank env must not pause unit tests: %s", got)
	}
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "nope")
	if got := mockWorkerPause(); got != 0 {
		t.Fatalf("garbage must not pause: %s", got)
	}
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "-3")
	if got := mockWorkerPause(); got != 0 {
		t.Fatalf("negative must not pause: %s", got)
	}
}
