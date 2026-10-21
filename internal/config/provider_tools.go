package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

const (
	APIModeChatCompletions = "chat_completions"
	APIModeResponses       = "responses"
)

var ErrInvalidProviderConfig = errors.New("invalid provider configuration")

// Validate rejects protocols and tools whose execution or approval lifecycle
// this client cannot carry. An unknown type must not become a local tool call.
func (p Provider) Validate() error {
	switch p.APIMode {
	case "", APIModeChatCompletions, APIModeResponses:
	default:
		return fmt.Errorf("%w: provider %q api_mode must be chat_completions or responses", ErrInvalidProviderConfig, p.ID)
	}
	if len(p.BuiltinTools) == 0 {
		return nil
	}
	if p.APIMode != APIModeResponses {
		return fmt.Errorf("%w: provider %q builtin_tools require api_mode responses", ErrInvalidProviderConfig, p.ID)
	}
	seen := make(map[string]bool, len(p.BuiltinTools))
	for i, tool := range p.BuiltinTools {
		kind, ok := tool["type"].(string)
		if !ok || kind == "" {
			return fmt.Errorf("%w: provider %q builtin_tools[%d] needs a nonempty string type", ErrInvalidProviderConfig, p.ID, i)
		}
		switch kind {
		case "web_search", "web_search_preview", "file_search", "code_interpreter":
		default:
			return fmt.Errorf("%w: provider %q builtin_tools[%d] has an unsupported cloud tool type", ErrInvalidProviderConfig, p.ID, i)
		}
		if seen[kind] {
			return fmt.Errorf("%w: provider %q builtin_tools[%d] repeats a tool type", ErrInvalidProviderConfig, p.ID, i)
		}
		seen[kind] = true
	}
	if _, err := json.Marshal(p.BuiltinTools); err != nil {
		// Encoder errors can contain user data; identify the field without echoing
		// credentials or arbitrary option values into an HTTP error response.
		return fmt.Errorf("%w: provider %q builtin_tools must contain JSON-compatible options", ErrInvalidProviderConfig, p.ID)
	}
	return nil
}

func (c *Config) validateProviders() error {
	for _, p := range c.Models.Providers {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CloneBuiltinTools detaches nested JSON maps and lists before a mutable
// settings or model snapshot keeps them. Copying values directly preserves
// integer precision and the exact numeric representations of JSON options.
func CloneBuiltinTools(in []map[string]any) ([]map[string]any, error) {
	if _, err := json.Marshal(in); err != nil {
		return nil, fmt.Errorf("%w: builtin_tools must contain JSON-compatible options", ErrInvalidProviderConfig)
	}
	return cloneToolOption(reflect.ValueOf(in)).Interface().([]map[string]any), nil
}

func cloneToolOption(in reflect.Value) reflect.Value {
	switch in.Kind() {
	case reflect.Interface:
		if in.IsNil() {
			return in
		}
		return cloneToolOption(in.Elem())
	case reflect.Map:
		if in.IsNil() {
			return in
		}
		out := reflect.MakeMapWithSize(in.Type(), in.Len())
		for _, key := range in.MapKeys() {
			out.SetMapIndex(key, cloneToolOption(in.MapIndex(key)))
		}
		return out
	case reflect.Slice:
		if in.IsNil() {
			return in
		}
		out := reflect.MakeSlice(in.Type(), in.Len(), in.Len())
		for i := range in.Len() {
			out.Index(i).Set(cloneToolOption(in.Index(i)))
		}
		return out
	default:
		return in
	}
}
