package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func graphWrite(t *testing.T, st *store.Store, thread, project string, batch graph.Batch) graph.Result {
	t.Helper()
	turn := &store.Turn{ThreadID: thread, ProjectID: project, GraphTools: true}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	res, _, err := st.ApplyGraph(context.Background(), store.GraphCaller{ThreadID: thread, TurnID: turn.ID, AgentID: "manager", ProjectID: project}, batch)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The phone polls with the revision it holds. An unchanged graph costs one
// small frame, a credential never leaves the host, and a graph that cannot
// fit a frame is refused instead of drawn half.
func TestPhoneGraphPollReturnsOnlyChangesAndNeverACredential(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("graph", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seeded := graphWrite(t, e.Store(), th.ID, "", graph.Batch{Subjects: []graph.SubjectInput{
		{Kind: "repo", Attrs: map[string]any{"path": "acme/app", "credential": "fake-phone-credential"}}}})
	call := func(req Request) Response {
		req.Op = OpGraph
		return Handle(e, config.RemoteConfig{}, req, "relay", "session")
	}
	first := call(Request{ID: "1", ThreadID: th.ID})
	if !first.OK || first.Graph == nil || first.Graph.Revision != 1 || len(first.Graph.Subjects) != 1 {
		t.Fatalf("first=%+v", first)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "fake-phone-credential") {
		t.Fatal("a credential reached the phone")
	}
	same := call(Request{ID: "2", ThreadID: th.ID, Since: first.Graph.Revision, GraphOwner: first.Graph.Owner})
	if !same.OK || !same.Graph.Unchanged || len(same.Graph.Subjects) != 0 {
		t.Fatalf("unchanged poll=%+v", same)
	}
	graphWrite(t, e.Store(), th.ID, "", graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "addr", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.20"}}},
		Links:    []graph.LinkInput{{From: "addr", To: seeded.Subjects[0].ID, Rel: "served_from"}},
	})
	next := call(Request{ID: "3", ThreadID: th.ID, Since: first.Graph.Revision})
	if next.Graph.Unchanged || next.Graph.Revision != 2 || len(next.Graph.Subjects) != 2 {
		t.Fatalf("after a commit=%+v", next)
	}
	for name, req := range map[string]Request{
		"neither scope":  {ID: "4"},
		"both scopes":    {ID: "5", ThreadID: th.ID, ProjectID: "pr_x"},
		"negative since": {ID: "6", ThreadID: th.ID, Since: -1},
	} {
		if got := call(req); got.OK || got.Code != "bad_request" {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := call(Request{ID: "7", ThreadID: "th_missing"}); got.OK || got.Code != "not_found" {
		t.Errorf("missing conversation: %+v", got)
	}
	if got := call(Request{ID: "8", ProjectID: "pr_missing"}); got.OK || got.Code != "not_found" {
		t.Errorf("missing project: %+v", got)
	}
}

func TestPhoneGraphRefusesWhatDoesNotFitAFrame(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("big graph", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Each result is legal and the results alone exceed the frame budget.
	// This reaches the real encoder without depending on catalog heuristics
	// for lengthy subject names.
	todos := make([]graph.TodoInput, plaintextBudget()/graph.MaxResult+1)
	for i := range todos {
		todos[i] = graph.TodoInput{
			Title: fmt.Sprintf("Check %d", i+1), Status: graph.TodoFailed, Result: strings.Repeat("x", graph.MaxResult),
		}
	}
	graphWrite(t, e.Store(), th.ID, "", graph.Batch{Todos: todos})
	got := Handle(e, config.RemoteConfig{}, Request{Op: OpGraph, ID: "big", ThreadID: th.ID}, "relay", "session")
	if got.OK || got.Code != "graph_too_large" {
		t.Fatalf("an oversized graph was not refused: ok=%v code=%q", got.OK, got.Code)
	}
}

func TestPhoneProjectGraphReadsTheProjectOwner(t *testing.T) {
	e := testEngine(t)
	p := &store.Project{Name: "shared"}
	if err := e.Store().CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "member", ProjectID: p.ID}
	if err := e.Store().CreateThread(th); err != nil {
		t.Fatal(err)
	}
	graphWrite(t, e.Store(), th.ID, p.ID, graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.21"}}}})
	got := Handle(e, config.RemoteConfig{}, Request{Op: OpGraph, ID: "p", ProjectID: p.ID}, "relay", "session")
	if !got.OK || len(got.Graph.Subjects) != 1 {
		t.Fatalf("project graph=%+v", got)
	}
	again := Handle(e, config.RemoteConfig{}, Request{Op: OpGraph, ID: "p2", ProjectID: p.ID, Since: got.Graph.Revision, GraphOwner: got.Graph.Owner}, "relay", "session")
	if !again.OK || !again.Graph.Unchanged {
		t.Fatalf("unchanged project poll=%+v", again)
	}
}

// Two graphs can both be at revision 1. Moving a conversation onto the other
// one has to return that graph; the matching revision alone is not enough.
func TestPhoneGraphChangesWhenAConversationJoinsAProjectAtTheSameRevision(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("moving", "", "")
	if err != nil {
		t.Fatal(err)
	}
	graphWrite(t, e.Store(), th.ID, "", graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.30"}}}})
	project := &store.Project{Name: "destination"}
	if err := e.Store().CreateProject(project); err != nil {
		t.Fatal(err)
	}
	member := &store.Thread{Title: "member", ProjectID: project.ID}
	if err := e.Store().CreateThread(member); err != nil {
		t.Fatal(err)
	}
	graphWrite(t, e.Store(), member.ID, project.ID, graph.Batch{Subjects: []graph.SubjectInput{{Kind: "domain", Attrs: map[string]any{"domain": "moved.example.test"}}}})
	if err := e.Store().SetThreadProject(th.ID, project.ID); err != nil {
		t.Fatal(err)
	}
	got := Handle(e, config.RemoteConfig{}, Request{
		Op: OpGraph, ID: "move", ThreadID: th.ID, Since: 1, GraphOwner: th.ID,
	}, "relay", "session")
	if !got.OK || got.Graph == nil || got.Graph.Unchanged || got.Graph.Owner != project.ID || len(got.Graph.Subjects) != 1 || got.Graph.Subjects[0].Kind != "domain" {
		t.Fatalf("after the move=%+v", got.Graph)
	}
	held := Handle(e, config.RemoteConfig{}, Request{
		Op: OpGraph, ID: "held", ThreadID: th.ID, Since: got.Graph.Revision, GraphOwner: got.Graph.Owner,
	}, "relay", "session")
	if !held.OK || !held.Graph.Unchanged {
		t.Fatalf("the project graph the phone now holds=%+v", held.Graph)
	}
}
