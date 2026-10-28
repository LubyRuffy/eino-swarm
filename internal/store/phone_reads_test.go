package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestPhoneTurnReadsPageOnlyTheVisiblePreviewFields(t *testing.T) {
	s := open(t)
	withThreads(t, s, "thread", "other")
	turns := []Turn{
		{ID: "old", ThreadID: "thread", Status: TurnDone, UserText: "old question", Final: "old answer", Error: "large private diagnostic"},
		{ID: "quiet", ThreadID: "thread", Status: TurnDone, Quiet: true, Final: "quiet answer"},
		{ID: "failed", ThreadID: "thread", Status: TurnError, UserText: "failed question"},
		{ID: "running", ThreadID: "thread", Status: TurnRunning, UserText: "current question"},
		{ID: "elsewhere", ThreadID: "other", Status: TurnDone, Final: "other answer"},
	}
	for i := range turns {
		if err := s.CreateTurn(&turns[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Columns added to an older database can contain NULL. Go decoded those
	// legacy fields as zero values before this query optimization.
	if err := s.db.Model(&Turn{}).Where("id = ?", "old").Update("quiet", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Turn{}).Where("id = ?", "quiet").Update("status", nil).Error; err != nil {
		t.Fatal(err)
	}
	summary, err := s.ListSummaryTurns("thread", 0, 2)
	if err != nil || len(summary) != 2 || summary[0].Seq != turns[2].Seq || summary[1].UserText != "current question" {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if summary[0].ID != "" || summary[0].Status != "" || summary[0].Error != "" {
		t.Fatalf("unused fields were loaded: %+v", summary[0])
	}
	older, err := s.ListSummaryTurns("thread", summary[0].Seq, 2)
	if err != nil || len(older) != 1 || older[0].Final != "old answer" {
		t.Fatalf("older summary=%+v err=%v", older, err)
	}
	done, err := s.ListCompletedTurnPreviews("thread", 2)
	if err != nil || len(done) != 2 || done[0].ID != "quiet" || done[1].ID != "failed" || done[1].Status != TurnError {
		t.Fatalf("completed=%+v err=%v", done, err)
	}
	if done[0].Quiet || done[0].Error != "" || done[0].ThreadID != "" {
		t.Fatalf("unused detail fields were loaded: %+v", done[0])
	}
	latest, err := s.LatestTurnID("thread")
	if err != nil || latest != "running" {
		t.Fatalf("latest=%q err=%v", latest, err)
	}
	latest, err = s.LatestTurnID("missing")
	if err != nil || latest != "" {
		t.Fatalf("missing latest=%q err=%v", latest, err)
	}
	for _, limit := range []int{0, -1} {
		if got, err := s.ListSummaryTurns("thread", 0, limit); err != nil || len(got) != 0 {
			t.Fatalf("zero summary=%+v %v", got, err)
		}
		if got, err := s.ListCompletedTurnPreviews("thread", limit); err != nil || len(got) != 0 {
			t.Fatalf("zero detail=%+v %v", got, err)
		}
	}
	if got, err := s.ListSummaryTurns("missing", 0, 2); err != nil || len(got) != 0 {
		t.Fatalf("missing summary=%+v %v", got, err)
	}
	if got, err := s.ListCompletedTurnPreviews("missing", 2); err != nil || len(got) != 0 {
		t.Fatalf("missing detail=%+v %v", got, err)
	}
}

func TestPhoneActionReadsMergeIndexedKindsAndPageBeforeOldest(t *testing.T) {
	s := open(t)
	withThreads(t, s, "thread", "another")
	events := []Event{
		{ThreadID: "thread", TurnID: "turn", Seq: 0, Kind: "delta", Text: "legacy seq zero"},
		{ThreadID: "thread", TurnID: "turn", Seq: 1, Kind: "agent_message", Text: "first", AgentID: "not needed", Err: "not needed"},
		{ThreadID: "thread", TurnID: "turn", Seq: 2, Kind: "tool_call", Text: "second"},
		{ThreadID: "thread", TurnID: "turn", Seq: 3, Kind: "delta", Text: "third"},
		{ThreadID: "thread", TurnID: "turn", Seq: 4, Kind: "tool_result", Text: strings.Repeat("x", 4096)},
		{ThreadID: "thread", TurnID: "turn", Seq: 5, Kind: "agent_message", Text: "fifth"},
		{ThreadID: "thread", TurnID: "another", Seq: 6, Kind: "delta", Text: "another turn"},
		{ThreadID: "another", TurnID: "turn", Seq: 7, Kind: "delta", Text: "another thread"},
	}
	if err := s.db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	kinds := []string{"agent_message", "delta", "tool_call"}
	got, err := s.ListTailEventsByKinds("thread", "turn", kinds, 0, 2)
	if err != nil || len(got) != 2 || got[0].Text != "third" || got[1].Text != "fifth" {
		t.Fatalf("tail=%+v err=%v", got, err)
	}
	if got[1].AgentID != "" || got[1].ThreadID != "" || got[1].ID != 0 {
		t.Fatalf("unused fields were loaded: %+v", got[1])
	}
	older, err := s.ListTailEventsByKinds("thread", "turn", kinds, got[0].Seq, 2)
	if err != nil || len(older) != 2 || older[0].Text != "first" || older[1].Text != "second" {
		t.Fatalf("older=%+v err=%v", older, err)
	}
	none, err := s.ListTailEventsByKinds("thread", "turn", kinds, older[0].Seq, 2)
	if err != nil || len(none) != 0 {
		t.Fatalf("before first=%+v err=%v", none, err)
	}
	none, err = s.ListTailEventsByKinds("thread", "turn", nil, 0, 2)
	if err != nil || len(none) != 0 {
		t.Fatalf("no kinds=%+v err=%v", none, err)
	}
	none, err = s.ListTailEventsByKinds("thread", "turn", kinds, 0, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("no limit=%+v err=%v", none, err)
	}
}

func TestPhoneReadErrorsRetainOperationAndCause(t *testing.T) {
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func() error{
		"list summary turns":           func() error { _, e := s.ListSummaryTurns("thread", 1, 2); return e },
		"list completed turn previews": func() error { _, e := s.ListCompletedTurnPreviews("thread", 2); return e },
		"latest turn id":               func() error { _, e := s.LatestTurnID("thread"); return e },
		"list tail events by kinds":    func() error { _, e := s.ListTailEventsByKinds("thread", "turn", []string{"delta"}, 1, 2); return e },
	}
	for want, run := range tests {
		if err := run(); err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("%s: %v", want, err)
		}
	}
}

func TestPhoneReadIndexesMigrateAndActualQueriesStayBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	withThreads(t, s, "thread")
	turn := Turn{ID: "legacy", ThreadID: "thread", Status: TurnDone, UserText: "request", Final: "answer"}
	if err := s.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	events := make([]Event, 200)
	for i := range events {
		events[i] = Event{ThreadID: "thread", TurnID: turn.ID, Seq: int64(i + 1), Kind: "delta", Text: strings.Repeat("x", 1024)}
	}
	if err := s.db.CreateInBatches(&events, 100).Error; err != nil {
		t.Fatal(err)
	}
	indexes := map[string]string{
		"idx_turn_thread_seq":          "thread_id,seq",
		"idx_evt_thread_turn_seq":      "thread_id,turn_id,seq",
		"idx_evt_thread_turn_kind_seq": "thread_id,turn_id,kind,seq",
	}
	for name := range indexes {
		if err := s.db.Exec("DROP INDEX " + name).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for name, want := range indexes {
		var cols []struct{ Name string }
		if err := s.db.Raw("PRAGMA index_info(" + name + ")").Scan(&cols).Error; err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, col := range cols {
			names = append(names, col.Name)
		}
		if strings.Join(names, ",") != want {
			t.Fatalf("%s columns=%v", name, names)
		}
	}
	type query struct {
		sql  string
		args []any
		rows int64
	}
	var queries []query
	if err := s.db.Callback().Query().After("gorm:query").Register("test:phone_reads", func(tx *gorm.DB) {
		queries = append(queries, query{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...), tx.RowsAffected})
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.db.Callback().Query().Remove("test:phone_reads"); err != nil {
			t.Error(err)
		}
	}()
	checks := []struct {
		name, index string
		run         func() error
	}{
		{"summary", "idx_turn_thread_seq", func() error {
			rows, e := s.ListSummaryTurns("thread", 2, 24)
			if e == nil && (len(rows) != 1 || rows[0].Final != "answer") {
				t.Fatalf("migrated summary=%+v", rows)
			}
			return e
		}},
		{"detail", "idx_turn_thread_seq", func() error {
			rows, e := s.ListCompletedTurnPreviews("thread", 24)
			if e == nil && (len(rows) != 1 || rows[0].ID != "legacy") {
				t.Fatalf("migrated detail=%+v", rows)
			}
			return e
		}},
		{"latest", "idx_turn_thread_seq", func() error {
			got, e := s.LatestTurnID("thread")
			if e == nil && got != "legacy" {
				t.Fatalf("migrated latest=%q", got)
			}
			return e
		}},
		{"preview", "idx_evt_thread_turn_kind_seq", func() error {
			rows, e := s.ListTailEventsByKinds("thread", "legacy", []string{"delta", "tool_call", "agent_message"}, 190, 24)
			if e == nil && (len(rows) != 24 || rows[0].Seq != 166 || rows[23].Seq != 189) {
				t.Fatalf("migrated actions=%+v", rows)
			}
			return e
		}},
		{"turn tail", "idx_evt_thread_turn_seq", func() error { _, _, e := s.ListTailEventsByTurn("thread", "legacy", 23); return e }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			queries = nil
			if err := check.run(); err != nil {
				t.Fatal(err)
			}
			if len(queries) == 0 {
				t.Fatal("no actual SQL captured")
			}
			for _, q := range queries {
				if q.rows > 24 || !strings.Contains(q.sql, "LIMIT") {
					t.Fatalf("unbounded read rows=%d query=%s", q.rows, q.sql)
				}
				if check.name != "turn tail" && strings.Contains(q.sql, "SELECT *") {
					t.Fatalf("unnecessary body fields: %s", q.sql)
				}
				var plan []struct{ Detail string }
				if err := s.db.Raw("EXPLAIN QUERY PLAN "+q.sql, q.args...).Scan(&plan).Error; err != nil {
					t.Fatal(err)
				}
				details := fmt.Sprint(plan)
				if !strings.Contains(details, check.index) || strings.Contains(details, "USE TEMP B-TREE") {
					t.Fatalf("wanted ordered index %s: %s query=%s", check.index, details, q.sql)
				}
			}
		})
	}
}
