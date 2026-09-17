package engine

import (
	"strings"
	"unicode/utf8"
)

const (
	errUnusableBriefing   = "the model returned nothing usable as a briefing"
	errTranscriptBriefing = "the model returned a transcript dump, not a briefing"
)

var compactLinePrefixes = []string{"Tool:", "Human:", "Assistant:"}

var compactToolJSONMarkers = []string{
	`"elapsed_ms"`,
	`"full_command"`,
	`"truncated"`,
}

// acceptBriefing is the gate before a compact or session briefing is stored.
// Empty source skips the length/tail checks so a wrap-up or rolling
// session briefing is not compared against the summarizer's input.
func acceptBriefing(raw, source string) string {
	s := sanitizeCompact(raw)
	if s == "" {
		return ""
	}
	if compactTranscriptDump(s) {
		return ""
	}
	src := strings.TrimSpace(source)
	if src != "" {
		if utf8.RuneCountInString(s) >= utf8.RuneCountInString(src) {
			return ""
		}
		if briefingIsSourceTail(s, src) {
			return ""
		}
	}
	return s
}

func briefingRejectReason(raw, source string) string {
	s := sanitizeCompact(raw)
	if s == "" {
		return errUnusableBriefing
	}
	if acceptBriefing(raw, source) == "" {
		return errTranscriptBriefing
	}
	return ""
}

// compactTranscriptDump is a structural check: the summarizer echoed
// compactLines or pasted exec JSON. Markers are protocol fields, not a task.
func compactTranscriptDump(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	for _, p := range compactLinePrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	hits := 0
	for _, m := range compactToolJSONMarkers {
		if strings.Contains(s, m) {
			hits++
		}
	}
	return hits >= 2
}

func briefingIsSourceTail(briefing, source string) bool {
	b := strings.TrimSpace(briefing)
	src := strings.TrimSpace(source)
	if b == "" || src == "" {
		return false
	}
	return strings.HasSuffix(src, b)
}
