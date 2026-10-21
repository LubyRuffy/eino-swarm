package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Existing databases and old events remain readable. New checkpoints preserve
// the output identity independently of their immutable event sequence numbers.
func TestEventMessageIdentitySurvivesReopenWithoutChangingLegacyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint Event
	if err := json.Unmarshal([]byte(`{"thread_id":"thread","turn_id":"turn","kind":"reasoning","message_id":"output","text":"checking"}`), &checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: "thread", Kind: "reasoning", Text: "old thought"}); err != nil {
		t.Fatal(err)
	}
	// Reopen an actual pre-field schema so AutoMigrate has to add the column.
	if err := s.DB().Exec("ALTER TABLE events DROP COLUMN message_id").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events, err := s.ListEvents("thread", 0, 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("replay: %+v, %v", events, err)
	}
	for i, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Seq != int64(i+1) {
			t.Fatalf("sequence changed on reopen: %+v", ev)
		}
		if i == 0 && strings.Contains(string(raw), "message_id") {
			t.Fatalf("legacy row received a fabricated identity: %s", raw)
		}
		if i == 1 && !strings.Contains(string(raw), `"message_id":"output"`) {
			t.Fatalf("stored output identity was lost: %s", raw)
		}
	}
}
