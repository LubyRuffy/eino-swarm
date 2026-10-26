package store

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestTurnInputSourceUsesTheFirstUserRowAndDurableOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, source, event, want string
		noInput                   bool
	}{
		{name: "marked human", source: MessageSourceHuman, want: MessageSourceHuman},
		{name: "marked runtime", source: MessageSourceRuntime, want: MessageSourceRuntime},
		{name: "marked plan wins over later human event", source: "plan", event: "user_message", want: "plan"},
		{name: "marked schedule", source: "schedule", event: "user_message", want: "schedule"},
		{name: "legacy human event", event: "user_message", want: MessageSourceHuman},
		{name: "legacy goal continuation", event: "goal_continued"},
		{name: "legacy accepted plan", event: "plan_implemented"},
		{name: "legacy scheduled turn", event: "schedule_fired"},
		{name: "legacy steer is not a new human send", event: "steer"},
		{name: "legacy no source evidence"},
		{name: "missing input with human event", event: "user_message", noInput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			thread := &Thread{Title: tc.name}
			if err := s.CreateThread(thread); err != nil {
				t.Fatal(err)
			}
			turn := &Turn{ThreadID: thread.ID, UserText: "unclassified request"}
			if err := s.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if !tc.noInput {
				if err := s.AppendMessages(thread.ID, turn.ID, []Message{
					{Role: "assistant", Content: "earlier non-input", Source: MessageSourceRuntime},
					{Role: "user", Content: "initial input", Source: tc.source},
					{Role: "user", Content: "later human steering", Source: MessageSourceHuman},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.event != "" {
				// Identity comes from the event kind and owner, not equality
				// between model-visible text and any runtime cue.
				if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: tc.event, Text: "different event text"}); err != nil {
					t.Fatal(err)
				}
			}
			source, err := s.TurnInputSource(thread.ID, turn.ID)
			if err != nil || source != tc.want {
				t.Fatalf("source=%q, want %q, error=%v", source, tc.want, err)
			}
			for _, owner := range [][2]string{{"another-thread", turn.ID}, {thread.ID, "another-turn"}} {
				if source, err := s.TurnInputSource(owner[0], owner[1]); err != nil || source != "" {
					t.Fatalf("source escaped its thread/turn owner: source=%q, error=%v", source, err)
				}
			}
		})
	}
}

func TestTurnInputSourceDoesNotBorrowAnotherTurnsHumanEvent(t *testing.T) {
	s := open(t)
	thread := &Thread{Title: "origin ownership"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: thread.ID}
	other := &Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTurn(other); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(thread.ID, turn.ID, []Message{{Role: "user", Content: "legacy generated input"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: other.ID, Kind: "user_message", Text: "a real different request"}); err != nil {
		t.Fatal(err)
	}
	if source, err := s.TurnInputSource(thread.ID, turn.ID); err != nil || source != "" {
		t.Fatalf("another turn cannot make a generated input human: source=%q error=%v", source, err)
	}
}

func TestTurnInputSourcePreservesDatabaseErrors(t *testing.T) {
	s := open(t)
	cause := errors.New("origin query unavailable")
	// This private Store has no background actors; the fault stays installed
	// through Store.Close so no concurrent reader can race callback removal.
	if err := s.DB().Callback().Row().Before("gorm:row").Register("refuse_turn_input_source", func(tx *gorm.DB) {
		_ = tx.AddError(cause)
	}); err != nil {
		t.Fatal(err)
	}
	source, err := s.TurnInputSource("thread", "turn")
	if source != "" || !errors.Is(err, cause) || !strings.Contains(err.Error(), "turn input source") {
		t.Fatalf("source query lost its original failure: source=%q error=%v", source, err)
	}
}
