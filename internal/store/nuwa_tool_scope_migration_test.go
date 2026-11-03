package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNuwaToolReceiptScopeUpgradesExistingEventStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	legacy, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	withThreads(t, legacy, "thread")
	if err := legacy.AppendEvent(&Event{ThreadID: "thread", TurnID: "original", Kind: "tool_call", ToolCallID: "old"}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP INDEX idx_evt_tool_receipt_scope",
		"ALTER TABLE events DROP COLUMN tool_receipt_scope",
	} {
		if err := legacy.DB().Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if err := upgraded.AppendEvent(&Event{ThreadID: "thread", TurnID: "adopting", Kind: "tool_result", ToolCallID: "new", ToolReceiptScope: "current-execution"}); err != nil {
		t.Fatal(err)
	}
	rows, err := upgraded.ListEvents("thread", 0, 0)
	if err != nil || len(rows) != 2 || rows[0].Seq != 1 || rows[0].ToolCallID != "old" ||
		rows[0].ToolReceiptScope != "" || rows[1].Seq != 2 || rows[1].ToolReceiptScope != "current-execution" {
		t.Fatalf("upgrade changed event identity or lost scope: rows=%+v err=%v", rows, err)
	}
	raw, err := json.Marshal(rows[1])
	if err != nil || strings.Contains(string(raw), "current-execution") || strings.Contains(string(raw), "tool_receipt_scope") {
		t.Fatalf("execution scope reached the event JSON: %s err=%v", raw, err)
	}
}
