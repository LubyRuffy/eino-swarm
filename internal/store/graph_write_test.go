package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

type graphFixture struct {
	t       *testing.T
	s       *Store
	project string
	thread  *Thread
	turn    *Turn
	owner   string
}

func newGraphFixture(t *testing.T, inProject bool) *graphFixture {
	t.Helper()
	s := open(t)
	f := &graphFixture{t: t, s: s}
	if inProject {
		p := &Project{Name: "graph"}
		if err := s.CreateProject(p); err != nil {
			t.Fatal(err)
		}
		f.project = p.ID
	}
	f.thread, f.turn = f.newTurn()
	f.owner = GraphOwnerID(f.project, f.thread.ID)
	return f
}

func (f *graphFixture) newTurn() (*Thread, *Turn) {
	f.t.Helper()
	th := &Thread{Title: "graph", ProjectID: f.project}
	if err := f.s.CreateThread(th); err != nil {
		f.t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, ProjectID: f.project, GraphTools: true}
	if err := f.s.CreateTurn(turn); err != nil {
		f.t.Fatal(err)
	}
	return th, turn
}

func (f *graphFixture) caller(th *Thread, turn *Turn, agent string) GraphCaller {
	return GraphCaller{ThreadID: th.ID, TurnID: turn.ID, AgentID: agent, ProjectID: f.project}
}

// event stores one durable event the graph can cite.
func (f *graphFixture) event(th *Thread, turn *Turn, text string) graph.Evidence {
	f.t.Helper()
	ev := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "tool_result", AgentID: "manager", Text: text}
	if err := f.s.AppendEvent(ev); err != nil {
		f.t.Fatal(err)
	}
	return graph.Evidence{ThreadID: th.ID, Seq: ev.Seq}
}

func (f *graphFixture) apply(batch graph.Batch) graph.Result {
	f.t.Helper()
	res, _, err := f.s.ApplyGraph(context.Background(), f.caller(f.thread, f.turn, "manager"), batch)
	if err != nil {
		f.t.Fatalf("ApplyGraph: %v", err)
	}
	return res
}

func (f *graphFixture) applyErr(batch graph.Batch) error {
	f.t.Helper()
	_, _, err := f.s.ApplyGraph(context.Background(), f.caller(f.thread, f.turn, "manager"), batch)
	if err == nil {
		f.t.Fatal("write was accepted")
	}
	return err
}

func (f *graphFixture) read(q GraphQuery) GraphView {
	f.t.Helper()
	v, err := f.s.ReadGraph(context.Background(), f.owner, q)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func ip(addr string) graph.SubjectInput {
	return graph.SubjectInput{Kind: "ip", Attrs: map[string]any{"ip": addr}}
}

func TestSubjectsMergeOnAnyIdentityKeyAndKeepTheFirstOrigin(t *testing.T) {
	f := newGraphFixture(t, true)
	first := f.apply(graph.Batch{Subjects: []graph.SubjectInput{
		{Kind: "app", Attrs: map[string]any{"name": "Nginx"}},
	}})
	// A different spelling of the name and a new rule id reach the same subject.
	second := f.apply(graph.Batch{Subjects: []graph.SubjectInput{
		{Kind: "app", Attrs: map[string]any{"name": "  nginx ", "vendor": "F5"}},
	}})
	if second.Subjects[0].ID != first.Subjects[0].ID || second.Subjects[0].Created || second.Subjects[0].Version != 2 {
		t.Fatalf("second write did not merge: %+v", second.Subjects[0])
	}
	// Adding the rule id adds an identity key; a later write by rule id alone finds it.
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{{ID: first.Subjects[0].ID, Attrs: map[string]any{"rule_id": "r-9"}}}})
	third := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "app", Attrs: map[string]any{"rule_id": "R-9"}}}})
	if third.Subjects[0].ID != first.Subjects[0].ID {
		t.Fatalf("rule id did not reach the same subject: %+v", third.Subjects[0])
	}
	view := f.read(GraphQuery{})
	if len(view.Subjects) != 1 || view.Subjects[0].Attrs["vendor"] != "F5" || view.Subjects[0].ThreadID != f.thread.ID {
		t.Fatalf("view=%+v", view.Subjects)
	}
}

// Two workers finding the same subject at once must both succeed and keep each
// other's attributes; the identity index is what makes the second one a merge.
func TestConcurrentWorkersMergeAdditivelyWithoutConflict(t *testing.T) {
	f := newGraphFixture(t, true)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := f.s.ApplyGraph(context.Background(), f.caller(f.thread, f.turn, fmt.Sprintf("w%d", i)), graph.Batch{Subjects: []graph.SubjectInput{
				{Kind: "person", Attrs: map[string]any{"name": "Lee", "email": []string{"lee@x.test"}, "role": fmt.Sprintf("r%d", i)}},
			}})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent worker was refused: %v", err)
		}
	}
	view := f.read(GraphQuery{})
	if len(view.Subjects) != 1 {
		t.Fatalf("one person became %d subjects", len(view.Subjects))
	}
	if view.Subjects[0].Attrs["role"] == nil || view.Subjects[0].Attrs["email"] == nil {
		t.Fatalf("attributes lost: %+v", view.Subjects[0].Attrs)
	}
}

func TestOverwriteAndStatusNeedTheCurrentVersion(t *testing.T) {
	f := newGraphFixture(t, true)
	ev := f.event(f.thread, f.turn, "observation")
	created := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "company", Attrs: map[string]any{"name": "Acme", "country": "A"}}}})
	id := created.Subjects[0].ID
	// An additive write keeps the existing value and says so. It also fills a
	// blank attribute, which moves the version on.
	res := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "company", Attrs: map[string]any{"name": "Acme", "country": "B", "note": "n"}}}})
	if got := res.Subjects[0].Ignored; len(got) != 1 || got[0] != "country" {
		t.Fatalf("ignored=%v", got)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{ID: id, Status: graph.StatusRetracted}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("status change without version: %v", err)
	}
	if res.Subjects[0].Version != 2 {
		t.Fatalf("filling a blank attribute did not advance the version: %+v", res.Subjects[0])
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{ID: id, Version: 1, Attrs: map[string]any{"country": "C"}}}}); !errors.Is(err, graph.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}
	ok := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{ID: id, Version: res.Subjects[0].Version, Attrs: map[string]any{"country": "C"}, Status: graph.StatusConfirmed}}, Evidence: []graph.Evidence{ev}})
	if ok.Subjects[0].Version != res.Subjects[0].Version+1 {
		t.Fatalf("version did not advance: %+v", ok.Subjects[0])
	}
	got := f.read(GraphQuery{ID: id}).Subjects[0]
	if got.Attrs["country"] != "C" || got.Status != graph.StatusConfirmed {
		t.Fatalf("overwrite lost: %+v", got)
	}
}

func TestConfirmedNeedsEvidenceAndEvidenceMustBeARealEventOfTheScope(t *testing.T) {
	f := newGraphFixture(t, true)
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "1.1.1.1"}, Status: graph.StatusConfirmed}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("confirmed without evidence: %v", err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Evidence: []graph.Evidence{{ThreadID: f.thread.ID, Seq: 999}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("invented seq: %v", err)
	}
	// An event of a conversation outside the project is outside the scope.
	other := &Thread{Title: "elsewhere"}
	if err := f.s.CreateThread(other); err != nil {
		t.Fatal(err)
	}
	otherTurn := &Turn{ThreadID: other.ID}
	if err := f.s.CreateTurn(otherTurn); err != nil {
		t.Fatal(err)
	}
	foreign := f.event(other, otherTurn, "outside")
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Evidence: []graph.Evidence{foreign}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("foreign evidence: %v", err)
	}
	// A peer conversation of the same project is inside it.
	peer, peerTurn := f.newTurn()
	good := f.event(peer, peerTurn, "peer observation")
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "1.1.1.1"}, Status: graph.StatusConfirmed}}, Evidence: []graph.Evidence{good}})
	web := f.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.2")}, Evidence: []graph.Evidence{{URL: "https://news.test/a", Label: "article"}}})
	if len(web.Subjects) != 1 {
		t.Fatal("an external address is evidence")
	}
}

func TestEvidenceCannotCiteGraphToolTraffic(t *testing.T) {
	f := newGraphFixture(t, false)
	ev := &Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: "tool_result", AgentID: "manager", Text: "ok", WorkRecordPayload: true}
	if err := f.s.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Evidence: []graph.Evidence{{ThreadID: f.thread.ID, Seq: ev.Seq}}})
	if !errors.Is(err, graph.ErrInvalid) || !strings.Contains(err.Error(), "graph tool traffic") {
		t.Fatalf("err=%v", err)
	}
}

func TestOwnToolCallIDsResolveToTheCallersOwnResults(t *testing.T) {
	f := newGraphFixture(t, false)
	add := func(kind, call, agent string) int64 {
		ev := &Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: kind, AgentID: agent, ToolCallID: call, Text: kind}
		if err := f.s.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
		return ev.Seq
	}
	add("tool_call", "c1", "manager")
	result := add("tool_result", "c1", "manager")
	add("tool_call", "c2", "other-agent")
	add("tool_result", "c2", "other-agent")
	caller := f.caller(f.thread, f.turn, "manager")
	caller.CurrentToolCallID = "write-1"
	res, _, err := f.s.ApplyGraph(context.Background(), caller, graph.Batch{
		Subjects:       []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "2.2.2.2"}, Status: graph.StatusConfirmed}},
		OwnToolCallIDs: []string{"c1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.read(GraphQuery{ID: res.Subjects[0].ID}).Subjects[0]
	if len(got.Evidence) != 1 || got.Evidence[0].Seq != result {
		t.Fatalf("evidence=%+v want seq %d", got.Evidence, result)
	}
	for name, ids := range map[string][]string{"someone else's call": {"c2"}, "the write itself": {"write-1"}, "unknown": {"nope"}, "twice": {"c1", "c1"}} {
		_, _, err := f.s.ApplyGraph(context.Background(), caller, graph.Batch{Subjects: []graph.SubjectInput{ip("2.2.2.3")}, OwnToolCallIDs: ids})
		if !errors.Is(err, graph.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWriteMatchingTwoSubjectsIsRefusedWithBothIDs(t *testing.T) {
	f := newGraphFixture(t, true)
	a := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "app", Attrs: map[string]any{"name": "Alpha", "rule_id": "r1"}}}}).Subjects[0].ID
	loose := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "app", Attrs: map[string]any{"name": "Gamma"}}}}).Subjects[0].ID
	b := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "app", Attrs: map[string]any{"name": "Beta", "rule_id": "r2"}}}}).Subjects[0].ID
	err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "app", Attrs: map[string]any{"name": "Alpha", "rule_id": "r2"}}}})
	var amb *graph.AmbiguousError
	if !errors.As(err, &amb) || len(amb.IDs) != 2 || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Fatalf("err=%v", err)
	}
	if got := f.read(GraphQuery{}); len(got.Subjects) != 3 {
		t.Fatalf("a refusal changed the graph: %d subjects", len(got.Subjects))
	}
	// Giving one of them a key the other holds is the same collision.
	err = f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{ID: loose, Attrs: map[string]any{"rule_id": "r2"}}}})
	if !errors.As(err, &amb) {
		t.Fatalf("attaching another subject's key: %v", err)
	}
}

func TestIdentityKeyCannotChangeButCanBeAdded(t *testing.T) {
	f := newGraphFixture(t, true)
	company := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "company", Attrs: map[string]any{"name": "Acme"}}}}).Subjects[0]
	got := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{ID: company.ID, Attrs: map[string]any{"reg_no": "91-1"}}}})
	if got.Subjects[0].Version != company.Version+1 {
		t.Fatalf("adding a registration number was refused: %+v", got.Subjects[0])
	}
	// The registration number now identifies it, by itself.
	again := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "company", Attrs: map[string]any{"reg_no": " 91-1 ", "country": "A"}}}})
	if again.Subjects[0].ID != company.ID {
		t.Fatalf("the new key does not identify it: %+v", again.Subjects[0])
	}
	domain := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "domain", Attrs: map[string]any{"domain": "a.test"}}}}).Subjects[0]
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{ID: domain.ID, Version: domain.Version, Attrs: map[string]any{"domain": "b.test"}}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("a domain became another domain: %v", err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{ID: domain.ID, Kind: "ip"}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("kind change: %v", err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "colour", Attrs: map[string]any{"name": "x"}}}}); !errors.Is(err, graph.ErrInvalid) || !strings.Contains(err.Error(), "ip") {
		t.Errorf("unregistered kind must list the registered ones: %v", err)
	}
}

func TestRepositoryCredentialIsStoredButMovedOutOfTheAddress(t *testing.T) {
	f := newGraphFixture(t, true)
	res := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{"url": "https://alice:fake-token@git.test/acme/tool.git"}}}})
	got := f.read(GraphQuery{ID: res.Subjects[0].ID}).Subjects[0]
	if got.Attrs["url"] != "https://git.test/acme/tool.git" || got.Attrs["credential"] != "alice:fake-token" || got.Name != "acme/tool" {
		t.Fatalf("attrs=%v name=%q", got.Attrs, got.Name)
	}
	redacted, secrets := graph.MustDefault().Redact(got.Kind, got.Attrs)
	if _, present := redacted["credential"]; present || len(secrets) != 1 {
		t.Fatalf("redaction kept the credential: %v %v", redacted, secrets)
	}
	// The same repository over ssh is the same subject.
	ssh := f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{"url": "git@git.test:acme/tool"}}}})
	if ssh.Subjects[0].ID != res.Subjects[0].ID {
		t.Errorf("ssh and https spellings are different subjects")
	}
}

func TestAssetKeyedByIPPortProtocolAndHost(t *testing.T) {
	f := newGraphFixture(t, true)
	mk := func(ipAddr string, port int, host string) string {
		return f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "asset", Attrs: map[string]any{"ip": ipAddr, "port": port, "protocol": "https", "host": host}}}}).Subjects[0].ID
	}
	a := mk("9.9.9.9", 443, "A.test")
	if b := mk("9.9.9.9", 443, "a.test:443"); a != b {
		t.Error("explicit default port is another asset")
	}
	if c := mk("9.9.9.8", 443, "a.test"); a == c {
		t.Error("the same host on another IP merged")
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "asset", Attrs: map[string]any{"host": "a.test"}}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("an asset without ip and port: %v", err)
	}
}

func TestBatchIsAtomicAndRefusalChangesNothing(t *testing.T) {
	f := newGraphFixture(t, true)
	err := f.applyErr(graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "a", Kind: "ip", Attrs: map[string]any{"ip": "1.1.1.1"}}},
		Todos:    []graph.TodoInput{{Title: "x"}, {Title: "settled", Status: graph.TodoDone}},
	})
	if !errors.Is(err, graph.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
	if v := f.read(GraphQuery{}); len(v.Subjects) != 0 || v.Revision != 0 {
		t.Fatalf("a refused batch left %d subjects at revision %d", len(v.Subjects), v.Revision)
	}
	if v := f.read(GraphQuery{Todos: true}); len(v.Todos) != 0 {
		t.Fatalf("a refused batch left %d to-dos", len(v.Todos))
	}
}

func TestLinksResolveRefsMergeAttributesAndAllowCycles(t *testing.T) {
	f := newGraphFixture(t, true)
	res := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{
			{Ref: "co", Kind: "company", Attrs: map[string]any{"name": "Acme"}},
			{Ref: "d", Kind: "domain", Attrs: map[string]any{"domain": "acme.test"}},
		},
		Links: []graph.LinkInput{
			{From: "co", To: "d", Rel: "owns", Attrs: map[string]any{"note": "registrant"}},
			{From: "d", To: "co", Rel: "belongs to"},
		},
	})
	if len(res.Links) != 2 || !res.Links[0].Created {
		t.Fatalf("links=%+v", res.Links)
	}
	again := f.apply(graph.Batch{Links: []graph.LinkInput{
		{From: res.Subjects[0].ID, To: res.Subjects[1].ID, Rel: "owns", Attrs: map[string]any{"stake": "100%"}},
	}})
	if again.Links[0].Created || again.Links[0].ID != res.Links[0].ID {
		t.Fatalf("repeat link duplicated: %+v", again.Links[0])
	}
	view := f.read(GraphQuery{})
	if len(view.Links) != 2 {
		t.Fatalf("links=%d", len(view.Links))
	}
	for _, l := range view.Links {
		if l.Rel == "owns" && (l.Attrs["note"] != "registrant" || l.Attrs["stake"] != "100%") {
			t.Errorf("link attributes were not merged: %v", l.Attrs)
		}
	}
	for name, l := range map[string]graph.LinkInput{
		"unknown end": {From: "co", To: "nope", Rel: "x"},
		"self":        {From: res.Subjects[0].ID, To: res.Subjects[0].ID, Rel: "x"},
		"no rel":      {From: res.Subjects[0].ID, To: res.Subjects[1].ID},
		"big attrs":   {From: res.Subjects[0].ID, To: res.Subjects[1].ID, Rel: "y", Attrs: map[string]any{"a": strings.Repeat("x", graph.MaxLinkAttrSize+1)}},
	} {
		if err := f.applyErr(graph.Batch{Links: []graph.LinkInput{l}}); !errors.Is(err, graph.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTodoOrderingSettlementAndRules(t *testing.T) {
	f := newGraphFixture(t, true)
	ev := f.event(f.thread, f.turn, "result")
	res := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "co", Kind: "company", Attrs: map[string]any{"name": "Acme"}}},
		Todos: []graph.TodoInput{
			{Ref: "t1", Title: "first", About: "co"}, {Ref: "t2", Title: "second"}, {Ref: "t3", Title: "third", Before: "t1"},
		},
	})
	order := func() []string {
		var titles []string
		for _, td := range f.read(GraphQuery{Todos: true}).Todos {
			titles = append(titles, td.Title)
		}
		return titles
	}
	if got := strings.Join(order(), ","); got != "third,first,second" {
		t.Fatalf("order=%s", got)
	}
	id := func(title string) (string, int64) {
		for _, td := range f.read(GraphQuery{Todos: true}).Todos {
			if td.Title == title {
				return td.ID, td.Version
			}
		}
		t.Fatalf("no to-do %q", title)
		return "", 0
	}
	// Moving needs no version; changing does.
	second, secondVersion := id("second")
	first, _ := id("first")
	f.apply(graph.Batch{Todos: []graph.TodoInput{{ID: second, Before: first}}})
	if got := strings.Join(order(), ","); got != "third,second,first" {
		t.Fatalf("after move: %s", got)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: second, Status: graph.TodoDoing}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("changing without version: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: second, Version: secondVersion + 5, Status: graph.TodoDoing}}}); !errors.Is(err, graph.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}
	f.apply(graph.Batch{Todos: []graph.TodoInput{{ID: second, Version: secondVersion, Status: graph.TodoDoing}}})
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: first, Version: 1, Status: graph.TodoDone}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("done without evidence: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: first, Version: 1, Status: graph.TodoFailed}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("failed without a result: %v", err)
	}
	// Settling stamps what the same batch found.
	settled := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Kind: "domain", Attrs: map[string]any{"domain": "acme.test"}}},
		Todos:    []graph.TodoInput{{ID: first, Version: 1, Status: graph.TodoDone, Result: "found the domain"}},
		Evidence: []graph.Evidence{ev},
	})
	found := f.read(GraphQuery{ID: settled.Subjects[0].ID}).Subjects[0]
	if found.FoundByTodo != first {
		t.Fatalf("found_by_todo=%q want %q", found.FoundByTodo, first)
	}
	todo := f.read(GraphQuery{Todos: true, Status: graph.TodoDone}).Todos
	if len(todo) != 1 || todo[0].SettledAt == nil || len(todo[0].Evidence) != 1 || todo[0].About != res.Subjects[0].ID {
		t.Fatalf("settled to-do=%+v", todo)
	}
	// Two settlements in one batch make the attribution a guess: none is made.
	third, thirdVersion := id("third")
	second, secondVersion = id("second")
	two := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Kind: "domain", Attrs: map[string]any{"domain": "two.test"}}},
		Todos: []graph.TodoInput{
			{ID: third, Version: thirdVersion, Status: graph.TodoDone}, {ID: second, Version: secondVersion, Status: graph.TodoFailed, Result: "tried"},
		},
		Evidence: []graph.Evidence{ev},
	})
	if f.read(GraphQuery{ID: two.Subjects[0].ID}).Subjects[0].FoundByTodo != "" {
		t.Error("an ambiguous settlement was attributed")
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{Title: "x", Before: "nope"}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown before: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{Title: "x", About: "nope"}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown about: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{Status: graph.TodoOpen}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("untitled to-do: %v", err)
	}
}

// Two workers reordering at the same moment must leave one consistent order
// with every item still present.
func TestConcurrentReorderKeepsEveryTodoAndOneOrder(t *testing.T) {
	f := newGraphFixture(t, true)
	var titles []graph.TodoInput
	for i := 0; i < 6; i++ {
		titles = append(titles, graph.TodoInput{Title: fmt.Sprintf("t%d", i)})
	}
	f.apply(graph.Batch{Todos: titles})
	todos := f.read(GraphQuery{Todos: true}).Todos
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			target := todos[(i+3)%6].ID
			_, _, err := f.s.ApplyGraph(context.Background(), f.caller(f.thread, f.turn, fmt.Sprintf("w%d", i)),
				graph.Batch{Todos: []graph.TodoInput{{ID: todos[i].ID, Before: target}}})
			if err != nil {
				t.Errorf("reorder %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	after := f.read(GraphQuery{Todos: true}).Todos
	seen := map[string]bool{}
	for i, td := range after {
		if td.Position != i || seen[td.ID] {
			t.Fatalf("positions are not dense and unique: %+v", after)
		}
		seen[td.ID] = true
	}
	if len(after) != 6 {
		t.Fatalf("lost items: %d", len(after))
	}
}

func TestReplayedToolCallReturnsTheSameAnswerOnlyForTheSameBody(t *testing.T) {
	f := newGraphFixture(t, true)
	caller := f.caller(f.thread, f.turn, "manager")
	batch := graph.Batch{Subjects: []graph.SubjectInput{ip("5.5.5.5")}, Todos: []graph.TodoInput{{Title: "follow up"}}}
	hash, _ := HashGraphBatch(batch)
	caller.ReceiptKey, caller.ReceiptHash = "turn:call-1", hash
	first, replayed, err := f.s.ApplyGraph(context.Background(), caller, batch)
	if err != nil || replayed {
		t.Fatalf("first: %v replayed=%v", err, replayed)
	}
	second, replayed, err := f.s.ApplyGraph(context.Background(), caller, batch)
	if err != nil || !replayed || second.Subjects[0].ID != first.Subjects[0].ID || second.Revision != first.Revision {
		t.Fatalf("replay: %+v %v replayed=%v", second, err, replayed)
	}
	if v := f.read(GraphQuery{}); len(v.Subjects) != 1 || v.Revision != 1 {
		t.Fatalf("a replay wrote again: %d subjects at revision %d", len(v.Subjects), v.Revision)
	}
	changed := graph.Batch{Subjects: []graph.SubjectInput{ip("5.5.5.6")}}
	changed2, _ := HashGraphBatch(changed)
	caller.ReceiptHash = changed2
	if _, _, err := f.s.ApplyGraph(context.Background(), caller, changed); !errors.Is(err, graph.ErrConflict) {
		t.Fatalf("same id, other body: %v", err)
	}
	caller.ReceiptKey, caller.ReceiptHash = "turn:call-2", ""
	if _, _, err := f.s.ApplyGraph(context.Background(), caller, changed); !errors.Is(err, graph.ErrInvalid) {
		t.Fatalf("half a receipt: %v", err)
	}
}

func TestRevisionCountsBatchesPerOwner(t *testing.T) {
	f := newGraphFixture(t, true)
	other := newGraphFixture(t, false)
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}})
	f.apply(graph.Batch{Todos: []graph.TodoInput{{Title: "x"}}})
	other.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}})
	rev, err := f.s.GraphRevisionOf(context.Background(), f.owner)
	if err != nil || rev != 2 {
		t.Fatalf("rev=%d err=%v", rev, err)
	}
	if v := other.read(GraphQuery{}); v.Revision != 1 || len(v.Subjects) != 1 {
		t.Fatalf("owners are not isolated: %+v", v)
	}
}

func TestPlanningCancelledTurnsAndMovedConversationsCannotWrite(t *testing.T) {
	f := newGraphFixture(t, true)
	if err := f.s.DB().Model(&Thread{}).Where("id = ?", f.thread.ID).Update("plan_mode", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}}); !strings.Contains(err.Error(), "planning") {
		t.Errorf("plan mode: %v", err)
	}
	if err := f.s.DB().Model(&Thread{}).Where("id = ?", f.thread.ID).Update("plan_mode", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Model(&Turn{}).Where("id = ?", f.turn.ID).Update("work_ledger_read_only", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}}); !strings.Contains(err.Error(), "planning") {
		t.Errorf("read-only turn: %v", err)
	}
	f.s.DB().Model(&Turn{}).Where("id = ?", f.turn.ID).Updates(map[string]any{"work_ledger_read_only": false, "status": TurnCancelled})
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("cancelled turn: %v", err)
	}
	f.s.DB().Model(&Turn{}).Where("id = ?", f.turn.ID).Update("status", TurnRunning)
	if err := f.s.DB().Model(&Thread{}).Where("id = ?", f.thread.ID).Update("project_id", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}}); !strings.Contains(err.Error(), "moved") {
		t.Errorf("moved conversation: %v", err)
	}
}

// Once the conversation is deleted, a late write from a stale worker must not
// bring rows back, and deleting the owner removes everything it held.
func TestDeletedOwnerKeepsNoRowsAndRefusesLateWrites(t *testing.T) {
	f := newGraphFixture(t, false)
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Todos: []graph.TodoInput{{Title: "x"}}})
	if err := f.s.DeleteThread(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []any{&GraphSubject{}, &GraphSubjectKey{}, &GraphLink{}, &GraphTodo{}, &GraphRevision{}} {
		var n int64
		if err := f.s.DB().Model(m).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%T left %d rows (%v)", m, n, err)
		}
	}
	if err := f.applyErr(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.2")}}); !errors.Is(err, graph.ErrInvalid) {
		t.Fatalf("late write: %v", err)
	}
}

func TestDeletingAProjectRemovesItsGraph(t *testing.T) {
	f := newGraphFixture(t, true)
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Todos: []graph.TodoInput{{Title: "x"}}})
	if err := f.s.DeleteProject(f.project); err != nil {
		t.Fatal(err)
	}
	var n int64
	for _, m := range []any{&GraphSubject{}, &GraphSubjectKey{}, &GraphTodo{}, &GraphRevision{}} {
		if err := f.s.DB().Model(m).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%T left %d rows (%v)", m, n, err)
		}
	}
}

// After a rewind the cited event is gone. The finding stays, but its evidence
// reads as unavailable instead of being trusted.
func TestRewoundEvidenceReadsAsUnavailable(t *testing.T) {
	f := newGraphFixture(t, false)
	keep := f.event(f.thread, f.turn, "kept")
	later := &Turn{ThreadID: f.thread.ID}
	if err := f.s.CreateTurn(later); err != nil {
		t.Fatal(err)
	}
	drop := f.event(f.thread, later, "dropped")
	res := f.apply(graph.Batch{Subjects: []graph.SubjectInput{ip("1.1.1.1")}, Evidence: []graph.Evidence{keep, drop, {URL: "https://x.test/p"}}})
	if err := f.s.TruncateFromEventSeq(f.thread.ID, drop.Seq); err != nil {
		t.Fatal(err)
	}
	got := f.read(GraphQuery{ID: res.Subjects[0].ID}).Subjects[0].Evidence
	state := map[int64]bool{}
	for _, e := range got {
		if e.URL == "" {
			state[e.Seq] = e.Unavailable
		} else if e.Unavailable {
			t.Error("an external address cannot be rewound")
		}
	}
	if state[keep.Seq] || !state[drop.Seq] {
		t.Fatalf("availability=%v", state)
	}
}

func TestReadPagingSearchAndSingleSubject(t *testing.T) {
	f := newGraphFixture(t, true)
	var subjects []graph.SubjectInput
	for i := 0; i < 5; i++ {
		subjects = append(subjects, graph.SubjectInput{Kind: "domain", Attrs: map[string]any{"domain": fmt.Sprintf("host%d.test", i)}})
	}
	subjects = append(subjects, graph.SubjectInput{Kind: "company", Attrs: map[string]any{"name": "Zed_100% Corp", "reg_no": "ab-77"}})
	res := f.apply(graph.Batch{Subjects: subjects})
	page := f.read(GraphQuery{Limit: 2})
	if len(page.Subjects) != 2 || page.Total != 6 || !page.More {
		t.Fatalf("page=%d total=%d more=%v", len(page.Subjects), page.Total, page.More)
	}
	if got := f.read(GraphQuery{Text: "host3"}); len(got.Subjects) != 1 || got.Subjects[0].Name != "host3.test" {
		t.Fatalf("name search: %+v", got.Subjects)
	}
	if got := f.read(GraphQuery{Text: "AB-77"}); len(got.Subjects) != 1 || got.Subjects[0].Kind != "company" {
		t.Fatalf("identity key search: %+v", got.Subjects)
	}
	if got := f.read(GraphQuery{Text: "_100%"}); len(got.Subjects) != 1 {
		t.Fatalf("wildcards must be literal: %+v", got.Subjects)
	}
	if got := f.read(GraphQuery{Text: "%"}); len(got.Subjects) != 1 {
		t.Fatalf("a bare percent must be literal: %d", len(got.Subjects))
	}
	if _, err := f.s.ReadGraph(context.Background(), f.owner, GraphQuery{ID: "sj_missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing subject: %v", err)
	}
	if _, err := f.s.ReadGraph(context.Background(), "other-owner", GraphQuery{ID: res.Subjects[0].ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("another owner's subject: %v", err)
	}
}

func TestConfirmedClaimCitesTheCallersOwnToolResult(t *testing.T) {
	f := newGraphFixture(t, false)
	pair := func(id string, payload bool) int64 {
		t.Helper()
		call := &Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: "tool_call", AgentID: "manager", ToolCallID: id, Text: id}
		if err := f.s.AppendEvent(call); err != nil {
			t.Fatal(err)
		}
		result := &Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: "tool_result", AgentID: "manager", ToolCallID: id, Text: "result " + id, WorkRecordPayload: payload}
		if err := f.s.AppendEvent(result); err != nil {
			t.Fatal(err)
		}
		return result.Seq
	}
	graphSeq := pair("graph-write", true)
	readSeq := pair("read-file", false)
	caller := f.caller(f.thread, f.turn, "manager")
	caller.CurrentToolCallID = "this-graph-call"
	res, _, err := f.s.ApplyGraph(context.Background(), caller, graph.Batch{
		CiteOwnResults: true,
		Subjects:       []graph.SubjectInput{{Kind: "ip", Status: graph.StatusConfirmed, Attrs: map[string]any{"ip": "203.0.113.10"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.read(GraphQuery{ID: res.Subjects[0].ID}).Subjects[0]
	if len(got.Evidence) != 1 || got.Evidence[0].Seq != readSeq || got.Evidence[0].Seq == graphSeq {
		t.Fatalf("evidence = %+v, want the file result %d", got.Evidence, readSeq)
	}
	bare := f.caller(f.thread, f.turn, "other")
	if _, _, err := f.s.ApplyGraph(context.Background(), bare, graph.Batch{
		CiteOwnResults: true,
		Subjects:       []graph.SubjectInput{{Kind: "ip", Status: graph.StatusConfirmed, Attrs: map[string]any{"ip": "203.0.113.11"}}},
	}); !errors.Is(err, graph.ErrInvalid) {
		t.Fatalf("a confirmed subject with no tool result of its own: %v", err)
	}
}

func TestSearchNeverMatchesCredentialValues(t *testing.T) {
	f := newGraphFixture(t, true)
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{"path": "acme/tool", "credential": "fake-secret-value"}}}})
	if got := f.read(GraphQuery{Text: "fake-secret"}); len(got.Subjects) != 0 {
		t.Fatalf("searching reached a credential: %+v", got.Subjects)
	}
}
