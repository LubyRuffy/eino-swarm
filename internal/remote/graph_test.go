package remote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func graphWrite(t *testing.T, st *store.Store, thread, project string, batch graph.Batch) {
	t.Helper()
	turn := &store.Turn{ThreadID: thread, ProjectID: project, GraphTools: true}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ApplyGraph(context.Background(), store.GraphCaller{ThreadID: thread, TurnID: turn.ID, AgentID: "manager", ProjectID: project}, batch); err != nil {
		t.Fatal(err)
	}
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
	graphWrite(t, e.Store(), th.ID, "", graph.Batch{Subjects: []graph.SubjectInput{
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
	same := call(Request{ID: "2", ThreadID: th.ID, Since: first.Graph.Revision})
	if !same.OK || !same.Graph.Unchanged || len(same.Graph.Subjects) != 0 {
		t.Fatalf("unchanged poll=%+v", same)
	}
	graphWrite(t, e.Store(), th.ID, "", graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.20"}}}})
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
	for i := 0; i < 40; i++ {
		var subjects []graph.SubjectInput
		for j := 0; j < 50; j++ {
			subjects = append(subjects, graph.SubjectInput{Kind: "other", Attrs: map[string]any{"name": strings.Repeat("x", 200) + string(rune('a'+i)) + string(rune('a'+j%26)) + string(rune('A'+j/26))}})
		}
		graphWrite(t, e.Store(), th.ID, "", graph.Batch{Subjects: subjects})
	}
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
	again := Handle(e, config.RemoteConfig{}, Request{Op: OpGraph, ID: "p2", ProjectID: p.ID, Since: got.Graph.Revision}, "relay", "session")
	if !again.OK || !again.Graph.Unchanged {
		t.Fatalf("unchanged project poll=%+v", again)
	}
}
