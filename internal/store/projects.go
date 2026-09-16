package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// CreateProject inserts a new project, filling in the id and timestamps.
func (s *Store) CreateProject(p *Project) error {
	if p.ID == "" {
		p.ID = NewID("pj_")
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	if err := s.db.Create(p).Error; err != nil {
		return fmt.Errorf("store: create project: %w", err)
	}
	return nil
}

// GetProject loads one project.
func (s *Store) GetProject(id string) (*Project, error) {
	var p Project
	err := s.db.First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get project: %w", err)
	}
	return &p, nil
}

// ListProjects returns every project in sidebar order. Rank 0 is never
// dragged: those rows interleave by last update with ranked rows. A
// conversation in a project bumps UpdatedAt, so last used still wins
// among unranked rows.
func (s *Store) ListProjects() ([]Project, error) {
	var out []Project
	if err := s.db.Order("sort_rank asc, updated_at desc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	return interleaveByTime(out, func(p Project) int { return p.SortRank }, func(p Project) time.Time { return p.UpdatedAt }), nil
}

// UpdateProject applies a field patch to one project. Unknown ids report
// ErrNotFound rather than silently doing nothing.
func (s *Store) UpdateProject(id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()
	res := s.db.Model(&Project{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("store: update project: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project and every conversation in it. The rows go in
// one transaction; the directories on disk are the caller's to remove, the
// same way DeleteThread leaves the workspace to the engine.
func (s *Store) DeleteProject(id string) error {
	if _, err := s.GetProject(id); err != nil {
		return err
	}
	threadIDs, err := s.ListThreadIDsByProject(id)
	if err != nil {
		return err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if len(threadIDs) > 0 {
			for _, m := range []any{&Message{}, &Turn{}, &Event{}, &LLMCall{}, &Attachment{}, &Followup{}} {
				if err := tx.Where("thread_id IN ?", threadIDs).Delete(m).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("project_id = ?", id).Delete(&Thread{}).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", id).Delete(&Project{}).Error
	})
	if err != nil {
		return fmt.Errorf("store: delete project: %w", err)
	}
	s.mu.Lock()
	for _, tid := range threadIDs {
		delete(s.evtSeq, tid)
		delete(s.msgSeq, tid)
		delete(s.turnSeq, tid)
		delete(s.followupSeq, tid)
	}
	s.mu.Unlock()
	return nil
}

// ListThreadIDsByProject returns the conversations in one project. The engine
// needs the ids to stop their runtimes before the rows go away.
func (s *Store) ListThreadIDsByProject(projectID string) ([]string, error) {
	var out []string
	if err := s.db.Model(&Thread{}).Where("project_id = ?", projectID).
		Pluck("id", &out).Error; err != nil {
		return nil, fmt.Errorf("store: list project conversations: %w", err)
	}
	return out, nil
}

// SetThreadProject moves a conversation into a project, or out of every
// project when projectID is empty.
func (s *Store) SetThreadProject(threadID, projectID string) error {
	return s.UpdateThread(threadID, map[string]any{"project_id": strings.TrimSpace(projectID)})
}
