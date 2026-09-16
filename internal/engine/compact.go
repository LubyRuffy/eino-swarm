package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// KindCompacted is recorded when the human asks to fold earlier turns into a
// briefing. The event log stays complete; only later model replay shrinks.
const KindCompacted = "compacted"

// CompactAgentID is who the summarizer's events and model calls are attributed to.
const CompactAgentID = "compact-summarizer"

const (
	compactCallTimeout     = 60 * time.Second
	compactClipPerMessage  = 4000
	compactSummaryMaxRunes = 8000
)

// ErrNothingToCompact means the conversation is already short enough that
// folding it would throw away the only messages later turns still need.
var ErrNothingToCompact = errors.New("engine: there is not enough conversation to compact")

// compactGenerate is the summarizer's model call. Tests swap it to force a
// Generate failure without standing up a broken endpoint.
var compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	return m.Generate(ctx, msgs)
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
	Summary     string `json:"summary"`
	ThroughSeq  int64  `json:"through_seq"`
	CharsBefore int    `json:"chars_before"`
	CharsAfter  int    `json:"chars_after"`
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
	older, through, err := e.compactCandidates(th)
	if err != nil {
		return nil, err
	}
	turnID := e.lastTurnID(threadID)

	ctx, cancel := context.WithTimeout(context.Background(), compactCallTimeout)
	defer cancel()

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
	summary := sanitizeCompact(raw)
	if summary == "" {
		msg := "the model returned nothing usable as a briefing"
		e.recordCompact(threadID, turnID, "", msg)
		return nil, errors.New("engine: " + msg)
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
	var b strings.Builder
	if prev := strings.TrimSpace(previous); prev != "" {
		b.WriteString("Previous briefing:\n")
		b.WriteString(prev)
		b.WriteString("\n\n")
	}
	for _, m := range older {
		switch schema.RoleType(m.Role) {
		case schema.User:
			b.WriteString("Human: ")
		case schema.Assistant:
			b.WriteString("Assistant: ")
		default:
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" && len(m.Images) > 0 {
			text = "(image)"
		}
		b.WriteString(clip(text, compactClipPerMessage))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func compactPrompt() string {
	return `Write a dense briefing of the conversation so a later turn can continue the work without the earlier messages.

Reply with only the briefing. No heading that announces this is a briefing.

The briefing must:
- use the same language the human used
- keep standing constraints, decisions already made, and names a later turn would otherwise have to rediscover
- say what is still unfinished
- stay shorter than the messages it replaces`
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
