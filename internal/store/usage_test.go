package store

import "testing"

func TestSummarizeUsageCountsManagerContextSeparatelyFromWorkers(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}

	calls := []LLMCall{
		{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", PromptTokens: 800, CompletionTokens: 40, TotalTokens: 840, CachedTokens: 100},
		{ThreadID: th.ID, TurnID: turn.ID, AgentID: "worker-1", PromptTokens: 200, CompletionTokens: 30, TotalTokens: 230},
		{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", PromptTokens: 1200, CompletionTokens: 80, TotalTokens: 1280, ReasoningTokens: 20},
		{ThreadID: th.ID, TurnID: turn.ID, AgentID: "title-namer", PromptTokens: 50, CompletionTokens: 8, TotalTokens: 58},
	}
	for i := range calls {
		if err := s.AppendLLMCall(&calls[i]); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := s.SummarizeUsage(th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The meter is the last manager prompt, not the sum of every agent.
	if snap.ContextTokens != 1200 {
		t.Fatalf("context_tokens=%d, want the last manager prompt", snap.ContextTokens)
	}
	if snap.Turn.Calls != 4 || snap.Turn.PromptTokens != 800+200+1200+50 {
		t.Fatalf("turn totals wrong: %+v", snap.Turn)
	}
	if snap.Turn.CompletionTokens != 40+30+80+8 || snap.Turn.CachedTokens != 100 || snap.Turn.ReasoningTokens != 20 {
		t.Fatalf("turn token split wrong: %+v", snap.Turn)
	}
	if snap.Turn.TotalTokens != 840+230+1280+58 {
		t.Fatalf("turn total_tokens=%d", snap.Turn.TotalTokens)
	}
	if snap.Thread.Calls != snap.Turn.Calls {
		t.Fatalf("a one-turn conversation should match: thread=%+v turn=%+v", snap.Thread, snap.Turn)
	}
}

func TestSummarizeUsageFallsBackToTheLastTurnWhenNoneIsNamed(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	first := &Turn{ThreadID: th.ID, Status: TurnDone}
	second := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(first); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTurn(second); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: first.ID, AgentID: "manager", PromptTokens: 10, TotalTokens: 12}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: second.ID, AgentID: "manager", PromptTokens: 90, CompletionTokens: 10, TotalTokens: 100}); err != nil {
		t.Fatal(err)
	}

	snap, err := s.SummarizeUsage(th.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.ContextTokens != 90 {
		t.Fatalf("context_tokens=%d", snap.ContextTokens)
	}
	if snap.Turn.TotalTokens != 100 || snap.Thread.TotalTokens != 112 {
		t.Fatalf("unnamed turn should be the latest: %+v", snap)
	}
}

func TestSummarizeUsageIsZeroOnAnEmptyConversation(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	snap, err := s.SummarizeUsage(th.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.ContextTokens != 0 || snap.Thread.Calls != 0 || snap.Turn.Calls != 0 {
		t.Fatalf("empty conversation leaked numbers: %+v", snap)
	}
}

func TestSummarizeUsageFillsTotalFromPromptAndCompletion(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 11, CompletionTokens: 7,
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.SummarizeUsage(th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Turn.TotalTokens != 18 {
		t.Fatalf("a missing total_tokens must still add up, got %d", snap.Turn.TotalTokens)
	}
}
