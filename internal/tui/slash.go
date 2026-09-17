package tui

import (
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/slash"
)

// tuiSlashCommands is the composer catalog. Typing `/` lists these the way
// Codex and Claude do: name + description, prefix filter, aliases hidden
// until you type them. `/goal` is a real command (Codex Goal, Cursor's
// `/goal` skill): inline args start the objective now, they are not a
// user task that happens to begin with a slash. `/compact` stays desktop-
// only; this session has no conversation store to fold.
type slashCommand struct {
	name        string
	aliases     []string
	description string
	needsArg    bool
	hidden      bool
	parent      string // "model"/"reason" when this row is a picker value
	hint        string // right-side current value, like the desktop compact %
}

var tuiSlashCommands = []slashCommand{
	{
		name:        "goal",
		description: "set a standing objective and start it",
		needsArg:    true,
	},
	{
		name:        "model",
		description: "choose what model to use",
		needsArg:    true,
	},
	{
		name:        "reason",
		aliases:     []string{"reasoning", "think"},
		description: "choose thinking level",
		needsArg:    true,
	},
	{
		name:        "clear",
		description: "clear the transcript and keep the session",
	},
	{
		name:        "help",
		description: "list commands",
	},
	{
		name:        "exit",
		description: "leave the terminal session",
	},
	{
		// Codex hides /quit until typed; /exit is the visible twin.
		name:        "quit",
		description: "leave the terminal session",
		hidden:      true,
	},
}

type slashDraftQuery struct {
	token    string
	args     string
	hasSpace bool
}

func slashDraft(text string) *slashDraftQuery {
	rest, ok := slash.Leading(text)
	if !ok {
		return nil
	}
	if name, arg, parsed := parseTUICommand(text); parsed && arg != "" && name == "goal" {
		return nil
	}
	token, args, found := strings.Cut(rest, " ")
	return &slashDraftQuery{
		token:    token,
		args:     strings.TrimSpace(args),
		hasSpace: found,
	}
}

func (d *slashDraftQuery) key() string {
	if d == nil {
		return ""
	}
	return d.token + "\x00" + d.args + "\x00" + boolKey(d.hasSpace)
}

func boolKey(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func filterSlashCommands(query string) []slashCommand {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		var out []slashCommand
		for _, c := range tuiSlashCommands {
			if c.hidden {
				continue
			}
			out = append(out, c)
		}
		return out
	}
	var exact, prefix []slashCommand
	for _, c := range tuiSlashCommands {
		switch slashMatchKind(c, q) {
		case "exact":
			exact = append(exact, c)
		case "prefix":
			prefix = append(prefix, c)
		}
	}
	return append(exact, prefix...)
}

func slashMatchKind(c slashCommand, q string) string {
	if c.name == q {
		return "exact"
	}
	for _, a := range c.aliases {
		if a == q {
			return "exact"
		}
	}
	if strings.HasPrefix(c.name, q) {
		return "prefix"
	}
	for _, a := range c.aliases {
		if strings.HasPrefix(a, q) {
			return "prefix"
		}
	}
	return ""
}

func lookupSlash(name string) (slashCommand, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return slashCommand{}, false
	}
	for _, c := range tuiSlashCommands {
		if c.name == name {
			return c, true
		}
		for _, a := range c.aliases {
			if a == name {
				return c, true
			}
		}
	}
	return slashCommand{}, false
}

func parseTUICommand(text string) (name, arg string, ok bool) {
	name, arg, ok = slash.Split(text)
	if !ok {
		return "", "", false
	}
	cmd, known := lookupSlash(name)
	if !known {
		return "", "", false
	}
	return cmd.name, arg, true
}

func nextSlashIndex(current, count, delta int) int {
	if count <= 0 {
		return 0
	}
	current %= count
	if current < 0 {
		current += count
	}
	got := (current + delta) % count
	if got < 0 {
		got += count
	}
	return got
}

func (m swarmTUI) slashItems() []slashCommand {
	if !m.interactive || m.busy {
		return nil
	}
	d := slashDraft(m.input)
	if d == nil {
		return nil
	}
	if items, ok := m.slashPickerItems(d); ok {
		return items
	}
	return m.withSlashHints(filterSlashCommands(d.token))
}

func (m swarmTUI) withSlashHints(items []slashCommand) []slashCommand {
	if len(items) == 0 {
		return items
	}
	out := make([]slashCommand, len(items))
	copy(out, items)
	for i := range out {
		out[i].hint = m.slashHint(out[i].name)
	}
	return out
}

func (m swarmTUI) slashHint(name string) string {
	if m.choice == nil {
		return ""
	}
	switch name {
	case "model":
		return strings.TrimSpace(m.choice.CurrentModel())
	case "reason":
		return reasoningLabel(m.choice.CurrentReasoning())
	default:
		return ""
	}
}

func slashHelpText() string {
	var b strings.Builder
	for _, c := range tuiSlashCommands {
		if c.hidden {
			continue
		}
		fmt.Fprintf(&b, "/%-8s %s\n", c.name, c.description)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m swarmTUI) slashPickerItems(d *slashDraftQuery) ([]slashCommand, bool) {
	if d == nil {
		return nil, false
	}
	cmd, known := lookupSlash(d.token)
	if !known {
		return nil, false
	}
	open := d.hasSpace || strings.EqualFold(d.token, cmd.name) && d.args != ""
	if !open {
		return nil, false
	}
	switch cmd.name {
	case "model":
		return m.modelPickerItems(d.args), true
	case "reason":
		return m.reasonPickerItems(d.args), true
	default:
		// Freeform args (`/goal …`) submit; they are not a second picker.
		return nil, false
	}
}

func (m swarmTUI) modelPickerItems(filter string) []slashCommand {
	if m.choice == nil {
		return nil
	}
	cur := m.choice.CurrentModel()
	var out []slashCommand
	q := strings.ToLower(strings.TrimSpace(filter))
	for _, name := range m.choice.Catalog {
		if q != "" && !strings.HasPrefix(strings.ToLower(name), q) && !strings.EqualFold(name, q) {
			continue
		}
		desc := "use this model for the next turn"
		if name == cur {
			desc = "current"
		}
		out = append(out, slashCommand{name: name, description: desc, parent: "model"})
	}
	return out
}

func (m swarmTUI) reasonPickerItems(filter string) []slashCommand {
	if m.choice == nil {
		return nil
	}
	cur := m.choice.CurrentReasoning()
	q := strings.ToLower(strings.TrimSpace(filter))
	var out []slashCommand
	for _, level := range m.choice.Levels {
		label := reasoningLabel(level)
		if q != "" && !strings.HasPrefix(label, q) && !strings.EqualFold(level, q) && !strings.EqualFold(label, q) {
			continue
		}
		desc := "use this thinking level for the next turn"
		if level == cur {
			desc = "current"
		}
		out = append(out, slashCommand{name: label, description: desc, parent: "reason"})
	}
	return out
}

func (m *swarmTUI) noteSlashQuery() {
	key := ""
	if d := slashDraft(m.input); d != nil {
		key = d.key()
	}
	if key == m.slashQuery {
		return
	}
	m.slashQuery = key
	m.slashIndex = m.slashDefaultIndex()
}

func (m swarmTUI) slashDefaultIndex() int {
	items := m.slashItems()
	if m.choice == nil {
		return 0
	}
	want := ""
	if len(items) > 0 && items[0].parent == "model" {
		want = m.choice.CurrentModel()
	} else if len(items) > 0 && items[0].parent == "reason" {
		want = reasoningLabel(m.choice.CurrentReasoning())
	}
	if want == "" {
		return 0
	}
	for i, it := range items {
		if it.name == want {
			return i
		}
	}
	return 0
}

func (m swarmTUI) clampedSlashIndex(n int) int {
	return nextSlashIndex(m.slashIndex, n, 0)
}

func slashWindow(n, index, maxRows int) (start, end int) {
	if n <= maxRows {
		return 0, n
	}
	if maxRows < 1 {
		return 0, 0
	}
	if index < 0 {
		index = 0
	}
	if index >= n {
		index = n - 1
	}
	start = index - maxRows + 1
	if start < 0 {
		start = 0
	}
	end = start + maxRows
	if end > n {
		end = n
		start = end - maxRows
	}
	return start, end
}
