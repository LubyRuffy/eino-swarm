package tools

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrRepeatedManagedExecFailure ends a turn after the model has already
// received an actionable refusal for the same failed command.
var ErrRepeatedManagedExecFailure = errors.New("repeated managed exec failure")

const managedExecRetryCue = "Command did not run. The last two executions failed identically. Change the command or correct its inputs before retrying."

type managedExecIdentity struct {
	command, dir, shell string
	env, stdin          [sha256.Size]byte
	pipefail            bool
}

func (c *managedExecCommand) identity(stdin string) (managedExecIdentity, error) {
	// Cmd.Environ silently drops invalid entries. Preserve the launch
	// refusal rather than starting a command with a modified environment.
	for _, value := range c.cmd.Env {
		if strings.IndexByte(value, 0) >= 0 {
			return managedExecIdentity{}, errors.New("exec: environment variable contains NUL")
		}
	}
	// Freeze the same effective environment the child will receive. Hash
	// env and stdin so the retry record doesn't retain their raw contents.
	c.cmd.Env = c.cmd.Environ()
	env := slices.Clone(c.cmd.Env)
	slices.Sort(env)
	hash := sha256.New()
	for _, value := range env {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	var identity managedExecIdentity
	copy(identity.env[:], hash.Sum(nil))
	identity.command, identity.dir, identity.shell = c.command, c.cmd.Dir, c.cmd.Path
	identity.stdin = sha256.Sum256([]byte(stdin))
	identity.pipefail = c.pipefail
	return identity, nil
}

type managedExecFailure struct {
	identity       managedExecIdentity
	exitCode       int
	stdout, stderr string
}

type managedExecRetryGuard struct {
	seq       uint64
	failure   managedExecFailure
	result    string
	refused   bool
	refusedAt uint64
}

func (g *managedExecRetryGuard) record(job *managedExecSession) {
	last := *g
	*g = managedExecRetryGuard{seq: job.seq}
	failure, ok := job.completedFailure()
	if !ok {
		return
	}
	g.failure = failure
	if last.seq+1 != job.seq || last.failure != failure {
		return
	}
	// Keep the diagnostic intact in the refusal. Clipped evidence cannot
	// establish what the model actually needs to correct.
	result, _ := json.Marshal(map[string]any{
		"code": "repeated_exec_failure", "failed": true, "exit_code": failure.exitCode,
		"stdout": failure.stdout, "stderr": failure.stderr, "error": managedExecRetryCue,
	})
	if utf8.RuneCount(result) <= job.limit {
		g.result = string(result)
	}
}

func (job *managedExecSession) completedFailure() (managedExecFailure, bool) {
	command := job.command
	if job.exitCode <= 0 || command.stdout.truncated || command.stderr.truncated {
		return managedExecFailure{}, false
	}
	// A plain process exit may be joined with an observation, cancellation or
	// cleanup error. Only a sole ExitError establishes a completed failure.
	err := job.runErr
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) != 1 {
			return managedExecFailure{}, false
		}
		err = causes[0]
	}
	if _, ok := err.(*exec.ExitError); !ok {
		return managedExecFailure{}, false
	}
	return managedExecFailure{
		identity: job.identity, exitCode: job.exitCode,
		stdout: string(command.stdout.text), stderr: string(command.stderr.text),
	}, true
}

type managedExecRepeatRefusal struct {
	failure  managedExecFailure
	result   string
	terminal bool
}

func (e *managedExecRepeatRefusal) Error() string {
	diagnostic := e.failure.stderr
	if diagnostic == "" {
		diagnostic = e.failure.stdout
	}
	if diagnostic == "" {
		return fmt.Sprintf("%s. %s (exit status %d)", ErrRepeatedManagedExecFailure, managedExecRetryCue, e.failure.exitCode)
	}
	return fmt.Sprintf("%s. %s (exit status %d): %s", ErrRepeatedManagedExecFailure, managedExecRetryCue, e.failure.exitCode, diagnostic)
}

func (*managedExecRepeatRefusal) Unwrap() error { return ErrRepeatedManagedExecFailure }
