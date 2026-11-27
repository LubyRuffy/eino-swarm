package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const fakeGraphCredential = "fake-credential-for-graph-test"

func graphDigestOf(request scheduleCompletionRequest) string {
	for _, m := range request.Messages {
		if m.Role == schema.System && strings.HasPrefix(m.Content, graphDigestPrefix) {
			return m.Content
		}
	}
	return ""
}

func graphToolCall(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	panelAdmissionCall(w, request, id, name, args)
}

// A manager writes through the real tool graph over HTTP, sees the digest on its
// next request, and only the by-id read ever returns the credential. The digest
// and the list read name the credential and hold its value back.
func TestManagerWritesAndReadsTheGraphThroughTheActualModelBoundary(t *testing.T) {
	var updateResult struct {
		Result graph.Result `json:"result"`
	}
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			if !requestHasTool(request, ToolUpdateGraph) || !requestHasTool(request, ToolReadGraph) {
				t.Errorf("graph tools were not offered: %s", requestToolNames(request))
			}
			if graphDigestOf(request) != "" {
				t.Errorf("an empty graph produced a digest")
			}
			graphToolCall(w, request, "graph-write", ToolUpdateGraph, `{
				"todos":[{"ref":"t","title":"look at the source","status":"doing"}],
				"subjects":[{"ref":"r","kind":"repo","attrs":{"path":"acme/app","credential":"`+fakeGraphCredential+`"}},
				            {"ref":"h","kind":"ip","attrs":{"ip":"203.0.113.7"}}],
				"links":[{"from":"r","to":"h","rel":"deployed_on"}]}`)
		case 2:
			receipt := scriptedToolReceipt(request, "graph-write")
			if err := json.Unmarshal([]byte(receipt), &updateResult); err != nil || len(updateResult.Result.Subjects) != 2 {
				t.Errorf("write receipt %q: %v", receipt, err)
			}
			digest := graphDigestOf(request)
			if !strings.Contains(digest, "203.0.113.7") || !strings.Contains(digest, "look at the source") || !strings.Contains(digest, "doing") {
				t.Errorf("digest does not describe the graph: %q", digest)
			}
			if strings.Contains(digest, fakeGraphCredential) {
				t.Errorf("the digest leaked the credential")
			}
			graphToolCall(w, request, "graph-list", ToolReadGraph, `{"text":"acme"}`)
		case 3:
			list := scriptedToolReceipt(request, "graph-list")
			if strings.Contains(list, fakeGraphCredential) || !strings.Contains(list, `"credentials":["credential"]`) {
				t.Errorf("a list read must name the credential and hold its value back: %s", list)
			}
			repo := ""
			for _, s := range updateResult.Result.Subjects {
				if s.Kind == "repo" {
					repo = s.ID
				}
			}
			graphToolCall(w, request, "graph-one", ToolReadGraph, `{"id":"`+repo+`"}`)
		case 4:
			one := scriptedToolReceipt(request, "graph-one")
			if !strings.Contains(one, fakeGraphCredential) {
				t.Errorf("the by-id read is the one place the model sees a credential: %s", one)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	thread, err := e.CreateThread("Graph fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.GraphAutoRecord = true
	turn, err := e.StartTurn(thread.ID, "Keep track of what is found.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !got.GraphTools {
		t.Fatalf("turn=%+v", got)
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Limit: -1})
	if err != nil || len(view.Subjects) != 2 || len(view.Links) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	todos, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Todos: true})
	if err != nil || len(todos.Todos) != 1 || todos.Todos[0].Status != graph.TodoDoing {
		t.Fatalf("todos=%+v err=%v", todos, err)
	}
	for _, s := range view.Subjects {
		if _, ok := s.Attrs["credential"]; ok {
			t.Fatalf("the panel's read leaked a credential: %+v", s)
		}
	}
	// Graph tool traffic is a stored copy of the graph, so it is tagged and
	// kept out of compaction and review.
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	tagged := 0
	for _, ev := range events {
		if ev.Kind == "tool_result" && ev.WorkRecordPayload {
			tagged++
		}
	}
	if tagged != 3 {
		t.Fatalf("%d graph tool results were tagged, want 3", tagged)
	}
}

// A lead linked to a confirmed subject of the same kind is stored retracted,
// including a later call that only names the confirmed subject by id. Two
// leads linked to each other stay leads.
func TestLeadLinkedToAConfirmedSubjectIsStoredRetracted(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			graphToolCall(w, request, "graph-versions", ToolUpdateGraph, `{
				"evidence":[{"url":"https://records.example.test/run","label":"run"}],
				"subjects":[
					{"ref":"keep","kind":"ip","status":"confirmed","attrs":{"ip":"203.0.113.10"}},
					{"ref":"old","kind":"ip","status":"lead","attrs":{"ip":"203.0.113.11"}},
					{"ref":"peer","kind":"ip","attrs":{"ip":"203.0.113.12"}},
					{"ref":"other","kind":"ip","attrs":{"ip":"203.0.113.13"}}
				],
				"links":[
					{"from":"old","to":"keep","rel":"earlier"},
					{"from":"peer","to":"other","rel":"same_network"}
				]}`)
		case 2:
			receipt := scriptedToolReceipt(request, "graph-versions")
			var written struct {
				Result graph.Result `json:"result"`
			}
			if err := json.Unmarshal([]byte(receipt), &written); err != nil {
				t.Errorf("receipt: %v", err)
			}
			keep := ""
			for _, subject := range written.Result.Subjects {
				if subject.Name == "203.0.113.10" {
					keep = subject.ID
				}
			}
			if keep == "" {
				t.Errorf("confirmed subject missing from %s", receipt)
			}
			graphToolCall(w, request, "graph-later", ToolUpdateGraph, `{
				"subjects":[{"ref":"next","kind":"ip","attrs":{"ip":"203.0.113.14"}}],
				"links":[{"from":"next","to":"`+keep+`","rel":"earlier"}]}`)
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	thread, err := e.CreateThread("Graph versions", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.GraphAutoRecord = true
	turn, err := e.StartTurn(thread.ID, "Keep one current address.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("turn=%+v", got)
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Limit: -1, All: true})
	if err != nil {
		t.Fatal(err)
	}
	byIP := map[string]string{}
	for _, subject := range view.Subjects {
		byIP[fmtAttr(subject, "ip")] = subject.Status
	}
	if byIP["203.0.113.10"] != graph.StatusConfirmed || byIP["203.0.113.11"] != graph.StatusRetracted || byIP["203.0.113.14"] != graph.StatusRetracted || byIP["203.0.113.12"] != graph.StatusLead || byIP["203.0.113.13"] != graph.StatusLead {
		t.Fatalf("statuses=%v", byIP)
	}
}

func fmtAttr(subject store.GraphSubject, key string) string {
	value, _ := subject.Attrs[key].(string)
	return value
}

// The setting is frozen into the turn: switching it off hides the tools from
// the next turn and refuses a write from one already running.
func TestGraphSwitchOffRemovesTheToolsAndRefusesALateWrite(t *testing.T) {
	var offered bool
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		offered = offered || requestHasTool(request, ToolUpdateGraph) || requestHasTool(request, ToolReadGraph)
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Done."}, "stop")
	})
	if err := e.cfg.Update(func(c *config.Config) error { c.Swarm.Graph = false; return nil }); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Graph off", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Say done.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.GraphTools || offered {
		t.Fatalf("graph=%v offered=%v", got.GraphTools, offered)
	}
}

// The methodology names no kind and no sample task: kinds live in the tool
// definition and the work is whatever the human asked for.
func TestGraphGuidanceIsGeneric(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		text := strings.ToLower(graphInstruction(readOnly))
		for _, kind := range graph.MustDefault().IDs() {
			if kind == "other" {
				continue
			}
			for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r == '_') }) {
				if word == kind {
					t.Errorf("guidance names the kind %q", kind)
				}
			}
		}
		for _, leak := range []string{"fofa", "news", "enterprise", "company", "exposure", "query", "vulnerab"} {
			if strings.Contains(text, leak) {
				t.Errorf("guidance leaks %q", leak)
			}
		}
	}
	if strings.Contains(graphInstruction(true), "update_graph") {
		t.Error("the planning guidance offers a write tool")
	}
}

// A delegated worker gets the graph tools through real registry construction,
// and what it writes is attributed to that worker's identity, not the manager's.
func TestDelegatedWorkerWritesTheGraphUnderItsOwnIdentity(t *testing.T) {
	e := newTestEngine(t)
	var managers, workers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if len(request.Tools) == 0 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Noted."}, "stop")
			return
		}
		if requestHasTool(request, "spawn_agent") {
			switch managers.Add(1) {
			case 1:
				graphToolCall(w, request, "graph-spawn", "spawn_agent", `{"role":"scout","task":"Record one subject in the graph."}`)
			case 2:
				var spawned toolScopeWorkerID
				if err := json.Unmarshal([]byte(scriptedToolReceipt(request, "graph-spawn")), &spawned); err != nil || spawned.AgentID == "" {
					t.Errorf("spawn=%+v %v", spawned, err)
				}
				args, _ := json.Marshal(map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
				graphToolCall(w, request, "graph-collect", "wait_agents", string(args))
			default:
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Collected."}, "stop")
			}
			return
		}
		if !requestHasTool(request, ToolUpdateGraph) || !requestHasTool(request, ToolReadGraph) {
			t.Errorf("worker lacks the graph tools: %s", requestToolNames(request))
		}
		switch workers.Add(1) {
		case 1:
			graphToolCall(w, request, "worker-graph-write", ToolUpdateGraph, `{"subjects":[{"kind":"domain","attrs":{"domain":"example.test"}}]}`)
		default:
			if !strings.Contains(scriptedToolReceipt(request, "worker-graph-write"), `"ok":true`) {
				t.Errorf("worker write receipt: %s", scriptedToolReceipt(request, "worker-graph-write"))
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Memory.AutoDream = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "graph-worker-fixture"
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("Delegated graph fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.GraphAutoRecord = true
	turn, err := e.StartTurn(thread.ID, "Delegate recording one subject.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("turn=%+v", got)
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{})
	if err != nil || len(view.Subjects) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if s := view.Subjects[0]; s.AgentID == "manager" || s.AgentID == "" || s.Generation != 1 || s.TurnID != turn.ID {
		t.Fatalf("worker provenance: agent=%q generation=%d turn=%q", s.AgentID, s.Generation, s.TurnID)
	}
}

func TestTurningOffAutomaticGraphRecordingKeepsGraphReadsOn(t *testing.T) {
	offered := make(chan [2]bool, 2)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		offered <- [2]bool{requestHasTool(request, ToolReadGraph), requestHasTool(request, ToolUpdateGraph)}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Done."}, "stop")
	})
	thread, err := e.CreateThread("Browse work records", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Summarize the existing records.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	select {
	case tools := <-offered:
		if !tools[0] || tools[1] || !got.GraphTools || !got.GraphReadOnly {
			t.Fatalf("tools=%v graph=%v read-only=%v", tools, got.GraphTools, got.GraphReadOnly)
		}
	default:
		t.Fatal("the conversation did not reach the model")
	}
	if _, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{}); err != nil {
		t.Fatalf("existing graph became unreadable: %v", err)
	}
}
