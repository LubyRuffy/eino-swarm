package skills

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestLoaderReadsOnlyExplicitlyNamedExternalPackage(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.InstallSkillZIP(t.Context(), makeSkillZIP(t, skillZIPEntry{"SKILL.md", portableSkillFixture, 0o600})); err != nil {
		t.Fatal(err)
	}
	loader := store.Loader().(tool.InvokableTool)
	info, err := loader.Info(t.Context())
	if err != nil || info.Name != ToolLoad || !strings.Contains(info.Desc, "base_dir") {
		t.Fatalf("tool info=%+v err=%v", info, err)
	}
	for _, tc := range []struct {
		args string
		want string
	}{
		{`{`, "could not read the arguments"},
		{`{"name":"missing"}`, "no external skill"},
		{`{"name":"../verify-layout"}`, ErrBadName.Error()},
	} {
		got, err := loader.InvokableRun(t.Context(), tc.args)
		if err != nil || !strings.Contains(got, tc.want) || !strings.Contains(got, `"success":false`) {
			t.Fatalf("load %s returned %s, %v", tc.args, got, err)
		}
	}
	got, err := loader.InvokableRun(t.Context(), `{"name":" verify-layout "}`)
	var decoded struct {
		Success bool   `json:"success"`
		Name    string `json:"name"`
		Body    string `json:"body"`
		BaseDir string `json:"base_dir"`
	}
	if err != nil || json.Unmarshal([]byte(got), &decoded) != nil || !decoded.Success ||
		decoded.Name != "verify-layout" || !strings.Contains(decoded.Body, "references/notes.md") || decoded.BaseDir == "" {
		t.Fatalf("load result=%s err=%v", got, err)
	}
}

func TestPromptCapsExternalMetadataAndKeepsNamedPackagesReachable(t *testing.T) {
	items := make([]SkillInfo, maxPromptSkills+1)
	for i := range items {
		items[i] = SkillInfo{Name: fmt.Sprintf("skill-%02d", i), Description: "A package summary."}
	}
	prompt := Prompt(items)
	if strings.Count(prompt, "A package summary.") != maxPromptSkills ||
		strings.Contains(prompt, items[maxPromptSkills].Name) ||
		!strings.Contains(prompt, "1 additional packages") || !strings.Contains(prompt, ToolLoad) {
		t.Fatalf("external skill prompt=%s", prompt)
	}
}
