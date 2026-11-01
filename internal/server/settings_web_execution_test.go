package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestOlderSettingsSaveCannotResetAnExplicitLocalWebChoice(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"tools": map[string]any{"web_execution": "local"},
	}, http.StatusOK)
	if h.app.Config.Tools.WebExecution != config.WebExecutionLocal {
		t.Fatal("explicit local choice was not saved")
	}
	for _, omitted := range []map[string]any{
		{"disabled": []string{"exec"}},
		{"disabled": []string{}, "web_execution": nil},
	} {
		h.json(http.MethodPut, "/api/settings", map[string]any{"tools": omitted}, http.StatusOK)
		if h.app.Config.Tools.WebExecution != config.WebExecutionLocal {
			t.Fatal("an older settings client reset the explicit local Web tool choice")
		}
	}
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"tools": map[string]any{"web_execution": "unknown"},
	}, http.StatusBadRequest)
	if h.app.Config.Tools.WebExecution != config.WebExecutionLocal {
		t.Fatal("rejected settings partially changed Web tool execution")
	}
}

func TestConversationWebExecutionIsIndependentOfPCSettings(t *testing.T) {
	h := newHarness(t)
	thread, err := h.app.Engine.CreateThread("Conversation tools", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"local", "cloud", "auto", ""} {
		h.json(http.MethodPatch, "/api/threads/"+thread.ID, map[string]any{"web_execution": mode}, http.StatusOK)
		stored, err := h.app.Engine.Store().GetThread(thread.ID)
		if err != nil || stored.WebExecution != mode || h.app.Config.Tools.WebExecution != config.WebExecutionAuto {
			t.Fatalf("conversation preference changed the global PC setting: %+v %v", stored, err)
		}
	}
	h.json(http.MethodPatch, "/api/threads/"+thread.ID, map[string]any{"web_execution": "unknown", "title": "Should not change"}, http.StatusBadRequest)
	stored, err := h.app.Engine.Store().GetThread(thread.ID)
	if err != nil || stored.Title != "Conversation tools" || stored.WebExecution != "" {
		t.Fatal("an invalid preference partially changed the conversation")
	}
}
