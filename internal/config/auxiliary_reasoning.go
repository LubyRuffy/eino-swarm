package config

import "strings"

// ReasoningInherit asks a background call to think at the conversation's own
// level instead of the background default.
const ReasoningInherit = "inherit"

// DefaultAuxiliaryReasoning is the thinking level for the engine's own
// background calls when their setting is blank: the schedule-claim and
// wake-permission classifiers and post-turn memory review. Left to the
// conversation's level, one classifier call spent 57s and 10K reasoning tokens
// deciding a yes/no question while the answer waited behind it. These calls
// classify or extract; they do not plan.
const DefaultAuxiliaryReasoning = ReasoningLow

// AuxiliaryReasoningChoices is what Settings offers for a background call
// besides the blank default.
func AuxiliaryReasoningChoices() []string {
	return []string{ReasoningInherit, ReasoningLow, ReasoningMedium, ReasoningHigh}
}

// NormalizeAuxiliaryReasoning keeps inherit or an explicit level and maps
// anything else to blank, the background default.
func NormalizeAuxiliaryReasoning(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == ReasoningInherit {
		return s
	}
	return NormalizeReasoning(s)
}

// AuxiliaryEffort is the level a background call sends. A blank setting sends
// DefaultAuxiliaryReasoning and reports it as optional: the human did not
// choose it, so an endpoint that rejects it may be asked again without it.
// An explicit level, or the conversation's level under inherit, is not
// optional; its rejection stays the endpoint's error.
func AuxiliaryEffort(setting, conversation string) (level string, optional bool) {
	switch s := NormalizeAuxiliaryReasoning(setting); s {
	case ReasoningDefault:
		return DefaultAuxiliaryReasoning, true
	case ReasoningInherit:
		return NormalizeReasoning(conversation), false
	default:
		return s, false
	}
}

// PolicyEffort is the thinking level of the schedule-claim and
// wake-permission classifiers.
func (s SwarmConfig) PolicyEffort(conversation string) (string, bool) {
	return AuxiliaryEffort(s.PolicyReasoningEffort, conversation)
}
