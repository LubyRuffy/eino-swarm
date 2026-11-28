package graph

import (
	"strings"
	"testing"
)

func TestRedactToolTextKeepsCredentialNamesAndDropsValues(t *testing.T) {
	const secret = "fake-credential-for-redact-test"
	call := `update_graph({"subjects":[{"kind":"repo","attrs":{"path":"acme/app","credential":"` + secret + `"}}]})`
	got := RedactToolText(call)
	if got == call || strings.Contains(got, secret) || !strings.Contains(got, `"credentials":["credential"]`) || !strings.Contains(got, "acme/app") {
		t.Fatalf("call: %s", got)
	}
	raw := `{"ok":true,"result":{"subjects":[{"kind":"repo","attrs":{"path":"acme/app","credential":"` + secret + `"}}]}}`
	got = RedactToolText(raw)
	if strings.Contains(got, secret) || !strings.Contains(got, `"ok":true`) || !strings.Contains(got, "acme/app") {
		t.Fatalf("result: %s", got)
	}
	args := `{"tool_calls":[{"function":{"name":"update_graph","arguments":"{\"subjects\":[{\"kind\":\"repo\",\"attrs\":{\"path\":\"acme/app\",\"credential\":\"` + secret + `\"}}]}"}}]}`
	got = RedactToolText(args)
	if strings.Contains(got, secret) || !strings.Contains(got, "update_graph") {
		t.Fatalf("arguments: %s", got)
	}
	if RedactToolText("read_file({\"path\":\"notes.md\"})") != "read_file({\"path\":\"notes.md\"})" {
		t.Fatal("a non-graph call was rewritten")
	}
	if RedactToolText("update_graph({") != "update_graph("+omittedToolPayload+")" {
		t.Fatal("broken JSON was retained")
	}
	plain := `{"todos":[{"title":"look"}]}`
	if RedactToolText(plain) != plain {
		t.Fatal("a payload with no credential was rewritten")
	}
}

func TestRedactToolTextHandlesRawAddressesEditsAndRecoveryCopies(t *testing.T) {
	const secret = "synthetic-graph-password"
	for _, tc := range []struct{ name, input, want string }{
		{"address", `{"subjects":[{"kind":"repo","attrs":{"url":"https://fixture:` + secret + `@example.test/acme/app"}}]}`, `https://example.test/acme/app`},
		{"malformed address", `{"subjects":[{"kind":"repo","attrs":{"url":"https://fixture:` + secret + `%xx@example.test/acme/app"}}]}`, `https://example.test/acme/app`},
		{"by-ID", `{"subjects":[{"id":"sj_existing","attrs":{"credential":"` + secret + `"}}]}`, `"credentials":["credential"]`},
		{"truncated", `{"subjects":[{"kind":"repo","attrs":{"credential":"` + secret, omittedToolPayload},
		{"worker call", `{"in_flight":[{"name":"update_graph","call_id":"x","arguments":"{\"subjects\":[{\"id\":\"sj_x\",\"attrs\":{\"credential\":\"` + secret + `\"}}]}"}]}`, `"call_id":"x"`},
		{"worker result", `{"history":[{"role":"tool","tool_name":"read_graph","content":"{\"subjects\":[{\"kind\":\"repo\",\"attrs\":{\"credential\":\"` + secret + `\"}}]}"}]}`, `"tool_name":"read_graph"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := RedactToolText(tc.input)
			if strings.Contains(out, secret) || !strings.Contains(out, tc.want) {
				t.Fatal("redaction dropped its safe witness or retained a credential")
			}
		})
	}
	const other = `[{"function":{"name":"read_file","arguments":"ordinary input"}}]`
	if got := RedactToolText(other); got != other {
		t.Fatal("redaction changed an unrelated tool call")
	}
}
