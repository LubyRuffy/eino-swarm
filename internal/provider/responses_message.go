package provider

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

const responsesOutputKey = "responses_output"

// ResponsesReplayJSON exposes the actual hidden provider input to the context
// budget. It excludes removed function calls and avoids counting the rendered
// answer and its raw counterpart twice.
func ResponsesReplayJSON(msg *schema.Message) ([]byte, error) {
	if msg == nil || msg.Role != schema.Assistant || msg.Extra[responsesOutputKey] == nil {
		return nil, nil
	}
	items, err := responsesReplay(msg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(items)
}

func responsesInput(messages []*schema.Message) ([]any, error) {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.Assistant && msg.Extra[responsesOutputKey] != nil {
			items, err := responsesReplay(msg)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
			continue
		}
		if msg.Role == schema.Tool {
			out = append(out, map[string]any{"type": "function_call_output", "call_id": msg.ToolCallID, "output": msg.Content})
			continue
		}
		parts, err := responsesContent(msg)
		if err != nil {
			return nil, err
		}
		if len(parts) > 0 {
			out = append(out, map[string]any{"role": msg.Role, "content": parts})
		}
		for _, call := range msg.ToolCalls {
			out = append(out, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
		}
	}
	return out, nil
}

// The swarm may remove refused calls or repair arguments before execution.
// The current ToolCalls are authoritative; raw items retain only provider
// state, never resurrect a call whose local result will not exist.
func responsesReplay(msg *schema.Message) ([]any, error) {
	raw, err := json.Marshal(msg.Extra[responsesOutputKey])
	if err != nil {
		return nil, fmt.Errorf("provider: responses replay: %w", err)
	}
	var items []json.RawMessage
	if err = json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("provider: responses replay: %w", err)
	}
	current := map[string]schema.ToolCall{}
	for _, call := range msg.ToolCalls {
		current[call.ID] = call
	}
	seen := map[string]bool{}
	out := make([]any, 0, len(items))
	for _, rawItem := range items {
		var item map[string]any
		if err = json.Unmarshal(rawItem, &item); err != nil {
			return nil, fmt.Errorf("provider: responses replay item: %w", err)
		}
		if item["type"] != "function_call" {
			out = append(out, rawItem)
			continue
		}
		id, _ := item["call_id"].(string)
		call, ok := current[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		item["name"] = call.Function.Name
		item["arguments"] = call.Function.Arguments
		out = append(out, item)
	}
	for _, call := range msg.ToolCalls {
		if !seen[call.ID] {
			out = append(out, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
		}
	}
	return out, nil
}

func responsesContent(msg *schema.Message) ([]map[string]any, error) {
	var out []map[string]any
	appendText := func(text string) {
		if text != "" {
			out = append(out, map[string]any{"type": "input_text", "text": text})
		}
	}
	if len(msg.UserInputMultiContent) > 0 {
		parts := msg.UserInputMultiContent
		if msg.Content != "" && !inputHasText(parts, msg.Content) {
			appendText(msg.Content)
		}
		for _, part := range parts {
			switch part.Type {
			case schema.ChatMessagePartTypeText:
				appendText(part.Text)
			case schema.ChatMessagePartTypeImageURL:
				if part.Image == nil {
					return nil, fmt.Errorf("provider: responses image is missing its data")
				}
				image := part.Image
				source := ""
				if image.URL != nil {
					source = *image.URL
				} else if image.Base64Data != nil && image.MIMEType != "" {
					source = "data:" + image.MIMEType + ";base64," + *image.Base64Data
				}
				if source == "" {
					return nil, fmt.Errorf("provider: responses image requires a URL or base64 data with MIME type")
				}
				p := map[string]any{"type": "input_image", "image_url": source}
				if image.Detail != "" {
					p["detail"] = image.Detail
				}
				out = append(out, p)
			default:
				return nil, fmt.Errorf("provider: responses input part %q is unsupported", part.Type)
			}
		}
		return out, nil
	}
	appendText(msg.Content)
	for _, part := range msg.MultiContent {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if part.Text != msg.Content {
				appendText(part.Text)
			}
		case schema.ChatMessagePartTypeImageURL:
			if part.ImageURL == nil {
				return nil, fmt.Errorf("provider: responses image is missing its URL")
			}
			p := map[string]any{"type": "input_image", "image_url": part.ImageURL.URL}
			if part.ImageURL.Detail != "" {
				p["detail"] = part.ImageURL.Detail
			}
			out = append(out, p)
		default:
			return nil, fmt.Errorf("provider: responses input part %q is unsupported", part.Type)
		}
	}
	for _, part := range msg.AssistantGenMultiContent {
		if part.Type != schema.ChatMessagePartTypeText {
			return nil, fmt.Errorf("provider: responses assistant part %q is unsupported", part.Type)
		}
		if part.Text != msg.Content {
			appendText(part.Text)
		}
	}
	return out, nil
}

type responsesItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Content   []responsesText `json:"content"`
	Summary   []responsesText `json:"summary"`
	Action    map[string]any  `json:"action"`
	raw       json.RawMessage
}

func (i *responsesItem) UnmarshalJSON(raw []byte) error {
	type plain responsesItem
	var parsed plain
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	*i = responsesItem(parsed)
	i.raw = append(json.RawMessage(nil), raw...)
	return nil
}

type responsesText struct {
	Type        string              `json:"type"`
	Text        string              `json:"text"`
	Refusal     string              `json:"refusal"`
	Annotations []responsesCitation `json:"annotations"`
}

type responsesCitation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type responsesResponse struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	Output            []responsesItem  `json:"output"`
	Error             *openai.APIError `json:"error"`
	IncompleteDetails struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		InputDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func (r *responsesResponse) failure() error {
	if r.Error != nil {
		return r.Error
	}
	if r.Status == "completed" {
		return nil
	}
	reason := r.IncompleteDetails.Reason
	if reason == "" {
		reason = r.Status
	}
	if reason == "" {
		reason = "missing terminal status"
	}
	return &openai.APIError{Type: "response_error", Code: "response_" + r.Status, Message: "responses did not complete: " + reason}
}

func (r *responsesResponse) message() (*schema.Message, error) {
	if err := r.failure(); err != nil {
		return nil, err
	}
	out := &schema.Message{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}
	raw := make([]json.RawMessage, 0, len(r.Output))
	var citations []responsesCitation
	for _, item := range r.Output {
		raw = append(raw, item.raw)
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					out.Content += part.Text
					citations = append(citations, part.Annotations...)
				} else if part.Type == "refusal" {
					out.Content += part.Refusal
				}
			}
		case "reasoning":
			for _, part := range item.Summary {
				out.ReasoningContent += part.Text
			}
		case "function_call":
			index := len(out.ToolCalls)
			out.ToolCalls = append(out.ToolCalls, schema.ToolCall{Index: &index, ID: item.CallID, Type: "function", Function: schema.FunctionCall{Name: item.Name, Arguments: item.Arguments}})
		}
	}
	if len(out.ToolCalls) > 0 {
		out.ResponseMeta.FinishReason = "tool_calls"
	}
	out.Content += responsesCitations(citations)
	out.Extra = map[string]any{responsesOutputKey: raw}
	if u := r.Usage; u != nil {
		out.ResponseMeta.Usage = &schema.TokenUsage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens, PromptTokenDetails: schema.PromptTokenDetails{CachedTokens: u.InputDetails.CachedTokens}, CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: u.OutputDetails.ReasoningTokens}}
	}
	return out, nil
}

// Keep the original answer byte-for-byte. Offsets vary across compatible
// providers; appending visible links avoids guessing at Unicode indices or
// rewriting already streamed Markdown.
func responsesCitations(citations []responsesCitation) string {
	var out []string
	seen := map[string]bool{}
	for _, c := range citations {
		if c.Type != "url_citation" || seen[c.URL] {
			continue
		}
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		seen[c.URL] = true
		title := c.Title
		if title == "" {
			title = c.URL
		}
		title = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(title)
		link := strings.NewReplacer("(", "%28", ")", "%29", " ", "%20", "\n", "%0A", "\r", "%0D").Replace(c.URL)
		out = append(out, "["+title+"]("+link+")")
	}
	if len(out) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(out, " · ")
}
