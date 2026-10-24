package desktop

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func recoveryLocationBody(t *testing.T, location recoveryLocation) string {
	t.Helper()
	body, err := json.Marshal(struct {
		Type       string `json:"type"`
		Session    string `json:"session"`
		Generation int    `json:"generation"`
		Document   int    `json:"document"`
		recoveryLocation
	}{"zwai:recovery-location", "session", 2, 0, location})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRecoveryLocationRequiresTheCurrentNativeDocument(t *testing.T) {
	l := recoveryLocation{ThreadID: "th_selected", TurnID: "tn_current", ProjectID: "pr_selected", DraftKey: "thread:th_selected"}
	body := recoveryLocationBody(t, l)
	origin := &application.OriginInfo{Origin: "http://127.0.0.1:1234", TopOrigin: "http://127.0.0.1:1234", IsMainFrame: true}
	for name, input := range map[string]struct {
		message string
		origin  *application.OriginInfo
	}{
		"valid": {body, origin}, "nil": {body, nil},
		"subframe":         {body, &application.OriginInfo{Origin: origin.Origin}},
		"other origin":     {body, &application.OriginInfo{Origin: "http://127.0.0.1:1235", IsMainFrame: true}},
		"other top":        {body, &application.OriginInfo{Origin: origin.Origin, TopOrigin: "http://other.invalid", IsMainFrame: true}},
		"old session":      {strings.Replace(body, `"session":"session"`, `"session":"old"`, 1), origin},
		"old generation":   {strings.Replace(body, `"generation":2`, `"generation":1`, 1), origin},
		"old document":     {strings.Replace(body, `"document":0`, `"document":1`, 1), origin},
		"missing document": {strings.Replace(body, `"document":0,`, "", 1), origin},
		"paint message":    {strings.Replace(body, "zwai:recovery-location", "zwai:recovery", 1), origin},
		"malformed":        {"{", origin}, "oversized": {body + strings.Repeat(" ", recoveryMessageLimit), origin},
		"wrong type": {strings.Replace(body, `"thread_id":"th_selected"`, `"thread_id":3`, 1), origin},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := validRecoveryLocation(origin.Origin, "session", 2, 0, input.message, input.origin)
			if ok != (name == "valid") || ok && got != l {
				t.Fatalf("location=%+v accepted=%v", got, ok)
			}
		})
	}
	for _, field := range []string{"thread_id", "turn_id", "project_id", "draft_key"} {
		limit := 128
		if field == "draft_key" {
			limit = 256
		}
		for _, size := range []int{limit, limit + 1} {
			value, _ := json.Marshal(map[string]string{field: strings.Repeat("x", size)})
			if _, ok := readRecoveryLocation(string(value)); ok != (size == limit) {
				t.Fatalf("%s length %d accepted=%v", field, size, ok)
			}
		}
	}
}

func TestRecoveryReloadReplacesOnlyTheSelectedScopeInTheOriginalURL(t *testing.T) {
	base := "http://127.0.0.1:1234/path?shell=desktop&pid=456&presence=owned&other=keep&recovery_thread=old&recovery_project=old&recovery_draft=old#anchor"
	for name, location := range map[string]recoveryLocation{
		"thread":  {ThreadID: "th_new", ProjectID: "pr_new", DraftKey: "thread:th_new"},
		"project": {ProjectID: "pr_new", DraftKey: "new:pr_new"},
		"home":    {DraftKey: "new:home"}, "empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got := location.windowURL(base)
			u, err := url.Parse(got)
			if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:1234" || u.Path != "/path" || u.Fragment != "anchor" {
				t.Fatalf("changed original URL: %q", got)
			}
			for key, want := range map[string]string{"shell": "desktop", "pid": "456", "presence": "owned", "other": "keep", "recovery_thread": location.ThreadID, "recovery_project": location.ProjectID, "recovery_draft": location.DraftKey} {
				if u.Query().Get(key) != want {
					t.Fatalf("%s=%q want=%q", key, u.Query().Get(key), want)
				}
			}
			if back := recoveryLocationFromURL(got); !back.sameScope(location) {
				t.Fatalf("scope changed: %+v want=%+v", back, location)
			}
		})
	}
	for raw, want := range map[string]recoveryLocation{
		"%": {}, "http://localhost": {},
		"http://localhost?recovery_thread=th_selected":  {ThreadID: "th_selected", DraftKey: "thread:th_selected"},
		"http://localhost?recovery_project=pr_selected": {ProjectID: "pr_selected", DraftKey: "new:pr_selected"},
	} {
		if got := recoveryLocationFromURL(raw); got != want {
			t.Fatalf("%s decoded=%+v want=%+v", raw, got, want)
		}
	}
	if got := (recoveryLocation{}).windowURL("%"); got != "%" {
		t.Fatal("malformed original URL was rewritten")
	}
}

func TestSuccessorScopeLearnsTheSelectedThreadsActualProject(t *testing.T) {
	expected := recoveryLocationFromURL("http://localhost?shell=desktop&recovery_thread=th_selected&recovery_draft=thread:th_selected")
	actual := recoveryLocation{ThreadID: "th_selected", ProjectID: "pr_actual", DraftKey: "thread:th_selected"}
	if !expected.matchesRestored(actual) {
		t.Fatal("a project learned from the selected thread prevented restoration")
	}
	for _, mismatch := range []recoveryLocation{{DraftKey: "new:home"}, {ThreadID: "th_other", DraftKey: actual.DraftKey}, {ThreadID: actual.ThreadID, DraftKey: "new:pr_actual"}} {
		if expected.matchesRestored(mismatch) {
			t.Fatalf("a different selected scope passed: %+v", mismatch)
		}
	}
	expected.ProjectID = "pr_expected"
	if !expected.matchesRestored(actual) {
		t.Fatal("project metadata overrode an explicit thread route")
	}
	expected.ThreadID = ""
	if expected.matchesRestored(actual) {
		t.Fatal("a project-only route accepted another selected scope")
	}
}
