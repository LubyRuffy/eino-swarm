package engine

import (
	"context"
	"encoding/json"

	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// liveToolOffer applies the current built-in and MCP switches to each model
// request. The agent keeps the tools it was built with; ADK persists the tool
// list returned here, and a nil list is filled back in from that original set.
// The remembered list is what lets a switch come back on without waiting for
// another turn. A tool this turn never mounted is still absent until the next
// turn builds a toolset.
type liveToolOffer struct {
	adk.BaseChatModelAgentMiddleware
	known    []*schema.ToolInfo
	deferred []*schema.ToolInfo
	allow    func(string) bool
}

func (e *Engine) liveToolOffer() *liveToolOffer {
	return &liveToolOffer{allow: e.toolOfferAllowed}
}

var _ adk.ChatModelAgentMiddleware = (*liveToolOffer)(nil)

func (e *Engine) toolOfferAllowed(name string) bool {
	if e == nil || e.cfg == nil {
		return true
	}
	snap := e.cfg.Snapshot()
	return tools.Offered(snap.Tools, name) && e.mcpOffered(snap.MCP.Servers, name)
}

func (o *liveToolOffer) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if o == nil || state == nil {
		return ctx, state, nil
	}
	state.ToolInfos = selectOfferedTools(state.ToolInfos, &o.known, o.allow)
	state.DeferredToolInfos = selectOfferedTools(state.DeferredToolInfos, &o.deferred, o.allow)
	return ctx, state, nil
}

func (o *liveToolOffer) WrapInvokableToolCall(_ context.Context, next adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if o.allows(tCtx) {
		return next, nil
	}
	return func(context.Context, string, ...tool.Option) (string, error) {
		return switchedOffToolResult(tCtx.Name), nil
	}, nil
}

func (o *liveToolOffer) WrapStreamableToolCall(_ context.Context, next adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	if o.allows(tCtx) {
		return next, nil
	}
	return func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		return schema.StreamReaderFromArray([]string{switchedOffToolResult(tCtx.Name)}), nil
	}, nil
}

func (o *liveToolOffer) allows(tCtx *adk.ToolContext) bool {
	if o == nil || o.allow == nil || tCtx == nil || tCtx.Name == "" {
		return true
	}
	return o.allow(tCtx.Name)
}

func switchedOffToolResult(name string) string {
	payload, err := json.Marshal(map[string]any{"ok": false, "code": "tool_switched_off", "tool": name})
	if err != nil {
		return `{"ok":false,"code":"tool_switched_off"}`
	}
	return string(payload)
}

// selectOfferedTools remembers every tool seen for this agent and returns the
// ones the current switches still allow. An empty non-nil result stays empty:
// nil would be replaced by the agent's original list on the next request.
func selectOfferedTools(current []*schema.ToolInfo, known *[]*schema.ToolInfo, allow func(string) bool) []*schema.ToolInfo {
	if known == nil {
		empty := []*schema.ToolInfo{}
		known = &empty
	}
	seen := make(map[string]struct{}, len(*known))
	for _, info := range *known {
		if info != nil && info.Name != "" {
			seen[info.Name] = struct{}{}
		}
	}
	for _, info := range current {
		if info == nil || info.Name == "" {
			continue
		}
		if _, ok := seen[info.Name]; ok {
			continue
		}
		*known = append(*known, info)
		seen[info.Name] = struct{}{}
	}
	if len(*known) == 0 {
		if current == nil {
			return nil
		}
		return []*schema.ToolInfo{}
	}
	offered := make([]*schema.ToolInfo, 0, len(*known))
	for _, info := range *known {
		if info == nil || info.Name == "" {
			continue
		}
		if allow == nil || allow(info.Name) {
			offered = append(offered, info)
		}
	}
	return offered
}
