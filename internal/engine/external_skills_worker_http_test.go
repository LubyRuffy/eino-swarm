package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

// The manager's catalog does not prove a delegated worker can discover and
// load both kinds of skill at its own model boundary.
func TestDelegatedWorkerLoadsExternalAndLearnedSkillsWithTheSameName(t *testing.T) {
	const (
		name            = "source-check"
		externalSummary = "Check a source against the package checklist."
		externalBody    = "Compare each assumption with the attached checklist."
		learnedSummary  = "Use the project's recorded reconciliation practice."
		learnedBody     = "Verify discrepancies in the local register."
		externalCall    = "load-external-check"
		learnedCall     = "view-learned-check"
		workerAnswer    = "Both source-check procedures were read independently."
	)
	e := newTestEngine(t)
	e.cfg.Memory.Enabled = true
	project, err := e.CreateProject("Delegation skill project", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Delegated procedure", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	learned := e.ProjectMemory(project.ID)
	if _, err := learned.WriteSkill(name, learnedSummary, learnedBody); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	writer := zip.NewWriter(&bundle)
	manifest, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(manifest, "---\nname: %s\ndescription: %s\n---\n\n%s\n", name, externalSummary, externalBody); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Bytes()), int64(bundle.Len()))
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
	bundlePath := filepath.Join(e.cfg.ProjectDir(project.ID), "installed-skills", name)
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		offered := map[string]bool{}
		for _, candidate := range req.Tools {
			offered[candidate.Function.Name] = true
		}
		if !offered[skills.ToolLoad] || !offered[memory.ToolSkillView] || offered[memory.ToolSkillManage] || offered[memory.ToolMemory] {
			t.Errorf("wrong worker skill surface: %v", offered)
		}
		call := func(id, toolName string) {
			writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": toolName, "arguments": `{"name":"` + name + `"}`},
			}}}, "tool_calls")
		}
		assertResult := func(callID, wantBody, wantDir string) {
			for _, msg := range req.Messages {
				if msg.Role != "tool" || msg.ToolCallID != callID {
					continue
				}
				var got struct {
					Success bool   `json:"success"`
					Name    string `json:"name"`
					Body    string `json:"body"`
					BaseDir string `json:"base_dir"`
				}
				if err := json.Unmarshal([]byte(msg.Content), &got); err != nil ||
					!got.Success || got.Name != name || got.Body != wantBody || got.BaseDir != wantDir {
					t.Errorf("%s returned %s, %v", callID, msg.Content, err)
				}
				return
			}
			t.Errorf("%s result did not reach the worker's next model request", callID)
		}
		switch step := requests.Add(1); step {
		case 1:
			if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, "## External skills") ||
				!strings.Contains(req.Messages[0].Content, externalSummary) || !strings.Contains(req.Messages[0].Content, learnedSummary) {
				t.Error("worker system prompt missed one of the separate skill indexes")
			}
			if input := fmt.Sprint(req.Messages); strings.Contains(input, externalBody) || strings.Contains(input, learnedBody) {
				t.Error("full instructions reached the worker before it selected a skill")
			}
			call(externalCall, skills.ToolLoad)
		case 2:
			assertResult(externalCall, externalBody, bundlePath)
			if strings.Contains(fmt.Sprint(req.Messages), learnedBody) {
				t.Error("loading the external package also opened the same-name learned skill")
			}
			call(learnedCall, memory.ToolSkillView)
		case 3:
			assertResult(learnedCall, learnedBody, "")
			writeContextFlowReply(w, req.Stream, map[string]any{"content": workerAnswer}, "stop")
		default:
			t.Errorf("unexpected worker request %d", step)
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "Unexpected request."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	p := &e.cfg.Models.Providers[0]
	p.BaseURL = endpoint.URL
	p.Model = "offline-external-skill-worker"
	e.pool = provider.New(e.cfg)
	builder, err := e.pool.ModelBuilder(t.Context(), p.ID, p.Model, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(thread)
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, pc)
	t.Cleanup(reg.Close)
	owner := mustStoredTurn(t, e, thread.ID, store.Turn{})
	if err := e.store.FinishTurn(owner.ID, store.TurnDone, "Delegation retained its owner.", ""); err != nil {
		t.Fatal(err)
	}
	ownerCtx := withHistoryReadOwner(t.Context(), thread.ID, owner.ID)
	var spawnTool, waitTool tool.InvokableTool
	for _, base := range reg.Tools() {
		info, err := base.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		switch info.Name {
		case "spawn_agent":
			spawnTool = base.(tool.InvokableTool)
		case "wait_agents":
			waitTool = base.(tool.InvokableTool)
		}
	}
	if spawnTool == nil || waitTool == nil {
		t.Fatal("manager lifecycle tools are absent")
	}
	spawned, err := spawnTool.InvokableRun(ownerCtx, `{"role":"reviewer","task":"Read the applicable installed and project procedures."}`)
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(spawned), &identity); err != nil || identity.AgentID == "" {
		t.Fatalf("spawn receipt = %s, %v", spawned, err)
	}
	waitArgs, err := json.Marshal(map[string]any{"agent_ids": []string{identity.AgentID}, "timeout_s": 10})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ownerCtx, 10*time.Second)
	defer cancel()
	result, err := waitTool.InvokableRun(waitCtx, string(waitArgs))
	if err != nil || !strings.Contains(result, workerAnswer) || !strings.Contains(result, `"status":"done"`) {
		t.Fatalf("worker collection = %s, %v", result, err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("worker requests = %d, want initial, installed skill, and learned skill", got)
	}
	if got, err := learned.ReadSkill(name); err != nil || got.Views != 1 {
		t.Fatalf("only skill_view should count as learned skill use: %+v, %v", got, err)
	}
}
