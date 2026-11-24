package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	wikiHostCategory          = "hostname"
	wikiIPCategory            = "ip-address"
	wikiNetworkCategory       = "ip-network"
	wikiVulnerabilityCategory = "vulnerability"
	wikiCertificateCategory   = "certificate"
)

var wikiLiteral = regexp.MustCompile(`(?i)https?://[^\x00-\x20<>"\\]+|\bCVE-[0-9]{4}-[0-9]{4,19}\b|[0-9A-F]{0,4}:(?:[0-9A-F]{0,4}:){1,7}[0-9A-F:.]{0,45}(?:/[0-9]{1,3})?|[0-9]{1,3}(?:\.[0-9]{1,3}){3}(?:/[0-9]{1,3})?`)
var wikiVulnerability = regexp.MustCompile(`(?i)^CVE-[0-9]{4}-[0-9]{4,19}$`)

type wikiIdentity struct {
	title, kind, category string
	stable                bool
}

func (i wikiIdentity) id(source Source) string {
	identity := strings.ToLower(i.title)
	if !i.stable {
		identity = fmt.Sprintf("%s/%d/%s", source.ThreadID, source.Seq, source.RecordID)
	}
	sum := sha256.Sum256([]byte(i.kind + "\x00" + i.category + "\x00" + strings.ToLower(i.title) + "\x00" + identity))
	return "wiki-" + hex.EncodeToString(sum[:16])
}

type wikiLocalGraph struct {
	source Source
	nodes  map[string]*WikiNode
	links  map[[2]string]bool
}

// ExtractWikiObservations finds explicit typed facts in a source the caller
// already admitted. Each node keeps one exact representative quotation from
// this source. Plain identifier mentions establish no relationships.
func ExtractWikiObservations(text string, source Source) []WikiNode {
	graph := &wikiLocalGraph{source: source, nodes: make(map[string]*WikiNode)}
	graph.read(text)
	nodes := make([]WikiNode, 0, len(graph.nodes))
	for _, id := range slices.Sorted(maps.Keys(graph.nodes)) {
		nodes = append(nodes, *graph.nodes[id])
	}
	return nodes
}

// ResolveWikiIdentifier shares canonical IDs with semantic review when its
// evidence explicitly identifies the same globally distinguishable subject.
func ResolveWikiIdentifier(title, quote string) (id, category, canonicalTitle string, ok bool) {
	graph := &wikiLocalGraph{nodes: make(map[string]*WikiNode)}
	graph.read(quote)
	for _, node := range graph.nodes {
		switch node.Category {
		case wikiHostCategory, wikiIPCategory, wikiNetworkCategory, wikiVulnerabilityCategory, wikiCertificateCategory:
			identity, recognized := wikiTypedIdentity(node.Category, title)
			if recognized && identity.stable && identity.title == node.Title && identity.id(Source{}) == node.ID {
				return node.ID, node.Category, node.Title, true
			}
		}
	}
	return "", "", "", false
}

func (g *wikiLocalGraph) add(identity wikiIdentity, quote string) string {
	id := identity.id(g.source)
	if _, exists := g.nodes[id]; !exists {
		g.nodes[id] = &WikiNode{
			ID: id, Title: identity.title, Category: identity.category, Kind: identity.kind,
			Evidence: []WikiEvidence{{Source: g.source, Quote: quote}},
		}
	}
	return id
}

func (g *wikiLocalGraph) link(a, b, quote string) {
	if a == b || g.nodes[a] == nil || g.nodes[b] == nil {
		return
	}
	a, b = min(a, b), max(a, b)
	pair := [2]string{a, b}
	if g.links == nil {
		g.links = make(map[[2]string]bool)
	}
	if !g.links[pair] {
		g.nodes[a].Evidence = append(g.nodes[a].Evidence, WikiEvidence{Source: g.source, Quote: quote, To: b})
		g.links[pair] = true
	}
}

func (g *wikiLocalGraph) readLiterals(text string) {
	for _, match := range wikiLiteral.FindAllStringIndex(text, -1) {
		quote := text[match[0]:match[1]]
		if len(quote) >= 7 && strings.EqualFold(quote[:4], "http") {
			if identity, authority, ok := wikiAuthority(quote); ok {
				g.add(identity, authority)
			}
			continue
		}
		quote = strings.TrimRight(quote, ".")
		end := match[0] + len(quote)
		if match[0] > 0 && wikiIdentifierByte(text[match[0]-1]) ||
			end < len(text) && wikiIdentifierByte(text[end]) && text[end] != '.' {
			continue
		}
		if end < len(text)-1 && text[end] == '.' && wikiIdentifierByte(text[end+1]) {
			continue
		}
		if identity, ok := wikiTypedIdentity("", quote); ok {
			g.add(identity, quote)
		}
	}
}

func wikiIdentifierByte(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' ||
		ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':' || ch == '/'
}

func wikiAuthority(text string) (wikiIdentity, string, bool) {
	start := strings.Index(text, "://") + 3
	end := strings.IndexAny(text[start:], "/?#")
	if end >= 0 {
		text = text[:start+end]
	} else {
		text = strings.TrimRight(text, ".,;!)}")
	}
	if len(text) > 300 || strings.Contains(text[start:], "@") {
		return wikiIdentity{}, "", false
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.User != nil {
		return wikiIdentity{}, "", false
	}
	host := parsed.Hostname()
	if identity, ok := wikiTypedIdentity(wikiIPCategory, host); ok {
		return identity, text, true
	}
	identity, ok := wikiTypedIdentity(wikiHostCategory, host)
	return identity, text, ok
}

func wikiTypedIdentity(category, title string) (wikiIdentity, bool) {
	identity := wikiIdentity{title: title, kind: "entity", category: category, stable: true}
	switch category {
	case "":
		for _, candidate := range []string{wikiVulnerabilityCategory, wikiNetworkCategory, wikiIPCategory} {
			if resolved, ok := wikiTypedIdentity(candidate, title); ok {
				return resolved, true
			}
		}
	case wikiHostCategory:
		identity.title = strings.ToLower(strings.TrimSuffix(title, "."))
		if wikiDNSName(identity.title) {
			return identity, true
		}
	case wikiIPCategory:
		if ip, err := netip.ParseAddr(title); err == nil && ip.Zone() == "" {
			identity.title = ip.String()
			return identity, true
		}
	case wikiNetworkCategory:
		if network, err := netip.ParsePrefix(title); err == nil && network == network.Masked() {
			identity.title = network.String()
			return identity, true
		}
	case wikiVulnerabilityCategory:
		if wikiVulnerability.MatchString(title) {
			identity.title = strings.ToUpper(title)
			return identity, true
		}
	case wikiCertificateCategory:
		hexValue := strings.ReplaceAll(title, ":", "")
		decoded, err := hex.DecodeString(hexValue)
		if err == nil && len(decoded) == sha256.Size && (len(title) == sha256.Size*2 || wikiFingerprintFormat(title)) {
			identity.title = strings.ToLower(hexValue)
			return identity, true
		}
	}
	return wikiIdentity{}, false
}

func wikiDNSName(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	// A decimal last label belongs to address syntax rather than a DNS name.
	tail := host[strings.LastIndexByte(host, '.')+1:]
	return strings.ContainsAny(tail, "abcdefghijklmnopqrstuvwxyz")
}

func wikiFingerprintFormat(title string) bool {
	fields := strings.Split(title, ":")
	if len(fields) != sha256.Size {
		return false
	}
	for _, field := range fields {
		if len(field) != 2 {
			return false
		}
	}
	return true
}
