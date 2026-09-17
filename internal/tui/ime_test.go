package tui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestComposerIMECursorSitsAfterCommittedText(t *testing.T) {
	// bubbletea homes the real cursor to column 0 of the last line. IME
	// preedit follows that cursor, so a composing session used to appear
	// at the front of the prompt.
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "已有文字"
	view := m.View()
	row, col, show := m.imeCell(view)
	if !show {
		t.Fatal("an idle composer must expose a real cursor or IME has nowhere to attach")
	}
	if col <= 1 {
		t.Fatalf("IME cursor must not sit at column 0/1 (the line start), col=%d", col)
	}
	wantCol := composerInsertCol(m.composerShown())
	if col != wantCol {
		t.Fatalf("insert column=%d, want %d after the committed text", col, wantCol)
	}
	if row < 2 {
		t.Fatalf("composer row=%d is the home row, IME would paint at the top", row)
	}
}

func TestComposerIMECursorCountsWideRunesAsCells(t *testing.T) {
	narrow := composerInsertCol("ab")
	wide := composerInsertCol("中文")
	if lipgloss.Width("中文") <= len([]rune("中文")) {
		t.Fatal("this test needs CJK to be two cells wide")
	}
	if wide <= narrow {
		t.Fatalf("wide runes must move the insert point further than ASCII, ascii=%d cjk=%d", narrow, wide)
	}
}

func TestComposerIMECursorStaysOnThePromptWhenAStatusLineFollows(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.choice = &Switcher{Model: "some-model", Reasoning: "high"}
	m.input = "hello"
	view := m.View()
	row, col, show := m.imeCell(view)
	if !show {
		t.Fatal("status under the composer must not hide the IME cursor")
	}
	lines := viewLineCount(view)
	if row != lines-1 {
		t.Fatalf("cursor row=%d must be the composer (line %d), not the status (line %d)", row, lines-1, lines)
	}
	if col != composerInsertCol("hello") {
		t.Fatalf("col=%d, want after %q", col, "hello")
	}
}

func TestComposerIMECursorStaysOnThePromptWhenTheSlashMenuIsOpen(t *testing.T) {
	// The popup sits above the composer. IME still has to attach to the
	// insert point, not to the top of the menu.
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "/mo"
	view := m.View()
	row, col, show := m.imeCell(view)
	if !show {
		t.Fatal("a slash popup must not hide the IME cursor")
	}
	lines := viewLineCount(view)
	if row != lines {
		t.Fatalf("cursor row=%d must be the composer (line %d), not the menu", row, lines)
	}
	if col != composerInsertCol(m.composerShown()) {
		t.Fatalf("col=%d, want after %q", col, m.composerShown())
	}
}

func TestBusyComposerDoesNotParkTheIMECursor(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.busy = true
	m.width, m.height = 80, 24
	_, _, show := m.imeCell(m.View())
	if show {
		t.Fatal("a running turn must hide the composer cursor")
	}
}

func TestIdleTicksDoNotRepaintTheComposer(t *testing.T) {
	// bubbletea skips a flush when View is unchanged. A blink-driven paint
	// would home the cursor mid-composition and CJK IME would jump to column 0.
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.input = "已有"
	m.notifications = make(chan notificationMsg)
	first := m.View()
	for i := 0; i < 12; i++ {
		next, cmd := m.Update(tickMsg{})
		m = next.(swarmTUI)
		if cmd == nil {
			t.Fatal("ticks must keep draining notifications")
		}
	}
	if got := m.View(); got != first {
		t.Fatal("an idle tick must not rewrite the composer or IME preedit is wiped")
	}
}

func TestIMEWriterRepositionsAfterTheRendererHomesTheCursor(t *testing.T) {
	buf := &bytes.Buffer{}
	a := &imeAnchor{}
	w := newIMEWriter(buf, a)
	a.set(10, 7, true)
	if _, err := w.Write([]byte("frame\x1b[10;H")); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "frame") {
		t.Fatalf("payload missing: %q", got)
	}
	if !strings.Contains(got, cursorPosition(7, 10)) {
		t.Fatalf("expected CUP to the insert point, got %q", got)
	}
	if !strings.Contains(got, ansiShowCursor) {
		t.Fatal("IME needs a visible terminal cursor")
	}
}

func TestIMEWriterStaysPutWhenTheComposerIsIdleClosed(t *testing.T) {
	buf := &bytes.Buffer{}
	a := &imeAnchor{}
	w := newIMEWriter(buf, a)
	a.set(10, 7, false)
	if _, err := w.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "frame" {
		t.Fatalf("a hidden cursor must not inject CUP, got %q", buf.String())
	}
}

func TestIMEWriterHidesTheCursorWhenTheTurnStarts(t *testing.T) {
	buf := &bytes.Buffer{}
	a := &imeAnchor{}
	w := newIMEWriter(buf, a)
	a.set(10, 7, true)
	if _, err := w.Write([]byte("idle")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	a.set(0, 0, false)
	if _, err := w.Write([]byte("run")); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "run") || !strings.Contains(got, ansiHideCursor) {
		t.Fatalf("starting a turn must hide the leftover composer cursor, got %q", got)
	}
	if strings.Contains(got, cursorPosition(7, 10)) {
		t.Fatal("a running turn must not keep IME parked on the prompt")
	}
}

func TestViewPublishesTheIMEAnchor(t *testing.T) {
	a := &imeAnchor{}
	m := newModel(nil)
	m.interactive = true
	m.ime = a
	m.width, m.height = 80, 24
	m.input = "hello"
	_ = m.View()
	row, col, show := a.get()
	if !show || row < 2 || col != composerInsertCol("hello") {
		t.Fatalf("View must publish the insert cell, row=%d col=%d show=%v", row, col, show)
	}
}

func TestComposerHasNoPaintedBlockStealingTheInsertCell(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.input = "hello"
	got := m.composer(80)
	if strings.Contains(got, "█") {
		t.Fatalf("a painted block sits on the insert cell and IME lands in front of it: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("typed text missing: %q", got)
	}
}

func TestEmptyComposerKeepsTheHintAfterThePrompt(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	got := m.composer(80)
	iPrompt := strings.Index(got, composerPrompt)
	iHint := strings.Index(got, "type a task")
	if iPrompt < 0 || iHint < 0 || iHint < iPrompt {
		t.Fatalf("empty composer must keep the hint after the prompt, got %q", got)
	}
	if composerInsertCol("") != 1+lipgloss.Width(composerPrompt) {
		t.Fatal("empty insert point must be the cell after the prompt")
	}
}

func TestComposerRowEdges(t *testing.T) {
	if composerRow(0, false) != 0 {
		t.Fatal("no view means no cursor")
	}
	if composerRow(1, true) != 1 {
		t.Fatal("a single line is the composer even when a status was expected")
	}
	if composerRow(5, true) != 4 {
		t.Fatal("status sits on the last line, the prompt on the one above")
	}
}

func TestIMEAnchorNilIsANoop(t *testing.T) {
	var a *imeAnchor
	a.set(1, 1, true)
	row, col, show := a.get()
	if show || row != 0 || col != 0 {
		t.Fatal("a missing anchor must not invent a cursor")
	}
}

func TestIMEWriterFileMethodsDoNotCloseStdout(t *testing.T) {
	w := newIMEWriter(&bytes.Buffer{}, &imeAnchor{})
	if _, err := w.Read(nil); err != io.EOF {
		t.Fatalf("read=%v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Fd() != 0 {
		t.Fatalf("fd=%d", w.Fd())
	}
}

func TestQuittingHidesIME(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.quitting = true
	_, _, show := m.imeCell(">")
	if show {
		t.Fatal("leaving the TUI must not leave IME parked on a dying prompt")
	}
}

func TestInsertColCapsToTheWindow(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width = 3
	m.input = "中"
	_, col, show := m.imeCell(">")
	if !show || col != 3 {
		t.Fatalf("CUP past the line width wraps and IME jumps, col=%d show=%v", col, show)
	}
}

func TestViewLineCountEmpty(t *testing.T) {
	if viewLineCount("") != 0 || viewLineCount("a\nb") != 2 {
		t.Fatal("view line count")
	}
}

func TestIMEWriterSurfacesAWriteError(t *testing.T) {
	w := newIMEWriter(errWriter{}, &imeAnchor{})
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("a dead output must surface")
	}
}

func TestIMEWriterSurfacesACursorWriteError(t *testing.T) {
	a := &imeAnchor{}
	a.set(2, 4, true)
	w := newIMEWriter(&nthFail{fail: 2}, a)
	if _, err := w.Write([]byte("frame")); err == nil {
		t.Fatal("a failed CUP must surface or IME stays at column 0")
	}
}

func TestIMEWriterSurfacesAHideWriteError(t *testing.T) {
	a := &imeAnchor{}
	a.set(2, 4, true)
	w := newIMEWriter(&nthFail{fail: 4}, a)
	if _, err := w.Write([]byte("idle")); err != nil {
		t.Fatal(err)
	}
	a.set(0, 0, false)
	if _, err := w.Write([]byte("run")); err == nil {
		t.Fatal("a failed hide must surface")
	}
}

func TestIMEWriterFdFollowsTheFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "ime")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	w := newIMEWriter(f, &imeAnchor{})
	if w.Fd() != f.Fd() {
		t.Fatal("bubbletea needs the real stdout fd or it thinks there is no TTY")
	}
	if _, err := w.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read=%v", err)
	}
}

func TestEmptyViewDoesNotParkIME(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	_, _, show := m.imeCell("")
	if show {
		t.Fatal("no frame means no cursor")
	}
}

func TestInteractiveInitShowsTheTerminalCursor(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	if m.Init() == nil {
		t.Fatal("interactive init must tick and un-hide the cursor")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type nthFail struct {
	n, fail int
}

func (w *nthFail) Write(p []byte) (int, error) {
	w.n++
	if w.n == w.fail {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
