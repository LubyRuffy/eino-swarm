package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// Classification completion order is not human instruction order. Exercise two
// real cancellation identities with a newer allow classified before an older
// one, including the actual schedule mutation transactions between decisions.
func TestWakeConsentTimelineKeepsOriginalOrderAndSeparateCancellationIDs(t *testing.T) {
	s, first, cancelFirst := humanCancelFixture(t)
	second := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: first.ThreadID,
		Title: "Second independent receipt", Prompt: "Inspect current work.",
		Status: ScheduleActive, NextRunAt: time.Now().UTC().Add(time.Hour)})
	turn := &Turn{ThreadID: first.ThreadID, UserText: "Inspect current work."}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	scope := &GoalTurnScope{ThreadID: first.ThreadID, TurnID: turn.ID}
	older := wakeHuman(t, s, first.ThreadID, "user_message", "You may arrange a timed check for this conversation.")
	if changed, err := s.CancelScheduleByHuman(first.ID, &cancelFirst); err != nil || changed == nil {
		t.Fatalf("first human cancellation: %+v %v", changed, err)
	}
	newer := wakeHuman(t, s, first.ThreadID, "steer", "You may now arrange another timed check here.")
	saveWakeDecision(t, s, first.ThreadID, newer, WakeDecisionAllow, newer.Text)
	policy, _, err := s.ReadThreadWakePolicy(first.ThreadID)
	if err != nil || policy.SourceSeq != newer.Seq || policy.PendingSeq != older.Seq {
		t.Fatalf("pending older source was lost: %+v %v", policy, err)
	}
	current := &Schedule{Kind: ScheduleThread, ThreadID: first.ThreadID, Title: "Later allowed check",
		Prompt: "Inspect current work.", CreatedBy: ScheduleCreatedManager, NextRunAt: time.Now().UTC().Add(time.Hour)}
	if err := s.CreateScheduleForTurn(scope, current, 50); !errors.Is(err, ErrWakePolicyPending) {
		t.Fatalf("a pending earlier human source did not block the actual write: %v", err)
	}
	repeat := humanCancelEvent(first)
	if changed, err := s.CancelScheduleByHuman(first.ID, &repeat); err != nil || changed != nil {
		t.Fatalf("old DELETE was interpreted as a newer instruction: %+v %v", changed, err)
	}
	saveWakeDecision(t, s, first.ThreadID, older, WakeDecisionAllow, older.Text)
	policy, _, err = s.ReadThreadWakePolicy(first.ThreadID)
	if err != nil || policy.Denied || policy.PendingSeq != 0 || policy.SourceSeq != newer.Seq {
		t.Fatalf("late classification overwrote the newer human decision: %+v %v", policy, err)
	}
	if err := s.CreateScheduleForTurn(scope, current, 50); err != nil {
		t.Fatalf("settled explicit permission could not create a new wait: %v", err)
	}
	before, err := s.GetSchedule(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelSecond := humanCancelEvent(second)
	if changed, err := s.CancelScheduleByHuman(second.ID, &cancelSecond); err != nil || changed == nil {
		t.Fatalf("a different ID inherited the first ID's dedupe receipt: %+v %v", changed, err)
	}
	continuation := wakeHuman(t, s, first.ThreadID, "user_message", "Continue the current work.")
	saveWakeDecision(t, s, first.ThreadID, continuation, WakeDecisionUnchanged, "")
	policy, _, err = s.ReadThreadWakePolicy(first.ThreadID)
	if err != nil || !policy.Denied || policy.SourceSeq != cancelSecond.Seq || policy.SourceText != second.ID {
		t.Fatalf("second cancellation lost its own authority: %+v %v", policy, err)
	}
	for _, operation := range []string{"create", "replace", "rearm"} {
		if err := wakeMutation(t, s, scope, before, operation)(); !errors.Is(err, ErrThreadWakeDenied) {
			t.Fatalf("%s ignored the later cancellation: %v", operation, err)
		}
	}
	after, err := s.GetSchedule(current.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("denial changed a pre-existing active wait: before=%+v after=%+v err=%v", before, after, err)
	}
}
