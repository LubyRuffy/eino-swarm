package config

// CompactTrigger is how many prompt tokens may sit on the next manager call
// before older replay is folded. A confirmed window uses
// goal_auto_compact_percent of that window, and stays at least
// compact_output_reserve tokens under the ceiling so the next completion
// still fits. An unknown window (0) uses the fixed auto_compact_tokens
// budget: a context-length rejection is what later writes the real ceiling.
func (s SwarmConfig) CompactTrigger(window int) int {
	if window <= 0 {
		return s.AutoCompactLimit()
	}
	trigger := int(int64(window) * int64(s.GoalCompactPercent()) / 100)
	reserve := s.CompactOutputReserve()
	if reserve > 0 && window > reserve {
		headroom := window - reserve
		if trigger <= 0 || headroom < trigger {
			trigger = headroom
		}
	}
	if trigger <= 0 {
		return 1
	}
	return trigger
}

// CompactOutputReserve is how many tokens stay free under a confirmed window
// for the model's own completion. Zero or negative falls back to the default.
func (s SwarmConfig) CompactOutputReserve() int {
	if s.CompactOutputReserveTokens <= 0 {
		return DefaultCompactOutputReserveTokens
	}
	return s.CompactOutputReserveTokens
}
