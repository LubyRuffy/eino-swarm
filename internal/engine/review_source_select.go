package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const reviewSourceHeader = "Conversation to review:\n\n"
const reviewSourceOmitted = "[Some source events or text were omitted to fit the review budget.]\n\n"
const reviewSourceMinFraming = len("[derived] final answer: \n\n")

type reviewSourceItem struct {
	event    store.Event
	role     string
	text     string
	sourceAt int
	prefix   string
	size     int
	group    int
	selected bool
	clipped  bool
}
type reviewSourceGroup struct {
	items    []int
	priority int
}
type reviewToolKey struct{ agent, call string }

type reviewSourceInput struct {
	Transcript string
	Events     map[int64]store.Event
}

func selectReviewInputWithHistory(events, history []store.Event, sessionMemory, final string) reviewSourceInput {
	return selectReviewSources(events, sessionMemory, final, workerLifecycleFacts(history, workerFactTurns(events)))
}

func renderSelectedReviewSources(events []store.Event, sessionMemory, final string) string {
	return reviewSourceWithHistory(events, events, sessionMemory, final)
}

// The text and its authority come from the same selection. Source-looking
// text inside an event can never grant visibility to a different event.
func selectReviewInput(events []store.Event, sessionMemory, final string) reviewSourceInput {
	return selectReviewSources(events, sessionMemory, final, "")
}

func selectReviewSources(events []store.Event, sessionMemory, final, facts string) reviewSourceInput {
	items, groups := collectReviewSources(events, sessionMemory, final)
	if len(items) == 0 {
		return reviewSourceInput{Transcript: facts}
	}
	// Lifecycle context has its own bounded schema and leads all claims. It
	// spends the same total budget but never enters the writable source map.
	budget := reviewMaxChars - utf8.RuneCountInString(facts) - utf8.RuneCountInString(reviewSourceHeader+reviewSourceOmitted)
	seen := make([]bool, len(groups))
	selectedCount := 0
	for priority := 0; priority < 3 && budget >= reviewSourceMinFraming; priority++ {
		// A completed interleaved tool group is as recent as its last result, not
		// its call. Visiting source items backward preserves that distinction.
		for i := len(items) - 1; i >= 0 && budget >= reviewSourceMinFraming; i-- {
			group := items[i].group
			if seen[group] || groups[group].priority != priority {
				continue
			}
			seen[group] = true
			if used, ok := selectReviewSourceGroup(items, groups[group], budget); ok {
				budget -= used
				selectedCount += len(groups[group].items)
			}
		}
	}
	var b strings.Builder
	b.WriteString(facts)
	b.WriteString(reviewSourceHeader)
	input := reviewSourceInput{Events: make(map[int64]store.Event, selectedCount)}
	omitted := false
	for _, item := range items {
		if !item.selected {
			omitted = true
			continue
		}
		b.WriteString(item.prefix)
		b.WriteString(item.text)
		b.WriteString("\n\n")
		if item.event.Seq > 0 {
			visible := item.text
			if item.clipped {
				visible = strings.TrimSuffix(visible, " …")
				if visible == "…" {
					visible = ""
				}
			}
			// Calls and failures have display-only prefixes. Keep only the
			// original source bytes that survived both clipping passes.
			raw := strings.TrimSpace(item.event.Text)
			if raw != "" && item.sourceAt >= 0 && item.sourceAt < len(visible) {
				ev := item.event
				ev.Text = raw[:min(len(raw), len(visible)-item.sourceAt)]
				input.Events[ev.Seq] = ev
			}
		}
		omitted = omitted || item.clipped
	}
	if omitted {
		b.WriteString(reviewSourceOmitted)
	}
	input.Transcript = b.String()
	return input
}

func collectReviewSources(events []store.Event, sessionMemory, final string) ([]reviewSourceItem, []reviewSourceGroup) {
	items := make([]reviewSourceItem, 0, len(events)+2)
	groups := make([]reviewSourceGroup, 0, len(events)+2)
	appendItem := func(ev store.Event, role, text string, priority, group int) int {
		text = strings.TrimSpace(text)
		if text == "" {
			return -1
		}
		if group < 0 {
			group = len(groups)
			groups = append(groups, reviewSourceGroup{priority: priority})
		}
		sourceAt := -1
		if raw := strings.TrimSpace(ev.Text); raw != "" {
			switch {
			case ev.Kind == swarm.NotifyToolCall.String():
				sourceAt = len("[called ")
			case ev.Kind == swarm.NotifyToolResult.String() && ev.Err != "":
				sourceAt = len(text) - len(raw)
			case text == raw:
				sourceAt = 0
			}
		}
		index := len(items)
		items = append(items, reviewSourceItem{event: ev, role: role, text: text, group: group, sourceAt: sourceAt})
		groups[group].items = append(groups[group].items, index)
		return index
	}
	appendItem(store.Event{}, "session briefing (model-written claims)", withoutWorkerEvidence(sessionMemory), 2, -1)
	retracted := retractedSteerSeqs(events)
	revised := revisedSteerText(events)
	calls := make(map[reviewToolKey]int)
	for _, ev := range events {
		if ev.Kind == KindSteer {
			if _, ok := retracted[ev.Seq]; ok {
				continue
			}
			if text, ok := revised[ev.Seq]; ok {
				ev.Text = text
			}
		}
		role, text, ok := reviewEventLine(ev)
		if !ok {
			continue
		}
		priority, group := 2, -1
		key := reviewToolKey{ev.AgentID, ev.ToolCallID}
		switch ev.Kind {
		case KindUser, KindSteer:
			priority = 0
		case swarm.NotifyToolCall.String(), swarm.NotifyToolResult.String():
			priority = 1
			if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID != "" {
				if existing, ok := calls[key]; ok {
					group = existing
				}
			}
		}
		index := appendItem(ev, role, text, priority, group)
		if ev.Kind == swarm.NotifyToolCall.String() && ev.ToolCallID != "" {
			calls[key] = items[index].group
		}
	}
	appendItem(store.Event{}, "final answer", final, 2, -1)
	return items, groups
}

func prepareReviewSourceItem(item *reviewSourceItem, groupSize int) {
	if item.size != 0 {
		return
	}
	if item.event.Kind == "" {
		item.prefix = "[derived] " + item.role + ": "
	} else {
		pair := ""
		if item.event.Kind == swarm.NotifyToolCall.String() || item.event.Kind == swarm.NotifyToolResult.String() {
			pair = " unpaired"
			if groupSize > 1 {
				pair = " paired"
			}
		}
		item.prefix = fmt.Sprintf("[seq=%d kind=%s agent=%q role=%q call=%q%s] %s: ", item.event.Seq, item.event.Kind, item.event.AgentID, item.event.Role, item.event.ToolCallID, pair, item.role)
	}
	// Omitted groups never need framing or a clipped copy of their body.
	framing := utf8.RuneCountInString(item.prefix) + 2
	item.text, item.clipped = clipReviewSourceText(item.text, reviewMaxCharsPerMessage-framing)
	item.size = framing + utf8.RuneCountInString(item.text)
}

// selectReviewSourceGroup keeps every available half of a tool interaction.
// When the last group only partly fits, share the remaining text allowance
// instead of selecting a result without its call (or an older shorter group).
func selectReviewSourceGroup(items []reviewSourceItem, group reviewSourceGroup, budget int) (int, bool) {
	if budget < reviewSourceMinFraming {
		return 0, false
	}
	total, framing, minimum := 0, 0, 0
	for _, index := range group.items {
		item := &items[index]
		prepareReviewSourceItem(item, len(group.items))
		if item.size > reviewMaxCharsPerMessage {
			// Identity framing cannot be clipped into a different source.
			return 0, false
		}
		total += item.size
		framing += utf8.RuneCountInString(item.prefix) + 2
		minimum += min(utf8.RuneCountInString(item.text), 3)
	}
	if framing+minimum > budget {
		return 0, false
	}
	used := 0
	room := budget - framing
	for position, index := range group.items {
		item := &items[index]
		if total > budget {
			limit := room / (len(group.items) - position)
			text, clipped := clipReviewSourceText(item.text, limit)
			item.text = text
			item.clipped = item.clipped || clipped
			item.size = utf8.RuneCountInString(item.prefix) + utf8.RuneCountInString(text) + 2
		}
		item.selected = true
		room -= utf8.RuneCountInString(item.text)
		used += item.size
	}
	return used, true
}

// Stop counting at the cap: converting an entire multi-megabyte tool result
// to []rune just to retain its first few characters would copy the whole body.
func clipReviewSourceText(text string, limit int) (string, bool) {
	if limit <= 0 {
		return "", text != ""
	}
	count, cut := 0, 0
	for offset := range text {
		if count == limit {
			if limit == 1 {
				return "…", true
			}
			return strings.TrimSpace(text[:cut]) + " …", true
		}
		if count == limit-2 {
			cut = offset
		}
		count++
	}
	return text, false
}
