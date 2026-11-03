package swarm

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/cloudwego/eino/components/tool"
)

type workerToolOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func cloneWorkerToolNames(names *[]string) *[]string {
	if names == nil {
		return nil
	}
	copy := slices.Clone(*names)
	slices.Sort(copy)
	return &copy
}

type namedWorkerTool struct {
	option workerToolOption
	tool   tool.BaseTool
}

func (c WorkerConfig) namedWorkerTools(ctx context.Context) ([]namedWorkerTool, error) {
	named := make([]namedWorkerTool, 0, len(c.SubAgentTools))
	seen := make(map[string]bool, len(c.SubAgentTools))
	for _, candidate := range c.SubAgentTools {
		if candidate == nil {
			return nil, fmt.Errorf("worker tool metadata is unavailable")
		}
		info, err := candidate.Info(ctx)
		if err != nil || info == nil || info.Name == "" {
			return nil, fmt.Errorf("worker tool metadata is unavailable")
		}
		if seen[info.Name] {
			return nil, fmt.Errorf("worker tool %q has ambiguous metadata", info.Name)
		}
		seen[info.Name] = true
		named = append(named, namedWorkerTool{option: workerToolOption{Name: info.Name, Description: info.Desc}, tool: candidate})
	}
	return named, nil
}

func (r *Registry) listWorkerTools(ctx context.Context, _ string) (string, error) {
	config := r.workerConfigurationForContext(ctx)
	if config.SelectableToolNames == nil {
		return ctlRefuse("worker tool selection is not enabled")
	}
	named, err := config.namedWorkerTools(ctx)
	if err != nil {
		return ctlRefuse(err.Error())
	}
	options := []workerToolOption{}
	for _, entry := range named {
		if slices.Contains(config.SelectableToolNames, entry.option.Name) {
			options = append(options, entry.option)
		}
	}
	slices.SortFunc(options, func(a, b workerToolOption) int { return cmp.Compare(a.Name, b.Name) })
	return marshal(map[string]any{"tools": options}), nil
}

// A nil selection inherits the caller's surface. An explicit selection must
// fit that same effective surface, including on delegated worker calls.
func (c WorkerConfig) withWorkerToolSelection(ctx context.Context, names *[]string) (WorkerConfig, error) {
	if names == nil {
		return c, nil
	}
	if c.SelectableToolNames == nil {
		return c, fmt.Errorf("worker tool selection is not enabled")
	}
	if *names == nil {
		return c, fmt.Errorf("tool_names must be an array, not null")
	}
	named, err := c.namedWorkerTools(ctx)
	if err != nil {
		return c, err
	}
	selected := slices.Clone(*names)
	slices.Sort(selected)
	for i, name := range selected {
		if i > 0 && selected[i-1] == name {
			return c, fmt.Errorf("duplicate tool_names entry %q", name)
		}
		if !slices.ContainsFunc(named, func(entry namedWorkerTool) bool { return entry.option.Name == name }) {
			return c, fmt.Errorf("unknown or unavailable worker tool %q; use list_worker_tools", name)
		}
		if !slices.Contains(c.SelectableToolNames, name) {
			return c, fmt.Errorf("worker tool %q is already available. Omit it from tool_names and keep instructions for using it in task", name)
		}
	}
	filtered := make([]tool.BaseTool, 0, len(named))
	for _, entry := range named {
		name := entry.option.Name
		if !slices.Contains(c.SelectableToolNames, name) || slices.Contains(selected, name) {
			filtered = append(filtered, entry.tool)
		}
	}
	c.SubAgentTools = filtered
	c.workerToolNames = &selected
	return c, nil
}

func (c WorkerConfig) restoreWorkerToolSelection(ctx context.Context, names *[]string) (WorkerConfig, error) {
	c.workerToolNames = nil
	config, err := c.withWorkerToolSelection(ctx, names)
	if err != nil {
		return c, fmt.Errorf("restore worker tool selection: %w", err)
	}
	return config, nil
}

func sameWorkerToolSelection(a, b *[]string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return slices.Equal(*a, *b)
}

func checkWorkerToolSelection(existing, requested *[]string) error {
	if requested != nil && !sameWorkerToolSelection(existing, requested) {
		return fmt.Errorf("spawn_agent: tool_names conflicts with the existing worker tool selection; use a distinct role for a different selection")
	}
	return nil
}

// The caller holds r.mu. A cold restore can legitimately encounter a child
// first, so only a known scoped parent constrains stored child metadata here.
func (r *Registry) checkRestoredWorkerToolParentLocked(parentID string, names *[]string) error {
	if parentID == "" {
		return nil
	}
	parent := r.agents[parentID]
	if parent == nil {
		parent = r.pending[parentID]
	}
	var parentNames *[]string
	if parent != nil {
		parent.mu.Lock()
		parentNames = parent.workerToolNames
		parent.mu.Unlock()
	} else if past := r.past[parentID]; past != nil {
		parentNames = past.checkpoint.WorkerToolNames
	}
	if parentNames == nil {
		return nil
	}
	if names == nil {
		return fmt.Errorf("restore: worker tool selection would widen its parent's scope")
	}
	for _, name := range *names {
		if !slices.Contains(*parentNames, name) {
			return fmt.Errorf("restore: worker tool selection would widen its parent's scope")
		}
	}
	return nil
}
