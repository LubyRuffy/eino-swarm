package swarm

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// WorkerConfig is an immutable execution surface for subsequently created
// workers. Existing workers and their descendants retain their own snapshot.
// Concurrency, handoff and Stop remain shared Registry state.
type WorkerConfig struct {
	Tag                string
	ModelBuilder       ModelBuilder
	SelectWorkerModel  func(context.Context, string) (ModelBinding, ModelBuilder, error)
	RestoreWorkerModel func(context.Context, ModelBinding) (ModelBuilder, error)
	// VerifyAdmission is nil outside Nuwa. It refuses a verify or fix that
	// would exceed the host's per-record allowance before any worker starts.
	VerifyAdmission  func(context.Context, string, string, string) error
	EnableDelegation bool
	SubAgentTools    []tool.BaseTool
	// SelectableToolNames opts in to selection of these configured worker tools.
	// Nil disables selection; an empty non-nil slice enables an empty catalog.
	SelectableToolNames []string
	WorkerPreamble      string
	ToolOutputBinder    func(context.Context, func(string), string, string) context.Context
	WorkerContextValues func(context.Context, context.Context) context.Context
	// WorkerMiddlewares builds host-owned handlers once for each actual worker
	// generation. A non-nil factory is retained with its worker's configuration.
	WorkerMiddlewares func(AgentIdentity) []adk.ChatModelAgentMiddleware
	workerToolNames   *[]string
}

func cloneWorkerConfig(config WorkerConfig) WorkerConfig {
	config.SubAgentTools = append([]tool.BaseTool(nil), config.SubAgentTools...)
	config.SelectableToolNames = slices.Clone(config.SelectableToolNames)
	config.workerToolNames = cloneWorkerToolNames(config.workerToolNames)
	return config
}

// WorkerConfiguration returns a detached current configuration. Direct public
// Registry fields remain supported for initialization only; a running host uses
// ConfigureWorkers instead of rewriting them concurrently with worker tools.
func (r *Registry) WorkerConfiguration() WorkerConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workerConfig != nil {
		return cloneWorkerConfig(*r.workerConfig)
	}
	return cloneWorkerConfig(WorkerConfig{ModelBuilder: r.ModelBuilder, SelectWorkerModel: r.SelectWorkerModel, RestoreWorkerModel: r.RestoreWorkerModel, VerifyAdmission: r.VerifyAdmission, EnableDelegation: r.EnableDelegation, SubAgentTools: r.SubAgentTools, SelectableToolNames: r.SelectableToolNames, WorkerPreamble: r.WorkerPreamble, ToolOutputBinder: r.ToolOutputBinder, WorkerContextValues: r.WorkerContextValues})
}

// RetainedWorkerConfigTags returns the distinct host tags of addressable
// workers, including finished workers that can still be collected or resumed.
func (r *Registry) RetainedWorkerConfigTags() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	tags := map[string]bool{}
	for _, workers := range []map[string]*Handle{r.agents, r.pending} {
		for _, worker := range workers {
			worker.mu.Lock()
			tags[worker.configTag] = true
			worker.mu.Unlock()
		}
	}
	for _, worker := range r.past {
		tags[worker.checkpoint.ConfigTag] = true
	}
	delete(tags, "")
	out := make([]string, 0, len(tags))
	for tag := range tags {
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}

// ConfigureWorkers atomically replaces the surface for new manager-dispatched
// workers without changing already-running or archived worker generations.
func (r *Registry) ConfigureWorkers(config WorkerConfig) {
	config = cloneWorkerConfig(config)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workerConfig = &config
	r.delegationUsed = r.delegationUsed || config.EnableDelegation
}

type workerConfigKey struct{}

func withWorkerConfig(ctx context.Context, config WorkerConfig) context.Context {
	return context.WithValue(ctx, workerConfigKey{}, &config)
}

func (r *Registry) workerConfigurationForContext(ctx context.Context) WorkerConfig {
	if config, ok := ctx.Value(workerConfigKey{}).(*WorkerConfig); ok {
		return *config
	}
	return r.WorkerConfiguration()
}

func workerInstructionWithConfig(config WorkerConfig, task string) string {
	preamble := strings.TrimSpace(config.WorkerPreamble)
	if preamble == "" {
		return task
	}
	return preamble + "\n\n" + task
}

func (r *Registry) restoreWorkerConfiguration(ctx context.Context, tag string, enabled *bool) (WorkerConfig, error) {
	config := r.workerConfigurationForContext(ctx)
	if tag != "" {
		if r.ResolveWorkerConfig == nil {
			return WorkerConfig{}, fmt.Errorf("worker configuration %q has no resolver", tag)
		}
		var err error
		config, err = r.ResolveWorkerConfig(ctx, tag)
		if err != nil {
			return WorkerConfig{}, fmt.Errorf("restore worker configuration: %w", err)
		}
		if config.Tag != "" && config.Tag != tag {
			return WorkerConfig{}, fmt.Errorf("worker configuration resolver returned a different tag")
		}
		config.Tag = tag
		if enabled != nil && config.EnableDelegation != *enabled {
			return WorkerConfig{}, fmt.Errorf("worker configuration resolver changed delegation mode")
		}
	} else if enabled != nil {
		config.EnableDelegation = *enabled
	}
	return cloneWorkerConfig(config), nil
}
