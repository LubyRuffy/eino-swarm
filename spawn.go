package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// Spawn starts a sub-agent in the background and returns its handle
// immediately. The agent's context derives from ctx: if the Spawn caller is
// extraTools are appended to the sub-agent's toolset on top of
// Registry.SubAgentTools and the mesh send_message tool. The worker does not
// die with ctx: a /goal session yield cancels the manager without killing
// in-flight sub-agents. Handle.Cancel, Cleanup, Close, and AgentTimeout still
// stop them.
func (r *Registry) Spawn(ctx context.Context, role, task string,
	modelOpt ModelBuilder, extraTools ...tool.BaseTool) (*Handle, error) {
	return r.spawnAgent(ctx, role, task, modelOpt, "", false, extraTools...)
}

// spawnAgent is Spawn with an inbox seed applied before the worker goroutine
// starts. fork_context uses it so the first model call cannot beat the
// context it is supposed to inherit. admit refuses instead of queueing when
// the live roster is already at MaxConcurrent; the check and the insert share
// the registry lock so parallel tool calls cannot all slip through.
func (r *Registry) spawnAgent(ctx context.Context, role, task string,
	modelOpt ModelBuilder, seed string, admit bool, extraTools ...tool.BaseTool) (*Handle, error) {
	config := r.workerConfigurationForContext(ctx)
	instruction := workerInstructionWithConfig(config, task)
	r.mu.Lock()
	if r.handingOff {
		r.mu.Unlock()
		return nil, ErrHandoff
	}
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("swarm: registry closed")
	}
	if admit && r.liveCountLocked() >= r.capLocked() {
		n := r.capLocked()
		r.mu.Unlock()
		return nil, &atCapacityError{n: n}
	}
	r.seq++
	id := fmt.Sprintf("%s-%d", role, r.seq)
	h := &Handle{configTag: config.Tag, workerConfig: &config,
		ID: id, Role: role, done: make(chan struct{}), spawned: time.Now(), gen: 1}
	h.handoff.task, h.handoff.instruction = task, instruction
	r.agents[id] = h
	r.notePendingLocked(h)
	hook := r.spawnHook
	r.mu.Unlock()
	if hook != nil {
		hook(role, id, instruction)
	}
	r.startWorker(ctx, h, task, task, instruction, modelOpt, seed, extraTools)
	return h, nil
}
