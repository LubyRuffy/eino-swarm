package store

import "testing"

// A restored transcript can contain higher sequence numbers than the process
// cached before the restore. The next append has to follow the database.
func TestResetSequencesReadsTheSeedAgain(t *testing.T) {
	s := &Store{
		evtSeq:      map[string]int64{"th": 1},
		msgSeq:      map[string]int64{"th": 1},
		turnSeq:     map[string]int64{"th": 1},
		followupSeq: map[string]int64{"th": 1},
	}
	s.ResetSequences()
	if got := s.nextSeq(s.evtSeq, "th", func() int64 { return 9 }); got != 10 {
		t.Fatalf("seq = %d, want 10 from the restored rows", got)
	}
}

func TestRebuildSearchIndexesTheConversation(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "search me"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Where("thread_id = ?", th.ID).Delete(&searchDoc{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RebuildSearch(); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := s.db.Model(&searchDoc{}).Where("thread_id = ?", th.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("docs = %d, want the conversation searchable again", n)
	}
}
