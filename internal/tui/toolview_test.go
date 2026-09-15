package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummariseToolArgsShowsTheCommandNotTheJSON(t *testing.T) {
	got := summariseToolArgs("exec", `{"command":"echo hi","cwd":"."}`)
	if got != "echo hi" {
		t.Fatalf("exec summary = %q", got)
	}
	if strings.ContainsAny(got, `{}"`) {
		t.Fatalf("the JSON envelope leaked into the summary: %q", got)
	}
}

func TestSummariseToolArgsShowsTheSearchQuery(t *testing.T) {
	got := summariseToolArgs("web_search", `{"query":"alpha terms"}`)
	if got != "alpha terms" {
		t.Fatalf("search summary = %q", got)
	}
	if strings.Contains(got, "query") {
		t.Fatalf("the field name leaked into the summary: %q", got)
	}
}

func TestSummariseToolArgsKeepsAPlainPath(t *testing.T) {
	// Older notifications were `read(notes.md)`, not a JSON object.
	if got := summariseToolArgs("read", "notes.md"); got != "notes.md" {
		t.Fatalf("plain args = %q", got)
	}
}

func TestViewToolResultSurfacesAFailedCommand(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"exit_code":    127,
		"stdout":       "",
		"stderr":       "not found",
		"failed":       true,
		"error":        "exit status 127",
		"full_command": "nope",
	})
	view := viewToolResult("exec", string(raw))
	if !view.failed || view.error != "exit status 127" {
		t.Fatalf("failed view = %+v", view)
	}
	if strings.Contains(view.display(), "full_command") || strings.Contains(view.body, "{") {
		t.Fatalf("JSON envelope leaked into the result: %q", view.display())
	}
	if view.body != "not found" {
		t.Fatalf("stderr should be the body, got %q", view.body)
	}
}

func TestViewToolResultListsSearchHits(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"results": []map[string]any{
			{"title": "One", "url": "https://example.invalid/one", "summary": "first hit"},
		},
	})
	view := viewToolResult("web_search", string(raw))
	if view.failed || !strings.Contains(view.body, "One") || strings.Contains(view.body, `"title"`) {
		t.Fatalf("search view = %+v", view)
	}
}

func TestViewToolResultTreatsAnErrorPrefixAsFailed(t *testing.T) {
	view := viewToolResult("web_fetch", "error: timed out")
	if !view.failed || view.error != "timed out" {
		t.Fatalf("prefix view = %+v", view)
	}
}

func TestSummariseToolArgsCoversTheFallbackPaths(t *testing.T) {
	if got := summariseToolArgs("exec", ""); got != "" {
		t.Fatalf("empty args = %q", got)
	}
	if got := summariseToolArgs("grep", `{"pattern":"TODO","path":"src"}`); got != "TODO · src" {
		t.Fatalf("grep = %q", got)
	}
	if got := summariseToolArgs("unknown", `{"url":"https://example.invalid"}`); got != "https://example.invalid" {
		t.Fatalf("fallback key = %q", got)
	}
	if got := summariseToolArgs("unknown", `{"role":"researcher"}`); got != "researcher" {
		t.Fatalf("first value = %q", got)
	}
	if got := summariseToolArgs("exec", `{"cwd":"."}`); got != "" {
		t.Fatalf("exec without a command should stay empty, got %q", got)
	}
	if got := summariseToolArgs("wait_agents", `{"timeout_s":60,"agent_ids":["a-1"]}`); got != "" {
		t.Fatalf("wait_agents should not pick a stray field, got %q", got)
	}
}

func TestViewToolResultParsesStdoutAndBareExit(t *testing.T) {
	ok := viewToolResult("python_runner", `{"exit_code":0,"stdout":"ok\nline","stderr":"","failed":false}`)
	if ok.failed || ok.body != "ok\nline" {
		t.Fatalf("success view = %+v", ok)
	}
	fail := viewToolResult("exec", `{"exit_code":2,"stdout":"out","stderr":"err","failed":true}`)
	if !fail.failed || fail.error != "exit 2" || !strings.Contains(fail.display(), "out") {
		t.Fatalf("bare exit view = %+v display=%q", fail, fail.display())
	}
	plain := viewToolResult("ls", "a\nb")
	if plain.failed || plain.body != "a\nb" || plain.display() != "a\nb" {
		t.Fatalf("plain view = %+v", plain)
	}
	if viewToolResult("exec", "").display() != "" {
		t.Fatal("empty result should display nothing")
	}
	raw := viewToolResult("exec", `{"file_path":"x"}`)
	if raw.failed || !strings.Contains(raw.body, "file_path") {
		t.Fatalf("non-run exec payload = %+v", raw)
	}
	bareFail := viewToolResult("exec", `{"failed":true}`)
	if !bareFail.failed || bareFail.error != "exit ?" {
		t.Fatalf("failed without a code = %+v", bareFail)
	}
	if _, ok := parseRunResult(`{"file_path":"x"}`); ok {
		t.Fatal("a non-run payload must not parse as a command result")
	}
	if _, ok := parseRunResult(`{"stderr":"e"}`); !ok {
		t.Fatal("stderr alone is still a run payload")
	}
}

func TestParseSearchHitsAcceptsWrappedAndBareLists(t *testing.T) {
	bare, _ := json.Marshal([]map[string]any{
		{"name": "Bare", "href": "https://example.invalid/b", "description": "desc"},
		{"url": "https://example.invalid/only"},
		{"nested": true},
	})
	if hits := parseSearchHits(string(bare)); len(hits) != 2 {
		t.Fatalf("bare hits = %v", hits)
	}
	items, _ := json.Marshal(map[string]any{
		"items": []map[string]any{{"title": "Item", "link": "https://example.invalid/i", "content": "body"}},
	})
	if hits := parseSearchHits(string(items)); len(hits) != 1 || !strings.Contains(hits[0], "Item") {
		t.Fatalf("items hits = %v", hits)
	}
	data, _ := json.Marshal(map[string]any{
		"data": []map[string]any{{"title": "Data", "url": "https://example.invalid/d", "snippet": "sn"}},
	})
	if hits := parseSearchHits(string(data)); len(hits) != 1 {
		t.Fatalf("data hits = %v", hits)
	}
	if parseSearchHits("not-json") != nil {
		t.Fatal("garbage is not a hit list")
	}
	if parseSearchHits(`{"message":"ok"}`) != nil {
		t.Fatal("an object without rows is not a hit list")
	}
}

func TestAsTextAndAsIntCoverTheScalarCases(t *testing.T) {
	if asText(3.0) != "3" || asText(1.5) != "1.5" {
		t.Fatalf("number asText: %q %q", asText(3.0), asText(1.5))
	}
	if asText(true) != "true" || asText(false) != "false" || asText(struct{}{}) != "" {
		t.Fatal("bool/default asText")
	}
	if n, ok := asInt(float64(7)); !ok || n != 7 {
		t.Fatalf("float asInt %v %v", n, ok)
	}
	if n, ok := asInt(json.Number("8")); !ok || n != 8 {
		t.Fatalf("json.Number asInt %v %v", n, ok)
	}
	if n, ok := asInt("9"); !ok || n != 9 {
		t.Fatalf("string asInt %v %v", n, ok)
	}
	if _, ok := asInt("nope"); ok {
		t.Fatal("garbage asInt")
	}
	if n, ok := asInt(json.Number("x")); ok || n != 0 {
		t.Fatalf("bad json.Number asInt %v %v", n, ok)
	}
	if _, ok := asInt(true); ok {
		t.Fatal("bool asInt")
	}
	if firstText(map[string]any{"a": ""}, "a", "b") != "" {
		t.Fatal("firstText should miss empty keys")
	}
}
