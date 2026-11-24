package knowledge

import (
	"slices"
	"strings"
	"testing"
)

func TestLocalWikiObservationsKeepOnlyExplicitAttributableIdentities(t *testing.T) {
	const text = "Observed HTTPS://WORKSPACE.TEST:443/path?key=private-value, 192.0.2.40 and 2001:db8::1. " +
		"Network 198.51.100.0/25 and CVE-2024-12345. Ignore 999.1.2.3, README.md, A192.0.2.99z and " +
		"https://operator:secret@private.invalid/203.0.113.90. Plain co-occurrence does not assert a relation."
	source := Source{Kind: "event", ThreadID: "thread", Seq: 7, RecordID: "event-seven"}
	nodes := ExtractWikiObservations(text, source)
	want := []string{"192.0.2.40", "198.51.100.0/25", "2001:db8::1", "CVE-2024-12345", "workspace.test"}
	var titles []string
	for _, n := range nodes {
		titles = append(titles, n.Title)
		if !ValidSlug(n.ID) || n.Kind != "entity" || len(n.Evidence) != 1 {
			t.Fatalf("invalid extracted node %#v", n)
		}
		ev := n.Evidence[0]
		if ev.Source != source || ev.To != "" || !strings.Contains(text, ev.Quote) ||
			strings.Contains(ev.Quote, "private-value") || strings.Contains(ev.Quote, "secret") {
			t.Fatalf("invalid source quote %#v", ev)
		}
	}
	slices.Sort(titles)
	if !slices.Equal(titles, want) {
		t.Fatalf("local titles %q, want %q", titles, want)
	}
}

func TestLocalWikiStructureRequiresExplicitRelationAndKeepsWeakNamesSeparate(t *testing.T) {
	text := `{"company_name":"Acme Supply","postal_address":"17 Garden Road","cve_id":"CVE-2024-12345",
	"person_name":"Example 梅","command":"https://command.invalid",
	"token":"https://token.invalid",
	"certificate_fingerprint":"AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99",
	"same_object":{"person":"Adjacent A","company":"Adjacent B"},
	"explicit":{"from":{"type":"person","name":"Casey 名"},"relation":"attended","to":{"type":"event","name":"Open day"}},
	"without_relation":{"from":{"type":"person","name":"Unknown"},"to":{"type":"event","name":"Unrelated"}},
	"duplicate":{"event_name":"Ambiguous first","event_name":"Ambiguous last"}}`
	source := Source{Kind: "event", ThreadID: "thread", Seq: 7, RecordID: "event-seven"}
	nodes := ExtractWikiObservations(text, source)
	byTitle := make(map[string]WikiNode)
	edges := 0
	for _, node := range nodes {
		byTitle[node.Title] = node
		for _, ev := range node.Evidence {
			if ev.Source != source || !strings.Contains(text, ev.Quote) {
				t.Fatalf("lost exact quote or provenance %#v", ev)
			}
			if ev.To != "" {
				edges++
				if !strings.Contains(ev.Quote, `"relation":"attended"`) {
					t.Fatalf("co-occurrence created link %#v", ev)
				}
			}
		}
	}
	for _, title := range []string{"Acme Supply", "17 Garden Road", "Example 梅", "Casey 名", "Open day", "Adjacent A", "Adjacent B", "CVE-2024-12345", "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"} {
		if _, ok := byTitle[title]; !ok {
			t.Errorf("explicitly typed node %q absent", title)
		}
	}
	for _, title := range []string{"command.invalid", "token.invalid", "Ambiguous first", "Ambiguous last"} {
		if _, ok := byTitle[title]; ok {
			t.Errorf("invalid candidate %q", title)
		}
	}
	if byTitle["Open day"].Kind != "event" || len(nodes) == 0 || edges != 1 {
		t.Fatalf("events or explicit edges lost, nodes=%d edges=%d", len(nodes), edges)
	}
	other := ExtractWikiObservations(`{"person_name":"Example 梅"}`, Source{Kind: "event", ThreadID: "thread", Seq: 8, RecordID: "event-eight"})
	if len(other) != 1 || other[0].ID == byTitle["Example 梅"].ID {
		t.Fatalf("same name in distinct sources collapsed %#v", other)
	}
}

func TestWikiIdentifierBindsModelToTheSameLiteralIdentity(t *testing.T) {
	testCases := []struct{ title, quote, canonical, category string }{
		{"WORKSPACE.TEST", "from https://workspace.test/", "workspace.test", "hostname"},
		{"192.0.2.40", "Observed 192.0.2.40", "192.0.2.40", "ip-address"},
		{"2001:0db8:0000::1", "Observed 2001:db8::1", "2001:db8::1", "ip-address"},
		{"198.51.100.0/25", "198.51.100.0/25", "198.51.100.0/25", "ip-network"},
		{"cve-2024-12345", "CVE-2024-12345", "CVE-2024-12345", "vulnerability"},
		{"WORKSPACE.TEST", `"domain": "workspace.test"`, "workspace.test", "hostname"},
	}
	for _, tc := range testCases {
		id, category, title, ok := ResolveWikiIdentifier(tc.title, tc.quote)
		if !ok || title != tc.canonical || category != tc.category || !ValidSlug(id) {
			t.Errorf("resolve %q yielded (%q, %q, %q, %t)", tc.title, id, category, title, ok)
		}
		nodes := ExtractWikiObservations(tc.quote, Source{Kind: "event", ThreadID: "test", Seq: 1})
		if len(nodes) != 1 || nodes[0].ID != id {
			t.Errorf("model and local identifiers diverged for %q", tc.title)
		}
	}
	for _, pair := range [][2]string{{"workspace.test", "file workspace.test"}, {"192.0.2.40", "192.0.2.4"}, {"Example 梅", `{"person":"Example 梅"}`}, {"https://workspace.test/resource", "https://workspace.test/resource"}} {
		if id, _, _, ok := ResolveWikiIdentifier(pair[0], pair[1]); ok {
			t.Errorf("weak or different-kind label %q was mapped to %q", pair[0], id)
		}
	}
}

func TestLocalWikiReportsNoFactFromMalformedOrUnconfirmedStructuredOutput(t *testing.T) {
	source := Source{Kind: "event", ThreadID: "thread", Seq: 7}
	for i, raw := range []string{
		"",
		`{"token":"https://private.invalid"`,
		`[{"token":"https://private.invalid"`,
		`{"name":"Loose name","untyped":"Loose value","certificate_sha256":"invalid"}`,
		`{"person_name":" leading space","event_name":"A\nB","certificate":"aabb:ccdd:eefe:1122:3344:5566:7788:9900"}`,
		strings.Repeat("[", 65) + `"https://deep.invalid"` + strings.Repeat("]", 65),
		`https://bad-.invalid https://empty..invalid https://local https://bad%41.invalid https://192.000.2.11`,
	} {
		if nodes := ExtractWikiObservations(raw, source); len(nodes) != 0 {
			t.Errorf("unsupported source %d produced %d local observations", i, len(nodes))
		}
	}
	const unconfirmed = `[{"from":{"name":"First","type":"person"},"relation":"not_related","to":{"type":"event","name":"Test event"}},
	{"from":{"type":"person","name":"First"},"relation":"possibly part of","to":{"type":"event","name":"Test event"}},
	{"from":{"type":"person","name":"First"},"relation":"101","to":{"type":"event","name":"Test event"}},
	{"from":{"type":"person","name":"First"},"relation":"mentions","to":{"type":"person","name":"First"}}]`
	nodes := ExtractWikiObservations(unconfirmed, source)
	if len(nodes) != 2 {
		t.Fatalf("typed endpoints should remain visible: %+v", nodes)
	}
	for _, node := range nodes {
		if len(node.Evidence) != 1 || node.Evidence[0].To != "" {
			t.Fatalf("unconfirmed or self relationship created a line: %+v", node)
		}
	}
}

func BenchmarkLocalWikiObservations(b *testing.B) {
	text := strings.Repeat("Observed https://workspace.test/status and 192.0.2.40 on 198.51.100.0/25. Unstructured observations remain available.\n", 400)
	source := Source{Kind: "event", ThreadID: "thread", Seq: 7, RecordID: "event-seven"}
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		if nodes := ExtractWikiObservations(text, source); len(nodes) != 3 {
			b.Fatalf("candidate loss or duplicate publication simulation: %d", len(nodes))
		}
	}
}
