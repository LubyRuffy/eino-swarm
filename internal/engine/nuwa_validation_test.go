package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func nuwaValidationSource(t *testing.T, e *Engine, name string) (*store.Project, *store.Thread, *store.Turn) {
	t.Helper()
	e.cfg.Swarm.AutoTitle = false
	project, err := e.CreateProject(name, "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Explicit validation source", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, UserText: "Inspect the current validation evidence", ProviderID: thread.ProviderID, Model: thread.Model, NuwaEnabled: true}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	return project, thread, turn
}

func nuwaValidationNode(t *testing.T, e *Engine, project *store.Project, thread *store.Thread, turn *store.Turn, title, detail string) store.NuwaNode {
	t.Helper()
	node := store.NuwaNode{ProjectID: project.ID, ThreadID: thread.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: "fact", Title: title, Detail: detail}
	if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
		t.Fatal(err)
	}
	return node
}

// Artifact publication must use the selected file's exact identity. A trimmed
// sibling or a symlink outside the workspace is not the claimed deliverable.
func TestNuwaValidationArtifactUsesRealWorkspaceFiles(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	selected := "report.txt \t"
	for _, path := range []string{filepath.Join(root, selected), filepath.Join(root, "report.txt"), filepath.Join(outside, "private.txt")} {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "private.txt"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, selected), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, workspace, path, want, refusal string }{
		{"empty", root, "", "", ""},
		{"relative", root, "report.txt", "report.txt", ""},
		{"absolute", root, filepath.Join(root, selected), selected, ""},
		{"trailing whitespace", root, selected, selected, ""},
		{"inside symlink", root, "alias", selected, ""},
		{"directory", root, root, "", "regular file"},
		{"outside absolute", root, filepath.Join(outside, "private.txt"), "", "within"},
		{"outside symlink", root, "escape", "", "within"},
		{"parent traversal", root, "../" + filepath.Base(outside) + "/private.txt", "", "within"},
		{"missing", root, "missing.txt", "", "does not exist"},
		{"missing workspace", filepath.Join(root, "absent"), "report.txt", "", "resolve artifact workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nuwaArtifactPath(tc.workspace, tc.path)
			if tc.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("artifact=%q err=%v want refusal %q", got, err, tc.refusal)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("artifact=%q err=%v want exact %q", got, err, tc.want)
			}
		})
	}
	if err := os.Remove(filepath.Join(root, selected)); err != nil {
		t.Fatal(err)
	}
	if _, err := nuwaArtifactPath(root, selected); err == nil {
		t.Fatal("a deleted selection silently resolved to its trimmed sibling")
	}
}

// Index reads return summaries within one project's scope. Detailed Chinese
// evidence pages must be complete JSON and concatenate to the original text.
func TestNuwaValidationReadWorkIsScopedPagedAndBounded(t *testing.T) {
	e := newTestEngine(t)
	project, thread, turn := nuwaValidationSource(t, e, "Selected project")
	other, foreignThread, foreignTurn := nuwaValidationSource(t, e, "Unrelated project")
	foreign := nuwaValidationNode(t, e, other, foreignThread, foreignTurn, "Foreign source", "do not disclose")
	detail := strings.Repeat("原始观察保留数字42与否定条件，不可省略。", 100)
	first := nuwaValidationNode(t, e, project, thread, turn, "First observation", detail)
	second := nuwaValidationNode(t, e, project, thread, turn, "Second observation", "separate detail")
	type index struct {
		Nodes []store.NuwaNode `json:"nodes"`
		Total int              `json:"total"`
		Next  int              `json:"next_offset"`
		More  bool             `json:"more"`
	}
	var page index
	body, err := e.readNuwaWork(t.Context(), project.ID, `{"limit":1}`)
	if err != nil || json.Unmarshal([]byte(body), &page) != nil || page.Total != 2 || !page.More || page.Next != 1 || len(page.Nodes) != 1 || page.Nodes[0].Detail != "" || page.Nodes[0].ID != first.ID || strings.Contains(body, foreign.ID) {
		t.Fatalf("scoped summary page=%s err=%v", body, err)
	}
	body, err = e.readNuwaWork(t.Context(), project.ID, `{"offset":1,"limit":1}`)
	if err != nil || json.Unmarshal([]byte(body), &page) != nil || page.More || len(page.Nodes) != 1 || page.Nodes[0].ID != second.ID {
		t.Fatalf("second page=%s err=%v", body, err)
	}
	for _, args := range []string{`{"offset":999}`, `{"kind":"task"}`} {
		body, err := e.readNuwaWork(t.Context(), project.ID, args)
		if err != nil || json.Unmarshal([]byte(body), &page) != nil || len(page.Nodes) != 0 || page.More {
			t.Fatalf("empty page=%s err=%v", body, err)
		}
	}
	for _, args := range []string{`{"offset":-1}`, `{"limit":-1}`, `{"unknown":true}`, `{} {}`, `{"id":"` + foreign.ID + `"}`} {
		if _, err := e.readNuwaWork(t.Context(), project.ID, args); err == nil {
			t.Fatalf("invalid or foreign query was accepted: %s", args)
		}
	}
	e.cfg.Tools.ResultMaxChars = 900
	type full struct {
		Node  store.NuwaNode `json:"node"`
		Next  int            `json:"next_offset"`
		Total int            `json:"detail_total"`
		More  bool           `json:"more"`
	}
	var combined strings.Builder
	for offset := 0; ; {
		args := fmt.Sprintf(`{"id":%q,"offset":%d,"limit":120}`, first.ID, offset)
		body, err := e.readNuwaWork(t.Context(), project.ID, args)
		var response full
		if err != nil || utf8.RuneCountInString(body) > 900 || json.Unmarshal([]byte(body), &response) != nil || response.Total != utf8.RuneCountInString(detail) {
			t.Fatalf("complete bounded detail page=%s err=%v", body, err)
		}
		combined.WriteString(response.Node.Detail)
		if !response.More {
			break
		}
		if response.Next <= offset {
			t.Fatal("detail paging made no progress")
		}
		offset = response.Next
	}
	if combined.String() != detail {
		t.Fatal("bounded pages changed the original Chinese evidence")
	}
	if _, err := e.readNuwaWork(t.Context(), project.ID, fmt.Sprintf(`{"id":%q,"offset":999999}`, first.ID)); err != nil {
		t.Fatalf("detail end offset: %v", err)
	}
	// Whole summary nodes must shrink as a page, never as invalid JSON.
	e.cfg.Tools.ResultMaxChars = 1300
	body, err = e.readNuwaWork(t.Context(), project.ID, `{}`)
	if err != nil || json.Unmarshal([]byte(body), &page) != nil || len(page.Nodes) == 0 || utf8.RuneCountInString(body) > 1300 {
		t.Fatalf("budgeted index=%s err=%v", body, err)
	}
	huge := nuwaValidationNode(t, e, project, thread, turn, strings.Repeat("大", 3000), "detail")
	for _, args := range []string{fmt.Sprintf(`{"id":%q}`, huge.ID), `{"offset":2}`} {
		if _, err := e.readNuwaWork(t.Context(), project.ID, args); err == nil || !strings.Contains(err.Error(), "metadata exceeds") {
			t.Fatalf("oversized metadata silently looked complete: %v", err)
		}
	}
	if _, err := e.readNuwaWork(t.Context(), "unknown-project", `{}`); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project error=%v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.readNuwaWork(cancelled, project.ID, `{}`); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error=%v", err)
	}
}

func TestNuwaValidationModelCatalogAndDefaultMode(t *testing.T) {
	e := newTestEngine(t)
	disabled := false
	e.cfg.Models = config.ModelsConfig{Default: "available", Providers: []config.Provider{
		{ID: "available", BaseURL: "http://127.0.0.1:1", Model: "selected-model", ContextWindow: 4096},
		{ID: "incomplete", Model: "unready-model"},
		{ID: "disabled", BaseURL: "http://127.0.0.1:1", Model: "disabled-model", Enabled: &disabled},
	}}
	e.pool = provider.New(e.cfg)
	project, thread, turn := nuwaValidationSource(t, e, "Catalog project")
	turn.ProviderID, turn.Model = "available", "selected-model"
	catalog := e.nuwaModels(turn)
	if len(catalog) != 1 || catalog[0].ProviderID != "available" || !catalog[0].Default || catalog[0].Ref != "model:"+base64.RawURLEncoding.EncodeToString([]byte(provider.ChoiceID("available", "selected-model"))) {
		t.Fatalf("catalog included unavailable choices or lost its opaque reference: %+v", catalog)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(thread)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	e.wireNuwaRegistry(reg, set, pc, nil)
	standard := *turn
	standard.NuwaEnabled = false
	e.wireNuwaRegistry(reg, set, pc, &standard)
	if current := reg.WorkerConfiguration(); current.SelectWorkerModel != nil || current.EnableDelegation {
		t.Fatal("standard mode enabled model routing or nested delegation")
	}
	for _, base := range reg.Tools() {
		info, err := base.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "spawn_agent" {
			params, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := params.Properties.Get("model_ref"); ok {
				t.Fatal("standard spawn advertised opt-in selection")
			}
		}
	}
	e.wireNuwaRegistry(reg, set, pc, turn)
	current := reg.WorkerConfiguration()
	if current.SelectWorkerModel == nil || !current.EnableDelegation {
		t.Fatal("Nuwa routing was not wired")
	}
	if _, _, err := current.SelectWorkerModel(t.Context(), "unknown-reference"); err == nil {
		t.Fatal("unknown model selection silently fell back")
	}
	binding, builder, err := current.SelectWorkerModel(t.Context(), "")
	if err != nil || builder == nil || binding.ProviderID != turn.ProviderID || binding.Model != turn.Model {
		t.Fatalf("default binding=%+v builder=%v err=%v", binding, builder != nil, err)
	}
	if _, err := current.RestoreWorkerModel(t.Context(), binding); err != nil {
		t.Fatal(err)
	}
	binding, _, err = current.SelectWorkerModel(t.Context(), catalog[0].Ref)
	if err != nil || binding.Ref != provider.ChoiceID("available", "selected-model") {
		t.Fatalf("explicit binding=%+v err=%v", binding, err)
	}
	readOnly := *turn
	readOnly.NuwaReadOnly = true
	readonlyReg := swarm.NewRegistry()
	t.Cleanup(readonlyReg.Close)
	e.wireNuwaRegistry(readonlyReg, set, pc, &readOnly)
	for _, base := range readonlyReg.WorkerConfiguration().SubAgentTools {
		info, err := base.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "record_work" {
			t.Fatal("planning worker was given shared writes")
		}
	}
	if !strings.Contains(readonlyReg.WorkerConfiguration().WorkerPreamble, "Planning is read-only") || project.ID == "" {
		t.Fatal("read-only worker lost its instruction")
	}
	for _, ctx := range []context.Context{t.Context(), func() context.Context { ctx, cancel := context.WithCancel(t.Context()); cancel(); return ctx }()} {
		base := e.nuwaModelTool(turn)
		if _, err := base.Info(ctx); err != nil {
			t.Fatal(err)
		}
		out, err := base.(tool.InvokableTool).InvokableRun(ctx, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			if !strings.Contains(out, "canceled") {
				t.Fatalf("cancelled catalog=%s", out)
			}
		} else if !strings.Contains(out, catalog[0].Ref) {
			t.Fatalf("catalog receipt=%s", out)
		}
	}
}

func TestNuwaValidationArgumentAndCallerRefusals(t *testing.T) {
	for _, args := range []string{`{`, `{}` + `{}`, `{"agent_id":"pretend-runtime"}`, `{"kind":7}`} {
		var decoded recordWorkArgs
		if err := decodeNuwaArgs(args, &decoded); err == nil {
			t.Fatalf("malformed/forged arguments accepted: %s", args)
		}
	}
	var empty recordWorkArgs
	if err := decodeNuwaArgs(" ", &empty); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(t)
	project, _, turn := nuwaValidationSource(t, e, "Caller validation")
	if _, err := e.recordNuwaWork(t.Context(), project.ID, "", turn, `{}`); err == nil || !strings.Contains(err.Error(), "actual Nuwa agent") {
		t.Fatalf("untrusted caller error=%v", err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("validation caller cancelled")
	cancel(cause)
	if _, err := e.recordNuwaWork(ctx, project.ID, "", turn, `{}`); !errors.Is(err, cause) {
		t.Fatalf("caller cancellation was replaced: %v", err)
	}
}

// Refusals must return through the actual Registry/ADK/HTTP loop so a model can
// correct an invalid call; a direct hook call cannot prove this boundary.
func TestNuwaValidationActualManagerRefusesForgedOrInvalidWrites(t *testing.T) {
	for _, args := range []string{
		`{"kind":"fact","title":"Observed","agent_id":"someone-else"}`,
		`{"kind":"fact","title":"Observed","unexpected":true}`,
		`{"id":"missing-node","version":1,"summary":"change"}`,
		`{"kind":"artifact","title":"Missing file","artifact_path":"absent.txt"}`,
		`{"kind":"fact","title":"Missing source","evidence":[{"thread_id":"foreign","seq":1}]}`,
	} {
		t.Run(args, func(t *testing.T) {
			e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					writeScheduleCompletionTool(w, request, "record_work", args)
					return
				}
				last := request.Messages[len(request.Messages)-1]
				if last.Role != "tool" || !strings.Contains(last.Content, `"ok":false`) {
					t.Errorf("refused write did not reach the actual next model request: %s", last.Content)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The invalid operation was refused."}, "stop")
			})
			project, err := e.CreateProject("Actual write refusal", "", "", false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit refusal request", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Check the supplied evidence.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
			if got.Status != store.TurnDone || err != nil || len(nodes) != 0 || len(calls.snapshot()) != 2 {
				t.Fatalf("refusal status=%s nodes=%+v err=%v calls=%d", got.Status, nodes, err, len(calls.snapshot()))
			}
		})
	}
}

func TestNuwaValidationPlanningManagerCannotCallSharedWrites(t *testing.T) {
	e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		for _, available := range request.Tools {
			if available.Function.Name == "record_work" {
				t.Error("planning manager was given record_work")
			}
		}
		writeScheduleCompletionTool(w, request, "record_work", `{"kind":"fact","title":"Unapproved planning write"}`)
	})
	project, err := e.CreateProject("Read-only planning", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Plan without writes", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetPlanMode(thread.ID, true); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Explore the supplied evidence before proposing a plan.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
	if got.Status != store.TurnError || len(nodes) != 0 || err != nil || len(calls.snapshot()) != 1 {
		t.Fatalf("planning tool refusal=%+v nodes=%+v err=%v requests=%d", got, nodes, err, len(calls.snapshot()))
	}
}

// Complete writes and CAS patches cross the same actual model/tool boundary as
// refusals. A correction must keep omitted fields and explicitly clear arrays.
func TestNuwaValidationActualManagerPreservesAndClearsCASPatchFields(t *testing.T) {
	var threadID string
	var taskID, factID string
	const selected = "result.txt \t"
	e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		var result struct {
			OK   bool           `json:"ok"`
			Node store.NuwaNode `json:"node"`
		}
		if step > 1 && step <= 7 {
			last := request.Messages[len(request.Messages)-1]
			if err := json.Unmarshal([]byte(last.Content), &result); err != nil || !result.OK {
				t.Errorf("actual receipt=%s err=%v", last.Content, err)
			}
		}
		var args map[string]any
		switch step {
		case 1:
			args = map[string]any{"kind": "task", "title": "Inspect evidence", "status": "done"}
		case 2:
			taskID = result.Node.ID
			args = map[string]any{"kind": "fact", "title": "Recorded observation", "summary": "Original report", "detail": "完整原始证据，不可替换为猜测。", "status": "reported", "depends_on": []string{taskID}, "evidence": []store.NuwaEvidence{{ThreadID: threadID, Seq: 1}}}
		case 3:
			factID = result.Node.ID
			args = map[string]any{"id": factID, "version": result.Node.Version, "summary": "Corrected report"}
		case 4:
			if result.Node.Kind != "fact" || result.Node.Title != "Recorded observation" || result.Node.Detail != "完整原始证据，不可替换为猜测。" || len(result.Node.Evidence) != 1 || len(result.Node.DependsOn) != 1 || result.Node.DependsOn[0] != taskID {
				t.Errorf("patch discarded omitted data: %+v", result.Node)
			}
			args = map[string]any{"id": factID, "version": result.Node.Version, "detail": "Updated detail without replacing its summary"}
		case 5:
			if result.Node.Summary != "Corrected report" {
				t.Errorf("omitting summary changed it: %+v", result.Node)
			}
			args = map[string]any{"id": factID, "version": result.Node.Version, "kind": "fact", "title": "Rechecked observation", "summary": "Latest report", "detail": "Latest evidence", "status": "reported", "depends_on": []string{}, "evidence": []store.NuwaEvidence{}, "artifact_path": ""}
		case 6:
			if len(result.Node.DependsOn) != 0 || len(result.Node.Evidence) != 0 || result.Node.Title != "Rechecked observation" {
				t.Errorf("explicit patch fields were ignored: %+v", result.Node)
			}
			args = map[string]any{"kind": "artifact", "title": "Reviewable file", "artifact_path": selected, "depends_on": []string{factID}}
		case 7:
			if result.Node.ArtifactPath != selected || result.Node.Kind != "artifact" {
				t.Errorf("published artifact lost exact selected path: %+v", result.Node)
			}
			nuwaValidationTool(w, request, "validation-read", "read_work", `{}`)
			return
		default:
			var index struct {
				Nodes []store.NuwaNode `json:"nodes"`
			}
			last := request.Messages[len(request.Messages)-1]
			if err := json.Unmarshal([]byte(last.Content), &index); err != nil || len(index.Nodes) != 3 {
				t.Errorf("registered read_work failed at actual model boundary: %s err=%v", last.Content, err)
			}
			for _, node := range index.Nodes {
				if node.Detail != "" {
					t.Error("index inlined detailed evidence")
				}
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The corrected evidence and selected artifact are recorded."}, "stop")
			return
		}
		body, err := json.Marshal(args)
		if err != nil {
			t.Errorf("encode fixture: %v", err)
			http.Error(w, "invalid fixture", 500)
			return
		}
		nuwaValidationTool(w, request, fmt.Sprintf("validation-write-%d", step), "record_work", string(body))
	})
	project, err := e.CreateProject("Actual complete records", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Explicit complete records", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	threadID = thread.ID
	for _, name := range []string{selected, strings.TrimSpace(selected)} {
		if err := os.WriteFile(filepath.Join(e.WorkspaceDir(thread.ID), name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the current evidence and publish its reviewable result.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil || got.Status != store.TurnDone || len(view.Nodes) != 3 || len(calls.snapshot()) != 8 {
		t.Fatalf("actual records turn=%+v nodes=%+v err=%v calls=%d", got, view.Nodes, err, len(calls.snapshot()))
	}
}

// Public Registry/ADK adapters may be embedded by hosts. These negative hosts
// own the right runtime but omit its history owner, or bind a vanished project;
// the actual tool call must refuse instead of manufacturing trusted context.
func TestNuwaValidationActualRegisteredHostRefusesMissingAuthority(t *testing.T) {
	for _, mode := range []string{"missing-owner", "missing-project"} {
		t.Run(mode, func(t *testing.T) {
			e, calls := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					nuwaValidationTool(w, request, "validation-authority", "record_work", `{"id":"existing-node","version":1,"summary":"change"}`)
					return
				}
				last := request.Messages[len(request.Messages)-1]
				if !strings.Contains(last.Content, `"ok":false`) || (mode == "missing-owner" && !strings.Contains(last.Content, "matching trusted turn owner")) {
					t.Errorf("missing host authority reached write: %s", last.Content)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The unavailable write was refused."}, "stop")
			})
			project, thread, turn := nuwaValidationSource(t, e, "Host authority")
			set, err := e.conversationTools(t.Context(), thread)
			if err != nil {
				t.Fatal(err)
			}
			pc, err := e.projectContextFor(thread)
			if err != nil {
				t.Fatal(err)
			}
			builder, err := e.pool.ModelBuilder(t.Context(), turn.ProviderID, turn.Model, "", e.callRecorder(thread.ID, turn.ID))
			if err != nil {
				t.Fatal(err)
			}
			reg := e.newTurnRegistry(builder, set, pc, turn)
			t.Cleanup(reg.Close)
			acc := newAccumulator(e, thread.ID, turn.ID, 0)
			rt := e.runtimeFor(thread.ID)
			var result swarm.RunResult
			if mode == "missing-owner" {
				ctx, release := ownManagerTestFixture(t.Context(), rt, turn.ID, reg)
				result, err = reg.RunWith(ctx, swarm.RunConfig{Instruction: ManagerPrompt(set, ""), Task: "Inspect the current evidence.", ManagerTools: e.nuwaWorkTools(project.ID, set.WorkspaceDir, turn, false)}, swarm.Callback(acc.onNotify))
				release()
			} else {
				vanished := *pc.project
				vanished.ID = "missing-project"
				pc.project = &vanished
				result, err = rt.runOwnedTestManager(t.Context(), turn, reg, acc, set, pc, []adk.Message{schema.UserMessage("Inspect the current evidence.")})
			}
			acc.flushAll()
			if err != nil || result.Final == "" || len(calls.snapshot()) != 2 {
				t.Fatalf("authority refusal failed in the actual adapter: result=%+v err=%v requests=%d", result, err, len(calls.snapshot()))
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, result.Final, ""); err != nil {
				t.Fatal(err)
			}
			_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
			if err != nil || len(nodes) != 0 {
				t.Fatalf("refused host mutated shared state: %+v err=%v", nodes, err)
			}
		})
	}
}

func nuwaValidationTool(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args},
	}}}, "tool_calls")
}

func nuwaValidationReceipt(request scheduleCompletionRequest, id string) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "tool" && request.Messages[i].ToolCallID == id {
			return request.Messages[i].Content
		}
	}
	return ""
}

// These are actual HTTP workers, not model-authored completion claims. The
// projection must distinguish their success, provider failure and cancellation.
func TestNuwaValidationSnapshotTracksActualHTTPWorkerOutcomes(t *testing.T) {
	for _, outcome := range []string{"done", "failed", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			workerReady := make(chan struct{})
			var ready sync.Once
			e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, _ int, request scheduleCompletionRequest) {
				manager := len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "You are the manager of a team")
				if !manager {
					ready.Do(func() { close(workerReady) })
					switch outcome {
					case "failed":
						http.Error(w, `{"error":{"type":"invalid_request_error","message":"The model service rejected this work"}}`, http.StatusBadRequest)
					case "cancelled":
						<-r.Context().Done()
					default:
						writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned evidence was inspected."}, "stop")
					}
					return
				}
				spawned := nuwaValidationReceipt(request, "validation-spawn")
				if spawned == "" {
					nuwaValidationTool(w, request, "validation-spawn", "spawn_agent", `{"role":"validation-worker","task":"Inspect the supplied evidence and report the outcome."}`)
					return
				}
				var identity struct {
					AgentID string `json:"agent_id"`
				}
				if err := json.Unmarshal([]byte(spawned), &identity); err != nil || identity.AgentID == "" {
					t.Errorf("spawn receipt=%s err=%v", spawned, err)
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The operation was refused."}, "stop")
					return
				}
				if outcome == "cancelled" && nuwaValidationReceipt(request, "validation-close") == "" {
					select {
					case <-workerReady:
					case <-time.After(5 * time.Second):
						t.Error("worker never made an actual HTTP request")
					}
					nuwaValidationTool(w, request, "validation-close", "close_agent", fmt.Sprintf(`{"agent_id":%q}`, identity.AgentID))
					return
				}
				if nuwaValidationReceipt(request, "validation-wait") == "" {
					nuwaValidationTool(w, request, "validation-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, identity.AgentID))
					return
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned worker outcome was collected."}, "stop")
			})
			project, err := e.CreateProject("Actual worker outcome", "", "", false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit actual worker", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect the current input through one delegated worker.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone {
				t.Fatalf("manager did not collect worker: %+v", got)
			}
			var snapshot NuwaProjectView
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				snapshot, err = e.ProjectNuwaSnapshot(t.Context(), project.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.Workers) == 1 && snapshot.Workers[0].Status == outcome {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != outcome || snapshot.Workers[0].ModelBinding == nil || snapshot.Workers[0].ModelBinding.Model != turn.Model {
				t.Fatalf("actual outcome %s projected as %+v", outcome, snapshot.Workers)
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			finished := false
			for _, event := range events {
				if event.Kind == swarm.NotifyFinished.String() && event.AgentID == snapshot.Workers[0].AgentID {
					finished = true
					if outcome == "cancelled" && !strings.HasPrefix(event.Err, "swarm: agent "+event.AgentID+" cancelled: ") {
						t.Fatalf("cancellation had no actual engine cause: %+v", event)
					}
				}
			}
			if !finished {
				t.Fatal("projection was accepted without the actual worker finished receipt")
			}
		})
	}
}

// Controlled durable logs isolate replay ordering and forged error-text
// counterexamples; actual worker producers are covered by the HTTP test above.
func TestNuwaValidationSnapshotHonorsAuthoritativeLifecycleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, err, want                                        string
		cleanup, close, checkpoint, finishedCheckpoint, resume bool
	}{
		{name: "success", want: "done"},
		{name: "model failure", err: "provider rejected the request", want: "failed"},
		{name: "same worker cancellation", err: "swarm: agent worker-1 cancelled: human stop", want: "cancelled"},
		{name: "wrong worker cancellation", err: "swarm: agent worker-other cancelled: human stop", want: "failed"},
		{name: "empty cancellation cause", err: "swarm: agent worker-1 cancelled: ", want: "failed"},
		{name: "ordinary interrupted string", err: "interrupted", want: "failed"},
		{name: "cleanup before finish", cleanup: true, want: "cancelled"},
		{name: "finished success survives cleanup", cleanup: true, finishedCheckpoint: true, want: "done"},
		{name: "close awaits its actual callback", close: true, want: "cancelled"},
		{name: "human stopped checkpoint", checkpoint: true, want: "cancelled"},
		{name: "finished checkpoint survives human stop", checkpoint: true, finishedCheckpoint: true, want: "done"},
		{name: "new generation rejects old stopped checkpoint", checkpoint: true, resume: true, err: "interrupted", want: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			project, thread, turn := nuwaValidationSource(t, e, "Lifecycle replay")
			appendEvent := func(event store.Event) {
				t.Helper()
				event.ThreadID, event.TurnID = thread.ID, turn.ID
				if err := e.store.AppendEvent(&event); err != nil {
					t.Fatal(err)
				}
			}
			spawn := store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "reviewer", WorkerGeneration: 3, ParentID: "manager", ModelBinding: &swarm.ModelBinding{ProviderID: turn.ProviderID, Model: "bound-model"}}
			appendEvent(spawn)
			if tc.checkpoint {
				body, err := json.Marshal(restartCheckpoint{Version: 1, HumanStopped: true, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "reviewer", Generation: 3, Finished: tc.finishedCheckpoint, Result: "Earlier accepted result", ModelBinding: spawn.ModelBinding}}})
				if err != nil {
					t.Fatal(err)
				}
				appendEvent(store.Event{Kind: KindRestartCheckpoint, AgentID: "manager", Text: string(body)})
				if tc.resume {
					spawn.WorkerGeneration = 4
					appendEvent(spawn)
					appendEvent(store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Err: tc.err})
				}
			} else if tc.close {
				appendEvent(store.Event{Kind: swarm.NotifyToolCall.String(), AgentID: "manager", ToolCallID: "close-current", Text: `close_agent({"agent_id":"worker-1"})`})
				appendEvent(store.Event{Kind: swarm.NotifyToolResult.String(), AgentID: "manager", ToolCallID: "close-current", Text: `{"agent_id":"worker-1","generation":3,"cancelled":true}`})
			} else if tc.cleanup {
				if tc.finishedCheckpoint {
					appendEvent(store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Text: "Accepted result"})
				}
				appendEvent(store.Event{Kind: KindCleanup, AgentID: "manager", Text: "stopped unfinished work"})
			} else {
				appendEvent(store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Text: "Reported result", Err: tc.err})
			}
			view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
			if err != nil || len(view.Workers) != 1 || view.Workers[0].Status != tc.want {
				t.Fatalf("replay wanted %s, got %+v err=%v", tc.want, view.Workers, err)
			}
			if view.Workers[0].ThreadID != thread.ID || view.Workers[0].TurnID != turn.ID {
				t.Fatalf("worker lost its original source: %+v", view.Workers[0])
			}
		})
	}
}

func TestNuwaValidationWorkerInvalidationUsesDurableProjectOwnership(t *testing.T) {
	e := newTestEngine(t)
	project, thread, turn := nuwaValidationSource(t, e, "Worker invalidation")
	for _, event := range []store.Event{{Kind: "delta", ThreadID: thread.ID}, {Kind: swarm.NotifySpawned.String(), ThreadID: "missing"}} {
		e.invalidateNuwaWorkers(event)
	}
	plain, err := e.CreateThread("Nonproject worker", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.invalidateNuwaWorkers(store.Event{Kind: swarm.NotifySpawned.String(), ThreadID: plain.ID})
	ordinary, err := e.CreateProject("Standard project", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	standardThread, err := e.CreateThread("Standard worker", "", ordinary.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.invalidateNuwaWorkers(store.Event{Kind: swarm.NotifySpawned.String(), ThreadID: standardThread.ID})
	if revision, _, err := e.store.NuwaSnapshot(t.Context(), project.ID); err != nil || revision != 0 {
		t.Fatalf("unrelated events changed revision=%d err=%v", revision, err)
	}
	e.record(store.Event{Kind: swarm.NotifySpawned.String(), ThreadID: thread.ID, TurnID: turn.ID, AgentID: "owned-worker", Role: "reviewer", WorkerGeneration: 1})
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil || view.Revision != 1 || len(view.Workers) != 1 || view.Workers[0].Status != "running" {
		t.Fatalf("persisted worker was not invalidated: %+v err=%v", view, err)
	}
	if _, err := e.ProjectNuwaSnapshot(t.Context(), "missing-project"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing projection error=%v", err)
	}
	// A private Store fault remains registered through Close; no background actor
	// can outlive a callback that this test removes early.
	faulted := newTestEngineAt(t, t.TempDir())
	fp, ft, _ := nuwaValidationSource(t, faulted, "Storage failure")
	if err := faulted.store.DB().Callback().Create().Before("gorm:create").Register("nuwa-validation-revision-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "nuwa_revisions" {
			_ = tx.AddError(errors.New("revision write refused"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	faulted.invalidateNuwaWorkers(store.Event{Kind: swarm.NotifyFinished.String(), ThreadID: ft.ID})
	if revision, _, err := faulted.store.NuwaSnapshot(t.Context(), fp.ID); err != nil || revision != 0 {
		t.Fatalf("failed revision was falsely durable: revision=%d err=%v", revision, err)
	}
}
