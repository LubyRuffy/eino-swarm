package clients

import (
	"bytes"
	"os"
)

// A page is whole jsonl lines. The cursor is the byte offset of a line, so
// cutting inside a line would drop the rest of that line on the next page.
// The window grows from the live end: a fixed head-plus-tail slice leaves
// a hole, and the notice that used to admit the hole was not a page.

type marked struct {
	at int64
	n  int
	e  Entry
}

func pageEntries(f *os.File, size int64, tool string, before int64, limit int) ([]Entry, bool, int64) {
	opening, bodyStart := scanOpening(f, size, tool)
	// -1 means the file never left the opening (or never had a line).
	// The body starts at EOF, so the tail page is just that request.
	if bodyStart < 0 {
		bodyStart = size
	}
	shown, leadEnd := clipOpening(opening, bodyStart, limit)
	budget := limit - len(shown)
	if budget < 1 {
		budget = 1
	}
	end := size
	if before > 0 && before < end {
		end = before
	}
	if end <= leadEnd {
		return shown, false, 0
	}
	got, covered := scanBody(f, tool, leadEnd, end, size, budget)
	trimmed := keepTail(got, budget)
	older := len(trimmed) < len(got) || !covered
	var cursor int64
	if older && len(trimmed) > 0 {
		cursor = trimmed[0].at
	} else {
		older = false
	}
	out := make([]Entry, 0, len(shown)+len(trimmed))
	out = append(out, shown...)
	for _, m := range trimmed {
		e := m.e
		e.At = m.at
		e.N = m.n
		out = append(out, e)
	}
	return out, older, cursor
}

func clipOpening(opening []Entry, bodyStart int64, limit int) (shown []Entry, leadEnd int64) {
	leadEnd = bodyStart
	if len(opening) == 0 {
		return nil, leadEnd
	}
	// The pinned request is one line. A longer run of user lines is the
	// next page, otherwise a huge request would hide the live edge.
	maxOpen := limit - 1
	if maxOpen < 1 {
		maxOpen = 1
	}
	if len(opening) <= maxOpen {
		return append([]Entry{}, opening...), leadEnd
	}
	return append([]Entry{}, opening[:maxOpen]...), opening[maxOpen].At
}

func scanOpening(f *os.File, size int64, tool string) (users []Entry, bodyStart int64) {
	if size == 0 {
		return nil, 0
	}
	bodyStart = -1
	window := int64(readBytes)
	for {
		n := window
		if n > size {
			n = size
		}
		buf := readAt(f, 0, n)
		users = nil
		bodyStart = -1
		walkLines(buf, 0, false, n >= size, func(off int64, raw []byte) {
			if bodyStart >= 0 {
				return
			}
			ents := visibleEntries(tool, raw)
			if len(ents) == 0 {
				return
			}
			for _, e := range ents {
				if e.Role != "user" {
					bodyStart = off
					return
				}
			}
			for i, e := range ents {
				e.At = off
				e.N = i
				users = append(users, e)
			}
		})
		if bodyStart >= 0 || n >= size || window >= maxPageWindow {
			break
		}
		window *= 2
	}
	return users, bodyStart
}

func scanBody(f *os.File, tool string, leadEnd, end, size int64, budget int) ([]marked, bool) {
	if end <= leadEnd {
		return nil, true
	}
	window := int64(readBytes)
	var got []marked
	covered := false
	for {
		start := end - window
		if start < leadEnd {
			start = leadEnd
		}
		dropHead := start > leadEnd
		buf := readAt(f, start, end-start)
		got = parseMarked(tool, buf, start, dropHead, end >= size, leadEnd, end)
		covered = start <= leadEnd
		if len(got) >= budget || covered || window >= maxPageWindow {
			break
		}
		window *= 2
	}
	return got, covered
}

func keepTail(got []marked, budget int) []marked {
	if len(got) == 0 {
		return nil
	}
	if len(got) <= budget {
		return got
	}
	start := len(got) - budget
	at := got[start].at
	for start > 0 && got[start-1].at == at {
		start--
	}
	return got[start:]
}

func parseMarked(tool string, buf []byte, base int64, dropHead, atEOF bool, minAt, maxAt int64) []marked {
	var out []marked
	walkLines(buf, base, dropHead, atEOF, func(off int64, raw []byte) {
		if off < minAt || off >= maxAt {
			return
		}
		ents := visibleEntries(tool, raw)
		for i, e := range ents {
			out = append(out, marked{at: off, n: i, e: e})
		}
	})
	return out
}

func visibleEntries(tool string, raw []byte) []Entry {
	line := bytes.TrimSpace(raw)
	if len(line) == 0 || line[0] != '{' || len(line) > 2<<20 {
		return nil
	}
	switch tool {
	case ToolCodex:
		return codexEntries(line)
	case ToolCursor:
		return cursorEntries(line)
	default:
		return claudeEntries(line)
	}
}

func walkLines(buf []byte, base int64, dropHead, atEOF bool, fn func(off int64, line []byte)) {
	if dropHead {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return
		}
		base += int64(i + 1)
		buf = buf[i+1:]
	}
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			if atEOF {
				fn(base, buf)
			}
			return
		}
		fn(base, buf[:i])
		base += int64(i + 1)
		buf = buf[i+1:]
	}
}

func readAt(f *os.File, off, n int64) []byte {
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	read, err := f.ReadAt(buf, off)
	if read == 0 && err != nil {
		return nil
	}
	return buf[:read]
}
