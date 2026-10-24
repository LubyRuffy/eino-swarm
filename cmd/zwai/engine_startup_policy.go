package main

import (
	"context"
	"errors"
	"fmt"
)

// The signal owner survives the bounded handoff. Once that handoff has stopped
// the old engine, readiness expiry may report the replacement but cannot kill it.
type engineStartupPolicyKey struct{}

type engineStartupPolicy struct {
	owner       context.Context
	forwardOnly bool
}

type engineStartupPendingError struct {
	PID     int
	LogPath string
	Cause   error
}

func (e *engineStartupPendingError) Error() string {
	return fmt.Sprintf("engine %d is still initializing; launcher wait ended: %v; log: %s", e.PID, e.Cause, e.LogPath)
}

func (e *engineStartupPendingError) Unwrap() error { return e.Cause }

// A caller may use cancellation causes. Keep its category and its actual cause
// so an explicit interruption cannot be reported as a readiness timeout.
func engineContextError(ctx context.Context) error {
	err := ctx.Err()
	if cause := context.Cause(ctx); cause != err {
		return errors.Join(err, cause)
	}
	return err
}
