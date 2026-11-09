package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// ExecSessionToolName is the opt-in name and identifies historical session receipts.
const ExecSessionToolName = "exec_session"

const (
	maxManagedExecTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxManagedExecYieldMS   = 30_000
)

type workspaceExec struct {
	tool.InvokableTool
	legacy       tool.InvokableTool
	workspaceDir string
	resultLimit  int
	sessions     *ManagedExecSessions
}

func newWorkspaceExec(legacy tool.InvokableTool, workspaceDir string, resultLimit int, managed bool, sessions *ManagedExecSessions) *workspaceExec {
	var inner tool.InvokableTool
	if managed {
		inner = wrapExecOutputBudget(&managedWorkspaceExec{
			legacy: legacy, workspaceDir: workspaceDir, resultLimit: resultLimit, sessions: sessions,
		}, resultLimit)
	} else {
		inner = &execSleepBias{inner: wrapExecWorkDir(wrapExecOutputBudget(legacy, resultLimit), workspaceDir)}
	}
	advertised := &advertisedParams{inner: inner}
	return &workspaceExec{
		InvokableTool: &resultCap{inner: advertised, limit: resultLimit, exec: true},
		legacy:        legacy,
		workspaceDir:  workspaceDir,
		resultLimit:   resultLimit,
		sessions:      sessions,
	}
}

func (w *workspaceExec) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	if w.sessions != nil {
		ctx = context.WithValue(ctx, managedExecArgsKey{}, args)
	}
	return w.InvokableTool.InvokableRun(ctx, args, opts...)
}

type managedExecArgsKey struct{}

// ManagedExec selects the owned runner for the original workspace exec tool.
// A turn's session owner lets long calls yield without ending their processes.
func ManagedExec(base tool.BaseTool, background ...*ManagedExecSessions) tool.BaseTool {
	workspace, ok := base.(*workspaceExec)
	if !ok || !ManagedExecSupported() {
		return base
	}
	var sessions *ManagedExecSessions
	if len(background) > 0 {
		sessions = background[0]
	}
	return newWorkspaceExec(workspace.legacy, workspace.workspaceDir, workspace.resultLimit, true, sessions)
}

// ManagedExecSupported reports whether the platform supports group ownership.
func ManagedExecSupported() bool { return managedExecSupported() }

type managedWorkspaceExec struct {
	legacy       tool.InvokableTool
	workspaceDir string
	resultLimit  int
	sessions     *ManagedExecSessions
}

func (m *managedWorkspaceExec) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := m.legacy.Info(ctx)
	if err != nil || info == nil {
		return info, err
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	out := *info
	if m.sessions != nil {
		out.Desc += " Long commands can return a running session. The engine keeps the original command and delivers its terminal result. Continue independent work, or end the current response to await the command. A simple sleep used to poll an owned command waits for its result. Use command sleep when an actual delay is required."
	} else {
		out.Desc += " The engine observes the command while it runs and returns its exit result."
	}
	if params != nil && params.Properties != nil {
		if timeout, ok := params.Properties.Get("timeout_ms"); ok {
			timeout.Description = "Optional hard command deadline in whole milliseconds. Omit or pass 0 to use the owner's lifetime."
		}
		params.Properties.Set("pipefail", &jsonschema.Schema{
			Type: string(schema.Boolean), Description: "Set true when a pipeline must report a failure from any stage.",
		})
		if m.sessions != nil {
			params.Properties.Set("yield_after_ms", &jsonschema.Schema{
				Type:        string(schema.Integer),
				Description: fmt.Sprintf("How long to observe the command before returning a running session, up to %d ms. Omit or pass 0 for %d ms. Use a longer observation for a short operation whose result you need immediately. The command deadline is separate.", maxManagedExecYieldMS, managedExecObservationWindow.Milliseconds()),
			})
		}
		out.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(params)
	}
	return &out, nil
}

func (m *managedWorkspaceExec) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	var params map[string]any
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return refusedExecResult(fmt.Errorf("failed to parse arguments: %w", err)), nil
	}
	command, _ := params["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return refusedExecResult(errors.New("command is required")), nil
	}
	timeoutMS, err := managedExecMillis(params["timeout_ms"], "timeout_ms", maxManagedExecTimeoutMS)
	if err != nil {
		return refusedExecResult(err), nil
	}
	var yieldAfter time.Duration
	if m.sessions != nil {
		yield, explicit := params["yield_after_ms"]
		yieldMS, err := managedExecMillis(yield, "yield_after_ms", maxManagedExecYieldMS)
		if err != nil {
			return refusedExecResult(err), nil
		}
		if explicit {
			yieldAfter = managedExecObservationWindow
			if yieldMS > 0 {
				yieldAfter = time.Duration(yieldMS) * time.Millisecond
			}
		}
	}
	pipefail, valid := params["pipefail"].(bool)
	if !valid && params["pipefail"] != nil {
		return refusedExecResult(errors.New("pipefail must be a boolean; command did not run")), nil
	}

	cwd, err := m.workDir(params)
	if err != nil {
		return refusedExecResult(err), nil
	}
	observation := managedExecObservationFrom(ctx)
	callID := compose.GetToolCallID(ctx)
	if callID == "" {
		callID = observation.callID
	}
	stdin, _ := params["stdin"].(string)
	env, _ := params["env"].(map[string]any)
	shellCommand := command
	var backgroundWait *time.Duration
	var probeDeadline time.Time
	if m.sessions != nil && callID != "" && stdin == "" && len(env) == 0 && os.Getenv("BASH_ENV") == "" {
		if followup, poll := managedExecPollFollowup(command); poll {
			started := time.Now()
			waitCtx := ctx
			var deadline time.Time
			if timeoutMS > 0 {
				deadline = started.Add(time.Duration(timeoutMS) * time.Millisecond)
				var cancel context.CancelFunc
				waitCtx, cancel = context.WithDeadline(ctx, deadline)
				defer cancel()
			}
			pending, err := m.sessions.awaitPollCompletion(waitCtx, callID)
			waited := time.Since(started)
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return m.sessions.pollResult(ctx, callID, command, timeoutMS, waited, true)
			}
			if err != nil {
				return refusedExecResult(err), nil
			}
			if pending {
				if followup == "" {
					return m.sessions.pollResult(ctx, callID, command, timeoutMS, waited, false)
				}
				shellCommand = followup
				backgroundWait = &waited
				probeDeadline = deadline
			}
		}
	}
	shell, err := exec.LookPath("bash")
	if err != nil {
		// Match the legacy workspace tool when PATH has no bash.
		shell = "/bin/bash"
	}
	shellArgs := []string{"--noprofile", "--norc"}
	if pipefail {
		shellArgs = append(shellArgs, "-o", "pipefail")
	}
	cmd := exec.Command(shell, append(shellArgs, "-c", shellCommand)...)
	cmd.Dir = cwd
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			key = strings.TrimSpace(key)
			if value, ok := value.(string); ok && key != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
	}

	maxOutputKB := workspaceExecInt(params["max_output_kb"])
	if maxOutputKB <= 0 {
		maxOutputKB = 256
	}
	outputBytes := min(maxOutputKB, 1024) * 1024
	stdout := &managedExecOutput{limit: outputBytes, listener: observation.listener, stream: "stdout"}
	stderr := &managedExecOutput{limit: outputBytes, listener: observation.listener, stream: "stderr"}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	process := managedExecCommand{
		cmd: cmd, command: command, timeoutMS: timeoutMS, pipefail: pipefail, backgroundWait: backgroundWait, deadline: probeDeadline,
		stdout: stdout, stderr: stderr,
	}
	if m.sessions != nil && callID != "" {
		originalArgs, _ := ctx.Value(managedExecArgsKey{}).(string)
		identity, err := process.identity(stdin)
		if err != nil {
			return refusedExecResult(fmt.Errorf("failed to start command: %w", err)), nil
		}
		return m.sessions.run(ctx, callID, originalArgs, &process, identity, m.resultLimit, yieldAfter)
	}

	wait, cancel, err := process.start(ctx)
	defer cancel()
	if err != nil {
		return refusedExecResult(fmt.Errorf("failed to start command: %w", err)), nil
	}
	exitCode, runErr := wait()
	process.finished = time.Now()
	return process.result("", exitCode, runErr)
}

type managedExecCommand struct {
	cmd            *exec.Cmd
	command        string
	timeoutMS      int64
	pipefail       bool
	backgroundWait *time.Duration
	deadline       time.Time
	started        time.Time
	finished       time.Time
	stdout, stderr *managedExecOutput
}

func (c *managedExecCommand) start(ctx context.Context) (func() (int, error), context.CancelFunc, error) {
	var owner context.Context
	var cancel context.CancelFunc
	switch {
	case !c.deadline.IsZero():
		owner, cancel = context.WithDeadline(ctx, c.deadline)
	case c.timeoutMS > 0:
		owner, cancel = context.WithTimeout(ctx, time.Duration(c.timeoutMS)*time.Millisecond)
	default:
		owner, cancel = context.WithCancel(ctx)
	}
	c.started = time.Now()
	wait, err := startManagedExecProcess(owner, c.cmd)
	return wait, cancel, err
}

func (c *managedExecCommand) result(sessionID string, exitCode int, runErr error) (string, error) {
	payload := map[string]any{
		"exit_code":     exitCode,
		"stdout":        string(c.stdout.text),
		"stderr":        string(c.stderr.text),
		"truncated":     c.stdout.truncated || c.stderr.truncated,
		"elapsed_ms":    c.finished.Sub(c.started).Milliseconds(),
		"workdir":       c.cmd.Dir,
		"resolved_exec": c.cmd.Path,
		"full_command":  c.command,
		"failed":        runErr != nil,
		"timeout_ms":    c.timeoutMS,
	}
	if c.backgroundWait != nil {
		payload["waited_for_background"] = true
		payload["background_wait_ms"] = c.backgroundWait.Milliseconds()
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
		payload["status"] = managedExecTerminalStatus(runErr)
	}
	if runErr != nil {
		payload["error"] = managedExecErrorText(runErr)
	}
	out, err := json.Marshal(payload)
	return string(out), err
}

func managedExecErrorText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return "command timeout or canceled: " + err.Error()
	default:
		return err.Error()
	}
}

func managedExecTerminalStatus(err error) string {
	switch {
	case errors.Is(err, errManagedExecCleanup):
		return "cleanup_failed"
	case errors.Is(err, errManagedExecStalled):
		return "stalled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed_out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "exited"
	}
}

func (m *managedWorkspaceExec) workDir(params map[string]any) (string, error) {
	base := m.workspaceDir
	if override, _ := params["base_dir"].(string); strings.TrimSpace(override) != "" {
		base = strings.TrimSpace(override)
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base_dir: %w", err)
	}
	info, err := os.Stat(base)
	if err != nil {
		return "", fmt.Errorf("base_dir not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("base_dir is not a directory")
	}
	cwd, _ := params["cwd"].(string)
	if strings.TrimSpace(cwd) == "" {
		return base, nil
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(base, cwd)
	}
	cwd = filepath.Clean(cwd)
	if err := checkExecWorkDir(cwd); err != nil {
		return "", err
	}
	return cwd, nil
}

func managedExecMillis(value any, name string, maxMS int64) (int64, error) {
	switch n := value.(type) {
	case nil:
		return 0, nil
	case float64:
		if n >= 0 && n <= float64(maxMS) && n == math.Trunc(n) {
			return int64(n), nil
		}
	case string:
		if ms, err := strconv.ParseInt(n, 10, 64); err == nil && ms >= 0 && ms <= maxMS {
			return ms, nil
		}
	}
	return 0, fmt.Errorf("%s must be a whole number from 0 to %d milliseconds; command did not run", name, maxMS)
}

func workspaceExecInt(value any) int {
	switch n := value.(type) {
	case float64:
		// Keep an unrepresentable explicit positive request above the limit.
		if n >= math.MaxInt {
			return math.MaxInt
		}
		if n > 0 {
			return int(n)
		}
	case string:
		v, err := strconv.Atoi(n)
		if err == nil || errors.Is(err, strconv.ErrRange) {
			return v
		}
	}
	return 0
}

type managedExecOutput struct {
	text      []byte
	limit     int
	truncated bool
	stream    string
	listener  einoexec.OutputListener
	liveBytes int
	progress  *managedExecProgress
}

func (o *managedExecOutput) Write(p []byte) (int, error) {
	if o.progress != nil && len(p) != 0 {
		o.progress.write(o.stream, p)
	}
	n := min(len(p), o.limit-len(o.text))
	o.text = append(o.text, p[:n]...)
	o.truncated = o.truncated || n < len(p)
	// Bound input to the accumulating live binder while leaving enough bytes
	// for the first omitted rune, which makes its normal clipping visible.
	if o.listener != nil {
		live := min(len(p), (liveExecNotifyLimit+1)*utf8.UTFMax-o.liveBytes)
		if live > 0 {
			o.listener(o.stream, p[:live])
			o.liveBytes += live
		}
	}
	return len(p), nil
}
