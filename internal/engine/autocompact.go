package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	compactPhaseStart       = "start"
	compactBriefingExtraKey = "zwai_compact_briefing"
	autoCompactMinGrowth    = 3
	spawnAgentToolName      = "spawn_agent"
	resumeAgentToolName     = "resume_agent"
)

// autoCompact rewrites the manager's ADK state before a model call once
// billed/estimated prompt tokens pass swarm.auto_compact_tokens. The human
// transcript is untouched; only what the next Generate sees shrinks.
type autoCompact struct {
	adk.BaseChatModelAgentMiddleware
	engine     *Engine
	threadID   string
	turnID     string
	providerID string
	modelName  string
	threshold  int
	keep       int

	mu        sync.Mutex
	model     model.BaseChatModel
	lastN     int
	lastAfter int
}

func (e *Engine) autoCompactHandlers(threadID, turnID string, th *store.Thread) []adk.ChatModelAgentMiddleware {
	return []adk.ChatModelAgentMiddleware{e.newAutoCompact(threadID, turnID, th)}
}

func (e *Engine) newAutoCompact(threadID, turnID string, th *store.Thread) *autoCompact {
	ac := &autoCompact{
		engine:    e,
		threadID:  threadID,
		turnID:    turnID,
		threshold: e.cfg.Swarm.AutoCompactLimit(),
		keep:      e.cfg.Swarm.CompactKeep(),
	}
	if th != nil {
		ac.providerID, ac.modelName = e.cfg.Swarm.ResolveCompact(th.ProviderID, th.Model)
	}
	return ac
}

func (m *autoCompact) BeforeModelRewriteState(ctx context.Context,
	state *adk.ChatModelAgentState, _ *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil || ctx.Err() != nil {
		return ctx, state, nil
	}
	tokens := promptTokenCount(state.Messages)
	if tokens > m.threshold {
		cleared := microCompact(state.Messages, microcompactKeepResults)
		if microCompactChanged(state.Messages, cleared) {
			next := *state
			next.Messages = cleared
			clearBilledUsage(next.Messages)
			state = &next
			tokens = promptTokenCount(state.Messages)
		}
	}
	if tokens <= m.threshold {
		return ctx, state, nil
	}
	m.mu.Lock()
	last := m.lastN
	lastAfter := m.lastAfter
	m.mu.Unlock()
	if last > 0 && len(state.Messages) < last+autoCompactMinGrowth {
		return ctx, state, nil
	}
	if lastAfter > 0 && tokens <= lastAfter {
		return ctx, state, nil
	}
	_, older, _ := splitCompactable(state.Messages, m.keep)
	if len(older) == 0 {
		return ctx, state, nil
	}

	m.emitStart(tokens)
	_ = m.engine.syncSessionMemory(ctx, m.threadID, m.turnID, true)
	folded := m.foldWithBriefing(state, m.engine.sessionMemoryOf(m.threadID))
	var err error
	if folded == nil {
		folded, err = m.foldWithEino(ctx, state)
	}
	summary := ""
	if folded != nil {
		summary = briefingFromMessages(folded.Messages)
	}
	if err != nil || summary == "" {
		msg := "the model returned nothing usable as a briefing"
		if err != nil {
			msg = err.Error()
		}
		m.engine.recordCompact(m.threadID, m.turnID, "", msg)
		return ctx, state, nil
	}

	after := promptTokenCount(folded.Messages)
	through := m.engine.throughSeqMatching(m.threadID, older)
	m.persist(summary, through, tokens, after)
	m.mu.Lock()
	m.lastN = len(folded.Messages)
	m.lastAfter = after
	m.mu.Unlock()
	return ctx, folded, nil
}

func (m *autoCompact) foldWithBriefing(state *adk.ChatModelAgentState, summary string) *adk.ChatModelAgentState {
	summary = acceptBriefing(summary, "")
	if state == nil || summary == "" {
		return nil
	}
	system, older, tail := splitCompactable(state.Messages, m.keep)
	if len(older) == 0 {
		return nil
	}
	roster := dropPairsAlreadyIn(m.rosterPin(), tail)
	next := *state
	next.Messages = assembleCompacted(system, summary, older, tail, roster)
	clearBilledUsage(next.Messages)
	return &next
}

func (m *autoCompact) emitStart(tokens int) {
	body, _ := json.Marshal(compactPayload{
		Auto:         true,
		Phase:        compactPhaseStart,
		TokensBefore: tokens,
	})
	m.engine.emit(store.Event{
		ThreadID: m.threadID, TurnID: m.turnID,
		Kind: KindCompacted, AgentID: CompactAgentID,
		Text: string(body),
	})
}

func (m *autoCompact) summarizer(ctx context.Context) (model.BaseChatModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model != nil {
		return m.model, nil
	}
	builder, err := m.engine.pool.ModelBuilder(ctx, m.providerID, m.modelName, "",
		m.engine.callRecorder(m.threadID, m.turnID))
	if err != nil {
		return nil, err
	}
	m.model = &compactStreamModel{inner: builder(CompactAgentID, CompactAgentID)}
	return m.model, nil
}

func (m *autoCompact) persist(summary string, through int64, tokensBefore, tokensAfter int) {
	patch := map[string]any{"compact_summary": summary}
	if through > 0 {
		patch["compact_through_seq"] = through
	}
	if err := m.engine.store.UpdateThread(m.threadID, patch); err != nil {
		m.engine.log.Warn("could not store auto-compact briefing", "thread", m.threadID, "err", err)
		m.engine.recordCompact(m.threadID, m.turnID, "", err.Error())
		return
	}
	th, err := m.engine.store.GetThread(m.threadID)
	if err != nil {
		m.engine.recordCompact(m.threadID, m.turnID, "", err.Error())
		return
	}
	body, _ := json.Marshal(compactPayload{
		Summary:      summary,
		ThroughSeq:   th.CompactThroughSeq,
		CharsAfter:   m.engine.contextChars(th),
		Auto:         true,
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
	})
	m.engine.recordCompact(m.threadID, m.turnID, string(body), "")
}

func (e *Engine) throughSeqMatching(threadID string, older []*schema.Message) int64 {
	folded := map[string]struct{}{}
	for _, m := range older {
		if k := strings.TrimSpace(compactMessageText(m)); k != "" {
			folded[k] = struct{}{}
		}
	}
	if len(folded) == 0 {
		return 0
	}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return 0
	}
	var through int64
	for _, r := range rows {
		if _, ok := folded[strings.TrimSpace(r.Content)]; ok && r.Seq > through {
			through = r.Seq
		}
	}
	return through
}

func (e *Engine) compactWatermark(threadID string) int64 {
	th, err := e.store.GetThread(threadID)
	if err != nil || strings.TrimSpace(th.CompactSummary) == "" {
		return 0
	}
	return th.CompactThroughSeq
}

func compactInputFromADK(previous string, older []*schema.Message) string {
	prefix := ""
	if prev := acceptBriefing(previous, ""); prev != "" {
		prefix = "Previous briefing:\n" + prev + "\n\n"
	}
	return newestFittingPrompt(prefix, compactInputMaxRunes, compactClipPerMessage, func(emit func(string) bool) {
		for i := len(older) - 1; i >= 0; i-- {
			line := compactSchemaLine(older[i])
			if line == "" {
				continue
			}
			if !emit(line) {
				return
			}
		}
	})
}

func compactLines(msgs []*schema.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(compactSchemaLine(m))
	}
	return strings.TrimSpace(b.String())
}

func compactSchemaLine(m *schema.Message) string {
	if m == nil {
		return ""
	}
	var role string
	switch m.Role {
	case schema.User:
		role = "Human: "
	case schema.Assistant:
		role = "Assistant: "
	case schema.Tool:
		role = "Tool: "
	default:
		return ""
	}
	text := strings.TrimSpace(m.Content)
	if text == "" && len(m.ToolCalls) > 0 {
		names := make([]string, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if n := strings.TrimSpace(tc.Function.Name); n != "" {
				names = append(names, n)
			}
		}
		text = strings.Join(names, ", ")
	}
	return role + clip(text, compactClipPerMessage) + "\n\n"
}

func splitCompactable(msgs []*schema.Message, keep int) (system, older, tail []*schema.Message) {
	i := 0
	for i < len(msgs) && msgs[i] != nil && msgs[i].Role == schema.System {
		i++
	}
	system = msgs[:i]
	rest := msgs[i:]
	if keep < 1 {
		keep = 1
	}
	if len(rest) <= keep {
		return system, nil, rest
	}
	start := len(rest) - keep
	for start > 0 && !tailToolSafe(rest[start:]) {
		start--
	}
	if start == 0 {
		return system, nil, rest
	}
	return system, rest[:start], rest[start:]
}

func tailToolSafe(tail []*schema.Message) bool {
	if len(tail) == 0 {
		return true
	}
	if tail[0] != nil && tail[0].Role == schema.Tool {
		return false
	}
	calls := map[string]struct{}{}
	for _, m := range tail {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				calls[tc.ID] = struct{}{}
			}
		}
		if m.Role == schema.Tool && m.ToolCallID != "" {
			if _, ok := calls[m.ToolCallID]; !ok {
				return false
			}
		}
	}
	return true
}

func assembleCompacted(system []*schema.Message, summary string, older, tail, roster []*schema.Message) []*schema.Message {
	briefing := schema.UserMessage(summary)
	if briefing.Extra == nil {
		briefing.Extra = map[string]any{}
	}
	briefing.Extra[compactBriefingExtraKey] = true
	out := append([]*schema.Message{}, system...)
	out = append(out, briefing)
	out = append(out, roster...)
	if u := lastHuman(older); u != nil && !containsMsg(tail, u) && !containsMsg(roster, u) {
		out = append(out, u)
	}
	return append(out, tail...)
}

func lastHuman(msgs []*schema.Message) *schema.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m != nil && m.Role == schema.User && !isCompactBriefingMessage(m) {
			return m
		}
	}
	return nil
}

func containsMsg(msgs []*schema.Message, want *schema.Message) bool {
	for _, m := range msgs {
		if sameMsg(m, want) {
			return true
		}
	}
	return false
}

func sameMsg(a, b *schema.Message) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	return a.Role == b.Role && a.Content == b.Content && a.ToolCallID == b.ToolCallID
}

func isCompactBriefingMessage(m *schema.Message) bool {
	if m == nil || m.Role != schema.User || m.Extra == nil {
		return false
	}
	v, ok := m.Extra[compactBriefingExtraKey]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "false"
	default:
		return v != nil
	}
}

func clearBilledUsage(msgs []*schema.Message) {
	for _, m := range msgs {
		if m == nil || m.ResponseMeta == nil {
			continue
		}
		// Stale billed totals from the uncompacted prompt would trip every
		// later call even though the model is about to see a shorter list.
		m.ResponseMeta.Usage = nil
	}
}

func promptTokenCount(msgs []*schema.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != schema.Assistant || m.ResponseMeta == nil || m.ResponseMeta.Usage == nil {
			continue
		}
		base := m.ResponseMeta.Usage.PromptTokens
		if base <= 0 {
			base = m.ResponseMeta.Usage.TotalTokens
		}
		if base <= 0 {
			continue
		}
		extra := 0
		for _, n := range msgs[i+1:] {
			extra += estimateMessageTokens(n)
		}
		return base + extra
	}
	n := 0
	for _, m := range msgs {
		n += estimateMessageTokens(m)
	}
	return n
}

func estimateMessageTokens(m *schema.Message) int {
	if m == nil {
		return 0
	}
	n := (utf8.RuneCountInString(m.Content) + 3) / 4
	n += (utf8.RuneCountInString(m.ReasoningContent) + 3) / 4
	for _, tc := range m.ToolCalls {
		n += (utf8.RuneCountInString(tc.Function.Name) + utf8.RuneCountInString(tc.Function.Arguments) + 3) / 4
	}
	return n
}

func compactMessageText(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if s := strings.TrimSpace(m.Content); s != "" {
		return s
	}
	var b strings.Builder
	for _, tc := range m.ToolCalls {
		b.WriteString(tc.Function.Name)
		b.WriteString(tc.Function.Arguments)
	}
	return b.String()
}

func (e *Engine) storedUserText(threadID, turnID string) map[string]bool {
	out := map[string]bool{}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return out
	}
	for _, m := range rows {
		if m.TurnID != turnID || schema.RoleType(m.Role) != schema.User {
			continue
		}
		if key := strings.TrimSpace(m.Content); key != "" {
			out[key] = true
		}
	}
	return out
}

func compactPersistStart(transcript []*schema.Message, inputCount int) int {
	start := inputCount + 1
	if start <= len(transcript) {
		return start
	}
	start = 1
	for i, m := range transcript {
		if isCompactBriefingMessage(m) {
			return i + 1
		}
		if i == 0 && m != nil && m.Role == schema.System {
			continue
		}
	}
	return start
}
