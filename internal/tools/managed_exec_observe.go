package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	managedExecWaitIdle  = time.Minute
	managedExecTailBytes = 4096
)

// ObservationTools exposes read-only observations of this turn's yielded execs.
func (s *ManagedExecSessions) ObservationTools() []tool.BaseTool {
	return []tool.BaseTool{
		&managedExecObserver{sessions: s},
		&managedExecObserver{sessions: s, wait: true},
	}
}

type managedExecObserver struct {
	sessions *ManagedExecSessions
	wait     bool
}

func (o *managedExecObserver) Info(context.Context) (*schema.ToolInfo, error) {
	info := &schema.ToolInfo{
		Name: "exec_status",
		Desc: "Read a background exec's current status and recent stdout and stderr. A settled command returns its terminal status and exit code. Use the session ID from an exec in this turn.",
	}
	params := map[string]*schema.ParameterInfo{
		"session_id":     {Type: schema.String, Required: true, Desc: "The session_id returned by the original running exec."},
		"after_revision": {Type: schema.Integer, Desc: "Last revision observed for this session. If unchanged, return status without repeating output. Omit for full recent output."},
	}
	if o.wait {
		info.Name = "wait_exec"
		info.Desc = "Wait for one background exec by session ID. Another uncollected background command, or a foreground command in this manager model's current tool batch, settling returns early with code peer_exec_settled and the requested command's status. The engine preserves the completed result under its original call ID. New stdout or stderr renews the idle observation window. If the window expires, returns running and recent output while the command continues."
		params["idle_timeout_ms"] = &schema.ParameterInfo{
			Type: schema.Integer,
			Desc: "Maximum silence during this wait, in whole milliseconds. Omit or pass 0 for 60000. This is an observation window, not a command deadline.",
		}
	}
	info.ParamsOneOf = schema.NewParamsOneOfByParams(params)
	return info, nil
}

func (o *managedExecObserver) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	var request struct {
		SessionID     string  `json:"session_id"`
		IdleTimeoutMS int64   `json:"idle_timeout_ms"`
		AfterRevision *uint64 `json:"after_revision"`
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return encodeManagedExecObservation(managedExecView{
			Status: "error", Code: "invalid_exec_observation", Error: "Invalid exec observation arguments: " + err.Error(),
		}, 0)
	}
	id := strings.TrimSpace(request.SessionID)
	if id == "" || request.IdleTimeoutMS < 0 || request.IdleTimeoutMS > maxManagedExecTimeoutMS {
		return encodeManagedExecObservation(managedExecView{
			Status: "error", Code: "invalid_exec_observation",
			Error: fmt.Sprintf("session_id is required and idle_timeout_ms must be a whole number from 0 to %d.", maxManagedExecTimeoutMS),
		}, 0)
	}
	var afterRevision uint64
	if request.AfterRevision != nil {
		afterRevision = *request.AfterRevision
		if afterRevision == 0 {
			return encodeManagedExecObservation(managedExecView{
				Status: "error", Code: "invalid_exec_observation", Error: "after_revision must be a positive whole number.",
			}, 0)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	o.sessions.mu.Lock()
	progress := o.sessions.observations[id]
	o.sessions.mu.Unlock()
	if progress == nil {
		return encodeManagedExecObservation(managedExecView{
			SessionID: id, Status: "unknown", Code: "unknown_exec_session",
			Error: "No background exec with this ID belongs to the current turn.",
		}, 0)
	}
	if !o.wait {
		progress.mu.Lock()
		view := progress.view(id, false, afterRevision)
		progress.mu.Unlock()
		return encodeManagedExecObservation(view, progress.limit)
	}
	idle := managedExecWaitIdle
	if request.IdleTimeoutMS > 0 {
		idle = time.Duration(request.IdleTimeoutMS) * time.Millisecond
	}
	view, err := progress.wait(ctx, o.sessions, id, idle, afterRevision)
	if err != nil {
		return "", err
	}
	out, err := encodeManagedExecObservation(view, progress.limit)
	if err == nil {
		o.sessions.learnAdaptiveObservation(ctx, compose.GetToolCallID(ctx), view)
	}
	return out, err
}

type managedExecView struct {
	SessionID           string    `json:"session_id,omitempty"`
	OriginalCallID      string    `json:"original_call_id,omitempty"`
	Status              string    `json:"status"`
	Code                string    `json:"code,omitempty"`
	Revision            uint64    `json:"revision,omitempty"`
	Unchanged           bool      `json:"unchanged,omitempty"`
	Stdout              *string   `json:"stdout,omitempty"`
	Stderr              *string   `json:"stderr,omitempty"`
	Truncated           bool      `json:"truncated"`
	LastOutputAt        time.Time `json:"last_output_at,omitzero"`
	ElapsedMS           int64     `json:"elapsed_ms"`
	TimeoutMS           int64     `json:"timeout_ms"`
	ExitCode            *int      `json:"exit_code,omitempty"`
	Failed              bool      `json:"failed,omitempty"`
	Error               string    `json:"error,omitempty"`
	ObservationTimedOut bool      `json:"observation_timed_out,omitempty"`
	elapsed             time.Duration
}

func encodeManagedExecObservation(view managedExecView, limit int) (string, error) {
	if !view.Unchanged && view.Stdout == nil {
		// Error and unknown-session receipts keep their existing empty streams.
		empty := ""
		view.Stdout, view.Stderr = &empty, &empty
	}
	out, err := json.Marshal(view)
	if err != nil || limit <= 0 {
		return string(out), err
	}
	return capExecResult(string(out), limit), nil
}

// This object holds only bounded recent output and terminal scalars. The turn
// can retain it after the full command result and OS process are released.
type managedExecProgress struct {
	mu         sync.Mutex
	callID     string
	revision   uint64
	started    time.Time
	lastOutput time.Time
	finished   time.Time
	timeoutMS  int64
	limit      int
	stdout     []byte
	stderr     []byte
	truncated  bool
	status     string
	exitCode   int
	err        string
	changed    chan struct{}
}

func (p *managedExecProgress) write(stream string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tail := &p.stdout
	if stream == "stderr" {
		tail = &p.stderr
	}
	var clipped bool
	if len(data) >= managedExecTailBytes {
		clipped = len(*tail) > 0 || len(data) > managedExecTailBytes
		*tail = append((*tail)[:0], data[len(data)-managedExecTailBytes:]...)
	} else {
		if drop := len(*tail) + len(data) - managedExecTailBytes; drop > 0 {
			copy(*tail, (*tail)[drop:])
			*tail = (*tail)[:len(*tail)-drop]
			clipped = true
		}
		*tail = append(*tail, data...)
	}
	if clipped {
		p.truncated = true
		// A byte cutoff can bisect the leading rune. Keep the tail readable.
		start := 0
		for start < len(*tail) && !utf8.RuneStart((*tail)[start]) {
			start++
		}
		if start > 0 {
			*tail = (*tail)[:copy(*tail, (*tail)[start:])]
		}
	}
	p.lastOutput = time.Now()
	p.revision++
	p.signal()
}

func (p *managedExecProgress) finish(finished time.Time, exitCode int, runErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finished = finished
	p.status = managedExecTerminalStatus(runErr)
	p.exitCode = exitCode
	p.err = managedExecErrorText(runErr)
	p.revision++
	p.signal()
}

// signal must run with mu held. Allocate the next broadcast only when an
// observer actually needs it, so streaming output can stay allocation-free.
func (p *managedExecProgress) signal() {
	if p.changed != nil {
		close(p.changed)
		p.changed = nil
	}
}

func (p *managedExecProgress) view(id string, expired bool, afterRevision uint64) managedExecView {
	elapsed := time.Since(p.started)
	view := managedExecView{
		SessionID: id, OriginalCallID: p.callID, Status: "running",
		Revision: p.revision, Unchanged: afterRevision == p.revision,
		Truncated: p.truncated, LastOutputAt: p.lastOutput.UTC(), TimeoutMS: p.timeoutMS,
		ObservationTimedOut: expired,
	}
	if !view.Unchanged {
		stdout, stderr := string(p.stdout), string(p.stderr)
		view.Stdout, view.Stderr = &stdout, &stderr
	}
	if !p.finished.IsZero() {
		elapsed = p.finished.Sub(p.started)
		exitCode := p.exitCode
		view.Status, view.ExitCode, view.Error = p.status, &exitCode, p.err
		view.Failed = p.err != ""
		view.ObservationTimedOut = false
	}
	view.ElapsedMS, view.elapsed = elapsed.Milliseconds(), elapsed
	return view
}

func (p *managedExecProgress) wait(ctx context.Context, owner *ManagedExecSessions, id string, idle time.Duration, afterRevision uint64) (managedExecView, error) {
	entered := time.Now()
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		// Completion holds owner.mu before progress.mu. Snapshot in that order
		// and keep its broadcast channel so a peer exit cannot be missed.
		ready, _, foreground, ownerChanged := owner.observationState()
		p.mu.Lock()
		last := entered
		if p.lastOutput.After(last) {
			last = p.lastOutput
		}
		remaining := time.Until(last.Add(idle))
		if !p.finished.IsZero() {
			view := p.view(id, false, afterRevision)
			p.mu.Unlock()
			return view, nil
		}
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return managedExecView{}, err
		}
		if err := owner.ctx.Err(); err != nil {
			p.mu.Unlock()
			return managedExecView{}, err
		}
		for _, job := range ready {
			if job.sessionID != id {
				view := p.view(id, false, afterRevision)
				view.Code = "peer_exec_settled"
				p.mu.Unlock()
				return view, nil
			}
		}
		if foreground {
			view := p.view(id, false, afterRevision)
			view.Code = "peer_exec_settled"
			p.mu.Unlock()
			return view, nil
		}
		if remaining <= 0 {
			view := p.view(id, remaining <= 0, afterRevision)
			p.mu.Unlock()
			return view, nil
		}
		if p.changed == nil {
			p.changed = make(chan struct{})
		}
		changed := p.changed
		p.mu.Unlock()

		timer.Reset(remaining)
		select {
		case <-ctx.Done():
			return managedExecView{}, ctx.Err()
		case <-owner.ctx.Done():
			return managedExecView{}, owner.ctx.Err()
		case <-changed:
		case <-ownerChanged:
		case <-timer.C:
		}
	}
}
