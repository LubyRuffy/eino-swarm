package memory

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Refusals the tools turn into results the model can act on. Guessing a
// second skill name, or retrying a longer note, is the failure mode they
// exist to stop.
var (
	ErrDuplicateSkill   = errors.New("memory: a skill already covers this subject")
	ErrEntryTooLong     = errors.New("memory: that note is longer than one note may be")
	ErrNoteSkillOverlap = errors.New("memory: that note restates a recorded skill")
)

// DuplicateSkillError names the skill that already covers the subject, so
// the next call is skill_view + patch rather than create-with-a-suffix.
type DuplicateSkillError struct {
	Name        string
	Description string
	Reason      string
}

func (e *DuplicateSkillError) Error() string {
	why := e.Reason
	if why == "" {
		why = "same subject"
	}
	return fmt.Sprintf(
		"memory: a skill already covers this subject (%s — %s) [%s]. Do not create another. Call skill_view(%q) and patch that one, or delete it first.",
		e.Name, e.Description, why, e.Name)
}

func (e *DuplicateSkillError) Unwrap() error { return ErrDuplicateSkill }

// EntryTooLongError is a note that would ride in every turn at runbook
// length. The cap is the reason a procedure lands in a skill instead.
type EntryTooLongError struct {
	Chars int
	Max   int
}

func (e *EntryTooLongError) Error() string {
	return fmt.Sprintf(
		"memory: a note is %d characters; the per-note cap is %d. Notes ride in every turn — put a procedure or a runbook in a skill, or shorten the note to one or two sentences. Do not retry the same text.",
		e.Chars, e.Max)
}

func (e *EntryTooLongError) Unwrap() error { return ErrEntryTooLong }

// NoteSkillOverlapError is a note that restates a skill already in the
// index. Paying for it twice — once in the notes, once as a summary — is
// how the prompt fills with the same convention.
type NoteSkillOverlapError struct {
	Name        string
	Description string
}

func (e *NoteSkillOverlapError) Error() string {
	return fmt.Sprintf(
		"memory: that note restates skill %q (%s). Leave it in the skill; do not copy it into the notes.",
		e.Name, e.Description)
}

func (e *NoteSkillOverlapError) Unwrap() error { return ErrNoteSkillOverlap }

// Editions a model appends to a name instead of patching. Stripped from the
// right so weekly-rollup-loop is the same skill as weekly-rollup. The list is
// morphological, not a domain: a second procedure on a new subject still
// needs its own name.
var skillEditions = map[string]struct{}{
	"loop": {}, "v2": {}, "v3": {}, "new": {}, "alt": {}, "copy": {},
	"retry": {}, "again": {}, "variant": {}, "revised": {}, "extra": {},
	"more": {}, "next": {}, "old": {}, "tmp": {}, "wip": {},
}

const (
	// longLineRunes is the shortest body line that can identify a copied
	// procedure. Shorter lines are headings and "1. do it".
	longLineRunes = 48
	// descMinInter / descMinJaccard: two summaries of the same work, even
	// under different names.
	descMinInter   = 4
	descMinJaccard = 0.5
	// nameShareMin is how many hyphen tokens two names must share before a
	// weaker summary match still counts as the same subject.
	nameShareMin           = 2
	descWithNameMinJaccard = 0.35
	// note vs skill: high enough that a one-line preference does not match a
	// long summary by sharing "the project".
	noteDescMinInter   = 4
	noteDescMinJaccard = 0.45
	// Coverage is recall from the note's side. Jaccard against a long body
	// is always low (the union is the body); a note that copies the steps
	// still has most of its own tokens in that body.
	noteCoverMinInter = 6
	noteCoverMin      = 0.6
	noteBodyPrefix    = 800
)

func skillOverlap(name, description, body string, other Skill) string {
	if nameRelated(name, other.Name) {
		return "name"
	}
	if featuresRelated(description, other.Description, descMinInter, descMinJaccard) {
		return "summary"
	}
	if nameTokenIntersect(name, other.Name) >= nameShareMin &&
		featuresRelated(description, other.Description, descMinInter, descWithNameMinJaccard) {
		return "name+summary"
	}
	if sharedLongLine(body, other.Body) {
		return "body"
	}
	return ""
}

func noteOverlapsSkill(note string, skill Skill) bool {
	if featuresRelated(note, skill.Description, noteDescMinInter, noteDescMinJaccard) {
		return true
	}
	blob := skill.Description
	body := skill.Body
	if r := []rune(body); len(r) > noteBodyPrefix {
		body = string(r[:noteBodyPrefix])
	}
	if blob != "" && body != "" {
		blob += "\n"
	}
	blob += body
	if blob == "" {
		return false
	}
	return noteCoveredBy(note, blob)
}

func noteCoveredBy(note, blob string) bool {
	nt := featureTokens(note)
	if len(nt) < noteCoverMinInter {
		return false
	}
	st := featureTokens(blob)
	inter, _ := setOverlap(nt, st)
	if inter < noteCoverMinInter {
		return false
	}
	return float64(inter)/float64(len(nt)) >= noteCoverMin
}

func tooLong(content string, max int) *EntryTooLongError {
	if max <= 0 {
		return nil
	}
	n := utf8.RuneCountInString(content)
	if n <= max {
		return nil
	}
	return &EntryTooLongError{Chars: n, Max: max}
}

func nameRelated(a, b string) bool {
	ta, tb := nameTokens(a), nameTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	sa, sb := stripEditions(ta), stripEditions(tb)
	if len(sa) >= 2 && strings.Join(sa, "-") == strings.Join(sb, "-") {
		return true
	}
	if isTokenPrefix(sa, sb) || isTokenPrefix(sb, sa) {
		return true
	}
	inter, union := tokenListOverlap(ta, tb)
	return inter >= nameShareMin && union > 0 && float64(inter)/float64(union) >= 2.0/3.0
}

func nameTokenIntersect(a, b string) int {
	inter, _ := tokenListOverlap(nameTokens(a), nameTokens(b))
	return inter
}

func nameTokens(name string) []string {
	var out []string
	for _, p := range strings.Split(strings.ToLower(name), "-") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func stripEditions(tokens []string) []string {
	out := append([]string{}, tokens...)
	for len(out) > 1 {
		if _, ok := skillEditions[out[len(out)-1]]; !ok {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

func isTokenPrefix(short, long []string) bool {
	if len(short) < 2 || len(short) >= len(long) {
		return false
	}
	for i := range short {
		if short[i] != long[i] {
			return false
		}
	}
	return true
}

func tokenListOverlap(a, b []string) (inter, union int) {
	sa, sb := sliceSet(a), sliceSet(b)
	return setOverlap(sa, sb)
}

func sliceSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func featuresRelated(a, b string, minInter int, minJaccard float64) bool {
	inter, union := setOverlap(featureTokens(a), featureTokens(b))
	if inter < minInter || union == 0 {
		return false
	}
	return float64(inter)/float64(union) >= minJaccard
}

func setOverlap(a, b map[string]struct{}) (inter, union int) {
	union = len(a)
	for t := range b {
		if _, ok := a[t]; ok {
			inter++
		} else {
			union++
		}
	}
	return inter, union
}

// featureTokens is a cheap multilingual bag: latin words of 3+ letters, and
// overlapping CJK bigrams. It is not a parser. It only has to tell "the same
// procedure written twice" from "two unrelated one-liners".
func featureTokens(s string) map[string]struct{} {
	out := make(map[string]struct{})
	var latin strings.Builder
	var cjk []rune
	flushLatin := func() {
		w := latin.String()
		latin.Reset()
		if len(w) >= 3 {
			out[w] = struct{}{}
		}
	}
	flushCJK := func() {
		if len(cjk) >= 2 {
			for i := 0; i < len(cjk)-1; i++ {
				out[string(cjk[i:i+2])] = struct{}{}
			}
		} else if len(cjk) == 1 {
			out[string(cjk)] = struct{}{}
		}
		cjk = cjk[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			flushCJK()
			latin.WriteRune(r)
		case isIdeograph(r):
			flushLatin()
			cjk = append(cjk, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return out
}

func isIdeograph(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

func sharedLongLine(a, b string) bool {
	linesB := map[string]struct{}{}
	for _, line := range strings.Split(b, "\n") {
		line = strings.TrimSpace(line)
		if utf8.RuneCountInString(line) >= longLineRunes {
			linesB[line] = struct{}{}
		}
	}
	if len(linesB) == 0 {
		return false
	}
	for _, line := range strings.Split(a, "\n") {
		line = strings.TrimSpace(line)
		if _, ok := linesB[line]; ok {
			return true
		}
	}
	return false
}
