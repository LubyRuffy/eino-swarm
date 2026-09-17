package tui

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/charmbracelet/lipgloss"
)

func TestSlashDraftKeepsTheFirstTokenWhenArgsFollow(t *testing.T) {
	if slashDraft("") != nil || slashDraft("hello") != nil {
		t.Fatal("plain text is not a slash draft")
	}
	if d := slashDraft("/"); d == nil || d.token != "" || d.hasSpace {
		t.Fatalf("bare slash=%+v", d)
	}
	if d := slashDraft("/mo"); d == nil || d.token != "mo" || d.hasSpace {
		t.Fatalf("partial=%+v", d)
	}
	if d := slashDraft("/model "); d == nil || d.token != "model" || !d.hasSpace {
		t.Fatalf("Codex keeps the popup after a space, got %+v", d)
	}
	if d := slashDraft("/goal keep going"); d != nil {
		t.Fatal("inline /goal args close the menu, like the desktop composer")
	}
	if items, ok := (swarmTUI{}).slashPickerItems(nil); ok || items != nil {
		t.Fatal("a nil draft is not a picker")
	}
}

func TestFilterSlashCommandsHidesAliasesUntilTyped(t *testing.T) {
	all := namesOf(filterSlashCommands(""))
	if strings.Join(all, ",") != "goal,model,reason,clear,help,exit" {
		t.Fatalf("visible catalog=%v", all)
	}
	if got := namesOf(filterSlashCommands("mo")); len(got) != 1 || got[0] != "model" {
		t.Fatalf("prefix mo=%v", got)
	}
	if got := namesOf(filterSlashCommands("think")); len(got) != 1 || got[0] != "reason" {
		t.Fatalf("alias think=%v", got)
	}
	if got := namesOf(filterSlashCommands("q")); len(got) != 1 || got[0] != "quit" {
		t.Fatalf("hidden quit appears for a prefix, got %v", got)
	}
	if got := filterSlashCommands("nope"); len(got) != 0 {
		t.Fatalf("unknown query must not invent a command, got %v", namesOf(got))
	}
	if got := filterSlashCommands("keep"); len(got) != 0 {
		t.Fatal("prefix filter must not search descriptions")
	}
	if got := filterSlashCommands("list"); len(got) != 0 {
		t.Fatal("prefix filter must not search /help's description either")
	}
}

func TestParseTUICommandOnlyInterceptsTheCatalog(t *testing.T) {
	name, arg, ok := parseTUICommand("  /model  beta  ")
	if !ok || name != "model" || arg != "beta" {
		t.Fatalf("got %q %q ok=%v", name, arg, ok)
	}
	if _, _, ok = parseTUICommand("/think"); !ok {
		t.Fatal("think is an alias for reason")
	}
	if _, _, ok = parseTUICommand("/clear"); !ok {
		t.Fatal("/clear is a local command")
	}
	if _, _, ok = parseTUICommand("/help"); !ok {
		t.Fatal("/help is a local command")
	}
	if _, _, ok = parseTUICommand("/exit"); !ok {
		t.Fatal("/exit is a local command")
	}
	name, arg, ok = parseTUICommand("/goal keep going")
	if !ok || name != "goal" || arg != "keep going" {
		t.Fatalf("/goal is Codex Goal, got %q %q ok=%v", name, arg, ok)
	}
	name, arg, ok = parseTUICommand("/goal持续推进")
	if !ok || name != "goal" || arg != "持续推进" {
		t.Fatalf("glued CJK is still /goal, got %q %q ok=%v", name, arg, ok)
	}
	if _, _, ok = parseTUICommand("/nope keep going"); ok {
		t.Fatal("unknown slash commands must reach the swarm as a task")
	}
	if _, _, ok = parseTUICommand("/"); ok {
		t.Fatal("a lone slash is not a command")
	}
}

func TestNextSlashIndexWrapsLikeCodex(t *testing.T) {
	if nextSlashIndex(0, 3, 1) != 1 || nextSlashIndex(2, 3, 1) != 0 {
		t.Fatal("down must wrap")
	}
	if nextSlashIndex(0, 3, -1) != 2 {
		t.Fatal("up must wrap")
	}
	if nextSlashIndex(3, 0, 1) != 0 {
		t.Fatal("an empty menu has nowhere to move")
	}
}

func TestSlashWindowKeepsTheSelectionVisible(t *testing.T) {
	start, end := slashWindow(20, 0, 8)
	if start != 0 || end != 8 {
		t.Fatalf("top window=%d:%d", start, end)
	}
	start, end = slashWindow(20, 12, 8)
	if start != 5 || end != 13 {
		t.Fatalf("scrolled window=%d:%d", start, end)
	}
	start, end = slashWindow(3, 1, 8)
	if start != 0 || end != 3 {
		t.Fatalf("short list=%d:%d", start, end)
	}
	start, end = slashWindow(10, 0, 0)
	if start != 0 || end != 0 {
		t.Fatalf("no rows=%d:%d", start, end)
	}
}

func TestSlashMenuAppearsAboveTheComposer(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "/"
	view := m.View()
	for _, name := range []string{"/goal", "/model", "/reason", "/clear", "/help", "/exit"} {
		if !strings.Contains(view, name) {
			t.Fatalf("missing %s:\n%s", name, view)
		}
	}
	if !strings.Contains(view, "choose what model") {
		t.Fatalf("name + description missing:\n%s", view)
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╮") {
		t.Fatalf("the popup needs a rounded surface, like the desktop slash card:\n%s", view)
	}
	if !strings.Contains(view, slashInset+"/model") {
		t.Fatal("rows must be inset two cells, like Codex command_popup")
	}
	modelAt := strings.Index(view, "/model")
	promptAt := strings.LastIndex(view, composerPrompt)
	if modelAt < 0 || promptAt < 0 || modelAt > promptAt {
		t.Fatal("the menu sits above the composer, not below it")
	}
}

func TestSlashMenuDoesNotInventDesktopOrSampleNames(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "/"
	view := m.View()
	for _, leaked := range []string{"/compact", "/goglab", "/gogogo", "/quit"} {
		if strings.Contains(view, leaked) {
			t.Fatalf("bare slash leaked %q:\n%s", leaked, view)
		}
	}
}

func TestSlashMenuFiltersAsTheDraftGrows(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "/mo"
	view := m.View()
	if !strings.Contains(view, "/model") {
		t.Fatalf("missing model:\n%s", view)
	}
	if strings.Contains(view, "/reason") || strings.Contains(view, "/clear") {
		t.Fatalf("/mo must not still list the rest:\n%s", view)
	}
}

func TestSlashTabCompletesWithoutSending(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	m.choice = &Switcher{Model: "alpha", Catalog: []string{"alpha", "beta"}}
	m = typeKeys(t, m, "/mo")
	m = press(t, m, "tab")
	if m.input != "/model " {
		t.Fatalf("tab should fill the highlighted command, input=%q", m.input)
	}
	if m.busy {
		t.Fatal("tab must not start a turn")
	}
	select {
	case got := <-prompts:
		t.Fatalf("tab leaked a prompt %q", got)
	default:
	}
}

func TestSlashEnterOpensTheModelPicker(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.choice = &Switcher{Model: "alpha", Catalog: []string{"alpha", "beta"}}
	m.width, m.height = 80, 24
	m = typeKeys(t, m, "/mo")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if cmd != nil || m.busy {
		t.Fatal("enter on /model must open the picker, not start a turn")
	}
	if m.input != "/model " {
		t.Fatalf("picker input=%q", m.input)
	}
	view := m.View()
	if !strings.Contains(view, "/alpha") || !strings.Contains(view, "/beta") {
		t.Fatalf("model picker missing catalog names:\n%s", view)
	}
	if strings.Contains(view, "/reason") {
		t.Fatalf("command list leaked into the model picker:\n%s", view)
	}
	m = press(t, m, "down")
	m = press(t, m, "enter")
	if m.choice.CurrentModel() != "beta" {
		t.Fatalf("picking the second catalog name, got %q", m.choice.CurrentModel())
	}
}

func TestSlashUpDownMovesTheHighlightThenTabCompletesIt(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m = typeKeys(t, m, "/")
	m = press(t, m, "down")
	m = press(t, m, "tab")
	if m.input != "/model " {
		t.Fatalf("down then tab should complete the second row, input=%q", m.input)
	}
}

func TestSlashEscCancelsTheDraft(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m = typeKeys(t, m, "/mo")
	m = press(t, m, "esc")
	if m.input != "" {
		t.Fatalf("esc should drop the slash draft, input=%q", m.input)
	}
	if strings.Contains(m.View(), "choose what model") {
		t.Fatal("cancelling the draft must close the menu")
	}
}

func TestSlashDoesNotStealTabWhenTheComposerIsPlainText(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	feed(&m, swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "a"})
	m = press(t, m, "tab")
	if m.selected != 0 {
		t.Fatalf("tab with no slash draft still walks the roster, selected=%d", m.selected)
	}
}

func TestBusyComposerHidesTheSlashMenu(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.busy = true
	m.input = "/"
	m.width, m.height = 80, 24
	if strings.Contains(m.View(), "choose what model") {
		t.Fatal("a running turn must not keep the slash menu open")
	}
}

func TestSlashClearEmptiesTheTranscript(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "a"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "hello"},
	)
	m = typeKeys(t, m, "/clear")
	m = press(t, m, "enter")
	if len(m.manager.blocks) != 0 || len(m.agents) != 0 {
		t.Fatal("/clear must wipe the visible transcript")
	}
	if m.busy || m.notice != "cleared" {
		t.Fatalf("clear notice=%q busy=%v", m.notice, m.busy)
	}
}

func TestSlashQuitLeavesTheSession(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m = typeKeys(t, m, "/quit")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if !m.quitting || cmd == nil {
		t.Fatalf("quit quitting=%v cmd=%v", m.quitting, cmd)
	}
}

func TestSlashNoMatchesDoesNotInventARow(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "/zzzz"
	if !strings.Contains(m.View(), "no matches") {
		t.Fatalf("empty filter must say so:\n%s", m.View())
	}
}

func TestSlashModelWithoutANameAsksYouToPick(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.choice = &Switcher{}
	m.input = "/model "
	m = press(t, m, "enter")
	if m.notice != "choose a model" {
		t.Fatalf("empty catalog notice=%q", m.notice)
	}
}

func TestSlashPaintAndIndexEdgesStaySane(t *testing.T) {
	if padRight("x", 0) != "" {
		t.Fatal("a zero-width column is empty")
	}
	if padRight("toolong", 3) != "to…" {
		t.Fatalf("padRight must truncate, got %q", padRight("toolong", 3))
	}
	if (*slashDraftQuery)(nil).key() != "" {
		t.Fatal("a nil draft has no filter key")
	}
	if nextSlashIndex(-1, 3, 0) != 2 {
		t.Fatal("a wrapped index must land on the last row")
	}
	start, end := slashWindow(20, 20, 8)
	if start != 12 || end != 20 {
		t.Fatalf("clamped bottom window=%d:%d", start, end)
	}
	start, end = slashWindow(20, -1, 8)
	if start != 0 || end != 8 {
		t.Fatalf("negative index must pin to the top, got %d:%d", start, end)
	}

	row := paintSlashRow(slashCommand{name: "model", description: "choose what model to use", hint: "a-very-long-catalog-name"}, false, 8, "mo", 12)
	if strings.TrimSpace(row) == "" {
		t.Fatal("a cramped row must still paint something")
	}
	_ = paintSlashRow(slashCommand{name: "model", description: "choose"}, true, 8, "mo", 20)

	m := newModel(nil)
	m.input = "/"
	if m.slashItems() != nil {
		t.Fatal("one-shot tui has no slash menu")
	}
	m.interactive = true
	m.input = "/clear "
	items, ok := m.slashPickerItems(slashDraft(m.input))
	if ok || items != nil {
		t.Fatalf("a freeform command with a trailing space submits, ok=%v items=%v", ok, namesOf(items))
	}

	m.choice = &Switcher{Model: "alpha", Catalog: []string{"alpha"}}
	if got := m.applyCommand("nope", ""); got != "" {
		t.Fatalf("unknown command notice=%q", got)
	}
	if m.applyCommand("exit", "") != "" || !m.quitting {
		t.Fatal("/exit through applyCommand must quit")
	}

	m = newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m = typeKeys(t, m, "/")
	selected := m.selected
	m = press(t, m, "left")
	if m.selected != selected || m.input != "/" {
		t.Fatal("arrow keys must not steal the roster while the slash popup is open")
	}
	m.input = "/zzzz"
	m = press(t, m, "tab")
	if m.selected != selected {
		t.Fatal("tab with no matches is not roster navigation")
	}
	next, cmd := m.pickSlash(slashCommand{name: "nope"}, true)
	m = next.(swarmTUI)
	if cmd == nil || !m.busy {
		t.Fatal("an unknown catalog row still submits as a task")
	}
}

func TestSlashHelpListsTheCatalogInTheTranscript(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.width, m.height = 80, 24
	m = typeKeys(t, m, "/help")
	m = press(t, m, "enter")
	if m.busy || m.input != "" {
		t.Fatalf("help must not start a turn, busy=%v input=%q", m.busy, m.input)
	}
	view := m.View()
	if !strings.Contains(view, "/model") || !strings.Contains(view, "/exit") || !strings.Contains(view, "/goal") {
		t.Fatalf("help missing from the transcript:\n%s", view)
	}
	for _, leaked := range []string{"/compact", "/goglab"} {
		if strings.Contains(slashHelpText(), leaked) {
			t.Fatalf("help catalog leaked %q", leaked)
		}
	}
}

func TestSlashHintShowsTheCurrentModelOnThatRow(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.choice = &Switcher{Model: "alpha", Catalog: []string{"alpha", "beta"}}
	m.input = "/"
	found := false
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "/model") && strings.Contains(line, "choose") && strings.Contains(line, "alpha") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the /model row should carry the current catalog name as a hint:\n%s", m.View())
	}
}

func TestSlashNameColumnComesFromEveryRow(t *testing.T) {
	items := []slashCommand{{name: "a"}, {name: "longname"}}
	got := slashNameColumnWidth(items, 80)
	if got != lipgloss.Width("/longname") {
		t.Fatalf("name column=%d, want the longest name so scrolling stays still", got)
	}
	if slashNameColumnWidth(items, 8) > 8 {
		t.Fatal("a tiny terminal must still cap the name column")
	}
}

func TestSlashDefaultIndexLandsOnTheCurrentPickerValue(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.choice = &Switcher{Model: "beta", Catalog: []string{"alpha", "beta"}}
	m.input = "/model "
	m.noteSlashQuery()
	if m.slashIndex != 1 {
		t.Fatalf("picker should start on the current model, index=%d", m.slashIndex)
	}
}

func TestUnknownSlashIsSentAsATask(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	m = typeKeys(t, m, "/nope keep going")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if cmd == nil || !m.busy {
		t.Fatal("unknown slash commands must reach the swarm as a task")
	}
	cmd()
	got := <-prompts
	if got != "/nope keep going" {
		t.Fatalf("prompt=%q", got)
	}
}

func TestGoalSlashSendsTheObjectiveNotTheSlashLine(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	m = typeKeys(t, m, "/goal keep going")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if cmd == nil || !m.busy {
		t.Fatal("/goal with an argument must start the turn")
	}
	cmd()
	got := <-prompts
	if got != "keep going" {
		t.Fatalf("the model must see the objective, not the slash line, prompt=%q", got)
	}
}

func TestGoalSlashGluedCJKIsStillTheCommand(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	m = typeKeys(t, m, "/goal持续推进")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if cmd == nil || !m.busy {
		t.Fatal("a glued CJK objective must not fall through as a user task")
	}
	cmd()
	got := <-prompts
	if got != "持续推进" {
		t.Fatalf("prompt=%q", got)
	}
}

func TestSlashMenuDoesNotExceedTheTerminalWidth(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.choice = &Switcher{Model: "alpha", Catalog: []string{"alpha"}}
	m.input = "/"
	for i, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d cells:\n%s", i, lipgloss.Width(line), line)
		}
	}
}

func TestPaintSlashNameBoldsTheMatchedPrefix(t *testing.T) {
	if !strings.Contains(paintSlashName("/model", "mo", false), "model") {
		t.Fatal("unselected name must still be readable")
	}
	if !strings.Contains(paintSlashName("/model", "mo", true), "model") {
		t.Fatal("selected name must still be readable")
	}
	if !strings.Contains(paintSlashName("/model", "", false), "/model") {
		t.Fatal("no query still prints the command")
	}
	if !strings.Contains(paintSlashName("/model", "zzz", false), "/model") {
		t.Fatal("a miss still prints the command")
	}
}

func namesOf(cmds []slashCommand) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.name
	}
	return out
}
