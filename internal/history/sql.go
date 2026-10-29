package history

import (
	"fmt"
	"strings"
)

// This scanner does not attempt to parse SQL. SQLite parses the SELECT
// subquery. It only prevents prepare-tail statements and explicit schema
// qualification from bypassing the trusted TEMP views.
func selectSQL(input string) (string, error) {
	if strings.ContainsRune(input, 0) {
		return "", fmt.Errorf("history SQL contains NUL")
	}
	tokens := []string{}
	semicolon := -1
	for i := 0; i < len(input); {
		ch := input[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' {
			i++
			continue
		}
		if ch == '-' && i+1 < len(input) && input[i+1] == '-' {
			for i < len(input) && input[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '*' {
			end := strings.Index(input[i+2:], "*/")
			if end < 0 {
				return "", fmt.Errorf("unterminated SQL comment")
			}
			i += end + 4
			continue
		}
		if semicolon >= 0 {
			return "", fmt.Errorf("history accepts one SELECT statement")
		}
		if ch == ';' {
			semicolon = i
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' || ch == '[' {
			close := ch
			if ch == '[' {
				close = ']'
			}
			var token strings.Builder
			i++
			ended := false
			for i < len(input) {
				if input[i] == close {
					if close != ']' && i+1 < len(input) && input[i+1] == close {
						token.WriteByte(close)
						i += 2
						continue
					}
					i++
					ended = true
					break
				}
				token.WriteByte(input[i])
				i++
			}
			if !ended {
				return "", fmt.Errorf("unterminated SQL quote")
			}
			tokens = append(tokens, strings.ToLower(token.String()))
			continue
		}
		start := i
		if identifier(ch) {
			for i < len(input) && identifier(input[i]) {
				i++
			}
		} else {
			i++
		}
		tokens = append(tokens, strings.ToLower(input[start:i]))
	}
	for i := 0; i+1 < len(tokens); i++ {
		if (tokens[i] == "main" || tokens[i] == "temp") && tokens[i+1] == "." {
			return "", fmt.Errorf("use unqualified history table names; explicit schemas are unavailable")
		}
	}
	if len(tokens) == 0 {
		return "", nil
	}
	if semicolon >= 0 {
		input = input[:semicolon] + input[semicolon+1:]
	}
	return strings.TrimSpace(input), nil
}

func identifier(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '$' || ch >= 128
}
