package config

import "time"

// Model backoff bounds recovery traffic to an unhealthy chat service. These
// are recovery policy, not additional request attempts or provider timeouts.
const (
	DefaultModelBackoffInitial = 5 * time.Second
	DefaultModelBackoffMax     = 30 * time.Second
	// A read-only answer check must yield execution after repeated request
	// failures. Its unpublished candidate remains private and recoverable.
	DefaultScheduleAnswerFailures = 3
)
