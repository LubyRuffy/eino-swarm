package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

// Pruning needs cost next to use: a never-opened skill whose index line rides
// in every prompt is the dead weight the panel exists to expose.
func TestMemoryPanelsReportWhatTheSkillIndexCosts(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Memory.SkillsIndexMax = 1
	p := h.newProject(map[string]any{"name": "memory cost"})["id"].(string)
	for _, mem := range []interface {
		WriteSkill(name, description, body string) (memory.Skill, error)
	}{h.app.Engine.ProjectMemory(p), h.app.Engine.LibraryMemory()} {
		if _, err := mem.WriteSkill("alpha-step", "when alpha applies", "do alpha"); err != nil {
			t.Fatal(err)
		}
		if _, err := mem.WriteSkill("beta-step", "when beta applies", "do beta"); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/api/projects/" + p + "/memory", "/api/library"} {
		got := h.json(http.MethodGet, path, nil, http.StatusOK)["memory"].(map[string]any)
		first := got["skills"].([]any)[0].(map[string]any)
		want := float64(len([]rune("- alpha-step — when alpha applies\n")))
		if first["index_chars"] != want || first["body_chars"] != float64(len("do alpha")) {
			t.Fatalf("%s: skill cost %v", path, first)
		}
		index := got["skill_index"].(map[string]any)
		if index["listed"] != float64(1) || index["chars"] != want || index["unviewed_chars"] != want {
			t.Fatalf("%s: index cost %v (only listed skills cost a prompt)", path, index)
		}
	}
}

func TestMemoryPanelsUseTheConfiguredStaleThreshold(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Memory.SkillStaleDays = 17
	p := h.newProject(map[string]any{"name": "memory threshold"})
	for _, path := range []string{"/api/projects/" + p["id"].(string) + "/memory", "/api/library"} {
		got := h.json(http.MethodGet, path, nil, http.StatusOK)["memory"].(map[string]any)
		if got["skill_stale_days"] != float64(17) {
			t.Errorf("%s threshold=%v", path, got["skill_stale_days"])
		}
	}
}
