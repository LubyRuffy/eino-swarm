package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A conversation outside a project still records a procedure, in the shared
// library, and that procedure can be copied into a project afterwards.
func TestALooseConversationFillsTheLibraryAndCanBeCopied(t *testing.T) {
	h := newHarness(t)
	th := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+th+"/turns",
		map[string]any{"text": "a request for the swarm"}, http.StatusAccepted)
	turn := h.waitTurnDone(th)
	if turn.Status != store.TurnDone {
		t.Fatalf("turn status=%q", turn.Status)
	}
	h.waitForReviewEvent(turn.ID)

	view := h.json(http.MethodGet, "/api/library", nil, http.StatusOK)["memory"].(map[string]any)
	if view["enabled"] != true {
		t.Fatalf("library view=%v", view)
	}
	dir, _ := view["dir"].(string)
	if !strings.HasSuffix(dir, "library") {
		t.Fatalf("dir=%q", dir)
	}
	skills := view["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("skills=%v", skills)
	}
	name := skills[0].(map[string]any)["name"].(string)
	body := h.json(http.MethodGet, "/api/library/skills/"+name, nil, http.StatusOK)["skill"].(map[string]any)
	if body["body"] == "" {
		t.Fatalf("skill=%v", body)
	}

	dest := h.newProject(map[string]any{"name": "Dest"})
	copied := h.json(http.MethodPost, "/api/library/skills/copy",
		map[string]any{"to_project": dest["id"], "names": []string{name}}, http.StatusOK)
	hits := copied["copied"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["project_id"] != dest["id"] {
		t.Fatalf("copied=%v", copied)
	}

	got := h.json(http.MethodGet, "/api/projects/"+dest["id"].(string)+"/skills/"+name, nil, http.StatusOK)
	origin := got["skill"].(map[string]any)["origin"].(map[string]any)
	if origin["project_id"] != memory.LibraryID || origin["status"] != memory.OriginCurrent {
		t.Fatalf("origin=%v", origin)
	}
	// project_name is omitted when empty. A library origin is not a project row.
	if name, _ := origin["project_name"].(string); name != "" {
		t.Fatalf("the library is not a project name: %v", origin)
	}

	// A later edit of the library is an update the copy can pull. The
	// library file is the source; the project file stays put until then.
	skill, err := h.app.Engine.LibraryMemory().ReadSkill(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Engine.LibraryMemory().WriteSkill(name, skill.Description, skill.Body+"\n4. Stop.\n"); err != nil {
		t.Fatal(err)
	}
	again := h.json(http.MethodGet, "/api/projects/"+dest["id"].(string)+"/skills/"+name, nil, http.StatusOK)
	if again["skill"].(map[string]any)["origin"].(map[string]any)["status"] != memory.OriginUpdate {
		t.Fatalf("after the library moved, origin=%v", again["skill"].(map[string]any)["origin"])
	}
	pulled := h.json(http.MethodPost, "/api/projects/"+dest["id"].(string)+"/skills/"+name+"/pull",
		map[string]any{}, http.StatusOK)
	if !strings.Contains(pulled["skill"].(map[string]any)["body"].(string), "Stop") {
		t.Fatalf("pull did not take the library text: %v", pulled["skill"])
	}
}

func TestLibraryRoutesRefuseAMissingSkillAndAnEmptyCopy(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodGet, "/api/library/skills/missing", nil, http.StatusNotFound)
	h.json(http.MethodDelete, "/api/library/skills/missing", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/library/skills/Nope", nil, http.StatusBadRequest)

	dest := h.newProject(map[string]any{"name": "Dest"})
	got := h.json(http.MethodPost, "/api/library/skills/copy",
		map[string]any{"to_project": dest["id"]}, http.StatusBadRequest)
	if !strings.Contains(got["error"].(string), "shared library") {
		t.Fatalf("empty copy=%v", got)
	}
}

func TestLibraryTidyAndDelete(t *testing.T) {
	h := newHarness(t)
	dir := h.app.Engine.LibraryMemory().Dir()
	plantProjectSkill(t, dir, "alpha-prep", "when preparing", "1. prepare")

	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/api/library/tidy-skills", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	body := string(raw)
	if !strings.Contains(body, `"phase":"scan"`) || !strings.Contains(body, "event: done") {
		t.Fatalf("stream=%s", body)
	}

	h.json(http.MethodDelete, "/api/library/skills/alpha-prep", nil, http.StatusNoContent)
	h.json(http.MethodGet, "/api/library/skills/alpha-prep", nil, http.StatusNotFound)
	view := h.json(http.MethodGet, "/api/library", nil, http.StatusOK)["memory"].(map[string]any)
	skills, _ := view["skills"].([]any)
	if len(skills) != 0 {
		t.Fatalf("skills after delete=%v", skills)
	}
}

func TestLibraryTidyRefusesAfterShutdown(t *testing.T) {
	h := newHarness(t)
	h.app.Engine.Shutdown()
	got := h.json(http.MethodPost, "/api/library/tidy-skills", nil, http.StatusConflict)
	if got["code"] != "idle" {
		t.Fatalf("tidy after shutdown=%v", got)
	}
}
