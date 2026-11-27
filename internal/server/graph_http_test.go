package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const graphSecretFixture = "fake-credential-for-http-test"

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
	h.writeGraph(thread, "", graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.11"}}}})
	if rev := revision(); rev != 1 {
		t.Fatalf("after one commit: %d", rev)
	}
	h.writeGraph(thread, "", graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.12"}}}})
	if rev := revision(); rev != 2 {
		t.Fatalf("after a second commit: %d", rev)
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
