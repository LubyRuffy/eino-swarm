//go:build darwin

package desktop

import (
	"os/exec"
	"strings"
)

func presentStartupAlert(title, message string, failure bool) {
	// The dialog is the foreground prompt. A failure to show it still leaves
	// the error on stderr for a terminal launch.
	_ = exec.Command("osascript", "-e", startupAlertScript(title, message, failure)).Run()
}

func startupAlertScript(title, message string, failure bool) string {
	kind := "warning"
	if failure {
		kind = "critical"
	}
	return "display alert " + appleQuoted(title) + " message " + appleQuoted(message) + " as " + kind
}

func appleQuoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\r':
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
