package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

// References live only in this outgoing request. The Registry and ADK retain
// their original complete history, so compaction/removal cannot create a stale
// reference carried from a previous request.
type resultReference struct {
	ToolCallID string `json:"tool_call_id"`
	AgentID    string `json:"agent_id"`
	Generation int    `json:"generation"`
	SHA256     string `json:"result_error_sha256"`
}

type replacement struct {
	MessageIndex int `json:"message_index"`
	resultReference
	ResultChars int `json:"result_chars"`
}

type resultIdentity struct {
	id, role, hash string
	generation     int
}

func resultHash(result, errText string) string {
	// JSON string encoding separates the fields unambiguously and preserves
	// negation, conditions, whitespace and every code point of both strings.
	encoded, _ := json.Marshal([2]string{result, errText})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func completedIdentity(entry map[string]json.RawMessage) (resultIdentity, string, bool) {
	var id, role, status, result, errText string
	var generation int
	if json.Unmarshal(entry["agent_id"], &id) != nil || id == "" ||
		json.Unmarshal(entry["generation"], &generation) != nil || generation <= 0 ||
		json.Unmarshal(entry["status"], &status) != nil || status != "done" ||
		json.Unmarshal(entry["result"], &result) != nil || result == "" {
		return resultIdentity{}, "", false
	}
	if raw, ok := entry["error"]; ok && (bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &errText) != nil || errText != "") {
		return resultIdentity{}, "", false
	}
	if raw, ok := entry["role"]; ok && json.Unmarshal(raw, &role) != nil {
		return resultIdentity{}, "", false
	}
	if _, exists := entry["result_ref"]; exists {
		return resultIdentity{}, "", false
	}
	return resultIdentity{id: id, role: role, generation: generation, hash: resultHash(result, errText)}, result, true
}

func uniqueValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return true
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || keys[name] {
				return false
			}
			keys[name] = true
			if !uniqueValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !uniqueValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func uniqueJSON(text string) bool {
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.UseNumber()
	if !uniqueValue(decoder) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func duplicateWorkers(entries []map[string]json.RawMessage) bool {
	type identity struct {
		id         string
		generation int
	}
	seen := map[identity]bool{}
	for _, entry := range entries {
		var id string
		var generation int
		if json.Unmarshal(entry["agent_id"], &id) != nil || id == "" || json.Unmarshal(entry["generation"], &generation) != nil || generation <= 0 {
			continue
		}
		key := identity{id: id, generation: generation}
		if seen[key] {
			return true
		}
		seen[key] = true
	}
	return false
}

func referenceDuplicates(input []*schema.Message) ([]*schema.Message, []replacement) {
	output := append([]*schema.Message(nil), input...)
	seen := map[resultIdentity]resultReference{}
	type binding struct {
		name                  string
		index, count, results int
	}
	bindings := map[string]binding{}
	for index, message := range input {
		if message == nil {
			continue
		}
		if message.Role == schema.Assistant {
			for _, call := range message.ToolCalls {
				b := bindings[call.ID]
				b.name, b.index, b.count = call.Function.Name, index, b.count+1
				bindings[call.ID] = b
			}
		}
		if message.Role == schema.Tool {
			b := bindings[message.ToolCallID]
			b.results++
			bindings[message.ToolCallID] = b
		}
	}
	var changes []replacement
	for index, message := range input {
		if message == nil {
			continue
		}
		b := bindings[message.ToolCallID]
		if message.Role != schema.Tool || message.ToolCallID == "" || b.name != "wait_agents" || b.count != 1 || b.results != 1 || b.index >= index ||
			(message.ToolName != "" && message.ToolName != "wait_agents") {
			continue
		}
		if !uniqueJSON(message.Content) {
			continue
		}
		var report map[string]json.RawMessage
		if json.Unmarshal([]byte(message.Content), &report) != nil {
			continue
		}
		var entries []map[string]json.RawMessage
		if json.Unmarshal(report["agents"], &entries) != nil {
			continue
		}
		if duplicateWorkers(entries) {
			continue
		}
		changed := false
		for _, entry := range entries {
			identity, result, ok := completedIdentity(entry)
			if !ok {
				continue
			}
			if previous, exists := seen[identity]; exists && previous.ToolCallID != message.ToolCallID {
				encoded, _ := json.Marshal(previous)
				delete(entry, "result")
				entry["result_ref"] = encoded
				changes = append(changes, replacement{MessageIndex: index, resultReference: previous, ResultChars: utf8.RuneCountInString(result)})
				changed = true
			} else if !exists {
				seen[identity] = resultReference{ToolCallID: message.ToolCallID, AgentID: identity.id, Generation: identity.generation, SHA256: identity.hash}
			}
		}
		if changed {
			report["agents"], _ = json.Marshal(entries)
			encoded, _ := json.Marshal(report)
			clone := *message
			clone.Content = string(encoded)
			output[index] = &clone
		}
	}
	return output, changes
}
