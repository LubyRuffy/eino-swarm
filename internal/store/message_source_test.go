package store

import (
	"errors"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMessageHumanSourceRequiresItsOwnDurableInput(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "source fixture"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, UserText: "human source"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "human event"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "steer", Text: "human steer"},
	} {
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name  string
		row   Message
		human bool
	}{
		{"turn source", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "human source"}, true},
		{"human event", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "human event"}, true},
		{"steer", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "[steer] human steer"}, true},
		{"plain runtime cannot borrow steer", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "human steer"}, false},
		{"wrong steer sequence", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "[steer] human steer", EventSeq: 999}, false},
		{"runtime prose", Message{ThreadID: th.ID, TurnID: turn.ID, Content: "runtime correction"}, false},
		{"other turn", Message{ThreadID: th.ID, TurnID: "other", Content: "human event"}, false},
		{"other thread", Message{ThreadID: "other", TurnID: turn.ID, Content: "human source"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			human, err := s.MessageHasHumanSource(test.row)
			if err != nil || human != test.human {
				t.Fatalf("human=%v want=%v err=%v", human, test.human, err)
			}
		})
	}
	for _, automatic := range []*Turn{{ThreadID: th.ID, UserText: "generated input", GoalContinue: true}, {ThreadID: th.ID, UserText: "scheduled input", ScheduleContinue: true}} {
		if err := s.CreateTurn(automatic); err != nil {
			t.Fatal(err)
		}
		human, err := s.MessageHasHumanSource(Message{ThreadID: th.ID, TurnID: automatic.ID, Content: automatic.UserText})
		if err != nil || human {
			t.Fatalf("automatic turn became human: %v %v", human, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MessageHasHumanSource(Message{}); err == nil {
		t.Fatal("unreadable source storage must fail explicitly")
	}
}

func TestReplayPreservesMessageSourceAcrossStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	th := &Thread{Title: "source persistence"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, "turn", []Message{{Role: "user", Content: "quoted runtime text", Source: MessageSourceHuman}, {Role: "user", Content: "generated input", Source: MessageSourceRuntime}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListReplayMessages(th.ID, 0, "")
	if err != nil || len(rows) != 2 || rows[0].Source != MessageSourceHuman || rows[1].Source != MessageSourceRuntime {
		t.Fatalf("stored sources=%+v err=%v", rows, err)
	}
}

func TestLegacyMessageSourcePreservesDatabaseErrorCategory(t *testing.T) {
	s := open(t)
	storageErr := errors.New("fixture source read failed")
	if err := s.db.Callback().Row().Before("gorm:row").Register("test:source_read_failure", func(tx *gorm.DB) { _ = tx.AddError(storageErr) }); err != nil {
		t.Fatal(err)
	}
	_, err := s.MessageHasHumanSource(Message{})
	if !errors.Is(err, storageErr) || !strings.Contains(err.Error(), "resolve human message source") {
		t.Fatalf("storage cause lost: %v", err)
	}
}

func TestLegacyHumanSourceBindsOnlyTheInitialUserRow(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "repeated source fixture"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, UserText: "a human quoted a correction"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: turn.UserText}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{{Role: "user", Content: turn.UserText}, {Role: "assistant", Content: "a provisional response"}, {Role: "user", Content: turn.UserText}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListReplayMessages(th.ID, 0, "")
	if err != nil || len(rows) != 3 {
		t.Fatalf("actual rows=%+v err=%v", rows, err)
	}
	for _, index := range []int{0, 2} {
		human, err := s.MessageHasHumanSource(rows[index])
		if err != nil || human != (index == 0) {
			t.Fatalf("source at actual seq=%d human=%v want=%v err=%v", rows[index].Seq, human, index == 0, err)
		}
	}
}
