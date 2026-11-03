package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An already-open project must learn that an evidence source was erased even
// when the replacement turn does not publish any work or start any workers.
func TestNuwaRewindInvalidatesOpenProjectEvidence(t *testing.T) {
	// The existing offline peer-discovery script ends without spawning workers.
	t.Setenv("ZWAI_MOCK_TASK_COORDINATION", "1")
	h := newHarness(t)
	projectID := h.newProject(map[string]any{"name": "Shared work", "nuwa_enabled": true})["id"].(string)
	sourceID := createNuwaLifecycleThread(h, projectID, "Source")
	readerID := createNuwaLifecycleThread(h, projectID, "Reader")
	turnPath := "/api/threads/" + sourceID + "/turns"
	h.json(http.MethodPost, turnPath, map[string]any{"text": "Inspect the original source."}, http.StatusAccepted)
	original := h.waitTurnDone(sourceID)
	if original.Status != store.TurnDone {
		t.Fatalf("the original request did not complete: %+v", original)
	}
	cut := firstUserSeq(t, h.app.Store, sourceID)
	evidence := store.NuwaEvidence{ThreadID: sourceID, TurnID: original.ID, Seq: cut}
	reader := seedNuwaLifecycleTurn(t, h, projectID, readerID)
	node := store.NuwaNode{
		ProjectID: projectID, ThreadID: readerID, TurnID: reader.ID, AgentID: "manager",
		Kind: "fact", Title: "A retained observation", Evidence: []store.NuwaEvidence{evidence},
	}
	if err := h.app.Store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
		t.Fatal(err)
	}

	path := "/api/projects/" + projectID + "/nuwa"
	before := readNuwaLifecycleView(t, h, path)
	if len(before.Workers) != 0 || len(before.Nodes) != 1 || before.Nodes[0].ID != node.ID ||
		!reflect.DeepEqual(before.Nodes[0].Evidence, node.Evidence) || len(before.Nodes[0].UnavailableEvidence) != 0 {
		t.Fatalf("the initial snapshot must show the retained node with usable evidence and no workers: %+v", before)
	}
	next := openNuwaRevisionStream(t, h, path)
	if initial := next(); initial["project_id"] != projectID || initial["revision"] != float64(before.Revision) {
		t.Fatalf("the already-open project's initial revision: %v, want %d", initial, before.Revision)
	}

	h.json(http.MethodPost, turnPath,
		map[string]any{"text": "Inspect the replacement source.", "from_event_seq": cut}, http.StatusAccepted)
	replacement := h.waitTurnDone(sourceID)
	if replacement.ID == original.ID || replacement.Status != store.TurnDone {
		t.Fatalf("the replacement was not a successful new turn: %+v", replacement)
	}
	if _, err := h.app.Store.GetEvent(sourceID, cut); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rewind did not erase the referenced event %d: %v", cut, err)
	}
	after := readNuwaLifecycleView(t, h, path)
	if len(after.Workers) != 0 || len(after.Nodes) != 1 || after.Nodes[0].ID != node.ID ||
		!reflect.DeepEqual(after.Nodes[0].Evidence, node.Evidence) || !reflect.DeepEqual(after.Nodes[0].UnavailableEvidence, node.Evidence) {
		t.Fatalf("a fresh snapshot must keep the retained node and explicitly mark the erased reference unavailable: %+v", after)
	}
	if after.Revision <= before.Revision {
		t.Errorf("rewind erased a visible source without advancing the collaboration revision: before=%d after=%d", before.Revision, after.Revision)
	}
	if changed := next(); changed["project_id"] != projectID || changed["revision"] != float64(after.Revision) {
		t.Fatalf("the open project's invalidation does not match the durable revision %d: %v", after.Revision, changed)
	}
}

// A move preserves the source project's shared record while its author still
// exists. A later explicit deletion must clean up that author's content while
// keeping another conversation's reference visibly unavailable.
func TestNuwaDeleteMovedConversationCleansOriginalProject(t *testing.T) {
	for _, moveToProject := range []bool{true, false} {
		name := "standalone"
		if moveToProject {
			name = "another_project"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			projectID := h.newProject(map[string]any{"name": "Origin", "nuwa_enabled": true})["id"].(string)
			authorID := createNuwaLifecycleThread(h, projectID, "Author")
			readerID := createNuwaLifecycleThread(h, projectID, "Reader")
			author := seedNuwaLifecycleTurn(t, h, projectID, authorID)
			reader := seedNuwaLifecycleTurn(t, h, projectID, readerID)
			source := store.Event{ThreadID: authorID, TurnID: author.ID, Kind: engine.KindUser, AgentID: "manager", Text: "Source event."}
			if err := h.app.Store.AppendEvent(&source); err != nil {
				t.Fatal(err)
			}
			evidence := store.NuwaEvidence{ThreadID: authorID, TurnID: author.ID, Seq: source.Seq}
			authored := store.NuwaNode{
				ProjectID: projectID, ThreadID: authorID, TurnID: author.ID, AgentID: "manager",
				Kind: "fact", Title: "Authored observation", Evidence: []store.NuwaEvidence{evidence},
			}
			if err := h.app.Store.UpsertNuwaNode(t.Context(), &authored, 0); err != nil {
				t.Fatal(err)
			}
			retained := store.NuwaNode{
				ProjectID: projectID, ThreadID: readerID, TurnID: reader.ID, AgentID: "manager",
				Kind: "fact", Title: "Independent observation", Evidence: []store.NuwaEvidence{evidence},
				DependsOn: []string{authored.ID},
			}
			if err := h.app.Store.UpsertNuwaNode(t.Context(), &retained, 0); err != nil {
				t.Fatal(err)
			}

			destination := ""
			if moveToProject {
				destination = h.newProject(map[string]any{"name": "Destination", "nuwa_enabled": true})["id"].(string)
			}
			threadPath := "/api/threads/" + authorID
			h.json(http.MethodPatch, threadPath, map[string]any{"project_id": destination}, http.StatusOK)
			path := "/api/projects/" + projectID + "/nuwa"
			moved := readNuwaLifecycleView(t, h, path)
			if len(moved.Nodes) != 2 {
				t.Fatalf("moving a live author must retain both original project nodes: %+v", moved)
			}
			countVersions := func() int64 {
				t.Helper()
				var count int64
				if err := h.app.Store.DB().Model(&store.NuwaRevision{}).
					Where("project_id = ? AND node_id = ?", projectID, authored.ID).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				return count
			}
			if count := countVersions(); count == 0 {
				t.Fatal("the moved author's original private version is missing before deletion")
			}
			next := openNuwaRevisionStream(t, h, path)
			if initial := next(); initial["project_id"] != projectID || initial["revision"] != float64(moved.Revision) {
				t.Fatalf("initial revision for the open origin: %v, want %d", initial, moved.Revision)
			}

			h.json(http.MethodDelete, threadPath, nil, http.StatusNoContent)
			h.json(http.MethodGet, threadPath, nil, http.StatusNotFound)
			after := readNuwaLifecycleView(t, h, path)
			if after.Revision <= moved.Revision {
				t.Errorf("deleting a moved author did not invalidate the original project: before=%d after=%d", moved.Revision, after.Revision)
			}
			if len(after.Nodes) != 1 || after.Nodes[0].ID != retained.ID ||
				!reflect.DeepEqual(after.Nodes[0].DependsOn, retained.DependsOn) ||
				!reflect.DeepEqual(after.Nodes[0].MissingDependencies, retained.DependsOn) ||
				!reflect.DeepEqual(after.Nodes[0].Evidence, retained.Evidence) ||
				!reflect.DeepEqual(after.Nodes[0].UnavailableEvidence, retained.Evidence) {
				t.Errorf("the original project must drop the author's node and preserve explicit unavailable references on the other node: %+v", after)
			}
			if count := countVersions(); count != 0 {
				t.Errorf("the deleted author's private revisions survived in the original project: %d", count)
			}
			if changed := next(); changed["project_id"] != projectID || changed["revision"] != float64(after.Revision) {
				t.Fatalf("the origin's invalidation does not match the durable revision %d: %v", after.Revision, changed)
			}
		})
	}
}

func createNuwaLifecycleThread(h *harness, projectID, title string) string {
	h.t.Helper()
	created := h.json(http.MethodPost, "/api/threads",
		map[string]any{"project_id": projectID, "title": title}, http.StatusCreated)
	return created["thread"].(map[string]any)["id"].(string)
}

func seedNuwaLifecycleTurn(t *testing.T, h *harness, projectID, threadID string) store.Turn {
	t.Helper()
	turn := store.Turn{ProjectID: projectID, ThreadID: threadID, Status: store.TurnDone, NuwaEnabled: true}
	if err := h.app.Store.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	return turn
}

func readNuwaLifecycleView(t *testing.T, h *harness, path string) engine.NuwaProjectView {
	t.Helper()
	resp := h.do(http.MethodGet, path, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	var view engine.NuwaProjectView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("GET %s: decode collaboration snapshot: %v", path, err)
	}
	return view
}
