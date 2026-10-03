package config

import "testing"

func TestCompactTriggerUsesTheFixedBudgetWhenTheWindowIsUnknown(t *testing.T) {
	s := SwarmConfig{AutoCompactTokens: 80_000, GoalAutoCompactPercent: 80, CompactOutputReserveTokens: 8_192}
	if got := s.CompactTrigger(0); got != 80_000 {
		t.Fatalf("unknown window = %d, want the fixed budget", got)
	}
	// A negative window is the same as unset. Do not invent a percentage of it.
	if got := s.CompactTrigger(-1); got != 80_000 {
		t.Fatalf("negative window = %d, want the fixed budget", got)
	}
}

func TestCompactTriggerUsesPercentOfAConfirmedWindow(t *testing.T) {
	s := SwarmConfig{AutoCompactTokens: 80_000, GoalAutoCompactPercent: 80, CompactOutputReserveTokens: 8_192}
	// 80% of 256k is 204800, which is still under the output reserve.
	if got := s.CompactTrigger(256_000); got != 204_800 {
		t.Fatalf("256k window trigger = %d, want 204800", got)
	}
	if got := s.CompactTrigger(1_000_000); got != 800_000 {
		t.Fatalf("1M window trigger = %d, want 800000", got)
	}
	// The fixed 80k budget must not cap a confirmed large window.
	if got := s.CompactTrigger(1_000_000); got <= s.AutoCompactTokens {
		t.Fatalf("confirmed window collapsed to the fixed budget: %d", got)
	}
}

func TestCompactTriggerKeepsOutputHeadroomOnASmallWindow(t *testing.T) {
	s := SwarmConfig{AutoCompactTokens: 80_000, GoalAutoCompactPercent: 80, CompactOutputReserveTokens: 8_192}
	// The default 16384-token completion cap needs more room than the 8192 reserve.
	if got := s.CompactTrigger(32_000); got != 32_000-DefaultMaxCompletionTokens {
		t.Fatalf("32k window trigger = %d, want %d", got, 32_000-DefaultMaxCompletionTokens)
	}
	// The reserve is larger than this window, so the percent is the only lever.
	if got := s.CompactTrigger(4_000); got != 3_200 {
		t.Fatalf("window smaller than the reserve = %d, want 3200", got)
	}
}

func TestCompactTriggerRepairsAPercentThatRoundsToZero(t *testing.T) {
	s := SwarmConfig{AutoCompactTokens: 80_000, GoalAutoCompactPercent: 1, CompactOutputReserveTokens: 8_192}
	if got := s.CompactTrigger(1); got != 1 {
		t.Fatalf("1%% of 1 token = %d, want 1 so compression still runs", got)
	}
}

func TestCompactTriggerReservesTheConfiguredCompletionBudget(t *testing.T) {
	s := SwarmConfig{GoalAutoCompactPercent: 80, CompactOutputReserveTokens: 8_192, MaxCompletionTokens: 64_000}
	const window = 262_144
	if got := s.CompactTrigger(window); got != window-64_000 {
		t.Fatalf("trigger = %d, want %d so 198145 input tokens compact before a 64000-token completion", got, window-64_000)
	}
	if got := s.CompactTrigger(window); 198_145 <= got {
		t.Fatalf("reported failing prompt would not compact: trigger = %d", got)
	}
}

func TestCompletionTokenLimitRepairsZero(t *testing.T) {
	if (SwarmConfig{}).CompletionTokenLimit() != DefaultMaxCompletionTokens {
		t.Fatal("a blank cap must not omit max_tokens")
	}
	s := SwarmConfig{MaxCompletionTokens: 4096}
	if s.CompletionTokenLimit() != 4096 {
		t.Fatalf("cap = %d", s.CompletionTokenLimit())
	}
}

func TestCompactOutputReserveRepairsZero(t *testing.T) {
	if (SwarmConfig{}).CompactOutputReserve() != DefaultCompactOutputReserveTokens {
		t.Fatal("a blank reserve must not disable the headroom")
	}
	s := SwarmConfig{CompactOutputReserveTokens: 100}
	if s.CompactOutputReserve() != 100 {
		t.Fatalf("reserve = %d", s.CompactOutputReserve())
	}
}
