package graph

import "testing"

func TestNormalizersCollapseSpellingsOfOneThing(t *testing.T) {
	same := map[string][][2]string{
		"ip":         {{"::ffff:10.0.0.1", "10.0.0.1"}, {"2001:DB8::1", "2001:db8::1"}},
		"domain":     {{"Example.COM.", "example.com"}, {"bücher.example", "xn--bcher-kva.example"}},
		"url":        {{"HTTPS://A.test:443/x#frag", "https://a.test/x"}, {"http://a.test", "http://a.test:80/"}, {"https://u:p@a.test/x", "https://a.test/x"}},
		"repo":       {{"git@github.com:acme/tool.git", "https://github.com/acme/tool"}, {"ssh://git@GitHub.com/acme/tool/", "https://github.com/acme/tool.git"}},
		"repo_path":  {{"/acme/tool.git/", "acme/tool"}},
		"email":      {{" A@B.Test ", "a@b.test"}},
		"identifier": {{"cve-2024 0001", "CVE-20240001"}},
		"text":       {{"  Acme   Corp ", "acme corp"}},
		"host":       {{"Example.COM.", "example.com"}},
	}
	for name, pairs := range same {
		for _, p := range pairs {
			a, err := Normalize(name, p[0])
			if err != nil {
				t.Fatalf("%s %q: %v", name, p[0], err)
			}
			b, err := Normalize(name, p[1])
			if err != nil || a != b {
				t.Errorf("%s: %q -> %q but %q -> %q (%v)", name, p[0], a, p[1], b, err)
			}
		}
	}
}

func TestNormalizersKeepDifferentThingsApart(t *testing.T) {
	apart := map[string][2]string{
		"repo":      {"https://github.com/Acme/Tool", "https://github.com/acme/tool"},
		"repo_path": {"Acme/Tool", "acme/tool"},
		"url":       {"https://a.test/x", "https://a.test/y"},
		"text":      {"acme", "acme inc"},
	}
	for name, p := range apart {
		a, _ := Normalize(name, p[0])
		b, _ := Normalize(name, p[1])
		if a == b {
			t.Errorf("%s merged %q and %q", name, p[0], p[1])
		}
	}
}

func TestNormalizersRefuseWhatIsNotTheirType(t *testing.T) {
	for name, bad := range map[string][]string{
		"ip":     {"999.1.1.1", "fe80::1%en0", "host"},
		"domain": {"localhost", "1.2.3.4", "-a.test", "a b.test"},
		"url":    {"not a url", "/relative", "http://"},
		"email":  {"nobody", "a@b", "@b.test"},
		"repo":   {"acme/tool", "https://github.com/"},
	} {
		for _, v := range bad {
			if got, err := Normalize(name, v); err == nil {
				t.Errorf("%s accepted %q as %q", name, v, got)
			}
		}
	}
	if _, err := Normalize("ip", "  "); err == nil {
		t.Error("blank value was accepted")
	}
	if _, err := Normalize("magic", "x"); err == nil {
		t.Error("unknown normalizer was accepted")
	}
}

// Only whitespace outside quotes may change; quoted text, case and order
// stay, because the host does not parse query syntax.
func TestQueryNormalizationKeepsQuotedTextCaseAndOrder(t *testing.T) {
	cases := [][2]string{
		{`  app="Foo"   &&  port=80 `, `app="Foo" && port=80`},
		{"a=1\n\t&&\tb=2", "a=1 && b=2"},
		{`title="a   b"  &&  x=1`, `title="a   b" && x=1`},
		{`t="say \"  hi\""   y`, `t="say \"  hi\"" y`},
	}
	for _, c := range cases {
		if got := NormalizeQuery(c[0]); got != c[1] {
			t.Errorf("%q -> %q want %q", c[0], got, c[1])
		}
	}
	for _, p := range [][2]string{{`a=1 && b=2`, `b=2 && a=1`}, {`app="foo"`, `app="Foo"`}, {`t="a b"`, `t="a  b"`}} {
		if NormalizeQuery(p[0]) == NormalizeQuery(p[1]) {
			t.Errorf("%q and %q must stay distinct", p[0], p[1])
		}
	}
}

func TestAssetIdentityOmittedAndExplicitDefaultPortAreOneOrigin(t *testing.T) {
	a, err := NormalizeAsset("1.1.1.1", "443", "https", "A.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"a.test:443", "https://a.test", "HTTPS://A.TEST:443"} {
		b, err := NormalizeAsset("1.1.1.1", "443", "https", host)
		if err != nil || a != b {
			t.Errorf("host %q: %q vs %q (%v)", host, b, a, err)
		}
	}
	other, _ := NormalizeAsset("1.1.1.2", "443", "https", "a.test")
	if other == a {
		t.Error("the same host on another IP must be another asset")
	}
	for _, bad := range [][4]string{{"", "443", "https", ""}, {"1.1.1.1", "", "https", ""}, {"1.1.1.1", "70000", "https", ""}, {"1.1.1.1", "443", "https", "http://a.test"}} {
		if v, err := NormalizeAsset(bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Errorf("%v accepted as %q", bad, v)
		}
	}
}
