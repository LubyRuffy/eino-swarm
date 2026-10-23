package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"gorm.io/gorm"
)

func TestReplayIndexesMigrateExistingDatabaseAndBoundQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Thread{ID: "legacy-thread"}
	if err := s.CreateThread(legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(legacy.ID, "legacy-turn", []Message{{Role: "user", Content: "legacy-甲"}, {Role: "tool", Content: "retained-tool"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: legacy.ID, TurnID: "legacy-turn", Kind: "agent_message", Text: "legacy-answer"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"idx_evt_thread_kind_seq", "idx_msg_thread_role_seq"} {
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
	rows, err := s.ListReplayMessages(legacy.ID, 0, "")
	if err != nil || len(rows) != 1 || rows[0].Content != "legacy-甲" {
		t.Fatalf("migrated history=%+v err=%v", rows, err)
	}
	legacyEvents, err := s.ListReplayEvents(legacy.ID, 0, "")
	if err != nil || len(legacyEvents) != 1 || legacyEvents[0].Text != "legacy-answer" {
		t.Fatalf("migrated events=%+v err=%v", legacyEvents, err)
	}
	var toolCount int64
	if err := s.db.Model(&Message{}).Where("thread_id = ? AND role = 'tool'", legacy.ID).Count(&toolCount).Error; err != nil || toolCount != 1 {
		t.Fatalf("migration changed stored tool rows: %d %v", toolCount, err)
	}
	for name, want := range map[string]string{"idx_evt_thread_kind_seq": "thread_id,kind,seq", "idx_msg_thread_role_seq": "thread_id,role,seq"} {
		var cols []struct{ Name string }
		if err := s.db.Raw("PRAGMA index_info(" + name + ")").Scan(&cols).Error; err != nil {
			t.Fatal(err)
		}
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		if strings.Join(names, ",") != want {
			t.Fatalf("%s columns=%v", name, names)
		}
	}
	var query string
	var args []any
	if err := s.db.Callback().Query().After("gorm:query").Register("test:explain_replay", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" || tx.Statement.Table == "messages" {
			query = tx.Statement.SQL.String()
			args = append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, events := range []bool{false, true} {
		if events {
			_, err = s.ListReplayEvents("thread", 3, "skip")
		} else {
			_, err = s.ListReplayMessages("thread", 3, "skip")
		}
		if err != nil {
			t.Fatal(err)
		}
		var plan []struct{ Detail string }
		if err := s.db.Raw("EXPLAIN QUERY PLAN "+query, args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		details := fmt.Sprint(plan)
		t.Log(details)
		wanted := "idx_msg_thread_role_seq"
		if events {
			wanted = "idx_evt_thread_kind_seq"
			if strings.Count(details, "idx_messages_turn_id") != 2 {
				t.Fatalf("per-turn existence lookups not indexed: %s", details)
			}
		}
		if !strings.Contains(details, wanted) {
			t.Fatalf("kind/role filter lost index: %s", details)
		}
	}
}

func TestReplayWhitespaceMatchesGoTrimSpace(t *testing.T) {
	// Enumerating the full rune space catches omissions when changing the SQL
	// dedup character set; provider/user text still retains its original bytes.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) != strings.ContainsRune(replayWhitespace, r) {
			t.Fatalf("SQL whitespace differs from TrimSpace at U+%04X", r)
		}
	}
}

func TestReplayStoreKeepsOnlyRelevantRowsAndPreservesOldAnswerIdentity(t *testing.T) {
	s := open(t)
	th := &Thread{}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	appendRows := func(turn string, rows ...Message) {
		t.Helper()
		if err := s.AppendMessages(th.ID, turn, rows); err != nil {
			t.Fatal(err)
		}
	}
	appendRows("a", Message{Role: "assistant", Content: "\u3000answer\u00a0"}, Message{Role: "user", Content: "compact"})
	appendRows("a", Message{Role: "tool", Content: strings.Repeat("T", 1<<16)})
	appendRows("b", Message{Role: "assistant", Content: ""}, Message{Role: "user", Content: "U甲", Reasoning: "excluded", ToolCalls: "excluded", EventSeq: 9, Images: []ImageRef{{ID: "image"}}})
	appendRows("skip", Message{Role: "user", Content: "skip"})
	for _, ev := range []Event{
		{TurnID: "a", Kind: "agent_message", Text: "\tanswer\u2003"},
		{TurnID: "a", Kind: "agent_message", Text: "repair"},
		{TurnID: "b", Kind: "agent_message", AgentID: "worker", Text: "excluded"},
		{TurnID: "b", Kind: "agent_message", Text: "null manager"},
		{TurnID: "missing", Kind: "agent_message", Text: "no live messages"},
		{TurnID: "skip", Kind: "agent_message", Text: "skip"},
		{TurnID: "b", Kind: "steer", Text: "caption"},
		{TurnID: "b", Kind: "steer_retracted", Text: `{"seq":1}`},
		{TurnID: "b", Kind: "tool_result", Text: strings.Repeat("T", 1<<16)},
	} {
		ev.ThreadID = th.ID
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	// The legacy ListEvents(thread, 0, 0) ignored nonpositive seq values.
	if err := s.db.Create(&Event{ThreadID: th.ID, TurnID: "a", Kind: "agent_message", Text: "nonpositive-event"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&Event{}).Where("thread_id = ? AND text = ?", th.ID, "null manager").Update("agent_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListReplayMessages(th.ID, 2, "skip")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].Content != "U甲" || len(rows[1].Images) != 1 || rows[1].EventSeq != 9 || rows[1].Reasoning != "" || rows[1].ToolCalls != "" {
		t.Fatalf("filtered messages=%+v", rows)
	}
	events, err := s.ListReplayEvents(th.ID, 2, "skip")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, ev := range events {
		texts = append(texts, ev.Text)
	}
	if strings.Join(texts, "|") != "repair|null manager|caption|{\"seq\":1}" {
		t.Fatalf("filtered events=%v", texts)
	}
	rows, err = s.ListReplayMessages(th.ID, 0, "")
	if err != nil || len(rows) != 5 {
		t.Fatalf("uncompacted rows=%d err=%v", len(rows), err)
	}
	events, err = s.ListReplayEvents(th.ID, 0, "")
	if err != nil || len(events) != 5 {
		t.Fatalf("uncompacted events=%+v err=%v", events, err)
	}
}

func TestReplayStoreReportsReadErrors(t *testing.T) {
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListReplayMessages("thread", 0, ""); err == nil || !strings.Contains(err.Error(), "list replay messages") {
		t.Fatalf("messages err=%v", err)
	}
	if _, err := s.ListReplayEvents("thread", 0, ""); err == nil || !strings.Contains(err.Error(), "list replay events") {
		t.Fatalf("events err=%v", err)
	}
}

func TestReplayRepairSearchIndexDoesNotReadToolBodies(t *testing.T) {
	s := open(t)
	th := &Thread{}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, "turn", []Message{{Role: "tool", Content: strings.Repeat("excluded", 8192)}, {Role: "\u3000UsEr\t", Content: "findable-human"}, {Role: "ASSISTANT", Content: "findable-assistant"}}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	if err := s.db.Callback().Query().After("gorm:query").Register("test:index_replay_rows", func(tx *gorm.DB) {
		if rows, ok := tx.Statement.Dest.(*[]Message); ok {
			reads++
			for _, r := range *rows {
				if !indexableRole(r.Role) {
					t.Errorf("search repair decoded tool body (%d bytes)", len(r.Content))
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("search message reads=%d", reads)
	}
	for _, q := range []string{"findable-human", "findable-assistant"} {
		hits, err := s.SearchThreads(q, 10)
		if err != nil || len(hits) != 1 {
			t.Fatalf("search %q: %v %v", q, hits, err)
		}
	}
}

func TestReplayRepairSearchIndexReportsItsFilteredReadFailure(t *testing.T) {
	s := open(t)
	th := &Thread{}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("DROP TABLE messages").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err == nil || !strings.Contains(err.Error(), "list search messages") {
		t.Fatalf("index read err=%v", err)
	}
}

// Missing-answer repair still uses the existing search persistence contract.
// Actual SQLite failures must retain whether lookup, update or insert failed.
func TestReplayRepairSearchIndexPreservesStorageFailures(t *testing.T) {
	for _, operation := range []string{"lookup", "update", "insert"} {
		t.Run(operation, func(t *testing.T) {
			s := open(t)
			th := &Thread{}
			if err := s.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "lookup":
				if err := s.db.Exec("DROP TABLE search_docs").Error; err != nil {
					t.Fatal(err)
				}
			case "update":
				if err := s.db.Exec("CREATE TRIGGER reject_search_update BEFORE UPDATE ON search_docs BEGIN SELECT RAISE(ABORT, 'blocked search update'); END").Error; err != nil {
					t.Fatal(err)
				}
			case "insert":
				if err := s.db.Exec("DELETE FROM search_docs WHERE thread_id = ?", th.ID).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.db.Exec("CREATE TRIGGER reject_search_insert BEFORE INSERT ON search_docs BEGIN SELECT RAISE(ABORT, 'blocked search insert'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := s.IndexThread(th.ID); err == nil || !strings.Contains(err.Error(), operation+" search doc") {
				t.Fatalf("%s search failure=%v", operation, err)
			}
		})
	}
}
