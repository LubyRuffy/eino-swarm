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
	if reg == nil {
		return
	}
	config := reg.WorkerConfiguration()
	if config.ModelBuilder == nil {
		return
	}
	owner := rt.workerAccounting
	if owner == nil || owner.registry != reg {
		owner = &workerAccounting{registry: reg, engine: rt.engine, threadID: rt.threadID}
		rt.workerAccounting = owner
		// This is still admission of a fresh registry, before any worker runs.
		// Retained registries keep this stable resolver and dynamic owner.
		if resolve := reg.ResolveWorkerConfig; resolve != nil {
			reg.ResolveWorkerConfig = func(ctx context.Context, tag string) (swarm.WorkerConfig, error) {
				config, err := resolve(ctx, tag)
				if err != nil {
					return swarm.WorkerConfig{}, err
				}
				return accountWorkerConfig(config, owner), nil
			}
		}
	}
	owner.turnID.Store(&turnID)
	reg.ConfigureWorkers(accountWorkerConfig(config, owner))
}

func accountWorkerConfig(config swarm.WorkerConfig, owner *workerAccounting) swarm.WorkerConfig {
	wrapBuilder := func(builder swarm.ModelBuilder) swarm.ModelBuilder {
		if builder == nil {
			return nil
		}
		return func(role, id string) model.BaseChatModel {
			if isManagerAgent(id) {
				return builder(role, id)
			}
			return &workerAccountingModel{BaseChatModel: builder(role, id), owner: owner}
		}
	}
	config.ModelBuilder = wrapBuilder(config.ModelBuilder)
	if selectModel := config.SelectWorkerModel; selectModel != nil {
		config.SelectWorkerModel = func(ctx context.Context, ref string) (swarm.ModelBinding, swarm.ModelBuilder, error) {
			binding, builder, err := selectModel(ctx, ref)
			return binding, wrapBuilder(builder), err
		}
	}
	if restoreModel := config.RestoreWorkerModel; restoreModel != nil {
		config.RestoreWorkerModel = func(ctx context.Context, binding swarm.ModelBinding) (swarm.ModelBuilder, error) {
			builder, err := restoreModel(ctx, binding)
			return wrapBuilder(builder), err
		}
	}
	return config
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
