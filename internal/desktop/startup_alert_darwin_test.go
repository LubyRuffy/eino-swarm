//go:build darwin

package desktop

import (
	"strings"
	"testing"
)

func TestStartupAlertQuotingKeepsTheMessageInsideTheDialog(t *testing.T) {
	quoted := appleQuoted("say \"hello\" \\ now\r")
	if quoted != `"say \"hello\" \\ now"` {
		t.Fatalf("quote = %s", quoted)
	}
	script := startupAlertScript("zwai", "line\nnext", true)
	if script != `display alert "zwai" message "line
next" as critical` {
		t.Fatalf("script = %s", script)
	}
	if !strings.HasSuffix(startupAlertScript("t", "m", false), "as warning") {
		t.Fatal("a reset was marked as a failure")
	}
}
