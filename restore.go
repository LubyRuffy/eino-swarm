package swarm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// RestoredWorker is a sub-agent that was still running when the previous
// process died. Restore starts it under the same id so wait_agents and the
// roster keep working.
type RestoredWorker struct {
	ConfigTag         string        `json:"config_tag,omitempty"`
	DelegationEnabled *bool         `json:"delegation_enabled,omitempty"`
	WorkerToolNames   *[]string     `json:"worker_tool_names,omitempty"`
	ParentID          string        `json:"parent_id,omitempty"`
	ModelBinding      *ModelBinding `json:"model_binding,omitempty"`
	Generation        int           `json:"generation,omitempty"`
	Collected         bool          `json:"collected,omitempty"`
	CloseRequested    bool          `json:"close_requested,omitempty"`
	ID                string
	Role              string
	Instruction       string // stored system prompt; empty means workerInstruction(Task)
	Task              string
	Seed              string            // conversation so far, injected before the first model call
	TimeoutFailures   int               // consecutive watchdog stops before the process died
	History           []*schema.Message // structured replay, preferred over a flattened Seed
	Inbox             []string          // messages not yet delivered before the process stopped
}

// FinishedWorker is a sub-agent that already completed in the previous
// process. PlantFinished puts it back so wait_agents does not report unknown.
type FinishedWorker struct {
	ConfigTag         string        `json:"config_tag,omitempty"`
	DelegationEnabled *bool         `json:"delegation_enabled,omitempty"`
	WorkerToolNames   *[]string     `json:"worker_tool_names,omitempty"`
	ParentID          string        `json:"parent_id,omitempty"`
	ModelBinding      *ModelBinding `json:"model_binding,omitempty"`
	Generation        int           `json:"generation,omitempty"`
	Collected         bool          `json:"collected,omitempty"`
	CloseRequested    bool          `json:"close_requested,omitempty"`
	ID                string
	Role              string
	Result            string
	Err               error
	History           []adk.Message
	TimeoutFailures   int
}

// Restore starts a worker under a known agent_id. Unlike Spawn it does not
// mint a new id; unlike Resume the worker is treated as still in flight, not
// as a finished conversation being continued by the manager.
func (r *Registry) Restore(ctx context.Context, w RestoredWorker, modelOpt ModelBuilder, extraTools ...tool.BaseTool) (*Handle, error) {
	id := strings.TrimSpace(w.ID)
	role := strings.TrimSpace(w.Role)
	task := strings.TrimSpace(w.Task)
	if id == "" {
		return nil, fmt.Errorf("restore: agent_id is required")
	}
	if role == "" {
		return nil, fmt.Errorf("restore: role is required")
	}
	if task == "" {
		return nil, fmt.Errorf("restore: task is required")
	}
	if err := r.validateRestoredParent(id, w.ParentID); err != nil {
		return nil, err
	}
	config, err := r.restoreWorkerConfiguration(ctx, w.ConfigTag, w.DelegationEnabled)
	if err != nil {
		return nil, err
	}
	config, err = config.restoreWorkerToolSelection(ctx, w.WorkerToolNames)
	if err != nil {
		return nil, err
	}
	ctx = withWorkerConfig(ctx, config)
	if config.workerToolNames != nil || (w.ConfigTag != "" && len(extraTools) == 0) {
		extraTools = append(append([]tool.BaseTool(nil), config.SubAgentTools...), r.SendTool())
	}
	instruction := strings.TrimSpace(w.Instruction)
	if instruction == "" {
		instruction = workerInstructionWithConfig(config, task)
	}
	if modelOpt == nil {
		modelOpt = config.ModelBuilder
	}
	modelOpt, err = r.restoreWorkerModel(ctx, w.ModelBinding, modelOpt)
	if err != nil {
		return nil, err
	}
	h, err := r.claimForRestore(id, role, w.TimeoutFailures, w.ParentID, config.workerToolNames)
	if err != nil {
		return nil, err
	}
	r.restoreCollection(h, w.Generation, w.Collected, w.CloseRequested)
	h.mu.Lock()
	h.configTag = config.Tag
	enabled := config.EnableDelegation
	h.delegationEnabled = &enabled
	h.modelBinding = cloneModelBinding(w.ModelBinding)
	h.handoff.task, h.handoff.instruction = task, instruction
	h.handoff.restoreHistory = cloneMessages(w.History)
	h.inbox = append(h.inbox, w.Inbox...)
	h.mu.Unlock()
	r.mu.Lock()
	hook := r.spawnHook
	r.mu.Unlock()
	if hook != nil {
		hook(role, id, instruction)
	}
	r.startWorker(ctx, h, task, instruction, modelOpt, w.Seed, extraTools)
	return h, nil
}

func (r *Registry) claimForRestore(id, role string, failures int, parentID string, toolNames *[]string) (*Handle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handingOff {
		return nil, ErrHandoff
	}
	if r.closed {
		return nil, fmt.Errorf("restore: registry closed")
	}
	if err := r.validateParentRelationLocked(id, parentID); err != nil {
		return nil, err
	}
	if err := r.checkRestoredWorkerToolParentLocked(parentID, toolNames); err != nil {
		return nil, err
	}
	if p := r.past[id]; p != nil {
		if !sameWorkerToolSelection(p.checkpoint.WorkerToolNames, toolNames) {
			return nil, fmt.Errorf("restore: worker tool selection cannot change")
		}
		if p.TimeoutFailures > failures {
			failures = p.TimeoutFailures
		}
	}
	if cur, ok := r.agents[id]; ok {
		if cur.ParentID != parentID {
			return nil, fmt.Errorf("restore: worker parent identity cannot change")
		}
		if _, _, finished := cur.Result(); !finished {
			return nil, fmt.Errorf("restore: agent %q is still running; use send_message", id)
		}
		cur.mu.Lock()
		if !sameWorkerToolSelection(cur.workerToolNames, toolNames) {
			cur.mu.Unlock()
			return nil, fmt.Errorf("restore: worker tool selection cannot change")
		}
		if cur.timeoutFailures > failures {
			failures = cur.timeoutFailures
		}
		cur.mu.Unlock()
		if err := timeoutRecoveryError(id, failures); err != nil {
			return nil, err
		}
		cur.resetForResume(id)
		r.notePendingLocked(cur)
		cur.mu.Lock()
		cur.timeoutFailures = failures
		cur.mu.Unlock()
		r.noteIDLocked(id)
		return cur, nil
	}
	if err := timeoutRecoveryError(id, failures); err != nil {
		return nil, err
	}
	h := &Handle{
		ID: id, Role: role, ParentID: parentID, done: make(chan struct{}),
		spawned: time.Now(), resumedFrom: id, gen: 1, timeoutFailures: failures,
		workerToolNames: cloneWorkerToolNames(toolNames),
	}
	r.agents[id] = h
	r.notePendingLocked(h)
	r.noteIDLocked(id)
	return h, nil
}

// PlantFinished registers an already-completed worker so wait_agents and
// resume_agent still resolve the id after a process restart.
func (r *Registry) PlantFinished(w FinishedWorker) error {
	id := strings.TrimSpace(w.ID)
	if id == "" {
		return fmt.Errorf("restore: agent_id is required")
	}
	if w.WorkerToolNames != nil && *w.WorkerToolNames == nil {
		return fmt.Errorf("restore: worker tool selection must not be null")
	}
	role := strings.TrimSpace(w.Role)
	if role == "" {
		role = id
	}
	if err := r.validateRestoredParent(id, w.ParentID); err != nil {
		return err
	}
	h := &Handle{
		configTag: w.ConfigTag, delegationEnabled: w.DelegationEnabled, ID: id, Role: role, ParentID: w.ParentID, done: make(chan struct{}),
		spawned: time.Now(), finished: time.Now(),
		result: w.Result, err: w.Err, history: cloneMessages(w.History), timeoutFailures: w.TimeoutFailures,
		gen: w.Generation, collected: w.Collected, closeRequested: w.CloseRequested,
		modelBinding: cloneModelBinding(w.ModelBinding), workerToolNames: cloneWorkerToolNames(w.WorkerToolNames),
	}
	if h.gen <= 0 {
		h.gen = 1
		h.collected = true // legacy finished rosters predate collection tracking
	}
	close(h.done)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("restore: registry closed")
	}
	if err := r.validateParentRelationLocked(id, w.ParentID); err != nil {
		r.mu.Unlock()
		return err
	}
	if err := r.checkRestoredWorkerToolParentLocked(w.ParentID, w.WorkerToolNames); err != nil {
		r.mu.Unlock()
		return err
	}
	if cur, ok := r.agents[id]; ok {
		r.mu.Unlock()
		if _, _, finished := cur.Result(); !finished {
			return fmt.Errorf("restore: agent %q is still running; use send_message", id)
		}
		return nil
	}
	if past := r.past[id]; past != nil && !sameWorkerToolSelection(past.checkpoint.WorkerToolNames, h.workerToolNames) {
		r.mu.Unlock()
		return fmt.Errorf("restore: worker tool selection cannot change")
	}
	r.agents[id] = h
	r.notePendingLocked(h)
	r.noteIDLocked(id)
	r.mu.Unlock()
	r.remember(h)
	return nil
}

func (r *Registry) noteIDLocked(id string) {
	if n := trailingSeq(id); n > r.seq {
		r.seq = n
	}
}

func trailingSeq(id string) int {
	i := strings.LastIndex(id, "-")
	if i < 0 || i+1 >= len(id) {
		return 0
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Registry) applyRunWorkers(ctx context.Context, cfg RunConfig) error {
	for _, w := range cfg.FinishedWorkers {
		if err := r.PlantFinished(w); err != nil {
			return err
		}
	}
	if len(cfg.RestoreWorkers) == 0 {
		return nil
	}
	config := r.WorkerConfiguration()
	tools := append(append([]tool.BaseTool(nil), config.SubAgentTools...), r.SendTool())
	for _, w := range cfg.RestoreWorkers {
		workerTools := tools
		if w.ConfigTag != "" {
			workerTools = nil
		}
		if _, err := r.Restore(ctx, w, nil, workerTools...); err != nil {
			return err
		}
	}
	return nil
}
