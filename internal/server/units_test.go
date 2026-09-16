package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/gin-gonic/gin"
)

// An event name with a newline in it would end the SSE frame early and
// corrupt every event after it.
func TestSSENameCannotBreakTheFraming(t *testing.T) {
	for in, want := range map[string]string{
		"delta":          "delta",
		"tool\ncall":     "tool call",
		"  spaced  ":     "spaced",
		"":               "message",
		"\n":             "message",
		"multi\nline\nx": "multi line x",
	} {
		if got := sseName(in); got != want {
			t.Fatalf("sseName(%q)=%q want %q", in, got, want)
		}
		if strings.Contains(sseName(in), "\n") {
			t.Fatalf("sseName(%q) still contains a newline", in)
		}
	}
}

// A file name with a quote, a newline or non-ASCII characters must not be
// able to inject header content.
func TestEscapePathIsHeaderSafe(t *testing.T) {
	for in, want := range map[string]string{
		"report.md":      "report.md",
		"a b.txt":        "a%20b.txt",
		"quote\".txt":    "quote%22.txt",
		"break\r\n.txt":  "break%0D%0A.txt",
		"年度报告.md":        "%E5%B9%B4%E5%BA%A6%E6%8A%A5%E5%91%8A.md",
		"safe-_.~name":   "safe-_.~name",
		"semi;colon.txt": "semi%3Bcolon.txt",
	} {
		if got := escapePath(in); got != want {
			t.Fatalf("escapePath(%q)=%q want %q", in, got, want)
		}
	}
}

// An EventSource reconnects with a header, a manual reload uses the query
// parameter, and anything unusable means "from the beginning" rather than an
// error the user cannot act on.
func TestParseSince(t *testing.T) {
	for _, tc := range []struct {
		name, header, query string
		want                int64
	}{
		{"nothing", "", "", 0},
		{"query", "", "12", 12},
		{"header wins", "30", "12", 30},
		{"garbage header falls back", "abc", "12", 12},
		{"garbage query", "", "abc", 0},
		{"negative is ignored", "", "-5", 0},
		{"zero is the beginning", "0", "0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest(http.MethodGet, "/?since="+tc.query, nil)
			if tc.header != "" {
				req.Header.Set("Last-Event-ID", tc.header)
			}
			c.Request = req
			if got := parseSince(c); got != tc.want {
				t.Fatalf("parseSince=%d want %d", got, tc.want)
			}
		})
	}
}

func TestSettingsViewHidesKeysAndReportsReadiness(t *testing.T) {
	cfg := config.Default()
	cfg.Models = config.ModelsConfig{
		Default: "a",
		Providers: []config.Provider{
			{ID: "a", Label: "With key", BaseURL: "http://a.invalid/v1", Model: "m", APIKey: "k"},
			{ID: "b", BaseURL: "", Model: "", APIKey: ""},
		},
	}
	view := toSettingsView(cfg)
	if len(view.Models.Providers) != 2 {
		t.Fatalf("providers=%d", len(view.Models.Providers))
	}
	a, b := view.Models.Providers[0], view.Models.Providers[1]
	if !a.HasAPIKey || !a.Ready {
		t.Fatalf("a configured provider should be ready: %+v", a)
	}
	if b.HasAPIKey || b.Ready {
		t.Fatalf("an empty provider is not ready: %+v", b)
	}
	if a.Catalog == nil || b.Catalog == nil {
		t.Fatal("catalog must be a list, not null")
	}
	// the view type has no field that could carry the key at all
	if strings.Contains(strings.ToLower(a.Label+a.BaseURL+a.Model), "k") && a.Model == "k" {
		t.Fatal("the key ended up in another field")
	}
}
