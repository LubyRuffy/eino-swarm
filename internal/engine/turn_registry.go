package engine

import (
	"context"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
)

// newTurnRegistry is the one place a conversation's swarm is wired, so a
// resumed turn cannot forget WorkerPreamble the way a copy-pasted block would.
func (e *Engine) newTurnRegistry(builder swarm.ModelBuilder, toolset *tools.Set, pc *projectContext, turns ...*store.Turn) *swarm.Registry {
	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	e.bindSwarmLimits(reg)
	var turn *store.Turn
	if len(turns) > 0 {
		turn = turns[0]
	}
	e.wireWorkerSurface(reg, toolset, pc, turn)
	if turn != nil {
		e.wireWorkerModelRecovery(reg, turn)
		e.wireGraphRegistry(reg, turn)
		e.wireTaskPanelRegistry(reg, toolset, turn)
		config := reg.WorkerConfiguration()
		config.Tag = turn.ID
		config.RecoveredModels = turn.WorkerModelRecovery
		ownerTurn := turn
		if handlers := e.graphContextHandlers(ownerTurn); len(handlers) > 0 {
			config.WorkerMiddlewares = func(swarm.AgentIdentity) []adk.ChatModelAgentMiddleware {
				return append([]adk.ChatModelAgentMiddleware(nil), handlers...)
			}
		}
		config.WorkerContextValues = func(parent, detached context.Context) context.Context {
			detached = historyWorkerContextValues(parent, detached)
			owner, _ := detached.Value(historyReadOwnerKey{}).(historyReadOwner)
			owner.ThreadID, owner.TurnID = ownerTurn.ThreadID, ownerTurn.ID
			return context.WithValue(detached, historyReadOwnerKey{}, owner)
		}
		reg.ConfigureWorkers(config)
		reg.ResolveWorkerConfig = func(ctx context.Context, tag string) (swarm.WorkerConfig, error) {
			return e.workerConfigurationForTag(ctx, ownerTurn.ThreadID, tag)
		}
	}
	e.installLiveToolOffer(reg)
	return reg
}

// installLiveToolOffer gives every worker its own filter. Graph handlers stay
// outside it; each spawn reads the switches again on its next model request.
func (e *Engine) installLiveToolOffer(reg *swarm.Registry) {
	config := reg.WorkerConfiguration()
	previous := config.WorkerMiddlewares
	config.WorkerMiddlewares = func(id swarm.AgentIdentity) []adk.ChatModelAgentMiddleware {
		var out []adk.ChatModelAgentMiddleware
		if previous != nil {
			out = append(out, previous(id)...)
		}
		return append(out, e.liveToolOffer())
	}
	reg.ConfigureWorkers(config)
}
