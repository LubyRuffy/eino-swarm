package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

func skillOrigin(t *testing.T, skill map[string]any) map[string]any {
	t.Helper()
	origin, ok := skill["origin"].(map[string]any)
	if !ok {
		t.Fatalf("skill has no copy link: %v", skill)
	}
	return origin
}

func (h *harness) readSkill(projectID, name string) map[string]any {
	h.t.Helper()
	return h.json(http.MethodGet, "/api/projects/"+projectID+"/skills/"+name, nil, http.StatusOK)["skill"].(map[string]any)
}

// A business project has to be able to reuse a procedure and still change it
// without rewriting the project the steps came from. The reverse matters too:
// a later edit of the shared procedure must not silently replace that change.
func TestCopiedSkillStaysIndependentAndCanStillTakeAnUpdate(t *testing.T) {
	h := newHarness(t)
	library := h.newProject(map[string]any{"name": "Library"})
	work := h.newProject(map[string]any{"name": "Work"})
	third := h.newProject(map[string]any{"name": "Third"})
	libID := library["id"].(string)
	workID := work["id"].(string)
	thirdID := third["id"].(string)

	if _, err := h.app.Engine.ProjectMemory(libID).WriteSkill(
		"release-check", "when cutting a release", "1. run the checks",
	); err != nil {
		t.Fatal(err)
	}

	copied := h.json(http.MethodPost, "/api/projects/"+libID+"/skills/copy", map[string]any{
		"to_project": workID,
		"names":      []string{"release-check"},
	}, http.StatusOK)
	hits := copied["copied"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["name"] != "release-check" {
		t.Fatalf("copied=%v", copied)
	}

	got := h.readSkill(workID, "release-check")
	origin := skillOrigin(t, got)
	if origin["project_id"] != libID || origin["project_name"] != "Library" || origin["status"] != memory.OriginCurrent {
		t.Fatalf("origin=%v", origin)
	}
	if strings.Contains(got["body"].(string), "origin_project") {
		t.Fatalf("the opened skill included the copy link: %s", got["body"])
	}

	// The middle project has not edited the text, so the next copy still
	// tracks the library. An edit in Work must not become Third's source.
	h.json(http.MethodPost, "/api/projects/"+workID+"/skills/copy", map[string]any{
		"to_project": thirdID,
		"names":      []string{"release-check"},
	}, http.StatusOK)
	thirdSkill := h.readSkill(thirdID, "release-check")
	thirdOrigin := skillOrigin(t, thirdSkill)
	if thirdOrigin["project_id"] != libID || thirdOrigin["status"] != memory.OriginCurrent {
		t.Fatalf("third origin=%v", thirdOrigin)
	}

	if _, err := h.app.Engine.ProjectMemory(workID).PatchSkill("release-check", "run the checks", "run the local checks"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.readSkill(thirdID, "release-check")["body"].(string), "local") {
		t.Fatal("an edit in the middle project changed a copy that tracks the library")
	}
	if strings.Contains(h.readSkill(libID, "release-check")["body"].(string), "local") {
		t.Fatal("an edit in one project rewrote the library")
	}

	if _, err := h.app.Engine.ProjectMemory(libID).WriteSkill(
		"release-check", "when cutting a release", "1. run the checks\n2. publish",
	); err != nil {
		t.Fatal(err)
	}
	waiting := skillOrigin(t, h.readSkill(thirdID, "release-check"))
	if waiting["status"] != memory.OriginUpdate {
		t.Fatalf("a library change did not show as an update: %v", waiting)
	}
	pulled := h.json(http.MethodPost, "/api/projects/"+thirdID+"/skills/release-check/pull", map[string]any{}, http.StatusOK)["skill"].(map[string]any)
	if !strings.Contains(pulled["body"].(string), "publish") || strings.Contains(pulled["body"].(string), "local") {
		t.Fatalf("pull=%s", pulled["body"])
	}
	if skillOrigin(t, pulled)["status"] != memory.OriginCurrent {
		t.Fatalf("after pull origin=%v", skillOrigin(t, pulled))
	}
	if strings.Contains(h.readSkill(libID, "release-check")["body"].(string), "local") {
		t.Fatal("pull wrote the copy back onto the library")
	}

	if _, err := h.app.Engine.ProjectMemory(thirdID).PatchSkill("release-check", "publish", "publish locally"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Engine.ProjectMemory(libID).WriteSkill(
		"release-check", "when cutting a release", "1. run the checks\n2. publish widely",
	); err != nil {
		t.Fatal(err)
	}
	refused := h.json(http.MethodPost, "/api/projects/"+thirdID+"/skills/release-check/pull", map[string]any{}, http.StatusConflict)
	if refused["code"] != memory.OriginDiverged {
		t.Fatalf("both sides changed and the pull was not refused: %v", refused)
	}
	if !strings.Contains(h.readSkill(thirdID, "release-check")["body"].(string), "locally") {
		t.Fatal("a refused pull replaced the local edit")
	}
	forced := h.json(http.MethodPost, "/api/projects/"+thirdID+"/skills/release-check/pull", map[string]any{"force": true}, http.StatusOK)["skill"].(map[string]any)
	if !strings.Contains(forced["body"].(string), "widely") || strings.Contains(forced["body"].(string), "locally") {
		t.Fatalf("force pull=%s", forced["body"])
	}
	if strings.Contains(h.readSkill(libID, "release-check")["body"].(string), "locally") {
		t.Fatal("force pull wrote back to the library")
	}
}

func TestCopySkillsReportsWhatDidNotFit(t *testing.T) {
	h := newHarness(t)
	src := h.newProject(map[string]any{"name": "Library"})
	dst := h.newProject(map[string]any{"name": "Work"})
	srcID := src["id"].(string)
	dstID := dst["id"].(string)
	mem := h.app.Engine.ProjectMemory(srcID)
	if _, err := mem.WriteSkill("release-check", "when tagging after the verification checks have passed", "1. run the checks\n2. tag"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.WriteSkill("shell-dispatch", "when a command has to run outside the conversation", "1. run it\n2. read the output"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Engine.ProjectMemory(dstID).WriteSkill("release-check", "when tagging after the verification checks have passed", "1. run the checks\n2. tag"); err != nil {
		t.Fatal(err)
	}

	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": srcID,
	}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": "pj_missing",
		"names":      []string{"release-check"},
	}, http.StatusNotFound)
	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
		"names":      []string{"not-written"},
	}, http.StatusNotFound)
	exists := h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
		"names":      []string{"release-check"},
	}, http.StatusConflict)
	if exists["code"] != "exists" {
		t.Fatalf("second copy=%v", exists)
	}

	report := h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
	}, http.StatusOK)
	skipped := report["skipped"].([]any)
	copied := report["copied"].([]any)
	if len(copied) != 1 || copied[0].(map[string]any)["name"] != "shell-dispatch" {
		t.Fatalf("copied=%v", report)
	}
	if len(skipped) != 1 || skipped[0].(map[string]any)["code"] != "exists" {
		t.Fatalf("skipped=%v", report)
	}

	aliasProject := h.newProject(map[string]any{"name": "Alias"})
	aliasID := aliasProject["id"].(string)
	renamed := h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": aliasID,
		"names":      []string{"release-check"},
		"as":         "release-verify",
	}, http.StatusOK)
	if renamed["copied"].([]any)[0].(map[string]any)["name"] != "release-verify" {
		t.Fatalf("renamed=%v", renamed)
	}
	alias := h.readSkill(aliasID, "release-verify")
	if skillOrigin(t, alias)["name"] != "release-check" {
		t.Fatalf("renamed copy lost the upstream name: %v", alias["origin"])
	}

	bare := h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/shell-dispatch/pull", map[string]any{}, http.StatusOK)
	if bare["skill"].(map[string]any)["name"] != "shell-dispatch" {
		t.Fatalf("pull of an unchanged copy=%v", bare)
	}
	unlinked := h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/release-check/pull", map[string]any{}, http.StatusBadRequest)
	if unlinked["code"] != "unlinked" {
		t.Fatalf("a skill that was not copied should not pull: %v", unlinked)
	}

	if err := h.app.Engine.DeleteProject(srcID); err != nil {
		t.Fatal(err)
	}
	gone := skillOrigin(t, h.readSkill(dstID, "shell-dispatch"))
	if gone["status"] != memory.OriginMissing {
		t.Fatalf("deleted source still looked available: %v", gone)
	}
	h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/shell-dispatch/pull", map[string]any{}, http.StatusNotFound)
}

func TestCopySkipsWhatDoesNotFitAndRetargetsABrokenLink(t *testing.T) {
	h := newHarness(t)
	empty := h.newProject(map[string]any{"name": "Empty"})
	h.json(http.MethodGet, "/api/projects/"+empty["id"].(string)+"/memory", nil, http.StatusOK)
	h.json(http.MethodPost, "/api/projects/"+empty["id"].(string)+"/skills/copy", map[string]any{
		"to_project": h.newProject(map[string]any{"name": "Other"})["id"],
	}, http.StatusBadRequest)
	if h.raw(http.MethodPost, "/api/projects/"+empty["id"].(string)+"/skills/copy", "{") != http.StatusBadRequest {
		t.Fatal("a broken copy body was accepted")
	}
	h.json(http.MethodPost, "/api/projects/pj_missing/skills/copy", map[string]any{
		"to_project": empty["id"], "names": []string{"release-check"},
	}, http.StatusNotFound)
	h.json(http.MethodPost, "/api/projects/"+empty["id"].(string)+"/skills/missing/pull", map[string]any{}, http.StatusNotFound)
	if h.raw(http.MethodPost, "/api/projects/"+empty["id"].(string)+"/skills/missing/pull", "{") != http.StatusBadRequest {
		t.Fatal("a broken pull body was accepted")
	}
	h.json(http.MethodPost, "/api/projects/pj_missing/skills/missing/pull", map[string]any{}, http.StatusNotFound)

	src := h.newProject(map[string]any{"name": "Library"})
	dst := h.newProject(map[string]any{"name": "Work"})
	srcID := src["id"].(string)
	dstID := dst["id"].(string)
	desc := "when tagging after the verification checks have passed"
	body := "1. run the checks\n2. tag"
	mem := h.app.Engine.ProjectMemory(srcID)
	if _, err := mem.WriteSkill("release-check", desc, body); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.WriteSkill("shell-dispatch", "when a command has to run outside the conversation", "1. run it\n2. read the output"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.WriteSkill("notes-archive", "when filing a note that should outlive the conversation", "1. write it down\n2. leave it"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Engine.ProjectMemory(dstID).WriteSkill("period-summary", desc, body); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(h.app.Engine.ProjectMemory(dstID).Dir(), memory.SkillsDir, "shell-dispatch")
	if err := os.WriteFile(blocked, []byte("not a skill directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
		"names":      []string{"release-check", "notes-archive"},
		"as":         "publish-steps",
	}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
		"names":      []string{"release-check"},
		"as":         "Not A Name",
	}, http.StatusBadRequest)

	report := h.json(http.MethodPost, "/api/projects/"+srcID+"/skills/copy", map[string]any{
		"to_project": dstID,
		"names":      []string{"", "release-check", "release-check", "../nope", "missing-one", "shell-dispatch", "notes-archive"},
	}, http.StatusOK)
	codes := map[string]string{}
	for _, raw := range report["skipped"].([]any) {
		row := raw.(map[string]any)
		codes[row["name"].(string)] = row["code"].(string)
	}
	if codes["release-check"] != "duplicate" || codes["../nope"] != "bad_name" || codes["missing-one"] != "not_found" || codes["shell-dispatch"] != "rejected" {
		t.Fatalf("skipped=%v", report["skipped"])
	}
	copied := report["copied"].([]any)
	if len(copied) != 1 || copied[0].(map[string]any)["name"] != "notes-archive" {
		t.Fatalf("copied=%v", report["copied"])
	}

	// Work's copy still matches the text it was given. The library skill then
	// moves on, so copying that snapshot back must not name the library as
	// its own source — the bytes now live on Work.
	if _, err := mem.WriteSkill("notes-archive", "when scheduling a reminder for later", "1. pick a time\n2. leave it"); err != nil {
		t.Fatal(err)
	}
	back := h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/copy", map[string]any{
		"to_project": srcID,
		"names":      []string{"notes-archive"},
		"as":         "notes-kept",
	}, http.StatusOK)
	if back["copied"].([]any)[0].(map[string]any)["name"] != "notes-kept" {
		t.Fatalf("back=%v", back)
	}
	kept := skillOrigin(t, h.readSkill(srcID, "notes-kept"))
	if kept["project_id"] != dstID {
		t.Fatalf("copying back onto the source tracked itself: %v", kept)
	}

	if err := mem.DeleteSkill("notes-archive"); err != nil {
		t.Fatal(err)
	}
	third := h.newProject(map[string]any{"name": "Third"})
	h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/copy", map[string]any{
		"to_project": third["id"],
		"names":      []string{"notes-archive"},
	}, http.StatusOK)
	again := skillOrigin(t, h.readSkill(third["id"].(string), "notes-archive"))
	if again["project_id"] != dstID {
		t.Fatalf("a missing upstream was still the link: %v", again)
	}
	h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/notes-archive/pull", map[string]any{}, http.StatusNotFound)

	if err := h.app.Engine.DeleteProject(srcID); err != nil {
		t.Fatal(err)
	}
	fourth := h.newProject(map[string]any{"name": "Fourth"})
	h.json(http.MethodPost, "/api/projects/"+dstID+"/skills/copy", map[string]any{
		"to_project": fourth["id"],
		"names":      []string{"notes-archive"},
		"as":         "notes-moved",
	}, http.StatusOK)
	moved := skillOrigin(t, h.readSkill(fourth["id"].(string), "notes-moved"))
	if moved["project_id"] != dstID {
		t.Fatalf("a deleted source project was still the link: %v", moved)
	}

	brokenID := empty["id"].(string)
	skillsDir := filepath.Join(h.app.Engine.ProjectMemory(brokenID).Dir(), memory.SkillsDir)
	if err := os.MkdirAll(filepath.Dir(skillsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillsDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/projects/"+brokenID+"/skills/copy", map[string]any{
		"to_project": dstID,
	}, http.StatusBadRequest)
}
