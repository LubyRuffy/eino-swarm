package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MCPJSONError is a rejected MCP JSON document. Its text names the server and
// the rule; it does not include credentials, URLs or command text.
type MCPJSONError struct{ msg string }

func (e *MCPJSONError) Error() string { return e.msg }

func mcpJSONError(format string, args ...any) error {
	return &MCPJSONError{msg: fmt.Sprintf(format, args...)}
}

// mcpIDEntropy is the randomness for a server id the JSON document did not choose.
var mcpIDEntropy = rand.Read

type mcpJSONSource struct {
	label  string
	hasKey bool
	key    string
	raw    json.RawMessage
}

type parsedMCPServer struct {
	id         string
	idSet      bool
	name       string
	transport  string
	command    string
	args       []string
	argsSet    bool
	env        map[string]string
	envSet     bool
	url        string
	headers    map[string]string
	headersSet bool
	enabled    *bool
	timeout    *int
}

// ApplyMCPJSON adds or updates the servers in a Cursor-style document.
// Servers absent from the text stay, including their credentials. Omitted
// env and headers keep the saved maps; an empty object clears one map.
// A command written as one line is split into the program and its arguments.
// Quotes group one argument. The split never starts a shell.
func ApplyMCPJSON(existing []MCPServer, raw []byte) ([]MCPServer, error) {
	entries, err := parseMCPDocument(raw)
	if err != nil {
		return nil, err
	}
	next := cloneMCPServers(existing)
	claimed := map[int]bool{}
	created := map[string]bool{}
	usedNames := map[string]bool{}
	for _, entry := range entries {
		parsed, err := parseMCPServerEntry(entry.raw)
		if err != nil {
			return nil, mcpJSONLabeled(entry.label, err)
		}
		if parsed.name == "" && entry.hasKey {
			parsed.name = entry.key
		}
		if parsed.name == "" && parsed.idSet {
			parsed.name = parsed.id
		}
		if strings.TrimSpace(parsed.name) == "" {
			return nil, mcpJSONLabeled(entry.label, mcpJSONError("mcp: a server needs a name"))
		}
		if usedNames[parsed.name] {
			return nil, mcpJSONLabeled(parsed.name, mcpJSONError("mcp: duplicate server name"))
		}
		index, id, create, err := matchMCPServer(next, claimed, created, entry, parsed)
		if err != nil {
			return nil, mcpJSONLabeled(parsed.name, err)
		}
		current := MCPServer{ID: id}
		if !create {
			current = next[index]
		}
		if err := parsed.apply(&current, create); err != nil {
			return nil, mcpJSONLabeled(parsed.name, err)
		}
		if create {
			next = append(next, current)
			created[id] = true
			claimed[len(next)-1] = true
		} else {
			next[index] = current
			claimed[index] = true
		}
		usedNames[parsed.name] = true
	}
	seen := make(map[string]bool, len(next))
	for _, server := range next {
		if seen[server.ID] {
			return nil, mcpJSONError("mcp: duplicate server")
		}
		seen[server.ID] = true
	}
	return next, nil
}

func mcpJSONLabeled(label string, err error) error {
	msg := strings.TrimPrefix(err.Error(), "mcp: ")
	return mcpJSONError("mcp: %s: %s", mcpJSONLabel(label), msg)
}

func mcpJSONLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(label))
	runes := []rune(label)
	if len(runes) == 0 {
		return "server"
	}
	if len(runes) > 64 {
		return string(runes[:64])
	}
	return label
}

func cloneMCPServers(servers []MCPServer) []MCPServer {
	if servers == nil {
		return nil
	}
	out := make([]MCPServer, len(servers))
	for i, server := range servers {
		server.Args = append([]string(nil), server.Args...)
		if server.Env != nil {
			env := make(map[string]string, len(server.Env))
			for key, value := range server.Env {
				env[key] = value
			}
			server.Env = env
		}
		if server.Headers != nil {
			headers := make(map[string]string, len(server.Headers))
			for key, value := range server.Headers {
				headers[key] = value
			}
			server.Headers = headers
		}
		out[i] = server
	}
	return out
}

func parseMCPDocument(raw []byte) ([]mcpJSONSource, error) {
	raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte("\uFEFF"))
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, mcpJSONError("mcp: invalid JSON")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return nil, mcpJSONError("mcp: invalid JSON")
	}
	for key := range top {
		if key != "mcpServers" && key != "servers" {
			return nil, mcpJSONError("mcp: JSON only accepts mcpServers or servers")
		}
	}
	body, ok := top["mcpServers"]
	kind := "mcpServers"
	if !ok {
		body, ok = top["servers"]
		kind = "servers"
		if !ok {
			return nil, mcpJSONError("mcp: JSON must contain mcpServers or servers")
		}
	} else if _, other := top["servers"]; other {
		return nil, mcpJSONError("mcp: JSON contains both mcpServers and servers")
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, mcpJSONError("mcp: %s must be a JSON object", kind)
	}
	if kind == "servers" && trimmed[0] == '[' {
		return parseMCPServerArray(trimmed)
	}
	if trimmed[0] != '{' {
		return nil, mcpJSONError("mcp: %s must be a JSON object", kind)
	}
	return parseMCPServerObject(trimmed)
}

func parseMCPServerObject(raw json.RawMessage) ([]mcpJSONSource, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, mcpJSONError("mcp: server list must be a JSON object")
	}
	entries := []mcpJSONSource{}
	seen := map[string]bool{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, mcpJSONError("mcp: invalid JSON")
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, mcpJSONError("mcp: invalid JSON")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, mcpJSONError("mcp: invalid JSON")
		}
		if seen[key] {
			return nil, mcpJSONLabeled(key, mcpJSONError("mcp: duplicate server name"))
		}
		seen[key] = true
		entries = append(entries, mcpJSONSource{label: key, hasKey: true, key: key, raw: value})
	}
	tok, err = dec.Token()
	if err != nil || tok != json.Delim('}') {
		return nil, mcpJSONError("mcp: invalid JSON")
	}
	return entries, nil
}

func parseMCPServerArray(raw json.RawMessage) ([]mcpJSONSource, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('[') {
		return nil, mcpJSONError("mcp: servers must be a JSON array or object")
	}
	entries := []mcpJSONSource{}
	for dec.More() {
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, mcpJSONError("mcp: invalid JSON")
		}
		entries = append(entries, mcpJSONSource{label: fmt.Sprintf("servers[%d]", len(entries)), raw: value})
	}
	tok, err = dec.Token()
	if err != nil || tok != json.Delim(']') {
		return nil, mcpJSONError("mcp: invalid JSON")
	}
	return entries, nil
}

func parseMCPServerEntry(raw json.RawMessage) (parsedMCPServer, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return parsedMCPServer{}, mcpJSONError("mcp: a server entry must be a JSON object")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		return parsedMCPServer{}, mcpJSONError("mcp: a server entry must be a JSON object")
	}
	for key, value := range obj {
		switch strings.ToLower(key) {
		case "envfile", "env_file":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return parsedMCPServer{}, mcpJSONError("mcp: envFile is not supported; put variables in env")
			}
		case "cwd":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return parsedMCPServer{}, mcpJSONError("mcp: cwd is not supported")
			}
		}
	}
	var parsed parsedMCPServer
	var err error
	if parsed.id, parsed.idSet, err = optionalJSONString(obj, "id"); err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: id must be a string")
	}
	if parsed.name, _, err = optionalJSONString(obj, "name"); err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: name must be a string")
	}
	typeField, _, err := optionalJSONString(obj, "type")
	if err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: type must be a string")
	}
	transportField, _, err := optionalJSONString(obj, "transport")
	if err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: transport must be a string")
	}
	parsed.transport, err = explicitTransport(typeField, transportField)
	if err != nil {
		return parsedMCPServer{}, err
	}
	if parsed.command, _, err = optionalJSONString(obj, "command"); err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: command must be a string")
	}
	if parsed.url, _, err = optionalJSONString(obj, "url"); err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: url must be a string")
	}
	if parsed.args, parsed.argsSet, err = parseJSONArgs(obj["args"], argsKey(obj)); err != nil {
		return parsedMCPServer{}, err
	}
	if parsed.env, parsed.envSet, err = parseJSONStringMap(obj, "env", "environment variables"); err != nil {
		return parsedMCPServer{}, err
	}
	if parsed.headers, parsed.headersSet, err = parseJSONStringMap(obj, "headers", "headers"); err != nil {
		return parsedMCPServer{}, err
	}
	enabled, err := optionalJSONBool(obj, "enabled")
	if err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: enabled must be a boolean")
	}
	disabled, err := optionalJSONBool(obj, "disabled")
	if err != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: disabled must be a boolean")
	}
	if enabled != nil && disabled != nil {
		return parsedMCPServer{}, mcpJSONError("mcp: enabled and disabled conflict")
	}
	if disabled != nil {
		on := !*disabled
		enabled = &on
	}
	parsed.enabled = enabled
	if parsed.timeout, err = optionalJSONInt(obj, "timeout_seconds"); err != nil {
		return parsedMCPServer{}, err
	}
	return parsed, nil
}

func argsKey(obj map[string]json.RawMessage) bool {
	_, ok := obj["args"]
	return ok
}

func (p parsedMCPServer) apply(current *MCPServer, create bool) error {
	transport, err := inferMCPTransport(p.transport, p.command, p.url)
	if err != nil {
		return err
	}
	current.Name = p.name
	current.Transport = transport
	if transport == "stdio" {
		command, args, err := resolveStdioCommand(p.command, p.args, p.argsSet)
		if err != nil {
			return err
		}
		current.Command = command
		current.Args = args
		current.URL = ""
	} else {
		if p.argsSet && len(p.args) > 0 {
			return mcpJSONError("mcp: a remote server does not take command arguments")
		}
		current.URL = p.url
		current.Command = ""
		current.Args = []string{}
	}
	if p.enabled != nil {
		current.Enabled = *p.enabled
	} else if create {
		current.Enabled = true
	}
	if p.timeout != nil {
		current.TimeoutSeconds = *p.timeout
	} else if create {
		current.TimeoutSeconds = 0
	}
	if p.envSet {
		current.Env = p.env
	} else if create {
		current.Env = nil
	}
	if p.headersSet {
		current.Headers = p.headers
	} else if create {
		current.Headers = nil
	}
	return current.Validate()
}

func explicitTransport(typeField, transportField string) (string, error) {
	fromType, err := normalizeMCPTransport(typeField)
	if err != nil {
		return "", err
	}
	fromTransport, err := normalizeMCPTransport(transportField)
	if err != nil {
		return "", err
	}
	if fromType != "" && fromTransport != "" && fromType != fromTransport {
		return "", mcpJSONError("mcp: transport must be stdio, sse or http")
	}
	if fromType != "" {
		return fromType, nil
	}
	return fromTransport, nil
}

func normalizeMCPTransport(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), "_", "")) {
	case "stdio":
		return "stdio", nil
	case "sse":
		return "sse", nil
	case "http", "streamablehttp":
		return "streamable_http", nil
	default:
		return "", mcpJSONError("mcp: transport must be stdio, sse or http")
	}
}

func inferMCPTransport(explicit, command, endpoint string) (string, error) {
	hasCommand := strings.TrimSpace(command) != ""
	hasURL := strings.TrimSpace(endpoint) != ""
	switch explicit {
	case "":
		if hasCommand && hasURL {
			return "", mcpJSONError("mcp: a server cannot set both a command and a URL")
		}
		if hasURL {
			return "streamable_http", nil
		}
		return "stdio", nil
	case "stdio":
		if hasURL {
			return "", mcpJSONError("mcp: a server cannot set both a command and a URL")
		}
		return "stdio", nil
	case "sse", "streamable_http":
		if hasCommand {
			return "", mcpJSONError("mcp: a server cannot set both a command and a URL")
		}
		return explicit, nil
	default:
		return "", mcpJSONError("mcp: transport must be stdio, sse or http")
	}
}

func resolveStdioCommand(command string, args []string, argsSet bool) (string, []string, error) {
	command = strings.TrimSpace(command)
	if !argsSet {
		args = nil
	}
	if len(args) == 0 && strings.ContainsAny(command, " \t\"'") {
		argv, err := splitCommandLine(command)
		if err != nil {
			return "", nil, err
		}
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			return "", nil, mcpJSONError("mcp: stdio requires a command")
		}
		rest := argv[1:]
		if rest == nil {
			rest = []string{}
		}
		return argv[0], rest, nil
	}
	if command == "" {
		return "", nil, mcpJSONError("mcp: stdio requires a command")
	}
	if args == nil {
		args = []string{}
	}
	return command, args, nil
}

// splitCommandLine splits one program line into argv. It honors quotes and
// backslash escapes. Characters that would mean a shell command are rejected.
func splitCommandLine(line string) ([]string, error) {
	var tokens []string
	var b strings.Builder
	var quote byte
	started := false
	flush := func() {
		if !started {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
		started = false
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
				continue
			}
			b.WriteByte(c)
			continue
		}
		if quote == '"' {
			if c == '\\' {
				if i+1 >= len(line) {
					return nil, mcpJSONError("mcp: command line has an unfinished quote")
				}
				i++
				if line[i] == '\n' || line[i] == '\r' {
					return nil, mcpJSONError("mcp: command line must not use shell syntax")
				}
				b.WriteByte(line[i])
				continue
			}
			if c == '"' {
				quote = 0
				continue
			}
			if c == '\n' || c == '\r' {
				return nil, mcpJSONError("mcp: command line must not use shell syntax")
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case ' ', '\t':
			flush()
		case '\n', '\r', '|', '&', ';', '<', '>', '`', '$':
			return nil, mcpJSONError("mcp: command line must not use shell syntax")
		case '\\':
			if i+1 >= len(line) {
				return nil, mcpJSONError("mcp: command line has an unfinished quote")
			}
			i++
			if line[i] == '\n' || line[i] == '\r' {
				return nil, mcpJSONError("mcp: command line must not use shell syntax")
			}
			b.WriteByte(line[i])
			started = true
		case '\'', '"':
			quote = c
			started = true
		default:
			b.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, mcpJSONError("mcp: command line has an unfinished quote")
	}
	flush()
	return tokens, nil
}

func matchMCPServer(existing []MCPServer, claimed map[int]bool, created map[string]bool, entry mcpJSONSource, server parsedMCPServer) (int, string, bool, error) {
	if server.idSet {
		if !mcpIDPattern.MatchString(server.id) {
			return 0, "", false, mcpJSONError("mcp: id must contain 1–32 letters, digits, underscores or hyphens")
		}
		if created[server.id] {
			return 0, "", false, mcpJSONError("mcp: duplicate server")
		}
		if index := mcpServerIndex(existing, server.id); index >= 0 {
			if claimed[index] {
				return 0, "", false, mcpJSONError("mcp: duplicate server")
			}
			return index, server.id, false, nil
		}
		return -1, server.id, true, nil
	}
	if entry.hasKey && mcpIDPattern.MatchString(entry.key) {
		if index := mcpServerIndex(existing, entry.key); index >= 0 {
			if claimed[index] {
				return 0, "", false, mcpJSONError("mcp: duplicate server")
			}
			return index, entry.key, false, nil
		}
	}
	matches := make([]int, 0, 1)
	for index, current := range existing {
		if !claimed[index] && current.Name == server.name {
			matches = append(matches, index)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], existing[matches[0]].ID, false, nil
	default:
		if len(matches) > 1 {
			return 0, "", false, mcpJSONError("mcp: more than one saved server has this name")
		}
	}
	if entry.hasKey && mcpIDPattern.MatchString(entry.key) {
		if created[entry.key] {
			return 0, "", false, mcpJSONError("mcp: duplicate server")
		}
		return -1, entry.key, true, nil
	}
	id, err := newMCPServerID(existing, created)
	return -1, id, true, err
}

func mcpServerIndex(servers []MCPServer, id string) int {
	for i, server := range servers {
		if server.ID == id {
			return i
		}
	}
	return -1
}

func newMCPServerID(existing []MCPServer, created map[string]bool) (string, error) {
	used := make(map[string]bool, len(existing)+len(created))
	for _, server := range existing {
		used[server.ID] = true
	}
	for id := range created {
		used[id] = true
	}
	for range 8 {
		var buf [16]byte
		n, err := mcpIDEntropy(buf[:])
		if err != nil || n != len(buf) {
			return "", mcpJSONError("mcp: could not allocate a server id")
		}
		id := hex.EncodeToString(buf[:])
		if mcpIDPattern.MatchString(id) && !used[id] {
			return id, nil
		}
		used[id] = true
	}
	return "", mcpJSONError("mcp: could not allocate a server id")
}

func optionalJSONString(obj map[string]json.RawMessage, key string) (string, bool, error) {
	raw, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false, nil
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return "", false, mcpJSONError("mcp: expected a string")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false, err
	}
	return text, true, nil
}

func optionalJSONBool(obj map[string]json.RawMessage, key string) (*bool, error) {
	raw, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		on := true
		return &on, nil
	case "false":
		on := false
		return &on, nil
	default:
		return nil, mcpJSONError("mcp: expected a boolean")
	}
}

func optionalJSONInt(obj map[string]json.RawMessage, key string) (*int, error) {
	raw, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	original := bytes.TrimSpace(raw)
	digits := original
	if len(digits) > 0 && digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) == 0 || digits[0] < '0' || digits[0] > '9' {
		return nil, mcpJSONError("mcp: timeout_seconds must be a number")
	}
	var number int
	if err := json.Unmarshal(original, &number); err != nil {
		return nil, mcpJSONError("mcp: timeout_seconds must be a number")
	}
	return &number, nil
}

func parseJSONArgs(raw json.RawMessage, present bool) ([]string, bool, error) {
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false, mcpJSONError("mcp: arguments must be a JSON array of strings")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, false, mcpJSONError("mcp: arguments must be a JSON array of strings")
	}
	args := make([]string, 0, len(items))
	for _, item := range items {
		if len(bytes.TrimSpace(item)) == 0 || bytes.TrimSpace(item)[0] != '"' {
			return nil, false, mcpJSONError("mcp: arguments must be a JSON array of strings")
		}
		var text string
		if err := json.Unmarshal(item, &text); err != nil {
			return nil, false, mcpJSONError("mcp: arguments must be a JSON array of strings")
		}
		args = append(args, text)
	}
	return args, true, nil
}

func parseJSONStringMap(obj map[string]json.RawMessage, key, kind string) (map[string]string, bool, error) {
	raw, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false, mcpJSONError("mcp: %s must be a JSON object", kind)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, false, mcpJSONError("mcp: %s must be a JSON object", kind)
	}
	out := make(map[string]string, len(fields))
	for name, value := range fields {
		if len(bytes.TrimSpace(value)) == 0 || bytes.TrimSpace(value)[0] != '"' {
			return nil, false, mcpJSONError("mcp: %s values must be strings", kind)
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, false, mcpJSONError("mcp: %s values must be strings", kind)
		}
		out[name] = text
	}
	return out, true, nil
}
