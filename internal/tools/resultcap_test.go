package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const capNotice = "[tool result truncated:"

// A worker listed a large directory outside the workspace and the manager's
// context grew from 15K to 232K characters; every later call of that turn
// resent it. A listing tool's result must stay within the configured cap and
// tell the model how to narrow the request.
func TestALargeListingCannotFloodTheModelsContext(t *testing.T) {
	cfg := configFor(t)
	cfg.Tools.ResultMaxChars = 3000
	ws := t.TempDir()
	big := filepath.Join(ws, "many")
	if err := os.Mkdir(big, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("entry-%04d-with-a-long-enough-name.txt", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatal(err)
	}
	out, err := find(t, set, "ls").InvokableRun(context.Background(), `{"path":"many"}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _, _ := strings.Cut(out, "\n\n"+capNotice)
	if !strings.Contains(out, capNotice) || utf8.RuneCountInString(out) > 3000+300 {
		t.Fatalf("listing of %d runes was not capped", utf8.RuneCountInString(out))
	}
	if !strings.Contains(body, "entry-0000") || !strings.Contains(out, "entry-0399") {
		t.Fatal("the cap must keep both the start and the end of the result")
	}
	if !strings.Contains(out, "offset and limit") {
		t.Fatal("the notice must tell the model how to narrow the request")
	}

	small, err := find(t, set, "ls").InvokableRun(context.Background(), `{}`)
	if err != nil || strings.Contains(small, capNotice) {
		t.Fatalf("a small result was altered: %q err=%v", small, err)
	}
}

// exec clips its own streams to a budget derived from the cap, so a noisy
// command still returns parseable JSON flagged truncated rather than a JSON
// document cut in half.
func TestNoisyCommandOutputStaysParseableUnderTheCap(t *testing.T) {
	cfg := configFor(t)
	cfg.Tools.ResultMaxChars = 4096
	set, err := Build(context.Background(), cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, err := find(t, set, "exec").InvokableRun(context.Background(), execArgs(t, map[string]string{"command": "i=0; while [ $i -lt 3000 ]; do echo line-$i; i=$((i+1)); done"}))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Stdout    string `json:"stdout"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("exec result is no longer JSON: %v\n%.200s", err, out)
	}
	if !payload.Truncated || len(out) > 4096 {
		t.Fatalf("truncated=%v len=%d", payload.Truncated, len(out))
	}
}

func TestCapToolResultKeepsValidTextAndExplicitExecBudgets(t *testing.T) {
	text := strings.Repeat("工具输出", 50)
	got := capToolResult(text, 30)
	if !utf8.ValidString(got) || !strings.HasPrefix(got, string([]rune(text)[:20])) || !strings.HasSuffix(got, string([]rune(text)[len([]rune(text))-10:])) {
		t.Fatalf("cap split a rune or lost an end: %q", got)
	}
	if !strings.Contains(got, "170 of 200 characters omitted") {
		t.Fatalf("notice lacks the omitted count: %q", got)
	}
	if capToolResult(text, 200) != text {
		t.Fatal("a result at the cap must be unchanged")
	}
	for _, tc := range []struct{ args, want string }{
		{`{"command":"true"}`, `"max_output_kb":19`},
		{`{"command":"true","max_output_kb":512}`, `"max_output_kb":512`},
		{`not json`, `not json`},
		{`null`, `null`},
	} {
		if got := withDefaultExecOutput(tc.args, execOutputKB(config.DefaultToolResultMaxChars)); !strings.Contains(got, tc.want) {
			t.Fatalf("args %s became %s, want %s", tc.args, got, tc.want)
		}
	}
	if execOutputKB(100) != 1 {
		t.Fatal("a tiny cap still allows exec some output")
	}
	if wrapResultCap(nil, 0) != nil {
		t.Fatal("a disabled cap must return the tool unchanged")
	}
}

func TestToolResultLimitFallsBackToTheDefault(t *testing.T) {
	if (config.ToolsConfig{}).ToolResultLimit() != config.DefaultToolResultMaxChars || (config.ToolsConfig{ResultMaxChars: 7}).ToolResultLimit() != 7 {
		t.Fatal("tool result limit")
	}
	if configFor(t).Tools.ResultMaxChars != config.DefaultToolResultMaxChars {
		t.Fatal("a fresh config must carry the default cap")
	}
}
