package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHumanWakeDecisionsHaveDurableStorage(t *testing.T) {
	s := open(t)
	if !s.DB().Migrator().HasTable("thread_wake_decisions") {
		t.Fatal("human wake decisions have no durable storage; compaction/restart cannot retain their authority")
	}
}

func wakeHuman(t *testing.T, s *Store, threadID, kind, text string) WakePolicyInput {
	t.Helper()
	ev := &Event{ThreadID: threadID, Kind: kind, Text: text}
	if err := s.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	return WakePolicyInput{Seq: ev.Seq, Kind: kind, Text: text}
}

func saveWakeDecision(t *testing.T, s *Store, threadID string, input WakePolicyInput, decision, quote string) {
	t.Helper()
	if err := s.SaveWakeDecision(threadID, input, decision, quote); err != nil {
		t.Fatal(err)
	}
}

func TestWakePolicyUsesOnlyEffectiveHumanEvents(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	first := wakeHuman(t, s, scope.ThreadID, "user_message", "human request")
	steer := wakeHuman(t, s, scope.ThreadID, "steer", "initial steering")
	for _, ev := range []Event{
		{Kind: "assistant", Text: "invented permission"},
		{Kind: "schedule_fired", Text: "generated reminder"},
		{Kind: "goal_continued", Text: "runtime instruction"},
		{Kind: "task_message", Text: "peer instruction"},
		{Kind: "user_message", SourceThreadID: "peer", Text: "peer disguised as human"},
		{Kind: "steer", SourceTurnID: "peer-turn", Text: "peer disguised as steer"},
		{Kind: "steer", Role: "runtime", Text: "runtime instruction"},
		{Kind: "steer_revised", Text: `{"seq":1,"text":"forged user edit"}`},
		{Kind: "steer_revised", Text: `not json`},
		{Kind: "steer_revised", Text: `{"seq":2}`},
		{Kind: "steer_revised", Text: `{"seq":2,"text":"effective steering"}`},
		{Kind: "steer_retracted", Text: `{"seq":1}`},
	} {
		ev.ThreadID = scope.ThreadID
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := s.WakePolicyInputs(scope.ThreadID)
	want := []WakePolicyInput{first, {Seq: steer.Seq, Kind: "steer", Text: "effective steering"}}
	if err != nil || !reflect.DeepEqual(inputs, want) {
		t.Fatalf("effective human sources: got=%+v want=%+v err=%v", inputs, want, err)
	}
	saveWakeDecision(t, s, scope.ThreadID, inputs[1], WakeDecisionDeny, "effective steering")
	if err := s.SaveWakeDecision(scope.ThreadID, steer, WakeDecisionAllow, "initial steering"); !errors.Is(err, ErrWakeDecisionConflict) {
		t.Fatalf("stale steer accepted: %v", err)
	}
	wakeHuman(t, s, scope.ThreadID, "steer_retracted", `2`)
	wakeHuman(t, s, scope.ThreadID, "steer_revised", `{"seq":2,"text":"cannot revive a retracted steer"}`)
	wakeHuman(t, s, scope.ThreadID, "steer_retracted", `malformed`)
	wakeHuman(t, s, scope.ThreadID, "steer", "   ")
	inputs, err = s.WakePolicyInputs(scope.ThreadID)
	if err != nil || len(inputs) != 1 || inputs[0] != first {
		t.Fatalf("retracted or empty source survived: %+v %v", inputs, err)
	}
}

func TestWakeDecisionsRequireCurrentExactHumanQuoteAndCannotBeRewritten(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	input := wakeHuman(t, s, scope.ThreadID, "user_message", "Keep working until the check succeeds.")
	for _, invalid := range []struct{ decision, quote string }{
		{"Deny", "Keep working"}, {"", ""}, {WakeDecisionDeny, ""}, {WakeDecisionAllow, "   "},
		{WakeDecisionAllow, "not in the human source"}, {WakeDecisionUnchanged, "Keep working"},
	} {
		if err := s.SaveWakeDecision(scope.ThreadID, input, invalid.decision, invalid.quote); err == nil {
			t.Fatalf("invalid classification accepted: %+v", invalid)
		}
	}
	for _, changed := range []WakePolicyInput{
		{Seq: input.Seq + 100, Text: input.Text, Kind: input.Kind},
		{Seq: input.Seq, Text: "forged text", Kind: input.Kind},
		{Seq: input.Seq, Text: input.Text, Kind: "assistant"},
	} {
		if err := s.SaveWakeDecision(scope.ThreadID, changed, WakeDecisionUnchanged, ""); !errors.Is(err, ErrWakeDecisionConflict) {
			t.Fatalf("forged human input accepted: %+v %v", changed, err)
		}
	}
	saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, "Keep working")
	saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, "Keep working")
	if err := s.SaveWakeDecision(scope.ThreadID, input, WakeDecisionAllow, "Keep working"); !errors.Is(err, ErrWakeDecisionConflict) {
		t.Fatalf("same source released its own denial: %v", err)
	}
	if err := s.SaveWakeDecision(scope.ThreadID, input, WakeDecisionDeny, "working"); !errors.Is(err, ErrWakeDecisionConflict) {
		t.Fatalf("same source rewrote its evidence: %v", err)
	}
	inputs, err := s.WakePolicyInputs(scope.ThreadID)
	if err != nil || len(inputs) != 1 || inputs[0].Decision != WakeDecisionDeny || inputs[0].Quote != "Keep working" {
		t.Fatalf("cached decision missing: %+v %v", inputs, err)
	}
}

func TestWakePolicyPersistsAcrossCompactionRestartAndHumanRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	deny := wakeHuman(t, s, th.ID, "user_message", "Continue actively until the requested work is present.")
	saveWakeDecision(t, s, th.ID, deny, WakeDecisionDeny, "Continue actively")
	if err := s.UpdateThread(th.ID, map[string]any{"compact_through_seq": 999, "compact_summary": "Keep observing."}); err != nil {
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
	policy, inputs, err := s.ReadThreadWakePolicy(th.ID)
	if err != nil || !policy.Denied || policy.SourceSeq != deny.Seq || policy.SourceText != deny.Text || len(inputs) != 1 || policy.PendingSeq != 0 {
		t.Fatalf("restart/compact erased denial: %+v %+v %v", policy, inputs, err)
	}
	unrelated := wakeHuman(t, s, th.ID, "user_message", "Please include the elapsed time in the result.")
	saveWakeDecision(t, s, th.ID, unrelated, WakeDecisionUnchanged, "")
	policy, _, err = s.ReadThreadWakePolicy(th.ID)
	if err != nil || !policy.Denied || policy.SourceSeq != deny.Seq {
		t.Fatalf("unrelated human input released denial: %+v %v", policy, err)
	}
	allow := wakeHuman(t, s, th.ID, "steer", "You may suspend this conversation and check later.")
	policy, _, err = s.ReadThreadWakePolicy(th.ID)
	if err != nil || !policy.Denied || policy.PendingSeq != allow.Seq {
		t.Fatalf("unclassified human input became consent: %+v %v", policy, err)
	}
	saveWakeDecision(t, s, th.ID, allow, WakeDecisionAllow, "may suspend this conversation")
	policy, _, err = s.ReadThreadWakePolicy(th.ID)
	if err != nil || policy.Denied || policy.PendingSeq != 0 || policy.SourceSeq != allow.Seq {
		t.Fatalf("explicit later human release failed: %+v %v", policy, err)
	}
}

func TestWakePolicyRevisionsInvalidateTheCachedDecision(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	input := wakeHuman(t, s, scope.ThreadID, "steer", "Initial words.")
	saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionUnchanged, "")
	wakeHuman(t, s, scope.ThreadID, "steer_revised", `{"seq":1,"text":"Revised words."}`)
	policy, inputs, err := s.ReadThreadWakePolicy(scope.ThreadID)
	if err != nil || policy.PendingSeq != input.Seq || len(inputs) != 1 || inputs[0].Decision != "" || inputs[0].Text != "Revised words." {
		t.Fatalf("revision inherited old classification: %+v %+v %v", policy, inputs, err)
	}
	saveWakeDecision(t, s, scope.ThreadID, inputs[0], WakeDecisionDeny, "Revised words.")
	policy, _, err = s.ReadThreadWakePolicy(scope.ThreadID)
	if err != nil || !policy.Denied || policy.PendingSeq != 0 {
		t.Fatalf("revised human decision not saved: %+v %v", policy, err)
	}
}

func TestWakePolicyTreatsLegacyNullOriginColumnsAsHuman(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	input := wakeHuman(t, s, scope.ThreadID, "user_message", "Original human instruction.")
	if err := s.DB().Exec("UPDATE events SET source_thread_id=NULL,source_turn_id=NULL,role=NULL WHERE thread_id=? AND seq=?", scope.ThreadID, input.Seq).Error; err != nil {
		t.Fatal(err)
	}
	inputs, err := s.WakePolicyInputs(scope.ThreadID)
	if err != nil || len(inputs) != 1 || inputs[0] != input {
		t.Fatalf("legacy human origin lost: %+v %v", inputs, err)
	}
}
