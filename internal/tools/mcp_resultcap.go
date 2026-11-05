package tools

import (
	"encoding/json"
	"unicode/utf8"
)

// An MCP error must remain an error after clipping. Keep a valid envelope and
// retain the complete result when it fits, including its typed content blocks.
func capMCPResult(out string, limit int) string {
	if utf8.RuneCountInString(out) <= limit {
		return out
	}
	var result struct {
		IsError bool            `json:"isError"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(out), &result) != nil {
		return capToolResult(out, limit)
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		// Adapter failures have a bounded code/server identity and no remote
		// body. Keep their exact identity even below the envelope's soft floor.
		raw, _ := json.Marshal(map[string]any{"error": result.Error, "isError": true, "truncated": true})
		return string(raw)
	}
	encode := func(keep int) []byte {
		raw, _ := json.Marshal(map[string]any{"isError": result.IsError, "truncated": true, "content": []map[string]string{{"type": "text", "text": clipExecText([]rune(out), keep)}}})
		return raw
	}
	best := encode(0)
	limit = max(limit, utf8.RuneCount(best))
	for low, high := 0, utf8.RuneCountInString(out); low <= high; {
		keep := low + (high-low)/2
		raw := encode(keep)
		if utf8.RuneCount(raw) <= limit {
			best = raw
			low = keep + 1
		} else {
			high = keep - 1
		}
	}
	return string(best)
}
