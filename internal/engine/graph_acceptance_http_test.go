package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// graphTimeline samples the conversation's graph while a turn runs, the way the
// panel does. The acceptance below is about what a person could have watched,
// not only the final graph.
type graphTimeline struct {
	mu        sync.Mutex
	revisions []int64
	sawDoing  bool
	stop      chan struct{}
	done      chan struct{}
}

func watchGraph(t *testing.T, e *Engine, threadID string) *graphTimeline {
	tl := &graphTimeline{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(tl.done)
		for {
			select {
			case <-tl.stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			view, err := e.ReadThreadGraph(t.Context(), threadID, store.GraphQuery{Limit: -1, All: true, Preview: true})
			if err != nil {
				continue
			}
			tl.mu.Lock()
			if n := len(tl.revisions); n == 0 || tl.revisions[n-1] != view.Revision {
				tl.revisions = append(tl.revisions, view.Revision)
			}
			for _, todo := range view.Todos {
				tl.sawDoing = tl.sawDoing || todo.Status == graph.TodoDoing
			}
			tl.mu.Unlock()
		}
	}()
	return tl
}

func (tl *graphTimeline) finish() (revisions []int64, sawDoing bool) {
	close(tl.stop)
	<-tl.done
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return append([]int64(nil), tl.revisions...), tl.sawDoing
}

// graphNow is the graph as the scripted model would see it on its next turn
// of thought: it looks ids and versions up instead of remembering receipts.
type graphNow struct{ view store.GraphView }

func (g graphNow) todo(title string) (id, version string) {
	for _, todo := range g.view.Todos {
		if todo.Title == title {
			return todo.ID, itoa(todo.Version)
		}
	}
	return "", "0"
}

func (g graphNow) subject(kind string) (id, version string) {
	for _, s := range g.view.Subjects {
		if s.Kind == kind {
			return s.ID, itoa(s.Version)
		}
	}
	return "", "0"
}

// settle is one to-do update in tool-argument JSON.
func (g graphNow) settle(title, status string) string {
	id, version := g.todo(title)
	return `{"id":"` + id + `","version":` + version + `,"status":"` + status + `"}`
}

// runGraphScript plays one scripted manager: each entry is an update_graph call.
// A pause before each step stands for the time a real model spends working, so
// progress has somewhere to show.
func runGraphScript(t *testing.T, steps []func(graphNow) string) (*Engine, string, []int64, bool) {
	t.Helper()
	var eng *Engine
	var threadID string
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		callID := func(n int) string { return "graph-step-" + itoa(int64(n)) }
		if step > 1 && !strings.Contains(scriptedToolReceipt(request, callID(step-1)), `"ok":true`) {
			t.Errorf("step %d was refused: %s", step-1, scriptedToolReceipt(request, callID(step-1)))
		}
		if step > len(steps) {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Done."}, "stop")
			return
		}
		time.Sleep(120 * time.Millisecond)
		view, err := eng.ReadThreadGraph(t.Context(), threadID, store.GraphQuery{Limit: -1, All: true})
		if err != nil {
			t.Errorf("read graph: %v", err)
		}
		graphToolCall(w, request, callID(step), ToolUpdateGraph, steps[step-1](graphNow{view}))
	})
	eng = e
	thread, err := e.CreateThread("Graph acceptance", "", "")
	if err != nil {
		t.Fatal(err)
	}
	threadID = thread.ID
	timeline := watchGraph(t, e, thread.ID)
	e.cfg.Swarm.GraphAutoRecord = true
	turn, err := e.StartTurn(thread.ID, "Work through the task and keep the graph current.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	revisions, doing := timeline.finish()
	if got.Status != store.TurnDone {
		t.Fatalf("turn ended %q", got.Status)
	}
	return e, thread.ID, revisions, doing
}

// subjectsOf indexes the committed subjects by kind.
func subjectsOf(view store.GraphView) map[string][]store.GraphSubject {
	byKind := map[string][]store.GraphSubject{}
	for _, s := range view.Subjects {
		byKind[s.Kind] = append(byKind[s.Kind], s)
	}
	return byKind
}

// requireSettledProgress is the shared progress acceptance: the graph moved
// more than once while the turn was running, a to-do was visibly in progress,
// and nothing is left open or doing once the turn has finished.
func requireSettledProgress(t *testing.T, e *Engine, threadID string, revisions []int64, sawDoing bool) store.GraphView {
	t.Helper()
	if len(revisions) < 2 { // the picture before the commit, then the committed revision
		t.Errorf("the graph was observed at revisions %v; progress must be visible before the turn ends", revisions)
	}
	if !sawDoing {
		t.Errorf("no to-do was ever observed in progress")
	}
	view, err := e.ReadThreadGraph(t.Context(), threadID, store.GraphQuery{Limit: -1, All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, todo := range view.Todos {
		if todo.Status == graph.TodoOpen || todo.Status == graph.TodoDoing {
			t.Errorf("to-do %q is still %s after the turn", todo.Title, todo.Status)
		}
	}
	return view
}

// A model that records a finished to-do in its first write still shows that
// to-do as in progress. The host then marks it done, so the model does not
// spend another call on the flip. A later call that repeats the title while
// one is still in progress updates that to-do instead of adding another.
func TestAcceptanceFirstFinishedTodoStaysVisibleUntilItIsSettled(t *testing.T) {
	const ev = `,"evidence":[{"url":"https://records.example.test/run"}]`
	const title = "Record the finding"
	e, threadID, revisions, doing := runGraphScript(t, []func(graphNow) string{
		func(graphNow) string {
			return `{"todos":[{"ref":"t","title":"` + title + `","status":"done"}],
				"subjects":[{"kind":"domain","attrs":{"domain":"example.test"}}]` + ev + `}`
		},
	})
	view := requireSettledProgress(t, e, threadID, revisions, doing)
	if len(view.Todos) != 1 || view.Todos[0].Status != graph.TodoDone {
		t.Fatalf("to-dos: %+v", view.Todos)
	}
	if len(view.Subjects) != 1 || view.Subjects[0].Kind != "domain" {
		t.Fatalf("subjects: %+v", view.Subjects)
	}
	if view.Revision != 1 {
		t.Fatalf("the in-progress frame must not be its own commit, revision %d", view.Revision)
	}
}

// A finished to-do with nothing to cite is refused. The refusal is decided
// before any row is committed, so the graph stays empty.
func TestRefusedFinishedTodoLeavesTheGraphEmpty(t *testing.T) {
	var eng *Engine
	var threadID string
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step > 1 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Stopped."}, "stop")
			return
		}
		graphToolCall(w, request, "graph-refused", ToolUpdateGraph, `{
			"todos":[{"title":"Finish without evidence","status":"done"}],
			"subjects":[{"kind":"domain","attrs":{"domain":"refused.example.test"}}]}`)
	})
	eng = e
	thread, err := e.CreateThread("Refused graph", "", "")
	if err != nil {
		t.Fatal(err)
	}
	threadID = thread.ID
	turn, err := e.StartTurn(thread.ID, "Record the work.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("turn ended %q", got.Status)
	}
	view, err := eng.ReadThreadGraph(t.Context(), threadID, store.GraphQuery{All: true, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision != 0 || len(view.Subjects) != 0 || len(view.Todos) != 0 {
		t.Fatalf("a refused batch left %+v", view)
	}
}

// A model that names a tool call that never happened, and gives a made-up id
// to a subject and a to-do, still gets one recorded write instead of a refusal
// it has to answer by resending the whole batch.
func TestAcceptanceInventedIdsAndToolCallsDoNotRefuseTheWrite(t *testing.T) {
	const title = "Record the finding"
	e, threadID, revisions, doing := runGraphScript(t, []func(graphNow) string{
		func(graphNow) string {
			return `{"own_tool_call_ids":["exec-1"],
				"todos":[{"id":"todo-1","version":1,"title":"` + title + `","status":"doing"}],
				"subjects":[{"id":"domain-1","version":1,"kind":"domain","attrs":{"domain":"example.test"},"status":"lead"}]}`
		},
		func(g graphNow) string {
			return `{"own_tool_call_ids":["exec-1"],"evidence":[{"url":"https://records.example.test/run"}],
				"todos":[` + g.settle(title, "done") + `]}`
		},
	})
	view := requireSettledProgress(t, e, threadID, revisions, doing)
	if len(view.Subjects) != 1 || len(view.Todos) != 1 || view.Todos[0].Status != graph.TodoDone {
		t.Fatalf("graph: %+v", view)
	}
}

// Analysing an organisation: subjects of several kinds appear step by step,
// each to-do is settled with evidence, and the finished graph is complete.
func TestAcceptanceExposureAnalysisBuildsACompleteGraphWithVisibleProgress(t *testing.T) {
	const ev = `,"evidence":[{"url":"https://records.example.test/run"}]`
	const (
		scope, hosts, stack = "Identify the organisation", "List its addresses and regions", "List the software and cloud providers"
	)
	e, threadID, revisions, doing := runGraphScript(t, []func(graphNow) string{
		func(graphNow) string {
			return `{"todos":[{"title":"` + scope + `","status":"doing"},{"title":"` + hosts + `"},{"title":"` + stack + `"}],
				"subjects":[{"kind":"company","attrs":{"name":"Example Org"},"status":"lead"}]}`
		},
		func(g graphNow) string {
			return `{"todos":[` + g.settle(scope, "done") + `,` + g.settle(hosts, "doing") + `]` + ev + `}`
		},
		func(g graphNow) string {
			org, _ := g.subject("company")
			return `{"subjects":[{"ref":"a","kind":"ip","attrs":{"ip":"203.0.113.1","country":"NL"}},
				{"ref":"b","kind":"ip","attrs":{"ip":"203.0.113.2","country":"US"}},
				{"ref":"c","kind":"ip","attrs":{"ip":"203.0.113.3","country":"SG"}}],
				"links":[{"from":"` + org + `","to":"a","rel":"owns"},{"from":"` + org + `","to":"b","rel":"owns"},{"from":"` + org + `","to":"c","rel":"owns"}]}`
		},
		func(g graphNow) string {
			return `{"todos":[` + g.settle(hosts, "done") + `,` + g.settle(stack, "doing") + `]` + ev + `}`
		},
		func(g graphNow) string {
			org, _ := g.subject("company")
			return `{"subjects":[{"ref":"app","kind":"app","attrs":{"name":"web server"}},{"ref":"cloud","kind":"other","attrs":{"name":"ExampleCloud"}}],
				"links":[{"from":"` + org + `","to":"app","rel":"runs"},{"from":"` + org + `","to":"cloud","rel":"hosted_on"}]}`
		},
		func(g graphNow) string {
			org, version := g.subject("company")
			return `{"subjects":[{"id":"` + org + `","version":` + version + `,"status":"confirmed"}],"todos":[` + g.settle(stack, "done") + `]` + ev + `}`
		},
	})
	view := requireSettledProgress(t, e, threadID, revisions, doing)
	byKind := subjectsOf(view)
	if len(byKind["company"]) != 1 || byKind["company"][0].Status != graph.StatusConfirmed {
		t.Errorf("the organisation was not recorded once and confirmed: %+v", byKind["company"])
	}
	if len(byKind["ip"]) != 3 {
		t.Errorf("want 3 addresses, got %d", len(byKind["ip"]))
	}
	for _, ip := range byKind["ip"] {
		if country, _ := ip.Attrs["country"].(string); country == "" {
			t.Errorf("address %q has no region", ip.Name)
		}
	}
	if len(byKind["app"]) != 1 || len(byKind["other"]) != 1 || byKind["other"][0].Name != "ExampleCloud" || len(view.Links) != 5 {
		t.Errorf("apps=%+v cloud providers=%+v links=%d", byKind["app"], byKind["other"], len(view.Links))
	}
	for _, todo := range view.Todos {
		if todo.Status != graph.TodoDone {
			t.Errorf("to-do %q ended %s", todo.Title, todo.Status)
		}
	}
}

// Refining a rule over several rounds: the first draft is wrong, the run of it
// fails and says why, and the corrected statement is a new subject linked to
// the draft, which stays on record as retracted. A statement that was run is
// never rewritten.
func TestAcceptanceQueryRefinementKeepsTheFailedDraftAndLinksTheCorrection(t *testing.T) {
	const ev = `,"evidence":[{"url":"https://records.example.test/run"}]`
	const draft, check, fix = "Draft a query from the source", "Run the query and check the count", "Correct the query and run it again"
	const wrong, right = `title=\"wrong draft\"`, `domain=\"example.test\"`
	e, threadID, revisions, doing := runGraphScript(t, []func(graphNow) string{
		func(graphNow) string {
			return `{"todos":[{"title":"` + draft + `","status":"doing"},{"title":"` + check + `"}],
				"subjects":[{"kind":"fofa_query","attrs":{"query":"` + wrong + `","purpose":"first guess"}}]}`
		},
		func(g graphNow) string {
			return `{"todos":[` + g.settle(draft, "done") + `,` + g.settle(check, "doing") + `]` + ev + `}`
		},
		func(g graphNow) string {
			id, version := g.todo(check)
			old, oldVersion := g.subject("fofa_query")
			return `{"todos":[{"id":"` + id + `","version":` + version + `,"status":"failed","result":"the draft matched nothing; the field name was wrong"},
				{"title":"` + fix + `","status":"doing","before":"` + id + `"}],
				"subjects":[{"id":"` + old + `","version":` + oldVersion + `,"status":"retracted"}]}`
		},
		func(g graphNow) string {
			old, _ := g.subject("fofa_query")
			return `{"subjects":[{"ref":"next","kind":"fofa_query","attrs":{"query":"` + right + `","purpose":"corrected after the failed run"}}],
				"links":[{"from":"next","to":"` + old + `","rel":"refines"}]}`
		},
		func(g graphNow) string {
			return `{"todos":[` + g.settle(fix, "done") + `]` + ev + `}`
		},
	})
	view := requireSettledProgress(t, e, threadID, revisions, doing)
	queries := subjectsOf(view)["fofa_query"]
	if len(queries) != 2 {
		t.Fatalf("want the draft and its correction, got %d queries", len(queries))
	}
	var retracted, current *store.GraphSubject
	for i := range queries {
		if queries[i].Status == graph.StatusRetracted {
			retracted = &queries[i]
		} else {
			current = &queries[i]
		}
	}
	if retracted == nil || current == nil || retracted.Attrs["query"] != `title="wrong draft"` || current.Attrs["query"] != `domain="example.test"` {
		t.Fatalf("draft and correction are not told apart: %+v", queries)
	}
	if len(view.Links) != 1 || view.Links[0].FromID != current.ID || view.Links[0].ToID != retracted.ID {
		t.Errorf("the correction is not linked to its draft: %+v", view.Links)
	}
	failed := 0
	for _, todo := range view.Todos {
		if todo.Status == graph.TodoFailed {
			failed++
			if todo.Result == "" {
				t.Errorf("a failed to-do kept no reason: %+v", todo)
			}
		}
	}
	if failed != 1 {
		t.Errorf("the failed run should stay on record once, got %d", failed)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
