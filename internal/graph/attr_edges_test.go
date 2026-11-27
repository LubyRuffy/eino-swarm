package graph

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// A model writes numbers as JSON numbers, strings or floats depending on the
// provider; all of them must land as the same stored integer, and a bad value
// must be refused instead of silently becoming zero.
func TestIntegerAttributesAcceptEverySpellingOfAWholeNumber(t *testing.T) {
	k := kind(t, "asset")
	for name, raw := range map[string]any{
		"int": 443, "int64": int64(443), "float": float64(443), "json number": json.Number("443"), "text": " 443 ",
	} {
		got, err := k.Clean(map[string]any{"ip": "203.0.113.7", "port": raw})
		if err != nil || got["port"] != int64(443) {
			t.Errorf("%s: port = %v, %v", name, got["port"], err)
		}
	}
	for name, raw := range map[string]any{
		"fraction": 44.5, "not a number": "https", "bool": true, "below min": 0, "huge float": 1e300, "bad json number": json.Number("1.5"),
	} {
		if _, err := k.Clean(map[string]any{"ip": "203.0.113.7", "port": raw}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s was not refused: %v", name, err)
		}
	}
	got, err := k.Clean(map[string]any{"ip": "203.0.113.7", "port": "  "})
	if err != nil || got["port"] != nil {
		t.Errorf("a blank port must read as absent: %v, %v", got, err)
	}
}

func TestListAttributesDropBlanksAndDuplicatesAndRefuseOversizedItems(t *testing.T) {
	k := kind(t, "person")
	got, err := k.Clean(map[string]any{"name": "Ada", "email": []any{"a@x.test", " ", "a@x.test", "b@x.test"}})
	if err != nil || !reflect.DeepEqual(got["email"], []string{"a@x.test", "b@x.test"}) {
		t.Fatalf("list not cleaned: %v, %v", got["email"], err)
	}
	got, err = k.Clean(map[string]any{"name": "Ada", "email": []string{" "}})
	if err != nil || got["email"] != nil {
		t.Fatalf("an all-blank list must read as absent: %v, %v", got, err)
	}
	many := make([]any, MaxListItems+1)
	for i := range many {
		many[i] = strings.Repeat("a", i+1) + "@x.test"
	}
	for name, raw := range map[string]any{
		"too many items": many, "item too long": []any{strings.Repeat("a", DefaultStringMax+1)},
		"nested list": []any{[]any{"a", "b"}}, "object": map[string]any{"a": 1},
	} {
		if _, err := k.Clean(map[string]any{"name": "Ada", "email": raw}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s was not refused: %v", name, err)
		}
	}
}

func TestSingleValueAttributesRefuseListsAndNumbersBecomeText(t *testing.T) {
	k := kind(t, "other")
	if _, err := k.Clean(map[string]any{"name": []any{"a", "b"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a two-item list for a text attribute was accepted: %v", err)
	}
	got, err := k.Clean(map[string]any{"name": 42})
	if err != nil || got["name"] != "42" {
		t.Fatalf("a number for a text attribute: %v, %v", got, err)
	}
}

// Rows are stored as JSON, so what the database returns has lists as []any and
// numbers as float64. Normalized must turn that back into the cleaned form and
// drop what no longer belongs to the kind.
func TestNormalizedRestoresTheCleanedFormAfterAJSONRoundTrip(t *testing.T) {
	k := kind(t, "asset")
	var stored map[string]any
	if err := json.Unmarshal([]byte(`{"ip":"203.0.113.7","port":443,"removed_attr":"x","host":""}`), &stored); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"ip": "203.0.113.7", "port": int64(443)}
	if got := k.Normalized(stored); !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalized = %v, want %v", got, want)
	}
}

func TestRedactStripsOnlyCredentialAttributesAndNamesThem(t *testing.T) {
	c := MustDefault()
	attrs := map[string]any{"path": "acme/app", "credential": "token-value"}
	got, present := c.Redact("repo", attrs)
	if _, leaked := got["credential"]; leaked || got["path"] != "acme/app" || !reflect.DeepEqual(present, []string{"credential"}) {
		t.Fatalf("Redact = %v, %v", got, present)
	}
	if _, ok := attrs["credential"]; !ok {
		t.Fatal("Redact modified its input")
	}
	for _, id := range []string{"domain", "no-such-kind"} {
		got, present := c.Redact(id, map[string]any{"domain": "example.com"})
		if len(present) != 0 || got["domain"] != "example.com" {
			t.Errorf("%s: nothing to redact, got %v, %v", id, got, present)
		}
	}
}

func TestDisplayNameUsesTheFirstRuleWithAllItsFields(t *testing.T) {
	k := kind(t, "asset")
	for name, tc := range map[string]struct {
		attrs map[string]any
		want  string
	}{
		"host first":    {map[string]any{"host": "a.example.com", "ip": "203.0.113.7", "port": int64(443)}, "a.example.com"},
		"ip and port":   {map[string]any{"ip": "203.0.113.7", "port": float64(443)}, "203.0.113.7:443"},
		"list value":    {map[string]any{"host": []any{"a.example.com"}}, "a.example.com"},
		"not enough":    {map[string]any{"ip": "203.0.113.7"}, ""},
		"unusable type": {map[string]any{"host": true}, ""},
	} {
		if got := k.DisplayName(tc.attrs); got != tc.want {
			t.Errorf("%s: DisplayName = %q, want %q", name, got, tc.want)
		}
	}
	if got := kind(t, "company").DisplayName(map[string]any{"reg_no": []string{"91310000"}}); got != "91310000" {
		t.Errorf("a []string value must display its first item, got %q", got)
	}
	if got := kind(t, "company").DisplayName(map[string]any{"name": 7}); got != "7" {
		t.Errorf("an int value must display as text, got %q", got)
	}
}

func TestParseCatalogRefusesMalformedDocuments(t *testing.T) {
	for name, doc := range map[string]string{
		"not json":                   `{`,
		"no kinds":                   `{"kinds":[]}`,
		"bad kind id":                `{"kinds":[{"id":"Bad Id","attrs":[],"keys":[]}]}`,
		"unknown type":               `{"kinds":[{"id":"a","attrs":[{"name":"x","type":"blob"}],"keys":[{"name":"x","parts":["x"]}]}]}`,
		"key on a missing attribute": `{"kinds":[{"id":"a","attrs":[{"name":"x","type":"string"}],"keys":[{"name":"k","parts":["y"]}]}]}`,
	} {
		if _, err := ParseCatalog([]byte(doc)); err == nil {
			t.Errorf("%s: catalog was accepted", name)
		}
	}
}

func TestNormalizersRefuseMalformedInput(t *testing.T) {
	for name, fn := range map[string]func() error{
		"domain label starting with a dash": func() error { _, err := NormalizeDomain("-a.example.com"); return err },
		"domain with a space":               func() error { _, err := NormalizeDomain("a b.example.com"); return err },
		"domain that is an IP":              func() error { _, err := NormalizeDomain("203.0.113.7"); return err },
		"host with a path":                  func() error { _, err := NormalizeHost("a.example.com/x"); return err },
		"empty host":                        func() error { _, err := NormalizeHost(" . "); return err },
		"repo with no path":                 func() error { _, err := NormalizeRepo("https://git.example.com/"); return err },
		"repo path that is a URL":           func() error { _, err := normalizeRepoPath("https://git.example.com/a/b"); return err },
		"repo path that is blank":           func() error { _, err := normalizeRepoPath(" / "); return err },
		"url with no host":                  func() error { _, err := NormalizeURL("https:///x"); return err },
	} {
		if err := fn(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s was not refused: %v", name, err)
		}
	}
}

func TestRepositoryAddressSpellingsShareOnePathAndDefaultPortsDisappear(t *testing.T) {
	for _, address := range []string{
		"https://git.example.com/acme/app.git", "ssh://git@git.example.com:22/acme/app", "git@git.example.com:acme/app.git",
	} {
		got, err := NormalizeRepo(address)
		if err != nil || got != "git.example.com/acme/app" || RepoPath(address) != "acme/app" {
			t.Errorf("%s: %q, %v, path %q", address, got, err, RepoPath(address))
		}
	}
	if got, _ := NormalizeRepo("https://git.example.com:8443/acme/app"); got != "git.example.com:8443/acme/app" {
		t.Errorf("a custom port is part of the host: %q", got)
	}
	if RepoPath("not an address") != "" {
		t.Error("an unparsable address has no path")
	}
	if got, _ := NormalizeURL("https://[2001:db8::1]/x"); got != "https://[2001:db8::1]/x" {
		t.Errorf("an IPv6 origin keeps its brackets: %q", got)
	}
	if got, _ := NormalizeURL("http://Example.com:8080/a?b=1#frag"); got != "http://example.com:8080/a?b=1" {
		t.Errorf("custom port kept, fragment dropped: %q", got)
	}
}

// Evidence is deduplicated by what it points at; a URL and an event never
// collide, and a batch with nothing in it or an unknown shape is refused.
func TestEvidenceKeyAndBatchValidityDistinguishWhatTheyPointAt(t *testing.T) {
	url := Evidence{URL: "https://news.example.com/a", ThreadID: "t", Seq: 1}
	event := Evidence{ThreadID: "t", Seq: 1}
	if url.Key() == event.Key() || event.Key() != (Evidence{ThreadID: "t", Seq: 1, Label: "other"}).Key() {
		t.Fatalf("keys: %q %q", url.Key(), event.Key())
	}
	if err := (Batch{}).Valid(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty batch was accepted: %v", err)
	}
}
