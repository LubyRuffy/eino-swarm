package tools

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// hiddenBaseDir is the path-root parameter the workspace tools accept but do
// not advertise. Relative paths already resolve against the workspace and an
// absolute path reaches anything else, so the parameter only added a way to
// guess wrong: a worker that knew the host's home directory but not the
// workspace passed home as base_dir and searched the whole disk. Its schema
// entry was also resent on every model call, once per tool. Calls that still
// send it, as older transcripts do, keep working.
const hiddenBaseDir = "base_dir"

type advertisedParams struct {
	inner tool.InvokableTool
}

func wrapAdvertisedParams(t tool.BaseTool) tool.BaseTool {
	inv, ok := t.(tool.InvokableTool)
	if !ok {
		return t
	}
	return &advertisedParams{inner: inv}
}

func (a *advertisedParams) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := a.inner.Info(ctx)
	if err != nil || info == nil {
		return info, err
	}
	return withoutParam(info, hiddenBaseDir), nil
}

func (a *advertisedParams) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	return a.inner.InvokableRun(ctx, args, opts...)
}

// withoutParam returns a copy of info without the named parameter, in either
// of eino's parameter forms. The JSON round trip is the copy: the upstream
// ToolInfo is shared and must not change. Anything it cannot rewrite is
// advertised unchanged.
func withoutParam(info *schema.ToolInfo, name string) *schema.ToolInfo {
	raw, err := json.Marshal(info)
	if err != nil {
		return info
	}
	// Once info encodes, every later step decodes or re-encodes plain JSON
	// values and cannot fail.
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	removed := false
	if params, ok := doc["params"].(map[string]any); ok {
		if _, ok := params[name]; ok {
			delete(params, name)
			removed = true
		}
	}
	if js, ok := doc["json_schema"].(map[string]any); ok {
		if props, ok := js["properties"].(map[string]any); ok {
			if _, ok := props[name]; ok {
				delete(props, name)
				removed = true
				if req, ok := js["required"].([]any); ok {
					kept := req[:0]
					for _, r := range req {
						if r != name {
							kept = append(kept, r)
						}
					}
					js["required"] = kept
				}
			}
		}
	}
	if !removed {
		return info
	}
	raw, _ = json.Marshal(doc)
	out := &schema.ToolInfo{}
	_ = json.Unmarshal(raw, out)
	return out
}
