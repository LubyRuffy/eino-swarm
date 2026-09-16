package swarm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// RestoredWorker is a sub-agent that was still running when the previous
// process died. Restore starts it under the same id so wait_agents and the
// roster keep working.
type RestoredWorker struct {
	ID          string
	Role        string
	Instruction string // stored system prompt; empty means workerInstruction(Task)
	Task        string
	Seed        string // conversation so far, injected before the first model call
}

// FinishedWorker is a sub-agent that already completed in the previous
// process. PlantFinished puts it back so wait_agents does not report unknown.
type FinishedWorker struct {
	ID      string
	Role    string
	Result  string
	Err     error
	History []adk.Message
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
	instruction := strings.TrimSpace(w.Instruction)
	if instruction == "" {
		instruction = r.workerInstruction(task)
	}
	if modelOpt == nil {
		modelOpt = r.ModelBuilder
	}
	h, err := r.claimForRestore(id, role)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	hook := r.spawnHook
	r.mu.Unlock()
	if hook != nil {
		hook(role, id, instruction)
	}
	r.startWorker(ctx, h, task, instruction, modelOpt, w.Seed, extraTools)
	return h, nil
}

func (r *Registry) claimForRestore(id, role string) (*Handle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("restore: registry closed")
	}
	if cur, ok := r.agents[id]; ok {
		if _, _, finished := cur.Result(); !finished {
			return nil, fmt.Errorf("restore: agent %q is still running; use send_message", id)
		}
		cur.resetForResume(id)
		r.noteIDLocked(id)
		return cur, nil
	}
	h := &Handle{
		ID: id, Role: role, done: make(chan struct{}),
		spawned: time.Now(), resumedFrom: id, gen: 1,
	}
	r.agents[id] = h
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
	role := strings.TrimSpace(w.Role)
	if role == "" {
		role = id
	}
	h := &Handle{
		ID: id, Role: role, done: make(chan struct{}),
		spawned: time.Now(), finished: time.Now(),
		result: w.Result, err: w.Err, history: cloneMessages(w.History),
	}
	close(h.done)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("restore: registry closed")
	}
	if cur, ok := r.agents[id]; ok {
		r.mu.Unlock()
		if _, _, finished := cur.Result(); !finished {
			return fmt.Errorf("restore: agent %q is still running; use send_message", id)
		}
		return nil
	}
	r.agents[id] = h
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
	tools := make([]tool.BaseTool, 0, len(r.SubAgentTools)+1)
	tools = append(tools, r.SubAgentTools...)
	tools = append(tools, r.SendTool())
	for _, w := range cfg.RestoreWorkers {
		if _, err := r.Restore(ctx, w, r.ModelBuilder, tools...); err != nil {
			return err
		}
	}
	return nil
}
