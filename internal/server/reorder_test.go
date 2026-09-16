package server_test

import (
	"net/http"
	"testing"
)

// Dragging a conversation has to survive a reload. Recency is the default;
// a PUT of ids is what the sidebar sends after a drop.
func TestReorderThreadsPinsTheListedOrder(t *testing.T) {
	h := newHarness(t)
	first := h.newThread()
	second := h.newThread()
	listed := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)["threads"].([]any)
	if len(listed) != 2 {
		t.Fatalf("want 2 conversations, got %d", len(listed))
	}
	// newest-created is first until someone drags
	if idOf(listed[0]) != second {
		t.Fatalf("default order is last activity, got %s then %s", idOf(listed[0]), idOf(listed[1]))
	}

	got := h.json(http.MethodPut, "/api/threads/reorder", map[string]any{
		"ids": []string{first, second},
	}, http.StatusOK)
	rows := got["threads"].([]any)
	if len(rows) != 2 || idOf(rows[0]) != first || idOf(rows[1]) != second {
		t.Fatalf("want dragged order, got %+v", rows)
	}
	again := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)["threads"].([]any)
	if idOf(again[0]) != first {
		t.Fatalf("reload lost the dragged order: %+v", again)
	}
}

func TestReorderThreadsHonorsTheProjectFilter(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "Scoped"})["id"].(string)
	inProject := h.json(http.MethodPost, "/api/threads", map[string]any{
		"project_id": p,
	}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	loose := h.newThread()
	got := h.json(http.MethodPut, "/api/threads/reorder?project="+p, map[string]any{
		"ids": []string{inProject},
	}, http.StatusOK)
	rows := got["threads"].([]any)
	if len(rows) != 1 || idOf(rows[0]) != inProject {
		t.Fatalf("a filtered reorder must not leak other conversations: %+v", rows)
	}
	all := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)["threads"].([]any)
	if len(all) != 2 {
		t.Fatalf("the other conversation must still exist, got %d", len(all))
	}
	_ = loose
}

func TestReorderProjectsPinsTheListedOrder(t *testing.T) {
	h := newHarness(t)
	a := h.newProject(map[string]any{"name": "Alpha"})["id"].(string)
	b := h.newProject(map[string]any{"name": "Beta"})["id"].(string)
	listed := h.json(http.MethodGet, "/api/projects", nil, http.StatusOK)["projects"].([]any)
	if idOf(listed[0]) != b {
		t.Fatalf("default project order is last updated, got %s then %s", idOf(listed[0]), idOf(listed[1]))
	}

	got := h.json(http.MethodPut, "/api/projects/reorder", map[string]any{
		"ids": []string{a, b},
	}, http.StatusOK)
	rows := got["projects"].([]any)
	if len(rows) != 2 || idOf(rows[0]) != a || idOf(rows[1]) != b {
		t.Fatalf("want dragged order, got %+v", rows)
	}
}

func TestReorderRejectsABadList(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPut, "/api/threads/reorder", map[string]any{
		"ids": []string{id, id},
	}, http.StatusBadRequest)
	h.json(http.MethodPut, "/api/threads/reorder", map[string]any{
		"ids": []string{id, "th_missing"},
	}, http.StatusNotFound)
	h.json(http.MethodPut, "/api/threads/reorder", "\"nope\"", http.StatusBadRequest)
	h.json(http.MethodPut, "/api/projects/reorder", "\"nope\"", http.StatusBadRequest)
	h.json(http.MethodPut, "/api/projects/reorder", map[string]any{
		"ids": []string{"pj_missing"},
	}, http.StatusNotFound)
}

func TestReorderEmptyListIsANoOp(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	got := h.json(http.MethodPut, "/api/threads/reorder", map[string]any{
		"ids": []string{},
	}, http.StatusOK)
	rows := got["threads"].([]any)
	if len(rows) != 1 || idOf(rows[0]) != id {
		t.Fatalf("empty reorder must leave the list alone, got %+v", rows)
	}
}

func idOf(row any) string {
	return row.(map[string]any)["id"].(string)
}
