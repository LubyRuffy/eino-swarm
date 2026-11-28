package graph

import (
	"strings"
	"testing"
)

// The embedded catalog is shared with the desktop and the phone; if it stops
// parsing, no write can pass, so the build must fail first.
func TestEmbeddedCatalogLoadsWithEveryBuiltInKind(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip", "domain", "url", "asset", "app", "cert", "company", "icp", "person", "address", "vuln", "repo", "fofa_query", "account", "table", "file", "other"}
	if got := c.IDs(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds=%v want %v", got, want)
	}
	for _, id := range []string{"host", "uri"} {
		if c.Kind(id) != nil {
			t.Errorf("ambiguous kind %q must not exist", id)
		}
	}
	if got := c.Kind("repo").SecretAttrs(); len(got) != 1 || got[0] != "credential" {
		t.Errorf("credential attribute is not marked secret: %v", got)
	}
}

func TestCatalogValidationRefusesBrokenEntries(t *testing.T) {
	base := `{"version":1,"other":"other","kinds":[%s]}`
	other := `{"id":"other","name":{"en":"O","zh":"O"},"description":"d","display":["name"],"attrs":[{"name":"name","type":"string","label":{"en":"n","zh":"n"}}],"keys":[{"name":"name","normalizer":"text","parts":["name"]}]}`
	for name, kind := range map[string]string{
		"unknown normalizer": strings.Replace(other, `"text"`, `"magic"`, 1),
		"unknown part":       strings.Replace(other, `"parts":["name"]`, `"parts":["nope"]`, 1),
		"no keys":            strings.Replace(other, `"keys":[{"name":"name","normalizer":"text","parts":["name"]}]`, `"keys":[]`, 1),
		"bad display":        strings.Replace(other, `"display":["name"]`, `"display":["nope"]`, 1),
		"bad type":           strings.Replace(other, `"type":"string"`, `"type":"blob"`, 1),
		"no zh label":        strings.Replace(other, `"label":{"en":"n","zh":"n"}`, `"label":{"en":"n"}`, 1),
		"several parts":      strings.Replace(other, `"parts":["name"]`, `"parts":["name","name"]`, 1),
	} {
		if _, err := ParseCatalog([]byte(strings.Replace(base, "%s", kind, 1))); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := ParseCatalog([]byte(strings.Replace(base, "%s", "", 1))); err == nil {
		t.Error("a catalog without the generic kind was accepted")
	}
	if _, err := ParseCatalog([]byte(strings.Replace(base, "%s", other+","+other, 1))); err == nil {
		t.Error("a repeated kind was accepted")
	}
}

func TestDescribeListsEveryKindForTheToolDefinition(t *testing.T) {
	text := MustDefault().Describe()
	for _, id := range MustDefault().IDs() {
		if !strings.Contains(text, "- "+id+" (") {
			t.Errorf("kind %s missing from the tool description", id)
		}
	}
}

func TestDisplayNameFallsBackAcrossRules(t *testing.T) {
	asset := MustDefault().Kind("asset")
	if got := asset.DisplayName(map[string]any{"ip": "1.2.3.4", "port": int64(443)}); got != "1.2.3.4:443" {
		t.Errorf("no host: %q", got)
	}
	if got := asset.DisplayName(map[string]any{"ip": "1.2.3.4", "port": int64(443), "host": "a.test"}); got != "a.test" {
		t.Errorf("with host: %q", got)
	}
	if got := MustDefault().Kind("person").DisplayName(map[string]any{"email": []string{"a@b.test"}}); got != "a@b.test" {
		t.Errorf("first list element: %q", got)
	}
}
