package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"
)

const (
	execPollSeparator = `[ \t]*(?:;|&&)[ \t]*`
	execPollMarker    = `echo[ \t]+[[:alnum:]_][[:alnum:]_.-]*(?:[ \t]+[[:alnum:]_][[:alnum:]_.-]*)*`
	execPollListing   = `pip3?[ \t]+list[ \t]+2>[ \t]*/dev/null(?:[ \t]*\|[ \t]*(?:head|tail)[ \t]+-[1-9][0-9]*)?`
)

// Admit only a static wait with optional marker output and one read-only
// observation. The whole command must match, including its shell separators.
var managedExecPoll = regexp.MustCompile(`^(?:` + execPollMarker + execPollSeparator + `)?(sleep[ \t]+([0-9]+(?:\.[0-9]+)?))` +
	`(?:` + execPollSeparator + execPollMarker + `)?(?:` + execPollSeparator + execPollListing + `)?$`)

func managedExecPollFollowup(command string) (string, bool) {
	parts := managedExecPoll.FindStringSubmatchIndex(command)
	if parts == nil {
		return "", false
	}
	seconds, err := strconv.ParseFloat(command[parts[4]:parts[5]], 64)
	if err != nil || seconds < execSleepBiasMin.Seconds() || seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return "", false
	}
	start, end := parts[2], parts[3]
	if start == 0 && end == len(command) {
		return "", true
	}
	// Keep the other commands and control operators verbatim. The no-op
	// supplies the successful status the sleep would have returned.
	return command[:start] + ":" + command[end:], true
}

// awaitPollCompletion observes the owner's existing completion signal. The
// engine still collects terminal receipts before sending the next model input.
func (s *ManagedExecSessions) awaitPollCompletion(ctx context.Context, callID string) (bool, error) {
	s.mu.Lock()
	_, used := s.calls[callID]
	s.mu.Unlock()
	if used {
		// The normal invocation will handle an already-recorded call ID.
		return false, nil
	}
	ready, pending, changed := s.readyJobs()
	if !pending {
		return false, nil
	}
	if len(ready) == 0 {
		select {
		case <-ctx.Done():
		case <-s.ctx.Done():
		case <-changed:
		}
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return true, s.ctx.Err()
}

func (s *ManagedExecSessions) pollResult(ctx context.Context, callID, command string, timeoutMS int64, waited time.Duration, timedOut bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return refusedExecResult(err), nil
	}
	if err := s.ctx.Err(); err != nil {
		return refusedExecResult(err), nil
	}
	if _, used := s.calls[callID]; used {
		return refusedExecResult(errors.New("this tool call already owns a command")), nil
	}
	s.calls[callID] = managedExecCall{}
	payload := map[string]any{
		"code": "background_exec_ready", "waited_for_background": true, "timeout_ms": timeoutMS,
		"background_wait_ms": waited.Milliseconds(), "full_command": command,
	}
	if timedOut {
		payload["code"] = "background_exec_wait_timeout"
		payload["failed"] = true
		payload["error"] = "The wait reached this exec call's deadline. The original background command remains owned by the turn."
	} else {
		payload["message"] = "A background command finished. Its terminal result belongs to the original tool call."
	}
	out, err := json.Marshal(payload)
	return string(out), err
}
