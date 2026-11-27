package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The model controls the argument text and nothing else. A malformed, padded
// or unknown-field call must come back as repairable feedback, never as a
// partial write.
// A lead linked to a confirmed subject of the same kind would show two current
// things. Peers that are both still leads stay current, and a lead linked to
// another kind stays a lead.
func TestLeadLinkedToConfirmedSameKindIsRetracted(t *testing.T) {
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{
			{Ref: "keep", Kind: "ip", Status: graph.StatusConfirmed, Attrs: map[string]any{"ip": "203.0.113.10"}},
			{Ref: "old", Kind: "ip", Status: graph.StatusLead, Attrs: map[string]any{"ip": "203.0.113.11"}},
			{Ref: "peer", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.12"}},
			{Ref: "other", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.13"}},
			{Ref: "host", Kind: "domain", Status: graph.StatusConfirmed, Attrs: map[string]any{"domain": "example.test"}},
		},
		Links: []graph.LinkInput{
			{From: "old", To: "keep", Rel: "earlier"},
			{From: "peer", To: "other", Rel: "same_network"},
			{From: "old", To: "host", Rel: "resolved"},
		},
	}
	retractLeadsLinkedToConfirmed(&batch, nil)
	status := map[string]string{}
	for _, subject := range batch.Subjects {
		status[subject.Ref] = subject.Status
	}
	if status["old"] != graph.StatusRetracted || status["keep"] != graph.StatusConfirmed || status["peer"] != "" || status["other"] != "" || status["host"] != graph.StatusConfirmed {
		t.Fatalf("statuses=%v", status)
	}
	known := map[string]knownSubject{"sj_keep": {Kind: "ip", Status: graph.StatusConfirmed}}
	later := graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "next", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.14"}}},
		Links:    []graph.LinkInput{{From: "next", To: "sj_keep", Rel: "earlier"}},
	}
	retractLeadsLinkedToConfirmed(&later, known)
	if later.Subjects[0].Status != graph.StatusRetracted {
		t.Fatalf("a lead linked to an existing confirmed subject stayed %q", later.Subjects[0].Status)
	}
}

func TestIllFormedEvidenceIsDroppedAndOwnResultsAreCited(t *testing.T) {
	batch := graph.Batch{Evidence: []graph.Evidence{{URL: "file:///tmp/report.json", Label: "the file"}}}
	prepareGraphEvidence(&batch)
	if len(batch.Evidence) != 0 || !batch.CiteOwnResults {
		t.Fatalf("ill-formed evidence stayed: %+v", batch)
	}
	kept := graph.Batch{Evidence: []graph.Evidence{{URL: "https://records.example.test/run"}}}
	prepareGraphEvidence(&kept)
	if len(kept.Evidence) != 1 || kept.CiteOwnResults {
		t.Fatalf("a usable address was replaced: %+v", kept)
	}
	named := graph.Batch{OwnToolCallIDs: []string{"call-1"}}
	prepareGraphEvidence(&named)
	if named.CiteOwnResults || len(named.OwnToolCallIDs) != 1 {
		t.Fatalf("an explicit tool-call citation was replaced: %+v", named)
	}
}

func TestGraphToolArgumentsAreStrictlyDecoded(t *testing.T) {
	var batch graph.Batch
	if err := decodeGraphArgs("  ", &batch); err != nil {
		t.Fatalf("blank arguments are an empty object: %v", err)
	}
	for name, args := range map[string]string{
		"unknown field":  `{"subjects":[],"colour":"red"}`,
		"not json":       `{`,
		"trailing value": `{"todos":[{"title":"x"}]} {"todos":[]}`,
	} {
		err := decodeGraphArgs(args, &batch)
		if !errors.Is(err, graph.ErrInvalid) || !isGraphFeedback(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if isGraphFeedback(errors.New("disk full")) {
		t.Error("a storage failure is not something the model can repair")
	}
	for _, err := range []error{graph.ErrConflict, store.ErrNotFound, ErrGraphDisabled} {
		if !isGraphFeedback(fmt.Errorf("wrapped: %w", err)) {
			t.Errorf("%v must read as feedback", err)
		}
	}
}

// A read result is bounded by whole rows: cutting JSON mid-row would hand the
// model text it cannot parse, so rows drop and More says so.
func TestGraphReadReplyShrinksByWholeRowsAndSaysThereIsMore(t *testing.T) {
	reply := &graphReadReply{
		Subjects: []graphReadSubject{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Links:    []graphReadLink{{From: "a", To: "b"}, {From: "b", To: "c"}},
	}
	steps := 0
	for reply.shrink() {
		steps++
	}
	if len(reply.Links) != 0 || len(reply.Subjects) != 1 || !reply.More || steps < 3 {
		t.Fatalf("after shrinking: %d subjects, %d links, more=%v, %d steps", len(reply.Subjects), len(reply.Links), reply.More, steps)
	}
	todos := &graphReadReply{Todos: []graphReadTodo{{ID: "1"}, {ID: "2"}, {ID: "3"}}}
	for todos.shrink() {
	}
	if len(todos.Todos) != 1 || !todos.More {
		t.Fatalf("to-dos: %+v", todos)
	}
	if (&graphReadReply{}).shrink() {
		t.Fatal("an empty reply has nothing to drop")
	}
}

func TestGraphReplyShowsACredentialOnlyInTheByIDRead(t *testing.T) {
	view := store.GraphView{Revision: 3, Total: 1, Subjects: []store.GraphSubject{{
		ID: "sj_1", Kind: "repo", Name: "acme/app", Status: "lead", Version: 1,
		Attrs: map[string]any{"path": "acme/app", "credential": fakeGraphCredential},
	}}, Links: []store.GraphLink{{FromID: "sj_1", ToID: "sj_2", Rel: "uses"}}, Todos: []store.GraphTodo{{ID: "td_1", Title: "look", About: "sj_1", Status: "open", Version: 1}}}
	list, one := graphReply(view, ""), graphReply(view, "sj_1")
	if _, leaked := list.Subjects[0].Attrs["credential"]; leaked || len(list.Subjects[0].Credentials) != 1 {
		t.Fatalf("list read: %+v", list.Subjects[0])
	}
	if one.Subjects[0].Attrs["credential"] != fakeGraphCredential || len(one.Subjects[0].Credentials) != 0 {
		t.Fatalf("by-id read: %+v", one.Subjects[0])
	}
	if len(list.Links) != 1 || list.Links[0].Rel != "uses" || len(list.Todos) != 1 || list.Todos[0].About != "sj_1" {
		t.Fatalf("links and to-dos were not projected: %+v", list)
	}
}

func TestGraphWritesAreRefusedWithoutTrustedRuntimeProvenance(t *testing.T) {
	e := newTestEngine(t)
	turn := &store.Turn{ID: "tn_x", ThreadID: "th_x", GraphTools: true}
	cancelled, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("stopped by a person"))
	if _, _, err := e.graphCaller(cancelled, turn); err == nil || !strings.Contains(err.Error(), "stopped by a person") {
		t.Errorf("a cancelled call must carry its cause: %v", err)
	}
	if _, _, err := e.graphCaller(context.Background(), &store.Turn{ID: "tn_x"}); !errors.Is(err, ErrGraphDisabled) {
		t.Errorf("a turn that froze the graph off: %v", err)
	}
	if _, _, err := e.graphCaller(context.Background(), turn); err == nil || !strings.Contains(err.Error(), "trusted turn owner") {
		t.Errorf("no owner in the context: %v", err)
	}
	owned := context.WithValue(context.Background(), historyReadOwnerKey{}, historyReadOwner{ThreadID: "th_other", TurnID: "tn_x"})
	if _, _, err := e.graphCaller(owned, turn); err == nil || !strings.Contains(err.Error(), "trusted turn owner") {
		t.Errorf("an owner of another thread: %v", err)
	}
	owned = context.WithValue(context.Background(), historyReadOwnerKey{}, historyReadOwner{ThreadID: "th_x", TurnID: "tn_x"})
	if _, _, err := e.graphCaller(owned, turn); err == nil || !strings.Contains(err.Error(), "actual agent invocation") {
		t.Errorf("no agent identity: %v", err)
	}
	if _, err := e.updateGraphTool(owned, &store.Turn{ID: "tn_x", ThreadID: "th_x", GraphTools: true, GraphReadOnly: true}, `{}`); err == nil {
		t.Error("a planning turn wrote the graph")
	}
	if _, err := e.readGraphTool(cancelled, turn, `{}`); err == nil {
		t.Error("a cancelled read succeeded")
	}
	if _, err := e.readGraphTool(context.Background(), &store.Turn{ID: "tn_x"}, `{}`); !errors.Is(err, ErrGraphDisabled) {
		t.Errorf("read with the graph off: %v", err)
	}
	if _, err := e.readGraphTool(context.Background(), turn, `{}`); err == nil {
		t.Error("a read without a trusted owner succeeded")
	}
	if _, err := e.readGraphTool(owned, turn, `{"offset":-1}`); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("negative offset: %v", err)
	}
	if _, err := e.readGraphTool(owned, turn, `{"colour":"red"}`); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown read field: %v", err)
	}
}

// A late write must not recreate state for a turn that a rewind removed or a
// settings change switched off, and must not outlive a cancellation.
func TestGraphWriteLockRefusesDroppedTurnsDisabledGraphAndCancellation(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.GraphAutoRecord = true
	unlock, err := e.lockGraphWrite(context.Background(), "tn_live")
	if err != nil {
		t.Fatal(err)
	}
	unlock()

	e.recordMu.Lock()
	e.droppedTurns["tn_rewound"] = struct{}{}
	e.recordMu.Unlock()
	if _, err := e.lockGraphWrite(context.Background(), "tn_rewound"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("rewound turn: %v", err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("handoff"))
	if _, err := e.lockGraphWrite(ctx, "tn_live"); err == nil || !strings.Contains(err.Error(), "handoff") {
		t.Errorf("cancelled: %v", err)
	}
	// Both locks must be free again after every refusal.
	unlock, err = e.lockGraphWrite(context.Background(), "tn_live")
	if err != nil {
		t.Fatalf("a refusal left a lock held: %v", err)
	}
	unlock()

	e.configUpdateMu.Lock()
	e.cfg.Swarm.GraphAutoRecord = false
	e.configUpdateMu.Unlock()
	if _, err := e.lockGraphWrite(context.Background(), "tn_live"); !errors.Is(err, ErrGraphDisabled) {
		t.Errorf("automatic recording switched off: %v", err)
	}

	e.configUpdateMu.Lock()
	e.cfg.Swarm.GraphAutoRecord = true
	e.cfg.Swarm.Graph = false
	e.configUpdateMu.Unlock()
	if _, err := e.lockGraphWrite(context.Background(), "tn_live"); !errors.Is(err, ErrGraphDisabled) {
		t.Errorf("graph switched off: %v", err)
	}
}

func TestPanelReadsRefuseUnknownOwnersAndNeverReturnCredentials(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	if _, err := e.ReadThreadGraph(ctx, "th_missing", store.GraphQuery{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown thread: %v", err)
	}
	if _, err := e.ThreadGraphRevision(ctx, "th_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown thread revision: %v", err)
	}
	if _, err := e.ReadProjectGraph(ctx, "pr_missing", store.GraphQuery{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
	project, err := e.CreateProject("Graph panel", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	view, err := e.ReadProjectGraph(ctx, project.ID, store.GraphQuery{All: true})
	if err != nil || view.OwnerID != project.ID || len(view.Subjects) != 0 {
		t.Fatalf("empty project graph: %+v, %v", view, err)
	}
	thread, err := e.CreateThread("Panel", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if rev, err := e.ThreadGraphRevision(ctx, thread.ID); err != nil || rev != 0 {
		t.Fatalf("a graph nothing was written to has revision 0: %d, %v", rev, err)
	}
}

// Stored receipts survive the upgrade: the payload boundary is the marker or
// either generation of tool name, never a guess from the result text.
func TestStoredGraphPayloadIsRecognisedByMarkerOrEitherToolGeneration(t *testing.T) {
	for _, name := range []string{ToolUpdateGraph, ToolReadGraph, "record_work", "read_work"} {
		if !isWorkRecordTool(name) {
			t.Errorf("%s is graph traffic", name)
		}
	}
	if isWorkRecordTool("exec") {
		t.Error("an unrelated tool was classified as graph traffic")
	}
	marked := restoredToolReceipt("{}", "call", true)
	plain := restoredToolReceipt("{}", "call", false)
	named := schema.ToolMessage("{}", "call")
	named.ToolName = ToolReadGraph
	if !workRecordToolResult(marked) || workRecordToolResult(plain) || !workRecordToolResult(named) {
		t.Fatalf("marked=%v plain=%v named=%v", workRecordToolResult(marked), workRecordToolResult(plain), workRecordToolResult(named))
	}
	if workRecordToolResult(schema.UserMessage("{}")) {
		t.Error("only a tool result can be graph payload")
	}
}

// A copied ref, a ref: prefix and an attribute the kind does not declare used
// to refuse the whole batch and make the model send it again.
func TestGraphBatchRepairsRefCopiesAndUnknownAttributes(t *testing.T) {
	e := &Engine{graphRefs: map[string]map[string]string{"owner": {"example-corp": "sj_known"}}}
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{{
			ID: "draft", Ref: "draft", Kind: "fofa_query",
			Attrs: map[string]any{"query": `title="x"`, "note": "extra"},
		}},
		Links: []graph.LinkInput{{From: "ref:draft", To: "draft", Rel: "earlier"}},
	}
	dropped := e.relaxGraphBatch("owner", &batch)
	if batch.Subjects[0].ID != "" || batch.Subjects[0].Attrs["note"] != nil {
		t.Fatalf("subject was not repaired: %+v", batch.Subjects[0])
	}
	if batch.Links[0].From != "draft" || batch.Links[0].To != "draft" {
		t.Fatalf("a ref in this batch was rewritten: %+v", batch.Links[0])
	}
	if len(dropped) != 1 || dropped[0] != "fofa_query.note" {
		t.Fatalf("omitted=%v", dropped)
	}
	later := graph.Batch{Links: []graph.LinkInput{{From: "example-corp", To: "sj_other", Rel: "owns"}}}
	e.relaxGraphBatch("owner", &later)
	if later.Links[0].From != "sj_known" {
		t.Fatalf("an earlier ref was not the stored id: %+v", later.Links[0])
	}
}

// A name the model chose for a new subject or to-do, an empty evidence list and
// repeated omitted attributes used to refuse the batch or bloat its receipt.
func TestGraphBatchTreatsInventedIdsAsNewAndDeduplicatesOmissions(t *testing.T) {
	e := &Engine{}
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{
			{ID: "company-example", Version: 3, Ref: "company", Kind: "company", Attrs: map[string]any{"name": "x", "reg_no": nil}},
			{ID: "sj_real", Version: 2, Kind: "company", Status: "confirmed"},
			{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.1", "cloud": "a"}},
			{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.2", "cloud": "b"}},
		},
		Todos:          []graph.TodoInput{{ID: "todo-1", Version: 2, Title: "t"}, {ID: "td_real", Version: 4, Status: "done"}},
		OwnToolCallIDs: []string{},
	}
	prepareGraphEvidence(&batch)
	dropped := e.relaxGraphBatch("owner", &batch)
	if batch.Subjects[0].ID != "" || batch.Subjects[0].Version != 0 || len(batch.Subjects[0].Attrs) != 1 {
		t.Fatalf("invented subject id kept: %+v", batch.Subjects[0])
	}
	if batch.Subjects[1].ID != "sj_real" || batch.Subjects[1].Version != 2 {
		t.Fatalf("a real id was changed: %+v", batch.Subjects[1])
	}
	if batch.Todos[0].ID != "" || batch.Todos[0].Version != 0 || batch.Todos[1].ID != "td_real" || batch.Todos[1].Version != 4 {
		t.Fatalf("to-ids: %+v", batch.Todos)
	}
	if len(dropped) != 1 || dropped[0] != "ip.cloud" {
		t.Fatalf("omitted=%v", dropped)
	}
	if batch.OwnToolCallIDs != nil || !batch.CiteOwnResults {
		t.Fatalf("an empty id list blocked citing the caller's results: %+v", batch)
	}
}
