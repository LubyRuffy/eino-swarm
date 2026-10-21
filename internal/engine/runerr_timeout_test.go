package engine

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestPublicFirstByteTimeoutNamesTheConfiguredBudget(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] %w", provider.FirstByteTimeoutError{Wait: 2 * time.Minute, Err: errors.New("http2: timeout awaiting response headers")})
	if got := publicTurnError(err); got != "the model sent no first byte within 2m0s" {
		t.Fatalf("configured budget lost: %q", got)
	}
	if isRetryableModelError(err) {
		t.Fatal("the transport already reconnects once; a configured wait must not add model retry loops")
	}
}
