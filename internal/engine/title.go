package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// KindTitle is recorded when a conversation gets a generated sidebar name.
//
// The transcript ignores it — a title is metadata, not a chat row — but it
// is stored under the turn that produced it so `zwai trace` still shows the
// namer's model call and the name it picked.
const KindTitle = "title"

// TitleAgentID is who the namer's events and model calls are attributed to.
const TitleAgentID = "title-namer"

// titleMaxRunes keeps sidebar titles to one line at the narrowest supported
// sidebar width.
const titleMaxRunes = 48

// titleCallTimeout bounds the namer. Naming a conversation is one short
// Generate; waiting the full agent watchdog would pin Shutdown on a hung
// endpoint for a label nobody is staring at.
const titleCallTimeout = 30 * time.Second

// titleGenerate is the namer's model call. Tests swap it to force a Generate
// failure without standing up a broken endpoint.
var titleGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	return m.Generate(ctx, msgs)
}

// titlePool owns the namers in flight. One per conversation: two finished
// turns racing would otherwise both see TitleAuto and the second would
// waste a call (or, worse, rename after the first had already landed).
type titlePool struct {
	mu       sync.Mutex
	stopped  bool
	inflight map[string]struct{}
	wg       sync.WaitGroup
}

func newTitlePool() titlePool {
	return titlePool{inflight: map[string]struct{}{}}
}

func (p *titlePool) begin(threadID string) bool {
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

func (p *titlePool) done(threadID string) {
	p.mu.Lock()
	delete(p.inflight, threadID)
	p.mu.Unlock()
	p.wg.Done()
}

func (p *titlePool) stop(wait time.Duration) bool {
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

// autoTitle plants a readable placeholder from the first message so the
// sidebar is not "New conversation" while the namer is still thinking.
func (e *Engine) autoTitle(th *store.Thread, text string) {
	if !th.TitleAuto || strings.TrimSpace(th.Title) != "" {
		return
	}
	title := titleFrom(text)
	if title == "" {
		return
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"title": title}); err != nil {
		e.log.Warn("could not set the conversation title", "thread", th.ID, "err", err)
		return
	}
	th.Title = title
}

func titleFrom(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return ""
	}
	r := []rune(flat)
	if len(r) <= titleMaxRunes {
		return flat
	}
	return strings.TrimSpace(string(r[:titleMaxRunes])) + "…"
}

// scheduleTitle asks the model for a real name after a clean first finish.
// Interrupted and failed turns keep the placeholder: a half-answer is not
// what the sidebar should be named after.
func (e *Engine) scheduleTitle(threadID string, turn *store.Turn, status, userText, final string) {
	if !e.cfg.Swarm.AutoTitle || status != store.TurnDone {
		return
	}
	th, err := e.store.GetThread(threadID)
	if err != nil || !th.TitleAuto {
		return
	}
	if !e.titles.begin(threadID) {
		return
	}
	turnID := ""
	if turn != nil {
		turnID = turn.ID
	}
	go func() {
		defer e.titles.done(threadID)
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("conversation namer panicked", "turn", turnID, "panic", r)
			}
		}()
		e.runTitle(threadID, turn, userText, final)
	}()
}

func (e *Engine) runTitle(threadID string, turn *store.Turn, userText, final string) {
	ctx, cancel := context.WithTimeout(context.Background(), titleCallTimeout)
	defer cancel()

	providerID, model := e.cfg.Swarm.ResolveTitle(turn.ProviderID, turn.Model)
	builder, err := e.pool.ModelBuilder(ctx, providerID, model, "", e.callRecorder(threadID, turn.ID))
	if err != nil {
		e.recordTitle(threadID, turn.ID, "", err.Error())
		return
	}
	out, err := titleGenerate(ctx, builder(TitleAgentID, TitleAgentID), []*schema.Message{
		schema.SystemMessage(titlePrompt()),
		schema.UserMessage(titleInput(userText, final)),
	})
	if err != nil {
		e.recordTitle(threadID, turn.ID, "", err.Error())
		return
	}
	raw := ""
	if out != nil {
		raw = out.Content
	}
	title := sanitizeTitle(raw)
	if title == "" {
		e.recordTitle(threadID, turn.ID, "", "the model returned nothing usable as a title")
		return
	}
	ok, err := e.store.ApplyAutoTitle(threadID, title)
	if err != nil {
		e.log.Warn("could not apply the generated title", "thread", threadID, "err", err)
		e.recordTitle(threadID, turn.ID, title, err.Error())
		return
	}
	if !ok {
		// The user renamed it, or the conversation vanished. Do not emit a
		// title event: the sidebar would jump back to the generated name.
		return
	}
	e.recordTitle(threadID, turn.ID, title, "")
}

func (e *Engine) recordTitle(threadID, turnID, title, errText string) {
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindTitle, AgentID: TitleAgentID,
		Text: title, Err: errText,
	})
}

// titlePrompt is task-agnostic: the conversation is the input, this only
// says how to name it. Example tasks do not belong here.
func titlePrompt() string {
	return `Name this conversation for a short sidebar label.

Reply with only the title. No quotes, no markdown, no extra line.

The title must:
- be a compact noun phrase (a handful of words, or the equivalent in the human's language)
- name the subject of the work rather than repeating the request
- use the same language the human used`
}

func titleInput(user, assistant string) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(oneLine(user))
	if a := strings.TrimSpace(assistant); a != "" {
		b.WriteString("\n\nAssistant: ")
		b.WriteString(oneLine(a))
	}
	return b.String()
}

// sanitizeTitle turns a model reply into a sidebar label. Models pad with
// quotes, a heading, a trailing period, or a second paragraph of explanation;
// none of that belongs in the list.
func sanitizeTitle(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Trim(s, " \t\"'`“”‘’")
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, " -.–—:;,.!?。！？")
	return titleFrom(s)
}
