package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// graphDigestPrefix marks the per-request digest so the next request replaces
// it instead of stacking another copy.
const graphDigestPrefix = "<graph_digest>\n"

const digestNameRunes = 80

// graphDigestHandler puts the graph's current state in front of every model
// request of a turn, for the manager and its workers alike.
type graphDigestHandler struct {
	adk.BaseChatModelAgentMiddleware
	engine   *Engine
	owner    string
	threadID string
	turnID   string
	readOnly bool
}

// withoutGraphDigest returns the transcript without per-request digests. It
// copies only when there is one to remove.
func withoutGraphDigest(transcript []adk.Message) []adk.Message {
	isDigest := func(m adk.Message) bool {
		return m != nil && m.Role == schema.System && strings.HasPrefix(m.Content, graphDigestPrefix)
	}
	found := false
	for _, m := range transcript {
		if isDigest(m) {
			found = true
			break
		}
	}
	if !found {
		return transcript
	}
	out := make([]adk.Message, 0, len(transcript))
	for _, m := range transcript {
		if !isDigest(m) {
			out = append(out, m)
		}
	}
	return out
}

func (e *Engine) graphDigestHandler(turn *store.Turn) *graphDigestHandler {
	return &graphDigestHandler{
		engine: e, owner: store.GraphOwnerID(turn.ProjectID, turn.ThreadID),
		threadID: turn.ThreadID, turnID: turn.ID, readOnly: turn.GraphReadOnly,
	}
}

func (h *graphDigestHandler) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil || ctx.Err() != nil {
		return ctx, state, nil
	}
	turnID := h.turnID
	if h.readOnly || !h.engine.cfg.SwarmSnapshot().GraphAutoRecord {
		turnID = ""
	}
	digest, err := h.engine.store.ReadGraphDigest(ctx, h.owner, h.threadID, turnID)
	if err != nil {
		// A digest is a convenience; a storage hiccup must not fail the turn.
		h.engine.log.Warn("graph digest unavailable", "turn", h.turnID, "err", err)
		return ctx, state, nil
	}
	text := ""
	if !digest.Empty() {
		text = renderGraphDigest(digest)
	}
	next := *state
	next.Messages = make([]adk.Message, 0, len(state.Messages)+1)
	for _, msg := range state.Messages {
		if msg != nil && msg.Role == schema.System && strings.HasPrefix(msg.Content, graphDigestPrefix) {
			continue
		}
		next.Messages = append(next.Messages, msg)
	}
	if text != "" {
		index := 0
		for index < len(next.Messages) && next.Messages[index] != nil && next.Messages[index].Role == schema.System {
			index++
		}
		next.Messages = append(next.Messages, nil)
		copy(next.Messages[index+1:], next.Messages[index:])
		next.Messages[index] = schema.SystemMessage(text)
	}
	return ctx, &next, nil
}

// renderGraphDigest lists identifiers and versions, never attribute values:
// the digest is small on purpose, and credentials must not reach it.
func renderGraphDigest(d store.GraphDigest) string {
	var b strings.Builder
	b.WriteString(graphDigestPrefix)
	fmt.Fprintf(&b, "revision %d, %d subjects", d.Revision, d.Subjects)
	kinds := make([]string, 0, len(d.ByKind))
	for k := range d.ByKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for i, k := range kinds {
		sep := " ("
		if i > 0 {
			sep = ", "
		}
		fmt.Fprintf(&b, "%s%s %d", sep, k, d.ByKind[k])
	}
	if len(kinds) > 0 {
		b.WriteString(")")
	}
	if d.Retracted > 0 {
		fmt.Fprintf(&b, ", %d retracted", d.Retracted)
	}
	b.WriteString("\n")
	if len(d.CurrentByKind) > 0 {
		keys := make([]string, 0, len(d.CurrentByKind))
		for k := range d.CurrentByKind {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("current")
		for i, k := range keys {
			sep := " "
			if i > 0 {
				sep = ", "
			}
			fmt.Fprintf(&b, "%s%s %d", sep, k, d.CurrentByKind[k])
		}
		b.WriteString("\n")
	}
	if d.OpenTodoTotal > 0 {
		fmt.Fprintf(&b, "open to-dos (%d", d.OpenTodoTotal)
		if d.OpenTodoTotal > len(d.Todos) {
			fmt.Fprintf(&b, ", first %d", len(d.Todos))
		}
		b.WriteString("):\n")
		for _, t := range d.Todos {
			fmt.Fprintf(&b, "- %s %s v%d: %s", t.ID, t.Status, t.Version, clipRunes(t.Title, digestNameRunes))
			if t.About != "" {
				fmt.Fprintf(&b, " [about %s]", t.About)
			}
			b.WriteString("\n")
		}
	}
	if len(d.Recent) > 0 {
		b.WriteString("latest subjects:\n")
		for _, s := range d.Recent {
			fmt.Fprintf(&b, "- %s %s %s v%d: %s\n", s.ID, s.Kind, s.Status, s.Version, clipRunes(s.Name, digestNameRunes))
		}
	}
	if d.Unrecorded > 0 {
		fmt.Fprintf(&b, "%d tool results since the last graph write are not subjects; fold them into the next update_graph call instead of writing once per result.\n", d.Unrecorded)
	}
	return strings.TrimRight(b.String(), "\n")
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
