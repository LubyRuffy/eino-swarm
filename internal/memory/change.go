package memory

import "os"

func changeWithContent(change Change, before, after string) Change {
	change.Before, change.After = &before, &after
	return change
}

// changeSkill keeps receipt reads inside the same lock as the mutation. Failed
// evidence reads leave the existing preview rather than inventing missing text
// or changing an otherwise successful write into a tool failure.
func (s *Store) changeSkill(name string, mutate func() (Skill, error)) (Skill, Change, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, Change{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before, beforeErr := os.ReadFile(s.skillPath(name))
	skill, err := mutate()
	if err != nil {
		return skill, Change{}, err
	}
	after, afterErr := os.ReadFile(s.skillPath(name))
	if (beforeErr == nil || os.IsNotExist(beforeErr)) && (afterErr == nil || os.IsNotExist(afterErr)) {
		return skill, changeWithContent(Change{}, string(before), string(after)), nil
	}
	return skill, Change{}, nil
}
