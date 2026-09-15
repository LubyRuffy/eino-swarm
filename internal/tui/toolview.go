package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// primaryArgKeys is the field a built-in tool's one-line summary should show.
// Names come from the tool schemas, not from anyone's example query.
var primaryArgKeys = map[string][]string{
	"exec":          {"command"},
	"web_search":    {"query"},
	"web_fetch":     {"url"},
	"read":          {"file_path"},
	"write":         {"file_path"},
	"edit":          {"file_path"},
	"ls":            {"path"},
	"tree":          {"path"},
	"glob":          {"pattern", "path"},
	"grep":          {"pattern", "path"},
	"python_runner": {"code"},
	"screenshot":    {"path"},
	"spawn_agent":   {"role"},
	"send_message":  {"text"},
	"resume_agent":  {"agent_id"},
	"wait_agents":   {},
	"close_agent":   {"agent_id"},
}

// summariseToolArgs turns a tool's raw argument string into the one line a
// human should see. JSON envelopes become the command / query / path; plain
// text is left alone so older `read(notes.md)` notifications still render.
func summariseToolArgs(name, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	obj, ok := parseObject(args)
	if !ok {
		return flatten(args)
	}
	if keys, known := primaryArgKeys[name]; known {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if s := asText(obj[k]); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
		return ""
	}
	for _, k := range []string{"command", "query", "url", "file_path", "path", "pattern", "code", "text"} {
		if s := asText(obj[k]); s != "" {
			return s
		}
	}
	for _, v := range obj {
		if s := asText(v); s != "" {
			return s
		}
	}
	return flatten(args)
}

type toolView struct {
	failed bool
	error  string
	body   string
}

// viewToolResult parses a built-in tool's payload the same way the desktop UI
// does: exec stdout (or a failed flag), search hits, or the error: prefix.
func viewToolResult(name, raw string) toolView {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return toolView{}
	}
	if prefix, ok := errorPrefix(raw); ok {
		return toolView{failed: true, error: prefix}
	}
	if name == "exec" || name == "python_runner" {
		if run, ok := parseRunResult(raw); ok {
			failed := run.failed || (run.exitCode != nil && *run.exitCode != 0)
			body := joinNonEmpty(run.stdout, run.stderr)
			errText := run.err
			if failed && errText == "" {
				code := "?"
				if run.exitCode != nil {
					code = fmt.Sprintf("%d", *run.exitCode)
				}
				errText = "exit " + code
			}
			return toolView{failed: failed, error: errText, body: body}
		}
	}
	if name == "web_search" {
		if hits := parseSearchHits(raw); len(hits) > 0 {
			return toolView{body: strings.Join(hits, "\n")}
		}
	}
	return toolView{body: raw}
}

func (v toolView) display() string {
	if v.error != "" && v.body != "" {
		return v.error + "\n" + v.body
	}
	if v.error != "" {
		return v.error
	}
	return v.body
}

type runResult struct {
	stdout   string
	stderr   string
	failed   bool
	exitCode *int
	err      string
}

func parseRunResult(raw string) (runResult, bool) {
	obj, ok := parseObject(raw)
	if !ok {
		return runResult{}, false
	}
	if _, hasOut := obj["stdout"]; !hasOut {
		if _, hasErr := obj["stderr"]; !hasErr {
			if _, hasCode := obj["exit_code"]; !hasCode {
				if _, hasFail := obj["failed"]; !hasFail {
					return runResult{}, false
				}
			}
		}
	}
	run := runResult{
		stdout: asText(obj["stdout"]),
		stderr: asText(obj["stderr"]),
		failed: obj["failed"] == true,
		err:    asText(obj["error"]),
	}
	if n, ok := asInt(obj["exit_code"]); ok {
		run.exitCode = &n
	}
	return run, true
}

func parseSearchHits(raw string) []string {
	var val any
	if err := json.Unmarshal([]byte(raw), &val); err != nil {
		return nil
	}
	rows := asArray(val)
	if rows == nil {
		if obj, ok := val.(map[string]any); ok {
			rows = asArray(obj["results"])
			if rows == nil {
				rows = asArray(obj["items"])
			}
			if rows == nil {
				rows = asArray(obj["data"])
			}
		}
	}
	if rows == nil {
		return nil
	}
	hits := make([]string, 0, len(rows))
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			continue
		}
		title := firstText(obj, "title", "name")
		url := firstText(obj, "url", "link", "href")
		snippet := firstText(obj, "snippet", "summary", "body", "content", "description")
		if title == "" && url == "" && snippet == "" {
			continue
		}
		line := title
		if line == "" {
			line = url
		}
		if url != "" && url != title {
			line += " " + url
		}
		if snippet != "" {
			line += " — " + snippet
		}
		hits = append(hits, line)
	}
	return hits
}

func errorPrefix(raw string) (string, bool) {
	t := strings.TrimSpace(raw)
	if len(t) >= 6 && strings.EqualFold(t[:6], "error:") {
		return strings.TrimSpace(t[6:]), true
	}
	return "", false
}

func parseObject(raw string) (map[string]any, bool) {
	var val any
	if err := json.Unmarshal([]byte(raw), &val); err != nil {
		return nil, false
	}
	obj, ok := val.(map[string]any)
	return obj, ok
}

func asArray(v any) []any {
	a, ok := v.([]any)
	if !ok {
		return nil
	}
	return a
}

func asText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case string:
		n := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func firstText(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asText(obj[k]); s != "" {
			return s
		}
	}
	return ""
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
