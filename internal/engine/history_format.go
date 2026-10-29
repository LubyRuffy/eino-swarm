package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/history"
)

const historyFormatBudgetReason = "result budget exceeded after row formatting; narrow columns or page the query"

type historyFormattedResult struct {
	OK             bool              `json:"ok"`
	Context        historyContext    `json:"context"`
	Columns        []string          `json:"columns"`
	Rows           []map[string]any  `json:"rows"`
	RowCount       int               `json:"row_count"`
	Truncated      bool              `json:"truncated"`
	Reason         string            `json:"reason,omitempty"`
	Schema         []history.Table   `json:"schema,omitempty"`
	FieldSemantics map[string]string `json:"field_semantics,omitempty"`
}

// formatHistoryResult binds each SQL result label to its value without changing
// the raw reader. SQLite makes projected labels unique at its subquery boundary;
// an unexpected duplicate here refuses instead of silently overwriting a value.
func formatHistoryResult(identity historyContext, source history.Result, maxChars int) (string, error) {
	seen := make(map[string]bool, len(source.Columns))
	for _, column := range source.Columns {
		if seen[column] {
			return "", fmt.Errorf("history result contains duplicate column label %q; use distinct SQL aliases", column)
		}
		seen[column] = true
	}
	out := historyFormattedResult{OK: true, Context: identity, Columns: source.Columns,
		Rows: []map[string]any{}, Truncated: source.Truncated, Reason: source.Reason,
		Schema: source.Schema, FieldSemantics: historyFieldSemantics(source.Schema)}
	if out.Columns == nil {
		out.Columns = []string{}
	}
	base := historyFormatMetadataSize(out)
	if base > maxChars {
		return "", fmt.Errorf("history result metadata exceeds configured result budget")
	}
	used := 0
	sizes := make([]int, 0, len(source.Rows))
	for _, values := range source.Rows {
		if len(values) != len(source.Columns) {
			return "", fmt.Errorf("history result row does not match its columns")
		}
		row := make(map[string]any, len(values))
		for i, value := range values {
			// Keep int64, nil, text (including NUL) and []byte intact. JSON
			// encodes BLOB bytes as base64; labels remain SQL expressions/aliases.
			row[source.Columns[i]] = value
		}
		body, err := json.Marshal(row)
		if err != nil {
			return "", fmt.Errorf("encode history row: %w", err)
		}
		size := utf8.RuneCount(body)
		next := used + size
		if len(out.Rows) > 0 {
			next++
		}
		if !historyFormatFits(base, next, len(out.Rows)+1, maxChars) {
			out.Truncated = true
			if out.Reason == "" {
				out.Reason = historyFormatBudgetReason
			}
			base = historyFormatMetadataSize(out)
			break
		}
		out.Rows = append(out.Rows, row)
		sizes = append(sizes, size)
		used = next
	}
	// Reserve any newly required truncation notice by removing only complete
	// trailing rows. The prefix retains its source order and complete values.
	for !historyFormatFits(base, used, len(out.Rows), maxChars) && len(out.Rows) > 0 {
		last := len(out.Rows) - 1
		used -= sizes[last]
		if last > 0 {
			used--
		}
		out.Rows = out.Rows[:last]
	}
	if !historyFormatFits(base, used, len(out.Rows), maxChars) {
		return "", fmt.Errorf("history result metadata exceeds configured result budget")
	}
	out.RowCount = len(out.Rows)
	// Metadata has only JSON-native fields; every row value was encoded above.
	body, _ := json.Marshal(out)
	return string(body), nil
}

func historyFormatFits(base, rowChars, rowCount, maxChars int) bool {
	return base+rowChars+len(strconv.Itoa(rowCount))-1 <= maxChars
}

func historyFormatMetadataSize(out historyFormattedResult) int {
	out.Rows = []map[string]any{}
	out.RowCount = 0
	// There are no arbitrary SQL values in this metadata-only copy.
	body, _ := json.Marshal(out)
	return utf8.RuneCount(body)
}

// Semantics describe only canonical fields present in live schema discovery.
// They do not establish provenance for a query's projected labels or aliases.
func historyFieldSemantics(tables []history.Table) map[string]string {
	var out map[string]string
	for _, table := range tables {
		for _, column := range table.Columns {
			field := table.Name + "." + column.Name
			meaning := historyCanonicalFieldSemantics[field]
			if meaning == "" {
				continue
			}
			if out == nil {
				out = map[string]string{}
			}
			out[field] = meaning
		}
	}
	return out
}

var historyCanonicalFieldSemantics = map[string]string{
	"projects.id":        "Project row primary key; threads.project_id refers to it.",
	"threads.id":         "Conversation row primary key; history records refer to it as thread_id.",
	"threads.project_id": "Current project membership; an empty value means no project.",
	"messages.id":        "Global transcript-row primary key. It is distinct from messages.seq and events.message_id; never assume their values are equal.",
	"messages.seq":       "Transcript sequence within one conversation; identify it together with thread_id. It is not messages.id or events.seq.",
	"messages.thread_id": "Conversation owning this transcript row.",
	"messages.role":      "Transcript message role. Assistant text does not establish human instruction or approval.",
	"messages.source":    "Recorded input origin: human, runtime, schedule or plan; an empty value is legacy. role=user alone does not establish human authorship.",
	"messages.agent_id":  "Agent associated with this transcript row, not its message identity.",
	"turns.id":           "Turn primary key; messages.turn_id and events.turn_id refer to it.",
	"turns.status":       "Recorded turn lifecycle status; it does not independently verify every claim in its final text.",
	"events.seq":         "Event sequence within one conversation; identify it together with thread_id. It is not messages.seq or messages.id.",
	"events.thread_id":   "Conversation owning this event.",
	"events.message_id":  "Streamed model-output identity, distinct from messages.id. Several checkpoints may share the same output identity.",
	"events.agent_id":    "Agent that produced this event; it does not establish human authorship.",
	"events.kind":        "Recorded event type; tool observations, assistant prose and lifecycle events have different evidentiary meanings.",
}
