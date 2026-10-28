package store

import (
	"testing"
)

func TestAtomicDecisionReceiptsCommitTogetherOrRollbackTogether(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "second-write-fails"}[fail], func(t *testing.T) {
			s := open(t)
			withThreads(t, s, "thread")
			if fail {
				if err := s.DB().Exec("CREATE TRIGGER refuse_consumption BEFORE INSERT ON events WHEN NEW.kind='model_retry' BEGIN SELECT RAISE(ABORT,'consumption unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			first := &Event{ThreadID: "thread", TurnID: "turn", AgentID: "manager", Kind: "max_iterations_continued", Text: "continuing for another 4 model rounds"}
			second := &Event{ThreadID: "thread", TurnID: "turn", AgentID: "manager", Kind: "model_retry", Text: `{"reason":"workers_uncollected"}`}
			if err := s.AppendEventsAtomic([]*Event{first, second}); (err != nil) != fail {
				t.Fatalf("decision transaction: %v", err)
			}
			events, err := s.ListTurnEvents("turn")
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				if len(events) != 0 {
					t.Fatalf("partial decision escaped rollback: %+v", events)
				}
				return
			}
			if len(events) != 2 || events[0].Kind != first.Kind || events[1].Kind != second.Kind || events[0].Seq >= events[1].Seq {
				t.Fatalf("atomic decision order changed: %+v", events)
			}
		})
	}
}

func TestAtomicDecisionReceiptsRefuseTerminalAndMixedOwners(t *testing.T) {
	s := open(t)
	for _, events := range [][]*Event{nil, {nil}, {{ThreadID: "thread", TurnID: "turn", AgentID: "manager", Kind: "done"}}, {{ThreadID: "thread", TurnID: "turn"}, {ThreadID: "other", TurnID: "turn"}}, {{ThreadID: "thread", TurnID: "turn"}, {ThreadID: "thread", TurnID: "other"}}, {{TurnID: "turn"}}} {
		if err := s.AppendEventsAtomic(events); err == nil {
			t.Fatal("invalid atomic decision bypassed its owning turn or completion watermark")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEventsAtomic([]*Event{{ThreadID: "new", TurnID: "turn", Kind: "receipt"}}); err == nil {
		t.Fatal("closed store granted a receipt sequence")
	}
}
