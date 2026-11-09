package engine

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestInstalledSkillIsListedThenLoadedAtTheModelBoundary(t *testing.T) {
	const skill = "document-check"
	const summary = "Inspect a source for internal consistency."
	const body = "First, read references/procedure.md for the checks."
	const resource = "The reference marker is 4b37."
	var bundlePath string
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, req scheduleCompletionRequest) {
		answer := func(text string) {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
		}
		hasLoader := false
		for _, tl := range req.Tools {
			switch tl.Function.Name {
			case skills.ToolLoad:
				hasLoader = true
			case memory.ToolMemory, memory.ToolSkillManage, memory.ToolSkillView:
				t.Error("installing a skill enabled memory writes")
			}
		}
		switch step {
		case 1:
			if !hasLoader || !strings.Contains(fmt.Sprint(req.Messages), summary) {
				t.Error("the model did not receive the installed skill metadata and read tool")
				answer("I could not find the installed skill.")
				return
			}
			if strings.Contains(fmt.Sprint(req.Messages), body) || strings.Contains(fmt.Sprint(req.Messages), resource) {
				t.Error("the package contents reached the model before it asked")
			}
			writeScheduleCompletionTool(w, req, skills.ToolLoad, `{"name":"document-check"}`)
		case 2:
			for _, msg := range req.Messages {
				if msg.Role != "tool" {
					continue
				}
				var result struct {
					Success bool   `json:"success"`
					Body    string `json:"body"`
					BaseDir string `json:"base_dir"`
				}
				if err := json.Unmarshal([]byte(msg.Content), &result); err != nil || !result.Success ||
					result.Body != body || result.BaseDir != bundlePath {
					t.Errorf("load_skill returned %s (%v)", msg.Content, err)
					continue
				}
				args, err := json.Marshal(map[string]string{"file_path": filepath.Join(result.BaseDir, "references", "procedure.md")})
				if err != nil {
					t.Error(err)
					break
				}
				writeScheduleCompletionTool(w, req, "read", string(args))
				return
			}
			t.Error("the load_skill tool result did not reach the actual model")
			answer("The installed skill could not be loaded.")
		case 3:
			if !strings.Contains(fmt.Sprint(req.Messages), resource) {
				t.Error("the model could not read the relative package resource")
			}
			answer("The installed skill and its reference were loaded.")
		case 4:
			if hasLoader || strings.Contains(fmt.Sprint(req.Messages), summary) {
				t.Error("another project could discover the installed skill")
			}
			answer("This project has its own skill scope.")
		default:
			t.Errorf("unexpected model request %d", step)
			answer("Unexpected request.")
		}
	})
	e.cfg.Memory.Enabled = false
	project, err := e.CreateProject("Skill consumer", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	bundlePath = filepath.Join(e.cfg.ProjectDir(project.ID), "installed-skills", skill)
	var data bytes.Buffer
	zipWriter := zip.NewWriter(&data)
	for path, content := range map[string]string{
		"SKILL.md":                "---\nname: " + skill + "\ndescription: " + summary + "\n---\n\n" + body + "\n",
		"references/procedure.md": resource,
	} {
		file, err := zipWriter.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := e.ProjectExternalSkills(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.InstallSkillZIP(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
	other, err := e.CreateProject("Independent project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*store.Project{project, other} {
		th, err := e.CreateThread("Skill test", "", p.ID)
		if err != nil {
			t.Fatal(err)
		}
		turn, err := e.StartTurn(th.ID, "Inspect the available procedure.")
		if err != nil {
			t.Fatal(err)
		}
		got := waitForTurn(t, e, turn.ID)
		waitSettled(t, e, th.ID)
		if got.Status != store.TurnDone {
			t.Fatalf("skill test turn: %+v", got)
		}
	}
	if n := len(fixture.snapshot()); n != 4 {
		t.Errorf("actual model requests=%d want 4", n)
	}
}
