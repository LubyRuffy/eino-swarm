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

// The actual Build -> exec chain must cap the encoded result, including both
// streams and JSON escaping, without turning a failed command into plain text.
func TestNoisyCommandOutputStaysParseableUnderTheCap(t *testing.T) {
	escaped := `printf 'OUT-FIRST\n'; printf 'ERR-FIRST\n' >&2; i=0; while [ $i -lt 6000 ]; do printf 'out<>&"\\\001\n'; printf 'err<>&"\\\002\n' >&2; i=$((i+1)); done; printf 'OUT-LAST\n'; printf 'ERR-LAST\n' >&2; exit 7`
	cases := []struct {
		name          string
		limit         int
		command       string
		outputKB      int
		wantExit      int
		wantOutput    string
		wantTruncated bool
		wantTail      bool
	}{
		{"small_unchanged", 4096, "printf intact; printf diagnostic >&2; exit 7", 1, 7, "intact", false, false},
		{"stdout", 4096, "i=0; while [ $i -lt 3000 ]; do echo line-$i; i=$((i+1)); done", 0, 0, "line-", true, false},
		{"escaped_both_streams_default_cap", config.DefaultToolResultMaxChars, escaped, 0, 7, "out", true, false},
		{"explicit_large_budget", 4096, escaped, 512, 7, "out", true, true},
		{"explicit_small_budget", config.DefaultToolResultMaxChars, "i=0; while [ $i -lt 6000 ]; do printf data; i=$((i+1)); done; exit 7", 1, 7, "data", true, false},
		{"unicode", 4096, "i=0; while [ $i -lt 3000 ]; do printf '工具🙂stdout\n'; printf '观察🙂stderr\n' >&2; i=$((i+1)); done; exit 7", 512, 7, "工具🙂", true, false},
		{"large_command_metadata", 4096, "printf ok; printf diagnostic >&2; # " + strings.Repeat(`<>&"\`, 5000) + "\nexit 7", 0, 7, "ok", true, false},
		{"tiny_cap_metadata_floor", 1, "printf output; printf diagnostic >&2; exit 7", 0, 7, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configFor(t)
			cfg.Tools.ResultMaxChars = tc.limit
			set, err := Build(context.Background(), cfg, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"command": tc.command, "timeout_ms": 10000}
			if tc.outputKB > 0 {
				args["max_output_kb"] = tc.outputKB
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			out, err := find(t, set, "exec").InvokableRun(context.Background(), string(raw))
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Stdout      string `json:"stdout"`
				Stderr      string `json:"stderr"`
				ExitCode    int    `json:"exit_code"`
				Failed      bool   `json:"failed"`
				Error       string `json:"error"`
				FullCommand string `json:"full_command"`
				Truncated   bool   `json:"truncated"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("exec result is no longer JSON (%d runes): %v\n%.200s", utf8.RuneCountInString(out), err, out)
			}
			if !utf8.ValidString(out) || !utf8.ValidString(payload.Stdout) || !utf8.ValidString(payload.Stderr) {
				t.Fatal("capping broke UTF-8")
			}
			if tc.name == "unicode" && strings.ContainsRune(payload.Stdout+payload.Stderr, utf8.RuneError) {
				t.Fatal("capping introduced a replacement character at a Unicode boundary")
			}
			if payload.ExitCode != tc.wantExit || payload.Failed != (tc.wantExit != 0) || payload.Truncated != tc.wantTruncated {
				t.Fatalf("exit=%d failed=%v truncated=%v", payload.ExitCode, payload.Failed, payload.Truncated)
			}
			if tc.wantExit != 0 && payload.Error != "exit status 7" {
				t.Fatalf("failed-command error metadata was lost: %q", payload.Error)
			}
			if tc.limit == 1 {
				// An impossibly small limit keeps the complete metadata envelope;
				// every capped text field is empty, rather than broken JSON.
				if payload.Stdout != "" || payload.Stderr != "" || payload.FullCommand != "" {
					t.Fatal("a tiny cap must not retain a command/output text budget")
				}
			} else if utf8.RuneCountInString(out) > tc.limit {
				t.Fatalf("encoded JSON has %d runes, limit %d", utf8.RuneCountInString(out), tc.limit)
			}
			if tc.wantOutput != "" && !strings.Contains(payload.Stdout, tc.wantOutput) {
				t.Fatalf("stdout lost its retained content: %.100s", payload.Stdout)
			}
			if strings.Contains(tc.command, "printf 'err") && !strings.Contains(payload.Stderr, "err") {
				t.Fatal("stderr lost its retained content")
			}
			if !tc.wantTruncated && (payload.Stdout != "intact" || payload.Stderr != "diagnostic" || payload.FullCommand != tc.command) {
				t.Fatal("a small exec result must keep its complete output and command")
			}
			if tc.wantTail && (!strings.HasPrefix(payload.Stdout, "OUT-FIRST\n") || !strings.HasSuffix(payload.Stdout, "OUT-LAST\n") || !strings.HasPrefix(payload.Stderr, "ERR-FIRST\n") || !strings.HasSuffix(payload.Stderr, "ERR-LAST\n")) {
				t.Fatal("result capping must retain both ends of both captured streams")
			}
			if tc.outputKB == 1 && len(payload.Stdout) > 1024 {
				t.Fatalf("explicit per-stream byte limit was exceeded: %d", len(payload.Stdout))
			}
			t.Logf("encoded JSON=%d runes/%d bytes cap=%d exit=%d failed=%v truncated=%v", utf8.RuneCountInString(out), len(out), tc.limit, payload.ExitCode, payload.Failed, payload.Truncated)
		})
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
