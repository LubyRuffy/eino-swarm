package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGraphPreviewAndEventsKeepRepositoryCredentialsPrivate(t *testing.T) {
	const secret = "dummy-audit-repo-password"
	const secretByID = "dummy-audit-by-id-password"
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			graphToolCall(w, request, "audit-write", ToolUpdateGraph, `{"evidence":[{"url":"https://source.example.test/reference"}],"subjects":[{"ref":"repo","kind":"repo","attrs":{"url":"https://fixture:`+secret+`@example.test/acme/audit.git"}},{"ref":"addr","kind":"ip","attrs":{"ip":"203.0.113.9"}}],"links":[{"from":"repo","to":"addr","rel":"served_from"}],"todos":[{"title":"audit","status":"done"}]}`)
		case 2:
			var receipt struct{ Result graph.Result }
			if err := json.Unmarshal([]byte(scriptedToolReceipt(request, "audit-write")), &receipt); err != nil || len(receipt.Result.Subjects) != 2 {
				t.Error("write receipt", err)
				return
			}
			repo := receipt.Result.Subjects[0]
			args, _ := json.Marshal(graph.Batch{Subjects: []graph.SubjectInput{{ID: repo.ID, Version: repo.Version, Attrs: map[string]any{"credential": secretByID}}}})
			graphToolCall(w, request, "audit-by-id", ToolUpdateGraph, string(args))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	th, err := e.CreateThread("Audit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Record a repository.")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		v, err := e.ReadThreadGraph(t.Context(), th.ID, store.GraphQuery{All: true, Preview: true, Limit: -1})
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Todos) > 0 && v.Todos[0].Status == graph.TodoDoing {
			observed = true
			raw, _ := json.Marshal(v)
			if strings.Contains(string(raw), secret) {
				t.Error("embedded graph credential escaped through the preview")
			}
			if len(v.Subjects) != 2 {
				t.Errorf("preview should show both provisional subjects; got %d", len(v.Subjects))
			}
			filtered, err := e.ReadThreadGraph(t.Context(), th.ID, store.GraphQuery{All: true, Preview: true, Text: "no matching subject", Limit: 1})
			if err != nil || len(filtered.Subjects) != 0 || len(filtered.Todos) != 0 {
				t.Errorf("filtered snapshot absorbed the preview: subjects=%d todos=%d err=%v", len(filtered.Subjects), len(filtered.Todos), err)
			}
			committed, err := e.ReadThreadGraph(t.Context(), th.ID, store.GraphQuery{All: true, Limit: -1})
			if err != nil || committed.Revision != 0 || len(committed.Subjects) != 0 || len(committed.Todos) != 0 {
				t.Errorf("revision-only readers must not receive the preview: revision=%d subjects=%d todos=%d err=%v", committed.Revision, len(committed.Subjects), len(committed.Todos), err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed {
		t.Fatal("did not observe the in-progress preview")
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if strings.Contains(ev.Text, secret) || strings.Contains(ev.Text, secretByID) {
			t.Errorf("credential escaped through event %s (%s)", ev.Kind, ev.ToolCallID)
		}
	}
}

func TestGraphReplayPreservesNewUnrecordedResults(t *testing.T) {
	const body = `{"subjects":[{"kind":"ip","attrs":{"ip":"203.0.113.9"}}]}`
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			graphToolCall(w, request, "audit-repeat", ToolUpdateGraph, body)
		case 2:
			graphToolCall(w, request, "audit-unrelated", "exec", `{"command":"printf offline-audit"}`)
		case 3:
			graphToolCall(w, request, "audit-repeat", ToolUpdateGraph, body)
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	th, err := e.CreateThread("Replay audit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Record a finding.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawReplay, sawOrdinary bool
	for _, ev := range events {
		if ev.Kind != "tool_result" {
			continue
		}
		if ev.ToolCallID == "audit-unrelated" && !ev.WorkRecordPayload {
			sawOrdinary = true
		}
		if ev.ToolCallID == "audit-repeat" && strings.Contains(ev.Text, `"replayed":true`) {
			sawReplay = true
		}
	}
	if !sawReplay || !sawOrdinary {
		t.Fatalf("missing ordinary/replayed receipts: %v/%v", sawOrdinary, sawReplay)
	}
	n, err := e.store.GraphUnrecorded(t.Context(), th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("replayed write cleared a newer result; unrecorded=%d", n)
	}
}
