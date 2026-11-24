package memory

import (
	"fmt"
	"os"
)

func changeWithContent(change Change, before, after string) Change {
	change.Before, change.After = &before, &after
	return change
}

// changeSkill checks correction protection and reads receipts under the same
// lock as the agent's mutation. An unreadable previous file cannot be checked.
// A failed readback leaves the preview without inventing missing content.
func (s *Store) changeSkill(name string, mutate func() (Skill, error)) (Skill, Change, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, Change{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before, beforeErr := os.ReadFile(s.skillPath(name))
	if beforeErr != nil && !os.IsNotExist(beforeErr) {
		return Skill{}, Change{}, fmt.Errorf("memory: read skill %s: %w", name, beforeErr)
	}
	front, _ := splitFrontMatter(string(before))
	if front[skillCorrectionKey] != "" {
		return Skill{}, Change{}, errCorrectedSkill
	}
	skill, err := mutate()
	if err != nil {
		return skill, Change{}, err
	}
	after, afterErr := os.ReadFile(s.skillPath(name))
	if afterErr == nil || os.IsNotExist(afterErr) {
		return skill, changeWithContent(Change{}, string(before), string(after)), nil
	}
	return skill, Change{}, nil
}
