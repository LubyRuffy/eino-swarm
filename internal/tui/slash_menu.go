package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Codex command_popup + selection_popup_common: 8 visible rows, 2-cell inset,
// name column from every row so scrolling does not shove the description
// around, selected row in the accent color, unmatched descriptions dim,
// empty state "no matches". The rounded surface is the desktop slash card
// (Claude/Codex both sit the list on a panel above the composer).
const (
	slashMaxRows = 8
	slashInset   = "  "
	slashGap     = 2
)

var (
	cSlashName   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cSlashDesc   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	cSlashHint   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cSlashSel    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Background(lipgloss.Color("238"))
	cSlashEmpty  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("243"))
	slashSurface = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)
)

func (m swarmTUI) slashMenu(w int) string {
	d := slashDraft(m.input)
	if d == nil {
		return ""
	}
	inner := slashInnerWidth(w)
	body := m.paintSlashBody(m.slashItems(), d, inner)
	return slashSurface.Width(inner).Render(body)
}

func slashInnerWidth(w int) int {
	// Width is content+padding; the rounded border sits outside it.
	return maxInt(1, w-2)
}

func slashContentWidth(inner int) int {
	return maxInt(1, inner-2)
}

func (m swarmTUI) paintSlashBody(items []slashCommand, d *slashDraftQuery, inner int) string {
	cw := slashContentWidth(inner)
	if len(items) == 0 {
		return cSlashEmpty.Render("no matches")
	}
	idx := m.clampedSlashIndex(len(items))
	start, end := slashWindow(len(items), idx, slashMaxRows)
	nameW := slashNameColumnWidth(items, cw)
	query := strings.ToLower(d.token)
	if items[0].parent != "" {
		query = strings.ToLower(d.args)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(paintSlashRow(items[i], i == idx, nameW, query, cw))
	}
	return b.String()
}

func slashNameColumnWidth(items []slashCommand, w int) int {
	nameW := 0
	for _, c := range items {
		if n := lipgloss.Width("/" + c.name); n > nameW {
			nameW = n
		}
	}
	// Codex caps the label column at 70% so the description still has room.
	capW := maxInt(1, w*7/10)
	if nameW+slashGap > capW {
		nameW = maxInt(1, capW-slashGap)
	}
	return nameW
}

func paintSlashRow(c slashCommand, selected bool, nameW int, query string, w int) string {
	label := "/" + c.name
	name := padRight(label, nameW)
	rest := w - len(slashInset) - nameW - slashGap
	if rest < 1 {
		rest = 1
	}
	hint := strings.TrimSpace(c.hint)
	hintW := 0
	if hint != "" {
		hintW = lipgloss.Width(hint) + 1
		if hintW >= rest {
			hint = trunc(hint, maxInt(1, rest-1))
			hintW = lipgloss.Width(hint) + 1
		}
	}
	descW := maxInt(1, rest-hintW)
	desc := trunc(c.description, descW)
	if selected {
		row := slashInset + name + strings.Repeat(" ", slashGap) + padRight(desc, descW)
		if hint != "" {
			row += " " + hint
		}
		return cSlashSel.MaxWidth(w).Width(w).Render(row)
	}
	descStyle := cSlashDesc.Render(padRight(desc, descW))
	hintStyle := ""
	if hint != "" {
		hintStyle = " " + cSlashHint.Render(hint)
	}
	row := slashInset + paintSlashName(name, query, false) + strings.Repeat(" ", slashGap) + descStyle + hintStyle
	return lipgloss.NewStyle().MaxWidth(w).Render(row)
}

func padRight(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) > w {
		return trunc(s, w)
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

func paintSlashName(label, query string, selected bool) string {
	style := cSlashName
	if selected {
		style = cSlashSel
	}
	trimmed := strings.TrimRight(label, " ")
	if query == "" || !strings.HasPrefix(strings.ToLower(strings.TrimPrefix(trimmed, "/")), query) {
		return style.Render(label)
	}
	// Bold the matched prefix after the slash, the way Codex highlights
	// filter indices shifted by one for the leading '/'.
	runes := []rune(trimmed)
	n := 1 + len([]rune(query))
	if n > len(runes) {
		n = len(runes)
	}
	matched := style.Bold(true).Render(string(runes[:n]))
	rest := ""
	if n < len(runes) {
		rest = style.Render(string(runes[n:]))
	}
	pad := strings.Repeat(" ", lipgloss.Width(label)-lipgloss.Width(trimmed))
	return matched + rest + pad
}
