package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A project can change in another conversation while this page is open. The
// named subscription must invalidate its snapshot without a history/Trace read.
func TestNuwaProjectSSEPublishesCommittedRevisionsAcrossConversations(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "Shared work", "nuwa_enabled": true})
	id := p["id"].(string)
	path := "/api/projects/" + id + "/nuwa"
	initial := h.json(http.MethodGet, path, nil, http.StatusOK)
	if initial["revision"] != float64(0) || len(initial["nodes"].([]any)) != 0 || len(initial["workers"].([]any)) != 0 {
		t.Fatalf("initial snapshot=%v", initial)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+path+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := h.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream status=%d headers=%v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	next := func() map[string]any {
		t.Helper()
		name := ""
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "event:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") && name == "nuwa_updated" {
				var value map[string]any
				if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
		}
	}
	if ready := next(); ready["project_id"] != id || ready["revision"] != float64(0) {
		t.Fatalf("ready=%v", ready)
	}
	thread, err := h.app.Engine.CreateThread("Another conversation", "", id)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, NuwaEnabled: true}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	source, err := h.app.Engine.CreateThread("Evidence source", "", id)
	if err != nil {
		t.Fatal(err)
	}
	sourceEvent := store.Event{ThreadID: source.ID, Kind: "agent_message", AgentID: "manager", Text: "Measured result."}
	if err := h.app.Store.AppendEvent(&sourceEvent); err != nil {
		t.Fatal(err)
	}
	evidence := store.NuwaEvidence{ThreadID: source.ID, Seq: sourceEvent.Seq}
	node := store.NuwaNode{ProjectID: id, Kind: "fact", Title: "New observation", Summary: "A measured result.", ThreadID: thread.ID, TurnID: turn.ID, AgentID: "manager", Evidence: []store.NuwaEvidence{evidence}}
	if err := h.app.Store.UpsertNuwaNode(ctx, &node, 0); err != nil {
		t.Fatal(err)
	}
	if change := next(); change["project_id"] != id || change["revision"] != float64(1) {
		t.Fatalf("live revision=%v", change)
	}
	snapshot := h.json(http.MethodGet, path, nil, http.StatusOK)
	nodes := snapshot["nodes"].([]any)
	if snapshot["revision"] != float64(1) || len(nodes) != 1 || nodes[0].(map[string]any)["id"] != node.ID {
		t.Fatalf("snapshot after named event=%v", snapshot)
	}
	if nodes[0].(map[string]any)["unavailable_evidence"] != nil {
		t.Fatalf("the original source should be available: %v", nodes[0])
	}
	// Moving a different conversation must update the currently open project
	// even though its shared record and the evidence event both still exist.
	destination := h.newProject(map[string]any{"name": "Destination", "nuwa_enabled": true})["id"].(string)
	movePath := "/api/threads/" + source.ID
	h.json(http.MethodPatch, movePath, map[string]any{"project_id": destination}, http.StatusOK)
	afterMove := h.json(http.MethodGet, path, nil, http.StatusOK)
	movedNode := afterMove["nodes"].([]any)[0].(map[string]any)
	unavailable, _ := movedNode["unavailable_evidence"].([]any)
	if afterMove["revision"] != float64(2) || movedNode["id"] != node.ID || len(unavailable) != 1 {
		t.Fatalf("moving the evidence source must invalidate the open snapshot: %v", afterMove)
	}
	if change := next(); change["project_id"] != id || change["revision"] != afterMove["revision"] {
		t.Fatalf("move invalidation=%v", change)
	}
	destinationPath := "/api/projects/" + destination + "/nuwa"
	if to := h.json(http.MethodGet, destinationPath, nil, http.StatusOK); to["revision"] != float64(1) || len(to["nodes"].([]any)) != 0 {
		t.Fatalf("destination did not get its own invalidation: %v", to)
	}
	h.json(http.MethodPatch, movePath, map[string]any{"project_id": destination}, http.StatusOK)
	if repeated := h.json(http.MethodGet, path, nil, http.StatusOK); repeated["revision"] != afterMove["revision"] {
		t.Fatalf("repeating a move changed the original project again: %v", repeated)
	}
	h.json(http.MethodPatch, movePath, map[string]any{"project_id": id}, http.StatusOK)
	if change := next(); change["project_id"] != id || change["revision"] != float64(3) {
		t.Fatalf("move-back invalidation=%v", change)
	}
	if back := h.json(http.MethodGet, path, nil, http.StatusOK); back["nodes"].([]any)[0].(map[string]any)["unavailable_evidence"] != nil {
		t.Fatalf("move back did not restore the evidence link: %v", back)
	}
	h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{"nuwa_enabled": false}, http.StatusOK)
	if after := h.json(http.MethodGet, path, nil, http.StatusOK); len(after["nodes"].([]any)) != 1 {
		t.Fatalf("disabling erased shared history: %v", after)
	}
	cancel()
}

func TestNuwaReadRoutesRefuseMissingProjects(t *testing.T) {
	h := newHarness(t)
	for _, suffix := range []string{"", "/events"} {
		h.json(http.MethodGet, "/api/projects/pj_missing/nuwa"+suffix, nil, http.StatusNotFound)
	}
}
