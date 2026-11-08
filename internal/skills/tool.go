package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	ToolLoad        = "load_skill"
	maxPromptSkills = 32
)

// Prompt describes installed packages without disclosing their instructions or resources.
func Prompt(items []SkillInfo) string {
	var b strings.Builder
	b.WriteString("## External skills\n\nPackages installed by the user in this project:\n\n")
	for _, skill := range items[:min(len(items), maxPromptSkills)] {
		fmt.Fprintf(&b, "- %s — %s\n", skill.Name, skill.Description)
	}
	if extra := len(items) - maxPromptSkills; extra > 0 {
		fmt.Fprintf(&b, "\n%d additional packages are installed. A package the user names can be opened directly.\n", extra)
	}
	b.WriteString("\nCall " + ToolLoad + " for a relevant package or one the user names. " +
		"Resolve relative paths in its instructions against the returned base_dir using the tools you already have.\n\n")
	return b.String()
}

// Loader is read-only and binds model requests to one project's external packages.
func (s *Store) Loader() tool.BaseTool {
	return &loadTool{store: s}
}

type loadTool struct{ store *Store }

func (t *loadTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolLoad,
		Desc: "Read one external skill package installed by the user in this project. " +
			"Use its base_dir for package-relative resources. Loading returns instructions and does not execute scripts.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {Type: schema.String, Required: true, Desc: "the external skill package name"},
		}),
	}, nil
}

func (t *loadTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var request struct {
		Name string `json:"name"`
	}
	var result struct {
		Success bool   `json:"success"`
		Name    string `json:"name,omitempty"`
		Body    string `json:"body,omitempty"`
		BaseDir string `json:"base_dir,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(args), &request); err != nil {
		result.Error = "could not read the arguments: " + err.Error()
	} else {
		skill, err := t.store.ReadInstalledSkill(strings.TrimSpace(request.Name))
		switch {
		case errors.Is(err, ErrNoMatch):
			result.Error = fmt.Sprintf("no external skill %q is installed in this project", request.Name)
		case err != nil:
			result.Error = err.Error()
		default:
			result.Success = true
			result.Name, result.Body, result.BaseDir = skill.Name, skill.Body, skill.BaseDir
		}
	}
	data, err := json.Marshal(result)
	return string(data), err
}
