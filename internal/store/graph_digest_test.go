package store

import (
	"context"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

func (f *graphFixture) toolResult(graphPayload bool) {
	f.t.Helper()
	ev := &Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: "tool_result", AgentID: "manager", WorkRecordPayload: graphPayload, Text: "r"}
	if err := f.s.AppendEvent(ev); err != nil {
		f.t.Fatal(err)
	}
}

// The unrecorded count is what lets the model notice it has run tools without
// telling the graph. It must reset at a graph write and ignore graph traffic.
func TestUnrecordedCountsToolResultsSinceTheLastGraphWrite(t *testing.T) {
	f := newGraphFixture(t, false)
	count := func() int {
		d, err := f.s.ReadGraphDigest(context.Background(), f.owner, f.thread.ID, f.turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		return d.Unrecorded
	}
	f.toolResult(false)
	f.toolResult(false)
	if got := count(); got != 2 {
		t.Fatalf("two results before any write: %d", got)
	}
	f.toolResult(true)
	if got := count(); got != 0 {
		t.Fatalf("a graph write must settle what came before it: %d", got)
	}
	f.toolResult(false)
	f.toolResult(true) // a read, still graph traffic: not itself unrecorded
	f.toolResult(false)
	if got := count(); got != 1 {
		t.Fatalf("after the last graph traffic: %d", got)
	}
	if n, err := f.s.GraphUnrecorded(context.Background(), f.thread.ID, f.turn.ID); err != nil || n != 1 {
		t.Fatalf("GraphUnrecorded=%d %v", n, err)
	}
}

func TestDigestIsABoundedWindowOnTheGraph(t *testing.T) {
	f := newGraphFixture(t, true)
	var subjects []graph.SubjectInput
	var todos []graph.TodoInput
	for i := 0; i < DigestSubjects+5; i++ {
		subjects = append(subjects, ip("203.0.113."+itoa(i+1)))
		todos = append(todos, graph.TodoInput{Title: "step " + itoa(i)})
	}
	f.apply(graph.Batch{Subjects: subjects, Todos: todos})
	d, err := f.s.ReadGraphDigest(context.Background(), f.owner, f.thread.ID, f.turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subjects != len(subjects) || len(d.Recent) != DigestSubjects || d.OpenTodoTotal != len(todos) || len(d.Todos) != DigestTodos {
		t.Fatalf("digest sizes: subjects=%d recent=%d todos=%d/%d", d.Subjects, len(d.Recent), len(d.Todos), d.OpenTodoTotal)
	}
	if d.ByKind["ip"] != len(subjects) || d.CurrentByKind["ip"] != len(subjects) || d.Empty() {
		t.Fatalf("digest=%+v", d)
	}
	owner, err := f.s.ThreadGraphOwner(context.Background(), f.thread.ID)
	if err != nil || owner != f.owner {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
	if _, err := f.s.ThreadGraphOwner(context.Background(), "th_missing"); err == nil {
		t.Fatal("a missing conversation has no graph owner")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// The panel draws subjects, links and to-dos from one snapshot, so a revision
// cannot describe subjects that its to-dos have already moved past.
func TestReadAllReturnsSubjectsLinksAndTodosTogether(t *testing.T) {
	f := newGraphFixture(t, false)
	f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "a", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.1"}}, {Ref: "b", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.2"}}},
		Links:    []graph.LinkInput{{From: "a", To: "b", Rel: "same_network"}},
		Todos:    []graph.TodoInput{{Title: "look"}},
	})
	view := f.read(GraphQuery{All: true, Limit: -1})
	if len(view.Subjects) != 2 || len(view.Links) != 1 || len(view.Todos) != 1 || view.Revision != 1 || view.Total != 2 {
		t.Fatalf("view=%+v", view)
	}
}
