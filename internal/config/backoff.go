package config

import "time"

// Model backoff bounds recovery traffic to an unhealthy chat service. These
// are recovery policy, not additional request attempts or provider timeouts.
const (
	DefaultModelBackoffInitial = 5 * time.Second
	DefaultModelBackoffMax     = 30 * time.Second
	// Confirmed "no healthy provider" refusals stop one logical model call.
	// Ordinary 408/429/5xx retries keep their own budget.
	DefaultRouteRecoveryAttempts = 3
	// Read-only answer checks yield after either failure streak or the total
	// candidate budget. Unpublished candidates remain private and recoverable.
	DefaultScheduleAnswerFailures = 3
	DefaultScheduleAnswerInvalids = 3
	DefaultScheduleAnswerAttempts = 6
)
