package graph

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
)

const omittedToolPayload = `{"omitted":"unreadable graph payload"}`

// RedactToolText removes credential attribute values from a graph tool call or
// result, keeping the attribute name. The model still receives the value from
// the tool return; this copy is what gets stored, shown and backed up.
func RedactToolText(text string) string {
	name, body, wrapped := splitCall(text)
	if wrapped && !graphToolName(name) {
		return text
	}
	next, ok := redactJSON(body)
	if !ok {
		next = omittedToolPayload
	}
	if wrapped {
		return name + "(" + next + ")"
	}
	return next
}

func graphToolName(name string) bool {
	switch name {
	case "update_graph", "read_graph", "read_work", "record_work":
		return true
	}
	return false
}

func splitCall(text string) (name, body string, wrapped bool) {
	open := strings.IndexByte(text, '(')
	if open <= 0 {
		return "", text, false
	}
	name = text[:open]
	if strings.Contains(name, " ") || strings.Contains(name, "{") {
		return "", text, false
	}
	return name, strings.TrimSuffix(text[open+1:], ")"), true
}

func redactJSON(text string) (string, bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return text, false
	}
	changed := redactValue(value)
	if !changed {
		return text, true
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return text, false
	}
	return strings.TrimRight(buf.String(), "\n"), true
}

func redactValue(value any) bool {
	changed := false
	switch node := value.(type) {
	case map[string]any:
		if role, _ := node["role"].(string); role == "tool" {
			name, _ := node["tool_name"].(string)
			extra, _ := node["extra"].(map[string]any)
			marked, _ := extra["zwai_work_record_payload"].(bool)
			if content, ok := node["content"].(string); ok && (graphToolName(name) || marked) {
				next := RedactToolText(content)
				node["content"], changed = next, next != content
			}
		}
		if name, _ := node["name"].(string); graphToolName(name) {
			if args, ok := node["arguments"].(string); ok {
				next := RedactToolText(args)
				node["arguments"], changed = next, changed || next != args
			}
		}
		if fn, ok := node["function"].(map[string]any); ok {
			name, _ := fn["name"].(string)
			if args, ok := fn["arguments"].(string); ok && graphToolName(name) {
				if next := RedactToolText(args); next != args {
					fn["arguments"] = next
					changed = true
				}
			}
		}
		kind, kindSet := node["kind"].(string)
		_, hasID := node["id"].(string)
		if kindSet || hasID {
			if attrs, ok := node["attrs"].(map[string]any); ok {
				redacted, secrets := MustDefault().PublicAttrs(kind, attrs)
				if len(secrets) > 0 {
					node["attrs"] = redacted
					node["credentials"] = secretNames(node["credentials"], secrets)
					changed = true
				}
			}
		}
		for key, child := range node {
			if key == "function" || key == "attrs" || key == "credentials" {
				continue
			}
			if redactValue(child) {
				changed = true
			}
		}
	case []any:
		for _, child := range node {
			if redactValue(child) {
				changed = true
			}
		}
	}
	return changed
}

// PublicAttrs also removes credentials from raw address input. A by-ID edit
// can omit kind, so diagnostic copies use the catalog's sensitive fields in
// that case, without requiring the storage lookup to have succeeded.
func (c *Catalog) PublicAttrs(kind string, attrs map[string]any) (map[string]any, []string) {
	kinds := c.Kinds
	if k := c.Kind(kind); k != nil {
		kinds = []Kind{*k}
	}
	redacted := make(map[string]any, len(attrs))
	for name, value := range attrs {
		redacted[name] = value
	}
	var names []string
	for _, k := range kinds {
		for _, attr := range k.Attrs {
			value, present := redacted[attr.Name]
			if !present {
				continue
			}
			if attr.Secret {
				delete(redacted, attr.Name)
				names = append(names, attr.Name)
			}
			if address, ok := value.(string); ok && attr.StripUserinfoTo != "" {
				if cleaned, found := publicAddress(address); found {
					redacted[attr.Name] = cleaned
					names = append(names, attr.StripUserinfoTo)
				}
			}
		}
	}
	slices.Sort(names)
	return redacted, slices.Compact(names)
}

func publicAddress(address string) (string, bool) {
	if cleaned, credential := stripUserinfo(address); credential != "" {
		return cleaned, true
	}
	// A rejected escape in the authority can prevent URL parsing before
	// userinfo is reached. It still cannot be part of the public address.
	scheme, rest, ok := strings.Cut(address, "://")
	if !ok {
		return address, false
	}
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		return scheme + "://" + rest[at+1:], true
	}
	return address, false
}

func secretNames(current any, secrets []string) []any {
	have := map[string]bool{}
	var out []any
	if list, ok := current.([]any); ok {
		for _, item := range list {
			name, ok := item.(string)
			if ok && !have[name] {
				have[name] = true
				out = append(out, name)
			}
		}
	}
	for _, name := range secrets {
		if !have[name] {
			out = append(out, name)
		}
	}
	return out
}
