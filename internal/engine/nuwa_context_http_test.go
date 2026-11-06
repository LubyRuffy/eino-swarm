package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const nuwaContextHTTPHeader = "## Nuwa shared project state"

type nuwaContextHTTPView struct {
	ProjectID string           `json:"project_id"`
	Revision  int64            `json:"revision"`
	Nodes     []store.NuwaNode `json:"nodes"`
}

// Parse only the runtime System block. Human messages may legitimately quote
// its heading; matching text alone must neither delete those messages nor
// count them as host-supplied collaboration state.
func nuwaContextHTTPBlock(t *testing.T, request scheduleCompletionRequest, want bool) nuwaContextHTTPView {
	t.Helper()
	var blocks []string
	for _, message := range request.Messages {
		if message.Role != schema.System {
			continue
		}
		count := strings.Count(message.Content, nuwaContextHTTPHeader)
		if count > 1 {
			t.Errorf("one System message accumulated %d shared-state blocks", count)
		}
		if count > 0 {
			blocks = append(blocks, message.Content)
		}
	}
	if !want {
		if len(blocks) != 0 {
			t.Errorf("ordinary model input gained %d Nuwa runtime blocks", len(blocks))
		}
		return nuwaContextHTTPView{}
	}
	if len(blocks) != 1 {
		t.Errorf("actual model input has %d shared-state System blocks, want one without read_work", len(blocks))
		return nuwaContextHTTPView{}
	}
	block := blocks[0]
	for _, term := range []string{"untrusted", "reported"} {
		if !strings.Contains(strings.ToLower(block), term) {
			t.Errorf("shared-state block does not distinguish %s observations from instructions or verified facts", term)
		}
	}
	body := block[strings.Index(block, nuwaContextHTTPHeader)+len(nuwaContextHTTPHeader):]
	start := strings.Index(body, "{")
	if start < 0 {
		t.Error("shared-state block has no JSON data")
		return nuwaContextHTTPView{}
	}
	var view nuwaContextHTTPView
	if err := json.NewDecoder(strings.NewReader(body[start:])).Decode(&view); err != nil {
		t.Errorf("shared-state block is not complete JSON: %v", err)
	}
	return view
}

func nuwaContextHTTPNode(t *testing.T, view nuwaContextHTTPView, id string) store.NuwaNode {
	t.Helper()
	for _, node := range view.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Errorf("shared-state revision %d omitted node %q", view.Revision, id)
	return store.NuwaNode{}
}

func nuwaContextHTTPSource(t *testing.T, e *Engine, name string) (*store.Project, *store.Thread, *store.Turn) {
	t.Helper()
	project, thread, turn := nuwaValidationSource(t, e, name)
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	return project, thread, turn
}

func assertNuwaContextNotDurable(t *testing.T, e *Engine, threadID, human string) {
	t.Helper()
	messages, err := e.store.ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		if message.Role == string(schema.User) && message.Content == human {
			found = true
			continue
		}
		if strings.Contains(message.Content, nuwaContextHTTPHeader) {
			t.Errorf("transient shared-state block entered durable %s history", message.Role)
		}
	}
	if !found {
		t.Error("literal human input was removed or rewritten")
	}
}

// A new graph observation must affect the next decision even when the model
// never elects to call read_work. Updating between HTTP requests distinguishes
// per-call middleware from a prompt assembled only at turn admission.
func TestNuwaSharedContextRefreshesManagerAtActualHTTP(t *testing.T) {
	var e *Engine
	var source store.NuwaNode
	var editor *store.Thread
	var editTurn *store.Turn
	correction := store.NewID("correction_")
	var fixture *scheduleCompletionHTTP
	e, fixture = newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			update := source
			update.ThreadID, update.TurnID = editor.ID, editTurn.ID
			update.Summary = correction
			if err := e.store.UpsertNuwaNode(t.Context(), &update, source.Version); err != nil {
				t.Errorf("commit inter-request correction: %v", err)
			}
			nuwaValidationTool(w, request, "context-catalog", "list_models", `{}`)
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The current collaboration state was inspected."}, "stop")
	})
	e.cfg.Memory.AutoReview = false
	project, seedThread, seedTurn := nuwaContextHTTPSource(t, e, "Shared decision context")
	source = nuwaValidationNode(t, e, project, seedThread, seedTurn, store.NewID("source_"), "")
	dependent := store.NuwaNode{ProjectID: project.ID, ThreadID: seedThread.ID, TurnID: seedTurn.ID,
		AgentID: swarm.DefaultManagerID, Kind: "task", Title: store.NewID("dependent_"), DependsOn: []string{source.ID}}
	if err := e.store.UpsertNuwaNode(t.Context(), &dependent, 0); err != nil {
		t.Fatal(err)
	}
	editor, err := e.CreateThread("Independent state editor", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	editTurn = &store.Turn{ThreadID: editor.ID, ProjectID: project.ID, NuwaEnabled: true}
	if err := e.store.CreateTurn(editTurn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(editTurn.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	foreignProject, foreignThread, foreignTurn := nuwaContextHTTPSource(t, e, "Unrelated state")
	foreign := nuwaValidationNode(t, e, foreignProject, foreignThread, foreignTurn, store.NewID("foreign_"), "")
	thread, err := e.CreateThread("Current decision", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	const human = "Inspect the current shared state before deciding the next step."
	turn, err := e.StartTurn(thread.ID, human)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	requests := fixture.snapshot()
	if finished.Status != store.TurnDone || len(requests) != 2 {
		t.Fatalf("shared-state decision: turn=%+v requests=%d", finished, len(requests))
	}
	first := nuwaContextHTTPBlock(t, requests[0], true)
	next := nuwaContextHTTPBlock(t, requests[1], true)
	if first.ProjectID != project.ID || next.ProjectID != project.ID || first.Revision != 2 || next.Revision != 3 {
		t.Errorf("shared state was not re-read per request: first=%+v next=%+v", first, next)
	}
	initialSource, changedSource := nuwaContextHTTPNode(t, first, source.ID), nuwaContextHTTPNode(t, next, source.ID)
	initialDependent, changedDependent := nuwaContextHTTPNode(t, first, dependent.ID), nuwaContextHTTPNode(t, next, dependent.ID)
	if initialSource.Version != 1 || initialSource.Status != "reported" || changedSource.Version != 2 || changedSource.Summary != correction || changedSource.Status != "reported" {
		t.Errorf("reported observation/correction lost: before=%+v after=%+v", initialSource, changedSource)
	}
	if changedSource.ThreadID != seedThread.ID || changedSource.UpdatedThreadID != editor.ID || changedSource.UpdatedTurnID != editTurn.ID {
		t.Errorf("automatic context lost original versus correction identity: %+v", changedSource)
	}
	if initialDependent.Status != "pending" || len(initialDependent.StaleDependencies) != 0 || len(changedDependent.StaleDependencies) != 1 || changedDependent.StaleDependencies[0] != source.ID {
		t.Errorf("corrected basis still appeared current: before=%+v after=%+v", initialDependent, changedDependent)
	}
	for i, request := range requests {
		if strings.Contains(joinedContent(request.Messages), foreign.ID) {
			t.Errorf("request %d disclosed another project's state", i+1)
		}
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				if call.Function.Name == "read_work" {
					t.Error("fixture accidentally depended on an explicit shared-state read")
				}
			}
		}
	}
	assertNuwaContextNotDurable(t, e, thread.ID, human)
}

// Recovery must obey the turn's frozen mode; changing the project switch is
// not permission to inject a graph into an ordinary turn or to hide a Nuwa one.
func TestNuwaSharedContextUsesFrozenTurnModeAtActualHTTP(t *testing.T) {
	for _, nuwa := range []bool{false, true} {
		for _, recovery := range []bool{false, true} {
			t.Run(fmt.Sprintf("nuwa=%v/recovery=%v", nuwa, recovery), func(t *testing.T) {
				e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The current input was inspected."}, "stop")
				})
				e.cfg.Memory.AutoReview = false
				project, seedThread, seedTurn := nuwaContextHTTPSource(t, e, "Frozen shared-state mode")
				node := nuwaValidationNode(t, e, project, seedThread, seedTurn, store.NewID("visible_"), "")
				if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &nuwa); err != nil {
					t.Fatal(err)
				}
				thread, err := e.CreateThread("Mode boundary", "", project.ID)
				if err != nil {
					t.Fatal(err)
				}
				human := "Preserve this literal reference:\n" + nuwaContextHTTPHeader + "\n" + store.NewID("human_")
				var turn *store.Turn
				if recovery {
					turn = &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, NuwaEnabled: nuwa,
						ProviderID: e.cfg.Models.Providers[0].ID, Model: e.cfg.Models.Providers[0].Model, UserText: human}
					if err := e.store.CreateTurn(turn); err != nil {
						t.Fatal(err)
					}
					if err := e.store.AppendMessages(thread.ID, turn.ID, []store.Message{{Role: string(schema.User), Content: human, Source: store.MessageSourceHuman}}); err != nil {
						t.Fatal(err)
					}
					changed := !nuwa
					if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &changed); err != nil {
						t.Fatal(err)
					}
					if count, err := e.ResumeOrphanedTurns(); err != nil || count != 1 {
						t.Fatalf("recover frozen turn: count=%d err=%v", count, err)
					}
				} else {
					turn, err = e.StartTurn(thread.ID, human)
					if err != nil {
						t.Fatal(err)
					}
				}
				finished := waitForTurn(t, e, turn.ID)
				waitSettled(t, e, thread.ID)
				requests := fixture.snapshot()
				if finished.Status != store.TurnDone || len(requests) != 1 {
					t.Fatalf("frozen mode: turn=%+v requests=%d", finished, len(requests))
				}
				view := nuwaContextHTTPBlock(t, requests[0], nuwa)
				if nuwa {
					if view.ProjectID != project.ID {
						t.Errorf("wrong shared project %q", view.ProjectID)
					}
					nuwaContextHTTPNode(t, view, node.ID)
				} else if strings.Contains(joinedContent(requests[0].Messages), node.ID) {
					t.Error("ordinary turn received shared graph data")
				}
				if !strings.Contains(joinedContent(requests[0].Messages), human) {
					t.Error("runtime-block filtering removed a real human quotation")
				}
				assertNuwaContextNotDurable(t, e, thread.ID, human)
			})
		}
	}
}
