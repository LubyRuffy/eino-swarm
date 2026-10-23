package engine

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

// The caller selects the current candidate by its immutable answer marker.
// Content alone is insufficient: Responses replays its provider-owned raw
// output first. Copy both representations without mutating shared history.
func withoutScheduleAnswerProse(m *schema.Message) *schema.Message {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Content = ""
	cp.MultiContent = nil
	cp.AssistantGenMultiContent = nil
	cp.Extra = make(map[string]any, len(m.Extra))
	for key, value := range m.Extra {
		cp.Extra[key] = value
	}
	if raw, ok := cp.Extra["responses_output"]; ok {
		filtered, valid := withoutResponsesAnswerText(raw)
		if valid && len(filtered) > 0 {
			cp.Extra["responses_output"] = filtered
		} else {
			// Malformed provider metadata cannot safely retain a rejected answer.
			// Keep the unrelated metadata and the authoritative local tool calls.
			delete(cp.Extra, "responses_output")
		}
	}
	return &cp
}

func withoutResponsesAnswerText(value any) ([]json.RawMessage, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil || item == nil {
			return nil, false
		}
		var kind string
		if json.Unmarshal(item["type"], &kind) != nil {
			return nil, false
		}
		if kind != "message" {
			out = append(out, rawItem)
			continue
		}
		var parts []json.RawMessage
		if json.Unmarshal(item["content"], &parts) != nil {
			return nil, false
		}
		kept := make([]json.RawMessage, 0, len(parts))
		for _, rawPart := range parts {
			var part struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(rawPart, &part) != nil {
				return nil, false
			}
			if part.Type != "output_text" && part.Type != "refusal" {
				kept = append(kept, rawPart)
			}
		}
		if len(kept) == 0 {
			continue
		}
		if len(kept) == len(parts) {
			out = append(out, rawItem)
			continue
		}
		item["content"], _ = json.Marshal(kept)
		filtered, _ := json.Marshal(item)
		out = append(out, filtered)
	}
	return out, true
}
