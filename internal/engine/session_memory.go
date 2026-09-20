package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KindSessionMemory is recorded when the rolling session briefing is
// refreshed. The transcript ignores it the way it ignores a generated title:
// the briefing is for later compact and for the reviewer, not a chat row.
const KindSessionMemory = "session_memory"

// SessionMemoryAgentID is who the briefing's model calls are attributed to.
const SessionMemoryAgentID = "session-memory"

const (
	sessionMemoryInitTokens    = 10_000
	sessionMemoryUpdateTokens  = 5_000
	sessionMemoryMinToolCalls  = 3
	sessionMemoryInputMaxRunes = compactSummaryMaxRunes * 4
	sessionMemoryToolClip      = 400
	sessionMemoryMessageClip   = 1200
)

// sessionMemoryPool serializes refreshes of one conversation. Two overlapping
// summarizer calls would each read the same through-seq and the later write
// would drop the earlier briefing's new events.
type sessionMemoryPool struct {
	mu       sync.Mutex
	stopped  bool
	inflight map[string]chan struct{}
	wg       sync.WaitGroup
}

func newSessionMemoryPool() sessionMemoryPool {
	return sessionMemoryPool{inflight: map[string]chan struct{}{}}
}

func (p *sessionMemoryPool) begin(threadID string) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil
	}
	if ch, ok := p.inflight[threadID]; ok {
		p.wg.Add(1)
		return ch
	}
	ch := make(chan struct{}, 1)
	p.inflight[threadID] = ch
	p.wg.Add(1)
	return ch
}

// spawn accounts for a post-turn wrapper before skip checks or begin().
// Shutdown would otherwise see wg==0 and return while briefing/review are
// already in flight, then refuse the review that was supposed to follow.
func (p *sessionMemoryPool) spawn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return false
	}
	p.wg.Add(1)
	return true
}

func (p *sessionMemoryPool) done() { p.wg.Done() }

func (p *sessionMemoryPool) stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
}

func (p *sessionMemoryPool) wait(d time.Duration) bool {
	finished := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(d):
		return false
	}
}

// shouldRefreshSessionMemory is Claude Code's session-memory gate: first
// briefing after the init token floor, later ones after the window has grown
// and either enough tool calls landed or the last assistant turn was a
// natural break (no tools).
func shouldRefreshSessionMemory(tokens, lastTokens, toolsSince int, naturalBreak bool) bool {
	if tokens < sessionMemoryInitTokens {
		return false
	}
	if lastTokens <= 0 {
		return true
	}
	if tokens-lastTokens < sessionMemoryUpdateTokens {
		return false
	}
	return toolsSince >= sessionMemoryMinToolCalls || naturalBreak
}

func sessionMemoryPrompt() string {
	return `Write a dense briefing of the conversation so a later turn can continue the work, and so a later memory review can see what was learned.

Reply with only the briefing. No heading that announces this is a briefing.

The briefing must:
- use the same language the human used
- keep standing constraints, decisions already made, and names a later turn would otherwise have to rediscover
- record errors and how they were resolved
- record a procedure that worked or failed, in enough detail to follow again
- say what is still unfinished
- stay shorter than the text it replaces
- not be a Tool/Human/Assistant transcript and not paste tool JSON`
}

func sessionMemoryInput(previous string, events []store.Event) string {
	body, _ := boundSessionMemoryInput(previous, events)
	return body
}

// boundSessionMemoryInput keeps previous briefing plus the newest events
// that fit the rune cap. Tool results are clipped harder than assistant
// text so one exec payload cannot crowd out the work.
func boundSessionMemoryInput(previous string, events []store.Event) (string, int64) {
	prev := acceptBriefing(previous, "")
	prefix := ""
	if prev != "" {
		prefix = "Previous briefing:\n" + prev + "\n\n"
	}
	budget := sessionMemoryInputMaxRunes - utf8.RuneCountInString(prefix)
	if budget < sessionMemoryToolClip {
		budget = sessionMemoryToolClip
	}
	type item struct {
		seq  int64
		line string
	}
	var picked []item
	used := 0
	for i := len(events) - 1; i >= 0; i-- {
		role, text, ok := reviewEventLine(events[i])
		if !ok {
			continue
		}
		clipN := sessionMemoryMessageClip
		if role == "tool" {
			clipN = sessionMemoryToolClip
		}
		line := role + ": " + clip(text, clipN) + "\n\n"
		n := utf8.RuneCountInString(line)
		if used+n > budget && len(picked) > 0 {
			break
		}
		picked = append(picked, item{events[i].Seq, line})
		used += n
	}
	var b strings.Builder
	b.WriteString(prefix)
	var through int64
	if len(picked) > 0 {
		b.WriteString("New events:\n")
		for i := len(picked) - 1; i >= 0; i-- {
			b.WriteString(picked[i].line)
			if picked[i].seq > through {
				through = picked[i].seq
			}
		}
	}
	body := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(body) > sessionMemoryInputMaxRunes {
		body = clip(body, sessionMemoryInputMaxRunes)
	}
	return body, through
}

func skipSessionMemoryRefresh(force bool, tokens, lastTokens, toolsSince int, natural bool) bool {
	if shouldRefreshSessionMemory(tokens, lastTokens, toolsSince, natural) {
		return false
	}
	// A failed refresh stamps lastTokens so auto-compact cannot
	// resend the same payload on every Generate.
	return !force || lastTokens > 0
}

func estimateEventTokens(events []store.Event) int {
	n := 0
	for _, ev := range events {
		n += (utf8.RuneCountInString(ev.Text) + 3) / 4
	}
	return n
}

func countToolCalls(events []store.Event) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == "tool_call" {
			n++
		}
	}
	return n
}

func lastAssistantHadTools(events []store.Event) bool {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case "tool_call":
			return true
		case "agent_message", "done":
			return false
		}
	}
	return false
}

func eventsAfter(events []store.Event, through int64) []store.Event {
	if through <= 0 {
		return events
	}
	out := events[:0:0]
	for _, ev := range events {
		if ev.Seq > through {
			out = append(out, ev)
		}
	}
	return out
}

func lastEventSeq(events []store.Event) int64 {
	var n int64
	for _, ev := range events {
		if ev.Seq > n {
			n = ev.Seq
		}
	}
	return n
}

func (e *Engine) syncSessionMemory(ctx context.Context, threadID, turnID string, force bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.sessionMemoryRefreshTimeout())
		defer cancel()
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return err
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return err
	}
	tokens := estimateEventTokens(events)
	fresh := eventsAfter(events, th.SessionMemoryThroughSeq)
	toolsSince := countToolCalls(fresh)
	natural := !lastAssistantHadTools(events)
	if skipSessionMemoryRefresh(force, tokens, th.SessionMemoryTokens, toolsSince, natural) {
		return nil
	}
	// KindSessionMemory (and other trace noise) is recorded after the
	// through-seq stamp. Counting it as "new work" would make every
	// force refresh re-summarize the briefing it just wrote.
	if strings.TrimSpace(sessionMemoryInput("", fresh)) == "" {
		return nil
	}
	gate := e.sessions.begin(threadID)
	if gate == nil {
		return nil
	}
	defer e.sessions.done()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case gate <- struct{}{}:
	}
	defer func() { <-gate }()

	th, err = e.store.GetThread(threadID)
	if err != nil {
		return err
	}
	events, err = e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return err
	}
	tokens = estimateEventTokens(events)
	fresh = eventsAfter(events, th.SessionMemoryThroughSeq)
	if skipSessionMemoryRefresh(force, tokens, th.SessionMemoryTokens, countToolCalls(fresh), !lastAssistantHadTools(events)) {
		return nil
	}
	if strings.TrimSpace(sessionMemoryInput("", fresh)) == "" {
		return nil
	}
	input := sessionMemoryInput(th.SessionMemory, fresh)
	return e.refreshSessionMemory(ctx, threadID, turnID, th, events, tokens, input)
}

func (e *Engine) refreshSessionMemory(ctx context.Context, threadID, turnID string, th *store.Thread, events []store.Event, tokens int, input string) error {
	if turnID == "" {
		turnID = e.lastTurnID(threadID)
	}
	builder, err := e.pool.ModelBuilder(ctx, th.ProviderID, th.Model, "",
		e.callRecorder(threadID, turnID))
	if err != nil {
		e.recordSessionMemory(threadID, turnID, "", 0, err.Error())
		e.markSessionMemoryAttempt(threadID, tokens)
		return err
	}
	out, err := compactGenerate(ctx, builder(SessionMemoryAgentID, SessionMemoryAgentID), []*schema.Message{
		schema.SystemMessage(sessionMemoryPrompt()),
		schema.UserMessage(input),
	})
	if err != nil {
		e.recordSessionMemory(threadID, turnID, "", 0, err.Error())
		e.markSessionMemoryAttempt(threadID, tokens)
		return err
	}
	raw := ""
	if out != nil {
		raw = out.Content
	}
	// The extract is already short. Length-vs-source would refuse a
	// briefing that restates the last request (the mock, and many models).
	summary := acceptBriefing(raw, "")
	if summary == "" {
		msg := briefingRejectReason(raw, input)
		e.recordSessionMemory(threadID, turnID, "", 0, msg)
		e.markSessionMemoryAttempt(threadID, tokens)
		return nil
	}
	through := lastEventSeq(events)
	if err := e.store.UpdateThread(threadID, map[string]any{
		"session_memory":             summary,
		"session_memory_through_seq": through,
		"session_memory_tokens":      tokens,
	}); err != nil {
		e.recordSessionMemory(threadID, turnID, "", 0, err.Error())
		return err
	}
	e.recordSessionMemory(threadID, turnID, summary, through, "")
	return nil
}

func (e *Engine) recordSessionMemory(threadID, turnID, summary string, through int64, errText string) {
	body := ""
	if errText == "" {
		raw, _ := json.Marshal(compactPayload{Summary: summary, ThroughSeq: through})
		body = string(raw)
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSessionMemory, AgentID: SessionMemoryAgentID,
		Text: body, Err: errText,
	})
}

// scheduleSessionAndReview refreshes the rolling briefing, then starts the
// post-turn memory review. Both are extras: a queued follow-up must not wait
// for either, or the composer looks idle with a message that never starts.
func (e *Engine) scheduleSessionAndReview(threadID string, turn *store.Turn, status string,
	pc *projectContext, final string,
) {
	if !e.sessions.spawn() {
		return
	}
	turnID := ""
	if turn != nil {
		turnID = turn.ID
	}
	go func() {
		defer e.sessions.done()
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("session briefing panicked", "turn", turnID, "panic", r)
			}
		}()
		if err := e.syncSessionMemory(context.Background(), threadID, turnID, false); err != nil {
			e.log.Warn("could not refresh the session briefing", "turn", turnID, "err", err)
		}
		e.scheduleReview(threadID, turn, status, pc, final)
	}()
}

func (e *Engine) sessionMemoryOf(threadID string) string {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return ""
	}
	return acceptBriefing(th.SessionMemory, "")
}

func (e *Engine) markSessionMemoryAttempt(threadID string, tokens int) {
	if tokens <= 0 {
		return
	}
	_ = e.store.UpdateThread(threadID, map[string]any{
		"session_memory_tokens": tokens,
	})
}

func (e *Engine) sessionMemoryRefreshTimeout() time.Duration {
	if e == nil || e.cfg == nil || len(e.cfg.Models.Providers) == 0 {
		return config.DefaultRequestTimeout
	}
	def := strings.TrimSpace(e.cfg.Models.Default)
	for _, p := range e.cfg.Models.Providers {
		if def == "" || p.ID == def {
			return p.Timeout()
		}
	}
	return e.cfg.Models.Providers[0].Timeout()
}

func mergeSessionProgress(previous, progress string) string {
	progress = acceptBriefing(progress, "")
	prev := acceptBriefing(previous, "")
	if progress == "" {
		return prev
	}
	if prev == "" {
		return clip(progress, compactSummaryMaxRunes)
	}
	return clip(prev+"\n\n"+progress, compactSummaryMaxRunes)
}

func (e *Engine) mergeSessionMemory(threadID, turnID, progress string) {
	progress = strings.TrimSpace(progress)
	if progress == "" {
		return
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return
	}
	summary := mergeSessionProgress(th.SessionMemory, progress)
	if summary == "" || summary == strings.TrimSpace(th.SessionMemory) {
		return
	}
	if err := e.store.UpdateThread(threadID, map[string]any{
		"session_memory": summary,
	}); err != nil {
		e.recordSessionMemory(threadID, turnID, "", 0, err.Error())
		return
	}
	e.recordSessionMemory(threadID, turnID, summary, th.SessionMemoryThroughSeq, "")
}
