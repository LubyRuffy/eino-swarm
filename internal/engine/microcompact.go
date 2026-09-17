package engine

import (
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/schema"
)

const (
	microcompactKeepResults = 3
	microcompactPlaceholder = "[old tool result cleared]"
)

// microCompact clears older replayable tool results in a copy of the manager
// state. The event log is untouched: this is only what the next Generate
// sees. Lifecycle tools and memory writes stay — those are not a file body
// the agent can re-read.
func microCompact(msgs []*schema.Message, keep int) []*schema.Message {
	if keep < 0 {
		keep = 0
	}
	if len(msgs) == 0 {
		return msgs
	}
	names := toolResultNames(msgs)
	var replayable []int
	for i, m := range msgs {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		if !tools.ReplayableResult(names[i]) {
			continue
		}
		if strings.TrimSpace(m.Content) == "" || strings.TrimSpace(m.Content) == microcompactPlaceholder {
			continue
		}
		replayable = append(replayable, i)
	}
	if keep >= len(replayable) {
		return msgs
	}
	drop := map[int]struct{}{}
	for _, i := range replayable[:len(replayable)-keep] {
		drop[i] = struct{}{}
	}
	out := make([]*schema.Message, len(msgs))
	changed := false
	for i, m := range msgs {
		if m == nil {
			continue
		}
		if _, ok := drop[i]; !ok {
			out[i] = m
			continue
		}
		cp := *m
		cp.Content = microcompactPlaceholder
		out[i] = &cp
		changed = true
	}
	if !changed {
		return msgs
	}
	return out
}

func toolResultNames(msgs []*schema.Message) []string {
	names := make([]string, len(msgs))
	idName := map[string]string{}
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				idName[tc.ID] = tc.Function.Name
			}
		}
	}
	for i, m := range msgs {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		names[i] = idName[m.ToolCallID]
	}
	return names
}

func microCompactChanged(before, after []*schema.Message) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		if before[i] == nil || after[i] == nil {
			return true
		}
		if before[i].Content != after[i].Content {
			return true
		}
	}
	return false
}
