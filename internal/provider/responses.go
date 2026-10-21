package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

// BuiltinToolEvent is a provider-owned execution receipt. It is never a local
// function call: only the upstream knows when its hosted tool ran.
type BuiltinToolEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Status  string          `json:"status"`
	Action  map[string]any  `json:"action,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

type builtinToolObserverKey struct{}

// WithBuiltinToolObserver observes the hosted tool lifecycle of one model call.
// The callback runs synchronously with Generate or the stream reader goroutine.
func WithBuiltinToolObserver(ctx context.Context, fn func(BuiltinToolEvent)) context.Context {
	return context.WithValue(ctx, builtinToolObserverKey{}, fn)
}

type responsesOptions struct{ reasoning string }

func responsesReasoningOption(effort string) model.Option {
	return model.WrapImplSpecificOptFn(func(o *responsesOptions) { o.reasoning = effort })
}

// responsesModel keeps the Eino boundary while preserving the Responses output
// items needed for a stateless follow-up (including encrypted reasoning).
type responsesModel struct {
	provider config.Provider
	client   *http.Client
	mu       sync.RWMutex
	tools    []*schema.ToolInfo
}

func newResponsesModel(p config.Provider) (*responsesModel, error) {
	tools, err := config.CloneBuiltinTools(p.BuiltinTools)
	if err != nil {
		return nil, err
	}
	p.BuiltinTools = tools
	return &responsesModel{provider: p, client: chatHTTPClient(p.Timeout(), p.FirstByteTimeout())}, nil
}

func (m *responsesModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if _, err := responsesTools(nil, tools); err != nil {
		return nil, err
	}
	return &responsesModel{provider: m.provider, client: m.client, tools: append([]*schema.ToolInfo{}, tools...)}, nil
}

func (m *responsesModel) BindTools(tools []*schema.ToolInfo) error {
	if _, err := responsesTools(nil, tools); err != nil {
		return err
	}
	m.mu.Lock()
	m.tools = append([]*schema.ToolInfo{}, tools...)
	m.mu.Unlock()
	return nil
}

func (m *responsesModel) request(ctx context.Context, in []*schema.Message, stream bool, opts ...model.Option) (*http.Response, error) {
	m.mu.RLock()
	bound := append([]*schema.ToolInfo(nil), m.tools...)
	m.mu.RUnlock()
	o := model.GetCommonOptions(&model.Options{Tools: bound}, opts...)
	specific := model.GetImplSpecificOptions(&responsesOptions{}, opts...)
	input, err := responsesInput(in)
	if err != nil {
		return nil, err
	}
	tools, err := responsesTools(m.provider.BuiltinTools, o.Tools)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"model": strings.TrimSpace(m.provider.Model), "input": input, "stream": stream, "store": false, "include": []string{"reasoning.encrypted_content"}}
	if o.Model != nil {
		body["model"] = *o.Model
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if o.MaxTokens != nil {
		body["max_output_tokens"] = *o.MaxTokens
	}
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		body["top_p"] = *o.TopP
	}
	if specific.reasoning != "" {
		body["reasoning"] = map[string]any{"effort": specific.reasoning, "summary": "auto"}
	}
	if o.ToolChoice != nil {
		choice := "auto"
		switch *o.ToolChoice {
		case schema.ToolChoiceForbidden:
			choice = "none"
		case schema.ToolChoiceForced:
			choice = "required"
		}
		body["tool_choice"] = choice
	}
	if len(o.AllowedToolNames) > 0 && (o.ToolChoice == nil || *o.ToolChoice != schema.ToolChoiceForbidden) {
		allowed := make([]map[string]any, 0, len(o.AllowedToolNames))
		for _, name := range o.AllowedToolNames {
			allowed = append(allowed, map[string]any{"type": "function", "name": name})
		}
		mode := "auto"
		if o.ToolChoice != nil && *o.ToolChoice == schema.ToolChoiceForced {
			mode = "required"
		}
		body["tool_choice"] = map[string]any{"type": "allowed_tools", "mode": mode, "tools": allowed}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("provider: responses request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(m.provider.BaseURL), "/")+"/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(m.provider.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, responsesHTTPError(resp.StatusCode, resp.Status, raw)
	}
	return resp, nil
}

func (m *responsesModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := m.request(ctx, in, false, opts...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out responsesResponse
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, &openai.RequestError{HTTPStatusCode: resp.StatusCode, HTTPStatus: resp.Status, Err: err}
	}
	observer := newResponsesObserver(ctx)
	for _, item := range out.Output {
		observer.item(item)
	}
	return out.message()
}

func responsesHTTPError(status int, statusText string, raw []byte) error {
	var out struct {
		Error *openai.APIError `json:"error"`
	}
	err := json.Unmarshal(raw, &out)
	if err == nil && out.Error != nil {
		out.Error.HTTPStatusCode = status
		out.Error.HTTPStatus = statusText
		return out.Error
	}
	if err == nil {
		err = fmt.Errorf("model response has no error details")
	}
	return &openai.RequestError{HTTPStatusCode: status, HTTPStatus: statusText, Err: err}
}

func responsesTools(builtin []map[string]any, local []*schema.ToolInfo) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(builtin)+len(local))
	out = append(out, builtin...)
	for _, tool := range local {
		if tool == nil || strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("provider: responses function requires a name")
		}
		params, err := tool.ToJSONSchema()
		if err != nil {
			return nil, fmt.Errorf("provider: responses function %q schema: %w", tool.Name, err)
		}
		var parameters any = params
		if params == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		// Responses otherwise normalizes a schema into strict mode, changing
		// optional parameters of existing local functions into required ones.
		out = append(out, map[string]any{"type": "function", "name": tool.Name, "description": tool.Desc, "parameters": parameters, "strict": false})
	}
	return out, nil
}

type responsesObserver struct {
	ctx  context.Context
	seen map[string]bool
}

func newResponsesObserver(ctx context.Context) *responsesObserver {
	return &responsesObserver{ctx: ctx, seen: map[string]bool{}}
}

func (o *responsesObserver) item(item responsesItem) {
	switch item.Type {
	case "web_search_call", "file_search_call", "code_interpreter_call":
	default:
		return
	}
	if item.ID == "" || item.Status == "" {
		return
	}
	key := item.ID + ":" + item.Status
	if o.seen[key] {
		return
	}
	o.seen[key] = true
	if fn, _ := o.ctx.Value(builtinToolObserverKey{}).(func(BuiltinToolEvent)); fn != nil {
		fn(BuiltinToolEvent{ID: item.ID, Type: item.Type, Status: item.Status, Action: item.Action, Details: append(json.RawMessage(nil), item.raw...)})
	}
}
