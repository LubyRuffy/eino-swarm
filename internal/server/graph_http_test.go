package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const graphSecretFixture = "fake-credential-for-http-test"

func TestTraceRedactsCredentialsFromHistoricalGraphEvents(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	turn := &store.Turn{ThreadID: thread, Status: store.TurnDone, GraphTools: true}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	payload := `{"subjects":[{"kind":"repo","attrs":{"url":"https://fixture:` + graphSecretFixture + `@example.test/acme/app"}},{"id":"sj_existing","attrs":{"credential":"` + graphSecretFixture + `"}}]}`
	if err := h.app.Store.AppendEvent(&store.Event{
		ThreadID: thread, TurnID: turn.ID, Kind: "tool_call", WorkRecordPayload: true, Text: "update_graph(" + payload + ")",
	}); err != nil {
		t.Fatal(err)
	}
	body := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), graphSecretFixture) || !strings.Contains(string(raw), "https://example.test/acme/app") {
		t.Fatal("Trace exposed a credential or dropped the sanitized repository URL")
	}
}

func (h *harness) writeGraph(thread, project string, batch graph.Batch) graph.Result {
	h.t.Helper()
	turn := &store.Turn{ThreadID: thread, ProjectID: project, GraphTools: true}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		h.t.Fatal(err)
	}
	res, _, err := h.app.Store.ApplyGraph(context.Background(),
		store.GraphCaller{ThreadID: thread, TurnID: turn.ID, AgentID: "manager", ProjectID: project}, batch)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

// The panel reads the graph over HTTP. A credential is named and never sent,
// and a project graph is the same data under the project's owner.
func TestGraphRoutesServeTheGraphWithoutCredentials(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	h.writeGraph(thread, "", graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "r", Kind: "repo", Attrs: map[string]any{"path": "acme/app", "credential": graphSecretFixture}}, {Ref: "h", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.9"}}},
		Links:    []graph.LinkInput{{From: "r", To: "h", Rel: "deployed_on"}},
		Todos:    []graph.TodoInput{{Title: "check it", Status: graph.TodoDoing}},
	})
	resp := h.do(http.MethodGet, "/api/threads/"+thread+"/graph", nil)
	defer resp.Body.Close()
	var view store.GraphView
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&view) != nil {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if len(view.Subjects) != 2 || len(view.Links) != 1 || len(view.Todos) != 1 || view.Revision != 1 {
		t.Fatalf("view=%+v", view)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), graphSecretFixture) || !strings.Contains(string(raw), `"credentials":["credential"]`) {
		t.Fatalf("credential handling: %s", raw)
	}
	todos := h.json(http.MethodGet, "/api/threads/"+thread+"/graph?todos=1&status=doing", nil, http.StatusOK)
	if rows, _ := todos["todos"].([]any); len(rows) != 1 {
		t.Fatalf("todos=%v", todos)
	}
	one := h.json(http.MethodGet, "/api/threads/"+thread+"/graph?id="+view.Subjects[0].ID, nil, http.StatusOK)
	if len(one["subjects"].([]any)) == 0 {
		t.Fatalf("by id=%v", one)
	}
	h.json(http.MethodGet, "/api/threads/"+thread+"/graph?id=sj_missing", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/threads/"+thread+"/graph?limit=x", nil, http.StatusBadRequest)
	h.json(http.MethodGet, "/api/threads/"+thread+"/graph?offset=-1", nil, http.StatusBadRequest)
	h.json(http.MethodGet, "/api/threads/th_missing/graph", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/pr_missing/graph", nil, http.StatusNotFound)

	kinds := h.do(http.MethodGet, "/api/graph/kinds", nil)
	defer kinds.Body.Close()
	var catalog struct {
		Kinds []struct{ ID string } `json:"kinds"`
	}
	if json.NewDecoder(kinds.Body).Decode(&catalog) != nil || len(catalog.Kinds) < 10 {
		t.Fatalf("kinds=%+v", catalog)
	}
}

func TestProjectGraphRouteReadsTheSharedGraph(t *testing.T) {
	h := newHarness(t)
	p := &store.Project{Name: "shared"}
	if err := h.app.Store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "member", ProjectID: p.ID}
	if err := h.app.Store.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	h.writeGraph(th.ID, p.ID, graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.10"}}}})
	got := h.json(http.MethodGet, "/api/projects/"+p.ID+"/graph", nil, http.StatusOK)
	if rows, _ := got["subjects"].([]any); len(rows) != 1 {
		t.Fatalf("project graph=%v", got)
	}
	// The member conversation reads the same graph under its project.
	got = h.json(http.MethodGet, "/api/threads/"+th.ID+"/graph", nil, http.StatusOK)
	if rows, _ := got["subjects"].([]any); len(rows) != 1 {
		t.Fatalf("member read=%v", got)
	}
}

// graph_updated is its own frame: it carries the revision, never a chat cursor,
// and comes again each time a commit moves the revision.
func TestGraphUpdatedFrameFollowsEachCommit(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/threads/"+thread+"/events?graph=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	revision := func() int64 {
		t.Helper()
		for {
			frame := nextPanelFrame(t, scanner)
			if frame.kind != "graph_updated" {
				continue
			}
			if frame.id != "" {
				t.Fatalf("graph_updated changed the chat cursor: %+v", frame)
			}
			var payload struct {
				Revision int64 `json:"revision"`
			}
			if err := json.Unmarshal([]byte(frame.data), &payload); err != nil {
				t.Fatal(err)
			}
			return payload.Revision
		}
	}
	if rev := revision(); rev != 0 {
		t.Fatalf("a new connection starts from revision %d", rev)
	}
	first := h.writeGraph(thread, "", graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.11"}}}})
	if rev := revision(); rev != 1 {
		t.Fatalf("after one commit: %d", rev)
	}
	h.writeGraph(thread, "", graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "next", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.12"}}},
		Links:    []graph.LinkInput{{From: "next", To: first.Subjects[0].ID, Rel: "next"}},
	})
	if rev := revision(); rev != 2 {
		t.Fatalf("after a second commit: %d", rev)
	}
}

func TestGraphUpdatedFrameFollowsOwnerAtEqualAndLowerRevisions(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	h.writeGraph(thread, "", graph.Batch{Todos: []graph.TodoInput{{Title: "conversation task"}}})
	shared := &store.Project{Name: "Shared work"}
	empty := &store.Project{Name: "Empty work"}
	for _, project := range []*store.Project{shared, empty} {
		if err := h.app.Store.CreateProject(project); err != nil {
			t.Fatal(err)
		}
	}
	member := &store.Thread{Title: "Shared work member", ProjectID: shared.ID}
	if err := h.app.Store.CreateThread(member); err != nil {
		t.Fatal(err)
	}
	h.writeGraph(member.ID, shared.ID, graph.Batch{Todos: []graph.TodoInput{{Title: "shared task"}}})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := "/api/threads/" + thread
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+path+"/events?graph=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	type graphFrame struct {
		ThreadID string `json:"thread_id"`
		OwnerID  string `json:"owner_id"`
		Revision int64  `json:"revision"`
		Preview  bool   `json:"preview"`
	}
	next := func() graphFrame {
		t.Helper()
		for {
			frame := nextPanelFrame(t, scanner)
			if frame.kind != "graph_updated" {
				continue
			}
			var data graphFrame
			if err := json.Unmarshal([]byte(frame.data), &data); err != nil || frame.id != "" {
				t.Fatalf("invalid graph frame %+v: %v", frame, err)
			}
			return data
		}
	}
	initial := next()
	for _, step := range []struct {
		owner    string
		revision int64
		todos    int
	}{{shared.ID, 1, 1}, {empty.ID, 0, 0}, {"", 1, 1}} {
		h.json(http.MethodPatch, path, map[string]any{"project_id": step.owner}, http.StatusOK)
		owner := step.owner
		if owner == "" {
			owner = thread
		}
		// No graph write occurred. The already open stream must invalidate
		// even when both graphs have the same revision or the new one is empty.
		if got, want := next(), (graphFrame{ThreadID: thread, OwnerID: owner, Revision: step.revision}); got != want {
			t.Fatalf("after moving to %q: graph frame %+v, want %+v", owner, got, want)
		}
		res := h.do(http.MethodGet, path+"/graph", nil)
		var view store.GraphView
		err := json.NewDecoder(res.Body).Decode(&view)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || view.OwnerID != owner || view.Revision != step.revision || len(view.Todos) != step.todos {
			t.Fatalf("after moving to %q: graph response %d %+v, err=%v", owner, res.StatusCode, view, err)
		}
		if owner == shared.ID && view.Todos[0].Title != "shared task" {
			t.Fatalf("same revision returned the old owner's work: %+v", view.Todos[0])
		}
	}
	if want := (graphFrame{ThreadID: thread, OwnerID: thread, Revision: 1}); initial != want {
		t.Fatalf("initial graph frame %+v, want %+v", initial, want)
	}
}

// Trace is the one-id troubleshooting path: a turn that had the graph tools
// reports how many tool results the graph was never told about, and a turn
// without them reports nothing rather than a misleading zero.
func TestTraceReportsUnrecordedToolResultsOnlyForGraphTurns(t *testing.T) {
	h := newHarness(t)
	th := h.newThread()
	with := &store.Turn{ThreadID: th, GraphTools: true}
	without := &store.Turn{ThreadID: th}
	for _, turn := range []*store.Turn{with, without} {
		if err := h.app.Store.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			ev := &store.Event{ThreadID: th, TurnID: turn.ID, Kind: "tool_result", AgentID: "manager", Text: "r"}
			if err := h.app.Store.AppendEvent(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	trace := h.json(http.MethodGet, "/api/trace/"+with.ID, nil, http.StatusOK)
	if got, _ := trace["graph_unrecorded"].(float64); got != 2 {
		t.Fatalf("graph turn: graph_unrecorded=%v", trace["graph_unrecorded"])
	}
	trace = h.json(http.MethodGet, "/api/trace/"+without.ID, nil, http.StatusOK)
	if _, present := trace["graph_unrecorded"]; present {
		t.Fatalf("a turn without graph tools reported graph_unrecorded: %v", trace["graph_unrecorded"])
	}
}

// The panel button starts a turn. It does not write rows itself, and it does
// not run when work records are switched off.
func TestExtractGraphStartsACatchUpTurnAndRefusesWhenOff(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	got := h.json(http.MethodPost, "/api/threads/"+thread+"/graph/extract", nil, http.StatusAccepted)
	turn, _ := got["turn"].(map[string]any)
	id, _ := turn["id"].(string)
	stored, err := h.app.Store.GetTurn(id)
	if err != nil || stored.UserText != engine.GraphExtractText() || !stored.GraphTools || !stored.GraphReplay || stored.GraphClear {
		t.Fatalf("turn=%+v err=%v", stored, err)
	}
	other := h.newThread()
	cleared := h.json(http.MethodPost, "/api/threads/"+other+"/graph/extract?clear=1", nil, http.StatusAccepted)
	clearedTurn, _ := cleared["turn"].(map[string]any)
	clearedID, _ := clearedTurn["id"].(string)
	stored, err = h.app.Store.GetTurn(clearedID)
	if err != nil || !stored.GraphReplay || !stored.GraphClear {
		t.Fatalf("clear turn=%+v err=%v", stored, err)
	}
	if err := h.app.Config.Update(func(c *config.Config) error {
		c.Swarm.Graph = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	refused := h.json(http.MethodPost, "/api/threads/"+thread+"/graph/extract", nil, http.StatusConflict)
	if refused["code"] != "graph_disabled" {
		t.Fatalf("switch off: %+v", refused)
	}
}

func TestExtractGraphCannotClearAnOwnerTheUserDidNotConfirm(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	h.writeGraph(thread, "", graph.Batch{Todos: []graph.TodoInput{{Title: "Old scope"}}})
	project := &store.Project{Name: "Destination"}
	if err := h.app.Store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	member := &store.Thread{Title: "Project member", ProjectID: project.ID}
	if err := h.app.Store.CreateThread(member); err != nil {
		t.Fatal(err)
	}
	h.writeGraph(member.ID, project.ID, graph.Batch{Todos: []graph.TodoInput{{Title: "Keep this project work"}}})
	path := "/api/threads/" + thread
	confirmed := h.json(http.MethodGet, path+"/graph", nil, http.StatusOK)["owner_id"].(string)
	h.json(http.MethodPatch, path, map[string]any{"project_id": project.ID}, http.StatusOK)
	refused := h.json(http.MethodPost, path+"/graph/extract?clear=1&owner_id="+confirmed, nil, http.StatusConflict)
	if refused["code"] != "graph_conflict" {
		t.Fatalf("stale confirmation must return a graph conflict: %+v", refused)
	}
	turns, err := h.app.Store.ListTurns(thread)
	if err != nil || len(turns) != 1 || turns[0].GraphReplay {
		t.Fatalf("stale confirmation started a replay: %+v, err=%v", turns, err)
	}
	view := h.json(http.MethodGet, path+"/graph", nil, http.StatusOK)
	if view["owner_id"] != project.ID || view["revision"] != float64(1) || len(view["todos"].([]any)) != 1 {
		t.Fatalf("stale confirmation changed the project graph: %+v", view)
	}
}
