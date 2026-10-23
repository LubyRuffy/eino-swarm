package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/cloudwego/eino/components/tool"
)

// Parameter refusals can be repaired. A move that already wrote its target and
// then hit storage failure must finish as an error with those writes retained.
type tidyReclassifyTool struct{ tool.InvokableTool }

func tidyReclassifyTools(mem *memory.Store, onChange func(memory.Change)) []tool.BaseTool {
	tools := memory.ReclassifyTools(mem, onChange)
	for i, t := range tools {
		tools[i] = &tidyReclassifyTool{InvokableTool: t.(tool.InvokableTool)}
	}
	return tools
}

func (t *tidyReclassifyTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	out, err := t.InvokableTool.InvokableRun(ctx, args, opts...)
	if err != nil {
		return out, err
	}
	var result struct {
		Success bool   `json:"success"`
		Changed bool   `json:"changed"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return out, fmt.Errorf("memory tidy: decode move result: %w", err)
	}
	if !result.Success && result.Changed {
		return out, fmt.Errorf("memory tidy: destination changes were saved but reclassification failed: %s", result.Error)
	}
	return out, nil
}
