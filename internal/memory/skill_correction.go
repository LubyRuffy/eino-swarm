package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const skillCorrectionKey = "dream_correction"

// ErrSkillCorrectionConflict means the complete expected body is no longer
// current, or the same correction source already set a different body.
var ErrSkillCorrectionConflict = errors.New("memory: this skill no longer matches the expected correction")

var errCorrectedSkill = errors.New("memory: skill is protected by an accepted correction")

// CorrectSkill applies an authorized correction to an existing skill. The
// caller binds source to the original human instruction. The full before body
// must match the canonical stored body. A retry of the same applied correction
// changes nothing. The content and private protection marker are saved together.
// Explicit host edits can replace the skill or remove its protection.
func (s *Store) CorrectSkill(name, before, after, source string) (Change, bool, error) {
	if err := ValidSkillName(name); err != nil {
		return Change{}, false, err
	}
	_, after = normalizeSkill("", after)
	if strings.TrimSpace(before) == "" || after == "" || strings.TrimSpace(source) == "" {
		return Change{}, false, fmt.Errorf("memory: a skill correction needs before, after and source")
	}
	digest := sha256.Sum256([]byte(source))
	marker := hex.EncodeToString(digest[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft != nil {
		return Change{}, false, fmt.Errorf("memory: skill correction is unavailable in a notes draft")
	}
	skill, err := s.readSkill(name)
	if err != nil {
		return Change{}, false, err
	}
	if skill.correction == marker && skill.Body == after {
		return Change{}, false, nil
	}
	if skill.correction == marker || skill.Body != before {
		return Change{}, false, ErrSkillCorrectionConflict
	}
	if before == after {
		return Change{}, false, nil
	}
	if err := writeAtomic(s.skillPath(name), renderSkillDocument(name, skill.Description, after, skill.Origin, marker)); err != nil {
		return Change{}, false, err
	}
	change := Change{Target: ToolSkillManage, Action: "patch", Name: name, Text: clipText(after)}
	return changeWithContent(change, before, after), true, nil
}

func (s Skill) corrected() bool { return s.correction != "" }
