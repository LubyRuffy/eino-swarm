package engine

import (
	"context"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func scheduleTitlePoolKey(id string) string {
	return "sch:" + id
}

// kickScheduleTitle asks the conversation namer for a short inbox label.
// The truncated prompt is already on the row so the list is not blank
// while this runs. A title the human or a tool supplied is TitleAuto=false
// and never enters this path. Do not emit KindTitle: that event renames
// the origin conversation in the sidebar.
func (e *Engine) kickScheduleTitle(row *store.Schedule) {
	if row == nil || !row.TitleAuto || !e.cfg.Swarm.AutoTitle {
		return
	}
	id := row.ID
	if !e.titles.begin(scheduleTitlePoolKey(id)) {
		return
	}
	prompt := row.Prompt
	providerID := strings.TrimSpace(row.ProviderID)
	model := strings.TrimSpace(row.Model)
	if providerID == "" {
		providerID = e.scheduleDefaultProvider()
	}
	// Pin the namer endpoint before the goroutine. Tests (and PUT
	// /settings) copy e.cfg; reading it from the namer races that write.
	providerID, model = e.cfg.Swarm.ResolveTitle(providerID, model)
	origin := strings.TrimSpace(row.OriginThreadID)
	if origin == "" {
		origin = strings.TrimSpace(row.ThreadID)
	}
	go func() {
		defer e.titles.done(scheduleTitlePoolKey(id))
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("schedule namer panicked", "schedule", id, "panic", r)
			}
		}()
		e.runScheduleTitle(id, origin, prompt, providerID, model)
	}()
}

func (e *Engine) runScheduleTitle(id, origin, prompt, providerID, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), titleCallTimeout)
	defer cancel()

	turnID := ""
	if origin != "" {
		turnID = e.lastTurnID(origin)
	}
	builder, err := e.pool.ModelBuilder(ctx, providerID, model, "", e.callRecorder(origin, turnID))
	if err != nil {
		e.log.Warn("could not build the schedule namer", "schedule", id, "err", err)
		return
	}
	out, err := titleGenerate(ctx, builder(TitleAgentID, TitleAgentID), []*schema.Message{
		schema.SystemMessage(scheduleTitlePrompt()),
		schema.UserMessage(titleInput(prompt, "")),
	})
	if err != nil {
		e.log.Warn("schedule namer failed", "schedule", id, "err", err)
		return
	}
	raw := ""
	if out != nil {
		raw = out.Content
	}
	title := sanitizeTitle(raw)
	if title == "" {
		return
	}
	if _, err := e.store.ApplyAutoScheduleTitle(id, title); err != nil {
		e.log.Warn("could not apply the generated schedule title", "schedule", id, "err", err)
	}
}

// scheduleTitlePrompt is task-agnostic: the wait's instruction is the
// input, this only says how to name it. Example tasks do not belong here.
func scheduleTitlePrompt() string {
	return `Name this scheduled wait for a short inbox label.

Reply with only the title. No quotes, no markdown, no extra line.

The title must:
- be a compact noun phrase (a handful of words, or the equivalent in the human's language)
- name the subject of the work rather than repeating the request
- use the same language the human used`
}
