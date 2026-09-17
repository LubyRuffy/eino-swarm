package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// KindCompacted is recorded when earlier context is folded into a briefing,
// either by /compact or automatically at swarm.auto_compact_tokens. The event
// log stays complete; only later model replay shrinks.
const KindCompacted = "compacted"

// CompactAgentID is who the summarizer's events and model calls are attributed to.
const CompactAgentID = "compact-summarizer"

const (
	compactClipPerMessage  = 4000
	compactSummaryMaxRunes = 8000
	compactInputMaxRunes   = compactSummaryMaxRunes * 4
)

// ErrNothingToCompact means the conversation is already short enough that
// folding it would throw away the only messages later turns still need.
var ErrNothingToCompact = errors.New("engine: there is not enough conversation to compact")

// compactGenerate is the summarizer's model call. Tests swap it to force a
// failure without standing up a broken endpoint. The default drains Stream
// so a thinking model that is still emitting tokens is not killed the way
// a one-shot Generate JSON body was.
var compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	return compactStream(ctx, m, msgs)
}

// compactStream is Codex-style: headers arrive with the first token, and
// each chunk resets the provider idle clock. There is no second compact
// deadline — wrapping Stream in WithTimeout would turn timeout_seconds
// back into a total cap and kill a briefing that is still emitting tokens.
func compactStream(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	sr, err := m.Stream(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.ConcatMessageStream(sr)
}

// compactStreamModel makes eino's summarizer Stream even though it calls
// Generate. Tests that inject a stub skip this wrapper.
type compactStreamModel struct {
	inner model.BaseChatModel
}

func (m *compactStreamModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return compactStream(ctx, m.inner, in, opts...)
}

func (m *compactStreamModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, in, opts...)
}

type compactPool struct {
	mu       sync.Mutex
	stopped  bool
	inflight map[string]struct{}
	wg       sync.WaitGroup
}

func newCompactPool() compactPool {
	return compactPool{inflight: map[string]struct{}{}}
}

func (p *compactPool) begin(threadID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return false
	}
	if _, ok := p.inflight[threadID]; ok {
		return false
	}
	p.inflight[threadID] = struct{}{}
	p.wg.Add(1)
	return true
}

func (p *compactPool) done(threadID string) {
	p.mu.Lock()
	delete(p.inflight, threadID)
	p.mu.Unlock()
	p.wg.Done()
}

func (p *compactPool) stop(wait time.Duration) bool {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(wait):
		return false
	}
}

type compactPayload struct {
	Summary      string `json:"summary"`
	ThroughSeq   int64  `json:"through_seq"`
	CharsBefore  int    `json:"chars_before,omitempty"`
	CharsAfter   int    `json:"chars_after,omitempty"`
	Auto         bool   `json:"auto,omitempty"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	TokensAfter  int    `json:"tokens_after,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

// CompactThread folds older replay messages into a briefing for later turns.
// The transcript the human sees does not change: events stay. A running turn
// is busy — its in-flight history is already built.
func (e *Engine) CompactThread(threadID string) (*store.Thread, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	if e.Status(threadID).Running {
		return nil, ErrBusy
	}
	if !e.compacts.begin(threadID) {
		return nil, ErrBusy
	}
	defer e.compacts.done(threadID)

	before := e.contextChars(th)
	older, through, candErr := e.compactCandidates(th)
	turnID := e.lastTurnID(threadID)
	ctx := context.Background()
	// Compact is a view of the rolling session briefing when one exists.
	// Catch-up is the caller's job (auto-compact, /goal continue); this
	// path must not mint a second summarizer call on the conversation model
	// and skip a pinned compact model. A /goal wrap-up still copies that
	// briefing when there is not enough replay to fold.
	summary := acceptBriefing(th.SessionMemory, "")
	if candErr != nil {
		if !errors.Is(candErr, ErrNothingToCompact) || summary == "" || summary == strings.TrimSpace(th.CompactSummary) {
			return nil, candErr
		}
		through = th.CompactThroughSeq
	} else if summary == "" {
		providerID, model := e.cfg.Swarm.ResolveCompact(th.ProviderID, th.Model)
		builder, err := e.pool.ModelBuilder(ctx, providerID, model, "", e.callRecorder(threadID, turnID))
		if err != nil {
			e.recordCompact(threadID, turnID, "", err.Error())
			return nil, err
		}
		input := compactInput(th.CompactSummary, older)
		out, err := compactGenerate(ctx, builder(CompactAgentID, CompactAgentID), []*schema.Message{
			schema.SystemMessage(compactPrompt()),
			schema.UserMessage(input),
		})
		if err != nil {
			e.recordCompact(threadID, turnID, "", err.Error())
			return nil, err
		}
		raw := ""
		if out != nil {
			raw = out.Content
		}
		summary = acceptBriefing(raw, input)
		if summary == "" {
			msg := briefingRejectReason(raw, input)
			e.recordCompact(threadID, turnID, "", msg)
			return nil, errors.New("engine: " + msg)
		}
	}

	if err := e.store.UpdateThread(threadID, map[string]any{
		"compact_summary":     summary,
		"compact_through_seq": through,
	}); err != nil {
		e.recordCompact(threadID, turnID, "", err.Error())
		return nil, err
	}
	th, err = e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(compactPayload{
		Summary:     summary,
		ThroughSeq:  through,
		CharsBefore: before,
		CharsAfter:  e.contextChars(th),
	})
	e.recordCompact(threadID, turnID, string(payload), "")
	return th, nil
}

func (e *Engine) compactCandidates(th *store.Thread) ([]store.Message, int64, error) {
	rows, err := e.store.ListMessages(th.ID)
	if err != nil {
		return nil, 0, err
	}
	all := replayableMessages(rows)
	keep := e.cfg.Swarm.CompactKeep()
	if keep < 0 {
		keep = 0
	}
	if len(all) <= keep {
		return nil, 0, ErrNothingToCompact
	}
	older := all[:len(all)-keep]
	if th.CompactThroughSeq > 0 && strings.TrimSpace(th.CompactSummary) != "" {
		var fresh []store.Message
		for _, m := range older {
			if m.Seq > th.CompactThroughSeq {
				fresh = append(fresh, m)
			}
		}
		older = fresh
	}
	if len(older) == 0 {
		return nil, 0, ErrNothingToCompact
	}
	return older, older[len(older)-1].Seq, nil
}

func replayableMessages(rows []store.Message) []store.Message {
	out := make([]store.Message, 0, len(rows))
	for _, r := range rows {
		switch schema.RoleType(r.Role) {
		case schema.User:
			if strings.TrimSpace(r.Content) != "" || len(r.Images) > 0 {
				out = append(out, r)
			}
		case schema.Assistant:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, r)
			}
		}
	}
	return out
}

func compactInput(previous string, older []store.Message) string {
	prefix := ""
	if prev := acceptBriefing(previous, ""); prev != "" {
		prefix = "Previous briefing:\n" + prev + "\n\n"
	}
	return newestFittingPrompt(prefix, compactInputMaxRunes, compactClipPerMessage, func(emit func(string) bool) {
		for i := len(older) - 1; i >= 0; i-- {
			line := compactStoreLine(older[i])
			if line == "" {
				continue
			}
			if !emit(line) {
				return
			}
		}
	})
}

func compactStoreLine(m store.Message) string {
	var role string
	switch schema.RoleType(m.Role) {
	case schema.User:
		role = "Human: "
	case schema.Assistant:
		role = "Assistant: "
	default:
		return ""
	}
	text := strings.TrimSpace(m.Content)
	if text == "" && len(m.Images) > 0 {
		text = "(image)"
	}
	if text == "" {
		return ""
	}
	return role + clip(text, compactClipPerMessage) + "\n\n"
}

// newestFittingPrompt keeps prefix plus the newest lines that fit max
// runes. each must yield newest-first and stop when emit returns false.
func newestFittingPrompt(prefix string, max, minKeep int, each func(emit func(string) bool)) string {
	if max < 1 {
		max = minKeep
	}
	budget := max - utf8.RuneCountInString(prefix)
	if budget < minKeep {
		budget = minKeep
	}
	var picked []string
	used := 0
	each(func(line string) bool {
		if line == "" {
			return true
		}
		n := utf8.RuneCountInString(line)
		if used+n > budget && len(picked) > 0 {
			return false
		}
		picked = append(picked, line)
		used += n
		return true
	})
	var b strings.Builder
	b.WriteString(prefix)
	for i := len(picked) - 1; i >= 0; i-- {
		b.WriteString(picked[i])
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > max {
		return clip(out, max)
	}
	return out
}

func compactPrompt() string {
	return `Write a dense briefing of the conversation so a later turn can continue the work without the earlier messages.

Reply with only the briefing. No heading that announces this is a briefing.

The briefing must:
- use the same language the human used
- keep standing constraints, decisions already made, and names a later turn would otherwise have to rediscover
- say what is still unfinished
- stay shorter than the messages it replaces
- not be a Tool/Human/Assistant transcript and not paste tool JSON`
}

func sanitizeCompact(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.Trim(s, " \t\"'`")
	return clip(s, compactSummaryMaxRunes)
}

func (e *Engine) recordCompact(threadID, turnID, text, errText string) {
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindCompacted, AgentID: CompactAgentID,
		Text: text, Err: errText,
	})
}

func (e *Engine) lastTurnID(threadID string) string {
	turns, err := e.store.ListTurns(threadID)
	if err != nil || len(turns) == 0 {
		return ""
	}
	return turns[len(turns)-1].ID
}

// ContextUsage is how full this conversation's replay looks, for the /compact hint.
func (e *Engine) ContextUsage(threadID string) (chars, budget int, err error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return 0, 0, err
	}
	return e.contextChars(th), e.cfg.Swarm.ContextBudget(), nil
}

func (e *Engine) contextChars(th *store.Thread) int {
	n := len([]rune(strings.TrimSpace(th.Goal))) + len([]rune(strings.TrimSpace(th.CompactSummary)))
	hist, err := e.replayHistory(th.ID)
	if err != nil {
		return n
	}
	for _, m := range hist {
		if m == nil {
			continue
		}
		n += len([]rune(m.Content))
	}
	return n
}
