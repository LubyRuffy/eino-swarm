package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// bubbletea v1 paints a fake caret and then homes the real cursor to column 0
// of the last line. macOS IME attaches preedit to that real cursor, so a
// composing session appears at the front of the prompt. We re-park the
// hardware cursor after every flush and keep View stable while idle so a
// tick cannot wipe the preedit.

const (
	ansiShowCursor = "\x1b[?25h"
	ansiHideCursor = "\x1b[?25l"
	composerPrompt = "> "
)

type imeAnchor struct {
	mu   sync.Mutex
	row  int // 1-based CUP row; 0 means do not move
	col  int // 1-based CUP column
	show bool
}

func (a *imeAnchor) set(row, col int, show bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.row, a.col, a.show = row, col, show
	a.mu.Unlock()
}

func (a *imeAnchor) get() (row, col int, show bool) {
	if a == nil {
		return 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.row, a.col, a.show
}

// imeWriter sits under bubbletea's renderer. After each frame (which ends by
// parking the cursor at column 0) it moves and shows the hardware cursor at
// the composer insert point so IME preedit follows committed text.
type imeWriter struct {
	w        io.Writer
	f        *os.File
	anchor   *imeAnchor
	mu       sync.Mutex
	wasShown bool
}

func newIMEWriter(out io.Writer, anchor *imeAnchor) *imeWriter {
	w := &imeWriter{w: out, anchor: anchor}
	if f, ok := out.(*os.File); ok {
		w.f = f
	}
	return w
}

func (w *imeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.w.Write(p)
	if err != nil {
		return n, err
	}
	row, col, show := w.anchor.get()
	if !show || row < 1 || col < 1 {
		if w.wasShown {
			w.wasShown = false
			if _, werr := w.w.Write([]byte(ansiHideCursor)); werr != nil {
				return n, werr
			}
		}
		return n, nil
	}
	w.wasShown = true
	if _, werr := w.w.Write([]byte(cursorPosition(col, row) + ansiShowCursor)); werr != nil {
		return n, werr
	}
	return n, nil
}

func (w *imeWriter) Read(p []byte) (int, error) {
	if w.f != nil {
		return w.f.Read(p)
	}
	return 0, io.EOF
}

func (w *imeWriter) Close() error {
	// stdout outlives the TUI; bubbletea may Close the output File on exit.
	return nil
}

func (w *imeWriter) Fd() uintptr {
	if w.f != nil {
		return w.f.Fd()
	}
	return 0
}

func cursorPosition(col, row int) string {
	return fmt.Sprintf("\x1b[%d;%dH", row, col)
}

func (m swarmTUI) publishIME(view string) {
	row, col, show := m.imeCell(view)
	m.ime.set(row, col, show)
}

func (m swarmTUI) imeCell(view string) (row, col int, show bool) {
	if !m.interactive || m.busy || m.quitting {
		return 0, 0, false
	}
	lines := viewLineCount(view)
	row = composerRow(lines, m.hasSwitchBar())
	if row < 1 {
		return 0, 0, false
	}
	col = composerInsertCol(m.composerShown())
	if m.width > 0 && col > m.width {
		col = m.width
	}
	return row, col, true
}

func (m swarmTUI) hasSwitchBar() bool {
	return strings.TrimSpace(m.switchBar(m.width)) != ""
}

func (m swarmTUI) composerShown() string {
	if strings.TrimSpace(m.input) == "" {
		return ""
	}
	w := m.width
	if w == 0 {
		w = 110
	}
	return trunc(m.input, maxInt(1, w-6))
}

func composerInsertCol(shown string) int {
	// CUP is 1-based: column 1 is the first cell of the line. The prompt is
	// two cells; committed text then the insert point.
	return 1 + lipgloss.Width(composerPrompt) + lipgloss.Width(shown)
}

func composerRow(lineCount int, statusBelow bool) int {
	if lineCount < 1 {
		return 0
	}
	if statusBelow {
		if lineCount < 2 {
			return 1
		}
		return lineCount - 1
	}
	return lineCount
}

func viewLineCount(view string) int {
	if view == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(view); i++ {
		if view[i] == '\n' {
			n++
		}
	}
	return n
}
