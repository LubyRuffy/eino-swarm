package provider

import "context"

type isolatedRecoveryKey struct{}

// WithIsolatedRecovery leaves transport recovery to a caller that already owns
// durable retries for a read-only candidate. Its requests neither wait for nor
// update shared service health, and do not receive a hidden HTTP reconnect.
func WithIsolatedRecovery(ctx context.Context) context.Context {
	return context.WithValue(ctx, isolatedRecoveryKey{}, true)
}
