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
	node := store.NuwaNode{ProjectID: id, Kind: "fact", Title: "New observation", Summary: "A measured result.", ThreadID: thread.ID, TurnID: turn.ID, AgentID: "manager"}
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
