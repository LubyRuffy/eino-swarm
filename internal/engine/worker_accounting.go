package engine

import (
	"context"
	"sync/atomic"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Existing worker models retain this shared owner when the Registry moves to
// another manager turn. A parked gap keeps the last successfully claimed turn.
type workerAccounting struct {
	registry *swarm.Registry
	engine   *Engine
	threadID string
	turnID   atomic.Pointer[string]
}

// Called with rt.mu held, only after this turn's runtime claim succeeds.
func (rt *runtime) bindWorkerAccountingLocked(reg *swarm.Registry, turnID string) {
	if reg == nil || reg.ModelBuilder == nil {
		return
	}
	owner := rt.workerAccounting
	if owner == nil || owner.registry != reg {
		owner = &workerAccounting{registry: reg, engine: rt.engine, threadID: rt.threadID}
		rt.workerAccounting = owner
	}
	owner.turnID.Store(&turnID)
	builder := reg.ModelBuilder
	reg.ModelBuilder = func(role, id string) model.BaseChatModel {
		cm := builder(role, id)
		if isManagerAgent(id) {
			return cm
		}
		return &workerAccountingModel{BaseChatModel: cm, owner: owner}
	}
}

func (a *workerAccounting) recorder() provider.Recorder {
	return a.engine.callRecorder(a.threadID, *a.turnID.Load())
}

func (a *workerAccounting) recordBackoff(record provider.CallRecord) {
	a.recorder()(record)
}

type workerAccountingModel struct {
	model.BaseChatModel
	owner *workerAccounting
}

func (m *workerAccountingModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.BaseChatModel.Generate(provider.WithCallRecorders(ctx, m.owner.recorder, m.owner.recordBackoff), in, opts...)
}

func (m *workerAccountingModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.BaseChatModel.Stream(provider.WithCallRecorders(ctx, m.owner.recorder, m.owner.recordBackoff), in, opts...)
}
