package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SkillUsageFile keeps read activity separate from the skill's content identity.
const SkillUsageFile = "usage.json"

type skillUsage struct {
	Views        int64     `json:"views"`
	LastViewedAt time.Time `json:"last_viewed_at"`
}

// ViewSkill reads a skill for an agent and records one successful view. Other
// readers use ReadSkill so listing, copying and update checks do not count as use.
func (s *Store) ViewSkill(name string) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	skill, err := s.readSkill(name)
	if err != nil {
		return Skill{}, err
	}
	usage := skillUsage{Views: skill.Views + 1, LastViewedAt: time.Now().UTC()}
	if err := s.writeSkillUsageLocked(name, usage); err != nil {
		return Skill{}, err
	}
	skill.Views = usage.Views
	skill.LastViewedAt = usage.LastViewedAt
	return skill, nil
}

func (s *Store) skillUsagePath(name string) string {
	return filepath.Join(s.skillsDir(), name, SkillUsageFile)
}

func (s *Store) readSkillUsage(name string) skillUsage {
	raw, err := os.ReadFile(s.skillUsagePath(name))
	if err != nil {
		return skillUsage{}
	}
	var usage skillUsage
	if err := json.Unmarshal(raw, &usage); err != nil || usage.Views < 0 {
		// A damaged optional sidecar must never hide the procedure itself.
		return skillUsage{}
	}
	return usage
}

func (s *Store) writeSkillUsageLocked(name string, usage skillUsage) error {
	raw, err := json.Marshal(usage)
	if err != nil {
		return fmt.Errorf("memory: encode skill %s usage: %w", name, err)
	}
	return writeAtomic(s.skillUsagePath(name), string(raw)+"\n")
}

// mergeSkillUsageLocked writes the sum before any source directory is removed.
// A sidecar failure may leave duplicate content, but cannot discard source use.
func (s *Store) mergeSkillUsageLocked(keep string, members []Skill) (skillUsage, error) {
	var usage skillUsage
	for _, member := range members {
		usage.Views += member.Views
		if member.LastViewedAt.After(usage.LastViewedAt) {
			usage.LastViewedAt = member.LastViewedAt
		}
	}
	if err := s.writeSkillUsageLocked(keep, usage); err != nil {
		return skillUsage{}, err
	}
	return usage, nil
}

// finishSkillMergeLocked restores the keeper's accounting if deleting a source
// fails, so retrying an ordinary failed merge cannot count that source twice.
// Like the content merge, these filesystem operations are not a crash transaction.
func (s *Store) finishSkillMergeLocked(keep string, members []Skill, sources []string) (skillUsage, error) {
	usage, err := s.mergeSkillUsageLocked(keep, members)
	if err != nil {
		return skillUsage{}, err
	}
	for _, name := range sources {
		if err := s.deleteSkillLocked(name); err != nil && err != ErrNoMatch {
			var transferred []Skill
			for _, member := range members {
				if member.Name != keep {
					// RemoveAll can delete SKILL.md or usage.json before a
					// later child fails. Only counters still held by a readable
					// source are safe to take back out of the keeper.
					current, readErr := s.readSkill(member.Name)
					if readErr == nil {
						member.Views -= current.Views
						if member.Views <= 0 {
							continue
						}
					} else if !errors.Is(readErr, ErrNoMatch) {
						return skillUsage{}, errors.Join(err, readErr)
					}
				}
				transferred = append(transferred, member)
			}
			_, rollbackErr := s.mergeSkillUsageLocked(keep, transferred)
			if rollbackErr != nil {
				rollbackErr = fmt.Errorf("memory: restore usage after interrupted merge: %w", rollbackErr)
			}
			return skillUsage{}, errors.Join(err, rollbackErr)
		}
	}
	return usage, nil
}

// StaleSkillCandidates returns a stable advisory list; callers decide what to
// keep. A never-viewed skill ages from its content timestamp. Unknown dates do
// not prove staleness, and changing content does not erase a known last view.
func StaleSkillCandidates(skills []SkillInfo, now time.Time, staleDays int) []SkillInfo {
	if staleDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -staleDays)
	var candidates []SkillInfo
	for _, skill := range skills {
		last := skill.LastViewedAt
		if last.IsZero() {
			last = skill.UpdatedAt
		}
		if !last.IsZero() && !last.After(cutoff) {
			candidates = append(candidates, skill)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	return candidates
}
