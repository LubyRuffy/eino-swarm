// Package slash parses composer commands the way Codex dispatches them,
// with one extra split Codex still gets wrong for CJK.
//
// Codex `parse_slash_name` cuts the name at the first whitespace. Typing
// `/goal` then an IME objective with no ASCII space makes the whole line
// the "name", lookup fails, and the line is sent as a user task. Cursor's
// `/goal <objective>` skill is the same contract: the command is the
// identifier, the rest is the objective — space optional.
package slash

import (
	"strings"
	"unicode/utf8"
)

const (
	solidus          = '/'
	fullwidthSolidus = '／' // IME "／", not a different command
)

// Leading reports a slash command at the start of s. A leading space is
// not a command; submit-time Split trims first.
func Leading(s string) (rest string, ok bool) {
	r, size := utf8.DecodeRuneInString(s)
	if r != solidus && r != fullwidthSolidus {
		return "", false
	}
	return s[size:], true
}

// Split returns the command name (lowercased) and argument. The name is
// the leading ASCII identifier; anything else — space, CJK, punctuation —
// is the argument. `/goals` is not `/goal`.
func Split(line string) (name, arg string, ok bool) {
	line = strings.TrimSpace(line)
	rest, ok := Leading(line)
	if !ok {
		return "", "", false
	}
	end := 0
	for _, r := range rest {
		if !isNameRune(r) {
			break
		}
		end += utf8.RuneLen(r)
	}
	if end == 0 {
		return "", "", false
	}
	return strings.ToLower(rest[:end]), strings.TrimSpace(rest[end:]), true
}

// Lookup is Split plus an exact name match (aliases are the caller's job).
func Lookup(line string, names ...string) (arg string, ok bool) {
	name, arg, ok := Split(line)
	if !ok {
		return "", false
	}
	for _, n := range names {
		if name == strings.ToLower(strings.TrimSpace(n)) {
			return arg, true
		}
	}
	return "", false
}

func isNameRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
