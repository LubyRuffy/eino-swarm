package knowledge

import (
	"encoding/json"
	"strings"
)

const maxWikiJSONDepth = 64
const maxWikiStructuredQuote = 768

type wikiJSONValue struct {
	start, end int
	text       string
	node       string
}

type wikiJSONScanner struct {
	text    string
	decoder *json.Decoder
	graph   *wikiLocalGraph
}

func (g *wikiLocalGraph) read(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid([]byte(trimmed)) {
		scan := wikiJSONScanner{trimmed, json.NewDecoder(strings.NewReader(trimmed)), g}
		scan.decoder.UseNumber()
		scan.value(0, true)
		return
	}
	// Malformed object output remains for review. Its fields cannot be
	// interpreted safely enough to extract identifiers from their values.
	if trimmed[0] == '{' || strings.HasPrefix(trimmed, "[{") {
		return
	}
	// Review responses may quote a single typed field without its enclosing
	// object. Parse that fragment with the same rules.
	if trimmed[0] == '"' && strings.Contains(trimmed, ":") {
		object := "{" + trimmed + "}"
		if json.Valid([]byte(object)) {
			scan := wikiJSONScanner{object, json.NewDecoder(strings.NewReader(object)), g}
			scan.decoder.UseNumber()
			scan.value(0, true)
			return
		}
	}
	g.readLiterals(text)
}

func (s wikiJSONScanner) start() int {
	offset := int(s.decoder.InputOffset())
	return offset + len(s.text[offset:]) - len(strings.TrimLeft(s.text[offset:], " \r\n\t:,"))
}

func (s wikiJSONScanner) value(depth int, readLiterals bool) wikiJSONValue {
	start := s.start()
	if depth == maxWikiJSONDepth {
		s.skip()
		return wikiJSONValue{start: start, end: int(s.decoder.InputOffset())}
	}
	value, _ := s.decoder.Token()
	switch value {
	case json.Delim('{'):
		return s.object(start, depth)
	case json.Delim('['):
		for s.decoder.More() {
			s.value(depth+1, readLiterals)
		}
		_, _ = s.decoder.Token()
	}
	end := int(s.decoder.InputOffset())
	item := wikiJSONValue{start: start, end: end}
	if text, ok := value.(string); ok {
		item.text = text
		if readLiterals {
			s.graph.readLiterals(s.text[start+1 : end-1])
		}
	}
	return item
}

func (s wikiJSONScanner) skip() {
	var ignored json.RawMessage
	_ = s.decoder.Decode(&ignored)
}

type wikiJSONMember struct {
	key   string
	start int
	value wikiJSONValue
}

func (s wikiJSONScanner) object(start, depth int) wikiJSONValue {
	var members []wikiJSONMember
	all := 0
	duplicates := make(map[string]bool)
	keys := make(map[string]bool)
	for s.decoder.More() {
		keyStart := s.start()
		token, _ := s.decoder.Token()
		key := strings.ToLower(token.(string))
		all++
		switch key {
		case "arguments", "args", "command", "cmd", "full_command", "query", "authorization", "cookie", "password", "secret", "token", "api_key", "apikey", "private_key":
			s.skip()
			continue
		}
		known := wikiStructuredCategory(key) != "" || key == "name" || key == "type" || key == "from" || key == "to" || key == "relation"
		item := s.value(depth+1, !known)
		if known {
			duplicates[key] = keys[key]
			keys[key] = true
			members = append(members, wikiJSONMember{key: key, start: keyStart, value: item})
		}
	}
	_, _ = s.decoder.Token()
	end := int(s.decoder.InputOffset())
	item := wikiJSONValue{start: start, end: end}
	fields := make(map[string]wikiJSONMember)
	for _, member := range members {
		if duplicates[member.key] {
			continue
		}
		fields[member.key] = member
		quote := s.text[member.start:member.value.end]
		if category := wikiStructuredCategory(member.key); category != "" {
			s.typedNode(category, member.value.text, quote)
		}
	}
	kind, name := fields["type"], fields["name"]
	if kind.key != "" && name.key != "" {
		left, right := kind, name
		if right.start < left.start {
			left, right = right, left
		}
		if strings.TrimSpace(s.text[left.value.end:right.start]) == "," {
			item.node = s.typedNode(wikiStructuredCategory(strings.ToLower(kind.value.text)), name.value.text, s.text[left.start:right.value.end])
		}
	}
	if relation := fields["relation"].value.text; all == 3 && len(fields) == 3 && wikiDirectRelation(relation) &&
		end-start <= maxWikiStructuredQuote {
		s.graph.link(fields["from"].value.node, fields["to"].value.node, s.text[start:end])
	}
	return item
}

func wikiDirectRelation(label string) bool {
	if !wikiObservationLabel(label) {
		return false
	}
	for _, ch := range strings.ToLower(label) {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(label), func(ch rune) bool { return ch == '_' || ch == '-' }) {
		switch word {
		case "no", "not", "never", "negated", "unrelated", "possible", "maybe", "scheduled", "pending", "planned", "unknown", "denied":
			return false
		}
	}
	return strings.ContainsAny(strings.ToLower(label), "abcdefghijklmnopqrstuvwxyz")
}

func (s wikiJSONScanner) typedNode(category, title, quote string) string {
	if category == "" || !wikiObservationLabel(title) || len(quote) > maxWikiStructuredQuote || !strings.Contains(quote, title) {
		return ""
	}
	if category == wikiHostCategory {
		if address, ok := wikiTypedIdentity(wikiIPCategory, title); ok {
			return s.graph.add(address, quote)
		}
	}
	if identity, ok := wikiTypedIdentity(category, title); ok {
		return s.graph.add(identity, quote)
	}
	identity := wikiIdentity{title: title, kind: "entity", category: category}
	switch category {
	case "organization", "person", "postal-address":
	case "event":
		identity.kind = "event"
	default:
		return ""
	}
	return s.graph.add(identity, quote)
}

func wikiObservationLabel(label string) bool {
	return label != "" && len(label) <= 240 && strings.TrimSpace(label) == label && !strings.ContainsAny(label, "\r\n\t\x00")
}

func wikiStructuredCategory(key string) string {
	switch key {
	case "domain", "host", "hostname":
		return wikiHostCategory
	case "ip", "ip_address", wikiIPCategory:
		return wikiIPCategory
	case "cidr", wikiNetworkCategory:
		return wikiNetworkCategory
	case "cve", "cve_id", wikiVulnerabilityCategory:
		return wikiVulnerabilityCategory
	case "certificate_fingerprint", "certificate_sha256", wikiCertificateCategory:
		return wikiCertificateCategory
	case "organization", "organization_name", "company", "company_name":
		return "organization"
	case "person", "person_name":
		return "person"
	case "postal_address", "street_address", "postal-address":
		return "postal-address"
	case "event", "event_name":
		return "event"
	default:
		return ""
	}
}
