package provider

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// WithoutBuiltinTools gives one model instance no configured provider-executed
// tools. The shared client, local tool bindings, admission and telemetry remain
// intact; another agent using the original model keeps its configured tools.
func WithoutBuiltinTools(inner model.BaseChatModel) model.BaseChatModel {
	switch m := inner.(type) {
	case *recordingModel:
		copy := *m
		copy.inner = WithoutBuiltinTools(m.inner)
		return &copy
	case *responsesModel:
		m.mu.RLock()
		bound := append([]*schema.ToolInfo(nil), m.tools...)
		m.mu.RUnlock()
		configured := m.provider
		configured.BuiltinTools = nil
		return &responsesModel{provider: configured, client: m.client, tools: bound}
	default:
		return inner
	}
}
