package engine

import (
	"context"
	"regexp"
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

// titlePool owns the namers in flight. One per conversation: two opening
// messages racing would otherwise both see TitleAuto and the second would
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
// It reports whether it planted, so the namer runs once on that opening
// line — a follow-up must not get a second name just because the first
// call failed or the first turn ran long.
func (e *Engine) autoTitle(th *store.Thread, text string) bool {
	if !th.TitleAuto || strings.TrimSpace(th.Title) != "" {
		return false
	}
	title := titleFrom(text)
	if title == "" {
		return false
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"title": title}); err != nil {
		e.log.Warn("could not set the conversation title", "thread", th.ID, "err", err)
		return false
	}
	th.Title = title
	return true
}

func titleFrom(text string) string {
	flat := strings.Join(strings.Fields(plainUserText(text)), " ")
	if flat == "" {
		return ""
	}
	r := []rune(flat)
	if len(r) <= titleMaxRunes {
		return flat
	}
	return strings.TrimSpace(string(r[:titleMaxRunes])) + "…"
}

// scheduleTitle asks the model for a real name from the opening message.
// Waiting for the first finish made a long first turn rename the sidebar
// after the human had already learned the placeholder. A landed name, or
// a title the user typed, stays put.
func (e *Engine) scheduleTitle(threadID string, turn *store.Turn, userText, final string) {
	if !e.cfg.Swarm.AutoTitle {
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
	e.reindex(threadID)
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
	b.WriteString(oneLine(plainUserText(user)))
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

// plainUserText is what a sidebar label should name: the human's request,
// not the <selected_text> / <user_request> wrapper that travels with a
// quoted send. Tags in a highlight are unescaped so a stray closer cannot
// hide the request.
func plainUserText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.Contains(text, "<user_request>") {
		if s := innerXML(userRequestTagRe, text, "user_request"); s != "" {
			return s
		}
	}
	if strings.Contains(text, "<selected_text>") {
		if parts := innerXMLAll(selectedTextTagRe, text, "selected_text"); len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	if strings.HasPrefix(text, legacySelectedLabel) {
		return plainLegacyQuote(text)
	}
	return text
}

const legacySelectedLabel = "Selected text:"

var (
	selectedTextTagRe = regexp.MustCompile(`(?s)<selected_text>\n?(.*?)\n?</selected_text>`)
	userRequestTagRe  = regexp.MustCompile(`(?s)<user_request>\n?(.*?)\n?</user_request>`)
)

func innerXML(re *regexp.Regexp, text, tag string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return unescapeXMLClose(strings.TrimSpace(m[1]), tag)
}

func innerXMLAll(re *regexp.Regexp, text, tag string) []string {
	ms := re.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if len(m) < 2 {
			continue
		}
		s := unescapeXMLClose(strings.TrimSpace(m[1]), tag)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func unescapeXMLClose(s, tag string) string {
	return strings.ReplaceAll(s, "</ "+tag+">", "</"+tag+">")
}

func plainLegacyQuote(text string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(text, legacySelectedLabel))
	if i := strings.LastIndex(rest, "\n\n"); i >= 0 {
		tail := strings.TrimSpace(rest[i+2:])
		if tail != "" && !strings.HasPrefix(tail, legacySelectedLabel) {
			return tail
		}
	}
	return rest
}
