package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

func TestHTTPProjectAndLibraryCopiesStartWithIndependentViewCounts(t *testing.T) {
	for _, sharedLibrary := range []bool{false, true} {
		name := "project"
		if sharedLibrary {
			name = "shared-library"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			dest := h.newProject(map[string]any{"name": "Destination"})["id"].(string)
			sourceID := memory.LibraryID
			source := h.app.Engine.LibraryMemory()
			base := "/api/library"
			if !sharedLibrary {
				sourceID = h.newProject(map[string]any{"name": "Source"})["id"].(string)
				source = h.app.Engine.ProjectMemory(sourceID)
				base = "/api/projects/" + sourceID
			}
			const skillName = "audit-process"
			if _, err := source.WriteSkill(skillName, "When an audit process is required", "Inspect the source records and verify the resulting report."); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if _, err := source.ViewSkill(skillName); err != nil {
					t.Fatal(err)
				}
			}
			h.json(http.MethodPost, base+"/skills/copy", map[string]any{"to_project": dest, "names": []string{skillName}}, http.StatusOK)
			copied := h.readSkill(dest, skillName)
			if copied["views"] != float64(0) || copied["last_viewed_at"] != "0001-01-01T00:00:00Z" {
				t.Fatalf("copy carried source use: %v", copied)
			}
			if skillOrigin(t, copied)["project_id"] != sourceID {
				t.Fatalf("copy origin=%v", copied)
			}
			if _, err := os.Stat(filepath.Join(h.app.Engine.ProjectMemory(dest).Dir(), memory.SkillsDir, skillName, memory.SkillUsageFile)); !os.IsNotExist(err) {
				t.Fatalf("copy carried a usage file: %v", err)
			}
			for range 2 {
				opened := h.json(http.MethodGet, base+"/skills/"+skillName, nil, http.StatusOK)["skill"].(map[string]any)
				if opened["views"] != float64(3) {
					t.Fatalf("API read or copy changed use: %v", opened)
				}
			}
			catalogURL := base
			if !sharedLibrary {
				catalogURL += "/memory"
			}
			view := h.json(http.MethodGet, catalogURL, nil, http.StatusOK)["memory"].(map[string]any)
			listed := view["skills"].([]any)[0].(map[string]any)
			if listed["views"] != float64(3) || listed["last_viewed_at"] == "0001-01-01T00:00:00Z" {
				t.Fatalf("catalog omitted use: %v", listed)
			}
		})
	}
}
