package store

import "testing"

func TestGenerationEventsExcludeUnrelatedBodiesAndKeepRequestOrder(t *testing.T) {
	s := openTestStore(t)
	for _, kind := range []string{"generation_started", "reasoning", "generation_interrupted", "generation_recovered", "generation_completed", "tool_result"} {
		ev := Event{ThreadID: "thread", TurnID: "turn", Kind: kind, MessageID: "output"}
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	other := Event{ThreadID: "other", Kind: "generation_started"}
	_ = s.AppendEvent(&other)
	got, err := s.ListGenerationEvents("thread")
	if err != nil || len(got) != 4 {
		t.Fatalf("generation reconstruction=%+v err=%v", got, err)
	}
	for i, kind := range []string{"generation_started", "generation_interrupted", "generation_recovered", "generation_completed"} {
		if got[i].Kind != kind {
			t.Fatalf("generation order changed: %+v", got)
		}
	}
	db, _ := s.db.DB()
	_ = db.Close()
	if _, err := s.ListGenerationEvents("thread"); err == nil {
		t.Fatal("storage failure was hidden")
	}
}
