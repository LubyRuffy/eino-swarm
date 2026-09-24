package engine

import (
	"errors"
	"testing"
)

func TestContextOverflowRecognizesALengthRejection(t *testing.T) {
	for _, msg := range []string{
		"error, status code: 400, message: This model's maximum context length is 8192 tokens. However, you requested 9000 tokens",
		"prompt is too long: 200000 tokens > 199000 maximum",
		"context_length_exceeded",
		"input is too long",
		"too many tokens",
	} {
		if !isContextOverflow(errors.New(msg)) {
			t.Fatalf("must be a context overflow: %s", msg)
		}
	}
	for _, msg := range []string{
		"context canceled",
		"context deadline exceeded",
		"error, status code: 400, message: Unterminated string",
		"error, status code: 429, message: rate limited",
	} {
		if isContextOverflow(errors.New(msg)) {
			t.Fatalf("must not be a context overflow: %s", msg)
		}
	}
	if isContextOverflow(nil) {
		t.Fatal("nil is not an overflow")
	}
}

func TestContextLimitFromErrorPrefersTheStatedMaximum(t *testing.T) {
	err := errors.New("maximum context length is 8192 tokens. However, you requested 9000 tokens")
	if got := contextLimitFromError(err); got != 8192 {
		t.Fatalf("limit = %d, want the stated maximum, not the rejected request", got)
	}
	anth := errors.New("prompt is too long: 200000 tokens > 199000 maximum")
	if got := contextLimitFromError(anth); got != 199000 {
		t.Fatalf("anthropic-style limit = %d", got)
	}
	if contextLimitFromError(errors.New("context length exceeded")) != 0 {
		t.Fatal("a rejection without a number is not a window")
	}
	if contextLimitFromError(nil) != 0 {
		t.Fatal("nil")
	}
	if contextLimitFromError(errors.New("maximum context length is 0 tokens")) != 0 {
		t.Fatal("a zero maximum is not a window")
	}
}

func TestLearnContextCeilingLowersAndNeverRaises(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// The scripted pool reports 128000 until Settings says otherwise.
	// A stated 8192 is smaller, so it becomes this model's window.
	overflow := errors.New("maximum context length is 8192 tokens. However, you requested 9000 tokens")
	if !e.learnContextCeiling(th.ProviderID, th.Model, overflow, 9000) {
		t.Fatal("a stated ceiling under the current window must be stored")
	}
	if got := e.pool.WindowFor(th.ProviderID, th.Model); got != 8192 {
		t.Fatalf("learned window = %d, want 8192", got)
	}

	// A larger advertised maximum must not wipe the learned ceiling.
	bigger := errors.New("maximum context length is 128000 tokens")
	if e.learnContextCeiling(th.ProviderID, th.Model, bigger, 9000) {
		t.Fatal("learning must not raise a window")
	}
	if got := e.pool.WindowFor(th.ProviderID, th.Model); got != 8192 {
		t.Fatalf("window moved to %d", got)
	}

	// No stated number: the rejected prompt is an upper bound, and only if it
	// is under the window we already believe.
	if e.learnContextCeiling(th.ProviderID, th.Model, errors.New("context length exceeded"), 9000) {
		t.Fatal("9000 is above the learned 8192 window")
	}
	if !e.learnContextCeiling(th.ProviderID, th.Model, errors.New("context length exceeded"), 4000) {
		t.Fatal("a rejected prompt under the current window is a new ceiling")
	}
	if got := e.pool.WindowFor(th.ProviderID, th.Model); got != 4000 {
		t.Fatalf("prompt-sized ceiling = %d, want 4000", got)
	}
}

func TestLearnContextCeilingStoresANamedModel(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Models.Providers[0].Model = "named-model"
	overflow := errors.New("maximum context length is 16384 tokens")
	if !e.learnContextCeiling("default", "named-model", overflow, 20000) {
		t.Fatal("a named model under the mock window must be stored on its own row")
	}
	if got := e.Config().Models.Providers[0].ModelContext["named-model"]; got != 16384 {
		t.Fatalf("model_context = %d", got)
	}
	if e.Config().Models.Providers[0].ContextWindow != 0 {
		t.Fatal("a named ceiling must not overwrite the provider fallback")
	}
	if e.learnContextCeiling("default", "named-model", errors.New("maximum context length is 32000 tokens"), 0) {
		t.Fatal("a second, larger maximum must not raise the named row")
	}
}

func TestLearnContextCeilingIgnoresAnEmptyRejection(t *testing.T) {
	e := newTestEngine(t)
	if e.learnContextCeiling("default", "", errors.New("context length exceeded"), 0) {
		t.Fatal("no number and no prompt size is not a ceiling")
	}
	if e.lowerModelWindow("missing", "", 100) {
		t.Fatal("an unknown provider must not grow a window")
	}
	if e.lowerModelWindow("default", "", 0) {
		t.Fatal("a zero ceiling is not a window")
	}
	var bare *Engine
	if bare.lowerModelWindow("default", "", 100) {
		t.Fatal("a missing engine must not store a window")
	}
}

func TestCompactThresholdFollowsTheLiveWindow(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	// Mock window is 128000. 80% is 102400, still above the output reserve.
	if got := e.compactThreshold(th.ID); got != 102_400 {
		t.Fatalf("mock window trigger = %d, want 102400", got)
	}
	e.Config().Models.Providers[0].ContextWindow = 0
	if !e.learnContextCeiling(th.ProviderID, th.Model, errors.New("maximum context length is 32000 tokens"), 40000) {
		t.Fatal("learn")
	}
	// 80% of 32k is 25600; reserve leaves 32000-8192=23808.
	if got := e.compactThreshold(th.ID); got != 32_000-8_192 {
		t.Fatalf("after learn trigger = %d, want %d", got, 32_000-8_192)
	}
}
