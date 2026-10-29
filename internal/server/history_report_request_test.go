package server_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestHTTPHistoryReportChoiceReachesDurableTurnState(t *testing.T) {
	h := newHarness(t)
	project := h.json(http.MethodPost, "/api/projects", map[string]any{"name": "Original history", "memory_enabled": false}, http.StatusCreated)["project"].(map[string]any)["id"].(string)
	thread := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "Explicit source report", "project_id": project}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	turn := h.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "Review the original project records", "history_report": true}, http.StatusAccepted)["turn"].(map[string]any)["id"].(string)
	events, err := h.app.Store.ListTurnEvents(turn)
	if err != nil {
		t.Fatal(err)
	}
	markers := 0
	for _, event := range events {
		if event.Kind == "history_report_requested" {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("accepted HTTP report request lost its durable choice: %d", markers)
	}
	// An injection must not silently change the meaning of an existing turn.
	h.json(http.MethodPost, "/api/threads/"+thread+"/steer", map[string]any{"text": "Another request", "history_report": true}, http.StatusBadRequest)
}

func TestHistoryModeStorageFailureStopsBeforeModelInput(t *testing.T) {
	h := newHarness(t)
	thread := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "Request storage refusal"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	fault := errors.New("history mode write refused")
	if err := h.app.Store.DB().Callback().Create().Before("gorm:create").Register("history-mode-refusal", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_report_requested" {
			_ = tx.AddError(fault)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "Review original records", "history_report": true}, http.StatusBadRequest)
	var calls int64
	if err := h.app.Store.DB().Model(&store.LLMCall{}).Where("thread_id = ?", thread).Count(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("model received a request whose report mode was not durable: %d calls", calls)
	}
	var turns []store.Turn
	if err := h.app.Store.DB().Where("thread_id = ?", thread).Find(&turns).Error; err != nil || len(turns) != 1 || turns[0].Status != store.TurnError {
		t.Fatalf("failed request was left active: %+v %v", turns, err)
	}
}
