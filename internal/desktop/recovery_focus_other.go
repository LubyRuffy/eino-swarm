//go:build !darwin

package desktop

import "context"

func FocusRecoverySuccessor(ctx context.Context, _ int, _ string) error { return ctx.Err() }
