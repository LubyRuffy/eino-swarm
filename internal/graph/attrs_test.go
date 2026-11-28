package graph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func kind(t *testing.T, id string) *Kind {
	t.Helper()
	k := MustDefault().Kind(id)
	if k == nil {
		t.Fatalf("no kind %s", id)
	}
	return k
}

func keysOf(t *testing.T, id string, attrs map[string]any) []KeyValue {
	t.Helper()
	k := kind(t, id)
	clean, err := k.Clean(attrs)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := k.IdentityKeys(clean)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestCleanRefusesUnknownAttributesAndOversizedValues(t *testing.T) {
	k := kind(t, "other")
	for name, attrs := range map[string]map[string]any{
		"unknown":  {"name": "x", "colour": "red"},
		"too long": {"name": "x", "note": strings.Repeat("a", 501)},
		"object":   {"name": map[string]any{"a": 1}},
	} {
		if _, err := k.Clean(attrs); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := kind(t, "asset").Clean(map[string]any{"ip": "1.1.1.1", "port": 70000}); !errors.Is(err, ErrInvalid) {
		t.Errorf("port range: %v", err)
	}
	got, err := k.Clean(map[string]any{"name": "  x ", "note": "  "})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"name": "x"}) {
		t.Errorf("blank values must be dropped: %v %v", got, err)
	}
}

func TestIdentityKeysPerKind(t *testing.T) {
	cases := []struct {
		kind  string
		attrs map[string]any
		want  []KeyValue
	}{
		{"ip", map[string]any{"ip": "10.0.0.1"}, []KeyValue{{"ip", "10.0.0.1"}}},
		{"domain", map[string]any{"domain": "A.Test"}, []KeyValue{{"domain", "a.test"}}},
		{"url", map[string]any{"url": "http://a.test:80/x#f"}, []KeyValue{{"url", "http://a.test/x"}}},
		{"app", map[string]any{"name": "Nginx", "rule_id": "r 1"}, []KeyValue{{"rule_id", "R1"}, {"name", "nginx"}}},
		{"cert", map[string]any{"serial": "ab cd"}, []KeyValue{{"serial", "ABCD"}}},
		{"company", map[string]any{"name": "Acme", "reg_no": "91-1"}, []KeyValue{{"reg_no", "91-1"}}},
		{"company", map[string]any{"name": "Acme  Corp"}, []KeyValue{{"name", "acme corp"}}},
		{"icp", map[string]any{"number": "京ICP备1号"}, []KeyValue{{"number", "京ICP备1号"}}},
		{"person", map[string]any{"name": "Lee", "email": []any{"A@x.test", "b@x.test"}}, []KeyValue{{"email", "a@x.test"}, {"email", "b@x.test"}}},
		{"person", map[string]any{"name": "Lee"}, []KeyValue{{"name", "lee"}}},
		{"address", map[string]any{"address": " 1 Main  St "}, []KeyValue{{"address", "1 main st"}}},
		{"vuln", map[string]any{"number": "cve-2024-1", "title": "T"}, []KeyValue{{"number", "CVE-2024-1"}}},
		{"vuln", map[string]any{"title": "Some Flaw"}, []KeyValue{{"title", "some flaw"}}},
		{"fofa_query", map[string]any{"query": `app="x"   &&  y=1`}, []KeyValue{{"query", `app="x" && y=1`}}},
		{"account", map[string]any{"name": "Batch-Job"}, []KeyValue{{"name", "batch-job"}}},
		{"table", map[string]any{"name": "Main.Widget"}, []KeyValue{{"name", "main.widget"}}},
		{"file", map[string]any{"path": `Docs\Notes\`}, []KeyValue{{"path", "Docs/Notes"}}},
		{"other", map[string]any{"name": "Thing"}, []KeyValue{{"name", "thing"}}},
	}
	for _, c := range cases {
		if got := keysOf(t, c.kind, c.attrs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v: got %v want %v", c.kind, c.attrs, got, c.want)
		}
	}
}

func TestAssetNeedsIPAndPort(t *testing.T) {
	k := kind(t, "asset")
	for _, attrs := range []map[string]any{{"host": "a.test"}, {"ip": "1.1.1.1"}, {"port": 80}, {"ip": "1.1.1.1", "host": "a.test"}} {
		clean, err := k.Clean(attrs)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := k.IdentityKeys(clean); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v identified an asset: %v", attrs, err)
		}
	}
}

func TestSubjectWithNoIdentityIsRefusedWithAHint(t *testing.T) {
	k := kind(t, "company")
	clean, _ := k.Clean(map[string]any{"country": "X"})
	_, err := k.IdentityKeys(clean)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "reg_no or name") {
		t.Fatalf("hint missing: %v", err)
	}
}

// Credentials inside an address belong to the credential attribute so the
// address can be shown as it is.
func TestRepositoryAddressLosesEmbeddedCredentialsAndGainsAPath(t *testing.T) {
	k := kind(t, "repo")
	got, err := k.Clean(map[string]any{"url": "https://alice:s3cret@github.com/acme/tool.git"})
	if err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://github.com/acme/tool.git" || got["credential"] != "alice:s3cret" || got["path"] != "acme/tool" {
		t.Fatalf("got %v", got)
	}
	explicit, _ := k.Clean(map[string]any{"url": "https://alice:pw@github.com/acme/tool", "credential": "token-1"})
	if explicit["credential"] != "token-1" {
		t.Errorf("an explicit credential must win: %v", explicit)
	}
	scp, _ := k.Clean(map[string]any{"url": "git@github.com:acme/tool.git"})
	if scp["url"] != "github.com:acme/tool.git" || scp["credential"] != nil || scp["path"] != "acme/tool" {
		t.Errorf("scp address: %v", scp)
	}
}

func TestRepositoryIdentityByAddressOrPath(t *testing.T) {
	ssh := keysOf(t, "repo", map[string]any{"url": "git@github.com:acme/tool.git"})
	https := keysOf(t, "repo", map[string]any{"url": "https://u:t@GitHub.com/acme/tool"})
	if !reflect.DeepEqual(ssh, https) || ssh[0].Name != "url" {
		t.Fatalf("ssh %v vs https %v", ssh, https)
	}
	pathOnly := keysOf(t, "repo", map[string]any{"path": "acme/tool"})
	if pathOnly[0].Name != "path" || pathOnly[0] == ssh[0] {
		t.Fatalf("a path-only repository must not merge with an addressed one: %v", pathOnly)
	}
	// path is derived from the address, but the address key then wins.
	if both := keysOf(t, "repo", map[string]any{"url": "https://github.com/acme/tool", "path": "acme/tool"}); len(both) != 1 || both[0].Name != "url" {
		t.Errorf("fallback key not suppressed: %v", both)
	}
}

func TestMergeAdditiveFillsBlanksUnionsListsAndReportsConflicts(t *testing.T) {
	k := kind(t, "person")
	merged, conflicts := k.MergeAdditive(
		map[string]any{"name": "Lee", "email": []string{"a@x.test"}},
		map[string]any{"name": "Li", "role": "CTO", "email": []string{"b@x.test", "a@x.test"}})
	want := map[string]any{"name": "Lee", "role": "CTO", "email": []string{"a@x.test", "b@x.test"}}
	if !reflect.DeepEqual(merged, want) || !reflect.DeepEqual(conflicts, []string{"name"}) {
		t.Fatalf("merged=%v conflicts=%v", merged, conflicts)
	}
	over := k.Overwrite(want, map[string]any{"name": "Li"})
	if over["name"] != "Li" || over["role"] != "CTO" {
		t.Errorf("overwrite: %v", over)
	}
}

func TestKeyTransitionRefusesRenamingButAllowsAddingAndSuperseding(t *testing.T) {
	k := kind(t, "company")
	byName := []KeyValue{{"name", "acme"}}
	if err := k.CheckKeyTransition(byName, []KeyValue{{"reg_no", "1"}}); err != nil {
		t.Errorf("a registration number supersedes the name key: %v", err)
	}
	if err := k.CheckKeyTransition(byName, []KeyValue{{"name", "other"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("renaming a key must be refused: %v", err)
	}
	d := kind(t, "domain")
	if err := d.CheckKeyTransition([]KeyValue{{"domain", "a.test"}}, []KeyValue{{"domain", "b.test"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a domain cannot become another domain: %v", err)
	}
	p := kind(t, "person")
	if err := p.CheckKeyTransition([]KeyValue{{"email", "a@x.test"}}, []KeyValue{{"email", "a@x.test"}, {"email", "b@x.test"}}); err != nil {
		t.Errorf("adding an email is allowed: %v", err)
	}
	if err := p.CheckKeyTransition([]KeyValue{{"email", "a@x.test"}, {"email", "b@x.test"}}, []KeyValue{{"email", "b@x.test"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("dropping an email is refused: %v", err)
	}
}

func TestLinkAttributesAreBounded(t *testing.T) {
	if got, err := CleanLinkAttrs(map[string]any{"stake": 51.5, "note": " held "}); err != nil || got["note"] != "held" || got["stake"] != "51.5" {
		t.Errorf("%v %v", got, err)
	}
	big := map[string]any{}
	for i := 0; i < MaxLinkAttrs+1; i++ {
		big[string(rune('a'+i))] = "x"
	}
	if _, err := CleanLinkAttrs(big); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many attributes: %v", err)
	}
	if _, err := CleanLinkAttrs(map[string]any{"a": strings.Repeat("x", MaxLinkAttrSize+1)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("too large: %v", err)
	}
	if _, err := CleanLinkAttrs(map[string]any{"a": []any{"x", "y"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a list is not a link attribute: %v", err)
	}
}

func TestEvidenceShapeAndBatchValidation(t *testing.T) {
	good := Evidence{ThreadID: "th_1", Seq: 3}
	if err := good.ValidateShape(); err != nil || good.Key() != "e:th_1:3" {
		t.Errorf("%v %s", err, good.Key())
	}
	for _, bad := range []Evidence{{}, {ThreadID: "t"}, {URL: "ftp://x.test/a"}, {URL: "http://x.test", Seq: 1, ThreadID: "t"}} {
		bad := bad
		if err := bad.ValidateShape(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", bad)
		}
	}
	web := Evidence{URL: " https://X.test/a "}
	if err := web.ValidateShape(); err != nil || web.URL != "https://X.test/a" {
		t.Errorf("%v %q", err, web.URL)
	}
	if err := (Batch{}).Valid(); !errors.Is(err, ErrInvalid) {
		t.Error("an empty batch was accepted")
	}
	b := Batch{Subjects: []SubjectInput{{Kind: "ip"}}, Evidence: []Evidence{good}, OwnToolCallIDs: []string{"c1"}}
	if err := b.Valid(); !errors.Is(err, ErrInvalid) {
		t.Error("evidence together with own_tool_call_ids was accepted")
	}
	amb := &AmbiguousError{IDs: []string{"a", "b"}}
	if !errors.Is(amb, ErrInvalid) || !strings.Contains(amb.Error(), "a, b") {
		t.Errorf("ambiguity: %v", amb)
	}
}

func TestOtherNamesAThingNotAnExplanation(t *testing.T) {
	k := kind(t, "other")
	if _, err := k.Clean(map[string]any{"name": "widget"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"option: stop", "body=empty", "file (caller)", "标题：说明", "field notes"} {
		if _, err := k.Clean(map[string]any{"name": name}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q was accepted as a subject name", name)
		}
	}
}

// A login that carries its host, or a phrase standing in for a table, is
// more than one thing. The host and the phrase stay out of the subject.
func TestAccountAndTableNameOneThing(t *testing.T) {
	account := kind(t, "account")
	if _, err := account.Clean(map[string]any{"name": "batch-job"}); err != nil {
		t.Fatal(err)
	}
	if _, err := account.Clean(map[string]any{"note": "kept"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"worker@host", "10.0.0.1", "two words", "login: extra"} {
		if _, err := account.Clean(map[string]any{"name": name}); !errors.Is(err, ErrInvalid) {
			t.Errorf("account %q: %v", name, err)
		}
	}
	table := kind(t, "table")
	if _, err := table.Clean(map[string]any{"name": "main.widget"}); err != nil {
		t.Fatal(err)
	}
	if _, err := table.Clean(map[string]any{"name": "field notes"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a phrase was stored as a table: %v", err)
	}
}
