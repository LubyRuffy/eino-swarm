package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func (h *harness) newProject(body map[string]any) map[string]any {
	h.t.Helper()
	got := h.json(http.MethodPost, "/api/projects", body, http.StatusCreated)
	return got["project"].(map[string]any)
}

// raw sends a body that is not valid JSON, which h.json cannot express
// because it encodes whatever it is given.
func (h *harness) raw(method, path, body string) int {
	h.t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// The whole project surface in one pass: create, list, read, patch, delete.
func TestProjectRoutesRoundTrip(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{
		"name": "Work", "system_prompt": "the project's own instruction",
	})
	id := p["id"].(string)
	if p["name"] != "Work" || p["system_prompt"] != "the project's own instruction" {
		t.Fatalf("project=%v", p)
	}
	// The UI shows where the work happens and where the notes live without
	// having to derive either path itself.
	if p["resolved_workdir"] == "" || p["memory_dir"] == "" {
		t.Fatalf("project view does not say where its files are: %v", p)
	}
	if p["workdir"] != "" {
		t.Fatalf("a project that named no directory must report none: %v", p["workdir"])
	}
	if p["memory_enabled"] != true {
		t.Fatalf("a project should default to the configured memory setting: %v", p)
	}
	if _, ok := p["skills"].([]any); !ok {
		t.Fatalf("the sidebar iterates skills, so they must arrive as a list: %v", p["skills"])
	}

	list := h.json(http.MethodGet, "/api/projects", nil, http.StatusOK)["projects"].([]any)
	if len(list) != 1 {
		t.Fatalf("projects=%v", list)
	}
	got := h.json(http.MethodGet, "/api/projects/"+id, nil, http.StatusOK)["project"].(map[string]any)
	if got["id"] != id {
		t.Fatalf("project=%v", got)
	}

	patched := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{
		"name": "Renamed", "memory_enabled": false,
	}, http.StatusOK)["project"].(map[string]any)
	if patched["name"] != "Renamed" || patched["memory_enabled"] != false {
		t.Fatalf("patch=%v", patched)
	}
	if patched["system_prompt"] != "the project's own instruction" {
		t.Fatalf("a field nobody sent was changed: %v", patched)
	}

	h.json(http.MethodDelete, "/api/projects/"+id, nil, http.StatusNoContent)
	h.json(http.MethodGet, "/api/projects/"+id, nil, http.StatusNotFound)
}

// A bad working directory has to point at the field that is wrong. Without the
// code the dialog can only show the message somewhere the user has to hunt.
func TestABadWorkingDirectoryIsReportedAgainstItsField(t *testing.T) {
	h := newHarness(t)
	for name, dir := range map[string]string{
		"relative":  "relative/path",
		"not there": filepath.Join(t.TempDir(), "does-not-exist"),
	} {
		got := h.json(http.MethodPost, "/api/projects",
			map[string]any{"name": "P", "workdir": dir}, http.StatusBadRequest)
		if got["code"] != "workdir" {
			t.Fatalf("%s: the refusal must name the field: %v", name, got)
		}
	}

	p := h.newProject(map[string]any{"name": "P"})
	got := h.json(http.MethodPatch, "/api/projects/"+p["id"].(string),
		map[string]any{"workdir": "still/relative"}, http.StatusBadRequest)
	if got["code"] != "workdir" {
		t.Fatalf("patch: %v", got)
	}
}

func TestProjectRoutesRefuseWhatTheyShould(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodGet, "/api/projects/pj_missing", nil, http.StatusNotFound)
	h.json(http.MethodPatch, "/api/projects/pj_missing", map[string]any{"name": "x"}, http.StatusNotFound)
	h.json(http.MethodDelete, "/api/projects/pj_missing", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/pj_missing/memory", nil, http.StatusNotFound)
	h.json(http.MethodPut, "/api/projects/pj_missing/memory", map[string]any{"text": "x"}, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/pj_missing/skills/anything", nil, http.StatusNotFound)

	// A project needs a name, and a body that is not JSON is a bad request
	// rather than a nameless project.
	h.json(http.MethodPost, "/api/projects", map[string]any{"name": "  "}, http.StatusBadRequest)
	resp := h.raw(http.MethodPost, "/api/projects", "{not json")
	if resp != http.StatusBadRequest {
		t.Fatalf("a malformed body=%d", resp)
	}
	p := h.newProject(map[string]any{"name": "P"})
	if resp := h.raw(http.MethodPatch, "/api/projects/"+p["id"].(string), "{not json"); resp != http.StatusBadRequest {
		t.Fatalf("a malformed patch=%d", resp)
	}
	if resp := h.raw(http.MethodPut, "/api/projects/"+p["id"].(string)+"/memory", "{not json"); resp != http.StatusBadRequest {
		t.Fatalf("a malformed memory body=%d", resp)
	}
}

// The sidebar filters by project, and a conversation in a project reports it
// so the header can show which one it belongs to.
func TestThreadsCarryTheirProjectAndCanBeFilteredByIt(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)

	created := h.json(http.MethodPost, "/api/threads",
		map[string]any{"project_id": id}, http.StatusCreated)["thread"].(map[string]any)
	if created["project_id"] != id {
		t.Fatalf("thread=%v", created)
	}
	loose := h.newThread()

	filtered := h.json(http.MethodGet, "/api/threads?project="+id, nil, http.StatusOK)["threads"].([]any)
	if len(filtered) != 1 || filtered[0].(map[string]any)["id"] != created["id"] {
		t.Fatalf("filtered=%v", filtered)
	}
	all := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)["threads"].([]any)
	if len(all) != 2 {
		t.Fatalf("unfiltered=%v", all)
	}

	// Moving a conversation in and out of a project goes through the same
	// patch the rest of its settings do.
	moved := h.json(http.MethodPatch, "/api/threads/"+loose,
		map[string]any{"project_id": id}, http.StatusOK)["thread"].(map[string]any)
	if moved["project_id"] != id {
		t.Fatalf("moved=%v", moved)
	}
	out := h.json(http.MethodPatch, "/api/threads/"+loose,
		map[string]any{"project_id": ""}, http.StatusOK)["thread"].(map[string]any)
	if out["project_id"] != "" {
		t.Fatalf("moved out=%v", out)
	}
	h.json(http.MethodPatch, "/api/threads/"+loose,
		map[string]any{"project_id": "pj_missing"}, http.StatusNotFound)

	// A conversation created in a project that is not there is a 404, not a
	// conversation anchored somewhere unexpected.
	h.json(http.MethodPost, "/api/threads", map[string]any{"project_id": "pj_missing"}, http.StatusNotFound)
}

// Deleting a project takes its conversations. The confirm dialog says so, so
// the API had better do it.
func TestDeletingAProjectTakesItsConversations(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)
	th := h.json(http.MethodPost, "/api/threads",
		map[string]any{"project_id": id}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)

	h.json(http.MethodDelete, "/api/projects/"+id, nil, http.StatusNoContent)
	h.json(http.MethodGet, "/api/threads/"+th, nil, http.StatusNotFound)
}

// The Memory panel reads this, edits it by hand, and deletes a skill it no
// longer wants.
func TestMemoryRoutesReadEditAndPrune(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)

	got := h.json(http.MethodGet, "/api/projects/"+id+"/memory", nil, http.StatusOK)["memory"].(map[string]any)
	if got["dir"] == "" || got["enabled"] != true {
		t.Fatalf("memory=%v", got)
	}
	// The panel iterates the skills, so an empty store must send a list and
	// never JSON null.
	if _, ok := got["skills"].([]any); !ok {
		t.Fatalf("skills must arrive as a list: %v", got["skills"])
	}
	snap := got["memory"].(map[string]any)
	if snap["limit"].(float64) <= 0 {
		t.Fatalf("the panel shows a budget, so the limit must be reported: %v", snap)
	}
	if snap["rev"] == "" {
		t.Fatalf("the editor needs a revision to send back: %v", snap)
	}

	edited := h.json(http.MethodPut, "/api/projects/"+id+"/memory",
		map[string]any{"text": "a note typed by hand\n\nand a second one"}, http.StatusOK)["memory"].(map[string]any)
	entries := edited["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("hand edit=%v", edited)
	}
	// A hand edit that no longer fits in a prompt is the same problem whoever
	// typed it, so it is refused rather than truncated.
	h.json(http.MethodPut, "/api/projects/"+id+"/memory",
		map[string]any{"text": strings.Repeat("x", 100000)}, http.StatusBadRequest)

	// Skills: a name nobody wrote is missing, a name that could never be one
	// is a bad request, and a real one reads and deletes.
	h.json(http.MethodGet, "/api/projects/"+id+"/skills/not-written", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/"+id+"/skills/Not%20A%20Name", nil, http.StatusBadRequest)
	h.json(http.MethodDelete, "/api/projects/"+id+"/skills/not-written", nil, http.StatusNotFound)

	if _, err := h.app.Engine.ProjectMemory(id).WriteSkill("a-procedure", "when it applies", "steps"); err != nil {
		t.Fatal(err)
	}
	// The sidebar lists skills under the project, so the project list itself
	// has to carry them rather than making the UI round-trip per row.
	listed := h.json(http.MethodGet, "/api/projects", nil, http.StatusOK)["projects"].([]any)
	var listedSkills []any
	for _, raw := range listed {
		row := raw.(map[string]any)
		if row["id"] == id {
			listedSkills, _ = row["skills"].([]any)
		}
	}
	if len(listedSkills) != 1 || listedSkills[0].(map[string]any)["name"] != "a-procedure" {
		t.Fatalf("project list did not carry the skill: %v", listedSkills)
	}
	skill := h.json(http.MethodGet, "/api/projects/"+id+"/skills/a-procedure", nil, http.StatusOK)["skill"].(map[string]any)
	if skill["name"] != "a-procedure" || !strings.Contains(skill["body"].(string), "steps") {
		t.Fatalf("skill=%v", skill)
	}
	h.json(http.MethodDelete, "/api/projects/"+id+"/skills/a-procedure", nil, http.StatusNoContent)
	h.json(http.MethodGet, "/api/projects/"+id+"/skills/a-procedure", nil, http.StatusNotFound)
}

// The panel and a review write the same file. The save that lands second
// without noticing would erase the other, and the lost note is in no
// transcript.
func TestSavingStaleNotesIsRefusedWithWhatIsStoredNow(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)

	loaded := h.json(http.MethodGet, "/api/projects/"+id+"/memory", nil, http.StatusOK)["memory"].(map[string]any)
	rev := loaded["memory"].(map[string]any)["rev"].(string)
	if rev == "" {
		t.Fatal("a snapshot has to identify itself for an editor to send it back")
	}

	if _, _, err := h.app.Engine.ProjectMemory(id).Add("what the review stored"); err != nil {
		t.Fatal(err)
	}

	got := h.json(http.MethodPut, "/api/projects/"+id+"/memory",
		map[string]any{"text": "the hand edit", "rev": rev}, http.StatusConflict)
	if got["code"] != "conflict" {
		t.Fatalf("a stale write must name itself a conflict: %v", got)
	}
	current := got["memory"].(map[string]any)
	if !strings.Contains(current["text"].(string), "what the review stored") {
		t.Fatalf("the refusal must carry what is stored now: %v", current)
	}

	// Retried against the current revision, it goes through.
	h.json(http.MethodPut, "/api/projects/"+id+"/memory",
		map[string]any{"text": "the hand edit", "rev": current["rev"]}, http.StatusOK)
}

// A project whose memory the process cannot read must fail loudly on the
// panel rather than showing an empty store that looks like nothing was learned.
func TestAnUnreadableMemoryStoreIsReportedNotHidden(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)
	dir := h.app.Engine.ProjectMemory(id).Dir()
	if err := os.MkdirAll(filepath.Join(dir, "MEMORY.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/projects/"+id+"/memory", nil, http.StatusBadRequest)

	if err := os.RemoveAll(filepath.Join(dir, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/projects/"+id+"/memory", nil, http.StatusBadRequest)

	// The sidebar still has to list the project: hiding every project
	// because one skills directory is broken would be the worse failure.
	listed := h.json(http.MethodGet, "/api/projects", nil, http.StatusOK)["projects"].([]any)
	if len(listed) != 1 {
		t.Fatalf("projects=%v", listed)
	}
	skills, ok := listed[0].(map[string]any)["skills"].([]any)
	if !ok || len(skills) != 0 {
		t.Fatalf("a broken skills dir must arrive as an empty list: %v", listed[0])
	}
}

// The end of the path a user actually walks: a turn in a project runs, the
// review curates memory on its own, and both the notes and the review event
// are reachable through the turn's id.
func TestATurnInAProjectIsReviewedAndTraceable(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)
	th := h.json(http.MethodPost, "/api/threads",
		map[string]any{"project_id": id}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)

	h.json(http.MethodPost, "/api/threads/"+th+"/turns",
		map[string]any{"text": "a request for the swarm"}, http.StatusAccepted)
	turn := h.waitTurnDone(th)
	if turn.Status != store.TurnDone {
		t.Fatalf("turn=%+v", turn)
	}
	h.waitForReviewEvent(turn.ID)

	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	raw, _ := trace["events"].([]any)
	found := false
	for _, item := range raw {
		ev := item.(map[string]any)
		if ev["kind"] == engine.KindMemoryReview && ev["agent_id"] == engine.ReviewAgentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the review is not in the turn's trace: %v", raw)
	}

	mem := h.json(http.MethodGet, "/api/projects/"+id+"/memory", nil, http.StatusOK)["memory"].(map[string]any)
	if entries := mem["memory"].(map[string]any)["entries"].([]any); len(entries) == 0 {
		t.Fatalf("the review stored nothing: %v", mem)
	}
	if skills := mem["skills"].([]any); len(skills) == 0 {
		t.Fatalf("the review recorded no skill: %v", mem)
	}
}

// "Review now" re-reads a finished conversation. Nothing to review is a 409
// with a code, the same shape the UI already handles for busy and idle.
func TestReviewNowAcceptsOrExplainsItself(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "P"})
	id := p["id"].(string)
	th := h.json(http.MethodPost, "/api/threads",
		map[string]any{"project_id": id}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)

	// Nothing has run yet.
	got := h.json(http.MethodPost, "/api/threads/"+th+"/review", nil, http.StatusConflict)
	if got["code"] != "idle" {
		t.Fatalf("review with nothing to review=%v", got)
	}

	h.json(http.MethodPost, "/api/threads/"+th+"/turns",
		map[string]any{"text": "a request"}, http.StatusAccepted)
	turn := h.waitTurnDone(th)
	h.waitForReviewEvent(turn.ID)

	accepted := h.json(http.MethodPost, "/api/threads/"+th+"/review", nil, http.StatusAccepted)
	if accepted["turn"].(map[string]any)["id"] != turn.ID {
		t.Fatalf("review now=%v", accepted)
	}

	// A conversation in no project has no memory to curate.
	loose := h.newThread()
	if got := h.json(http.MethodPost, "/api/threads/"+loose+"/review", nil, http.StatusConflict); got["code"] != "idle" {
		t.Fatalf("review of a conversation in no project=%v", got)
	}
	h.json(http.MethodPost, "/api/threads/th_missing/review", nil, http.StatusNotFound)
}

// Memory is configuration like everything else: editable in Settings, written
// to the file, and never hardcoded.
func TestMemorySettingsRoundTrip(t *testing.T) {
	h := newHarness(t)
	got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)
	mem, ok := got["memory"].(map[string]any)
	if !ok {
		t.Fatalf("settings do not include memory: %v", got)
	}
	if mem["char_limit"].(float64) <= 0 {
		t.Fatalf("memory settings=%v", mem)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"memory": map[string]any{
			"enabled": true, "auto_review": false,
			"char_limit": 900, "review_max_iterations": 3, "skills_index_max": 7,
			"notifications": "verbose",
		},
	}, http.StatusOK)
	reread := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["memory"].(map[string]any)
	if reread["auto_review"] != false || reread["char_limit"].(float64) != 900 ||
		reread["review_max_iterations"].(float64) != 3 || reread["skills_index_max"].(float64) != 7 ||
		reread["notifications"] != "verbose" {
		t.Fatalf("memory settings not persisted: %v", reread)
	}
}

func (h *harness) waitForReviewEvent(turnID string) {
	h.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		events, err := h.app.Store.ListTurnEvents(turnID)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, ev := range events {
			if ev.Kind == engine.KindMemoryReview {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("turn %s was never reviewed", turnID)
}

func (h *harness) waitForTitleEvent(turnID string) {
	h.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		events, err := h.app.Store.ListTurnEvents(turnID)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, ev := range events {
			if ev.Kind == engine.KindTitle {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("turn %s was never named", turnID)
}
