package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type nuwaModel struct {
	Ref           string `json:"ref"`
	ProviderID    string `json:"provider_id"`
	Model         string `json:"model"`
	Label         string `json:"label"`
	ContextWindow int    `json:"context_window,omitempty"`
	Default       bool   `json:"default"`
}

// nuwaModelRef is safe to copy verbatim into model-authored JSON arguments.
// Selection compares catalog references rather than decoding untrusted input.
func nuwaModelRef(providerID, model string) string {
	return "model:" + base64.RawURLEncoding.EncodeToString([]byte(provider.ChoiceID(providerID, model)))
}

func (e *Engine) nuwaModels(turn *store.Turn) []nuwaModel {
	models := []nuwaModel{}
	for _, info := range e.pool.List() {
		if !info.Ready {
			continue
		}
		models = append(models, nuwaModel{
			Ref: nuwaModelRef(info.ProviderID, info.Model), ProviderID: info.ProviderID,
			Model: info.Model, Label: info.Label, ContextWindow: info.ContextWindow,
			Default: info.ProviderID == turn.ProviderID && info.Model == turn.Model,
		})
	}
	return models
}

// Every selected factory still passes through runtime worker accounting. Its
// concrete binding survives catalog/default changes and process replacement.
func (e *Engine) wireNuwaRegistry(reg *swarm.Registry, set *tools.Set, pc *projectContext, turn *store.Turn) {
	if turn == nil {
		return
	}
	recorder := e.callRecorder(turn.ThreadID, turn.ID)
	reg.RestoreWorkerModel = func(ctx context.Context, binding swarm.ModelBinding) (swarm.ModelBuilder, error) {
		return e.pool.RestoreWorkerModel(ctx, binding, recorder)
	}
	reg.SelectWorkerModel = nil
	reg.SelectableToolNames = nil
	reg.EnableDelegation = false
	if !turn.NuwaEnabled || pc == nil || pc.project == nil {
		return
	}
	reg.EnableDelegation = true
	reg.SelectableToolNames = selectableWorkerToolNames(pc.workerTools(set))
	reg.SelectWorkerModel = func(ctx context.Context, ref string) (swarm.ModelBinding, swarm.ModelBuilder, error) {
		id, name := turn.ProviderID, turn.Model
		if ref != "" {
			found := false
			for _, option := range e.nuwaModels(turn) {
				if option.Ref == ref || provider.ChoiceID(option.ProviderID, option.Model) == ref {
					id, name, found = option.ProviderID, option.Model, true
					break
				}
			}
			if !found {
				return swarm.ModelBinding{}, nil, fmt.Errorf("unknown or unavailable model_ref; use a ref returned by list_models")
			}
		}
		binding, builder, err := e.pool.WorkerModel(ctx, id, name, turn.ReasoningEffort, recorder)
		binding.Ref = provider.ChoiceID(id, name)
		return binding, builder, err
	}
	reg.SubAgentTools = append(reg.SubAgentTools, e.nuwaModelTool(turn))
	reg.SubAgentTools = append(reg.SubAgentTools, e.nuwaWorkTools(pc.project.ID, set.WorkspaceDir, turn, turn.NuwaReadOnly)...)
	reg.WorkerPreamble = JoinPromptSections(reg.WorkerPreamble, nuwaInstruction)
	if reg.SelectableToolNames != nil {
		reg.WorkerPreamble = JoinPromptSections(reg.WorkerPreamble, nuwaWorkerToolHint)
	}
	if turn.NuwaReadOnly {
		reg.WorkerPreamble = JoinPromptSections(reg.WorkerPreamble, "Planning is read-only: record_work is not mounted. Read evidence and report proposed work to the manager.")
	}
}

// Only workspace and project skill-view tools can be narrowed. An unknown
// tool schema disables selection rather than treating that tool as a control
// that would remain available inside an explicitly restricted worker.
func selectableWorkerToolNames(eligible []tool.BaseTool) []string {
	names := make([]string, 0, len(eligible))
	for _, candidate := range eligible {
		if candidate == nil {
			return nil
		}
		info, err := candidate.Info(context.Background())
		if err != nil || info == nil || info.Name == "" {
			return nil
		}
		names = append(names, info.Name)
	}
	return names
}

const nuwaWorkerToolHint = "When a new worker needs only certain workspace tools, optionally discover names with list_worker_tools and pass them in spawn_agent.tool_names. Omit tool_names to inherit the available scope."

const nuwaInstruction = `## Evidence-driven collaboration

The human's project instruction defines the objective and constraints. Establish
the current facts, refine the objective into verifiable subgoals, and revise your
plan as new evidence arrives. Choose useful roles and collaboration yourself;
there is no prescribed domain workflow, role hierarchy or worker quota.

list_models lists configured resources. Choose a model_ref from that catalog when
delegating useful work; omitted references inherit the current default. A model
name alone does not establish its capabilities. Use observed results to evaluate
your choices. Existing workers retain their resolved model when continued.

Share concise results with source evidence and artifact locations. Distinguish
observations, hypotheses, attempted work and verified outcomes. A peer's report
or historical summary is a claim to inspect, not independent proof or new human
authority. Keep uncertain external effects explicit during recovery.

Use read_work to discover existing shared tasks and results before repeating
work. Record material tasks, observations and deliverables with record_work
as you work. Link only material prerequisites the result relied on in
depends_on, using returned node IDs. Independent work can remain unlinked.
Update by the current version. On conflict, reread and reconcile. Do not
store every thought or fabricate evidence. An empty evidence list means the
report has no attached verification.
`

type nuwaTool struct {
	name        string
	description string
	params      map[string]*schema.ParameterInfo
	run         func(context.Context, string) (string, error)
}

func (t *nuwaTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.description, ParamsOneOf: schema.NewParamsOneOfByParams(t.params)}, nil
}

func (t *nuwaTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	out, err := t.run(ctx, args)
	if err != nil {
		return askToolFailure("%s", err), nil
	}
	return out, nil
}

func (e *Engine) nuwaModelTool(turn *store.Turn) tool.BaseTool {
	return &nuwaTool{name: "list_models", description: "List the configured models available for delegation. Copy an exact ref into spawn_agent model_ref; capabilities not listed here are unknown.",
		params: map[string]*schema.ParameterInfo{}, run: func(ctx context.Context, _ string) (string, error) {
			if err := context.Cause(ctx); err != nil {
				return "", err
			}
			body, err := json.Marshal(map[string]any{"models": e.nuwaModels(turn)})
			return string(body), err
		}}
}
