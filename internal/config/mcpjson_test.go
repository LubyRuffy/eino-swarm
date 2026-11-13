package config

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestMCPJSONImportMergesCursorServersWithoutDroppingSecrets(t *testing.T) {
	existing := []MCPServer{{
		ID: "kept", Name: "Desk", Transport: "stdio", Command: "original", Args: []string{"--old"},
		Enabled: false, TimeoutSeconds: 30, Env: map[string]string{"TOKEN": "secret-value"},
		Headers: map[string]string{"X-Token": "secret-value"},
	}}
	raw := `{
		"mcpServers": {
			"Desk": {"command": "next", "args": ["--new"]},
			"remote": {"url": "https://example.invalid/mcp", "headers": {"Authorization": "secret-value"}},
			"events": {"type": "sse", "url": "https://example.invalid/sse", "disabled": true, "timeout_seconds": 15},
			"line": {"type": "stdio", "command": "dart mcp-server --experimental-mcp-server --force-roots-fallback", "args": [], "env": {}}
		}
	}`
	got, err := ApplyMCPJSON(existing, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if existing[0].Command != "original" || existing[0].Env["TOKEN"] != "secret-value" {
		t.Fatal("import mutated the input")
	}
	got[0].Env["TOKEN"] = "changed"
	if existing[0].Env["TOKEN"] != "secret-value" {
		t.Fatal("result shares the saved credential map")
	}
	if len(got) != 4 || got[0].ID != "kept" || got[0].Name != "Desk" || got[0].Command != "next" || len(got[0].Args) != 1 || got[0].Args[0] != "--new" {
		t.Fatalf("updated server: %+v", got)
	}
	if got[0].Enabled || got[0].TimeoutSeconds != 30 || got[0].Env["TOKEN"] != "changed" || got[0].Headers["X-Token"] != "secret-value" {
		t.Fatalf("omitted private or ordinary fields changed: %+v", got[0])
	}
	if got[1].ID != "remote" || got[1].Name != "remote" || got[1].Transport != "streamable_http" || !got[1].Enabled || got[1].Headers["Authorization"] != "secret-value" || got[1].Command != "" {
		t.Fatalf("remote: %+v", got[1])
	}
	if got[2].ID != "events" || got[2].Transport != "sse" || got[2].Enabled || got[2].TimeoutSeconds != 15 {
		t.Fatalf("sse: %+v", got[2])
	}
	if got[3].Command != "dart" || strings.Join(got[3].Args, " ") != "mcp-server --experimental-mcp-server --force-roots-fallback" || len(got[3].Env) != 0 {
		t.Fatalf("command line: %+v", got[3])
	}
	again, err := ApplyMCPJSON(got, []byte(raw))
	if err != nil || len(again) != 4 || again[1].ID != "remote" {
		t.Fatalf("reapplying created a server: %v %+v", err, again)
	}
}

func TestMCPJSONImportKeepsASpacedProgramAndQuotedArguments(t *testing.T) {
	raw := `{"mcpServers":{
		"spaced": {"command": "/opt/My Server", "args": ["--flag"]},
		"quoted": {"command": "\"/opt/My Server\"", "args": []},
		"grouped": {"command": "tool \"a b\" 'c d' e\\ f", "env": null}
	}}`
	got, err := ApplyMCPJSON(nil, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Command != "/opt/My Server" || len(got[0].Args) != 1 || got[0].Args[0] != "--flag" {
		t.Fatalf("explicit args: %+v", got[0])
	}
	if got[1].Command != "/opt/My Server" || len(got[1].Args) != 0 {
		t.Fatalf("quoted program: %+v", got[1])
	}
	if got[2].Command != "tool" || strings.Join(got[2].Args, "|") != "a b|c d|e f" || got[2].Env != nil {
		t.Fatalf("quoted args: %+v", got[2])
	}
}

func TestMCPJSONImportAcceptsTheOtherDocumentShapes(t *testing.T) {
	raw := "\uFEFF" + `{"servers":[
		{"id":"fixed","name":"Fixed","transport":"stdio","command":" tool ","args":["--ok"],"enabled":false},
		{"name":"Remote","transport":"streamable_http","url":"https://example.invalid/mcp","timeout_seconds":0}
	]}`
	got, err := ApplyMCPJSON(nil, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "fixed" || got[0].Command != "tool" || got[0].Enabled || got[1].Name != "Remote" || got[1].Transport != "streamable_http" || got[1].TimeoutSeconds != 0 {
		t.Fatalf("%+v", got)
	}
	vscode, err := ApplyMCPJSON(got, []byte(`{"servers":{"fixed":{"type":"streamableHttp","url":"https://example.invalid/other"}}}`))
	if err != nil || vscode[0].Transport != "streamable_http" || vscode[0].URL != "https://example.invalid/other" || vscode[0].Command != "" || vscode[1].ID != got[1].ID {
		t.Fatalf("vscode update: %v %+v", err, vscode)
	}
	same, err := ApplyMCPJSON(got, []byte(`{"mcpServers":{}}`))
	if err != nil || len(same) != 2 || same[0].Command != "tool" {
		t.Fatalf("empty document: %v %+v", err, same)
	}
}

func TestMCPJSONImportMatchesAUniqueNameAndAllocatesOtherwise(t *testing.T) {
	prev := mcpIDEntropy
	t.Cleanup(func() { mcpIDEntropy = prev })
	mcpIDEntropy = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0xab
		}
		return len(b), nil
	}
	existing := []MCPServer{
		{ID: "aa", Name: "My Server", Transport: "stdio", Command: "original"},
		{ID: "bb", Name: "Other", Transport: "stdio", Command: "original"},
	}
	got, err := ApplyMCPJSON(existing, []byte(`{"mcpServers":{"My Server":{"command":"next"}}}`))
	if err != nil || len(got) != 2 || got[0].ID != "aa" || got[0].Command != "next" || got[1].Command != "original" {
		t.Fatalf("name match: %v %+v", err, got)
	}
	created, err := ApplyMCPJSON(existing, []byte(`{"mcpServers":{"Another Name":{"command":"tool"}}}`))
	if err != nil || created[2].ID != strings.Repeat("ab", 16) || created[2].Name != "Another Name" {
		t.Fatalf("allocated: %v %+v", err, created)
	}
	mcpIDEntropy = func(b []byte) (int, error) { return 0, errors.New("no entropy") }
	_, err = ApplyMCPJSON(nil, []byte(`{"mcpServers":{"Another Name":{"command":"tool"}}}`))
	if err == nil || strings.Contains(err.Error(), "no entropy") || !strings.Contains(err.Error(), "allocate") {
		t.Fatal(err)
	}
	mcpIDEntropy = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 1
		}
		return len(b), nil
	}
	blocked := strings.Repeat("01", 16)
	_, err = ApplyMCPJSON([]MCPServer{{ID: blocked, Name: "Kept", Transport: "stdio", Command: "tool"}}, []byte(`{"mcpServers":{"Another Name":{"command":"tool"}}}`))
	if err == nil || !strings.Contains(err.Error(), "allocate") {
		t.Fatal(err)
	}
}

func TestMCPJSONImportRejectsUnsafeOrAmbiguousTextWithoutEchoingIt(t *testing.T) {
	existing := []MCPServer{{ID: "kept", Name: "Kept", Transport: "stdio", Command: "original", Env: map[string]string{"TOKEN": "secret-value"}}}
	for _, raw := range []string{
		`{"mcpServers":{"bad":{"command":"tool --token secret-value && rm"}}}`,
		`{"mcpServers":{"bad":{"command":"tool \"secret-value"}}}`,
		`{"mcpServers":{"bad":{"url":"http://user:secret-value@example.invalid/mcp"}}}`,
		`{"mcpServers":{"bad":{"command":"tool","envFile":"/tmp/secret-value"}}}`,
		`{"mcpServers":{"bad":{"command":"tool","cwd":"/tmp/secret-value"}}}`,
		`{"mcpServers":{"bad":{"command":"tool","url":"https://example.invalid/mcp"}}}`,
		`{"mcpServers":{"bad":{"type":"sse","command":"tool"}}}`,
		`{"mcpServers":{"bad":{"type":"http","args":["secret-value"]}}}`,
		`{"mcpServers":{"bad":{"command":"tool","args":[1]}}}`,
		`{"mcpServers":{"bad":{"command":"tool","env":{"TOKEN":1}}}}`,
		`{"mcpServers":{"bad":{"url":"https://example.invalid/mcp","headers":{"X":1}}}}`,
		`{"mcpServers":{"bad":{"command":"tool","enabled":true,"disabled":true}}}`,
		`{"mcpServers":{"bad":{"command":"tool","timeout_seconds":-1}}}`,
		`{"mcpServers":{"bad":{"command":"tool","timeout_seconds":"secret-value"}}}`,
		`{"mcpServers":{"bad":{"type":"socket","command":"tool"}}}`,
		`{"mcpServers":{"bad":{"type":"stdio","transport":"sse","command":"tool"}}}`,
		`{"mcpServers":{},"servers":{}}`,
		`{"extra":true}`,
		`{"mcpServers":[{"command":"tool"}]}`,
		`[]`,
		`{"TOKEN":"secret-value"`,
		`{"servers":[{"id":"bad.id","command":"tool"}]}`,
		`{"mcpServers":{"a":{"command":"tool"},"a":{"command":"other"}}}`,
		`{"mcpServers":{"Kept":{"command":"one"},"alias":{"name":"Kept","command":"two"}}}`,
		" \n ",
		`{}`,
		`{"mcpServers":null}`,
		`{"servers":null}`,
		`{"mcpServers":{"a":{"type":"stdio","url":"https://example.invalid/mcp"}}}`,
		`{"mcpServers":{"a":{"command":"   "}}}`,
		`{"mcpServers":{"a":{"command":"\"\""}}}`,
		`{"mcpServers":{"a":{"name":"  ","command":"tool"}}}`,
		`{"mcpServers":{"a":[]}}`,
		`{"servers":["nope"]}`,
		`{"mcpServers":{"a":{"id":1,"command":"tool"}}}`,
		`{"mcpServers":{"a":{"name":1,"command":"tool"}}}`,
		`{"mcpServers":{"a":{"type":1,"command":"tool"}}}`,
		`{"mcpServers":{"a":{"transport":1,"command":"tool"}}}`,
		`{"mcpServers":{"a":{"transport":"socket","command":"tool"}}}`,
		`{"mcpServers":{"a":{"command":1}}}`,
		`{"mcpServers":{"a":{"url":1}}}`,
		`{"mcpServers":{"a":{"command":"tool","enabled":1}}}`,
		`{"mcpServers":{"a":{"command":"tool","disabled":"no"}}}`,
		`{"mcpServers":{"a":{"command":"tool","args":{}}}}`,
		`{"mcpServers":{"a":{"command":"tool","env":[]}}}`,
		`{"mcpServers":{"a":{"command":"tool","headers":[]}}}`,
		`{"mcpServers":{"a":{"command":"tool","timeout_seconds":1.5}}}`,
		`{"servers":[{"id":"fixed","name":"One","command":"a"},{"id":"fixed","name":"Two","command":"b"}]}`,
		`{"servers":[{"id":"kept","name":"One","command":"a"},{"id":"kept","name":"Two","command":"b"}]}`,
	} {
		_, err := ApplyMCPJSON(existing, []byte(raw))
		var invalid *MCPJSONError
		if err == nil || !errors.As(err, &invalid) || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("raw %s error=%v", raw, err)
		}
		if existing[0].Command != "original" || existing[0].Env["TOKEN"] != "secret-value" {
			t.Fatal("rejected import changed the saved server")
		}
	}
	ambiguous := []MCPServer{
		{ID: "one", Name: "Same", Transport: "stdio", Command: "a"},
		{ID: "two", Name: "Same", Transport: "stdio", Command: "b"},
	}
	if _, err := ApplyMCPJSON(ambiguous, []byte(`{"mcpServers":{"Same":{"command":"c"}}}`)); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatal(err)
	}
	if ambiguous[0].Command != "a" || ambiguous[1].Command != "b" {
		t.Fatal("ambiguous match wrote a server")
	}
}

func TestMCPJSONImportCoversLabelsNullFieldsAndCommandSplits(t *testing.T) {
	_, err := ApplyMCPJSON(nil, []byte(`{"mcpServers":{"":{"command":"tool"}}}`))
	if err == nil || !strings.Contains(err.Error(), "server") {
		t.Fatalf("empty key: %v", err)
	}
	long := strings.Repeat("n", 70)
	_, err = ApplyMCPJSON(nil, []byte(`{"mcpServers":{`+strconv.Quote(long)+`:{"url":"not a url"}}}`))
	if err == nil || strings.Contains(err.Error(), long) || !strings.Contains(err.Error(), long[:64]) {
		t.Fatalf("long label: %v", err)
	}
	_, err = ApplyMCPJSON(nil, []byte("{\"mcpServers\":{\"line\\nbad\":{\"command\":\"tool | echo\"}}}"))
	if err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "line bad") {
		t.Fatalf("control label: %q", err)
	}

	kept := []MCPServer{{ID: "aa", Name: "Desk", Transport: "stdio", Command: "old", Args: []string{}, Enabled: true}}
	_, err = ApplyMCPJSON(kept, []byte(`{"mcpServers":{"Desk":{"command":"next"},"aa":{"name":"Other","command":"next"}}}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate server") || kept[0].Command != "old" {
		t.Fatalf("claimed id: %v %+v", err, kept[0])
	}

	next, err := ApplyMCPJSON(nil, []byte(`{"mcpServers":{"plain":{"command":"tool","args":null,"envFile":null,"cwd":null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if next[0].Command != "tool" || len(next[0].Args) != 0 || next[0].Env != nil {
		t.Fatalf("null optional fields: %+v", next[0])
	}

	argv, err := splitCommandLine(`"a\"b"`)
	if err != nil || len(argv) != 1 || argv[0] != `a"b` {
		t.Fatalf("escaped quote: %v %q", err, argv)
	}
	for _, line := range []string{`"a\`, "\"a\\\n\"", "\"a\nb\"", `a\`, "a\\\n"} {
		if _, err := splitCommandLine(line); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
	for _, raw := range [][]byte{[]byte(`{`), []byte(`[]`), []byte(`{"a":`), []byte(`{"a":1`)} {
		if _, err := parseMCPServerObject(raw); err == nil {
			t.Fatalf("accepted object %s", raw)
		}
	}
	for _, raw := range [][]byte{[]byte(`]`), []byte(`[`), []byte(`[}`)} {
		if _, err := parseMCPServerArray(raw); err == nil {
			t.Fatalf("accepted array %s", raw)
		}
	}
	if _, err := parseMCPServerEntry([]byte(`{`)); err == nil {
		t.Fatal("accepted a truncated entry")
	}
	if _, _, err := optionalJSONString(map[string]json.RawMessage{"id": []byte(`"unterminated`)}, "id"); err == nil {
		t.Fatal("accepted an unterminated string")
	}
	if _, _, err := parseJSONArgs([]byte(`[`), true); err == nil {
		t.Fatal("accepted a truncated argument array")
	}
	if _, _, err := parseJSONStringMap(map[string]json.RawMessage{"env": []byte(`{`)}, "env", "environment variables"); err == nil {
		t.Fatal("accepted a truncated environment object")
	}
	if _, err := inferMCPTransport("other", "", ""); err == nil {
		t.Fatal("accepted an unknown transport")
	}
	_, _, _, err = matchMCPServer(nil, map[int]bool{}, map[string]bool{"taken": true}, mcpJSONSource{hasKey: true, key: "taken"}, parsedMCPServer{name: "Fresh"})
	if err == nil || !strings.Contains(err.Error(), "duplicate server") {
		t.Fatalf("created id: %v", err)
	}
}

func TestMCPJSONKeepsToolSwitchesUnlessTheDocumentReplacesThem(t *testing.T) {
	existing := []MCPServer{{
		ID: "desk", Name: "Desk", Transport: "stdio", Command: "tool", Enabled: true,
		DisabledTools: []string{"lookup"},
	}}
	got, err := ApplyMCPJSON(existing, []byte(`{"mcpServers":{"desk":{"command":"tool"}}}`))
	if err != nil || len(got) != 1 || len(got[0].DisabledTools) != 1 || got[0].DisabledTools[0] != "lookup" {
		t.Fatalf("omitted switch: %+v %v", got, err)
	}
	got[0].DisabledTools[0] = "changed"
	if existing[0].DisabledTools[0] != "lookup" {
		t.Fatal("import shared the saved switch list")
	}
	got, err = ApplyMCPJSON(existing, []byte(`{"mcpServers":{"desk":{"command":"tool","disabled_tools":["search","search"]}}}`))
	if err != nil || len(got[0].DisabledTools) != 1 || got[0].DisabledTools[0] != "search" {
		t.Fatalf("replaced switch: %+v %v", got, err)
	}
	if _, err := ApplyMCPJSON(existing, []byte(`{"mcpServers":{"desk":{"command":"tool","disabled_tools":"lookup"}}}`)); err == nil {
		t.Fatal("accepted a tool switch that is not a list")
	}
	if _, err := ApplyMCPJSON(existing, []byte(`{"mcpServers":{"desk":{"command":"tool","disabled_tools":["bad\u0000"]}}}`)); err == nil {
		t.Fatal("accepted a broken tool name")
	}
	if existing[0].DisabledTools[0] != "lookup" {
		t.Fatal("rejected document changed the saved switch")
	}
}
