package config

// MemoryConfig governs a project's memory: the notes carried into every turn
// and the skill documents the agents write for themselves.
//
// The character limit is the reason the rest of it works. Memory is injected
// into the system prompt, so an unbounded store would grow the prompt of every
// turn forever; a limit forces the agent to consolidate instead of accumulate.
type MemoryConfig struct {
	// Enabled turns the memory tools and the prompt sections on. Projects can
	// still opt out one at a time.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// AutoReview runs a review after each completed turn, which is what makes
	// memory grow without anyone being asked to maintain it.
	AutoReview bool `yaml:"auto_review" json:"auto_review"`
	// CharLimit bounds MEMORY.md. A write that would exceed it fails with the
	// current entries attached, rather than silently dropping the oldest.
	CharLimit int `yaml:"char_limit" json:"char_limit"`
	// EntryMax bounds one note. Notes ride in every turn, so a runbook or a
	// procedure that would fill a quarter of the budget belongs in a skill.
	// Zero is repaired to the default; a value above CharLimit is clamped.
	EntryMax int `yaml:"entry_max" json:"entry_max"`
	// ReviewMaxIterations caps the reviewer's ReAct loop. It reads one
	// conversation and writes a handful of files; a high cap only buys a
	// runaway.
	ReviewMaxIterations int `yaml:"review_max_iterations" json:"review_max_iterations"`
	// SkillsIndexMax is how many skills the prompt lists. Only the name and
	// the one-line summary are listed, so the agent pays for the index and not
	// for every procedure it might not need.
	SkillsIndexMax int `yaml:"skills_index_max" json:"skills_index_max"`
	// Notifications is how chatty a completed review is in the transcript:
	// off (nothing), on (one line naming what changed), verbose (the line
	// plus a preview of the written text).
	Notifications string `yaml:"notifications" json:"notifications"`
}

// ReviewIterations is the reviewer's iteration cap.
func (m MemoryConfig) ReviewIterations() int {
	if m.ReviewMaxIterations <= 0 {
		return DefaultReviewMaxIterations
	}
	return m.ReviewMaxIterations
}

// Limit is the MEMORY.md character budget.
func (m MemoryConfig) Limit() int {
	if m.CharLimit <= 0 {
		return DefaultMemoryCharLimit
	}
	return m.CharLimit
}

// EntryLimit is the per-note cap the agent tools enforce. The Memory panel's
// editor still writes under the total budget: a person who pastes a longer
// note is spending their own prompt, not filling it by accident after a turn.
func (m MemoryConfig) EntryLimit() int {
	n := m.EntryMax
	if n <= 0 {
		n = DefaultMemoryEntryMax
	}
	if lim := m.Limit(); n > lim {
		return lim
	}
	return n
}

// IndexMax is how many skills the prompt lists.
func (m MemoryConfig) IndexMax() int {
	if m.SkillsIndexMax <= 0 {
		return DefaultSkillsIndexMax
	}
	return m.SkillsIndexMax
}

const (
	MemoryNotifyOff     = "off"
	MemoryNotifyOn      = "on"
	MemoryNotifyVerbose = "verbose"
)

// NotifyLevel is how a completed review is announced. An unknown value is
// treated as the default rather than as silence: a typo in the config must
// not hide that something was stored.
func (m MemoryConfig) NotifyLevel() string {
	switch m.Notifications {
	case MemoryNotifyOff, MemoryNotifyOn, MemoryNotifyVerbose:
		return m.Notifications
	default:
		return DefaultMemoryNotifications
	}
}
