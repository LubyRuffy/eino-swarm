package server_test

import (
	"net/http"
	"testing"
)

func TestNuwaProjectCreateFillsAndKeepsTheModelPair(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Default", "base_url": "http://endpoint.invalid/v1",
				"model": "planner-model", "catalog": []string{"planner-model", "executor-model"},
			}},
		},
	}, http.StatusOK)

	created := h.json(http.MethodPost, "/api/projects", map[string]any{
		"name": "Chosen models", "nuwa_enabled": true,
	}, http.StatusCreated)
	project := created["project"].(map[string]any)
	id := project["id"].(string)
	if project["nuwa_planner_provider_id"] != "default" || project["nuwa_planner_model"] != "planner-model" {
		t.Fatalf("planner was not filled from the ready catalog: %+v", project)
	}
	if project["nuwa_executor_provider_id"] != "default" || project["nuwa_executor_model"] != "executor-model" {
		t.Fatalf("executor was not the other ready model: %+v", project)
	}

	partial := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{
		"nuwa_planner_provider_id": "default", "nuwa_planner_model": "planner-model",
	}, http.StatusBadRequest)
	if partial["code"] != "nuwa_model" {
		t.Fatalf("half pair response: %+v", partial)
	}

	rejected := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{
		"nuwa_planner_provider_id": "default", "nuwa_planner_model": "planner-model",
		"nuwa_executor_provider_id": "default", "nuwa_executor_model": "missing-model",
	}, http.StatusBadRequest)
	if rejected["code"] != "nuwa_model" {
		t.Fatalf("unknown model response: %+v", rejected)
	}

	disabled := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{
		"nuwa_enabled": false,
	}, http.StatusOK)
	kept := disabled["project"].(map[string]any)
	if kept["nuwa_enabled"] != false || kept["nuwa_executor_model"] != "executor-model" || kept["nuwa_planner_model"] != "planner-model" {
		t.Fatalf("closing Nuwa cleared the pair: %+v", kept)
	}

	changed := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{
		"nuwa_planner_provider_id": "default", "nuwa_planner_model": "planner-model",
		"nuwa_executor_provider_id": "default", "nuwa_executor_model": "planner-model",
	}, http.StatusOK)
	updated := changed["project"].(map[string]any)
	if updated["nuwa_executor_model"] != "planner-model" || updated["nuwa_enabled"] != false {
		t.Fatalf("executor update did not stay on the closed project: %+v", updated)
	}

	named := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{"name": "Renamed models"}, http.StatusOK)
	if named["project"].(map[string]any)["nuwa_executor_model"] != "planner-model" {
		t.Fatalf("a name change cleared the pair: %+v", named["project"])
	}
	reopened := h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{"nuwa_enabled": true}, http.StatusOK)
	again := reopened["project"].(map[string]any)
	if again["nuwa_enabled"] != true || again["nuwa_executor_model"] != "planner-model" || again["nuwa_planner_model"] != "planner-model" {
		t.Fatalf("reopening Nuwa replaced the saved pair: %+v", again)
	}
}
