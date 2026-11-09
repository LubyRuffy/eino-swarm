package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
)

// Verification intent is a host resource class, not a guess about task text.
// Ordinary registries leave VerifyAdmission nil and ignore these fields.
func (r *Registry) admitVerification(ctx context.Context, a spawnArgs) error {
	config := r.workerConfigurationForContext(ctx)
	if config.VerifyAdmission == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	intent := a.Intent
	if intent == "" {
		intent = "work"
	}
	switch intent {
	case "work":
		return nil
	case "verify", "fix":
	default:
		return fmt.Errorf("spawn_agent: intent must be work, verify, or fix")
	}
	if a.Target == "" {
		return fmt.Errorf("spawn_agent: verify and fix require target, an existing shared record id")
	}
	callID := strings.TrimSpace(compose.GetToolCallID(ctx))
	if callID == "" {
		return fmt.Errorf("spawn_agent: verification requires a real tool call identity")
	}
	if err := config.VerifyAdmission(ctx, intent, a.Target, callID); err != nil {
		return err
	}
	return nil
}

// refuseVerification reports a correctable spawn refusal. Cancellation is a
// real error so the turn stops instead of looking like a successful dispatch.
func (r *Registry) refuseVerification(ctx context.Context, a spawnArgs) (string, error, bool) {
	err := r.admitVerification(ctx, a)
	if err == nil {
		return "", nil, false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err, true
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "spawn_agent:") {
		msg = "spawn_agent: " + msg
	}
	out, callErr := ctlRefuse(msg)
	return out, callErr, true
}
