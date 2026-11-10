package store

import (
	"errors"
	"strings"
	"testing"
)

func TestDreamHistoryUsesOwnedBoundedReviewAndToolTails(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	completed := func(threadID, projectID string) Turn {
		t.Helper()
		turn := Turn{ThreadID: threadID, ProjectID: projectID, ProviderID: "test-route", Status: TurnDone}
		if err := f.s.CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	record := func(turn Turn, kind, text string) Event {
		t.Helper()
		ev := Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: kind, AgentID: "reader", WorkerGeneration: 2, ToolCallID: "call", ToolReceiptScope: "scope", Text: text}
		if err := f.s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	trigger := completed(f.recipient.ID, f.project.ID)
	local := completed(f.source.ID, f.project.ID)
	foreign := completed(f.other.ID, f.foreign.ID)
	excluded := record(foreign, "memory_review", "before move")
	if err := f.s.SetThreadProject(f.other.ID, f.project.ID); err != nil {
		t.Fatal(err)
	}
	first := record(local, "memory_review", "beyond tail")
	for range dreamHistoryReviews - 1 {
		record(local, "memory_review", "owned patch")
	}
	oversized := record(local, "memory_review", strings.Repeat("界", dreamHistoryDocBytes/3+1))
	rows, err := f.s.DreamReviewEvents(t.Context(), f.project.ID, "test-route", trigger.ThreadID, trigger.ID)
	if err != nil || len(rows) != dreamHistoryReviews {
		t.Fatalf("review tail: %d rows, %v", len(rows), err)
	}
	for _, row := range rows {
		if row.ID == first.ID || row.ID == excluded.ID || row.TurnID != local.ID || row.ID == oversized.ID && row.Text != "" {
			t.Fatalf("unexpected review source or oversized body: id=%d, turn=%s, bytes=%d", row.ID, row.TurnID, len(row.Text))
		}
	}
	for _, project := range []string{"", f.foreign.ID} {
		if _, err := f.s.DreamReviewEvents(t.Context(), project, "test-route", trigger.ThreadID, trigger.ID); !errors.Is(err, ErrHistorySourceUnavailable) {
			t.Fatalf("unowned review query was accepted: %v", err)
		}
	}
	if _, err := f.s.DreamReviewEvents(t.Context(), f.project.ID, "other-route", trigger.ThreadID, trigger.ID); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("different provider acquired trigger access: %v", err)
	}
	if err := f.s.DB().Model(&Turn{}).Where("id=?", local.ID).Update("provider_id", "other-route").Error; err != nil {
		t.Fatal(err)
	}
	if rows, err := f.s.DreamReviewEvents(t.Context(), f.project.ID, "test-route", trigger.ThreadID, trigger.ID); err != nil || len(rows) != 0 {
		t.Fatalf("other provider's receipts entered the search: %d rows, %v", len(rows), err)
	}
	if _, err := f.s.DreamToolEvents(t.Context(), f.project.ID, "test-route", local.ThreadID, local.ID); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("different provider acquired old tool receipts: %v", err)
	}
	if err := f.s.DB().Model(&Turn{}).Where("id=?", local.ID).Update("provider_id", "test-route").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DreamToolEvents(t.Context(), f.project.ID, "test-route", foreign.ThreadID, foreign.ID); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("moved source was accepted: %v", err)
	}
	call := record(local, "tool_call", "exec({})")
	result := record(local, "tool_result", strings.Repeat("a", DreamToolTextBytes+1))
	rows, err = f.s.DreamToolEvents(t.Context(), f.project.ID, "test-route", local.ThreadID, local.ID)
	if err != nil || len(rows) != 2 || rows[0].ID != call.ID || rows[0].Text != call.Text || rows[0].ToolReceiptScope != call.ToolReceiptScope ||
		rows[0].WorkerGeneration != call.WorkerGeneration || rows[1].ID != result.ID || rows[1].Text != "" {
		t.Fatalf("incorrect tool observation tail: %d rows, %v", len(rows), err)
	}
	for range DreamToolEventLimit - 1 {
		record(local, "tool_result", "unrelated")
	}
	if rows, err := f.s.DreamToolEvents(t.Context(), f.project.ID, "test-route", local.ThreadID, local.ID); rows != nil || !errors.Is(err, ErrHistoryEvidenceLimit) {
		t.Fatalf("ambiguous partial scan accepted: %d rows, %v", len(rows), err)
	}
	if err := f.s.DeleteThread(trigger.ThreadID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DreamReviewEvents(t.Context(), f.project.ID, "test-route", trigger.ThreadID, trigger.ID); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("deleted trigger retained review access: %v", err)
	}
	if err := f.s.DB().Exec("DROP INDEX idx_evt_thread_kind_seq").Error; err != nil {
		t.Fatal(err)
	}
	if rows, err := f.s.DreamReviewEvents(t.Context(), f.project.ID, "test-route", local.ThreadID, local.ID); rows != nil || err == nil {
		t.Fatalf("review query failure did not fail closed: %d rows, %v", len(rows), err)
	}
	if err := f.s.DB().Exec("ALTER TABLE turns RENAME TO unavailable_turns").Error; err != nil {
		t.Fatal(err)
	}
	if rows, err := f.s.DreamToolEvents(t.Context(), f.project.ID, "test-route", local.ThreadID, local.ID); rows != nil || err == nil || errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("owner query failure disguised as missing evidence: %d rows, %v", len(rows), err)
	}

}

func TestDreamHistoryQueriesUseExistingOrderedIndexes(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	for _, test := range []struct{ query, index string }{
		{`SELECT id FROM threads WHERE project_id=? AND id<>? ORDER BY rowid DESC LIMIT 7`, "idx_threads_project_id"},
		{`SELECT id FROM events INDEXED BY idx_evt_thread_kind_seq WHERE thread_id=? AND kind=? ORDER BY seq DESC LIMIT 8`, "idx_evt_thread_kind_seq"},
		{`SELECT id FROM events INDEXED BY idx_evt_thread_turn_kind_seq WHERE thread_id=? AND turn_id=? AND kind='tool_call' ORDER BY seq DESC LIMIT 193`, "idx_evt_thread_turn_kind_seq"},
	} {
		var rows []struct{ Detail string }
		if err := f.s.DB().Raw("EXPLAIN QUERY PLAN "+test.query, "key", "target").Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || !strings.Contains(rows[0].Detail, test.index) || strings.Contains(rows[0].Detail, "TEMP B-TREE") {
			t.Fatalf("bounded tail lost its ordered index %s: %+v", test.index, rows)
		}
	}
}
