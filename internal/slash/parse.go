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
	"unicode"
	"unicode/utf8"
)

const (
	solidus          = '/'
	fullwidthSolidus = '／' // IME "／", not a different command
	ideographicComma = '、' // Slash key with CJK punctuation, not a list mark
)

// Leading reports a slash command at the start of s. A leading space is
// not a command; submit-time Split trims first.
func Leading(s string) (rest string, ok bool) {
	r, size := utf8.DecodeRuneInString(s)
	if r != solidus && r != fullwidthSolidus && r != ideographicComma {
		return "", false
	}
	return s[size:], true
}

// NormalizePrefix rewrites a leading fullwidth solidus or CJK punctuation
// comma to ASCII `/`. CJK IMEs emit those from the Slash key; the catalog
// is the ASCII command prefix.
func NormalizePrefix(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == fullwidthSolidus || r == ideographicComma {
		return string(solidus) + s[size:]
	}
	return s
}

// Split returns the command name (lowercased) and argument. The name is
// the leading ASCII identifier; anything else — space, CJK, punctuation —
// is the argument. `/goals` is not `/goal`.
func Split(line string) (name, arg string, ok bool) {
	line = trimSpace(line)
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
	return strings.ToLower(rest[:end]), trimSpace(rest[end:]), true
}

// Lookup matches leading commands and the trailing /goal objective marker.
// Aliases are the caller's job; a bare /goal still has an empty argument.
func Lookup(line string, names ...string) (arg string, ok bool) {
	name, arg, ok := Split(line)
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if ok && name == n {
			rest, _ := Leading(trimSpace(line))
			if tail := rest[len(name):]; tail == "" || !strings.ContainsRune("./\\", rune(tail[0])) {
				return arg, true
			}
		}
		if n == "goal" {
			if objective, trailing := trailingGoal(line); trailing {
				return objective, true
			}
		}
	}
	return "", false
}

// isSpace is the composer's JavaScript whitespace (`\s`, String.trim): it
// includes U+FEFF, which text pasted from a file with a byte-order mark can
// carry, and excludes U+0085. Where the two disagree, the desktop and the
// engine decide differently whether a line is a command and what its
// argument is.
func isSpace(r rune) bool {
	return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r))
}

func trimSpace(s string) string {
	return strings.TrimFunc(s, isSpace)
}

func isNameRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
