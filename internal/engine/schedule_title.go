package engine

import (
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
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
	if row == nil || !row.TitleAuto || !e.cfg.SwarmSnapshot().AutoTitle {
		return
	}
	id := row.ID
	if !e.titles.begin(scheduleTitlePoolKey(id)) {
		return
	}
	prompt := row.Prompt
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
		e.runScheduleTitle(id, origin, prompt)
	}()
}

func (e *Engine) runScheduleTitle(id, origin, prompt string) {
	if !e.cfg.SwarmSnapshot().AutoTitle {
		return
	}
	turnID := ""
	if origin != "" {
		turnID = e.lastTurnID(origin)
	}
	raw, err := e.generateAutomaticTitle(origin, turnID, scheduleTitlePrompt(), titleInput(prompt, ""))
	if !e.cfg.SwarmSnapshot().AutoTitle {
		return
	}
	if err != nil {
		e.log.Warn("schedule namer failed", "schedule", id, "err", err)
		return
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
