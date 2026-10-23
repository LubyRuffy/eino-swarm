package slash

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// trailingGoal accepts a command marker after its objective. Literal command
// examples in paths, quotations, Markdown quotes and code remain ordinary text.
func trailingGoal(line string) (string, bool) {
	line = strings.TrimSpace(line)
	start := strings.LastIndexAny(line, "/／、")
	if start <= 0 {
		return "", false
	}
	_, size := utf8.DecodeRuneInString(line[start:])
	if !strings.EqualFold(line[start+size:], "goal") {
		return "", false
	}
	before, _ := utf8.DecodeLastRuneInString(line[:start])
	if !unicode.IsSpace(before) && (isNameRune(before) || strings.ContainsRune("/:.\\([]", before)) {
		return "", false
	}
	if literalCommandPosition(line, start) {
		return "", false
	}
	return strings.TrimSpace(line[:start]), true
}

func literalCommandPosition(line string, pos int) bool {
	lineStart := strings.LastIndexByte(line[:pos], '\n') + 1
	if strings.HasPrefix(strings.TrimSpace(line[lineStart:pos]), ">") {
		return true
	}
	var quote rune
	var code rune
	width := 0
	previous := rune(0)
	for i := 0; i < pos; {
		r, n := utf8.DecodeRuneInString(line[i:])
		i += n
		if r == '\\' {
			_, n = utf8.DecodeRuneInString(line[i:])
			i += n
			continue
		}
		if code != 0 {
			if r == code {
				count := 1
				for i < pos {
					next, size := utf8.DecodeRuneInString(line[i:])
					if next != r {
						break
					}
					i += size
					count++
				}
				if count == width {
					code = 0
				}
			}
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '`' || r == '~' {
			count := 1
			for i < pos {
				next, size := utf8.DecodeRuneInString(line[i:])
				if next != r {
					break
				}
				i += size
				count++
			}
			if r == '`' || count >= 3 {
				code, width = r, count
			}
		} else {
			switch r {
			case '"':
				quote = r
			case '\'':
				if !unicode.IsLetter(previous) && !unicode.IsDigit(previous) {
					quote = r
				}
			case '“':
				quote = '”'
			case '‘':
				quote = '’'
			case '「':
				quote = '」'
			case '『':
				quote = '』'
			}
		}
		previous = r
	}
	return quote != 0 || code != 0
}
